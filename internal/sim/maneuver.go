package sim

import (
	"math"
	"sort"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TriggerEvent selects how a node's TriggerTime is determined. v0.6.0+.
//
// Absolute (zero value) preserves the v0.1–v0.5 semantics: TriggerTime
// is set explicitly at plant time and never changes.
//
// The event-relative modes leave TriggerTime zero at plant time; the
// resolver in executeDueNodes computes TriggerTime once at the first
// Tick where the live orbit yields a future crossing (lazy freeze).
// After resolution the node behaves like an Absolute node — TriggerTime
// is frozen and the dispatch path is unchanged.
type TriggerEvent int

const (
	TriggerAbsolute TriggerEvent = iota
	TriggerNextPeri
	TriggerNextApo
	TriggerNextAN
	TriggerNextDN
)

// String returns a human-readable label for the trigger event.
func (e TriggerEvent) String() string {
	switch e {
	case TriggerAbsolute:
		return "T+"
	case TriggerNextPeri:
		return "next peri"
	case TriggerNextApo:
		return "next apo"
	case TriggerNextAN:
		return "next AN"
	case TriggerNextDN:
		return "next DN"
	}
	return "?"
}

// AllTriggerEvents lists the trigger modes in canonical UI cycle order.
var AllTriggerEvents = [...]TriggerEvent{
	TriggerAbsolute,
	TriggerNextPeri,
	TriggerNextApo,
	TriggerNextAN,
	TriggerNextDN,
}

// ManeuverNode represents a planned burn. v0.5.14+: TriggerTime is the
// burn-CENTER moment (the planner's intended firing point), not the
// burn start. For impulsive burns (Duration=0) center == start ==
// TriggerTime. For finite burns the integrator actually starts the
// burn at TriggerTime - Duration/2 so the burn is centered on
// TriggerTime. The HUD displays TriggerTime as "T+(burn moment)" so
// the player sees the planner's intent, not the implementation start.
//
// Duration controls finite vs impulsive: zero = instant Δv (legacy v0.1
// path); non-zero = sustained engine burn lasting up to Duration or
// until DV is delivered, whichever first. Finite-burn execution is
// driven by World.ActiveBurn during subsequent ticks.
//
// PrimaryID is the body whose frame the burn was planned in (empty =
// the craft's home primary at plant time, which is the v0.1 default
// and keeps legacy nodes working). Auto-plant transfers (v0.3.1) plant
// a geocentric departure plus a heliocentric arrival; PrimaryID lets
// the planner UI render a frame-distinct glyph and lets the burn-
// execution layer warn if a node fires in an unexpected frame.
//
// Event (v0.6.0+) selects whether TriggerTime is absolute or resolved
// from a live-orbit event. Zero value = TriggerAbsolute, preserving
// pre-v0.6 semantics. For event-relative nodes TriggerTime is zero at
// plant time and resolved on the first Tick where the orbit yields a
// future crossing.
type ManeuverNode struct {
	TriggerTime time.Time
	Mode        spacecraft.BurnMode
	DV          float64
	Duration    time.Duration
	PrimaryID   string
	Event       TriggerEvent
}

// IsResolved reports whether the node's TriggerTime has been set —
// either because the node was planted with TriggerAbsolute or because
// the lazy-freeze resolver has fired for an event-relative node.
// Unresolved event-relative nodes have TriggerTime == zero.
func (n ManeuverNode) IsResolved() bool {
	return n.Event == TriggerAbsolute || !n.TriggerTime.IsZero()
}

// BurnStart returns the sim-time at which the integrator should fire
// this node's burn. For impulsive nodes (Duration=0) BurnStart equals
// TriggerTime. For finite nodes BurnStart is `TriggerTime - Duration/2`
// so the burn is centered on TriggerTime. v0.5.14+.
func (n ManeuverNode) BurnStart() time.Time {
	if n.Duration <= 0 {
		return n.TriggerTime
	}
	return n.TriggerTime.Add(-n.Duration / 2)
}

// BurnEnd returns the sim-time at which the integrator should
// terminate this node's burn (regardless of Δv-remaining or fuel
// state). For impulsive nodes BurnEnd equals TriggerTime. v0.5.14+.
func (n ManeuverNode) BurnEnd() time.Time {
	if n.Duration <= 0 {
		return n.TriggerTime
	}
	return n.TriggerTime.Add(n.Duration / 2)
}

// ActiveBurn is the runtime state of an in-progress finite burn. Set by
// executeDueNodes when a node with Duration>0 fires; cleared by the
// integrator when DVRemaining hits zero or SimTime passes EndTime.
// PrimaryID is propagated from the originating ManeuverNode (empty for
// legacy nodes) — diagnostic only; the integrator always works in the
// craft's current primary frame.
type ActiveBurn struct {
	Mode        spacecraft.BurnMode
	DVRemaining float64
	EndTime     time.Time
	PrimaryID   string
}

// PlanNode inserts a node into World.Nodes, keeping the slice sorted by
// TriggerTime. Past-dated nodes are allowed — they fire on the next Tick.
//
// v0.6.0: unresolved event-relative nodes (Event != Absolute and
// TriggerTime not yet set by the lazy-freeze resolver) sort to the end
// of the slice so they don't trip the dispatch path's "next due" walk
// before resolveEventNodes runs.
func (w *World) PlanNode(n ManeuverNode) {
	w.Nodes = append(w.Nodes, n)
	sortNodes(w.Nodes)
}

// sortNodes orders nodes by TriggerTime ascending, with unresolved
// event-relative nodes pushed to the end. Used by PlanNode and by
// resolveEventNodes after it freezes a previously-unresolved node.
func sortNodes(nodes []ManeuverNode) {
	sort.Slice(nodes, func(i, j int) bool {
		ri, rj := nodes[i].IsResolved(), nodes[j].IsResolved()
		if ri != rj {
			return ri
		}
		return nodes[i].TriggerTime.Before(nodes[j].TriggerTime)
	})
}

// resolveEventNodes attempts to resolve every unresolved event-relative
// node against the craft's current orbit, freezing TriggerTime to the
// next-event sim-time when a future crossing exists. Called once per
// Tick before the warp-clamp + dispatch pass.
//
// Resolution failure (no future crossing — escape trajectory, equatorial
// orbit asking for AN/DN, etc.) leaves the node unresolved; the helper
// will retry on subsequent ticks. The retry cost is one
// ElementsFromState call per unresolved node per tick — negligible.
func (w *World) resolveEventNodes() {
	if w.Craft == nil {
		return
	}
	mu := w.Craft.Primary.GravitationalParameter()
	if mu == 0 {
		return
	}
	state := orbital.Vec3State{R: w.Craft.State.R, V: w.Craft.State.V}
	resolvedAny := false
	for i := range w.Nodes {
		n := &w.Nodes[i]
		if n.IsResolved() {
			continue
		}
		var dt float64
		switch n.Event {
		case TriggerNextPeri:
			dt = orbital.TimeToPeriapsis(state, mu)
		case TriggerNextApo:
			dt = orbital.TimeToApoapsis(state, mu)
		case TriggerNextAN:
			dt = orbital.TimeToNodeCrossing(state, mu, true)
		case TriggerNextDN:
			dt = orbital.TimeToNodeCrossing(state, mu, false)
		default:
			continue
		}
		if dt < 0 {
			continue // unreachable from current state — retry next tick
		}
		// TriggerTime is the burn-CENTER per v0.5.14 semantics; for
		// finite burns the integrator fires at TriggerTime - Duration/2.
		// We still anchor the *event* to the burn center, so the
		// player's "fire at next periapsis" intent matches the moment
		// the orbit reaches that ν.
		n.TriggerTime = w.Clock.SimTime.Add(time.Duration(dt * float64(time.Second)))
		if n.PrimaryID == "" {
			n.PrimaryID = w.Craft.Primary.ID
		}
		resolvedAny = true
	}
	if resolvedAny {
		sortNodes(w.Nodes)
	}
}

// PlanTransfer constructs a Hohmann auto-plant to the body at the given
// index in the active system and plants the resulting two-burn plan
// onto World.Nodes (departure + arrival). Returns the plan so callers
// can inspect Δv totals; returns nil and an error if the geometry is
// degenerate (target index invalid, target is the system primary, or
// craft state isn't ready).
//
// Phasing is not enforced — the plan assumes ideal alignment, matching
// the v0.3.1 sandbox scope per docs/plan.md. Porkchop-plot polish for
// real launch windows is v0.3.2.
func (w *World) PlanTransfer(targetIdx int) (*planner.TransferPlan, error) {
	sys := w.System()
	if targetIdx <= 0 || targetIdx >= len(sys.Bodies) {
		return nil, errInvalidTransferTarget
	}
	if w.Craft == nil {
		return nil, errNoCraftForTransfer
	}
	target := sys.Bodies[targetIdx]
	if target.SemimajorAxis == 0 {
		return nil, errInvalidTransferTarget
	}

	rPark := w.Craft.State.R.Norm()
	rCapture := target.RadiusMeters() + 200e3
	muDestination := target.GravitationalParameter()

	// v0.5.7: if target shares the craft's primary (e.g. craft in LEO
	// targeting Luna, both around Earth), use intra-primary Hohmann.
	// The patched-conic inter-primary path is wrong for in-SOI targets
	// (it adds an Earth-escape burn that isn't physically required —
	// craft and target both stay inside the shared primary's SOI).
	//
	// v0.5.9: pass current craft + target angles so the planner can
	// phase-correct the launch window. Without phasing, craft arrives
	// at apoapsis but target is somewhere else along its orbit and
	// the rendezvous misses.
	if target.ParentID == w.Craft.Primary.ID {
		muShared := w.Craft.Primary.GravitationalParameter()
		rArrival := target.SemimajorAxisMeters()
		craftAngle := math.Atan2(w.Craft.State.R.Y, w.Craft.State.R.X)
		// Target's position in its parent's frame == craft's primary
		// here, since target.ParentID == craft.Primary.ID.
		targetAngle := primaryFrameAngle(w, target)
		// v0.5.13: back to finite burns. With the new S-IVB-1 vessel
		// (J-2 thrust 1023 kN), TLI burn is ~110 s — half-arc 2.5°,
		// finite-burn integration loss < 0.1%. Pre-v0.5.13 the ICPS-
		// class vessel had a 10-min TLI that dropped apoapsis ~27% from
		// the impulsive ideal, forcing the impulsive workaround.
		// v0.6 finite-burn-aware planner will close the remaining gap
		// for low-TWR vessels (e.g. when ICPS comes back as a test
		// loadout).
		mass := w.Craft.TotalMass()
		thrust := w.Craft.Thrust
		dvDepEstimate := estimateIntraPrimaryDepDv(muShared, rPark, rArrival)
		var minLead float64
		if thrust > 0 && mass > 0 {
			minLead = (dvDepEstimate * mass / thrust) / 2
		}
		plan, err := planner.PlanIntraPrimaryHohmann(
			muShared, rPark, rArrival,
			craftAngle, targetAngle, minLead,
			w.Craft.Primary.ID,
			muDestination, rCapture, target.ID,
		)
		if err != nil {
			return nil, err
		}
		// v0.6.2: refine the departure Δv so the FINITE burn delivers
		// the target apoapsis under integration. For the S-IVB-1
		// default the impulsive guess is already < 0.1 % off so the
		// iterator converges in 1-2 steps; for low-TWR loadouts where
		// the burn arc is a non-trivial fraction of the parking
		// orbit, the iterator catches errors of several percent.
		// Iteration failure (max-iter, derivative collapse) silently
		// falls back to the impulsive guess.
		refineFiniteDeparture(&plan, muShared, rPark, mass, thrust, w.Craft.Isp, rArrival)
		now := w.Clock.SimTime
		w.PlanNode(transferNodeToManeuver(plan.Departure, now, mass, thrust))
		w.PlanNode(transferNodeToManeuver(plan.Arrival, now, mass, thrust))
		return &plan, nil
	}

	// v0.6.3: target is the craft's primary's parent (e.g., craft in
	// Luna's SOI, target Earth). The pre-v0.6.3 fallthrough sent these
	// to the heliocentric Hohmann path, which treated Earth's
	// heliocentric semimajor axis as the destination radius around
	// Luna and produced nonsensical Δv. The moon-escape planner instead
	// targets a bound transfer ellipse whose apolune sits on Luna's
	// SOI; the SOI-aware integrator then drops the craft into Earth's
	// frame automatically. The arrival node is a zero-Δv frame marker
	// — the player plants their own circularization once they see the
	// post-escape Earth-frame trajectory.
	if w.Craft.Primary.ParentID != "" && target.ID == w.Craft.Primary.ParentID {
		moon := w.Craft.Primary
		moonParent := sys.ParentOf(moon)
		if moonParent == nil {
			return nil, errInvalidTransferTarget
		}
		muMoon := moon.GravitationalParameter()
		rSOI := physics.SOIRadius(moon, *moonParent)
		if rSOI == 0 || rSOI <= rPark {
			return nil, errInvalidTransferTarget
		}
		mass := w.Craft.TotalMass()
		thrust := w.Craft.Thrust
		// Pre-size the centered-finite-burn lead pad from the impulsive
		// estimate, mirroring the intra-primary branch above.
		aT := (rPark + rSOI) / 2
		vCirc := math.Sqrt(muMoon / rPark)
		vTransAtPeri := math.Sqrt(muMoon * (2/rPark - 1/aT))
		dvEstimate := vTransAtPeri - vCirc
		var minLead float64
		if thrust > 0 && mass > 0 && dvEstimate > 0 {
			minLead = (dvEstimate * mass / thrust) / 2
		}
		plan, err := planner.PlanMoonEscape(muMoon, rPark, rSOI, minLead, moon.ID, target.ID)
		if err != nil {
			return nil, err
		}
		// Reuse v0.6.2's iterator: target the SOI radius as apolune so
		// finite-burn integration delivers the bound transfer ellipse
		// the impulsive math designed.
		refineFiniteDeparture(&plan, muMoon, rPark, mass, thrust, w.Craft.Isp, rSOI)
		now := w.Clock.SimTime
		w.PlanNode(transferNodeToManeuver(plan.Departure, now, mass, thrust))
		w.PlanNode(transferNodeToManeuver(plan.Arrival, now, mass, thrust))
		return &plan, nil
	}

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()
	rDeparture := w.CraftInertial().Norm()
	rArrival := target.SemimajorAxisMeters()

	muDeparture := w.Craft.Primary.GravitationalParameter()

	plan, err := planner.PlanHohmannTransfer(
		muSun, rDeparture, rArrival,
		muDeparture, rPark, w.Craft.Primary.ID,
		muDestination, rCapture, target.ID,
	)
	if err != nil {
		return nil, err
	}

	now := w.Clock.SimTime
	mass := w.Craft.TotalMass()
	thrust := w.Craft.Thrust
	w.PlanNode(transferNodeToManeuver(plan.Departure, now, mass, thrust))
	w.PlanNode(transferNodeToManeuver(plan.Arrival, now, mass, thrust))
	return &plan, nil
}

// RefinePlan re-runs a heliocentric Lambert from the craft's current
// state to the destination body at the pending arrival node's
// TriggerTime, plants a mid-course correction burn at the current
// sim-time for Δv = |v1_lambert − v_craft_heliocentric|, and replaces
// the arrival node's Δv with |v2_lambert − v_target_heliocentric| via
// CaptureBurnDeltaV. Closes the porkchop / PlanTransfer loop by giving
// the player a way to correct drift during a coast.
//
// Returns (correctionDv, refinedArrivalDv, error). err != nil if no
// pending arrival node exists (PlanTransfer / PlanTransferAt hasn't
// been called, or arrival already fired) or Lambert fails to converge.
//
// The correction burn's mode (prograde vs retrograde) is picked by the
// sign of (v1_lambert − v_craft) · v_craft: aligned → prograde, else
// retrograde. This is a scalar approximation — full vector mid-course
// correction would need a new burn mode; for v0.4.1 scalar-along-
// velocity corrections are sufficient to close small drifts.
func (w *World) RefinePlan() (correctionDv, arrivalDv float64, err error) {
	if w.Craft == nil {
		return 0, 0, errNoCraftForTransfer
	}
	// Find the latest pending "arrival" node — one whose PrimaryID
	// identifies a non-home body. PlanTransfer / PlanTransferAt plants
	// arrival with PrimaryID = target.ID.
	arrIdx := -1
	for i := len(w.Nodes) - 1; i >= 0; i-- {
		n := w.Nodes[i]
		if n.PrimaryID != "" && n.PrimaryID != w.Craft.Primary.ID {
			arrIdx = i
			break
		}
	}
	if arrIdx < 0 {
		return 0, 0, errNoRefineTarget
	}
	arrNode := w.Nodes[arrIdx]
	sys := w.System()
	var target bodies.CelestialBody
	targetFound := false
	for _, b := range sys.Bodies {
		if b.ID == arrNode.PrimaryID {
			target = b
			targetFound = true
			break
		}
	}
	if !targetFound {
		return 0, 0, errNoRefineTarget
	}

	now := w.Clock.SimTime
	tof := arrNode.TriggerTime.Sub(now).Seconds()
	if tof <= 0 {
		return 0, 0, errNoRefineTarget
	}

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()

	// Craft's heliocentric state now.
	vCraftHelio := w.bodyInertialVelocity(w.Craft.Primary).Add(w.Craft.State.V)
	rCraftHelio := w.BodyPosition(w.Craft.Primary).Add(w.Craft.State.R)

	// Target's heliocentric state at arrival time.
	arrEph := w.bodyEphemeris(target)
	rArr, vArrBody := arrEph(float64(arrNode.TriggerTime.Unix()))

	v1, v2, err := planner.LambertSolve(rCraftHelio, rArr, tof, muSun)
	if err != nil {
		return 0, 0, err
	}

	// Correction burn: Δv to transition craft from v_current to v1.
	dvVec := v1.Sub(vCraftHelio)
	correctionDv = dvVec.Norm()
	alignment := dvVec.X*vCraftHelio.X + dvVec.Y*vCraftHelio.Y + dvVec.Z*vCraftHelio.Z
	correctionMode := spacecraft.BurnPrograde
	if alignment < 0 {
		correctionMode = spacecraft.BurnRetrograde
	}

	// Arrival burn: updated Δv based on refined Lambert v2.
	vInfArr := v2.Sub(vArrBody).Norm()
	muDest := target.GravitationalParameter()
	rCapture := target.RadiusMeters() + 200e3
	arrivalDv, err = planner.CaptureBurnDeltaV(vInfArr, muDest, rCapture)
	if err != nil {
		return 0, 0, err
	}

	mass := w.Craft.TotalMass()
	thrust := w.Craft.Thrust

	// Plant the correction burn at now (tiny offset to avoid firing the
	// same tick it lands if executeDueNodes has already run).
	var correctionDur time.Duration
	if thrust > 0 && mass > 0 && correctionDv > 0 {
		correctionDur = time.Duration(correctionDv * mass / thrust * float64(time.Second))
	}
	if correctionDv > 0 {
		w.PlanNode(ManeuverNode{
			TriggerTime: now.Add(time.Second),
			Mode:        correctionMode,
			DV:          correctionDv,
			Duration:    correctionDur,
			PrimaryID:   w.Craft.Primary.ID,
		})
	}

	// Rebuild the arrival node in place with refined Δv.
	newArrival := ManeuverNode{
		TriggerTime: arrNode.TriggerTime,
		Mode:        arrNode.Mode,
		DV:          arrivalDv,
		PrimaryID:   arrNode.PrimaryID,
	}
	if thrust > 0 && mass > 0 && arrivalDv > 0 {
		newArrival.Duration = time.Duration(arrivalDv * mass / thrust * float64(time.Second))
	}
	// Find arrNode again by index after PlanNode sorted the slice.
	for i, n := range w.Nodes {
		if n.TriggerTime.Equal(arrNode.TriggerTime) && n.PrimaryID == arrNode.PrimaryID {
			w.Nodes[i] = newArrival
			break
		}
	}
	return correctionDv, arrivalDv, nil
}

// PlanTransferAt constructs a Lambert-based transfer for a specific
// (departure-day, time-of-flight) pair — the cell selected on the
// porkchop plot — and plants the resulting two-burn plan onto
// World.Nodes. Parking and capture orbit parameters match PlanTransfer
// / PorkchopGrid so a cell's planted Δv equals the cell's scored Δv
// to within Lambert iteration tolerance.
//
// depDay / tofDay are in days; depDay is an offset from w.Clock.SimTime.
// Used by the porkchop screen's Enter-to-plant path (v0.4.1).
func (w *World) PlanTransferAt(targetIdx int, depDay, tofDay float64) (*planner.TransferPlan, error) {
	sys := w.System()
	if targetIdx <= 0 || targetIdx >= len(sys.Bodies) {
		return nil, errInvalidTransferTarget
	}
	if w.Craft == nil {
		return nil, errNoCraftForTransfer
	}
	if tofDay <= 0 {
		return nil, errInvalidTransferTarget
	}
	target := sys.Bodies[targetIdx]
	if target.SemimajorAxis == 0 {
		return nil, errInvalidTransferTarget
	}
	// v0.5.7: porkchop / Lambert is heliocentric — invalid for in-SOI
	// targets (moon of craft's primary). Caller (porkchop screen) shows
	// a banner directing the user to `P` (PlanTransfer auto-plants the
	// intra-primary Hohmann correctly).
	if target.ParentID == w.Craft.Primary.ID {
		return nil, errSamePrimaryUseHohmann
	}

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()
	rPark := w.Craft.State.R.Norm()
	muDep := w.Craft.Primary.GravitationalParameter()
	rCapture := target.RadiusMeters() + 200e3
	muArr := target.GravitationalParameter()

	depEph := w.bodyEphemeris(w.Craft.Primary)
	arrEph := w.bodyEphemeris(target)
	const secondsPerDay = 86400.0
	epoch0 := float64(w.Clock.SimTime.Unix())
	tDep := epoch0 + depDay*secondsPerDay
	tArr := tDep + tofDay*secondsPerDay
	rDep, vDep := depEph(tDep)
	rArr, vArr := arrEph(tArr)

	depOffset := time.Duration(depDay * secondsPerDay * float64(time.Second))
	plan, err := planner.PlanLambertTransfer(
		muSun,
		rDep, vDep,
		rArr, vArr,
		tofDay*secondsPerDay,
		muDep, rPark, w.Craft.Primary.ID,
		muArr, rCapture, target.ID,
		depOffset,
	)
	if err != nil {
		return nil, err
	}

	now := w.Clock.SimTime
	mass := w.Craft.TotalMass()
	thrust := w.Craft.Thrust
	w.PlanNode(transferNodeToManeuver(plan.Departure, now, mass, thrust))
	w.PlanNode(transferNodeToManeuver(plan.Arrival, now, mass, thrust))
	return &plan, nil
}

// PorkchopGrid computes a launch-window grid for a Hohmann-style
// transfer to the target body. Axes: depDays (offsets from now) and
// tofDays (time of flight). Each cell = total Δv (departure + capture,
// m/s); NaN for cells where Lambert didn't converge.
//
// Uses the same parking-orbit and capture-orbit defaults as PlanTransfer
// (craft's current |r| at departure, 200 km altitude at destination).
//
// v0.5.7: rejects same-primary targets (moon of craft's primary) with
// errSamePrimaryUseHohmann — the heliocentric Lambert math doesn't
// model in-SOI transfers. The porkchop screen surfaces the error as a
// "use [P] for Hohmann" banner.
func (w *World) PorkchopGrid(targetIdx int, depDays, tofDays []float64) ([][]float64, error) {
	sys := w.System()
	if targetIdx <= 0 || targetIdx >= len(sys.Bodies) {
		return nil, errInvalidTransferTarget
	}
	if w.Craft == nil {
		return nil, errNoCraftForTransfer
	}
	target := sys.Bodies[targetIdx]
	if target.SemimajorAxis == 0 {
		return nil, errInvalidTransferTarget
	}
	if target.ParentID == w.Craft.Primary.ID {
		return nil, errSamePrimaryUseHohmann
	}

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()
	rPark := w.Craft.State.R.Norm()
	muDep := w.Craft.Primary.GravitationalParameter()
	rCapture := target.RadiusMeters() + 200e3
	muArr := target.GravitationalParameter()

	// Build ephemerides that evaluate heliocentric r, v at arbitrary
	// epochs. Reuses the existing Kepler/calculator machinery.
	dep := w.bodyEphemeris(w.Craft.Primary)
	arr := w.bodyEphemeris(target)
	epoch0 := float64(w.Clock.SimTime.Unix())

	grid := planner.PorkchopGrid(
		muSun, dep, arr, epoch0,
		depDays, tofDays,
		muDep, rPark,
		muArr, rCapture,
	)
	return grid, nil
}

// compensateFiniteBurn inflates an ideal-impulsive Δv to account for
// gravity-rotation loss during a centered finite burn at the parking
// orbit's periapsis. Without compensation, a 4% loss on Hohmann TLI
// drops apoapsis from Luna distance to ~165k km — nowhere near the
// moon.
//
// Math: a centered burn over half-arc α sweeps the prograde direction
// from -α to +α relative to periapsis tangent. The along-track
// component of the integrated Δv is Δv_ideal × sin(α)/α (perpendicular
// components sum to zero by symmetry). To deliver Δv_target we request
// Δv such that Δv × sin(α(Δv))/α(Δv) = Δv_target, where
//   α(Δv) = (Δv·m / F) × n_craft / 2 = k · Δv,  k = m·n/(2F)
// Substituting: sin(k·Δv) = k·Δv_target → Δv = asin(k·Δv_target)/k.
//
// Returns the ideal Δv unchanged if k·Δv_target ≥ 1 (geometrically
// impossible — burn arc would exceed half the orbit), or if any input
// is degenerate.
func compensateFiniteBurn(dvIdeal, mass, thrust, mu, rPark float64) float64 {
	if dvIdeal <= 0 || mass <= 0 || thrust <= 0 || mu <= 0 || rPark <= 0 {
		return dvIdeal
	}
	nCraft := math.Sqrt(mu / (rPark * rPark * rPark))
	k := mass * nCraft / (2 * thrust)
	if k <= 0 {
		return dvIdeal
	}
	arg := k * dvIdeal
	if arg >= 1 {
		// Geometrically can't deliver this Δv with a centered burn —
		// the half-arc would exceed π/2. Fall back to ideal; the burn
		// will under-deliver but at least won't panic the planner.
		return dvIdeal
	}
	return math.Asin(arg) / k
}

// refineFiniteDeparture replaces plan.Departure.DV with the value the
// finite-burn-aware iterator says will actually deliver the target
// apoapsis. v0.6.2 — for S-IVB-1's short LEO burn the impulsive
// guess is already within 0.1 % so the iterator converges in 1-2
// steps; for low-TWR loadouts (revived ICPS, future ion stages)
// where the burn arc is a non-trivial fraction of the parking
// orbit, the iterator catches errors of several percent.
//
// The departure burn in PlanIntraPrimaryHohmann always fires at
// parking-orbit periapsis (= the craft's current position for a
// circular orbit). Iteration uses a synthesized state at periapsis
// — same |R|, tangent V matching circular-orbit speed — since the
// burn dynamics are translation-invariant around the orbit.
//
// Iteration failure (max-iter or derivative collapse) leaves the
// impulsive Δv untouched; falling back to the impulsive plan is
// strictly better than failing the transfer.
func refineFiniteDeparture(plan *planner.TransferPlan, mu, rPark, mass, thrust, isp, rArrival float64) {
	if thrust <= 0 || mass <= 0 || isp <= 0 || plan.Departure.DV <= 0 {
		return
	}
	parkV := math.Sqrt(mu / rPark)
	parkState := physics.StateVector{
		R: orbital.Vec3{X: rPark},
		V: orbital.Vec3{Y: parkV},
		M: mass,
	}
	mode := spacecraft.BurnPrograde
	if plan.Departure.IsRetrograde {
		mode = spacecraft.BurnRetrograde
	}
	direction := func(r, v orbital.Vec3) orbital.Vec3 {
		return spacecraft.DirectionUnit(mode, r, v)
	}
	const tolMeters = 1000.0 // 1 km on apoapsis radius is well below display precision.
	const maxIter = 8
	refinedDv, _, err := planner.IterateForTarget(
		parkState, mu, thrust, isp, plan.Departure.DV,
		direction,
		planner.TargetApoapsis(rArrival),
		tolMeters, maxIter,
	)
	if err != nil {
		return
	}
	plan.Departure.DV = refinedDv
}

// estimateIntraPrimaryDepDv returns the Hohmann departure Δv for a
// circular-to-circular transfer from rDep to rArr around a primary
// with GM mu. Used by World.PlanTransfer to pre-size the burn
// duration before calling the planner, so the planner can pad its
// wait window enough to fit a centered finite burn.
func estimateIntraPrimaryDepDv(mu, rDep, rArr float64) float64 {
	if mu <= 0 || rDep <= 0 || rArr <= 0 || rDep == rArr {
		return 0
	}
	aT := (rDep + rArr) / 2
	vDepCirc := math.Sqrt(mu / rDep)
	vTransAtDep := math.Sqrt(mu * (2/rDep - 1/aT))
	dv := vTransAtDep - vDepCirc
	if dv < 0 {
		dv = -dv
	}
	return dv
}

// primaryFrameAngle returns body b's angular position around its
// parent (radians, atan2 of position-vector y, x in the parent's
// frame), evaluated at the world's current sim time. Used by the
// phase-corrected intra-primary Hohmann to compute target lead
// angles.
func primaryFrameAngle(w *World, b bodies.CelestialBody) float64 {
	M := w.Calculator.CalculateMeanAnomaly(b, w.Clock.SimTime)
	E := orbital.SolveKepler(M, b.Eccentricity)
	nu := orbital.TrueAnomaly(E, b.Eccentricity)
	el := orbital.ElementsFromBody(b)
	rRel := orbital.PositionAtTrueAnomaly(el, nu)
	return math.Atan2(rRel.Y, rRel.X)
}

// bodyEphemeris returns an EphemerisFn closure for a body: heliocentric
// (r, v) evaluated at an arbitrary Unix-epoch timestamp.
//
// Recurses through the v0.5.0 hierarchy (v0.5.5 fix): a moon's
// heliocentric state = parent's heliocentric state + moon's state in
// the parent's frame. Velocity uses the parent's μ, not the system
// primary's. Pre-v0.5.5 this returned moon's parent-relative position
// as if it were heliocentric, breaking PorkchopGrid + PlanTransferAt
// for moon targets — Lambert solved from Earth_helio to ~origin and
// quoted nonsense Δv (porkchop displayed ~380 m/s, plant produced
// ~25 km/s, both wrong).
func (w *World) bodyEphemeris(b bodies.CelestialBody) planner.EphemerisFn {
	return func(epoch float64) (orbital.Vec3, orbital.Vec3) {
		return w.bodyHelioStateAt(b, epoch)
	}
}

// bodyHelioStateAt is the recursive worker behind bodyEphemeris.
// Returns (heliocentric position, heliocentric velocity) of body b
// at the given Unix epoch by recursively summing parent-relative
// state up the hierarchy.
func (w *World) bodyHelioStateAt(b bodies.CelestialBody, epoch float64) (orbital.Vec3, orbital.Vec3) {
	if b.SemimajorAxis == 0 {
		// System primary anchored at origin with zero velocity.
		return orbital.Vec3{}, orbital.Vec3{}
	}
	t := time.Unix(int64(epoch), 0)
	M := w.Calculator.CalculateMeanAnomaly(b, t)
	E := orbital.SolveKepler(M, b.Eccentricity)
	nu := orbital.TrueAnomaly(E, b.Eccentricity)
	el := orbital.ElementsFromBody(b)
	rRel := orbital.PositionAtTrueAnomaly(el, nu)

	sys := w.System()
	parent := sys.ParentOf(b)
	if parent == nil {
		parent = sys.Primary()
	}
	mu := parent.GravitationalParameter()
	vRel := orbital.VelocityAtTrueAnomaly(el, nu, mu)

	if b.ParentID == "" {
		return rRel, vRel
	}
	rParent, vParent := w.bodyHelioStateAt(*parent, epoch)
	return rParent.Add(rRel), vParent.Add(vRel)
}

// transferNodeToManeuver converts a planner.TransferNode into a
// sim.ManeuverNode, adding a realistic burn duration based on the
// craft's thrust and current mass (Δt = Δv · m / F). If thrust is
// zero or inputs are degenerate the node stays impulsive (Duration=0),
// matching the legacy behavior.
//
// TriggerTime is set to the planner's intended firing moment — i.e.,
// the burn-CENTER per ManeuverNode's v0.5.14+ semantics. The
// integrator fires the burn at TriggerTime - Duration/2 (via
// ManeuverNode.BurnStart) so the burn is centered on TriggerTime; the
// HUD reads TriggerTime directly so the player sees the planned
// moment, not the implementation start.
//
// Callers that need BurnStart ≥ now (so the integrator doesn't have
// to fire a node retroactively) must pad the planner's OffsetTime by
// ≥ Duration/2 in advance — for the intra-primary path that's done
// via PlanIntraPrimaryHohmann's minLeadSeconds.
func transferNodeToManeuver(tn planner.TransferNode, now time.Time, mass, thrust float64) ManeuverNode {
	mode := spacecraft.BurnPrograde
	if tn.IsRetrograde {
		mode = spacecraft.BurnRetrograde
	}
	var duration time.Duration
	if thrust > 0 && mass > 0 && tn.DV > 0 {
		secs := tn.DV * mass / thrust
		duration = time.Duration(secs * float64(time.Second))
	}
	return ManeuverNode{
		TriggerTime: now.Add(tn.OffsetTime),
		Mode:        mode,
		DV:          tn.DV,
		Duration:    duration,
		PrimaryID:   tn.PrimaryID,
	}
}

var (
	errInvalidTransferTarget = transferError("invalid transfer target body")
	errNoCraftForTransfer    = transferError("no craft to plan transfer for")
	errNoRefineTarget        = transferError("no pending transfer to refine")
	errSamePrimaryUseHohmann = transferError("target shares craft's primary — use [H] auto-Hohmann instead of porkchop")
)

type transferError string

func (e transferError) Error() string { return string(e) }

// ClearNodes wipes every pending node.
func (w *World) ClearNodes() { w.Nodes = nil }

// executeDueNodes fires every node whose BurnStart has passed, applying
// the burn to the spacecraft in order. Called from Tick after sim-time
// advances. Re-entrant: if two nodes fall in the same tick, both fire.
//
// Impulsive nodes (Duration==0) apply their Δv inline at TriggerTime
// and are popped. Finite nodes start an ActiveBurn at BurnStart
// (= TriggerTime - Duration/2) and are popped; the burn runs across
// subsequent ticks via the RK4 branch in integrateSpacecraft until
// BurnEnd or DV exhausted. If a finite burn is already active when a
// new finite node fires, the new one replaces it (last-write-wins;
// the planner UI is responsible for not over-stacking). v0.5.14+
// semantics: TriggerTime is the burn center, BurnStart/BurnEnd are
// the actual on-engine moments.
func (w *World) executeDueNodes() {
	if w.Craft == nil {
		return
	}
	fired := 0
	// Nodes are sorted by TriggerTime, but we need to walk them by
	// BurnStart for finite nodes. Since BurnStart ≤ TriggerTime, walking
	// the (TriggerTime-sorted) slice may skip an early-firing finite
	// node if a later impulsive node has TriggerTime < this finite's
	// BurnStart. In practice nodes are spaced by minutes / days so the
	// ordering coincides; the planner's pad-window guarantee keeps us
	// safe. Worst case: one tick of latency on the misordered finite,
	// which is invisible at any user-visible warp.
	for _, n := range w.Nodes {
		// v0.6.0: unresolved event-relative nodes have TriggerTime = 0
		// (year 1 AD) which would fire immediately if we didn't guard.
		// They sort to the end of the slice so we can break safely.
		if !n.IsResolved() {
			break
		}
		if n.BurnStart().After(w.Clock.SimTime) {
			break
		}
		if n.Duration == 0 {
			w.Craft.ApplyImpulsive(n.Mode, n.DV)
		} else {
			w.ActiveBurn = &ActiveBurn{
				Mode:        n.Mode,
				DVRemaining: n.DV,
				EndTime:     n.BurnEnd(),
				PrimaryID:   n.PrimaryID,
			}
		}
		fired++
	}
	if fired > 0 {
		w.Nodes = w.Nodes[fired:]
	}
}

// NodeInertialPosition returns the inertial (system-primary-centered)
// position where the node will fire. Forward-integrates the craft state
// from now to the node's trigger time using SOI-aware Verlet sub-
// stepping, then adds the OWNING primary's inertial position — the
// frame may differ from the craft's current primary if the trajectory
// crossed an SOI boundary.
//
// Returns zero Vec3 if the craft is nil or the node is already past-due.
func (w *World) NodeInertialPosition(n ManeuverNode) orbital.Vec3 {
	if w.Craft == nil {
		return orbital.Vec3{}
	}
	dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	if dt <= 0 {
		return w.CraftInertial()
	}
	state, primary := w.propagateCraftWithPrimary(dt)
	return w.BodyPosition(primary).Add(state.R)
}

// PostBurnState returns the craft's primary-relative state vector
// immediately after the given node would fire, plus the ID of the
// primary that frame is relative to. Forward-integrates SOI-aware to
// the trigger time, then applies the Δv in the node's direction mode.
// The PrimaryID return lets callers (OrbitView post-burn preview)
// correctly translate state.R into inertial coords when the burn fires
// in a frame other than the craft's home primary — critical for the
// v0.3.1 auto-plant arrival node, which fires heliocentrically (or in
// the destination SOI) by construction.
func (w *World) PostBurnState(n ManeuverNode) (physics.StateVector, string) {
	if w.Craft == nil {
		return physics.StateVector{}, ""
	}
	dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	var state physics.StateVector
	var primaryID string
	if dt <= 0 {
		state = w.Craft.State
		primaryID = w.Craft.Primary.ID
	} else {
		var primary bodies.CelestialBody
		state, primary = w.propagateCraftWithPrimary(dt)
		primaryID = primary.ID
	}
	dir := spacecraft.DirectionUnit(n.Mode, state.R, state.V)
	if dir.Norm() == 0 || n.DV == 0 {
		return state, primaryID
	}
	state.V = state.V.Add(dir.Scale(n.DV))
	return state, primaryID
}

// PredictedLeg describes the trajectory leg following a single
// planted maneuver node — the orbit the craft would fly between
// this node firing and the next one (or for one orbital period if
// there's no next node). v0.6.1 uses this to render each leg in a
// distinct color so the player can read which orbit segment
// belongs to which planted burn.
type PredictedLeg struct {
	NodeIndex   int                   // index into World.Nodes
	State       physics.StateVector   // post-burn state in Primary's frame
	Primary     bodies.CelestialBody  // frame the state is expressed in
	HorizonSecs float64               // duration to predict for (until next node, or one period)
}

// PredictedLegs walks every resolved planted node and returns one
// PredictedLeg per node, with the post-burn state expressed in the
// node's intended frame (PrimaryID, falling back to the propagated
// frame when unspecified). Returns nil during an active burn — the
// live state is mutating and chained predictions would flail (see
// PredictedFinalOrbit's same guard).
func (w *World) PredictedLegs() []PredictedLeg {
	if w.Craft == nil || len(w.Nodes) == 0 || w.ActiveBurn != nil {
		return nil
	}
	state := w.Craft.State
	primary := w.Craft.Primary
	clock := w.Clock.SimTime
	systems := w.Systems
	legs := make([]PredictedLeg, 0, len(w.Nodes))
	for i, n := range w.Nodes {
		if !n.IsResolved() {
			continue
		}
		dt := n.TriggerTime.Sub(clock).Seconds()
		if dt > 0 {
			state, primary = w.propagateStateWithPrimary(state, primary, dt)
			clock = n.TriggerTime
		}
		// Frame rebase if the node was planted in a specific
		// destination frame (matches PredictedFinalOrbit's behavior).
		if target, ok := bodies.LookupByID(systems, n.PrimaryID); ok && target.ID != primary.ID {
			oldInertial := w.BodyPosition(primary)
			newInertial := w.BodyPosition(target)
			vOld := w.bodyInertialVelocity(primary)
			vNew := w.bodyInertialVelocity(target)
			state = physics.Rebase(state, oldInertial, newInertial, vOld.Sub(vNew))
			primary = target
		}
		dir := spacecraft.DirectionUnit(n.Mode, state.R, state.V)
		if dir.Norm() != 0 && n.DV != 0 {
			state.V = state.V.Add(dir.Scale(n.DV))
		}
		// Horizon: until next planted node, else one orbital period.
		var horizon float64
		if i+1 < len(w.Nodes) && w.Nodes[i+1].IsResolved() {
			horizon = w.Nodes[i+1].TriggerTime.Sub(clock).Seconds()
		}
		if horizon <= 0 {
			mu := primary.GravitationalParameter()
			horizon = orbitalPeriod(state, mu)
			if horizon <= 0 || math.IsNaN(horizon) || math.IsInf(horizon, 0) {
				// Hyperbolic / degenerate — fall back to a short fixed window.
				horizon = 3600
			}
		}
		legs = append(legs, PredictedLeg{
			NodeIndex:   i,
			State:       state,
			Primary:     primary,
			HorizonSecs: horizon,
		})
	}
	return legs
}

// PreviewBurnState returns the craft state immediately after a
// hypothetical burn with the given (mode, dv, event) parameters
// would fire — without mutating world state. Used by the maneuver-
// planner screen so its shadow trajectory + PROJECTED ORBIT readout
// reflect where the burn would *actually* fire, not where the craft
// is sitting right now.
//
// For event != Absolute, the helper computes the time-of-flight to
// the event using the same orbital helpers as the lazy-freeze
// resolver, then propagates the craft forward via the SOI-aware
// integrator before applying Δv. Returns ok=false when the event is
// unreachable from the current orbit (hyperbolic, equatorial AN/DN,
// etc.) so the caller can fall back to a current-position preview.
//
// Absolute event: dt is taken as zero — the absolute-time preview is
// always "burn applied at current state," which matches the
// planner's pre-v0.6 semantics. Real Absolute nodes fire at
// TriggerTime + Duration/2 in flight; the planner doesn't yet know
// which TriggerTime the user will choose, so previewing at "now" is
// the least-surprising default.
func (w *World) PreviewBurnState(mode spacecraft.BurnMode, dv float64, event TriggerEvent) (physics.StateVector, bodies.CelestialBody, bool) {
	if w.Craft == nil {
		return physics.StateVector{}, bodies.CelestialBody{}, false
	}
	state := w.Craft.State
	primary := w.Craft.Primary

	if event != TriggerAbsolute {
		mu := primary.GravitationalParameter()
		ostate := orbital.Vec3State{R: state.R, V: state.V}
		var dt float64
		switch event {
		case TriggerNextPeri:
			dt = orbital.TimeToPeriapsis(ostate, mu)
		case TriggerNextApo:
			dt = orbital.TimeToApoapsis(ostate, mu)
		case TriggerNextAN:
			dt = orbital.TimeToNodeCrossing(ostate, mu, true)
		case TriggerNextDN:
			dt = orbital.TimeToNodeCrossing(ostate, mu, false)
		}
		if dt < 0 {
			return physics.StateVector{}, bodies.CelestialBody{}, false
		}
		if dt > 0 {
			state, primary = w.propagateStateWithPrimary(state, primary, dt)
		}
	}

	dir := spacecraft.DirectionUnit(mode, state.R, state.V)
	if dir.Norm() != 0 && dv != 0 {
		state.V = state.V.Add(dir.Scale(dv))
	}
	return state, primary, true
}

// PredictedFinalOrbit walks every planted node in trigger-time order
// and returns the craft state immediately after the last node fires,
// along with the primary body whose frame the state is relative to.
// ok=false when there are no planted nodes (or no craft) — caller
// should fall back to the live orbit.
//
// Chaining semantics: start from the live craft state at clock time;
// for each node, propagate forward to the node's TriggerTime, apply
// the burn (impulsive Δv in the node's mode direction — finite-burn
// deformation is approximated as instantaneous since this is a HUD
// readout, not a flight integrator), then advance the running clock.
// Unresolved event-relative nodes are skipped — they'll resolve on a
// future tick and appear in subsequent renders.
//
// SOI transitions during propagation are handled by the underlying
// integrator; bodies are snapshotted at the *current* clock time, so
// readouts on multi-day chains lose accuracy as planets move. That's
// fine for a glance-at-the-HUD reading; the planner's actual
// trajectory preview already has its own caveats around long
// horizons.
func (w *World) PredictedFinalOrbit() (physics.StateVector, bodies.CelestialBody, bool) {
	if w.Craft == nil || len(w.Nodes) == 0 {
		return physics.StateVector{}, bodies.CelestialBody{}, false
	}
	// v0.6.1: during an active finite burn the live craft state is
	// being mutated every integrator step. Chaining predictions
	// through that state produces flailing numbers each render and
	// a preview ellipse that rotates as fast as the engine fires.
	// Suppress the projection until the burn completes — the live
	// VESSEL block already shows the orbit changing in real time.
	if w.ActiveBurn != nil {
		return physics.StateVector{}, bodies.CelestialBody{}, false
	}
	state := w.Craft.State
	primary := w.Craft.Primary
	clock := w.Clock.SimTime
	any := false
	systems := w.Systems
	for _, n := range w.Nodes {
		if !n.IsResolved() {
			continue
		}
		dt := n.TriggerTime.Sub(clock).Seconds()
		if dt > 0 {
			state, primary = w.propagateStateWithPrimary(state, primary, dt)
			clock = n.TriggerTime
		}
		// v0.6.1: a node planted in a non-default frame (the
		// arrival burn of a Hohmann transfer is planted with
		// PrimaryID = destination body) wants its Δv applied in
		// THAT frame, not in whatever frame the chained
		// propagation landed in. Without this rebase, an Earth →
		// Mars Hohmann arrival fires its capture burn while the
		// state is still heliocentric (the integrator hasn't yet
		// crossed Mars's SOI at the rendezvous moment), and the
		// post-burn orbit comes out as a heliocentric Sol orbit.
		if target, ok := bodies.LookupByID(systems, n.PrimaryID); ok && target.ID != primary.ID {
			oldInertial := w.BodyPosition(primary)
			newInertial := w.BodyPosition(target)
			vOld := w.bodyInertialVelocity(primary)
			vNew := w.bodyInertialVelocity(target)
			state = physics.Rebase(state, oldInertial, newInertial, vOld.Sub(vNew))
			primary = target
		}
		dir := spacecraft.DirectionUnit(n.Mode, state.R, state.V)
		if dir.Norm() != 0 && n.DV != 0 {
			state.V = state.V.Add(dir.Scale(n.DV))
		}
		any = true
	}
	if !any {
		return physics.StateVector{}, bodies.CelestialBody{}, false
	}
	return state, primary, true
}

// propagateCraft forward-integrates the craft's primary-relative state
// dt seconds into the future without mutating live state. Returns only
// the state — used by callers that don't care which primary owns the
// frame (legacy v0.2 paths, tests). For v0.3.0+ callers that need to
// translate the result into inertial coords across SOI crossings, use
// propagateCraftWithPrimary instead.
func (w *World) propagateCraft(dt float64) physics.StateVector {
	state, _ := w.propagateCraftWithPrimary(dt)
	return state
}

// propagateCraftWithPrimary is the SOI-aware integrator: when a sub-
// step crosses an SOI boundary the state is rebased and μ switches for
// subsequent steps. Returns the final state plus the body that owns
// the frame at dt — callers add BodyPosition(primary) to convert state.R
// into inertial coords.
func (w *World) propagateCraftWithPrimary(dt float64) (physics.StateVector, bodies.CelestialBody) {
	return w.propagateStateWithPrimary(w.Craft.State, w.Craft.Primary, dt)
}

// propagateStateWithPrimary is the same SOI-aware integrator but
// parameterised on the starting state and primary. Used by
// PredictedFinalOrbit (v0.6.1) to chain through multiple planted
// nodes without mutating live craft state. Body-position snapshots
// are taken at the live Clock.SimTime — accurate over short horizons,
// loses precision for multi-day chains where bodies have moved
// appreciably; that's acceptable for a HUD readout.
func (w *World) propagateStateWithPrimary(startState physics.StateVector, startPrimary bodies.CelestialBody, dt float64) (physics.StateVector, bodies.CelestialBody) {
	current := startPrimary
	muNow := current.GravitationalParameter()
	state := startState

	sys := w.System()
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}

	period := orbitalPeriod(state, muNow)
	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	nSteps := int(math.Ceil(dt / maxStep))
	if nSteps < 1 {
		nSteps = 1
	}
	if nSteps > 1024 {
		nSteps = 1024
	}
	step := dt / float64(nSteps)
	for i := 0; i < nSteps; i++ {
		state = physics.StepVerlet(state, muNow, step)

		inertial := positions[current.ID].Add(state.R)
		cand := physics.FindPrimary(sys, inertial, positions)
		if cand.Body.ID != current.ID {
			vOld := w.bodyInertialVelocity(current)
			vNew := w.bodyInertialVelocity(cand.Body)
			state = physics.Rebase(state, positions[current.ID], cand.Inertial, vOld.Sub(vNew))
			current = cand.Body
			muNow = current.GravitationalParameter()
		}
	}
	return state, current
}
