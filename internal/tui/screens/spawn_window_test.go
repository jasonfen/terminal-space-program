package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// spawnFormMidCatalog builds a spawn form with the cursor parked on a
// vessel in the middle of the CRAFT TYPE catalog — the #373 repro case:
// deep enough into the list that the pre-fix, unwindowed Render scrolled
// the cursor, the title, and the footer off the top of a real terminal.
func spawnFormMidCatalog(t *testing.T) *SpawnCraft {
	t.Helper()
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "", nil)
	s.showAll = true // full unfiltered catalog — the biggest, most repro-prone list
	n := s.visibleCatalogCount()
	if n < 3 {
		t.Fatalf("catalog too small to test mid-list windowing: %d loadouts", n)
	}
	s.loadoutIdx = n / 2
	return s
}

// assertSpawnChromeVisible checks the invariants #373 / ADR 0046 require at
// every terminal size at or above the Playable Floor: the title, the
// "[f]" filter hint, the cursor marker on the selected CRAFT TYPE row, the
// always-on fields below it, and the footer must all be present in the
// rendered output — and the whole thing must fit within height lines, or
// something upstream of the footer has scrolled off the top of a real
// terminal (the exact #373 symptom).
func assertSpawnChromeVisible(t *testing.T, out string, height int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) > height {
		t.Errorf("rendered %d lines, want <= %d (terminal height) — chrome will scroll off the top:\n%s",
			len(lines), height, out)
	}
	if !strings.Contains(out, "spawn vessel") {
		t.Errorf("title missing from rendered output:\n%s", out)
	}
	if !strings.Contains(out, "[f]") {
		t.Errorf("\"[f]\" system-filter hint missing from rendered output:\n%s", out)
	}
	if !strings.Contains(out, "→ ") {
		t.Errorf("CRAFT TYPE cursor marker (\"→ \") missing from rendered output — the player can't tell which vessel Enter will spawn:\n%s", out)
	}
	for _, want := range []string{"POSITION", "PARENT BODY", "ALTITUDE", "DIRECTION"} {
		if !strings.Contains(out, want) {
			t.Errorf("field header %q missing from rendered output:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "[esc] cancel") {
		t.Errorf("footer missing from rendered output:\n%s", out)
	}
}

// TestSpawnFormFitsAndKeepsCursorVisibleAt104x24 is the #373 repro at the
// Playable Floor. Before the windowed-list fix, Render dumped the entire
// CRAFT TYPE catalog (headers + every loadout) unconditionally, so at
// 104x24 the title, the cursor, and the footer were routinely pushed off
// the top of the pane.
func TestSpawnFormFitsAndKeepsCursorVisibleAt104x24(t *testing.T) {
	s := spawnFormMidCatalog(t)
	assertSpawnChromeVisible(t, s.Render(104, 24), 24)
}

// TestSpawnFormFitsAndKeepsCursorVisibleAt120x36 pins the 2026-09-03
// reopen evidence directly: the same scrolling failure reproduced at
// 120x36, well above the size #373 was originally (wrongly) closed
// against, because the form is 46-52 rows regardless of terminal size.
func TestSpawnFormFitsAndKeepsCursorVisibleAt120x36(t *testing.T) {
	s := spawnFormMidCatalog(t)
	assertSpawnChromeVisible(t, s.Render(120, 36), 36)
}

// TestSpawnFormFitsAt140x40DesignSize checks the ADR 0046 Design Size
// too: the form (46-52 rows) still overflows 140x40 on its own, so the
// windowed catalog must still be doing work even well above the floor.
func TestSpawnFormFitsAt140x40DesignSize(t *testing.T) {
	s := spawnFormMidCatalog(t)
	assertSpawnChromeVisible(t, s.Render(140, 40), 40)
}

// TestSpawnFormWindowActuallyHidesRowsAtFloor confirms the catalog is
// ACTUALLY windowed (not just coincidentally short) at the floor: with the
// cursor mid-list and the full unfiltered catalog selected, far fewer
// loadout rows render than the catalog actually holds. (At the floor,
// fitting height takes priority over the "N more" markers themselves —
// see TestSpawnFormFitsAndKeepsCursorVisibleAt104x24 — so this checks row
// counts rather than requiring the marker text.)
func TestSpawnFormWindowActuallyHidesRowsAtFloor(t *testing.T) {
	s := spawnFormMidCatalog(t)
	total := s.visibleCatalogCount()
	out := s.Render(104, 24)
	shown := strings.Count(out, "➤")
	if shown >= total {
		t.Errorf("windowed render shows %d of %d catalog rows, want far fewer:\n%s", shown, total, out)
	}
	cursorName := spacecraft.Loadouts[s.orderedLoadoutIDs()[s.loadoutIdx]].Name
	if !strings.Contains(out, cursorName) {
		t.Errorf("the cursor's own row (%s) is missing from the windowed render:\n%s", cursorName, out)
	}
}

// TestSpawnFormRenderUnboundedHeightShowsWholeCatalog locks in the
// height<=0 escape hatch existing tests rely on: with no height budget,
// every catalog loadout still renders (no windowing, no markers) — the
// pre-#373 behavior, kept for callers with nothing to fit into.
func TestSpawnFormRenderUnboundedHeightShowsWholeCatalog(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "", nil)
	s.showAll = true
	out := s.Render(80, 0)
	for _, id := range spacecraft.LoadoutOrder {
		l := spacecraft.Loadouts[id]
		if !strings.Contains(out, l.Name) {
			t.Errorf("loadout %q missing from unbounded-height render", l.Name)
		}
	}
}
