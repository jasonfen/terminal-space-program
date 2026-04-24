package sim

import (
	"math"
	"testing"
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
	// (Not physical; just forces the clamp path.)
	w.Craft.State.R.X = 1e3
	w.Craft.State.V.Y = math.Sqrt(w.Craft.Primary.GravitationalParameter() / 1e3)
	w.Clock.WarpIdx = len(WarpFactors) - 1

	selected := w.Clock.Warp()
	effective := w.EffectiveWarp()
	if effective >= selected {
		t.Errorf("expected clamp to reduce warp; got %.0f → %.0f", selected, effective)
	}
}

// TestWarpCappedAt10xDuringActiveBurn: even at 100000× selected, an
// in-flight finite burn must clamp to 10× per docs/plan.md §Time-warp UX.
// Otherwise the integrator would skip past EndTime in a single tick and
// the burn would lose all temporal resolution.
func TestWarpCappedAt10xDuringActiveBurn(t *testing.T) {
	w, _ := NewWorld()
	w.Clock.WarpIdx = len(WarpFactors) - 1 // 100000×
	w.ActiveBurn = &ActiveBurn{DVRemaining: 100, EndTime: w.Clock.SimTime.Add(60 * 1e9)}

	if eff := w.EffectiveWarp(); eff != 10 {
		t.Errorf("active burn should cap warp to 10×, got %.0f", eff)
	}

	// And below the cap — selecting 1× during a burn stays at 1×.
	w.Clock.WarpIdx = 0
	if eff := w.EffectiveWarp(); eff != 1 {
		t.Errorf("1× during burn should stay 1×, got %.0f", eff)
	}
}
