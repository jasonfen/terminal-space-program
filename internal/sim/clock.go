package sim

import "time"

// WarpFactors are the discrete time-warp steps per docs/plan.md §Time-warp UX.
var WarpFactors = []float64{1, 10, 100, 1000, 10000, 100000}

// Clock tracks sim-time advancement and the currently active warp factor.
type Clock struct {
	SimTime   time.Time
	WarpIdx   int
	Paused    bool
	BaseStep  time.Duration // real-time step per tick at warp 1×
}

// NewClock starts a clock at the J2000 epoch + 0 days, warp 1×.
func NewClock(start time.Time, baseStep time.Duration) *Clock {
	return &Clock{SimTime: start, WarpIdx: 0, BaseStep: baseStep}
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

// Advance moves SimTime forward by BaseStep × warp. Called once per tick.
func (c *Clock) Advance() {
	if c.Paused {
		return
	}
	simDelta := time.Duration(float64(c.BaseStep) * c.Warp())
	c.SimTime = c.SimTime.Add(simDelta)
}
