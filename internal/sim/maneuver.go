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

// Type aliases — the underlying types now live in
// internal/spacecraft so each Spacecraft can own its own []Nodes,
// ActiveBurn, ManualBurn without an import cycle. Aliases keep
// existing `sim.ManeuverNode` / `sim.TriggerNextPeri` references
// working unchanged. v0.8.1+.
type (
	TriggerEvent = spacecraft.TriggerEvent
	ManeuverNode = spacecraft.ManeuverNode
	ActiveBurn   = spacecraft.ActiveBurn
	ManualBurn   = spacecraft.ManualBurn
)

const (
	TriggerAbsolute = spacecraft.TriggerAbsolute
	TriggerNextPeri = spacecraft.TriggerNextPeri
	TriggerNextApo  = spacecraft.TriggerNextApo
	TriggerNextAN   = spacecraft.TriggerNextAN
	TriggerNextDN   = spacecraft.TriggerNextDN
)

// AllTriggerEvents re-exports the spacecraft-package canonical UI
// cycle order so existing `sim.AllTriggerEvents` references keep
// compiling.
var AllTriggerEvents = spacecraft.AllTriggerEvents

// StartManualBurn opens the active craft's engine in its current
// AttitudeMode at its current Throttle. No-op if a planted
// ActiveBurn is already in flight on the active craft (planted
// burns own the engine until they complete), fuel is empty, or
// engine is in RCS mode. Idempotent.
//
// v0.8.1+: per-active-craft. Each craft owns its own ManualBurn —
// switching active craft mid-flight does not move the in-flight
// engine to the new craft.
func (w *World) StartManualBurn() {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	if c.ActiveBurn != nil || c.ManualBurn != nil {
		return
	}
	if c.Fuel <= 0 || c.Thrust <= 0 {
		return
	}
	if c.EngineMode != spacecraft.EngineMain {
		return
	}
	if c.EffectiveThrottle() <= 0 {
		return
	}
	c.ManualBurn = &ManualBurn{StartTime: w.Clock.SimTime}
}

// StopManualBurn cuts the active craft's manual burn. No-op when
// no manual burn is in flight on the active craft.
func (w *World) StopManualBurn() {
	if c := w.ActiveCraft(); c != nil {
		c.ManualBurn = nil
	}
}

// ToggleManualBurn engages or disengages the active craft's manual
// burn. v0.7.3.2+ explicit-engage gate.
func (w *World) ToggleManualBurn() {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	if c.ManualBurn != nil {
		w.StopManualBurn()
		return
	}
	w.StartManualBurn()
}

// SetThrottle clamps the requested throttle to [0, 1] and applies
// it to the active craft. Setting throttle to 0 also stops the
// active craft's in-flight manual burn so the "x = cut" muscle
// memory works in one keypress; planted burns keep running.
func (w *World) SetThrottle(t float64) {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	c.Throttle = t
	if t == 0 {
		w.StopManualBurn()
	}
}

// AdjustThrottle steps the active craft's Throttle by delta, clamped
// to [0, 1].
func (w *World) AdjustThrottle(delta float64) {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	w.SetThrottle(c.Throttle + delta)
}

// SetAttitudeMode updates the active craft's held attitude. If a
// manual burn is already in flight on that craft, the engine
// direction takes effect on the next tick. v0.8.1+: per-active-craft.
func (w *World) SetAttitudeMode(mode spacecraft.BurnMode) {
	if c := w.ActiveCraft(); c != nil {
		c.AttitudeMode = mode
	}
}

// PlanNode inserts a node into the active craft's Nodes slice,
// keeping the slice sorted by TriggerTime. Past-dated nodes are
// allowed — they fire on the next Tick. v0.8.1+: per-active-craft;
// the planted burn fires on the craft it was planted for, regardless
// of which craft the player is flying when it triggers.
func (w *World) PlanNode(n ManeuverNode) {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	c.Nodes = append(c.Nodes, n)
	sortNodes(c.Nodes)
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
// resolveEventNodes walks every craft in the slate and resolves any
// of its event-relative nodes against that craft's own orbit. Each
// craft's nodes are independent — a periapsis-relative burn planted
// on craft A resolves against craft A's orbit, not the active
// craft's. v0.8.1+.
func (w *World) resolveEventNodes() {
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		w.resolveEventNodesFor(c)
	}
}

func (w *World) resolveEventNodesFor(c *spacecraft.Spacecraft) {
	mu := c.Primary.GravitationalParameter()
	if mu == 0 {
		return
	}
	// v0.8.6+: resolve event timings in the primary's reference frame
	// (body-equatorial for non-Sun primaries) so AN/DN mean "crossing
	// of the body's equator" rather than "crossing of the world XY
	// plane". TimeToPeriapsis / TimeToApoapsis are frame-invariant
	// scalars but we pass the rotated state for consistency.
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	state := orbital.Vec3State{
		R: frame.FromWorld(c.State.R),
		V: frame.FromWorld(c.State.V),
	}
	resolvedAny := false
	for i := range c.Nodes {
		n := &c.Nodes[i]
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
		n.TriggerTime = w.Clock.SimTime.Add(time.Duration(dt * float64(time.Second)))
		if n.PrimaryID == "" {
			n.PrimaryID = c.Primary.ID
		}
		resolvedAny = true
	}
	if resolvedAny {
		sortNodes(c.Nodes)
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
	if w.ActiveCraft() == nil {
		return nil, errNoCraftForTransfer
	}
	target := sys.Bodies[targetIdx]
	if target.SemimajorAxis == 0 {
		return nil, errInvalidTransferTarget
	}

	rPark := w.ActiveCraft().State.R.Norm()
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
	if target.ParentID == w.ActiveCraft().Primary.ID {
		muShared := w.ActiveCraft().Primary.GravitationalParameter()
		rArrival := target.SemimajorAxisMeters()
		craftAngle := math.Atan2(w.ActiveCraft().State.R.Y, w.ActiveCraft().State.R.X)
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
		mass := w.ActiveCraft().TotalMass()
		thrust := w.ActiveCraft().Thrust
		dvDepEstimate := estimateIntraPrimaryDepDv(muShared, rPark, rArrival)
		// minLead = half the centred-finite-burn duration so the planner's
		// OffsetTime sits ≥ Duration/2 ahead of now (BurnStart can't be
		// retroactive). v0.6.5: rocket-equation form via BurnTimeForDV
		// instead of constant-mass `Δv·m/F` so this matches the duration
		// transferNodeToManeuver will actually plant.
		minLead := w.ActiveCraft().BurnTimeForDV(dvDepEstimate).Seconds() / 2
		plan, err := planner.PlanIntraPrimaryHohmann(
			muShared, rPark, rArrival,
			craftAngle, targetAngle, minLead,
			w.ActiveCraft().Primary.ID,
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
		refineFiniteDeparture(&plan, muShared, rPark, mass, thrust, w.ActiveCraft().Isp, rArrival)
		now := w.Clock.SimTime
		w.PlanNode(transferNodeToManeuver(plan.Departure, now, w.ActiveCraft()))
		w.PlanNode(transferNodeToManeuver(plan.Arrival, now, w.ActiveCraft()))
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
	if w.ActiveCraft().Primary.ParentID != "" && target.ID == w.ActiveCraft().Primary.ParentID {
		moon := w.ActiveCraft().Primary
		moonParent := sys.ParentOf(moon)
		if moonParent == nil {
			return nil, errInvalidTransferTarget
		}
		muMoon := moon.GravitationalParameter()
		rSOI := physics.SOIRadius(moon, *moonParent)
		if rSOI == 0 || rSOI <= rPark {
			return nil, errInvalidTransferTarget
		}
		mass := w.ActiveCraft().TotalMass()
		thrust := w.ActiveCraft().Thrust
		// Pre-size the centered-finite-burn lead pad from the impulsive
		// estimate, mirroring the intra-primary branch above.
		aT := (rPark + rSOI) / 2
		vCirc := math.Sqrt(muMoon / rPark)
		vTransAtPeri := math.Sqrt(muMoon * (2/rPark - 1/aT))
		dvEstimate := vTransAtPeri - vCirc
		minLead := w.ActiveCraft().BurnTimeForDV(dvEstimate).Seconds() / 2
		plan, err := planner.PlanMoonEscape(muMoon, rPark, rSOI, minLead, moon.ID, target.ID)
		if err != nil {
			return nil, err
		}
		// Reuse v0.6.2's iterator: target the SOI radius as apolune so
		// finite-burn integration delivers the bound transfer ellipse
		// the impulsive math designed.
		refineFiniteDeparture(&plan, muMoon, rPark, mass, thrust, w.ActiveCraft().Isp, rSOI)
		now := w.Clock.SimTime
		w.PlanNode(transferNodeToManeuver(plan.Departure, now, w.ActiveCraft()))
		w.PlanNode(transferNodeToManeuver(plan.Arrival, now, w.ActiveCraft()))
		return &plan, nil
	}

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()
	rDeparture := w.CraftInertial().Norm()
	rArrival := target.SemimajorAxisMeters()

	muDeparture := w.ActiveCraft().Primary.GravitationalParameter()

	plan, err := planner.PlanHohmannTransfer(
		muSun, rDeparture, rArrival,
		muDeparture, rPark, w.ActiveCraft().Primary.ID,
		muDestination, rCapture, target.ID,
	)
	if err != nil {
		return nil, err
	}

	now := w.Clock.SimTime
	w.PlanNode(transferNodeToManeuver(plan.Departure, now, w.ActiveCraft()))
	w.PlanNode(transferNodeToManeuver(plan.Arrival, now, w.ActiveCraft()))
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
	if w.ActiveCraft() == nil {
		return 0, 0, errNoCraftForTransfer
	}
	// Find the latest pending "arrival" node — one whose PrimaryID
	// identifies a non-home body. PlanTransfer / PlanTransferAt plants
	// arrival with PrimaryID = target.ID.
	arrIdx := -1
	for i := len(w.ActiveCraft().Nodes) - 1; i >= 0; i-- {
		n := w.ActiveCraft().Nodes[i]
		if n.PrimaryID != "" && n.PrimaryID != w.ActiveCraft().Primary.ID {
			arrIdx = i
			break
		}
	}
	if arrIdx < 0 {
		return 0, 0, errNoRefineTarget
	}
	arrNode := w.ActiveCraft().Nodes[arrIdx]
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
	vCraftHelio := w.bodyInertialVelocity(w.ActiveCraft().Primary).Add(w.ActiveCraft().State.V)
	rCraftHelio := w.BodyPosition(w.ActiveCraft().Primary).Add(w.ActiveCraft().State.R)

	// Target's heliocentric state at arrival time.
	arrEph := w.bodyEphemeris(target)
	rArr, vArrBody := arrEph(float64(arrNode.TriggerTime.Unix()))

	// Prograde branch — matches PlanLambertTransfer's default
	// (callers haven't requested a retrograde refinement yet).
	v1, v2, err := planner.LambertSolve(rCraftHelio, rArr, tof, muSun, false)
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

	// Plant the correction burn at now (tiny offset to avoid firing the
	// same tick it lands if executeDueNodes has already run). v0.6.5:
	// duration via the rocket-equation form so it matches the
	// transferNodeToManeuver path.
	if correctionDv > 0 {
		w.PlanNode(ManeuverNode{
			TriggerTime: now.Add(time.Second),
			Mode:        correctionMode,
			DV:          correctionDv,
			Duration:    w.ActiveCraft().BurnTimeForDV(correctionDv),
			PrimaryID:   w.ActiveCraft().Primary.ID,
		})
	}

	// Rebuild the arrival node in place with refined Δv.
	newArrival := ManeuverNode{
		TriggerTime: arrNode.TriggerTime,
		Mode:        arrNode.Mode,
		DV:          arrivalDv,
		Duration:    w.ActiveCraft().BurnTimeForDV(arrivalDv),
		PrimaryID:   arrNode.PrimaryID,
	}
	// Find arrNode again by index after PlanNode sorted the slice.
	for i, n := range w.ActiveCraft().Nodes {
		if n.TriggerTime.Equal(arrNode.TriggerTime) && n.PrimaryID == arrNode.PrimaryID {
			w.ActiveCraft().Nodes[i] = newArrival
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
	if w.ActiveCraft() == nil {
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
	if target.ParentID == w.ActiveCraft().Primary.ID {
		return nil, errSamePrimaryUseHohmann
	}

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()
	rPark := w.ActiveCraft().State.R.Norm()
	muDep := w.ActiveCraft().Primary.GravitationalParameter()
	rCapture := target.RadiusMeters() + 200e3
	muArr := target.GravitationalParameter()

	depEph := w.bodyEphemeris(w.ActiveCraft().Primary)
	arrEph := w.bodyEphemeris(target)
	const secondsPerDay = 86400.0
	epoch0 := float64(w.Clock.SimTime.Unix())
	tDep := epoch0 + depDay*secondsPerDay
	tArr := tDep + tofDay*secondsPerDay
	rDep, vDep := depEph(tDep)
	rArr, vArr := arrEph(tArr)

	depOffset := time.Duration(depDay * secondsPerDay * float64(time.Second))
	// Prograde transfer — the porkchop UI scores prograde transfers
	// today; v0.8+ multi-rev work will surface retrograde as a UI
	// toggle and start passing true.
	plan, err := planner.PlanLambertTransfer(
		muSun,
		rDep, vDep,
		rArr, vArr,
		tofDay*secondsPerDay,
		muDep, rPark, w.ActiveCraft().Primary.ID,
		muArr, rCapture, target.ID,
		depOffset,
		false,
	)
	if err != nil {
		return nil, err
	}

	now := w.Clock.SimTime
	w.PlanNode(transferNodeToManeuver(plan.Departure, now, w.ActiveCraft()))
	w.PlanNode(transferNodeToManeuver(plan.Arrival, now, w.ActiveCraft()))
	return &plan, nil
}

// FrameTransition describes an upcoming change of orbital frame —
// the craft (or a planted post-burn trajectory) crossing an SOI
// boundary into a new primary's frame. Surfaced by the HUD via
// World.NextFrameTransition so the player can anticipate where their
// integrator will hand off control.
//
// Today's heuristic is "the first planted node whose PrimaryID differs
// from the craft's current primary." That catches the v0.6.3 moon →
// parent escape's zero-Δv arrival marker (planted in parent frame
// for exactly this reason) and Hohmann arrival burns (planted in the
// destination's frame). True trajectory-walked SOI crossings (e.g.
// a planned Mars flyby with no arrival burn) stay out of scope until
// the predictor learns to surface SOI events. v0.7.6+.
type FrameTransition struct {
	NodeIndex int    // index into World.Nodes
	From, To  string // body IDs
	When      time.Time
}

// NextFrameTransition returns the next upcoming frame transition
// implied by the planted maneuver-node chain, walking nodes in
// trigger-time order. Each node carries the primary's ID it was
// planted in; the first node whose PrimaryID differs from the
// running frame ID is the transition. Returns ok=false when no
// planted node changes frame, or when the craft is missing /
// the chain is empty / no resolved nodes exist.
//
// The walk is intentionally cheap — no integration, no SOI math,
// just trusting the planner's PrimaryID labels. PlanMoonEscape and
// PlanHohmannTransfer both label arrival nodes in their target's
// frame, which is exactly what this surfaces. v0.7.6+.
func (w *World) NextFrameTransition() (FrameTransition, bool) {
	if w.ActiveCraft() == nil || len(w.ActiveCraft().Nodes) == 0 {
		return FrameTransition{}, false
	}
	current := w.ActiveCraft().Primary.ID
	for i, n := range w.ActiveCraft().Nodes {
		if !n.IsResolved() {
			continue
		}
		if n.PrimaryID == "" {
			continue
		}
		if n.PrimaryID != current {
			return FrameTransition{
				NodeIndex: i,
				From:      current,
				To:        n.PrimaryID,
				When:      n.TriggerTime,
			}, true
		}
		current = n.PrimaryID
	}
	return FrameTransition{}, false
}

// PlanInclinationChange plants a single normal-burn maneuver node
// that rotates the craft's orbital plane to targetIncl (radians, in
// [0, π]). The burn fires at the next ascending or descending node,
// whichever comes sooner; the planner picks the BurnNormal+ /
// BurnNormal- mode that drives inclination toward the target.
//
// Returns the planner's InclinationPlan (Δv + chosen node) for HUD
// flashing; surfaces the planner's error untouched if the source
// orbit is equatorial / hyperbolic / already-at-target.
//
// v0.7.4+. Composes with v0.6.0's burn-at-next scheduler — the planted
// node uses an absolute TriggerTime (event resolver isn't needed
// since the planner already computed the future event time).
func (w *World) PlanInclinationChange(targetIncl float64) (*planner.InclinationPlan, error) {
	if w.ActiveCraft() == nil {
		return nil, errNoCraftForTransfer
	}
	mu := w.ActiveCraft().Primary.GravitationalParameter()
	// v0.8.6+: targetIncl is interpreted in the primary's reference
	// frame (body-equatorial for non-Sun primaries; ecliptic for the
	// Sun). Rotate the state into that frame before calling the inner
	// planner — Δv, time-of-flight and NormalSign (which refers to
	// ±h, computed from the live state at burn time) are all
	// frame-invariant, so the resulting InclinationPlan flies through
	// unchanged.
	primary := w.ActiveCraft().Primary
	frame := orbital.ReferenceFrameForPrimary(primary)
	state := orbital.Vec3State{
		R: frame.FromWorld(w.ActiveCraft().State.R),
		V: frame.FromWorld(w.ActiveCraft().State.V),
	}
	plan, err := planner.PlanInclinationChange(state, mu, targetIncl, primary.ID)
	if err != nil {
		return nil, err
	}
	mode := spacecraft.BurnNormalPlus
	if plan.NormalSign < 0 {
		mode = spacecraft.BurnNormalMinus
	}
	now := w.Clock.SimTime
	w.PlanNode(ManeuverNode{
		TriggerTime: now.Add(plan.OffsetTime),
		Mode:        mode,
		DV:          plan.DV,
		Duration:    w.ActiveCraft().BurnTimeForDV(plan.DV),
		PrimaryID:   plan.PrimaryID,
	})
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
	if w.ActiveCraft() == nil {
		return nil, errNoCraftForTransfer
	}
	target := sys.Bodies[targetIdx]
	if target.SemimajorAxis == 0 {
		return nil, errInvalidTransferTarget
	}
	if target.ParentID == w.ActiveCraft().Primary.ID {
		return nil, errSamePrimaryUseHohmann
	}

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()
	rPark := w.ActiveCraft().State.R.Norm()
	muDep := w.ActiveCraft().Primary.GravitationalParameter()
	rCapture := target.RadiusMeters() + 200e3
	muArr := target.GravitationalParameter()

	// Build ephemerides that evaluate heliocentric r, v at arbitrary
	// epochs. Reuses the existing Kepler/calculator machinery.
	dep := w.bodyEphemeris(w.ActiveCraft().Primary)
	arr := w.bodyEphemeris(target)
	epoch0 := float64(w.Clock.SimTime.Unix())

	grid := planner.PorkchopGrid(
		muSun, dep, arr, epoch0,
		depDays, tofDays,
		muDep, rPark,
		muArr, rCapture,
		false, // prograde — v0.8+ multi-rev porkchop will toggle this.
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
// sim.ManeuverNode, deriving a realistic burn duration from the
// craft's current mass + thrust + Isp via the rocket equation
// (spacecraft.BurnTimeForDV). Zero-thrust craft yield Duration = 0,
// preserving the legacy impulsive path.
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
//
// v0.6.5: switched from constant-mass `Δv·m/F` to the rocket-equation
// form. Single source of truth — same call the maneuver planner UX
// uses — so player-input and auto-plant burns size identically.
func transferNodeToManeuver(tn planner.TransferNode, now time.Time, craft *spacecraft.Spacecraft) ManeuverNode {
	mode := spacecraft.BurnPrograde
	if tn.IsRetrograde {
		mode = spacecraft.BurnRetrograde
	}
	return ManeuverNode{
		TriggerTime: now.Add(tn.OffsetTime),
		Mode:        mode,
		DV:          tn.DV,
		Duration:    craft.BurnTimeForDV(tn.DV),
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

// ClearNodes wipes every pending node from the active craft. v0.8.1+:
// per-active-craft (was global pre-v0.8.1).
func (w *World) ClearNodes() {
	if c := w.ActiveCraft(); c != nil {
		c.Nodes = nil
	}
}

// DeleteNode removes the node at idx from the active craft's plan.
// Out-of-range idx is a no-op (callers may pass -1 to indicate
// "no edit target"). v0.8.6+ — paired with the maneuver form's
// per-node delete action that replaces the v0.8.5-and-earlier
// "wipe everything via N" keybinding.
func (w *World) DeleteNode(idx int) {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	if idx < 0 || idx >= len(c.Nodes) {
		return
	}
	c.Nodes = append(c.Nodes[:idx], c.Nodes[idx+1:]...)
}

// executeDueNodes fires every craft's due nodes onto themselves.
// Called from Tick after sim-time advances. Each craft's nodes are
// independent — a planted burn fires on the craft it was planted
// for, regardless of which craft the player is currently flying.
// v0.8.1+.
func (w *World) executeDueNodes() {
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		w.executeDueNodesFor(c)
	}
}

// executeDueNodesFor fires the given craft's due nodes onto itself.
// Impulsive nodes (Duration==0) apply their Δv inline; finite nodes
// start the craft's ActiveBurn. Both popped from the craft's own
// Nodes slice. v0.8.1+.
func (w *World) executeDueNodesFor(c *spacecraft.Spacecraft) {
	fired := 0
	for _, n := range c.Nodes {
		if !n.IsResolved() {
			break
		}
		if n.BurnStart().After(w.Clock.SimTime) {
			break
		}
		if n.Duration == 0 {
			c.ApplyImpulsive(n.Mode, n.DV)
		} else {
			c.ActiveBurn = &ActiveBurn{
				Mode:        n.Mode,
				DVRemaining: n.DV,
				EndTime:     n.BurnEnd(),
				PrimaryID:   n.PrimaryID,
				Throttle:    n.EffectiveThrottle(),
			}
		}
		fired++
	}
	if fired > 0 {
		c.Nodes = c.Nodes[fired:]
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
	if w.ActiveCraft() == nil {
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
	if w.ActiveCraft() == nil {
		return physics.StateVector{}, ""
	}
	dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	var state physics.StateVector
	var primaryID string
	if dt <= 0 {
		state = w.ActiveCraft().State
		primaryID = w.ActiveCraft().Primary.ID
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

// CapturePreview describes the post-arrival orbit at the last
// inter-primary node in the active craft's planted chain. v0.8.2.x:
// surfaces the capture-orbit inclination prominently so the player
// catches retrograde-around-target gotchas (a prograde Hohmann to
// Luna naturally arrives at ~110° lunar inclination, etc.) before
// firing.
//
// Two modes:
//
//   - Exact: the chained predictor's rebase produced a sane state.R
//     (well outside the target body's radius) and orbit elements
//     reflect the post-burn capture orbit directly. ApoapsisM /
//     PeriapsisM / Inclination / Hyperbolic are populated.
//   - Approximate: state.R came out ~0 (perfect-aim Hohmann, the
//     chained propagator's static body positions miss the SOI
//     entry geometry). Instead, the preview reports the relative
//     approach speed (|v_∞|) and a qualitative prograde / retrograde
//     direction inferred from v_∞ vs target's parent-frame velocity.
//     ApoapsisM / PeriapsisM / Inclination / Hyperbolic are zero;
//     ApproachSpeed and RetrogradeCapture are populated.
//
// The Approximate flag distinguishes the two — HUD branches on it.
type CapturePreview struct {
	Primary      bodies.CelestialBody
	NodeIndex    int       // index of the arrival node in c.Nodes
	When         time.Time // sim-time at which the arrival fires
	Approximate  bool      // true when only ApproachSpeed / RetrogradeCapture are populated
	Inclination  float64   // radians, [0, π] — exact mode only
	ApoapsisM    float64   // m, capture orbit apoapsis — exact mode only
	PeriapsisM   float64   // m — exact mode only
	Hyperbolic   bool      // capture failed — exact mode only
	Eccentricity float64   // exact mode only
	// Approximate-mode fields:
	ApproachSpeed     float64 // |v_∞| relative to target (m/s)
	RetrogradeCapture bool    // craft will orbit target in retrograde sense
}

// ArrivalCapturePreview returns a CapturePreview for the last node
// in the active craft's plan that lands in a different primary's
// frame, or ok=false when no such node is queued. v0.8.2.x.
func (w *World) ArrivalCapturePreview() (CapturePreview, bool) {
	c := w.ActiveCraft()
	if c == nil || len(c.Nodes) == 0 || c.ActiveBurn != nil {
		return CapturePreview{}, false
	}
	// Walk the planted chain, recording the post-burn state at any
	// frame-changing node. The LAST such node is the capture point.
	state := c.State
	primary := c.Primary
	clock := w.Clock.SimTime
	systems := w.Systems
	homeID := c.Primary.ID

	var (
		captureState   physics.StateVector
		capturePrimary bodies.CelestialBody
		captureTime    time.Time
		captureIdx     = -1
	)
	for i, n := range c.Nodes {
		if !n.IsResolved() {
			continue
		}
		dt := n.TriggerTime.Sub(clock).Seconds()
		if dt > 0 {
			state, primary = w.propagateStateWithPrimary(state, primary, clock, dt)
			clock = n.TriggerTime
		}
		if target, ok := bodies.LookupByID(systems, n.PrimaryID); ok && target.ID != primary.ID {
			oldInertial := w.BodyPositionAt(primary, clock)
			newInertial := w.BodyPositionAt(target, clock)
			vOld := w.bodyInertialVelocityAt(primary, clock)
			vNew := w.bodyInertialVelocityAt(target, clock)
			state = physics.Rebase(state, oldInertial, newInertial, vOld.Sub(vNew))
			primary = target
		}
		dir := spacecraft.DirectionUnit(n.Mode, state.R, state.V)
		if dir.Norm() != 0 && n.DV != 0 {
			state.V = state.V.Add(dir.Scale(n.DV))
		}
		// A node "captures" when it leaves the chain in a frame
		// different from the craft's home primary.
		if primary.ID != homeID {
			captureState = state
			capturePrimary = primary
			captureTime = clock
			captureIdx = i
		}
	}
	if captureIdx < 0 {
		return CapturePreview{}, false
	}
	// Detect degenerate "perfect-aim Hohmann" rebase: the chained
	// propagator's static body positions don't actually enter the
	// target's SOI during prediction, so when the rebase fires at
	// the arrival node, state.R lands ~0 (craft exactly at the
	// target's center). Orbit elements collapse and OrbitReadout
	// reports Hyperbolic, which would mis-message the preview.
	//
	// Threshold is 5× target radius — generous, captures the
	// "we're inside the body" case while still letting genuine
	// SOI-edge encounters through to the exact path.
	rThreshold := capturePrimary.RadiusMeters() * 5
	if captureState.R.Norm() < rThreshold {
		return w.approximateCapturePreview(captureState, capturePrimary, captureTime, captureIdx), true
	}
	mu := capturePrimary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(capturePrimary)
	ro := orbital.OrbitReadoutInFrame(captureState.R, captureState.V, mu, frame)
	return CapturePreview{
		Primary:      capturePrimary,
		NodeIndex:    captureIdx,
		When:         captureTime,
		Inclination:  ro.Inclination,
		ApoapsisM:    ro.ApoMeters,
		PeriapsisM:   ro.PeriMeters,
		Hyperbolic:   ro.Hyperbolic,
		Eccentricity: ro.Eccentricity,
	}, true
}

// approximateCapturePreview builds the qualitative preview shown
// when the chained-predictor's state.R degenerates near the target.
// |v_∞| comes from state.V (post-burn velocity in target frame —
// post-burn rather than pre-burn so the player sees the residual
// speed they'll actually live with). Direction is inferred from
// v_∞ · v_target_parent_frame: negative dot product → craft moving
// against target's orbital direction → retrograde capture.
//
// For Hohmann transfers from interior orbits (the typical Earth →
// Luna case), this nearly always returns Retrograde=true. Outer →
// inner Hohmanns can produce prograde encounters.
func (w *World) approximateCapturePreview(
	state physics.StateVector,
	primary bodies.CelestialBody,
	captureTime time.Time,
	idx int,
) CapturePreview {
	approach := state.V.Norm()
	retrograde := false

	// Compare state.V (in target's frame) with target's velocity in
	// its parent frame at captureTime. If they point opposite ways
	// (negative dot product), craft enters target's SOI from "ahead"
	// moving backward — retrograde capture. If positive, craft is
	// catching up to target from behind — prograde capture.
	sys := w.System()
	if parent := sys.ParentOf(primary); parent != nil {
		vTargetInert := w.bodyInertialVelocityAt(primary, captureTime)
		vParentInert := w.bodyInertialVelocityAt(*parent, captureTime)
		vTargetInParent := vTargetInert.Sub(vParentInert)
		if vTargetInParent.Norm() > 0 && approach > 0 {
			dot := state.V.X*vTargetInParent.X +
				state.V.Y*vTargetInParent.Y +
				state.V.Z*vTargetInParent.Z
			retrograde = dot < 0
		}
	}

	return CapturePreview{
		Primary:           primary,
		NodeIndex:         idx,
		When:              captureTime,
		Approximate:       true,
		ApproachSpeed:     approach,
		RetrogradeCapture: retrograde,
	}
}

// PredictedLeg describes the trajectory leg following a single
// planted maneuver node — the orbit the craft would fly between
// this node firing and the next one (or for one orbital period if
// there's no next node). v0.6.1 uses this to render each leg in a
// distinct color so the player can read which orbit segment
// belongs to which planted burn.
type PredictedLeg struct {
	NodeIndex   int                  // index into World.Nodes
	State       physics.StateVector  // post-burn state in Primary's frame
	Primary     bodies.CelestialBody // frame the state is expressed in
	HorizonSecs float64              // duration to predict for (until next node, or one period)
	StartClock  time.Time            // wall-clock at which the post-burn state lives — drives time-aware body lookups in PredictedSegmentsFrom (v0.8.4+)
}

// PredictedLegs walks every resolved planted node and returns one
// PredictedLeg per node, with the post-burn state expressed in the
// node's intended frame (PrimaryID, falling back to the propagated
// frame when unspecified). Returns nil during an active burn — the
// live state is mutating and chained predictions would flail (see
// PredictedFinalOrbit's same guard).
func (w *World) PredictedLegs() []PredictedLeg {
	if w.ActiveCraft() == nil || len(w.ActiveCraft().Nodes) == 0 || w.ActiveCraft().ActiveBurn != nil {
		return nil
	}
	state := w.ActiveCraft().State
	primary := w.ActiveCraft().Primary
	clock := w.Clock.SimTime
	systems := w.Systems
	legs := make([]PredictedLeg, 0, len(w.ActiveCraft().Nodes))
	for i, n := range w.ActiveCraft().Nodes {
		if !n.IsResolved() {
			continue
		}
		dt := n.TriggerTime.Sub(clock).Seconds()
		if dt > 0 {
			state, primary = w.propagateStateWithPrimary(state, primary, clock, dt)
			clock = n.TriggerTime
		}
		// Frame rebase if the node was planted in a specific
		// destination frame. v0.8.2.x: snapshot body position +
		// velocity at the node's trigger time, not at SimTime — Luna
		// moves ~30° around Earth in 3 days, and using SimTime here
		// misplaces the rebase by Luna's actual motion, which
		// distorts the post-capture inclination preview.
		if target, ok := bodies.LookupByID(systems, n.PrimaryID); ok && target.ID != primary.ID {
			oldInertial := w.BodyPositionAt(primary, clock)
			newInertial := w.BodyPositionAt(target, clock)
			vOld := w.bodyInertialVelocityAt(primary, clock)
			vNew := w.bodyInertialVelocityAt(target, clock)
			state = physics.Rebase(state, oldInertial, newInertial, vOld.Sub(vNew))
			primary = target
		}
		dir := spacecraft.DirectionUnit(n.Mode, state.R, state.V)
		if dir.Norm() != 0 && n.DV != 0 {
			state.V = state.V.Add(dir.Scale(n.DV))
		}
		// Horizon: until next planted node, else one orbital period.
		var horizon float64
		if i+1 < len(w.ActiveCraft().Nodes) && w.ActiveCraft().Nodes[i+1].IsResolved() {
			horizon = w.ActiveCraft().Nodes[i+1].TriggerTime.Sub(clock).Seconds()
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
			StartClock:  clock,
		})
	}
	return legs
}

// PreviewBurnState returns the craft state immediately after a
// hypothetical burn with the given (mode, dv, duration, event)
// parameters would fire — without mutating world state. Used by the
// maneuver-planner screen so its shadow trajectory + PROJECTED ORBIT
// readout reflect where the burn would *actually* fire, not where the
// craft is sitting right now.
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
//
// v0.6.3 polish: when duration > 0 the helper routes through
// `planner.SimulateFiniteBurn` so the preview reflects finite-burn
// deformation (off-tangential velocity rotation through the burn arc,
// finite-burn cosine loss, etc.) rather than the impulsive
// idealisation. The delivered Δv is also capped by the rocket-
// equation maximum the duration window allows — so a 400 m/s request
// with the form's default 10 s duration returns a preview reflecting
// only what 10 s of thrust would actually deliver (≈205 m/s for the
// S-IVB-1 default loadout), matching what the live integrator does
// when the burn terminates on duration rather than Δv.
func (w *World) PreviewBurnState(mode spacecraft.BurnMode, dv float64, duration time.Duration, event TriggerEvent) (physics.StateVector, bodies.CelestialBody, bool) {
	if w.ActiveCraft() == nil {
		return physics.StateVector{}, bodies.CelestialBody{}, false
	}
	state := w.ActiveCraft().State
	primary := w.ActiveCraft().Primary

	if event != TriggerAbsolute {
		mu := primary.GravitationalParameter()
		// v0.8.6+: AN/DN are body-equatorial. Frame-rotate state for
		// the event-time helpers; periapsis/apoapsis are frame-
		// invariant but we pass the rotated state for consistency.
		frame := orbital.ReferenceFrameForPrimary(primary)
		ostate := orbital.Vec3State{
			R: frame.FromWorld(state.R),
			V: frame.FromWorld(state.V),
		}
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
			state, primary = w.propagateStateWithPrimary(state, primary, w.Clock.SimTime, dt)
		}
	}

	if dv == 0 {
		return state, primary, true
	}

	thrust := w.ActiveCraft().Thrust
	isp := w.ActiveCraft().Isp
	useFinite := duration > 0 && thrust > 0 && isp > 0 && state.M > 0

	if useFinite {
		// Cap delivered Δv by what `duration` actually allows under
		// the rocket equation. Pre-v0.6.3 the preview used the raw
		// requested Δv; if the form's duration was too short to
		// deliver it (the in-flight burn terminates on duration, not
		// Δv) the projected orbit overshot what the player would see.
		mdot := thrust / (isp * 9.80665)
		massAfter := state.M - mdot*duration.Seconds()
		effectiveDv := dv
		if massAfter > 0 {
			maxDv := isp * 9.80665 * math.Log(state.M/massAfter)
			if effectiveDv > maxDv {
				effectiveDv = maxDv
			}
		}
		direction := func(r, v orbital.Vec3) orbital.Vec3 {
			return spacecraft.DirectionUnit(mode, r, v)
		}
		mu := primary.GravitationalParameter()
		state = planner.SimulateFiniteBurn(state, mu, thrust, isp, effectiveDv, direction)
		return state, primary, true
	}

	dir := spacecraft.DirectionUnit(mode, state.R, state.V)
	if dir.Norm() != 0 {
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
	if w.ActiveCraft() == nil || len(w.ActiveCraft().Nodes) == 0 {
		return physics.StateVector{}, bodies.CelestialBody{}, false
	}
	// v0.6.1: during an active finite burn the live craft state is
	// being mutated every integrator step. Chaining predictions
	// through that state produces flailing numbers each render and
	// a preview ellipse that rotates as fast as the engine fires.
	// Suppress the projection until the burn completes — the live
	// VESSEL block already shows the orbit changing in real time.
	if w.ActiveCraft().ActiveBurn != nil {
		return physics.StateVector{}, bodies.CelestialBody{}, false
	}
	state := w.ActiveCraft().State
	primary := w.ActiveCraft().Primary
	clock := w.Clock.SimTime
	any := false
	systems := w.Systems
	for _, n := range w.ActiveCraft().Nodes {
		if !n.IsResolved() {
			continue
		}
		dt := n.TriggerTime.Sub(clock).Seconds()
		if dt > 0 {
			state, primary = w.propagateStateWithPrimary(state, primary, clock, dt)
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
			oldInertial := w.BodyPositionAt(primary, clock)
			newInertial := w.BodyPositionAt(target, clock)
			vOld := w.bodyInertialVelocityAt(primary, clock)
			vNew := w.bodyInertialVelocityAt(target, clock)
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
	return w.propagateStateWithPrimary(w.ActiveCraft().State, w.ActiveCraft().Primary, w.Clock.SimTime, dt)
}

// propagateStateWithPrimary is the same SOI-aware integrator but
// parameterised on the starting state, primary, and clock. Used by
// PredictedFinalOrbit (v0.6.1) to chain through multiple planted
// nodes without mutating live craft state.
//
// v0.8.4: body positions refresh per chunk at the chunk's wall-clock
// offset rather than snapshotting at startClock. Without this an
// Earth→Mars Hohmann never crosses Mars's SOI during integration
// (Mars stays at its t=0 position), so the chained predictor
// degenerates and the arrival rebase lands state.R ≈ 0 (the
// "always-degenerate Hohmann" case). Per-chunk refresh costs O(N_bodies)
// of Kepler-ephemeris evaluation per Verlet sub-step, negligible vs the
// integration itself.
func (w *World) propagateStateWithPrimary(startState physics.StateVector, startPrimary bodies.CelestialBody, startClock time.Time, dt float64) (physics.StateVector, bodies.CelestialBody) {
	current := startPrimary
	muNow := current.GravitationalParameter()
	state := startState
	clock := startClock

	sys := w.System()

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
	stepDur := time.Duration(step * float64(time.Second))
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	bc := w.ActiveCraft().EffectiveBallisticCoefficient()
	for i := 0; i < nSteps; i++ {
		state = physics.StepVerletWithAccel(state, muNow, step, func(r, v orbital.Vec3) orbital.Vec3 {
			return physics.DragAccel(r, v, current, bc)
		})
		// v0.8.5: terminate propagation at surface contact. Mirrors the
		// live integrator (sim/world.go) and the trajectory predictor
		// (sim/predict.go) so node-planning sees the same landed state
		// the live craft would arrive at.
		if clamped, hit := physics.ClampToSurface(state, current); hit {
			return clamped, current
		}
		clock = clock.Add(stepDur)

		for _, b := range sys.Bodies {
			positions[b.ID] = w.BodyPositionAt(b, clock)
		}

		inertial := positions[current.ID].Add(state.R)
		cand := physics.FindPrimary(sys, inertial, positions)
		if cand.Body.ID != current.ID {
			vOld := w.bodyInertialVelocityAt(current, clock)
			vNew := w.bodyInertialVelocityAt(cand.Body, clock)
			state = physics.Rebase(state, positions[current.ID], cand.Inertial, vOld.Sub(vNew))
			current = cand.Body
			muNow = current.GravitationalParameter()
		}
	}
	return state, current
}
