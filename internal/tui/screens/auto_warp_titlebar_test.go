package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func plainTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// TestBurnButtonHitTest — the [»Burn] title-bar button registers a click
// in its range, sits clear of [Menu]/[Missions], and is row-0 only.
func TestBurnButtonHitTest(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(180, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	_ = v.Render(w, 0, 180, 40)

	mid := (v.burnColStart + v.burnColEnd) / 2
	if !v.HitBurnButton(mid, 0) {
		t.Errorf("center of [»Burn] range did not register a hit")
	}
	if v.HitBurnButton(mid, 1) {
		t.Errorf("row 1 should miss [»Burn] (row-0 only)")
	}
	if v.HitMenuButton(mid, 0) || v.HitMissionsButton(mid, 0) {
		t.Errorf("[»Burn] range overlaps [Menu]/[Missions]")
	}
	if v.HitBurnButton((v.menuColStart+v.menuColEnd)/2, 0) {
		t.Errorf("[Menu] range overlaps [»Burn]")
	}
}

// TestBurnButtonWidthStableAcrossEngage — the engaged [■Burn] label is
// the same rune width as [»Burn], so the hit-test ranges don't shift when
// Auto-Warp toggles (single-width runs, ADR 0016).
func TestBurnButtonWidthStableAcrossEngage(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(180, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.PlanNode(sim.ManeuverNode{TriggerTime: w.Clock.SimTime.Add(2 * time.Hour), DV: 10, Mode: spacecraft.BurnPrograde})

	_ = v.Render(w, 0, 180, 40)
	offWidth := v.burnColEnd - v.burnColStart

	if !w.EngageAutoWarp() {
		t.Fatal("EngageAutoWarp failed with an eligible burn planted")
	}
	_ = v.Render(w, 0, 180, 40)
	onWidth := v.burnColEnd - v.burnColStart

	if offWidth != onWidth {
		t.Errorf("button width shifted across engage: off=%d on=%d", offWidth, onWidth)
	}
	if offWidth != len([]rune("[»Burn]")) {
		t.Errorf("button width %d, want %d ([»Burn] runes)", offWidth, len([]rune("[»Burn]")))
	}
}

// TestTitleChipMorphsToAutoWhenEngaged — the warp readout shows the
// normal `warp Nx` when idle and morphs to an `AUTO →` chip with the
// `[■Burn]` label while engaged.
func TestTitleChipMorphsToAutoWhenEngaged(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(200, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.PlanNode(sim.ManeuverNode{TriggerTime: w.Clock.SimTime.Add(6 * time.Hour), DV: 10, Mode: spacecraft.BurnPrograde})

	idle := strings.Split(v.Render(w, 0, 200, 40), "\n")[0]
	if !strings.Contains(idle, "warp ") {
		t.Errorf("idle title missing `warp Nx`: %q", idle)
	}
	if strings.Contains(idle, "AUTO") {
		t.Errorf("idle title should not show AUTO: %q", idle)
	}
	if !strings.Contains(idle, "[»Burn]") {
		t.Errorf("idle title missing [»Burn] button: %q", idle)
	}

	if !w.EngageAutoWarp() {
		t.Fatal("engage failed")
	}
	on := strings.Split(v.Render(w, 0, 200, 40), "\n")[0]
	if !strings.Contains(on, "AUTO →") {
		t.Errorf("engaged title missing `AUTO →` chip: %q", on)
	}
	if !strings.Contains(on, "[■Burn]") {
		t.Errorf("engaged title missing [■Burn] label: %q", on)
	}
}

// The two-unit, prefix-free duration formatting this used to pin
// (TestCompactDuration, pre-ADR-0049) now lives on internal/tui/readout's
// own TestDuration: compactDuration is deleted, every screens/ call site
// routes through readout.Duration instead (ADR 0049 stage A2).

// TestAutoWarpFormFitsWarpField (R4 #3): the AUTO form must fit the fixed
// warp field even for a node years away, where readout.Duration alone
// would emit "11574d05h" and push the title bar.
func TestAutoWarpFormFitsWarpField(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for _, days := range []int{3, 400, 1500, 30000} {
		w.ClearNodes()
		w.PlanNode(sim.ManeuverNode{TriggerTime: w.Clock.SimTime.Add(time.Duration(days) * 24 * time.Hour), DV: 10, Mode: spacecraft.BurnPrograde})
		if !w.EngageAutoWarp() {
			t.Fatalf("engage failed at %d days", days)
		}
		txt := warpRateText(w)
		if !strings.HasPrefix(txt, "AUTO") {
			t.Fatalf("not AUTO form at %d days: %q", days, txt)
		}
		// Worst-case rate digits: swap the effective rate for 100000x.
		worst := strings.Replace(txt, strings.Fields(txt)[1], "→100000x", 1)
		if got := lipgloss.Width(worst); got > warpFieldWidth {
			t.Errorf("%d days: %q is %d cells, field is %d", days, worst, got, warpFieldWidth)
		}
	}
}
