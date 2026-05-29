package sim

import (
	"fmt"
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
	TriggerAbsolute            = spacecraft.TriggerAbsolute
	TriggerNextPeri            = spacecraft.TriggerNextPeri
	TriggerNextApo             = spacecraft.TriggerNextApo
	TriggerNextAN              = spacecraft.TriggerNextAN
	TriggerNextDN              = spacecraft.TriggerNextDN
	TriggerNextClosestApproach = spacecraft.TriggerNextClosestApproach
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
	if c.ActiveStageFuel() <= 0 || c.Thrust <= 0 {
		return
	}
	if c.EngineMode != spacecraft.EngineMain {
		return
	}
	if c.EffectiveThrottle() <= 0 {
		return
	}
	// v0.9.2+: engine ignition releases a Landed craft into normal
	// integration. Pre-fix, a craft on the launchpad with Landed=true
	// would stay parked even after `b` engaged — the manual-burn
	// thrust accumulated against an integrator that never updated R/V
	// because the Landed bypass returned early. Clearing here means
	// the next Tick runs normal physics with the surface co-rotation
	// velocity as the initial condition.
	c.Landed = false
	// v0.11.4+ (ADR 0004): first liftoff clears OnPad so a future
	// soft-landing's Landed=false→true transition doesn't fire the
	// ViewLaunch auto-route (which gates on OnPad).
	c.OnPad = false
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
	if t != c.Throttle {
		c.LastThrottleChangeAt = w.Clock.SimTime
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
		case TriggerNextClosestApproach:
			// v0.9.3+: bound to the craft captured at plant time
			// via n.TargetCraftIdx. Skip if the target slot is
			// stale (out-of-range / nil craft / different primary
			// — same-primary only for the manual rendezvous slice).
			tIdx, ok := n.TargetCraftIdxValue()
			if !ok {
				continue
			}
			if tIdx < 0 || tIdx >= len(w.Crafts) {
				continue
			}
			tc := w.Crafts[tIdx]
			if tc == nil {
				continue
			}
			if tc.Primary.ID != c.Primary.ID {
				continue
			}
			active := orbital.Vec3State{R: c.State.R, V: c.State.V}
			target := orbital.Vec3State{R: tc.State.R, V: tc.State.V}
			// 4 hours horizon — same as the HUD readout. If the
			// encounter is farther than that, the resolver retries
			// next tick (planner returns the in-horizon minimum
			// even if it's not a true encounter, but the player can
			// always extend the horizon by replanning closer to
			// the encounter).
			tCA, _, _, err := planner.NextClosestApproach(active, target, c.Primary, mu, 4*3600)
			if err != nil {
				continue
			}
			dt = tCA
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
	// Clear the dual-strategy record; only the intra-primary branch
	// (below) repopulates it, so non-intra-primary plants don't show a
	// stale combined/split comparison.
	w.LastTransfer = TransferComparison{}

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
		c := w.ActiveCraft()
		muShared := c.Primary.GravitationalParameter()
		rArrival := target.SemimajorAxisMeters()
		craftAngle := math.Atan2(c.State.R.Y, c.State.R.X)
		// Target's position in its parent's frame == craft's primary
		// here, since target.ParentID == craft.Primary.ID.
		targetAngle := primaryFrameAngle(w, target)
		mass := c.TotalMass()
		thrust := c.Thrust
		// minLead = half the centred finite-burn duration so the planner's
		// OffsetTime sits ≥ Duration/2 ahead of now (BurnStart can't be
		// retroactive). v0.12.x: the fused departure folds a plane change
		// into the burn, so it can be much longer than the coplanar TLI —
		// size minLead off (coplanar raise + plane-change allowance), an
		// upper bound on the fused Δv (triangle inequality), so BurnStart
		// stays ≥ now for the inclined LEO→Luna case.
		dvDepEstimate := estimateIntraPrimaryDepDv(muShared, rPark, rArrival) +
			intraPlaneChangeAllowance(w, c, target, muShared, rPark)
		minLead := c.BurnTimeForDV(dvDepEstimate).Seconds() / 2
		// Analytic Hohmann seed — its phasing (Departure.OffsetTime,
		// TransferDt) seeds the fused Lambert, and it is the graceful
		// fallback if the fused solve is degenerate.
		seed, err := planner.PlanIntraPrimaryHohmann(
			muShared, rPark, rArrival,
			craftAngle, targetAngle, minLead,
			c.Primary.ID,
			muDestination, rCapture, target.ID,
		)
		if err != nil {
			return nil, err
		}
		now := w.Clock.SimTime
		// v0.12.x (ADR 0005): dual-strategy. Compute BOTH transfers and
		// plant the cheaper, surfacing both totals for the HUD.
		//
		//  - Combined: a fused single-rev Lambert from the craft's actual
		//    departure state to the target's actual arrival position — the
		//    departure Δv carries eccentricity + raise + plane change
		//    together (a BurnVector). Wins when near-coplanar.
		//  - Split: the coplanar Hohmann raise (prograde) + a plane change
		//    at the transfer apoapsis (slowest point → cheapest plane
		//    change) + the capture braking burn. Wins for large departure
		//    inclinations (an equatorial LEO sits ~25° off Luna's plane).
		combinedDv := math.Inf(1)
		combined, combinedErr := w.fusedIntraPrimaryDeparture(seed, muShared, target, muDestination, rCapture)
		if combinedErr == nil {
			combinedDv = combined.Departure.DV + combined.Arrival.DV
		}

		// v0.12.x (ADR 0006 A): for an inclined target the split must
		// place its apoapsis on the line of nodes (craft-plane ∩ target-
		// plane) where the target will be, so the transfer rendezvous and
		// the plane change there arrives coplanar. Node-aligned timing
		// drives the departure point; near-coplanar targets (nodeOK=false)
		// keep the opposition phasing from the analytic seed.
		nodeTau, nodeTransfer, nodeOK := w.splitNodePhasing(c, target, muShared, rPark, rArrival, minLead)
		splitWait := seed.Departure.OffsetTime.Seconds()
		if nodeOK {
			splitWait = nodeTau
		}

		// Split sizing: coplanar raise + capture come from the analytic
		// seed; the plane change is added at the apoapsis (now on the node
		// line, so its rotation maps the craft's plane onto the target's).
		var planeChangeDv, planeChangeTheta float64
		depState, depOK := physics.KeplerStep(c.State, muShared, splitWait)
		if nCraftHat, nTargetHat, ok := w.craftTargetPlaneNormals(c, target); ok && depOK {
			planeChangeDv, planeChangeTheta = splitPlaneChangeAtApoapsis(
				depState, nCraftHat, nTargetHat, muShared, rPark, rArrival)
		}
		splitDv := seed.Departure.DV + planeChangeDv + seed.Arrival.DV

		w.LastTransfer = TransferComparison{CombinedDv: combinedDv, SplitDv: splitDv}

		if combinedDv <= splitDv {
			w.LastTransfer.Strategy = "combined"
			w.PlanNode(transferNodeToManeuver(combined.Departure, now, c))
			w.PlanNode(transferNodeToManeuver(combined.Arrival, now, c))
			return &combined, nil
		}

		// Split: refine the finite coplanar departure (v0.6.2 iterator) so
		// the burn delivers the target apoapsis under integration, then
		// plant raise → plane change → capture.
		w.LastTransfer.Strategy = "split"
		plan := seed
		refineFiniteDeparture(&plan, muShared, rPark, mass, thrust, c.Isp, rArrival)
		// Override the seed's opposition phasing with the node-aligned
		// departure/arrival so apoapsis lands on the node where the target
		// is (the Δv magnitudes above are phasing-independent).
		if nodeOK {
			plan.Departure.OffsetTime = time.Duration(nodeTau * float64(time.Second))
			plan.TransferDt = time.Duration(nodeTransfer * float64(time.Second))
			plan.Arrival.OffsetTime = plan.Departure.OffsetTime + plan.TransferDt
		}

		// v0.12.x (GH #67 follow-up): the plane change can only align the
		// planes at a node, and the only cheap node (apoapsis) sits inside
		// the target's SOI — firing it there would run in the target's
		// frame and collide with the capture (both at apoapsis). Instead
		// fire the plane change just before SOI entry (still in the
		// primary's frame, near-apoapsis so still cheap, off-node so it
		// only *reduces* the relative inclination) and the capture at
		// perilune. Distinct times; partial — not perfect — coplanarity
		// (see ADR 0006 A: a truly coplanar capture needs frame alignment
		// at capture, deferred).
		planeChangeOffset := plan.Arrival.OffsetTime
		captureOffset := plan.Arrival.OffsetTime
		pcDv, pcTheta := planeChangeDv, planeChangeTheta
		if nodeOK && depOK {
			raiseDir := spacecraft.DirectionUnit(spacecraft.BurnPrograde, depState.R, depState.V)
			post := depState
			post.V = depState.V.Add(raiseDir.Scale(seed.Departure.DV))
			depClock := now.Add(time.Duration(nodeTau * float64(time.Second)))
			if tEntry, tCA, found := w.transferEncounterTimes(post, c.Primary, target, depClock, nodeTransfer*1.2); found {
				if entry, ok := physics.KeplerStep(post, muShared, tEntry); ok {
					if _, nTgt, ok2 := w.craftTargetPlaneNormals(c, target); ok2 {
						pcDv, pcTheta = planeChangeAtState(entry, nTgt)
					}
				}
				planeChangeOffset = time.Duration((nodeTau + tEntry) * float64(time.Second))
				captureOffset = time.Duration((nodeTau + tCA) * float64(time.Second))
			}
		}

		w.PlanNode(transferNodeToManeuver(plan.Departure, now, c))
		if pcDv > 0 {
			w.PlanNode(ManeuverNode{
				TriggerTime:    now.Add(planeChangeOffset),
				Mode:           spacecraft.BurnPlaneChange,
				DV:             pcDv,
				Duration:       c.BurnTimeForDV(pcDv),
				PrimaryID:      c.Primary.ID,
				PlaneChangeRad: pcTheta,
			})
		}
		plan.Arrival.OffsetTime = captureOffset
		w.PlanNode(transferNodeToManeuver(plan.Arrival, now, c))
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
func (w *World) PlanTransferAt(targetIdx int, depDay, tofDay float64, opts TransferOptions) (*planner.TransferPlan, error) {
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
	plan, err := planner.PlanLambertTransfer(
		muSun,
		rDep, vDep,
		rArr, vArr,
		tofDay*secondsPerDay,
		muDep, rPark, w.ActiveCraft().Primary.ID,
		muArr, rCapture, target.ID,
		depOffset,
		opts.Retrograde,
		opts.NRev,
		opts.LongBranch,
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

// IterateBurnDV refines the commanded Δv for a finite burn so the
// post-burn orbit's apsides match what an impulsive burn at the same
// commanded Δv would have produced. Newton-iterates against an RK4
// finite-burn simulation (planner.IterateForTarget). Returns the
// refined Δv on success; falls back to dvGuess on iteration failure
// or for burn modes that don't have a meaningful apse target
// (BurnNormal±).
//
// Target picked from mode:
//   - Prograde / Retrograde → match the impulsive apoapsis.
//   - RadialOut / RadialIn → match the impulsive periapsis.
//   - Normal± → no iteration (skip; PlanInclinationChange handles
//     plane-rotation Δv compensation differently).
//
// Limitation: iterates from the craft's *current* state, not the
// state at TriggerTime. For burns scheduled minutes-or-less ahead the
// state drift is negligible; for hours-ahead schedules the iteration
// is approximate. v0.8.6 (b).
func (w *World) IterateBurnDV(mode spacecraft.BurnMode, dvGuess float64) (float64, error) {
	c := w.ActiveCraft()
	if c == nil {
		return dvGuess, errNoCraftForTransfer
	}
	if dvGuess <= 0 || c.Thrust <= 0 || c.Isp <= 0 {
		return dvGuess, nil
	}
	mu := c.Primary.GravitationalParameter()
	if mu <= 0 {
		return dvGuess, nil
	}

	// Compute the impulsive post-burn elements to extract the implicit
	// target — what the projected-orbit preview already shows.
	dirUnit := spacecraft.DirectionUnit(mode, c.State.R, c.State.V)
	if dirUnit.Norm() == 0 {
		return dvGuess, nil
	}
	postV := c.State.V.Add(dirUnit.Scale(dvGuess))
	impulsiveEl := orbital.ElementsFromState(c.State.R, postV, mu)

	var residual planner.ResidualFn
	switch mode {
	case spacecraft.BurnPrograde, spacecraft.BurnRetrograde:
		residual = planner.TargetApoapsis(impulsiveEl.Apoapsis())
	case spacecraft.BurnRadialOut, spacecraft.BurnRadialIn:
		residual = planner.TargetPeriapsis(impulsiveEl.Periapsis())
	default:
		// BurnNormal± — inclination targets need a different residual;
		// PlanInclinationChange already handles plane-rotation Δv.
		return dvGuess, nil
	}

	direction := func(r, v orbital.Vec3) orbital.Vec3 {
		return spacecraft.DirectionUnit(mode, r, v)
	}
	const tolMeters = 1000.0
	const maxIter = 8
	refined, _, err := planner.IterateForTarget(
		c.State, mu, c.Thrust, c.Isp, dvGuess,
		direction, residual, tolMeters, maxIter,
	)
	if err != nil {
		return dvGuess, err
	}
	return refined, nil
}

// PlanInclinationChange plants a single BurnPlaneChange maneuver node
// that rotates the craft's orbital plane to targetIncl (radians, in
// [0, π]). The burn fires at the next ascending or descending node,
// whichever comes sooner. The node carries the planner's signed
// rotation angle (PlaneChangeRad); the burn rotates the horizontal
// velocity through it about the radial axis, preserving |v| — see
// spacecraft.planeChangeDirection.
//
// Returns the planner's InclinationPlan (Δv + chosen node) for HUD
// flashing; surfaces the planner's error untouched if the source
// orbit is equatorial / hyperbolic / already-at-target.
//
// v0.7.4+. v0.10.4: a true plane change (was a pure orbit-normal burn,
// which over-sped the craft and left the orbit eccentric). Composes
// with v0.6.0's burn-at-next scheduler — the planted node uses an
// absolute TriggerTime (event resolver isn't needed since the planner
// already computed the future event time).
func (w *World) PlanInclinationChange(targetIncl float64) (*planner.InclinationPlan, error) {
	if w.ActiveCraft() == nil {
		return nil, errNoCraftForTransfer
	}
	mu := w.ActiveCraft().Primary.GravitationalParameter()
	// v0.8.6+: targetIncl is interpreted in the primary's reference
	// frame (body-equatorial for non-Sun primaries; ecliptic for the
	// Sun). Rotate the state into that frame before calling the inner
	// planner — Δv, time-of-flight and the signed rotation angle
	// (resolved against the live state at burn time) are all
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
	now := w.Clock.SimTime
	w.PlanNode(ManeuverNode{
		TriggerTime:    now.Add(plan.OffsetTime),
		Mode:           spacecraft.BurnPlaneChange,
		DV:             plan.DV,
		Duration:       w.ActiveCraft().BurnTimeForDV(plan.DV),
		PrimaryID:      plan.PrimaryID,
		PlaneChangeRad: plan.PlaneChangeRad,
	})
	return &plan, nil
}

// PlanPlaneMatch plants a single BurnPlaneChange node that rotates the
// active craft's orbital plane to *coincide* with the orbital plane of
// the body at targetIdx — matching both inclination magnitude AND the
// line of nodes (Ω), so a subsequent Hohmann transfer to that body
// departs in the right plane.
//
// PlanInclinationChange matches only the inclination *magnitude*: two
// orbits at equal inclination but different Ω are still tilted relative
// to each other (an equatorial LEO and the Moon's orbit, both read as
// ~19° in Earth's frame, sit ~25–39° apart). A Hohmann planned in the
// craft's plane then reaches the target's orbital radius far out of the
// target's plane and misses.
//
// "Coplanar with the target" is exactly "zero inclination measured in a
// frame whose Z axis is the target's orbit normal" — so PlanPlaneMatch
// re-expresses the craft state in that frame and asks the existing
// inclination solver for inclination 0. The burn fires where the craft
// crosses the target plane and rotates by the full dihedral angle; the
// signed rotation is frame-invariant, so the resulting BurnPlaneChange
// node flies through unchanged.
//
// Errors: ErrNoCraftForTransfer, errInvalidTransferTarget (bad index or
// a target with no orbit), and the planner's own errors surfaced
// untouched (already-coplanar / hyperbolic source). v0.10.4+.
func (w *World) PlanPlaneMatch(targetIdx int) (*planner.InclinationPlan, error) {
	if w.ActiveCraft() == nil {
		return nil, errNoCraftForTransfer
	}
	sys := w.System()
	if targetIdx <= 0 || targetIdx >= len(sys.Bodies) {
		return nil, errInvalidTransferTarget
	}
	target := sys.Bodies[targetIdx]
	nTarget := orbital.OrbitNormalWorld(target)
	if nTarget.Norm() == 0 {
		return nil, errInvalidTransferTarget // target has no orbital plane
	}
	nTargetHat := nTarget.Unit()
	c := w.ActiveCraft()
	primary := c.Primary
	mu := primary.GravitationalParameter()

	// Time to the next crossing of the target plane — an AN/DN crossing
	// in a frame whose Z axis is the target's orbit normal.
	planeFrame := orbital.FrameFromNormal(nTarget)
	stateTF := orbital.Vec3State{
		R: planeFrame.FromWorld(c.State.R),
		V: planeFrame.FromWorld(c.State.V),
	}
	tAN := orbital.TimeToNodeCrossing(stateTF, mu, true)
	tDN := orbital.TimeToNodeCrossing(stateTF, mu, false)
	dt := -1.0
	atAN := false
	if tAN >= 0 && (tDN < 0 || tAN <= tDN) {
		dt, atAN = tAN, true
	} else if tDN >= 0 {
		dt, atAN = tDN, false
	}
	if dt < 0 {
		// No crossing — the craft is already coplanar with the target.
		return nil, planner.ErrInclinationNoOp
	}

	// Propagate to the crossing and derive the burn geometrically. At
	// the crossing the radial axis lies along the two planes' mutual
	// node line, so a rotation of the craft's orbit normal about r̂
	// onto the target normal aligns the planes exactly. θ is that
	// signed rotation (|θ| = the dihedral angle); spacecraft.
	// planeChangeDirection turns it into the tilted burn at fire time.
	post := w.propagateCraft(dt)
	rHat := post.R.Unit()
	hHat := post.R.Cross(post.V).Unit()
	theta := math.Atan2(hHat.Cross(nTargetHat).Dot(rHat), hHat.Dot(nTargetHat))
	vHor := post.V.Sub(rHat.Scale(post.V.Dot(rHat)))
	dv := 2 * vHor.Norm() * math.Sin(math.Abs(theta)/2)
	if dv == 0 {
		return nil, planner.ErrInclinationNoOp
	}
	now := w.Clock.SimTime
	w.PlanNode(ManeuverNode{
		TriggerTime:    now.Add(time.Duration(dt * float64(time.Second))),
		Mode:           spacecraft.BurnPlaneChange,
		DV:             dv,
		Duration:       c.BurnTimeForDV(dv),
		PrimaryID:      primary.ID,
		PlaneChangeRad: theta,
	})
	return &planner.InclinationPlan{
		PrimaryID:      primary.ID,
		DV:             dv,
		OffsetTime:     time.Duration(dt * float64(time.Second)),
		NormalSign:     int(math.Copysign(1, theta)),
		PlaneChangeRad: theta,
		AtAN:           atAN,
	}, nil
}

// CircularizePlan summarises a planted circularize-at-apoapsis node
// for the caller's status flash. v0.9.4+.
type CircularizePlan struct {
	DV        float64 // m/s, prograde at next apoapsis
	ApoAltM   float64 // apoapsis altitude (m above primary mean radius) at plant time
	PrimaryID string
}

// PlanCircularizeAtApoapsis plants a prograde burn at the active
// craft's next apoapsis sized to circularise the orbit there
// (target periapsis = current apoapsis radius). Mirrors v0.9.3's
// "single-keystroke planter" pattern (auto-plant Hohmann via `H`,
// inclination match via `I`, rendezvous via `R` once that lands)
// applied to the ascent flow's natural last step.
//
// Δv is computed analytically from vis-viva — the prograde
// difference between circular speed at apoapsis (sqrt(mu/r_apo))
// and the orbit's along-track speed there
// (sqrt(mu·(2/r_apo − 1/a))). The integrator handles finite-burn
// loss at fire time using the existing planted-node burn pipeline;
// the impulsive Δv is within ~1-2% of the iterated finite-burn
// answer at S-IVB-class TWR (1+ in vacuum), enough to land the
// circularisation above the 200 km mission floor on most attempts.
//
// Errors:
//   - ErrNoCraftForCircularize: no active craft.
//   - ErrCircularizeBelowAtmosphere: apoapsis is inside the primary's
//     atmosphere (not a useful coast target). Player should keep
//     burning the ascent profile to raise apoapsis first.
//   - ErrCircularizeBadOrbit: hyperbolic / degenerate state — the
//     "next apoapsis" math doesn't converge.
//
// v0.9.4+.
func (w *World) PlanCircularizeAtApoapsis() (*CircularizePlan, error) {
	c := w.ActiveCraft()
	if c == nil {
		return nil, ErrNoCraftForCircularize
	}
	mu := c.Primary.GravitationalParameter()
	if mu <= 0 {
		return nil, ErrCircularizeBadOrbit
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	if el.E >= 1 || el.A <= 0 {
		return nil, ErrCircularizeBadOrbit
	}
	rApo := el.Apoapsis()
	primaryR := c.Primary.RadiusMeters()
	apoAltM := rApo - primaryR
	// Gate: apoapsis must clear the atmosphere (otherwise the burn
	// fires inside drag, defeating the whole point). For atmosphere-
	// less primaries, fall back to "above the surface" — a low-orbit
	// Mun-style scenario.
	atmosphereCutoff := 0.0
	if c.Primary.Atmosphere != nil {
		atmosphereCutoff = c.Primary.Atmosphere.CutoffAltitude
	}
	if apoAltM <= atmosphereCutoff {
		return nil, ErrCircularizeBelowAtmosphere
	}
	vAtApo := math.Sqrt(mu * (2/rApo - 1/el.A))
	vCircAtApo := math.Sqrt(mu / rApo)
	dv := vCircAtApo - vAtApo
	if dv <= 0 {
		// Already circular (or beyond) at apoapsis — nothing to plant.
		return nil, ErrCircularizeBadOrbit
	}
	w.PlanNode(ManeuverNode{
		Mode:      spacecraft.BurnPrograde,
		DV:        dv,
		Duration:  c.BurnTimeForDV(dv),
		Event:     spacecraft.TriggerNextApo,
		PrimaryID: c.Primary.ID,
	})
	return &CircularizePlan{
		DV:        dv,
		ApoAltM:   apoAltM,
		PrimaryID: c.Primary.ID,
	}, nil
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
// TransferOptions bundles the per-cell Lambert solve parameters that
// porkchop / PlanTransferAt forward to the planner: prograde-vs-
// retrograde, revolution count, and short-vs-long branch selection.
// Zero value (NRev=0, Retrograde=false, LongBranch=false) is the
// legacy single-rev prograde short-branch path. v0.10.5+.
type TransferOptions struct {
	NRev       int
	Retrograde bool
	LongBranch bool
}

func (w *World) PorkchopGrid(targetIdx int, depDays, tofDays []float64, opts TransferOptions) ([][]float64, error) {
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
		opts.Retrograde,
		opts.NRev,
		opts.LongBranch,
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

// TransferComparison is the dual-strategy Δv breakdown for an intra-
// primary [H] auto-plant: the combined fused-Lambert transfer (plane
// change folded into the departure) vs the split (coplanar raise + a
// plane change at the slow transfer apoapsis). PlanTransfer plants the
// cheaper and records this for the HUD. v0.12.x+ (ADR 0005).
type TransferComparison struct {
	CombinedDv float64 // total Δv of the combined fused-Lambert transfer (+Inf if non-convergent)
	SplitDv    float64 // total Δv of the split raise + plane-change + capture
	Strategy   string  // "combined" | "split"; "" when not an intra-primary plant
}

// Format renders the dual-strategy comparison as a one-line HUD flash —
// both candidate Δv totals and which was planted. Empty when the last
// plant wasn't an intra-primary transfer. v0.12.x+.
func (tc TransferComparison) Format() string {
	if tc.Strategy == "" {
		return ""
	}
	combined := "n/a"
	if !math.IsInf(tc.CombinedDv, 0) && !math.IsNaN(tc.CombinedDv) {
		combined = fmt.Sprintf("%.2f", tc.CombinedDv/1000)
	}
	return fmt.Sprintf("[H] combined %s / split %.2f km/s → planted %s",
		combined, tc.SplitDv/1000, tc.Strategy)
}

// craftTargetPlaneNormals returns the unit orbit-plane normals of the
// active craft and the target body (both in the shared primary's frame),
// and ok=false when either is degenerate. The target normal is sampled
// from two ephemeris positions a short time apart — frame-agnostic, no
// body-velocity API needed. v0.12.x+.
func (w *World) craftTargetPlaneNormals(c *spacecraft.Spacecraft, target bodies.CelestialBody) (nCraftHat, nTargetHat orbital.Vec3, ok bool) {
	now := w.Clock.SimTime
	const dt = time.Hour
	primary := c.Primary
	p0 := w.BodyPositionAt(target, now).Sub(w.BodyPositionAt(primary, now))
	p1 := w.BodyPositionAt(target, now.Add(dt)).Sub(w.BodyPositionAt(primary, now.Add(dt)))
	nTarget := p0.Cross(p1)
	nCraft := c.State.R.Cross(c.State.V)
	if nTarget.Norm() == 0 || nCraft.Norm() == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	return nCraft.Unit(), nTarget.Unit(), true
}

// relInclination returns the direction-agnostic plane tilt (radians, in
// [0, π/2]) between two unit normals.
func relInclination(nCraftHat, nTargetHat orbital.Vec3) float64 {
	cosI := nCraftHat.Dot(nTargetHat)
	if cosI > 1 {
		cosI = 1
	} else if cosI < -1 {
		cosI = -1
	}
	ang := math.Acos(cosI)
	return math.Min(ang, math.Pi-ang)
}

// splitPlaneChangeAtApoapsis sizes the split strategy's plane-change burn
// at the transfer apoapsis: the Δv (cheap, since apoapsis is the slowest
// point) and the signed rotation angle the BurnPlaneChange node carries.
// The apoapsis state is reconstructed analytically from the craft's
// departure state (apoapsis lies 180° from perigee in the craft's plane,
// at rArrival, with the prograde apoapsis velocity vApo) so the signed
// theta — computed exactly as PlanPlaneMatch does — rotates the craft's
// plane onto the target's at that point. v0.12.x+.
func splitPlaneChangeAtApoapsis(depState physics.StateVector, nCraftHat, nTargetHat orbital.Vec3, mu, rPark, rArrival float64) (dv, theta float64) {
	relIncl := relInclination(nCraftHat, nTargetHat)
	aT := (rPark + rArrival) / 2
	vApo := math.Sqrt(mu * (2/rArrival - 1/aT))
	dv = 2 * vApo * math.Sin(relIncl/2)
	// Apoapsis radial direction: opposite the departure-perigee position,
	// in the craft's orbital plane. Sign of theta from the same geometric
	// rotation PlanPlaneMatch uses (rotate ĥ_craft onto n̂_target about r̂).
	rApoHat := depState.R.Unit().Scale(-1)
	hHat := nCraftHat
	theta = math.Atan2(hHat.Cross(nTargetHat).Dot(rApoHat), hHat.Dot(nTargetHat))
	return dv, theta
}

// splitNodeMinIncl is the relative-inclination floor (rad, ≈0.5°) below
// which the craft and target planes have no distinct line of nodes worth
// constraining — splitNodePhasing bails and the caller keeps the coplanar
// opposition phasing.
const splitNodeMinIncl = 0.5 * math.Pi / 180

// wrapTau normalises an angle to [0, 2π).
func wrapTau(a float64) float64 {
	const tau = 2 * math.Pi
	a = math.Mod(a, tau)
	if a < 0 {
		a += tau
	}
	return a
}

// signedAngleAbout returns the signed angle (rad, in (−π, π]) from `from`
// to `to` measured about `axis` (right-handed): positive when `to` leads
// `from` in the +axis rotation sense. Used to time when a body sweeps
// onto a given inertial ray.
func signedAngleAbout(from, to, axis orbital.Vec3) float64 {
	f, t := from.Unit(), to.Unit()
	return math.Atan2(f.Cross(t).Dot(axis), f.Dot(t))
}

// timeToBodyDirection returns the soonest dt ≥ 0 (s) at which body b's
// position in primary's frame points along dHat — i.e. b crosses the
// inertial ray dHat (which must lie in b's orbit plane, normal nHat).
// A mean-motion estimate seeds a few Newton refinements against the true
// ephemeris, so the target's orbital eccentricity is accounted for rather
// than assuming uniform angular rate. period is b's orbital period (s).
func (w *World) timeToBodyDirection(b, primary bodies.CelestialBody, dHat, nHat orbital.Vec3, period float64) (float64, bool) {
	if period <= 0 || math.IsNaN(period) || math.IsInf(period, 0) {
		return 0, false
	}
	n := 2 * math.Pi / period
	psi := func(dt float64) float64 {
		t := w.Clock.SimTime.Add(time.Duration(dt * float64(time.Second)))
		p := w.BodyPositionAt(b, t).Sub(w.BodyPositionAt(primary, t))
		if p.Norm() == 0 {
			return 0
		}
		// Angle from dHat to the body about nHat; zero when aligned.
		return signedAngleAbout(dHat, p, nHat)
	}
	// Seed: the body must travel prograde (about nHat) by however far it
	// currently sits behind dHat.
	dt := wrapTau(-psi(0)) / n
	const h = 1.0 // 1 s finite-difference step for the angular rate
	for i := 0; i < 8; i++ {
		f := psi(dt)
		fp := (psi(dt+h) - f) / h
		if fp == 0 {
			break
		}
		step := f / fp
		dt -= step
		if math.Abs(step) < 1 {
			break
		}
	}
	for dt < 0 {
		dt += period
	}
	return dt, true
}

// splitNodePhasing computes the Line-of-Nodes departure timing for the
// split strategy (ADR 0006 decision A). It returns the wait time τ (s)
// until the prograde raise fires and the coast time (s) to apoapsis, so
// that:
//
//   - the raise's apoapsis lands on the craft-plane ∩ target-plane node
//     line (the craft departs from the antipodal node point, 180° away in
//     its own plane, so a pure in-plane raise puts apoapsis on the node);
//     and
//   - the target sits at that node when the craft arrives at apoapsis.
//
// A plane change at that apoapsis rotates about the node line (= the
// radial there), which maps the craft's plane exactly onto the target's —
// so the arrival is coplanar AND co-located with the target. This is what
// makes the inclined split actually rendezvous; pre-fix the plane change
// sat at an arbitrary apoapsis and the craft stayed ~sin(Δi)·r_apo (~100k
// km for LEO→Luna) out of the target's plane.
//
// Returns ok=false for near-coplanar geometry (no distinct node line) or
// a non-elliptic craft orbit; the caller keeps the opposition phasing.
func (w *World) splitNodePhasing(c *spacecraft.Spacecraft, target bodies.CelestialBody, muShared, rPark, rArrival, minLead float64) (tau, tTransfer float64, ok bool) {
	nCraftHat, nTargetHat, ok := w.craftTargetPlaneNormals(c, target)
	if !ok || relInclination(nCraftHat, nTargetHat) < splitNodeMinIncl {
		return 0, 0, false
	}
	nodeLine := nCraftHat.Cross(nTargetHat)
	if nodeLine.Norm() == 0 {
		return 0, 0, false
	}
	lineHat := nodeLine.Unit()

	aT := (rPark + rArrival) / 2
	tTransfer = math.Pi * math.Sqrt(aT*aT*aT/muShared)

	nCraft := math.Sqrt(muShared / (rPark * rPark * rPark))
	if nCraft <= 0 || math.IsNaN(nCraft) || math.IsInf(nCraft, 0) {
		return 0, 0, false
	}
	tPark := 2 * math.Pi / nCraft
	tTarget := 2 * math.Pi * math.Sqrt(rArrival*rArrival*rArrival/muShared)

	best := math.Inf(1)
	// Either node of the line can host the apoapsis; try both and take the
	// soonest feasible departure.
	for _, s := range []float64{1, -1} {
		nodeHat := lineHat.Scale(s) // apoapsis lands here; target must be here at arrival
		depHat := nodeHat.Scale(-1) // craft departs from the antipodal point

		// Craft's next pass through the departure point (circular parking
		// orbit, prograde about nCraftHat).
		tDepFirst := wrapTau(signedAngleAbout(c.State.R, depHat, nCraftHat)) / nCraft

		// Target's next arrival at the node, then the soonest later pass
		// whose implied departure (arrival − coast) clears minLead.
		tNode, okN := w.timeToBodyDirection(target, c.Primary, nodeHat, nTargetHat, tTarget)
		if !okN {
			continue
		}
		depTarget := tNode - tTransfer
		for depTarget < minLead {
			depTarget += tTarget
		}
		// Snap the departure to the nearest craft node-crossing (parking
		// period ≪ target period, so the residual is sub-orbit and the
		// target is still within its SOI at apoapsis).
		k := math.Round((depTarget - tDepFirst) / tPark)
		if k < 0 {
			k = 0
		}
		cand := tDepFirst + k*tPark
		for cand < minLead {
			cand += tPark
		}
		if cand < best {
			best = cand
		}
	}
	if math.IsInf(best, 0) {
		return 0, 0, false
	}
	return best, tTransfer, true
}

// planeChangeAtState sizes a plane-change burn fired at an arbitrary
// point on the transfer (not necessarily the line of nodes): the Δv
// magnitude (2·v_h·sin(|θ|/2)) and the signed rotation θ the
// BurnPlaneChange node carries. θ is the rotation about the radial that
// brings the orbit normal as close to the target's as a single radial-
// axis rotation allows — exact when the fire point is on the node line,
// a best-effort tilt reduction otherwise (v0.12.x, GH #67 follow-up: the
// split fires this just before SOI entry, off-apoapsis, so the burn can
// run in the primary's frame instead of the target's — see PlanTransfer).
func planeChangeAtState(st physics.StateVector, nTargetHat orbital.Vec3) (dv, theta float64) {
	rHat := st.R.Unit()
	vHor := st.V.Sub(rHat.Scale(st.V.Dot(rHat)))
	hHat := st.R.Cross(st.V).Unit()
	theta = math.Atan2(hHat.Cross(nTargetHat).Dot(rHat), hHat.Dot(nTargetHat))
	dv = 2 * vHor.Norm() * math.Sin(math.Abs(theta)/2)
	return dv, theta
}

// transferEncounterTimes propagates the post-departure transfer state
// analytically (Earth/primary frame, exact two-body) and returns the
// time (s, measured from departure) at which the craft first crosses into
// the target's SOI and the time of closest approach to the target.
// ok=false if the craft never enters the SOI within the horizon. Used by
// the split to fire the plane change just before SOI entry (in the
// primary's frame) and the capture at perilune — distinct burns rather
// than two stacked at the apoapsis instant (GH #67 follow-up).
func (w *World) transferEncounterTimes(post physics.StateVector, primary, target bodies.CelestialBody, startClock time.Time, horizon float64) (tEntry, tCA float64, ok bool) {
	mu := primary.GravitationalParameter()
	soi := physics.SOIRadius(target, primary)
	const n = 4000
	minD := math.Inf(1)
	tEntry = -1
	for i := 0; i <= n; i++ {
		dt := horizon * float64(i) / float64(n)
		st, k := physics.KeplerStep(post, mu, dt)
		if !k {
			continue
		}
		tt := startClock.Add(time.Duration(dt * float64(time.Second)))
		rel := st.R.Sub(w.BodyPositionAt(target, tt).Sub(w.BodyPositionAt(primary, tt)))
		d := rel.Norm()
		if d < minD {
			minD = d
			tCA = dt
		}
		if tEntry < 0 && d < soi {
			tEntry = dt
		}
	}
	if tEntry < 0 {
		return 0, 0, false
	}
	return tEntry, tCA, true
}

// intraPlaneChangeAllowance estimates the extra departure Δv a fused
// transfer needs to fold the craft→target plane change into the
// departure burn: ~2·v_circ·sin(Δi/2), where Δi is the angle between the
// craft's orbit plane and the target's orbit plane. Added to the
// coplanar raise estimate to upper-bound the fused departure Δv (triangle
// inequality), so World.PlanTransfer's minLead keeps BurnStart ≥ now even
// when the inclined fused burn runs much longer than the coplanar TLI.
// v0.12.x+.
func intraPlaneChangeAllowance(w *World, c *spacecraft.Spacecraft, target bodies.CelestialBody, mu, rPark float64) float64 {
	nCraftHat, nTargetHat, ok := w.craftTargetPlaneNormals(c, target)
	if !ok || mu <= 0 || rPark <= 0 {
		return 0
	}
	relIncl := relInclination(nCraftHat, nTargetHat)
	vCirc := math.Sqrt(mu / rPark)
	return 2 * vCirc * math.Sin(relIncl/2)
}

// fusedIntraPrimaryDeparture attempts the v0.12 combined plane-shift +
// Hohmann fused-Lambert transfer for an intra-primary target, seeded off
// the analytic plan's phasing (waitTime via Departure.OffsetTime, tof via
// TransferDt). It propagates the craft analytically (Kepler — exact, no
// Verlet drift over a multi-orbit phasing wait) to the departure epoch
// and the target via ephemeris to the arrival epoch, both expressed in
// the shared primary's frame, then solves a single-rev Lambert connecting
// them. The departure Δv carries eccentricity + raise + plane change as a
// BurnVector. Returns an error (caller falls back to the analytic seed)
// when the craft orbit isn't elliptic or the Lambert solve is degenerate.
// v0.12.x+ (ADR 0005).
func (w *World) fusedIntraPrimaryDeparture(seed planner.TransferPlan, muShared float64, target bodies.CelestialBody, muTarget, rCapture float64) (planner.TransferPlan, error) {
	c := w.ActiveCraft()
	waitTime := seed.Departure.OffsetTime
	tof := seed.TransferDt.Seconds()
	if tof <= 0 {
		return planner.TransferPlan{}, transferError("fused: non-positive transfer time")
	}
	depState, ok := physics.KeplerStep(c.State, muShared, waitTime.Seconds())
	if !ok {
		return planner.TransferPlan{}, transferError("fused: craft parking orbit not elliptic")
	}
	primary := c.Primary
	arrEpoch := w.Clock.SimTime.Add(waitTime + seed.TransferDt)
	rArr := w.BodyPositionAt(target, arrEpoch).Sub(w.BodyPositionAt(primary, arrEpoch))
	vArr := w.bodyInertialVelocityAt(target, arrEpoch).Sub(w.bodyInertialVelocityAt(primary, arrEpoch))
	return planner.PlanIntraPrimaryFused(
		muShared, depState.R, depState.V, rArr, vArr, tof,
		waitTime, primary.ID, muTarget, rCapture, target.ID)
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
	// v0.12.x: a fused-Lambert departure carries a full 3D Δv direction
	// (BurnDir) no prograde/retrograde flag can express — plant it as a
	// BurnVector node capturing the inertial unit direction.
	var burnDir orbital.Vec3
	if tn.BurnDir.Norm() > 0 {
		mode = spacecraft.BurnVector
		burnDir = tn.BurnDir.Unit()
	}
	return ManeuverNode{
		TriggerTime: now.Add(tn.OffsetTime),
		Mode:        mode,
		DV:          tn.DV,
		Duration:    craft.BurnTimeForDV(tn.DV),
		PrimaryID:   tn.PrimaryID,
		BurnDirUnit: burnDir,
	}
}

var (
	errInvalidTransferTarget = transferError("invalid transfer target body")
	errNoCraftForTransfer    = transferError("no craft to plan transfer for")
	errNoRefineTarget        = transferError("no pending transfer to refine")
	errSamePrimaryUseHohmann = transferError("target shares craft's primary — use [H] auto-Hohmann instead of porkchop")

	// PlanCircularizeAtApoapsis errors. Exported so app.go's status
	// flash can switch on them with errors.Is. v0.9.4+.
	ErrNoCraftForCircularize      = transferError("circularize: no active craft")
	ErrCircularizeBelowAtmosphere = transferError("circularize: apoapsis below atmosphere — keep climbing")
	ErrCircularizeBadOrbit        = transferError("circularize: hyperbolic / degenerate orbit")
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
		// v0.9.3+: resolve target snapshot for target-relative nodes
		// at fire time. Bound via n.TargetCraftIdx (captured at
		// plant), not the current World.Target — a target switch
		// between plant and fire doesn't silently retarget the burn.
		var rT, vT orbital.Vec3
		if n.IsTargetRelative() {
			if tIdx, ok := n.TargetCraftIdxValue(); ok && tIdx >= 0 && tIdx < len(w.Crafts) {
				if tc := w.Crafts[tIdx]; tc != nil && tc.Primary.ID == c.Primary.ID {
					rT, vT = tc.State.R, tc.State.V
				}
			}
		}
		if n.Duration == 0 {
			// A BurnPlaneChange or BurnVector node degrades to impulsive
			// only when BurnTimeForDV returned 0 (Δv past the fuel budget
			// / no engine) — both carry a direction that can't be decoded
			// from the BurnMode alone, so resolve via NodeBurnDirection.
			if n.Mode == spacecraft.BurnPlaneChange || n.Mode == spacecraft.BurnVector {
				c.ApplyImpulsiveDir(spacecraft.NodeBurnDirection(n, c.State.R, c.State.V), n.DV)
			} else {
				c.ApplyImpulsiveWithTarget(n.Mode, n.DV, rT, vT)
			}
		} else {
			c.ActiveBurn = &ActiveBurn{
				Mode:           n.Mode,
				DVRemaining:    n.DV,
				EndTime:        n.BurnEnd(),
				PrimaryID:      n.PrimaryID,
				Throttle:       n.EffectiveThrottle(),
				TargetCraftIdx: n.TargetCraftIdx,
				PlaneChangeRad: n.PlaneChangeRad,
				BurnDirUnit:    n.BurnDirUnit,
			}
		}
		// v0.9.2+: planted-burn ignition releases a Landed craft.
		// Symmetric with StartManualBurn; not strictly common
		// (planting on a launchpad is an unusual workflow) but
		// covers the case so a "planted node fires while the
		// craft is parked" scenario doesn't strand the integrator.
		// v0.11.4+ (ADR 0004): clear OnPad here too so a post-flight
		// soft-landing doesn't trip the ViewLaunch auto-route.
		c.Landed = false
		c.OnPad = false
		fired++
	}
	if fired > 0 {
		c.Nodes = c.Nodes[fired:]
	}
}

// nodeLeadSlack pads the computed lead time so a commanded direction
// that drifts during the lead window (e.g. prograde rotating in a low
// orbit) still converges before ignition. 1.25 = 25% slack. v0.10.0+.
const nodeLeadSlack = 1.25

// nodeLeadActive reports the upcoming node's ignition direction when
// the craft should already be slewing toward it — i.e. BurnStart is
// within nodeLeadSlack·angle/SlewRate sim-seconds of now. This is the
// lead-compensation that keeps planted-node Δv accurate under rate-
// limited attitude: the craft auto-orients ahead of T0 so it is
// converged at ignition (the node's Δv math + IsResolved/BurnStart
// gating in executeDueNodesFor are untouched — only attitude timing
// changes). Only the next-to-fire (first) node matters; nodes are
// sorted trigger-ascending with unresolved at the end.
//
// v0.10.0+.
func (w *World) nodeLeadActive(c *spacecraft.Spacecraft) (orbital.Vec3, bool) {
	if len(c.Nodes) == 0 {
		return orbital.Vec3{}, false
	}
	n := c.Nodes[0]
	if !n.IsResolved() {
		return orbital.Vec3{}, false
	}
	// Node's commanded ignition direction, evaluated against current
	// state + the target bound at plant (mirrors executeDueNodesFor).
	var rT, vT orbital.Vec3
	if n.IsTargetRelative() {
		if tIdx, ok := n.TargetCraftIdxValue(); ok && tIdx >= 0 && tIdx < len(w.Crafts) {
			if tc := w.Crafts[tIdx]; tc != nil && tc.Primary.ID == c.Primary.ID {
				rT, vT = tc.State.R, tc.State.V
			}
		}
	}
	dir := c.BurnDirectionForBurn(n.Mode, rT, vT, n.PlaneChangeRad, n.BurnDirUnit)
	if dir.Norm() == 0 {
		return orbital.Vec3{}, false
	}
	cosA := c.CurrentAttitudeDir.Unit().Dot(dir.Unit())
	if cosA > 1 {
		cosA = 1
	} else if cosA < -1 {
		cosA = -1
	}
	ang := math.Acos(cosA)
	leadSecs := nodeLeadSlack * ang / c.SlewRateRad()
	leadWindowStart := n.BurnStart().Add(-time.Duration(leadSecs * float64(time.Second)))
	if w.Clock.SimTime.Before(leadWindowStart) {
		return orbital.Vec3{}, false // not yet in the lead window
	}
	return dir, true
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
	dir := spacecraft.NodeBurnDirection(n, state.R, state.V)
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
		dir := spacecraft.NodeBurnDirection(n, state.R, state.V)
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
	Samples     int                  // adaptive trajectory-sample budget — ~96 points per orbital period the horizon spans (v0.10.3)
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
		dir := spacecraft.NodeBurnDirection(n, state.R, state.V)
		if dir.Norm() != 0 && n.DV != 0 {
			state.V = state.V.Add(dir.Scale(n.DV))
		}
		// Horizon: until next planted node, else one orbital period.
		period := orbitalPeriod(state, primary.GravitationalParameter())
		var horizon float64
		if i+1 < len(w.ActiveCraft().Nodes) && w.ActiveCraft().Nodes[i+1].IsResolved() {
			horizon = w.ActiveCraft().Nodes[i+1].TriggerTime.Sub(clock).Seconds()
		}
		if horizon <= 0 {
			horizon = period
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
			Samples:     adaptiveSampleCount(horizon, period),
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
		case TriggerNextClosestApproach:
			// v0.9.3+: preview against the current World.Target.
			// Plant-time binding kicks in at PlanNode; at preview the
			// player is still picking a target, so the live
			// World.Target is what matches their intent.
			rT, vT, ok := w.TargetStateRelativeToActivePrimary()
			if !ok {
				return physics.StateVector{}, bodies.CelestialBody{}, false
			}
			active := orbital.Vec3State{R: state.R, V: state.V}
			target := orbital.Vec3State{R: rT, V: vT}
			tCA, _, _, err := planner.NextClosestApproach(active, target, primary, mu, 4*3600)
			if err != nil {
				return physics.StateVector{}, bodies.CelestialBody{}, false
			}
			dt = tCA
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

	// v0.9.3+: target-relative modes resolve direction against the
	// current World.Target snapshot. Preview uses the snapshot
	// without forward-propagating the target across the burn — for
	// the short rendezvous burns these modes target (Δv ≪ |v|), the
	// approximation lands within UI noise.
	var rT, vT orbital.Vec3
	targetMode := spacecraft.IsTargetRelativeMode(mode)
	if targetMode {
		var ok bool
		rT, vT, ok = w.TargetStateRelativeToActivePrimary()
		if !ok {
			return state, primary, true
		}
	}

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
			if targetMode {
				return spacecraft.DirectionUnitTarget(mode, r, v, rT, vT)
			}
			return spacecraft.DirectionUnit(mode, r, v)
		}
		mu := primary.GravitationalParameter()
		state = planner.SimulateFiniteBurn(state, mu, thrust, isp, effectiveDv, direction)
		return state, primary, true
	}

	var dir orbital.Vec3
	if targetMode {
		dir = spacecraft.DirectionUnitTarget(mode, state.R, state.V, rT, vT)
	} else {
		dir = spacecraft.DirectionUnit(mode, state.R, state.V)
	}
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
		dir := spacecraft.NodeBurnDirection(n, state.R, state.V)
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
		// v0.12.x (GH #66): propagate ballistic coast legs with analytic
		// Kepler (predictStep), Verlet only for drag/hyperbolic/sub-
		// surface. A long phasing wait (an inclined transfer can sit ~50
		// parking orbits out) would otherwise drift tens of degrees of
		// phase under the 1024-step Verlet cap — misplacing the departure
		// point and, for the node-aligned split, the apoapsis off the
		// line of nodes. Mirrors the predictor in predict.go.
		state = predictStep(state, muNow, step, current, bc)
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
