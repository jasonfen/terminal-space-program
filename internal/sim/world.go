package sim

import (
	"fmt"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// World holds the simulation state: loaded systems, active-system index,
// the sim-clock, and — post-C15 — the spacecraft.
type World struct {
	Systems    []bodies.System
	SystemIdx  int
	Calculator orbital.Calculator
	Clock      *Clock

	// Craft is the player vessel. Spawns around Earth in Sol at startup.
	// Nil when no primary is loaded (unreachable in v0.1).
	Craft *spacecraft.Spacecraft

	// Focus selects what the OrbitView canvas is centered on. Zero value
	// (FocusSystem) matches v0.1.0 behavior.
	Focus Focus

	// Nodes holds planned burns, sorted by TriggerTime. Each fires
	// automatically when Clock.SimTime reaches its trigger.
	Nodes []ManeuverNode

	// ActiveBurn is non-nil while a finite-duration burn is mid-execution.
	// Set by executeDueNodes when a Duration>0 node fires; cleared by
	// integrateSpacecraft when DVRemaining hits zero or SimTime ≥ EndTime.
	ActiveBurn *ActiveBurn

	// soiCheckCounter throttles primary-reevaluation — we only need to
	// check every few ticks, not every Verlet sub-step.
	soiCheckCounter int
}

// NewWorld loads the embedded systems, seeds clock at J2000 + 50 ms base
// step, and spawns a spacecraft in LEO around Sol's Earth.
func NewWorld() (*World, error) {
	systems, err := bodies.LoadAll()
	if err != nil {
		return nil, fmt.Errorf("load systems: %w", err)
	}
	if len(systems) == 0 {
		return nil, fmt.Errorf("no systems loaded")
	}
	w := &World{
		Systems:   systems,
		SystemIdx: 0,
		Clock:     NewClock(bodies.J2000, 50*time.Millisecond),
	}
	w.Calculator = orbital.ForSystem(w.System(), w.Clock.SimTime)

	// Spawn spacecraft in LEO. v0.1: craft is always in Sol.
	earth := w.Systems[0].FindBody("Earth")
	if earth != nil {
		w.Craft = spacecraft.NewInLEO(*earth)
	}
	return w, nil
}

// System returns the currently active system.
func (w *World) System() bodies.System { return w.Systems[w.SystemIdx] }

// CycleSystem advances to the next system (wraps). Recreates the calculator.
// Spacecraft does not follow — remains in Sol per plan §MVP scope.
// Resets focus to system-wide because body indices don't carry across
// systems and the craft is only visible in Sol.
func (w *World) CycleSystem() {
	w.SystemIdx = (w.SystemIdx + 1) % len(w.Systems)
	w.Calculator = orbital.ForSystem(w.System(), w.Clock.SimTime)
	w.ResetFocus()
}

// CraftVisibleHere reports whether the spacecraft should be drawn in the
// currently-viewed system. v0.1 Craft lives in Sol only.
func (w *World) CraftVisibleHere() bool {
	return w.Craft != nil && w.SystemIdx == 0
}

// BodyPosition returns the inertial position (m) of a body in the current
// system at the current sim time. Primary (index 0) is anchored at origin.
func (w *World) BodyPosition(b bodies.CelestialBody) orbital.Vec3 {
	if b.SemimajorAxis == 0 {
		return orbital.Vec3{}
	}
	M := w.Calculator.CalculateMeanAnomaly(b, w.Clock.SimTime)
	E := orbital.SolveKepler(M, b.Eccentricity)
	nu := orbital.TrueAnomaly(E, b.Eccentricity)
	el := orbital.ElementsFromBody(b)
	return orbital.PositionAtTrueAnomaly(el, nu)
}

// CraftInertial returns the spacecraft's inertial position (Sun-centered)
// for rendering on the heliocentric canvas. Adds craft's primary-centric
// position to the primary's inertial position.
func (w *World) CraftInertial() orbital.Vec3 {
	if w.Craft == nil {
		return orbital.Vec3{}
	}
	primaryPos := w.BodyPosition(w.Craft.Primary)
	return primaryPos.Add(w.Craft.State.R)
}

// Tick advances sim-time one base step (scaled by warp factor) and
// integrates the spacecraft with velocity-Verlet sub-stepping so each
// sub-step is < 1/100th of the current orbital period.
func (w *World) Tick() {
	if w.Clock.Paused {
		return
	}

	// Apply SOI warp cap per plan §C21: if the current warp × base-step
	// would force the integrator to exceed its 1024-sub-step cap, reduce
	// effective warp this tick. Doesn't change the clock's displayed warp
	// (user still sees the level they picked); just prevents numerical
	// blow-up at pathologically high warps inside short-period orbits.
	effWarp := w.clampedWarp()
	simDelta := time.Duration(float64(w.Clock.BaseStep) * effWarp)
	w.Clock.SimTime = w.Clock.SimTime.Add(simDelta)

	if w.Craft != nil {
		w.integrateSpacecraft(simDelta)
		w.executeDueNodes()
		w.soiCheckCounter++
		if w.soiCheckCounter >= 20 {
			w.soiCheckCounter = 0
			w.maybeSwitchPrimary()
		}
	}
}

// clampedWarp returns min(selected warp, max warp allowed by the step-size
// guard, burn-warp cap if a finite burn is active). max = (1024 sub-steps
// × period/100) / base_step. Active-burn cap = 10× per docs/plan.md
// §Time-warp UX — finite burns at >10× warp would let the integrator
// blast past the EndTime in a single tick and lose temporal resolution.
func (w *World) clampedWarp() float64 {
	selected := w.Clock.Warp()
	if w.Craft == nil {
		return selected
	}
	if w.ActiveBurn != nil && selected > 10 {
		selected = 10
	}
	mu := w.Craft.Primary.GravitationalParameter()
	period := orbitalPeriod(w.Craft.State, mu)
	if math.IsInf(period, 0) || math.IsNaN(period) || period <= 0 {
		return selected
	}
	maxStep := period / 100.0
	maxSimDelta := 1024.0 * maxStep // seconds — our sub-step cap
	maxWarp := maxSimDelta / w.Clock.BaseStep.Seconds()
	if selected > maxWarp {
		return maxWarp
	}
	return selected
}

// EffectiveWarp exposes the clamped warp for HUD display. Returns the same
// as Clock.Warp() when the user isn't hitting the step-size guard.
func (w *World) EffectiveWarp() float64 { return w.clampedWarp() }

// integrateSpacecraft sub-steps the integrator so that each step dt obeys
// dt < period/100 (plan §Phase 1 numerical stability guard, hard-coded
// here; exposed as a configurable warp-clamp at C21). When ActiveBurn is
// in flight, sub-steps run RK4 with engine thrust on top of gravity so
// the non-conservative force is handled cleanly (Verlet would silently
// drift); otherwise pure Verlet for energy-conserving free flight.
//
// SOI check runs *inside* the sub-step loop (v0.4.2): when a sub-step
// crosses a sphere-of-influence boundary, the state is rebased to the
// new primary's frame and μ switches for subsequent steps. Mirrors
// propagateCraftWithPrimary's predictor path. Pre-v0.4.2 only the
// per-20-tick maybeSwitchPrimary throttle handled SOI transitions,
// which left the live integrator propagating in the wrong frame for
// up to a tick after a mid-tick crossing — visible as orbits diverging
// from the predicted trajectory at high warp.
//
// "Warp lock" (v0.4.3): when warp > 1× AND no active burn AND the
// orbit is bound with apoapsis comfortably inside the primary's SOI,
// take a single analytic Kepler step instead of looping Verlet. Verlet
// at coarse dt is symplectic but second-order — eccentricity does a
// random walk that turns 200×200 km circular orbits into 209×190 km
// after a few seconds at 10000× warp. KeplerStep is exact, so no drift.
func (w *World) integrateSpacecraft(simDelta time.Duration) {
	mu := w.Craft.Primary.GravitationalParameter()
	period := orbitalPeriod(w.Craft.State, mu)
	secs := simDelta.Seconds()

	// Warp-lock fast path: analytic Kepler propagation in chunks small
	// enough that the craft can't outrun any other body's SOI per
	// chunk (v0.4.4). Falls back to Verlet sub-stepping if the gate
	// rejects (active burn, hyperbolic, warp=1) or any chunk's
	// KeplerStep fails.
	if w.canKeplerStep(simDelta) {
		if w.keplerStepWithSOICheck(simDelta) {
			return
		}
	}

	maxStep := period / 100.0
	if maxStep <= 0 || math.IsNaN(maxStep) || math.IsInf(maxStep, 0) {
		maxStep = 1.0
	}
	nSteps := int(math.Ceil(secs / maxStep))
	if nSteps < 1 {
		nSteps = 1
	}
	// Cap sub-steps per tick so a warp spike can't grind the frame loop.
	// 1024 sub-steps per wall-tick at 20 Hz gives ≈ 20 kHz force evals.
	if nSteps > 1024 {
		nSteps = 1024
	}
	dt := secs / float64(nSteps)
	tickStart := w.Clock.SimTime.Add(-simDelta)

	sys := w.System()
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}

	for i := 0; i < nSteps; i++ {
		if w.burnActiveAt(tickStart, dt, i) {
			w.stepThrust(mu, dt)
		} else {
			w.Craft.State = physics.StepVerlet(w.Craft.State, mu, dt)
		}

		// Per-sub-step SOI re-evaluation. If the craft crossed into
		// another body's SOI during this dt, rebase to that frame so
		// the next sub-step uses the right μ.
		inertial := positions[w.Craft.Primary.ID].Add(w.Craft.State.R)
		cand := physics.FindPrimary(sys, inertial, positions)
		if cand.Body.ID != w.Craft.Primary.ID {
			vOld := w.bodyInertialVelocity(w.Craft.Primary)
			vNew := w.bodyInertialVelocity(cand.Body)
			w.Craft.State = physics.Rebase(w.Craft.State, positions[w.Craft.Primary.ID], cand.Inertial, vOld.Sub(vNew))
			w.Craft.Primary = cand.Body
			mu = w.Craft.Primary.GravitationalParameter()
		}
	}
	// Tear down the burn if it exhausted (Δv delivered, fuel gone, or
	// EndTime passed during this tick).
	if w.ActiveBurn != nil && w.burnExhausted() {
		w.ActiveBurn = nil
	}
}

// burnActiveAt reports whether sub-step i of the current tick should fire
// the engine: ActiveBurn must exist, the sub-step must start before
// EndTime, and DVRemaining + fuel must both be positive.
func (w *World) burnActiveAt(tickStart time.Time, dt float64, i int) bool {
	if w.ActiveBurn == nil {
		return false
	}
	if w.ActiveBurn.DVRemaining <= 0 || w.Craft.Fuel <= 0 {
		return false
	}
	subStart := tickStart.Add(time.Duration(float64(i) * dt * float64(time.Second)))
	return subStart.Before(w.ActiveBurn.EndTime)
}

// stepThrust advances one RK4 sub-step with engine thrust, debits the
// active-burn Δv budget by the analytical thrust contribution
// (Thrust/mass × dt), and burns fuel via the configured mass flow.
func (w *World) stepThrust(mu, dt float64) {
	accelFn := w.Craft.ThrustAccelFn(w.ActiveBurn.Mode, mu)
	w.Craft.State = physics.StepRK4(w.Craft.State, dt, accelFn, 0)

	mass := w.Craft.TotalMass()
	if mass > 0 {
		dvApplied := (w.Craft.Thrust / mass) * dt
		if dvApplied > w.ActiveBurn.DVRemaining {
			dvApplied = w.ActiveBurn.DVRemaining
		}
		w.ActiveBurn.DVRemaining -= dvApplied
	}
	fuelBurned := w.Craft.MassFlowRate() * dt
	if fuelBurned > w.Craft.Fuel {
		fuelBurned = w.Craft.Fuel
	}
	w.Craft.Fuel -= fuelBurned
	w.Craft.State.M = w.Craft.TotalMass()
}

// burnExhausted reports whether the active burn should be torn down: any
// of Δv delivered, fuel empty, or sim-time past the duration window
// terminates the burn.
func (w *World) burnExhausted() bool {
	return w.ActiveBurn.DVRemaining <= 0 ||
		w.Craft.Fuel <= 0 ||
		!w.Clock.SimTime.Before(w.ActiveBurn.EndTime)
}

// canKeplerStep reports whether the analytic warp-lock fast path is
// valid for this tick. Conditions (v0.4.4):
//   - warp > 1× (else Verlet is fine and we want to avoid behavioral
//     differences between paused/realtime and the live integrator)
//   - no active burn (analytic propagation can't accommodate thrust)
//   - bound orbit (e < 1) — KeplerStep itself rejects hyperbolic cases
//
// SOI containment is no longer gated here: keplerStepWithSOICheck
// chunks the analytic step finely enough to detect crossings between
// chunks (v0.4.4 fix for the v0.4.3 heliocentric-transfer-skips-Mars
// bug). If e ≥ 1 we still fall back to Verlet so the per-sub-step SOI
// path handles the non-conic case correctly.
func (w *World) canKeplerStep(simDelta time.Duration) bool {
	if w.ActiveBurn != nil {
		return false
	}
	if w.Clock.Warp() <= 1 {
		return false
	}
	mu := w.Craft.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(w.Craft.State.R, w.Craft.State.V, mu)
	if el.E >= 1 || el.A <= 0 {
		return false
	}
	return true
}

// keplerStepWithSOICheck propagates the craft analytically across the
// tick by chunking simDelta into pieces small enough that the craft
// can't outrun any non-current-primary body's SOI per chunk. Between
// chunks, FindPrimary catches SOI crossings and rebases the state.
//
// Chunk size = min(simDelta, smallestForeignSOI / (4·speed)). The
// factor of 4 leaves a 2× safety margin past the trivial "can't
// traverse SOI in one chunk" bound — a bound orbit re-encountering
// the same SOI region within a single tick would otherwise risk a
// missed crossing at high warp.
//
// Returns ok=false if any chunk's KeplerStep fails (e.g. eccentricity
// crossed into hyperbolic mid-propagation due to a primary switch);
// caller then falls back to Verlet for the remaining time.
func (w *World) keplerStepWithSOICheck(simDelta time.Duration) bool {
	sys := w.System()
	positions := make(map[string]orbital.Vec3, len(sys.Bodies))
	for _, b := range sys.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}

	chunkCap := chunkDtCap(sys, w.Craft.Primary, w.Craft.State.V.Norm())

	secs := simDelta.Seconds()
	if chunkCap <= 0 || math.IsInf(chunkCap, 0) || math.IsNaN(chunkCap) {
		chunkCap = secs
	}
	nChunks := int(math.Ceil(secs / chunkCap))
	if nChunks < 1 {
		nChunks = 1
	}
	// Safety cap matching the Verlet sub-step ceiling — a degenerate
	// near-zero chunk size shouldn't blow up the loop.
	if nChunks > 1024 {
		nChunks = 1024
	}
	chunk := secs / float64(nChunks)

	mu := w.Craft.Primary.GravitationalParameter()
	for i := 0; i < nChunks; i++ {
		newState, ok := physics.KeplerStep(w.Craft.State, mu, chunk)
		if !ok {
			return false
		}
		w.Craft.State = newState

		inertial := positions[w.Craft.Primary.ID].Add(w.Craft.State.R)
		cand := physics.FindPrimary(sys, inertial, positions)
		if cand.Body.ID != w.Craft.Primary.ID {
			vOld := w.bodyInertialVelocity(w.Craft.Primary)
			vNew := w.bodyInertialVelocity(cand.Body)
			w.Craft.State = physics.Rebase(w.Craft.State, positions[w.Craft.Primary.ID], cand.Inertial, vOld.Sub(vNew))
			w.Craft.Primary = cand.Body
			mu = w.Craft.Primary.GravitationalParameter()
		}
	}
	return true
}

// chunkDtCap returns the maximum analytic-step duration for the
// current craft primary, given craft speed. Bound by the smallest
// foreign body's SOI radius / (4·speed) so no SOI can be traversed
// without an intermediate FindPrimary check. +Inf when no foreign
// SOI exists (single-body system) — caller treats that as "one chunk
// covers the whole tick".
func chunkDtCap(sys bodies.System, currentPrimary bodies.CelestialBody, speed float64) float64 {
	if speed <= 0 {
		speed = 1.0
	}
	primaryID := sys.Bodies[0].ID
	cap := math.Inf(1)
	for _, b := range sys.Bodies {
		if b.ID == primaryID || b.ID == currentPrimary.ID {
			continue
		}
		soi := physics.SOIRadius(b, sys.Bodies[0])
		if soi <= 0 {
			continue
		}
		dt := soi / (4 * speed)
		if dt < cap {
			cap = dt
		}
	}
	return cap
}

// orbitalPeriod returns 2π√(a³/μ) or +Inf on unbound orbits. Used to
// size Verlet sub-steps.
func orbitalPeriod(s physics.StateVector, mu float64) float64 {
	a := physics.SemimajorAxis(s, mu)
	if a <= 0 || math.IsNaN(a) {
		return math.Inf(1)
	}
	return 2 * math.Pi * math.Sqrt(a*a*a/mu)
}

// maybeSwitchPrimary runs FindPrimary and, if a new body should now own
// the spacecraft, rebases its state vector. v0.1 spacecraft stays in Sol
// but can transition between Earth's SOI and heliocentric (e.g. after a
// Hohmann escape burn).
func (w *World) maybeSwitchPrimary() {
	sol := w.Systems[0]

	// Build body-position map in Sol-inertial.
	positions := make(map[string]orbital.Vec3, len(sol.Bodies))
	for _, b := range sol.Bodies {
		positions[b.ID] = w.BodyPosition(b)
	}

	// Craft inertial position needs the *current* primary offset.
	craftInertial := positions[w.Craft.Primary.ID].Add(w.Craft.State.R)

	newPrimary := physics.FindPrimary(sol, craftInertial, positions)
	if newPrimary.Body.ID == w.Craft.Primary.ID {
		return
	}

	// Compute relative velocity between old and new primary so Rebase
	// gets the velocity delta correct. Planet velocities come from
	// orbital.VelocityAtTrueAnomaly evaluated at current sim time.
	vOld := w.bodyInertialVelocity(w.Craft.Primary)
	vNew := w.bodyInertialVelocity(newPrimary.Body)
	dv := vOld.Sub(vNew)

	oldPos := positions[w.Craft.Primary.ID]
	w.Craft.State = physics.Rebase(w.Craft.State, oldPos, newPrimary.Inertial, dv)
	w.Craft.Primary = newPrimary.Body
}

func (w *World) bodyInertialVelocity(b bodies.CelestialBody) orbital.Vec3 {
	if b.SemimajorAxis == 0 {
		return orbital.Vec3{}
	}
	M := w.Calculator.CalculateMeanAnomaly(b, w.Clock.SimTime)
	E := orbital.SolveKepler(M, b.Eccentricity)
	nu := orbital.TrueAnomaly(E, b.Eccentricity)
	el := orbital.ElementsFromBody(b)
	// Velocity is relative to the SYSTEM primary (Sun). Use system primary's
	// GM — fetch from Sol.
	sunMu := w.Systems[0].Bodies[0].GravitationalParameter()
	return orbital.VelocityAtTrueAnomaly(el, nu, sunMu)
}
