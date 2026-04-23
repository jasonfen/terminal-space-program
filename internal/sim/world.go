package sim

import (
	"fmt"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// World holds the simulation state: all loaded systems, which one is active,
// the sim-clock, and (post-C14) the spacecraft.
type World struct {
	Systems    []bodies.System
	SystemIdx  int
	Calculator orbital.Calculator
	Clock      *Clock
}

// NewWorld loads the embedded systems and seeds the clock at J2000 with a
// 50 ms base step (20 Hz wall tick).
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
	return w, nil
}

// System returns the currently active system.
func (w *World) System() bodies.System { return w.Systems[w.SystemIdx] }

// CycleSystem advances to the next system (wraps). Recreates the calculator.
func (w *World) CycleSystem() {
	w.SystemIdx = (w.SystemIdx + 1) % len(w.Systems)
	w.Calculator = orbital.ForSystem(w.System(), w.Clock.SimTime)
}

// BodyPosition returns the inertial position (m) of a body at the current sim time.
// Primary (index 0) is anchored at the origin in v0.1.
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

// Tick advances sim-time one step.
func (w *World) Tick() { w.Clock.Advance() }
