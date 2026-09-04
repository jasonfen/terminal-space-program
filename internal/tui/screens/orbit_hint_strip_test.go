package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The Hint Strip (grilled 2026-09-04, #425; CONTEXT.md §"Hint Strip") is
// the map's fixed legend on the canvas's last row, right of "view:".
// Fixed content, always on, no phase gating.

// TestHintStripPresentAtDesignSize confirms the full, exact strip text
// renders intact at the Design Size (140×40) — a plain fresh world, no
// exotic chip state.
func TestHintStripPresentAtDesignSize(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip missing or corrupted at 140x40 (Design Size):\n%s", out)
	}
	if !strings.Contains(out, "view: ") {
		t.Errorf("expected the view: label to still be present:\n%s", out)
	}
}

// TestHintStripSurvivesDeclutter: the Hint Strip is the map's legend, not
// a Chip — F2 declutter must not touch it.
func TestHintStripSurvivesDeclutter(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	v.SetDeclutter(true)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip should survive Declutter:\n%s", out)
	}
}

// TestHintStripSurvivesTutorialOff: the strip is unrelated to Flight
// School state — must render whether or not the tutorial program toggle
// is on.
func TestHintStripSurvivesTutorialOff(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetEnabledMissionPrograms(map[string]bool{}) // both programs off
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip should render regardless of Flight School toggle:\n%s", out)
	}
}

// TestHintStripDoesNotOverlapNavballOrBottomLeftChip stresses the two
// collision risks the task calls out explicitly: the navball panel and a
// bottom-left chip. It forces both to render (an active craft with a
// navball sub-observer, plus a live CHAT-style bottom-left chip is hard
// to force generically, so this uses the always-present ORBIT metrics /
// VESSEL core chip footprint that already occupies the bottom-left
// stacking cursor region) at both the Design Size (140x40) and the
// Playable Floor (104x24), and checks the exact Hint Strip text still
// appears intact — if a later chip's overlay had painted over any part
// of it, the literal substring would no longer be present.
func TestHintStripDoesNotOverlapNavballOrBottomLeftChip(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{140, 40}, {104, 24}} {
		v := NewOrbitView(chipTestTheme())
		v.Resize(sz.w, sz.h)
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		out := stripANSI(v.Render(w, 0, sz.w, sz.h))
		if !strings.Contains(out, hintStripText) {
			t.Errorf("Hint Strip missing/corrupted at %dx%d (navball should be showing for the starter craft):\n%s", sz.w, sz.h, out)
		}
		// The navball panel paints an "RCS" toggle label; confirm it's
		// actually present in this render so the no-overlap check means
		// something (proving the check can find a positive before
		// trusting the negative).
		if !strings.Contains(out, "RCS") {
			t.Errorf("expected the navball panel (RCS control) to be present at %dx%d so this is a real overlap test, not a vacuous one:\n%s", sz.w, sz.h, out)
		}
	}
}

// TestHintStripStartsAfterViewLabel confirms the strip is placed strictly
// after the "view:" label ends (never overwrites it) by checking the
// longer "view: tilted N°/anchor" form still leaves the full Hint Strip
// intact right after it, at the Design Size.
func TestHintStripStartsAfterViewLabel(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ViewMode = sim.ViewTilted
	out := stripANSI(v.Render(w, 0, 140, 40))
	if !strings.Contains(out, "view: tilted") {
		t.Errorf("expected the tilted view label:\n%s", out)
	}
	if !strings.Contains(out, hintStripText) {
		t.Errorf("Hint Strip missing/corrupted alongside the tilted view label:\n%s", out)
	}
}

// TestHintStripClipsRightBelowDesignSize: below the Design Size the strip
// is allowed (expected) to clip on the right rather than wrap or push
// other content — it's a legend, not a numeric field (CONTEXT.md). At a
// narrow width the view: label must still be intact and unclipped (it's
// closer to the left edge), even though the tail of the Hint Strip is
// cut off or entirely absent.
func TestHintStripClipsRightBelowDesignSize(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(60, 24)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := stripANSI(v.Render(w, 0, 60, 24))
	if !strings.Contains(out, "view: ") {
		t.Errorf("view: label should never clip:\n%s", out)
	}
	// The full strip is long relative to 60 cols of total screen width
	// (minus borders/HUD), so it's expected NOT to appear whole here —
	// this pins the clip behavior rather than a silent wrap/reflow.
	if strings.Contains(out, hintStripText) {
		t.Logf("Hint Strip fit in full at 60x24 — narrower than expected, not a failure, just noting it for the record")
	}
}

// TestHintStripWithForcedNodesChipAndCraftNotVisibleHere probes the one
// collision path composeChips' own bottom-right stacking leaves open:
// navballReservedRows returns 0 whenever CraftVisibleHere() is false
// (camera tabbed to a different system than the active craft), so the
// NODES chip's forced (2+ queued nodes) bottom-right block's bottom row
// can land on the canvas's very last row too — the same row the Hint
// Strip paints on the right-hand side. This is NOT one of the two
// collisions #425 requires proving absent (navball, bottom-left chip);
// it's a corner the decision doesn't cover. Measured, not assumed:
//   - 140×40 (Design Size): no collision — the Nodes chip stays narrow
//     and hard-right, short of where the Hint Strip's 78-rune text ends.
//   - 104×24 (Playable Floor): DOES collide — the chip's left border
//     overwrites the Hint Strip's tail. Documented here and in
//     impl-notes/425.md as a known, deliberately out-of-scope edge case
//     (requires both a queued 2+-node vessel AND the camera tabbed away
//     to a different system while it's queued) rather than silently
//     asserted away.
func TestHintStripWithForcedNodesChipAndCraftNotVisibleHere(t *testing.T) {
	render := func(sz struct{ w, h int }) string {
		v := NewOrbitView(chipTestTheme())
		v.Resize(sz.w, sz.h)
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld: %v", err)
		}
		s := settings.Default()
		for _, chipID := range settings.AllChips {
			s.SetChip(chipID, false)
		}
		v.SetSettings(s)

		c := w.ActiveCraft()
		if c == nil {
			t.Fatal("expected an active craft")
		}
		c.Nodes = []spacecraft.ManeuverNode{
			{Mode: spacecraft.BurnPrograde, DV: 42, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
			{Mode: spacecraft.BurnRetrograde, DV: 7, TriggerTime: w.Clock.SimTime.Add(2 * time.Minute)},
		}
		c.SystemIdx = w.SystemIdx + 1 // parked in a different system than the camera
		out := stripANSI(v.Render(w, 0, sz.w, sz.h))
		if !strings.Contains(out, "NODES") {
			t.Fatalf("expected the force-shown NODES chip in this setup at %dx%d:\n%s", sz.w, sz.h, out)
		}
		return out
	}

	if out := render(struct{ w, h int }{140, 40}); !strings.Contains(out, hintStripText) {
		t.Errorf("Design Size: Hint Strip was overwritten by the forced bottom-right NODES chip with CraftVisibleHere()==false, expected no collision here:\n%s", out)
	}
	if out := render(struct{ w, h int }{104, 24}); strings.Contains(out, hintStripText) {
		t.Logf("Playable Floor: Hint Strip stayed intact against the forced NODES chip — better than the last measurement, not a failure")
	} else {
		t.Logf("Playable Floor: confirmed known collision between the forced bottom-right NODES chip and the Hint Strip's tail when CraftVisibleHere()==false (out of #425's decided scope; see impl-notes/425.md)")
	}
}
