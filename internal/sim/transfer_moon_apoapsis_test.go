package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestHToMoonProjectedOrbitsAreWhatTheyClaim (triage T1, 2026-10-12): from the
// default LEO start, `H` to the Moon planted three nodes and the NAVIGATION
// arrows read "Ap 500.0 km → 4907 km". The first burn must leave an Earth
// orbit reaching the Moon's distance (the number a player expects), and the
// 4907 km is the FINAL orbit, a circular-ish lunar capture orbit measured
// above the Moon's surface (PredictedFinalOrbit's primary is the Moon).
// The 4907 km itself is NOT pinned: the planner's Capture Orbit is 200 km, so
// the number is suspect (filed separately); this test pins what is not.
func TestHToMoonProjectedOrbitsAreWhatTheyClaim(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	// The real default start (tui.New): a few hours before the next Moon window.
	w.AdjustStartForLunarTransferWindow(DefaultLunarTransferLead)
	moonIdx := bodyIdxForTest(t, w, "moon")
	w.SetTargetBody(moonIdx)
	if _, err := w.PlanTransfer(moonIdx); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	c := w.ActiveCraft()
	if len(c.Nodes) < 2 {
		t.Fatalf("H planted %d nodes, want a TLI plus an arrival burn", len(c.Nodes))
	}

	for i, n := range c.Nodes {
		t.Logf("node %d: %v dv=%.0f in %v prim=%q", i, n.Mode, n.DV, n.TriggerTime.Sub(w.Clock.SimTime), n.PrimaryID)
	}
	// After the first burn: still an Earth orbit, apoapsis near the Moon's distance.
	post, _ := w.PostBurnState(c.Nodes[0])
	el := orbital.ElementsFromState(post.R, post.V, c.Primary.GravitationalParameter())
	apoKm := el.Apoapsis() / 1000
	if el.E >= 1 || apoKm < 330e3 || apoKm > 450e3 {
		t.Errorf("post-TLI apoapsis radius = %.0f km (e=%.3f), want ~384,000 km", apoKm, el.E)
	}

	// The projected FINAL orbit is lunar: primary Moon, Ap/Pe altitudes above the Moon.
	st, primary, ok := w.PredictedFinalOrbit()
	if !ok || primary.ID != "moon" {
		t.Fatalf("PredictedFinalOrbit primary = %q ok=%v, want moon", primary.ID, ok)
	}
	fel := orbital.ElementsFromStateInFrame(st.R, st.V, primary.GravitationalParameter(), orbital.ReferenceFrameForPrimary(primary))
	altKm := (fel.Apoapsis() - primary.RadiusMeters()) / 1000
	t.Logf("TLI apoapsis radius %.0f km; final lunar orbit Ap alt %.0f km, Pe alt %.0f km, e=%.3f",
		apoKm, altKm, (fel.Periapsis()-primary.RadiusMeters())/1000, fel.E)
	moon := primary
	t.Logf("final state radius from Moon centre = %.0f km (Moon R %.0f km), speed %.0f m/s (circular there %.0f)",
		st.R.Norm()/1000, moon.RadiusMeters()/1000, st.V.Norm(), math.Sqrt(moon.GravitationalParameter()/st.R.Norm()))
	if math.IsNaN(altKm) || fel.E > 0.5 {
		t.Errorf("final orbit Ap alt %.0f km e=%.3f is not a closed lunar orbit (altitude is logged, not pinned: see the issue filed for the default start planning a ~4,900 km capture orbit)", altKm, fel.E)
	}
}
