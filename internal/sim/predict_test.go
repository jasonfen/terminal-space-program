package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// TestPredictedSegmentsContinuousAtSOIBoundary: plant a hyperbolic
// trajectory escaping Earth SOI; PredictedSegments should split into
// (≥) two segments at the boundary, AND the last point of the inner
// segment should match (in inertial coordinates) the first point of the
// outer segment within a small tolerance. Pre-v0.3.0 the predictor
// integrated with Earth's μ throughout, so the post-escape segment was
// geometrically wrong but the JOIN was at least continuous because
// segments shared the same coordinates. v0.3.0 rebases on crossing —
// the join must still land continuously after the rebase.
func TestPredictedSegmentsContinuousAtSOIBoundary(t *testing.T) {
	w := mustWorld(t)

	// Boost velocity well past Earth escape (|v_circ| ≈ 7.78 km/s,
	// |v_esc| ≈ 11.0 km/s). 16 km/s gives v∞ ≈ 10 km/s — past Earth
	// SOI (~924 000 km) within ~1 day with margin to spare.
	post := w.Craft.State
	post.V = orbital.Vec3{Y: 16000}

	const totalSecs = 3 * 86400.0 // 3 days
	const samples = 600
	segs := w.PredictedSegments(post, totalSecs, samples)

	if len(segs) < 2 {
		t.Fatalf("expected ≥2 SOI segments after escape, got %d (no SOI crossing detected)", len(segs))
	}

	// Find the first inter-segment join and assert continuity in inertial.
	for i := 0; i+1 < len(segs); i++ {
		if len(segs[i].Points) == 0 || len(segs[i+1].Points) == 0 {
			t.Fatalf("segment %d or %d has zero points", i, i+1)
		}
		end := segs[i].Points[len(segs[i].Points)-1]
		start := segs[i+1].Points[0]
		gap := end.Sub(start).Norm()
		// Earth SOI ≈ 924,000 km; a discontinuity of more than 1000 km
		// would indicate the rebase math dropped the relative-position
		// bookkeeping. 100 km buffer accounts for one Verlet sub-step
		// of motion at the boundary (typically << 1 km, but we want
		// generous slack).
		if gap > 100e3 {
			t.Errorf("segment %d→%d join discontinuity: %.1f km (primary %s → %s)",
				i, i+1, gap/1000, segs[i].PrimaryID, segs[i+1].PrimaryID)
		}
	}
}

// TestPredictedSegmentsBoundOrbitStaysInOneSegment: an unmodified LEO
// orbit propagated for one full period must stay in a single segment
// labeled with Earth's ID. Catches a regression where the SOI check
// false-positively rebases inside the home SOI.
func TestPredictedSegmentsBoundOrbitStaysInOneSegment(t *testing.T) {
	w := mustWorld(t)
	post := w.Craft.State

	mu := w.Craft.Primary.GravitationalParameter()
	period := 2 * math.Pi * math.Sqrt(math.Pow(post.R.Norm(), 3)/mu)
	segs := w.PredictedSegments(post, period, 128)

	if len(segs) != 1 {
		ids := make([]string, len(segs))
		for i, s := range segs {
			ids[i] = s.PrimaryID
		}
		t.Errorf("LEO orbit produced %d segments (%v); want 1", len(segs), ids)
	}
	if len(segs) > 0 && segs[0].PrimaryID != w.Craft.Primary.ID {
		t.Errorf("LEO segment primary = %s, want %s", segs[0].PrimaryID, w.Craft.Primary.ID)
	}
}

// TestIntegrateSpacecraftSwitchesPrimaryMidTick: regression for v0.4.2.
// At high warp a single tick can cover an SOI crossing; the live
// integrator must rebase to the new primary inside its sub-step loop,
// not wait for maybeSwitchPrimary's per-20-tick throttle. Otherwise
// post-crossing sub-steps integrate with the wrong μ and the live
// orbit drifts off the predicted one.
func TestIntegrateSpacecraftSwitchesPrimaryMidTick(t *testing.T) {
	w := mustWorld(t)
	homeID := w.Craft.Primary.ID

	// 16 km/s y-velocity → hyperbolic Earth escape; ~3 days clears
	// Earth SOI (~924 000 km) with margin.
	w.Craft.State.V = orbital.Vec3{Y: 16000}

	// Single tick covering 3 days of sim time. integrateSpacecraft
	// caps sub-steps at 1024 and dt at period/100, but per-sub-step
	// SOI check should still fire when the boundary is crossed
	// regardless of how the dt is sized.
	w.integrateSpacecraft(time.Duration(3 * 86400 * float64(time.Second)))

	if w.Craft.Primary.ID == homeID {
		t.Errorf("live integrator stayed in home primary %q after 3-day escape; SOI check did not fire mid-tick",
			homeID)
	}
	// State should now be on a heliocentric scale, not 8e8 m geocentric.
	if w.Craft.State.R.Norm() < 1e9 {
		t.Errorf("post-tick |r|=%.3e m — looks like state wasn't rebased", w.Craft.State.R.Norm())
	}
}

// TestIntegrateSpacecraftMatchesPredictorAcrossSOI: at high warp the
// live integrator's end state should match the predictor's end state
// (same Verlet sub-stepping, same SOI boundary handling). Pre-v0.4.2
// the predictor's per-sub-step rebase didn't have a counterpart in
// integrateSpacecraft, so the two could diverge by tens of thousands
// of km after a mid-tick SOI crossing. The fix folds the same rebase
// logic into the live integrator; their post-crossing states should
// now match within a Verlet step's worth of motion.
func TestIntegrateSpacecraftMatchesPredictorAcrossSOI(t *testing.T) {
	w := mustWorld(t)
	w.Craft.State.V = orbital.Vec3{Y: 16000}

	// Snapshot starting state, run the predictor on it.
	startState := w.Craft.State
	predicted := w.propagateCraft(3 * 86400.0)

	// Reset craft, run live integrator over the same dt. Don't advance
	// Clock.SimTime — that would shift the body-position epoch and
	// the predictor / live snapshots wouldn't be comparable. (In
	// production Tick *does* advance SimTime first; the body-snapshot
	// drift is a known approximation orthogonal to this test.)
	w.Craft.State = startState
	w.integrateSpacecraft(time.Duration(3 * 86400 * float64(time.Second)))

	gap := w.Craft.State.R.Sub(predicted.R).Norm()
	// The two paths share Verlet step + SOI-rebase math; allow 1e6 m
	// (1000 km) for accumulated single-precision noise across 1024
	// sub-steps. Pre-v0.4.2 the gap was 10⁷–10⁸ m (post-crossing wrong-
	// frame integration) so even a generous bound catches the bug.
	if gap > 1e6 {
		t.Errorf("live vs predicted divergence after SOI crossing: %.3e m (>1e6)", gap)
	}
}

// TestPropagateCraftSOIAware: forward-integrate a hyperbolic escape via
// propagateCraft and confirm the resulting state isn't expressed in the
// original primary's frame anymore (i.e. |r| would have to be absurdly
// large if it were, but it should be reasonable in the new frame). This
// catches the case where propagateCraft forgot to rebase and returned
// a state vector still tied to Earth's center even after crossing Sol.
func TestPropagateCraftSOIAware(t *testing.T) {
	w := mustWorld(t)
	w.Craft.State.V = orbital.Vec3{Y: 16000}

	state := w.propagateCraft(3 * 86400.0)

	// Sanity: post-rebase r should be on a heliocentric scale (~1 AU, 1.5e11 m)
	// or planet-relative scale, never the geocentric escape distance which
	// would be ~v∞ × t ≈ 5km/s × 2d × 86400 ≈ 8.6e8 m if frame wasn't switched.
	// In the heliocentric frame after rebase, r should equal ≈ AU plus the
	// post-escape Earth-relative offset, so r > 1e11 m is the indicator.
	if state.R.Norm() < 1e10 {
		t.Errorf("propagateCraft after escape: |r|=%.3e m — looks like still in Earth frame", state.R.Norm())
	}
	// And shouldn't be NaN or stupidly large.
	if math.IsNaN(state.R.Norm()) || state.R.Norm() > 1e13 {
		t.Errorf("propagateCraft: unphysical |r|=%.3e m", state.R.Norm())
	}
	_ = physics.SemimajorAxis // reuse import
}
