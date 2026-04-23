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
func (w *World) CycleSystem() {
	w.SystemIdx = (w.SystemIdx + 1) % len(w.Systems)
	w.Calculator = orbital.ForSystem(w.System(), w.Clock.SimTime)
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
		w.soiCheckCounter++
		if w.soiCheckCounter >= 20 {
			w.soiCheckCounter = 0
			w.maybeSwitchPrimary()
		}
	}
}

// clampedWarp returns min(selected warp, max warp allowed by the step-size
// guard). max = (1024 sub-steps × period/100) / base_step.
func (w *World) clampedWarp() float64 {
	selected := w.Clock.Warp()
	if w.Craft == nil {
		return selected
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

// integrateSpacecraft sub-steps the Verlet integrator so that each
// step dt obeys dt < period/100 (plan §Phase 1 numerical stability guard,
// hard-coded here; exposed as a configurable warp-clamp at C21).
func (w *World) integrateSpacecraft(simDelta time.Duration) {
	mu := w.Craft.Primary.GravitationalParameter()
	period := orbitalPeriod(w.Craft.State, mu)
	secs := simDelta.Seconds()

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
	for i := 0; i < nSteps; i++ {
		w.Craft.State = physics.StepVerlet(w.Craft.State, mu, dt)
	}
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
