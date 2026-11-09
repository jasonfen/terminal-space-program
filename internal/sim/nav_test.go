package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestResolveAttitudeIntentOrbit — NavOrbit (the default) maps the six
// SAS-axis intents 1:1 to the v0.7.3+ orbit-frame burn modes.
func TestResolveAttitudeIntentOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	cases := []struct {
		intent AttitudeIntent
		want   spacecraft.BurnMode
	}{
		{IntentPrograde, spacecraft.BurnPrograde},
		{IntentRetrograde, spacecraft.BurnRetrograde},
		{IntentNormalPlus, spacecraft.BurnNormalPlus},
		{IntentNormalMinus, spacecraft.BurnNormalMinus},
		{IntentRadialOut, spacecraft.BurnRadialOut},
		{IntentRadialIn, spacecraft.BurnRadialIn},
	}
	for _, tc := range cases {
		if got := w.ResolveAttitudeIntent(tc.intent); got != tc.want {
			t.Errorf("orbit intent %v: got %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// TestResolveAttitudeIntentSurface — NavSurface only redefines
// prograde / retrograde; the other axes still resolve to their
// orbital meanings (KSP behaviour: the surface navball still shows
// orbital normal/radial markers).
func TestResolveAttitudeIntentSurface(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavSurface
	cases := []struct {
		intent AttitudeIntent
		want   spacecraft.BurnMode
	}{
		{IntentPrograde, spacecraft.BurnSurfacePrograde},
		{IntentRetrograde, spacecraft.BurnSurfaceRetrograde},
		{IntentNormalPlus, spacecraft.BurnNormalPlus},
		{IntentNormalMinus, spacecraft.BurnNormalMinus},
		{IntentRadialOut, spacecraft.BurnRadialOut},
		{IntentRadialIn, spacecraft.BurnRadialIn},
	}
	for _, tc := range cases {
		if got := w.ResolveAttitudeIntent(tc.intent); got != tc.want {
			t.Errorf("surface intent %v: got %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// TestResolveAttitudeIntentTarget — NavTarget rebinds prograde /
// retrograde to relative-velocity and radial± to BurnTarget /
// BurnAntiTarget (toward / away). Normal axes stay orbital because
// there is no target-relative normal SAS mode.
func TestResolveAttitudeIntentTarget(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	// SpawnSisterCraft promotes the new craft to active (idx 1); the
	// original LEO craft is now at idx 0 and is the targetable sibling.
	w.SetTargetCraft(0)
	if w.Target.Kind != TargetCraft {
		t.Fatalf("setup: target kind = %v, want TargetCraft", w.Target.Kind)
	}
	w.NavMode = NavTarget
	cases := []struct {
		intent AttitudeIntent
		want   spacecraft.BurnMode
	}{
		{IntentPrograde, spacecraft.BurnTargetPrograde},
		{IntentRetrograde, spacecraft.BurnTargetRetrograde},
		{IntentRadialOut, spacecraft.BurnTarget},
		{IntentRadialIn, spacecraft.BurnAntiTarget},
		{IntentNormalPlus, spacecraft.BurnNormalPlus},
		{IntentNormalMinus, spacecraft.BurnNormalMinus},
	}
	for _, tc := range cases {
		if got := w.ResolveAttitudeIntent(tc.intent); got != tc.want {
			t.Errorf("target intent %v: got %v, want %v", tc.intent, got, tc.want)
		}
	}
}

// TestResolveAttitudeIntentTargetFallback — NavTarget without a craft
// target silently degrades to NavOrbit so a stale mode doesn't yield
// a zero-direction SAS hold.
func TestResolveAttitudeIntentTargetFallback(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavTarget
	// No craft target bound. ResolveAttitudeIntent must fall back to
	// orbit-frame.
	if got := w.ResolveAttitudeIntent(IntentPrograde); got != spacecraft.BurnPrograde {
		t.Errorf("target+notarget prograde: got %v, want BurnPrograde", got)
	}
	if got := w.ResolveAttitudeIntent(IntentRadialOut); got != spacecraft.BurnRadialOut {
		t.Errorf("target+notarget radial+: got %v, want BurnRadialOut", got)
	}
}

// TestCycleNavModeSkipsTargetWithoutCraftTarget: the cycle order is
// Surface → Orbit → Target → Surface (Jason, 2026-10-08). Without a vessel
// target, Target is skipped, so `;` toggles Orbit ⇄ Surface and the player
// never lands on a mode that silently degrades.
func TestCycleNavModeSkipsTargetWithoutCraftTarget(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if w.NavMode != NavOrbit {
		t.Fatalf("default nav: got %v, want NavOrbit", w.NavMode)
	}
	if got := w.CycleNavMode(); got != NavSurface {
		t.Errorf("cycle 1 from Orbit (skips Target, none bound): got %v, want NavSurface", got)
	}
	if got := w.CycleNavMode(); got != NavOrbit {
		t.Errorf("cycle 2 from Surface: got %v, want NavOrbit", got)
	}
}

// TestCycleNavModeIncludesTargetWhenCraftTargetBound: with a vessel
// targeted the cycle visits all three in the order Surface → Orbit →
// Target → Surface (Jason, 2026-10-08; was Orbit → Surface → Target).
func TestCycleNavModeIncludesTargetWhenCraftTargetBound(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	w.SetTargetCraft(0)
	w.NavMode = NavSurface
	if got := w.CycleNavMode(); got != NavOrbit {
		t.Errorf("from Surface: got %v, want NavOrbit", got)
	}
	if got := w.CycleNavMode(); got != NavTarget {
		t.Errorf("from Orbit: got %v, want NavTarget", got)
	}
	if got := w.CycleNavMode(); got != NavSurface {
		t.Errorf("from Target: got %v, want NavSurface", got)
	}
}

// TestClearTargetSnapsNavTargetToOrbit — dropping the target while
// NavMode is NavTarget reconciles back to NavOrbit so the HUD doesn't
// claim a frame it can no longer resolve.
func TestClearTargetSnapsNavTargetToOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	w.SetTargetCraft(0)
	w.NavMode = NavTarget
	w.ClearTarget()
	if w.NavMode != NavOrbit {
		t.Errorf("after ClearTarget: nav = %v, want NavOrbit", w.NavMode)
	}
}

// TestSetTargetBodySnapsNavTargetToOrbit — swapping from a craft
// target to a body target also takes nav out of NavTarget (KSP target
// mode is craft-only in our model).
func TestSetTargetBodySnapsNavTargetToOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatalf("SpawnSisterCraft: %v", err)
	}
	w.SetTargetCraft(0)
	w.NavMode = NavTarget
	w.SetTargetBody(1) // any non-root body
	if w.NavMode != NavOrbit {
		t.Errorf("after SetTargetBody: nav = %v, want NavOrbit", w.NavMode)
	}
}

// TestAttitudeHoldPartsNamesFrame: the frame tags the HELD MODE, not the
// live NavMode, and NavTarget without a relative target falls back to orbit.
func TestAttitudeHoldPartsNamesFrame(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	cases := []struct {
		nav       NavMode
		mode      spacecraft.BurnMode
		wantLabel string
		wantFrame NavMode
	}{
		{NavOrbit, spacecraft.BurnSurfaceRetrograde, "Retrograde", NavSurface},
		{NavOrbit, spacecraft.BurnPortIn, spacecraft.BurnPortIn.String(), NavPort},
		{NavSurface, spacecraft.BurnPortIn, spacecraft.BurnPortIn.String(), NavPort},
		{NavTarget, spacecraft.BurnPrograde, "Prograde", NavOrbit}, // no relative target: fallback
		{NavOrbit, spacecraft.BurnPrograde, "Prograde", NavOrbit},
		{NavOrbit, spacecraft.BurnInertial, "Inertial", NavOrbit}, // ADR 0063: tagged ORBIT
		{NavPort, spacecraft.BurnInertial, "Inertial", NavOrbit},  // the mode's frame, not the live NavMode
		{NavOrbit, spacecraft.BurnSite, "Site+", NavSite},         // ADR 0067: frame-locked to the site
		{NavOrbit, spacecraft.BurnSiteOut, "Site\u2212", NavSite}, // P10: e in SITE
	}
	for _, tc := range cases {
		w.NavMode = tc.nav
		label, frame := w.AttitudeHoldParts(tc.mode)
		if label != tc.wantLabel || frame != tc.wantFrame {
			t.Errorf("nav %v mode %v: got (%q, %v), want (%q, %v)", tc.nav, tc.mode, label, frame, tc.wantLabel, tc.wantFrame)
		}
	}
}

// ADR 0067 decision 5: SITE is the fifth frame. `;` offers it only with a
// surface target aimed and the vessel off the ground, `q` there is Site+, and
// it drops to ORBIT when the target goes (the way PORT drops).
func TestNavSiteOfferedResolvedAndDropped(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0) // a flying Lander with a flag aimed
	w.NavMode = NavPort
	if got := w.CycleNavMode(); got != NavSite {
		t.Fatalf("; from PORT with a site aimed = %v, want SITE", got)
	}
	if got := w.ResolveAttitudeIntent(IntentRadialOut); got != spacecraft.BurnSite {
		t.Errorf("q in SITE = %v, want Site+", got)
	}
	if got := w.ResolveAttitudeIntent(IntentRadialIn); got != spacecraft.BurnSiteOut {
		t.Errorf("e in SITE = %v, want Site- (P10), not orbit Radial-", got)
	}
	if got := w.NavMode.Tag(); got != "SITE" {
		t.Errorf("tag = %q, want SITE", got)
	}
	if got := w.CycleNavMode(); got != NavSurface {
		t.Errorf("; from SITE = %v, want SURF", got)
	}
	w.NavMode = NavSite
	w.ClearTarget() // the target goes: SITE drops
	if w.NavMode != NavOrbit {
		t.Errorf("after the target cleared: nav = %v, want ORBIT", w.NavMode)
	}
	// no surface target: the cycle skips SITE and q is plain radial
	w.NavMode = NavPort
	if got := w.CycleNavMode(); got == NavSite {
		t.Error("; offered SITE with no surface target")
	}
	w.NavMode = NavSite
	if got := w.ResolveAttitudeIntent(IntentRadialIn); got == spacecraft.BurnSiteOut {
		t.Error("e resolved to Site- with no surface target")
	}
	if got := w.ResolveAttitudeIntent(IntentRadialOut); got == spacecraft.BurnSite {
		t.Error("q resolved to Site+ with no surface target")
	}
	// on the ground it is not offered either
	w.SetTargetSite(w.Sites[0].ID)
	c.Landed = true
	w.NavMode = NavPort
	if got := w.CycleNavMode(); got == NavSite {
		t.Error("; offered SITE to a landed vessel")
	}
}

// ADR 0067 playtest P2: the coast phase has its own hold word, so an engaged
// Site+ never reads as a bare hold; disengaged it is plain Site+.
func TestSiteHoldLabelNamesEveryPhase(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.Landing = &spacecraft.LandingAutopilot{}
	if got := w.siteHoldLabel(w.ActiveCraft()); got != "Site+" {
		t.Errorf("disengaged = %q, want Site+", got)
	}
	c.Landing.Engaged = true
	for _, nose := range []bool{false, true} {
		c.Landing.Nose = nose
		tag := " auto"
		if nose {
			tag = ""
		}
		for p, word := range map[spacecraft.LandingPhase]string{
			spacecraft.PhaseCoast: "wait", spacecraft.PhaseBrake: "brake",
			spacecraft.PhaseApproach: "approach", spacecraft.PhaseFinal: "final",
		} {
			c.Landing.Phase = p
			if want := "Site+" + tag + " " + word; w.siteHoldLabel(w.ActiveCraft()) != want {
				t.Errorf("nose=%v phase %v = %q, want %q", nose, p, w.siteHoldLabel(w.ActiveCraft()), want)
			}
		}
	}
}

// twoVessels returns a world with two crafts; craft 0 is active.
func twoVessels(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnSisterCraft(); err != nil {
		t.Fatal(err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("setup: %d crafts", len(w.Crafts))
	}
	w.SetActiveCraftIdx(0)
	w.NavMode = NavOrbit
	w.Crafts[1].NavFrame = int(NavOrbit)
	return w
}

// ADR 0067 P22: the frame travels with the vessel. SURFACE on A survives a
// trip to B (ORBIT) and back, and `;` on B never changes A.
func TestNavFrameIsPerVessel(t *testing.T) {
	w := twoVessels(t)
	w.NavMode = NavSurface // A
	w.SetActiveCraftIdx(1)
	if w.NavMode != NavOrbit {
		t.Fatalf("B arrives on %v, want ORBIT (A's frame leaked)", w.NavMode)
	}
	w.CycleNavMode() // `;` on B
	bFrame := w.NavMode
	w.SetActiveCraftIdx(0)
	if w.NavMode != NavSurface {
		t.Errorf("A reads %v after the round trip, want SURFACE", w.NavMode)
	}
	w.SetActiveCraftIdx(1)
	if w.NavMode != bFrame {
		t.Errorf("B reads %v, want its own %v", w.NavMode, bFrame)
	}
}

// A stored frame the vessel cannot offer drops to ORBIT on arrival, and the
// equal case (a frame it can offer) is kept.
func TestNavFrameUnofferedDropsOnSwitch(t *testing.T) {
	w := twoVessels(t)
	for _, f := range []NavMode{NavTarget, NavPort, NavSite} {
		w.Crafts[1].NavFrame = int(f)
		w.SetActiveCraftIdx(1)
		if w.NavMode != NavOrbit {
			t.Errorf("stored %v with nothing to aim at: arrives on %v, want ORBIT", f, w.NavMode)
		}
		w.SetActiveCraftIdx(0)
	}
	w.Crafts[1].NavFrame = int(NavSurface)
	w.SetActiveCraftIdx(1)
	if w.NavMode != NavSurface {
		t.Errorf("stored SURFACE arrives on %v", w.NavMode)
	}
}
