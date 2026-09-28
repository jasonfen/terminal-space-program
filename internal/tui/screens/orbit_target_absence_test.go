// orbit_target_absence_test.go (#480): with no target set, the TARGET
// box is not drawn at all rather than drawing its title and four dash
// rows (the ADR 0051 slot rule every OTHER box still keeps). Jason:
// "if there isn't a target, that chip shouldn't be there." Safe only for
// TARGET because it is the LAST box in the right column (NAVIGATION
// above it never shifts): see the 2026-09-28 amendment to ADR 0051 in
// the planning vault. These guards assert on the rendered frame
// (v.Render's output / v.chipRects), not on buildTargetBox's own return
// value, which still produces the all-dash form for callers that need
// its width (see TestTargetBoxNoTargetIsAllDashes).

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// targetableBodyIdx returns the first non-primary body index in w's
// system, a valid SetTargetBody argument (idx 0 is the primary and
// clears rather than sets, per SetTargetBody's own doc comment).
func targetableBodyIdx(t *testing.T, w *sim.World) int {
	t.Helper()
	sys := w.System()
	for i, b := range sys.Bodies {
		if i == 0 {
			continue
		}
		if b.EnglishName != "" {
			return i
		}
	}
	t.Fatal("setup: no non-primary body found to target")
	return -1
}

// TestTargetBoxAbsentWithNoTargetPresentWhenSet is the primary #480
// guard: no TARGET box with nothing targeted, the box appears the
// instant a target is set (the `t` key path, SetTargetBody here), and
// it leaves again the instant the target clears, all three read off
// the actual rendered frame.
func TestTargetBoxAbsentWithNoTargetPresentWhenSet(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Target = sim.Target{Kind: sim.TargetNone}

	out := v.Render(w, 0, 140, 40)
	if strings.Contains(out, "TARGET") {
		t.Fatalf("no target set: TARGET box must not render at all:\n%s", out)
	}

	idx := targetableBodyIdx(t, w)
	w.SetTargetBody(idx)
	out = v.Render(w, 0, 140, 40)
	if !strings.Contains(out, "TARGET") {
		t.Fatalf("target just set: TARGET box must render:\n%s", out)
	}

	w.ClearTarget()
	out = v.Render(w, 0, 140, 40)
	if strings.Contains(out, "TARGET") {
		t.Fatalf("target cleared again: TARGET box must be gone again:\n%s", out)
	}
}

// TestTargetBoxAbsentInLaunchViewToo: the map and the LAUNCH view share
// one layout (ADR 0051 decision 3), so TARGET leaves in both on the same
// condition: this exercises LaunchView.Render directly rather than
// assuming the shared assembleChips/composeChips pipeline covers it.
func TestTargetBoxAbsentInLaunchViewToo(t *testing.T) {
	th := launchThemeForTest()
	hud := NewOrbitView(th)
	v := NewLaunchView(th, hud)
	w, c := spawnSaturnVOnPad(t)
	_ = c
	w.Target = sim.Target{Kind: sim.TargetNone}

	out := stripANSI(v.Render(w, DesignWidth, DesignHeight))
	if strings.Contains(out, "TARGET") {
		t.Fatalf("LAUNCH view with no target must not draw TARGET:\n%s", out)
	}

	idx := targetableBodyIdx(t, w)
	w.SetTargetBody(idx)
	out = stripANSI(v.Render(w, DesignWidth, DesignHeight))
	if !strings.Contains(out, "TARGET") {
		t.Fatalf("LAUNCH view with a target set must draw TARGET:\n%s", out)
	}
}

// TestNavigationRowsAndNavballPositionUnchangedByTargetAbsence proves
// the reason #480 is safe for TARGET alone: TARGET is the last box in
// the right column, so NAVIGATION above it, and the navball below it,
// never move. Compares actual row AND column indices pulled from the
// rendered frame (v.chipRects for NAVIGATION; a distinctive navball
// glyph's row/col for the navball), not a builder's return value.
func TestNavigationRowsAndNavballPositionUnchangedByTargetAbsence(t *testing.T) {
	withTarget := NewOrbitView(chipTestTheme())
	withTarget.Resize(140, 40)
	wt, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	idx := targetableBodyIdx(t, wt)
	wt.SetTargetBody(idx)
	outWithTarget := withTarget.Render(wt, 0, 140, 40)
	navRowWith, ok := chipRowStart(withTarget, settings.ChipNavigation)
	if !ok {
		t.Fatal("setup: NAVIGATION chip not found with a target set")
	}
	navColWith, ok := chipColStart(withTarget, settings.ChipNavigation)
	if !ok {
		t.Fatal("setup: NAVIGATION chip col not found with a target set")
	}

	noTarget := NewOrbitView(chipTestTheme())
	noTarget.Resize(140, 40)
	wn, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	wn.Target = sim.Target{Kind: sim.TargetNone}
	outNoTarget := noTarget.Render(wn, 0, 140, 40)
	navRowWithout, ok := chipRowStart(noTarget, settings.ChipNavigation)
	if !ok {
		t.Fatal("setup: NAVIGATION chip not found with no target")
	}
	navColWithout, ok := chipColStart(noTarget, settings.ChipNavigation)
	if !ok {
		t.Fatal("setup: NAVIGATION chip col not found with no target")
	}

	if navRowWith != navRowWithout {
		t.Errorf("NAVIGATION row = %d with a target, %d with none; TARGET leaving must not move NAVIGATION", navRowWith, navRowWithout)
	}
	if navColWith != navColWithout {
		t.Errorf("NAVIGATION col = %d with a target, %d with none; TARGET leaving must not move NAVIGATION", navColWith, navColWithout)
	}

	rowWith, colWith, foundWith := navballMarkerPos(outWithTarget)
	rowWithout, colWithout, foundWithout := navballMarkerPos(outNoTarget)
	if !foundWith || !foundWithout {
		t.Fatalf("setup: navball marker not found (withTarget=%v withoutTarget=%v)", foundWith, foundWithout)
	}
	if rowWith != rowWithout {
		t.Errorf("navball row = %d with a target, %d with none; navball must not move when TARGET leaves", rowWith, rowWithout)
	}
	if colWith != colWithout {
		t.Errorf("navball col = %d with a target, %d with none; navball must not move when TARGET leaves", colWith, colWithout)
	}
}

// navballMarkerPos finds the row/col of the navball panel's "[ORBIT]"
// mode-button label (present in NavOrbit mode, the default) in a
// rendered frame, stripped of ANSI first so column math counts display
// cells rather than escape bytes.
func navballMarkerPos(out string) (row, col int, found bool) {
	plain := stripANSI(out)
	lines := strings.Split(plain, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "[ORBIT]"); idx >= 0 {
			return i, idx, true
		}
	}
	return 0, 0, false
}

// chipColStart returns the left screen column of the chip with the
// given id, from the most recent Render's recorded chipRects.
func chipColStart(v *OrbitView, id settings.Chip) (int, bool) {
	for _, r := range v.chipRects {
		if r.id == id {
			return r.colStart, true
		}
	}
	return 0, false
}

// TestTargetBoxSettingsOffLeavesBlankRowsRegardlessOfTarget: the new
// no-target absence must compose with the per-box Settings switch, not
// fight it (#480's own constraint). A box switched off in Settings
// still leaves its rows blank in place, whether or not a target is set.
func TestTargetBoxSettingsOffLeavesBlankRowsRegardlessOfTarget(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	s := settings.Default()
	s.SetChip(settings.ChipTarget, false)
	v.SetSettings(s)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Target = sim.Target{Kind: sim.TargetNone}
	out := v.Render(w, 0, 140, 40)
	if !strings.Contains(out, "TARGET") {
		t.Fatalf("Settings-hidden TARGET with no target must still render its bare title (blanked in place, not omitted):\n%s", out)
	}

	idx := targetableBodyIdx(t, w)
	w.SetTargetBody(idx)
	out = v.Render(w, 0, 140, 40)
	if !strings.Contains(out, "TARGET") {
		t.Fatalf("Settings-hidden TARGET with a target set must still render its bare title:\n%s", out)
	}
}

// TestDockedVesselKeepsTargetBox: docked is not "no target" (#480's own
// constraint): a docked vessel has a target (the vessel it's docked
// to/with) and keeps its TARGET box. Uses leadTestWorld at near-zero
// range/closing, DOCK READY territory, same fixture the box density
// table's "docked" phase already uses.
func TestDockedVesselKeepsTargetBox(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w := leadTestWorld(t, 0.01)
	out := v.Render(w, 0, 140, 40)
	if !strings.Contains(out, "TARGET") {
		t.Fatalf("a docked vessel (near-zero range/closing) has a target and must keep its TARGET box:\n%s", out)
	}
}

// TestComposeChipsBayGrowsBudgetWithTargetAbsent (#480 constraint: "lay
// out a four-notice fold with TARGET absent and confirm the bay still
// behaves"). The same four synthetic notices that fold 2-of-4 in
// TestComposeChipsBayFoldsOldestWhenOverflowing (with TARGET occupying
// the right column, bayTop pinned to its bottom row 19, budget 17) fold
// only the single oldest (FRAME TRANSITION) once assembleChips omits
// TARGET for a real no-target world: with just NAVIGATION on the right
// (bottom row 10), the bay's clamp instead pins to GUIDANCE's own
// bottom row (17, the widest-left-box carve-out composeChips' own
// comment documents: GUIDANCE is wider than MISSION, the box actually
// beside the bay, and now reaches deeper than NAVIGATION alone does),
// so the budget grows from 17 to 19: two rows reclaimed is enough for
// one fewer notice to fold (CAPTURE PREVIEW, 7 rows, now fits).
func TestComposeChipsBayGrowsBudgetWithTargetAbsent(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Target = sim.Target{Kind: sim.TargetNone}

	chips := v.assembleChips(w)
	for _, c := range chips {
		if c.id == settings.ChipTarget {
			t.Fatalf("assembleChips still included TARGET with no target set")
		}
	}
	chips = append(chips,
		builtChip{corner: cornerBay, lines: []string{"FRAME TRANSITION", "  Earth -> Moon", "  at T-8d17h"}},
		builtChip{corner: cornerBay, lines: []string{"CAPTURE PREVIEW", "  primary: Moon", "  arrival: 1593 m/s", "  direction: retrograde", "  extra row"}},
		builtChip{corner: cornerBay, lines: []string{"SOI PASS", "  body: Moon", "  planned: 616.9 km", "  entry: T-7d22h", "  extra row"}},
		builtChip{corner: cornerBay, lines: []string{"SESSION", "  bob joined"}},
	)

	const cCols, cRows = 138, 37
	out := v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, 0, 0, 0, chips)

	if strings.Contains(out, "TARGET") {
		t.Fatalf("composed frame must not contain TARGET with no target set:\n%s", out)
	}
	if strings.Contains(out, "Earth -> Moon") {
		t.Errorf("FRAME TRANSITION (oldest) should still fold, budget only grew by 2 rows (17 -> 19), not enough for all four:\n%s", out)
	}
	for _, want := range []string{"arrival: 1593 m/s", "planned: 616.9 km", "bob joined"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q should render in full: with TARGET's rows reclaimed the bay budget grows from 17 to 19, one more notice than TARGET-present fits:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "+1 more") {
		t.Errorf("want exactly one fold (FRAME TRANSITION) with the grown 19-row budget, TARGET-present folds two:\n%s", out)
	}
	if strings.Contains(out, "+2 more") {
		t.Errorf("still folding two notices; TARGET's reclaimed rows should have let CAPTURE PREVIEW back in:\n%s", out)
	}
	assertNoChipRectOverlaps(t, v.chipRects)
}
