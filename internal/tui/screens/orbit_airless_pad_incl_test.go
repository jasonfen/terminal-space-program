// Package screens: ADR 0050 decision 9 / issue #454 (readout half). An
// airless pad (Moon, Glyph, ...) never showed the LAUNCH HUD at all, since
// shouldShowLaunchHUD requires an atmosphere and buildDescentChip ran
// instead, so a Luna pad had no heading:/incl:/Δincl: rows however long
// the player warped for. This file pins the fix: while Landed, DESCENT
// carries the same heading/incl/Δincl block buildLaunchChip's SURFACE
// chip already shows on an atmospheric pad, sharing the block rather than
// duplicating it. Whether the LAUNCH HUD itself should ever appear on the
// Moon is a separate question (#454's own body says so) and is untouched
// here: shouldShowLaunchHUD keeps returning false for an airless primary.

package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestDescentChipShowsHeadingAndInclWhileLandedOnMoon is the sabotage-first
// proof for the readout half of #454. Verified red against the unfixed
// buildDescentChip (before landedInclHeadingRows existed and before this
// chip's Landed branch called it): failed with "expected a 'heading:' row
// on the airless DESCENT chip while Landed" because buildDescentChip's
// airless pad output was DESCENT + altitude/vert/horiz/fpa/twr/hold only,
// with nothing about heading or inclination.
func TestDescentChipShowsHeadingAndInclWhileLandedOnMoon(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	lines := v.buildDescentChip(w)
	if lines == nil {
		t.Fatal("buildDescentChip returned nil for a Landed craft on an airless body")
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "heading:") {
		t.Errorf("expected a 'heading:' row on the airless DESCENT chip while Landed:\n%s", joined)
	}
	if !strings.Contains(joined, "incl:") {
		t.Errorf("expected an 'incl:' row on the airless DESCENT chip while Landed:\n%s", joined)
	}
	if !strings.Contains(joined, "(min ") {
		t.Errorf("expected the incl: row to carry the '(min N°)' Inclination Floor tag, same as the atmospheric pad:\n%s", joined)
	}
	// The pre-existing descent rows must still be there: this is growth,
	// not a swap.
	for _, want := range []string{"DESCENT", "altitude:", "vert:", "horiz:", "fpa:", "TWR:", "hold:"} {
		if !strings.Contains(joined, want) {
			t.Errorf("expected the pre-existing DESCENT row %q to survive:\n%s", want, joined)
		}
	}
}

// TestDescentChipNoDeltaInclWithoutTargetOnMoon mirrors
// TestLandedPadNoDeltaInclWithoutTarget for the airless DESCENT chip:
// decision 9's Δincl row is conditional on a body target, same gate as
// the atmospheric pad.
func TestDescentChipNoDeltaInclWithoutTargetOnMoon(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	lines := v.buildDescentChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "Δincl:") {
		t.Errorf("expected no Δincl row on the airless pad without a target set:\n%s", joined)
	}
}

// TestDescentChipShowsDeltaInclWithTargetOnMoon mirrors
// TestLandedPadShowsDeltaInclWithTarget: with a body target set, the
// airless DESCENT chip's third row is Δincl, exactly like the atmospheric
// SURFACE chip.
func TestDescentChipShowsDeltaInclWithTargetOnMoon(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	sys := w.System()
	earthIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Earth" || b.ID == "earth" {
			earthIdx = i
			break
		}
	}
	if earthIdx < 0 {
		t.Fatalf("earth not found in default system")
	}
	w.SetTargetBody(earthIdx)

	lines := v.buildDescentChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Δincl:") {
		t.Fatalf("expected a Δincl row on the airless pad with a target set:\n%s", joined)
	}
}

// TestDescentChipNotLandedHasNoHeadingRow proves the added rows are
// Landed-only: an airless-body DESCENT chip for a craft still flying
// (shouldShowDescentHUD true via the low-altitude arm, not Landed) must
// not gain heading:/incl: rows: those are pad concepts, not a flight
// readout, and buildLaunchChip's own airborne branch (decision 10) makes
// the same distinction on atmospheric bodies.
func TestDescentChipNotLandedHasNoHeadingRow(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := placeLanderOnMoon(t, w, 5_000, 0, 100, 0)
	if c.Landed {
		t.Fatal("setup: expected a flying (not Landed) craft")
	}
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)

	lines := v.buildDescentChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "heading:") {
		t.Errorf("expected no 'heading:' row on a flying (not Landed) DESCENT chip:\n%s", joined)
	}
}

// TestDescentChipPadRowsAlignToColumn14 mirrors
// TestLaunchChipPadRowsAlignToColumn14 for the airless DESCENT chip: the
// shared landedInclHeadingRows helper must land its values at the same
// column every other DESCENT row uses (launchChipValueCol), not a
// hand-padded one of its own.
func TestDescentChipPadRowsAlignToColumn14(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	sys := w.System()
	earthIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Earth" || b.ID == "earth" {
			earthIdx = i
			break
		}
	}
	if earthIdx < 0 {
		t.Fatalf("earth not found in default system")
	}
	w.SetTargetBody(earthIdx)

	lines := v.buildDescentChip(w)

	rowFor := func(label string) string {
		t.Helper()
		for _, l := range lines {
			if strings.HasPrefix(l, "  "+label) {
				return l
			}
		}
		t.Fatalf("no row found with label %q in:\n%s", label, strings.Join(lines, "\n"))
		return ""
	}
	valueColumn := func(row, label string) int {
		t.Helper()
		prefix := "  " + label
		if !strings.HasPrefix(row, prefix) {
			t.Fatalf("row %q does not start with prefix %q", row, prefix)
		}
		rest := row[len(prefix):]
		spaces := 0
		for _, r := range rest {
			if r != ' ' {
				break
			}
			spaces++
		}
		return lipgloss.Width(prefix) + spaces
	}

	for _, label := range []string{"heading:", "incl:", "Δincl:"} {
		row := rowFor(label)
		gotCol := valueColumn(row, label)
		if gotCol != launchChipValueCol {
			t.Errorf("%s row's value column = %d, want %d (launchChipValueCol): row=%q", label, gotCol, launchChipValueCol, row)
		}
	}
}
