// orbit_empty_readings_test.go (#482 S4, ADR 0051 W6): the Empty readings
// setting, Full / Tidy / Compact, read off the assembled chip stack.

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func emptyReadingsView(mode settings.EmptyReadingsMode, off ...settings.Chip) *OrbitView {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	s := settings.Default()
	s.SetEmptyReadings(mode)
	for _, c := range off {
		s.SetChip(c, false)
	}
	v.SetSettings(s)
	return v
}

// boxLines returns the assembled lines of the box with the given id and
// whether it was placed at all. ENGINE is stamped ChipNodes (click routing).
func boxLines(v *OrbitView, w *sim.World, id settings.Chip) ([]string, bool) {
	for _, c := range v.assembleChips(w) {
		if c.id == id {
			return c.lines, true
		}
	}
	return nil, false
}

func plainCoastWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Target = sim.Target{Kind: sim.TargetNone}
	return w
}

func TestFoldTrailingDashRowsBottomOnly(t *testing.T) {
	in := []string{"TITLE", "  a:  1", "  b:  —", "  c:  2", "  d:  —  e:  —", "  f:  —"}
	got := foldTrailingDashRows(in)
	want := in[:4]
	if len(got) != len(want) {
		t.Fatalf("folded to %d rows, want %d (middle dash row b: must stay, only the bottom two go): %q", len(got), len(want), got)
	}
	if got := foldTrailingDashRows([]string{"T", "  a:  —"}); len(got) != 1 {
		t.Errorf("title must survive an all-dash box, got %q", got)
	}
	if isDashRow("  node:  ● braking burn at 3 km") || isDashRow("  a:  0.00") {
		t.Error("a row with a reading must not count as a dash row")
	}
	if !isDashRow("  impact:   —   stop:   —") || isDashRow("  plan:") {
		t.Error("dash detector wrong for label/dash rows")
	}
}

func TestEmptyReadingsFullKeepsEverything(t *testing.T) {
	w := plainCoastWorld(t)
	v := emptyReadingsView(settings.EmptyFull)
	tgt, ok := boxLines(v, w, settings.ChipTarget)
	if !ok || len(tgt) != targetBoxMaxLines {
		t.Fatalf("Full: TARGET must be drawn with all %d lines with no target, got placed=%v lines=%d", targetBoxMaxLines, ok, len(tgt))
	}
	nav, _ := boxLines(v, w, settings.ChipNavigation)
	if len(nav) != navigationBoxMaxLines {
		t.Errorf("Full: NAVIGATION lines = %d, want %d", len(nav), navigationBoxMaxLines)
	}
	eng, _ := boxLines(v, w, settings.ChipNodes)
	if len(eng) != engineBoxMaxLines {
		t.Errorf("Full: ENGINE lines = %d, want %d", len(eng), engineBoxMaxLines)
	}
}

func TestEmptyReadingsTidyFoldsNavigationOnlyWhileTargetAbsent(t *testing.T) {
	w := plainCoastWorld(t)
	v := emptyReadingsView(settings.EmptyTidy)
	if _, ok := boxLines(v, w, settings.ChipTarget); ok {
		t.Fatal("Tidy: TARGET must be absent with no target")
	}
	nav, _ := boxLines(v, w, settings.ChipNavigation)
	if len(nav) != navigationBoxMaxLines-2 {
		t.Fatalf("Tidy plain coast: NAVIGATION lines = %d, want %d (impact:/stop: and plan: folded): %q", len(nav), navigationBoxMaxLines-2, nav)
	}
	if strings.Contains(strings.Join(nav, "\n"), "plan:") {
		t.Error("Tidy: plan: row should be folded on a plain coast")
	}
	eng, _ := boxLines(v, w, settings.ChipNodes)
	if len(eng) != engineBoxMaxLines {
		t.Errorf("Tidy must not fold ENGINE, lines = %d, want %d", len(eng), engineBoxMaxLines)
	}

	// With a target showing NAVIGATION keeps every row.
	w.SetTargetBody(targetableBodyIdx(t, w))
	nav, _ = boxLines(v, w, settings.ChipNavigation)
	if len(nav) != navigationBoxMaxLines {
		t.Errorf("Tidy with a target: NAVIGATION lines = %d, want %d", len(nav), navigationBoxMaxLines)
	}
	if tgt, ok := boxLines(v, w, settings.ChipTarget); !ok || len(tgt) != targetBoxMaxLines {
		t.Errorf("Tidy with a target: TARGET must keep all %d rows under Tidy, placed=%v lines=%d", targetBoxMaxLines, ok, len(tgt))
	}
}

func TestEmptyReadingsTidyKeepsNavigationWhenTargetSwitchedOff(t *testing.T) {
	// A Settings-off TARGET still holds a blank slot below NAVIGATION,
	// so NAVIGATION must not fold (nothing may shift under decision 16).
	w := plainCoastWorld(t)
	v := emptyReadingsView(settings.EmptyTidy, settings.ChipTarget)
	nav, _ := boxLines(v, w, settings.ChipNavigation)
	if len(nav) != navigationBoxMaxLines {
		t.Errorf("Tidy, TARGET switched off: NAVIGATION lines = %d, want %d", len(nav), navigationBoxMaxLines)
	}
	if _, ok := boxLines(v, w, settings.ChipTarget); !ok {
		t.Error("switched-off TARGET must still occupy a blank slot")
	}
}

func TestEmptyReadingsCompactDropsTrailingDashRowsEverywhere(t *testing.T) {
	w := plainCoastWorld(t)
	v := emptyReadingsView(settings.EmptyCompact)
	eng, _ := boxLines(v, w, settings.ChipNodes)
	if len(eng) != engineBoxMaxLines-1 {
		t.Errorf("Compact: ENGINE lines = %d, want %d (node: — folded): %q", len(eng), engineBoxMaxLines-1, eng)
	}
	nav, _ := boxLines(v, w, settings.ChipNavigation)
	if len(nav) != navigationBoxMaxLines-2 {
		t.Errorf("Compact: NAVIGATION lines = %d, want %d", len(nav), navigationBoxMaxLines-2)
	}
	// Bottom only: no assembled box may end in a dash row, and every box
	// keeps its title.
	for _, c := range v.assembleChips(w) {
		if len(c.lines) > 1 && isDashRow(c.lines[len(c.lines)-1]) {
			t.Errorf("Compact: box %q still ends in a dash row %q", c.id, c.lines[len(c.lines)-1])
		}
	}
}

func TestEmptyReadingsCompactSettingsOffStillBlanksInPlace(t *testing.T) {
	w := plainCoastWorld(t)
	for _, mode := range settings.EmptyReadingsSteps {
		v := emptyReadingsView(mode, settings.ChipEngine, settings.ChipGuidance)
		eng, ok := boxLines(v, w, settings.ChipNodes)
		if !ok || len(eng) != engineBoxMaxLines {
			t.Errorf("%s: switched-off ENGINE must blank at its full %d lines, placed=%v lines=%d", mode, engineBoxMaxLines, ok, len(eng))
		}
		gd, ok := boxLines(v, w, settings.ChipGuidance)
		if !ok || len(gd) != guidanceBoxMaxLines {
			t.Errorf("%s: switched-off GUIDANCE must blank at its full %d lines, placed=%v lines=%d", mode, guidanceBoxMaxLines, ok, len(gd))
		}
	}
}

// Widths must not depend on the setting (S1 pins them per tier; this
// only guards that the setting never feeds width): every left-box line
// and NAVIGATION line that survives a fold is byte-identical to the Full
// rendering of that row.
func TestEmptyReadingsFoldedRowsAreFullRowsVerbatim(t *testing.T) {
	w := plainCoastWorld(t)
	full := emptyReadingsView(settings.EmptyFull)
	for _, mode := range []settings.EmptyReadingsMode{settings.EmptyTidy, settings.EmptyCompact} {
		v := emptyReadingsView(mode)
		for _, id := range []settings.Chip{settings.ChipNodes, settings.ChipPropellant, settings.ChipGuidance, settings.ChipComms, settings.ChipStages, settings.ChipMissions, settings.ChipNavigation} {
			a, _ := boxLines(full, w, id)
			b, _ := boxLines(v, w, id)
			for i := range b {
				if a[i] != b[i] {
					t.Errorf("%s box %q row %d differs from Full: %q vs %q", mode, id, i, b[i], a[i])
				}
			}
		}
	}
}

// F3 of the #482 review: on the launch pad GUIDANCE's last row is
// `fpa: — orbit fpa: —`, and "orbit fpa:" is a two-word label the first
// isDashRow did not know, so Compact left that one dash row standing.
func TestEmptyReadingsCompactFoldsGuidanceOnThePad(t *testing.T) {
	w := densityPad(t)
	v := emptyReadingsView(settings.EmptyCompact)
	gd, ok := boxLines(v, w, settings.ChipGuidance)
	if !ok {
		t.Fatal("GUIDANCE not placed on the pad")
	}
	if len(gd) != guidanceBoxMaxLines-1 {
		t.Errorf("Compact pad: GUIDANCE lines = %d, want %d (fpa/orbit fpa row folded): %q", len(gd), guidanceBoxMaxLines-1, gd)
	}
	for _, c := range v.assembleChips(w) {
		if len(c.lines) > 1 && isDashRow(c.lines[len(c.lines)-1]) {
			t.Errorf("Compact pad: box %q still ends in a dash row %q", c.id, c.lines[len(c.lines)-1])
		}
	}
}
