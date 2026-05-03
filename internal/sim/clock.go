package sim

import "time"

// WarpFactors are the discrete time-warp steps per docs/plan.md §Time-warp UX.
var WarpFactors = []float64{1, 10, 100, 1000, 10000, 100000}

// RotationCapWarp is the maximum effective warp factor for body
// rotation animation. Above this warp, RotationTime advances at
// the cap rate so planet surfaces don't blur into a stripe at
// extreme zoom-out warp. v0.8.5.7+ — implements the v0.8.5 plan's
// follow-up "policy (a): clamp rotation rate at high warp" slot.
//
// Picked at 10000× (one warp tier below max): at this rate Earth
// spins at ~42°/sec on screen, which is fast but trackable. Above
// it, sim time still advances at the user's selected warp; only
// the visible rotation is capped.
const RotationCapWarp = 10000.0

// Clock tracks sim-time advancement and the currently active warp factor.
type Clock struct {
	SimTime time.Time
	// RotationTime is sim time as seen by the rotation animation.
	// At warp ≤ RotationCapWarp it advances in lockstep with
	// SimTime; above, it advances at RotationCapWarp × BaseStep
	// per tick instead, so visible rotation stays smooth even when
	// SimTime is leaping forward at warp 100000×. Lags SimTime
	// while above the cap; the lag persists when warp drops back
	// (a side-effect of the "freeze rotation at high warp" model
	// the v0.8.5 plan called for).
	RotationTime time.Time
	WarpIdx      int
	Paused       bool
	BaseStep     time.Duration // real-time step per tick at warp 1×
}

// NewClock starts a clock at the J2000 epoch + 0 days, warp 1×.
func NewClock(start time.Time, baseStep time.Duration) *Clock {
	return &Clock{
		SimTime:      start,
		RotationTime: start,
		WarpIdx:      0,
		BaseStep:     baseStep,
	}
}

// Warp returns the current multiplier.
func (c *Clock) Warp() float64 {
	if c.Paused {
		return 0
	}
	return WarpFactors[c.WarpIdx]
}

// WarpUp steps up one warp level (saturates at max).
func (c *Clock) WarpUp() {
	if c.WarpIdx < len(WarpFactors)-1 {
		c.WarpIdx++
	}
}

// WarpDown steps down one level (saturates at 1×).
func (c *Clock) WarpDown() {
	if c.WarpIdx > 0 {
		c.WarpIdx--
	}
}

// TogglePause flips the paused state.
func (c *Clock) TogglePause() { c.Paused = !c.Paused }

// Advance moves SimTime forward by BaseStep × warp, and
// RotationTime forward by BaseStep × min(warp, RotationCapWarp)
// — capping the visible rotation rate so planets don't blur into
// solid stripes at extreme warp. Called once per tick.
func (c *Clock) Advance() {
	if c.Paused {
		return
	}
	w := c.Warp()
	simDelta := time.Duration(float64(c.BaseStep) * w)
	c.SimTime = c.SimTime.Add(simDelta)
	rotW := w
	if rotW > RotationCapWarp {
		rotW = RotationCapWarp
	}
	rotDelta := time.Duration(float64(c.BaseStep) * rotW)
	c.RotationTime = c.RotationTime.Add(rotDelta)
}
