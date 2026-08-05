// Package screens — entry-scale coverage for the launch/surface
// toggling jump key (issue #348 §4, ADR 0043): World.ToggleLaunchView
// resets LaunchZoom to 0 (auto) on entry, and LaunchView.CurrentScale
// then derives the on-screen scale live from the active vessel's
// altitude — launchAutoScale itself is pinned by TestLaunchViewAutoScale
// above; this exercises the real toggle path end to end at seeded low
// and high altitudes.
package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestToggleLaunchViewEntryScaleIsAltitudeDerived: a low-altitude entry
// (on the pad) scales tight for ground detail; a high-altitude entry
// (well into the ascent) scales wide for arc context — both computed
// fresh at the moment ToggleLaunchView opens the session, matching
// launchAutoScale's own formula.
func TestToggleLaunchViewEntryScaleIsAltitudeDerived(t *testing.T) {
	lowW, _ := spawnSaturnVOnPad(t)
	lowW.ViewMode = sim.ViewTilted
	if entered, refusal := lowW.ToggleLaunchView(); !entered || refusal != "" {
		t.Fatalf("low-altitude enter: entered=%v refusal=%q", entered, refusal)
	}

	highW, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := highW.SpawnCraft(sim.SpawnSpec{
		ParentBodyID: "earth",
		AltitudeM:    200_000,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	highW.ViewMode = sim.ViewTilted
	if entered, refusal := highW.ToggleLaunchView(); !entered || refusal != "" {
		t.Fatalf("high-altitude enter: entered=%v refusal=%q", entered, refusal)
	}

	v := NewLaunchView(launchThemeForTest(), NewOrbitView(launchThemeForTest()))
	v.Resize(120, 40)

	lowScale := v.CurrentScale(lowW)
	highScale := v.CurrentScale(highW)

	if lowW.LaunchZoom != 0 {
		t.Errorf("low-altitude LaunchZoom = %v, want 0 (auto — the entry Framing Event derives the scale live)", lowW.LaunchZoom)
	}
	if highW.LaunchZoom != 0 {
		t.Errorf("high-altitude LaunchZoom = %v, want 0 (auto)", highW.LaunchZoom)
	}
	if wantLow := launchAutoScale(lowW.ActiveCraft().Altitude(), v.canvas.Rows()); lowScale != wantLow {
		t.Errorf("low-altitude scale = %v, want %v (launchAutoScale at the pad's altitude)", lowScale, wantLow)
	}
	if wantHigh := launchAutoScale(highW.ActiveCraft().Altitude(), v.canvas.Rows()); highScale != wantHigh {
		t.Errorf("high-altitude scale = %v, want %v (launchAutoScale at 200 km)", highScale, wantHigh)
	}
	if highScale <= lowScale {
		t.Errorf("high-altitude scale (%v m/cell) should be wider than the pad's (%v m/cell) — more metres per cell for arc context",
			highScale, lowScale)
	}
}
