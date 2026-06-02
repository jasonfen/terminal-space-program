// Package sim — v0.12 Slice 2 / ADR 0007 surface-staging tests. These
// pin the "land the 2-stage Lander, decouple the descent stage on the
// ground" arc and the structural guards that keep the shed descent
// stage and the parked ascent stage from re-fusing before liftoff.

package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// landMoonLander spawns a 2-stage Lander parked on the Moon at a fixed
// touchdown lat/lon and returns the active world + craft. The craft is
// Landed with LandedLatDeg/LonDeg set, co-rotating via integrateLanded.
func landMoonLander(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := w.Systems[0].FindBody("Moon")
	if moon == nil {
		t.Skip("Moon missing from Sol")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	c.Landed = true
	c.LandedLatDeg, c.LandedLonDeg = 10.0, 45.0
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	// Seed R/V from the landed coords so the pre-stage state is realistic.
	integrateLanded(w, c, time.Second)
	return w, c
}

// TestSurfaceStageLanderLeavesLandedDescent — the canonical surface-
// staging moment: a 2-stage Lander sitting on the Moon stages its
// descent stage. The jettisoned descent stage is a Landed passive
// craft pinned to the same touchdown lat/lon (not nudged onto a
// retrograde inertial offset, the orbital path); the active craft is
// the single-stage Ascent, still Landed until ignition.
func TestSurfaceStageLanderLeavesLandedDescent(t *testing.T) {
	w, lander := landMoonLander(t)
	if len(lander.Stages) != 2 {
		t.Fatalf("setup: Lander should be 2-stage, got %d", len(lander.Stages))
	}

	_, jettIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("surface StageActive: %v", err)
	}

	// Active craft = single-stage Ascent, still Landed.
	active := w.Crafts[0]
	if len(active.Stages) != 1 || active.Stages[0].Name != "Ascent" {
		t.Errorf("active after surface stage: %d-stage bottom %q, want 1× Ascent",
			len(active.Stages), active.Stages[0].Name)
	}
	if !active.Landed {
		t.Errorf("active Ascent should stay Landed until ignition, got Landed=false")
	}
	if !active.CanSoftLand {
		t.Errorf("active Ascent CanSoftLand=false, want true (ADR 0007 decision 5)")
	}

	// Jettisoned descent = Landed passive, NOT Crashed, pinned to the
	// parent's touchdown coords.
	jett := w.Crafts[jettIdx]
	if !jett.Landed {
		t.Errorf("jettisoned descent should be Landed (surface placement), got Landed=false")
	}
	if jett.Crashed {
		t.Errorf("jettisoned descent should be intact (not Crashed)")
	}
	if jett.Throttle != 0 {
		t.Errorf("jettisoned descent should be passive (Throttle=0), got %v", jett.Throttle)
	}
	if jett.LandedLatDeg != lander.LandedLatDeg || jett.LandedLonDeg != lander.LandedLonDeg {
		t.Errorf("jettisoned descent landed coords = (%.3f, %.3f), want parent's (%.3f, %.3f)",
			jett.LandedLatDeg, jett.LandedLonDeg, lander.LandedLatDeg, lander.LandedLonDeg)
	}
	if jett.Stages[0].Name != "Descent" {
		t.Errorf("jettisoned bottom stage = %q, want Descent", jett.Stages[0].Name)
	}
}

// TestSurfaceStagedPairDoesNotRefuse — the re-fuse regression (ADR
// 0007 decision 2). After a surface decouple, descent + ascent are
// co-located, both Landed, with identical V = ω×R — inside both
// docking gates. The both-Landed guard in checkDocking must keep them
// apart across multiple ticks. Once the ascent ignites (clears
// Landed), the guard no longer applies (but they're no longer both
// Landed, so still safe).
func TestSurfaceStagedPairDoesNotRefuse(t *testing.T) {
	w, _ := landMoonLander(t)
	if _, _, err := w.StageActive(0); err != nil {
		t.Fatalf("surface StageActive: %v", err)
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("post-stage slate: got %d craft, want 2", len(w.Crafts))
	}

	// Re-pin both via the landed integrator (they share the same coords
	// → co-located, identical V) then run the dock check several times.
	for tick := 0; tick < 5; tick++ {
		for _, c := range w.Crafts {
			if c.Landed {
				integrateLanded(w, c, time.Second)
			}
		}
		w.checkDocking()
		if len(w.Crafts) != 2 {
			t.Fatalf("tick %d: slate fused to %d craft — both-Landed dock guard failed",
				tick, len(w.Crafts))
		}
	}
}

// TestAscentLiftoffDoesNotRefuseDescent — the playtest regression
// ("undocking ascent from descent rejoins the vessels"). After a
// surface decouple, the player ignites the ascent stage to climb back
// to orbit. Engine ignition clears the ascent's Landed flag while it
// is STILL co-located with the parked descent stage (it hasn't moved
// yet). The dock guard must skip the pair because the descent is still
// Landed — a both-Landed-only guard would let them re-fuse at this
// exact moment. Simulate by clearing only the ascent's Landed flag and
// running the dock check at co-location.
func TestAscentLiftoffDoesNotRefuseDescent(t *testing.T) {
	w, _ := landMoonLander(t)
	_, jettIdx, err := w.StageActive(0)
	if err != nil {
		t.Fatalf("surface StageActive: %v", err)
	}
	ascent := w.Crafts[0]
	descent := w.Crafts[jettIdx]

	// Re-pin both to the same surface point so they're co-located with
	// matched co-rotation velocity (inside both docking gates).
	integrateLanded(w, ascent, time.Second)
	integrateLanded(w, descent, time.Second)

	// Ignition: the ascent leaves the surface, clearing Landed, but is
	// still at the descent's position for this tick.
	ascent.Landed = false

	if _, _, ok := w.checkDocking(); ok {
		t.Error("ascent re-fused with descent on liftoff — either-Landed dock guard failed")
	}
	if len(w.Crafts) != 2 {
		t.Errorf("slate count = %d, want 2 (no re-fuse on liftoff)", len(w.Crafts))
	}
}

// TestExtractedLMSurfaceStagesDescentAlone — the "inherit no plan"
// rule (ADR 0007 decision 5), now reached via the ADR 0009 transposition
// path. An LM released from the Apollo Stack (drop the 3 Saturn stages,
// transpose, undock) is a 2-stage [Descent, Ascent] craft with
// DecouplePlan=nil. When it later surface-stages, the nil plan means
// single-pop: it drops the Descent stage ALONE, leaving a single-stage
// Ascent — it must NOT re-group descent+ascent and empty itself.
func TestExtractedLMSurfaceStagesDescentAlone(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	moon := w.Systems[0].FindBody("Moon")
	if moon == nil {
		t.Skip("Moon missing from Sol")
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	// Drop S-IC, S-II, S-IVB (the [1,1,1] plan), then transpose + undock
	// to release the LM as its own 2-stage craft.
	for i := 0; i < 3; i++ {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("decouple #%d: %v", i, err)
		}
	}
	if err := w.Transpose(0); err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	if !w.Undock(0) {
		t.Fatal("Undock after transpose returned false")
	}
	var lmIdx int = -1
	for i, c := range w.Crafts {
		if len(c.Stages) == 2 && c.Stages[0].Name == "Descent" {
			lmIdx = i
		}
	}
	if lmIdx < 0 {
		t.Fatal("no released LM craft found after transpose + undock")
	}
	lm := w.Crafts[lmIdx]
	if len(lm.Stages) != 2 || lm.DecouplePlan != nil {
		t.Fatalf("extracted LM: %d stages, plan %v — want 2 stages, nil plan",
			len(lm.Stages), lm.DecouplePlan)
	}

	// Land the LM on the Moon and surface-stage it.
	lm.Primary = *moon
	lm.Landed = true
	lm.LandedLatDeg, lm.LandedLonDeg = -5.0, 120.0
	w.SetActiveCraftIdx(lmIdx)
	integrateLanded(w, lm, time.Second)

	_, descentIdx, err := w.StageActive(lmIdx)
	if err != nil {
		t.Fatalf("LM surface stage: %v", err)
	}
	// The LM (now active at lmIdx) must be a single-stage Ascent — NOT
	// emptied by re-grouping.
	if len(lm.Stages) != 1 || lm.Stages[0].Name != "Ascent" {
		t.Errorf("LM after surface stage: %d-stage bottom %q, want 1× Ascent",
			len(lm.Stages), lm.Stages[0].Name)
	}
	descent := w.Crafts[descentIdx]
	if len(descent.Stages) != 1 || descent.Stages[0].Name != "Descent" {
		t.Errorf("dropped descent: %d-stage bottom %q, want 1× Descent",
			len(descent.Stages), descent.Stages[0].Name)
	}
	if !descent.Landed {
		t.Errorf("dropped descent should be Landed (surface placement)")
	}
}

// TestDecouplePlanAdvancesOnEachPress — the plan is consumed
// positionally: after dropping S-IC, the v0.12 / ADR 0009 Apollo Stack's
// remaining plan is [1,1]; after S-II it's [1]; after S-IVB it's empty
// (the LM is no longer a bottom-up group — it releases via transposition
// + undock). Pins the advance so a save/reload mid-staging (which
// persists the remaining plan) restores the correct Saturn-drop grouping.
func TestDecouplePlanAdvancesOnEachPress(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	wantRemaining := [][]int{
		{1, 1}, // after S-IC
		{1},    // after S-II
		nil,    // after S-IVB (plan emptied; LM releases via transpose)
	}
	for i, want := range wantRemaining {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("press #%d: %v", i, err)
		}
		got := w.Crafts[0].DecouplePlan
		if len(got) != len(want) {
			t.Fatalf("after press #%d: plan %v, want %v", i, got, want)
		}
		for j := range want {
			if got[j] != want[j] {
				t.Errorf("after press #%d: plan[%d] = %d, want %d", i, j, got[j], want[j])
			}
		}
	}
}
