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

// ManeuverNode represents a planned burn that will fire when
// World.Clock.SimTime reaches TriggerTime. Nodes are forward-looking only;
// once fired, they are removed from World.Nodes.
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
type ManeuverNode struct {
	TriggerTime time.Time
	Mode        spacecraft.BurnMode
	DV          float64
	Duration    time.Duration
	PrimaryID   string
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
func (w *World) PlanNode(n ManeuverNode) {
	w.Nodes = append(w.Nodes, n)
	sort.Slice(w.Nodes, func(i, j int) bool {
		return w.Nodes[i].TriggerTime.Before(w.Nodes[j].TriggerTime)
	})
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

	primary := sys.Bodies[0]
	muSun := primary.GravitationalParameter()
	rDeparture := w.CraftInertial().Norm()
	rArrival := target.SemimajorAxisMeters()

	// Parking-orbit radius at departure: craft's current |r| in its
	// home primary's frame. Capture radius at destination: hold-over
	// 200 km altitude default to mirror NewInLEO ergonomics — the
	// game-balance question of "what circular orbit does the player
	// want around Mars" is documented, not enforced.
	rPark := w.Craft.State.R.Norm()
	muDeparture := w.Craft.Primary.GravitationalParameter()
	rCapture := target.RadiusMeters() + 200e3
	muDestination := target.GravitationalParameter()

	plan, err := planner.PlanHohmannTransfer(
		muSun, rDeparture, rArrival,
		muDeparture, rPark, w.Craft.Primary.ID,
		muDestination, rCapture, target.ID,
	)
	if err != nil {
		return nil, err
	}

	now := w.Clock.SimTime
	w.PlanNode(transferNodeToManeuver(plan.Departure, now))
	w.PlanNode(transferNodeToManeuver(plan.Arrival, now))
	return &plan, nil
}

// PorkchopGrid computes a launch-window grid for a Hohmann-style
// transfer to the target body. Axes: depDays (offsets from now) and
// tofDays (time of flight). Each cell = total Δv (departure + capture,
// m/s); NaN for cells where Lambert didn't converge.
//
// Uses the same parking-orbit and capture-orbit defaults as PlanTransfer
// (craft's current |r| at departure, 200 km altitude at destination).
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

// bodyEphemeris returns an EphemerisFn closure for a body: heliocentric
// (r, v) evaluated at an arbitrary Unix-epoch timestamp.
func (w *World) bodyEphemeris(b bodies.CelestialBody) planner.EphemerisFn {
	return func(epoch float64) (orbital.Vec3, orbital.Vec3) {
		t := time.Unix(int64(epoch), 0)
		M := w.Calculator.CalculateMeanAnomaly(b, t)
		E := orbital.SolveKepler(M, b.Eccentricity)
		nu := orbital.TrueAnomaly(E, b.Eccentricity)
		el := orbital.ElementsFromBody(b)
		r := orbital.PositionAtTrueAnomaly(el, nu)
		mu := w.Systems[0].Bodies[0].GravitationalParameter()
		v := orbital.VelocityAtTrueAnomaly(el, nu, mu)
		return r, v
	}
}

func transferNodeToManeuver(tn planner.TransferNode, now time.Time) ManeuverNode {
	mode := spacecraft.BurnPrograde
	if tn.IsRetrograde {
		mode = spacecraft.BurnRetrograde
	}
	return ManeuverNode{
		TriggerTime: now.Add(tn.OffsetTime),
		Mode:        mode,
		DV:          tn.DV,
		PrimaryID:   tn.PrimaryID,
	}
}

var (
	errInvalidTransferTarget = transferError("invalid transfer target body")
	errNoCraftForTransfer    = transferError("no craft to plan transfer for")
)

type transferError string

func (e transferError) Error() string { return string(e) }

// ClearNodes wipes every pending node.
func (w *World) ClearNodes() { w.Nodes = nil }

// executeDueNodes fires every node whose TriggerTime has passed, applying
// the burn to the spacecraft in order. Called from Tick after sim-time
// advances. Re-entrant: if two nodes fall in the same tick, both fire.
//
// Impulsive nodes (Duration==0) apply their Δv inline and are popped.
// Finite nodes (Duration>0) start an ActiveBurn and are popped; the burn
// then runs across subsequent ticks via the RK4 branch in
// integrateSpacecraft. If a finite burn is already active when a new
// finite node fires, the new one replaces it (last-write-wins; the
// planner UI is responsible for not over-stacking).
func (w *World) executeDueNodes() {
	if w.Craft == nil {
		return
	}
	fired := 0
	for _, n := range w.Nodes {
		if n.TriggerTime.After(w.Clock.SimTime) {
			break
		}
		if n.Duration == 0 {
			w.Craft.ApplyImpulsive(n.Mode, n.DV)
		} else {
			w.ActiveBurn = &ActiveBurn{
				Mode:        n.Mode,
				DVRemaining: n.DV,
				EndTime:     n.TriggerTime.Add(n.Duration),
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
	current := w.Craft.Primary
	muNow := current.GravitationalParameter()
	state := w.Craft.State

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
