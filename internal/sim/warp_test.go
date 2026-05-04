package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestWarpClampRespectsOrbitalPeriod: at LEO (~5500 s period), max warp
// should be bounded by the 1024-sub-step cap. Plan §C21 guard.
func TestWarpClampRespectsOrbitalPeriod(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Force maximum warp (100000×).
	w.Clock.WarpIdx = len(WarpFactors) - 1
	selected := w.Clock.Warp()

	effective := w.EffectiveWarp()
	if effective > selected {
		t.Errorf("clamp increased warp (%.0f → %.0f)", selected, effective)
	}

	// LEO period ≈ 5543 s → max step 55.4 s → max simDelta 1024×55.4 ≈ 56743 s
	// → max warp 56743 / 0.05 ≈ 1.13M×. So 100000× should NOT be clamped.
	if effective != selected {
		t.Logf("100000× clamped to %.0fx for LEO orbit (unexpected but not wrong)", effective)
	}
}

// TestWarpClampActuallyClampsVeryShortPeriod: construct a degenerate
// scenario where the orbital period is ~1 s; the clamp must kick in.
func TestWarpClampActuallyClampsVeryShortPeriod(t *testing.T) {
	w, _ := NewWorld()
	// Shrink the craft's orbit to an absurdly tight radius — period ~1 s.
	// (Not physical; just forces the clamp path.) Fully overwrite R / V
	// rather than poking individual components — v0.8.6+ initialises
	// the default LEO state with non-zero Z (Earth's body-equatorial
	// frame is tilted in world coords).
	w.ActiveCraft().State.R = orbital.Vec3{X: 1e3}
	w.ActiveCraft().State.V = orbital.Vec3{Y: math.Sqrt(w.ActiveCraft().Primary.GravitationalParameter() / 1e3)}
	w.Clock.WarpIdx = len(WarpFactors) - 1

	selected := w.Clock.Warp()
	effective := w.EffectiveWarp()
	if effective >= selected {
		t.Errorf("expected clamp to reduce warp; got %.0f → %.0f", selected, effective)
	}
}

// TestWarpCappedAt10xAfterThrottleChange: changing throttle at high
// warp ramps thrust faster than the integrator can absorb, the same
// aliasing path the burn-active 10× cap exists for. The throttle-
// change cap must (a) fire while the change is fresh and (b) release
// once the window has elapsed.
func TestWarpCappedAt10xAfterThrottleChange(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1 // 100000×
	// Baseline: no recent throttle change → warp not clamped by us
	// (might still be clamped by orbit-period guard, but the LEO
	// period guard at this state allows ~1.1M× — see test above).
	pre := w.EffectiveWarp()
	if pre <= 10 {
		t.Skipf("baseline warp already ≤10× from another guard (got %.0f); throttle test inconclusive", pre)
	}

	// Trigger a throttle change. SetThrottle records SimTime.
	w.SetThrottle(0.5)
	if eff := w.EffectiveWarp(); eff != 10 {
		t.Errorf("throttle change at %.0f× should clamp to 10×, got %.0f", pre, eff)
	}

	// Advance sim time past the clamp window — clamp should release.
	w.Clock.SimTime = w.Clock.SimTime.Add(throttleClampWindow + time.Second)
	if eff := w.EffectiveWarp(); eff != pre {
		t.Errorf("after window expiry expected %.0f×, got %.0f", pre, eff)
	}
}

// TestSetThrottleNoChangeNoTimestamp: SetThrottle with the same value
// must not refresh the timestamp — otherwise repeated SetThrottle(1.0)
// calls (e.g. from input rebroadcast) would extend the clamp forever.
func TestSetThrottleNoChangeNoTimestamp(t *testing.T) {
	w, _ := NewWorld()
	c := w.ActiveCraft()
	c.Throttle = 0.5
	c.LastThrottleChangeAt = time.Time{} // reset
	w.SetThrottle(0.5)                   // no change
	if !c.LastThrottleChangeAt.IsZero() {
		t.Errorf("SetThrottle(same value) updated timestamp to %v, want zero", c.LastThrottleChangeAt)
	}
	w.SetThrottle(0.7) // real change
	if c.LastThrottleChangeAt.IsZero() {
		t.Errorf("SetThrottle(new value) didn't update timestamp")
	}
}

// TestWarpClampUpcomingNodeRampsDown: with a planted node 30 sim-
// seconds in the future at 100000× selected warp, the integrator
// would skip the node in a single tick (one tick at 100000× = 5000 s).
// The approach clamp must catch this and reduce warp.
func TestWarpClampUpcomingNodeRampsDown(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1 // 100000×
	pre := w.EffectiveWarp()
	if pre <= 10 {
		t.Skipf("baseline already ≤10× from another guard (got %.0f); test inconclusive", pre)
	}
	// Plant a node 30 sim-seconds in the future.
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(30 * time.Second),
		Mode:        spacecraft.BurnPrograde,
		DV:          10,
	})
	eff := w.EffectiveWarp()
	if eff >= pre {
		t.Errorf("upcoming node 30 s out should reduce warp from %.0f, got %.0f", pre, eff)
	}
	// Formula: maxWarp = 30 / (10 × 0.05) = 60. Allow ±5% slack.
	want := 60.0
	if eff < want*0.95 || eff > want*1.05 {
		t.Errorf("approach clamp at 30 s: got %.2f, want ~%.2f", eff, want)
	}
}

// TestWarpClampUpcomingNodeFloorsAt1x: a node firing essentially
// immediately must not clamp warp below 1× (real-time).
func TestWarpClampUpcomingNodeFloorsAt1x(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(50 * time.Millisecond),
		Mode:        spacecraft.BurnPrograde,
		DV:          10,
	})
	if eff := w.EffectiveWarp(); eff < 1 {
		t.Errorf("approach clamp must floor at 1×, got %.4f", eff)
	}
}

// TestWarpClampUpcomingNodeReleasesAfterPast: once the node's
// TriggerTime is in the past (e.g. fired or skipped), the approach
// clamp must release. Otherwise stale nodes would persistently
// dampen warp.
func TestWarpClampUpcomingNodeReleasesAfterPast(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1
	pre := w.EffectiveWarp()
	w.PlanNode(ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(-1 * time.Hour), // already past
		Mode:        spacecraft.BurnPrograde,
		DV:          10,
	})
	if eff := w.EffectiveWarp(); eff != pre {
		t.Errorf("past node shouldn't clamp warp; got %.0f, want %.0f", eff, pre)
	}
}

// TestWarpCappedAt10xDuringActiveBurn: even at 100000× selected, an
// in-flight finite burn must clamp to 10× per docs/plan.md §Time-warp UX.
// Otherwise the integrator would skip past EndTime in a single tick and
// the burn would lose all temporal resolution.
func TestWarpCappedAt10xDuringActiveBurn(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1 // 100000×
	w.ActiveCraft().ActiveBurn = &ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * 1e9)}

	if eff := w.EffectiveWarp(); eff != 10 {
		t.Errorf("active burn should cap warp to 10×, got %.0f", eff)
	}

	// And below the cap — selecting 1× during a burn stays at 1×.
	w.Clock.WarpIdx = 0
	if eff := w.EffectiveWarp(); eff != 1 {
		t.Errorf("1× during burn should stay 1×, got %.0f", eff)
	}
}
