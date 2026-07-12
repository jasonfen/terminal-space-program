package screens

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/save"
)

// savesTheme returns a style-free Theme so rendered output is plain
// text (assertable with strings.Contains without ANSI noise).
func savesTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// savesFixture returns three entries the way save.List would: newest
// SavedAt first — a named save, an autosave, and a Meta-less legacy
// import stand-in (blank in-game date + system, per the S1/S2 handoff).
func savesFixture() []save.SaveInfo {
	return []save.SaveInfo{
		{
			ID: "save-100.json",
			Meta: save.Meta{
				Name:             "Apollo run",
				SavedAt:          time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC),
				InGameEpoch:      time.Date(2000, 3, 14, 0, 0, 0, 0, time.UTC),
				ActiveVesselName: "Kern Stack",
				SystemName:       "Sol",
			},
			Lane: save.LaneNamed,
		},
		{
			ID: "autosave-1.json",
			Meta: save.Meta{
				SavedAt:          time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC),
				InGameEpoch:      time.Date(2000, 3, 13, 0, 0, 0, 0, time.UTC),
				ActiveVesselName: "Probe IV",
				SystemName:       "Lumen",
			},
			Lane: save.LaneAutosave,
		},
		{
			ID: "save-42.json",
			Meta: save.Meta{
				// Meta-less legacy header: SavedAt backfilled from ClockT0,
				// everything else zero.
				SavedAt: time.Date(2026, 7, 10, 8, 0, 0, 0, time.UTC),
			},
			Lane: save.LaneNamed,
		},
	}
}

func keyRunes(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

var (
	keyEnter = tea.KeyMsg{Type: tea.KeyEnter}
	keyEsc   = tea.KeyMsg{Type: tea.KeyEsc}
	keyDown  = tea.KeyMsg{Type: tea.KeyDown}
)

// TestSavesListRendersRows — the plan's "List renders N rows with the
// expected columns; newest-first ordering": every entry appears with
// its name / saved-at / in-game date / vessel columns, reserved lanes
// carry a badge, and the row order preserves the (already
// newest-first) List order. Meta-less entries render a blank in-game
// date rather than the zero time.
func TestSavesListRendersRows(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")
	out := sc.Render(110, 40)

	for _, want := range []string{
		"Apollo run", "2000-03-14", "Kern Stack", // named row columns
		"[AUTOSAVE 1]", "2000-03-13", "Probe IV", // autosave badge + columns
		"(unnamed)", // Meta-less legacy entry
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n%s", want, out)
		}
	}
	// Saved-at column renders in local time with the shared layout.
	wantSavedAt := time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC).Local().Format("2006-01-02 15:04")
	if !strings.Contains(out, wantSavedAt) {
		t.Errorf("render missing saved-at %q\n%s", wantSavedAt, out)
	}
	// Zero InGameEpoch must be blank, never the zero-time text.
	if strings.Contains(out, "0001-01-01") {
		t.Errorf("Meta-less entry rendered a zero in-game date\n%s", out)
	}
	// Newest-first: the given order is preserved on screen.
	iApollo := strings.Index(out, "Apollo run")
	iAuto := strings.Index(out, "[AUTOSAVE 1]")
	iLegacy := strings.Index(out, "(unnamed)")
	if !(iApollo < iAuto && iAuto < iLegacy) {
		t.Errorf("rows out of order: apollo=%d auto=%d legacy=%d", iApollo, iAuto, iLegacy)
	}
}

// TestSavesSaveModeShowsNewRow — save-mode prepends the "＋ New save…"
// row; load-mode does not.
func TestSavesSaveModeShowsNewRow(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeSave, savesFixture(), "default")
	if out := sc.Render(110, 40); !strings.Contains(out, "New save") {
		t.Errorf("save-mode render missing the New save row\n%s", out)
	}
	sc.Open(SavesModeLoad, savesFixture(), "default")
	if out := sc.Render(110, 40); strings.Contains(out, "New save") {
		t.Errorf("load-mode render must not show the New save row\n%s", out)
	}
}

// TestSavesSaveAsDefaultName — Enter on the New save row opens the
// name input prefilled with the default (active vessel + in-game
// day); Enter again commits a SavesActionSaveNew carrying it.
func TestSavesSaveAsDefaultName(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeSave, savesFixture(), "Kern Stack — 2000-03-14")

	if cmd := sc.HandleKey(keyEnter); cmd.Kind != SavesActionNone {
		t.Fatalf("enter on New save row returned %v, want None (opens naming)", cmd.Kind)
	}
	if out := sc.Render(110, 40); !strings.Contains(out, "Kern Stack — 2000-03-14") {
		t.Fatalf("naming input not prefilled with the default name\n%s", out)
	}
	cmd := sc.HandleKey(keyEnter)
	if cmd.Kind != SavesActionSaveNew || cmd.Name != "Kern Stack — 2000-03-14" {
		t.Fatalf("commit = %+v, want SaveNew with the default name", cmd)
	}
}

// TestSavesSaveAsWhitespaceFallsBack — an all-whitespace name falls
// back to the default rather than writing a nameless save.
func TestSavesSaveAsWhitespaceFallsBack(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeSave, nil, "fallback name")
	sc.HandleKey(keyEnter) // open naming
	sc.input.SetValue("   ")
	cmd := sc.HandleKey(keyEnter)
	if cmd.Kind != SavesActionSaveNew || cmd.Name != "fallback name" {
		t.Fatalf("commit = %+v, want SaveNew with the fallback default", cmd)
	}
}

// TestSavesOverwriteTargetsSelectedFile — with two rows sharing the
// SAME display name, Enter in save-mode confirms and the command
// carries the selected row's ID, not a name match.
func TestSavesOverwriteTargetsSelectedFile(t *testing.T) {
	twin := func(id string, at time.Time) save.SaveInfo {
		return save.SaveInfo{
			ID:   id,
			Meta: save.Meta{Name: "Twin", SavedAt: at},
			Lane: save.LaneNamed,
		}
	}
	entries := []save.SaveInfo{
		twin("save-2.json", time.Date(2026, 7, 11, 10, 0, 0, 0, time.UTC)),
		twin("save-1.json", time.Date(2026, 7, 11, 9, 0, 0, 0, time.UTC)),
	}
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeSave, entries, "default")

	// Rows: [＋ New save…, save-2, save-1] — select the OLDER twin.
	sc.HandleKey(keyDown)
	sc.HandleKey(keyDown)
	if cmd := sc.HandleKey(keyEnter); cmd.Kind != SavesActionNone {
		t.Fatalf("enter on named row returned %v, want None (confirm gate)", cmd.Kind)
	}
	if out := sc.Render(110, 40); !strings.Contains(out, "Overwrite") {
		t.Fatalf("no overwrite confirm prompt rendered\n%s", out)
	}
	cmd := sc.HandleKey(keyRunes("y"))
	if cmd.Kind != SavesActionOverwrite || cmd.ID != "save-1.json" {
		t.Fatalf("confirm = %+v, want Overwrite of save-1.json (the selected file)", cmd)
	}
}

// TestSavesReservedLaneNotOverwritable — Enter in save-mode on a
// reserved lane is a no-op (§F: loadable + deletable only).
func TestSavesReservedLaneNotOverwritable(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeSave, savesFixture(), "default")
	// Rows: [new, named, autosave, legacy] — cursor to the autosave.
	sc.HandleKey(keyDown)
	sc.HandleKey(keyDown)
	if cmd := sc.HandleKey(keyEnter); cmd.Kind != SavesActionNone {
		t.Fatalf("enter on reserved lane returned %v, want None", cmd.Kind)
	}
	if out := sc.Render(110, 40); strings.Contains(out, "Overwrite") {
		t.Fatalf("reserved lane opened an overwrite confirm\n%s", out)
	}
}

// TestSavesRenameNamedRow — r on a named row opens the input prefilled
// with the current name; committing returns SavesActionRename with the
// row's ID and the new name.
func TestSavesRenameNamedRow(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")
	if cmd := sc.HandleKey(keyRunes("r")); cmd.Kind != SavesActionNone {
		t.Fatalf("r returned %v, want None (opens naming)", cmd.Kind)
	}
	if got := sc.input.Value(); got != "Apollo run" {
		t.Fatalf("rename input prefill = %q, want the current name", got)
	}
	sc.input.SetValue("Apollo 11")
	cmd := sc.HandleKey(keyEnter)
	if cmd.Kind != SavesActionRename || cmd.ID != "save-100.json" || cmd.Name != "Apollo 11" {
		t.Fatalf("commit = %+v, want Rename save-100.json → Apollo 11", cmd)
	}
}

// TestSavesRenameReservedLaneDisabled — r on a reserved lane is a
// no-op: no naming state, no command (§F).
func TestSavesRenameReservedLaneDisabled(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")
	sc.HandleKey(keyDown) // cursor to the autosave row
	if cmd := sc.HandleKey(keyRunes("r")); cmd.Kind != SavesActionNone {
		t.Fatalf("r on reserved lane returned %v, want None", cmd.Kind)
	}
	if sc.state != savesStateBrowse {
		t.Fatalf("r on reserved lane left browse state (state=%v)", sc.state)
	}
}

// TestSavesLoadConfirmGate — the plan's "load path emits the confirm
// gate before the world swap": Enter renders the §H prompt and returns
// no command; n backs out; y returns SavesActionLoad with the ID.
func TestSavesLoadConfirmGate(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")

	if cmd := sc.HandleKey(keyEnter); cmd.Kind != SavesActionNone {
		t.Fatalf("enter returned %v before the confirm gate", cmd.Kind)
	}
	if out := sc.Render(110, 40); !strings.Contains(out, "Load and discard current state?") {
		t.Fatalf("missing the §H load confirm prompt\n%s", out)
	}
	if cmd := sc.HandleKey(keyRunes("n")); cmd.Kind != SavesActionNone {
		t.Fatalf("n returned %v, want None (back to browse)", cmd.Kind)
	}
	sc.HandleKey(keyEnter)
	cmd := sc.HandleKey(keyRunes("y"))
	if cmd.Kind != SavesActionLoad || cmd.ID != "save-100.json" {
		t.Fatalf("confirm = %+v, want Load save-100.json", cmd)
	}
}

// TestSavesDeleteConfirm — d opens a delete confirm (destructive, §H);
// y returns SavesActionDelete with the ID. Reserved lanes are
// deletable too (§F).
func TestSavesDeleteConfirm(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")
	sc.HandleKey(keyDown) // the autosave row — reserved lanes delete fine
	if cmd := sc.HandleKey(keyRunes("d")); cmd.Kind != SavesActionNone {
		t.Fatalf("d returned %v, want None (confirm gate)", cmd.Kind)
	}
	if out := sc.Render(110, 40); !strings.Contains(out, "Delete") {
		t.Fatalf("missing the delete confirm prompt\n%s", out)
	}
	cmd := sc.HandleKey(keyRunes("y"))
	if cmd.Kind != SavesActionDelete || cmd.ID != "autosave-1.json" {
		t.Fatalf("confirm = %+v, want Delete autosave-1.json", cmd)
	}
}

// TestSavesUnreadableRowNotLoadable — finding 6. An Unreadable entry
// (corrupt / newer-version) renders with its reason label but is NOT
// loadable or renamable; it stays deletable so the player can clear it.
func TestSavesUnreadableRowNotLoadable(t *testing.T) {
	entries := []save.SaveInfo{
		{ID: "save-newer.json", Lane: save.LaneNamed, Unreadable: true, Note: "newer version"},
	}
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, entries, "default")

	out := sc.Render(110, 40)
	if !strings.Contains(out, "unreadable (newer version)") {
		t.Fatalf("unreadable row missing its reason label\n%s", out)
	}
	// A directory holding a file must never read as empty.
	if strings.Contains(out, "no saves yet") {
		t.Errorf("rendered the empty-list message despite a listed (unreadable) entry\n%s", out)
	}

	// Enter must NOT open a load confirm — nothing to hydrate.
	if cmd := sc.HandleKey(keyEnter); cmd.Kind != SavesActionNone {
		t.Fatalf("enter on unreadable row returned %v, want None", cmd.Kind)
	}
	if strings.Contains(sc.Render(110, 40), "Load and discard") {
		t.Errorf("unreadable row opened a load confirm")
	}
	// Rename disabled.
	if cmd := sc.HandleKey(keyRunes("r")); cmd.Kind != SavesActionNone || sc.state != savesStateBrowse {
		t.Fatalf("r on unreadable row: cmd=%v state=%v, want no-op", cmd.Kind, sc.state)
	}
	// Delete still works — the player can clear the bad file.
	sc.HandleKey(keyRunes("d"))
	cmd := sc.HandleKey(keyRunes("y"))
	if cmd.Kind != SavesActionDelete || cmd.ID != "save-newer.json" {
		t.Fatalf("delete of unreadable = %+v, want Delete save-newer.json", cmd)
	}
}

// TestSavesSetEntriesClampsCursor — refreshing the list after a delete
// keeps the cursor in range.
func TestSavesSetEntriesClampsCursor(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")
	sc.HandleKey(keyDown)
	sc.HandleKey(keyDown) // cursor on the last row
	sc.SetEntries(savesFixture()[:1])
	if sc.cursor != 0 {
		t.Fatalf("cursor = %d after shrink to 1 row, want 0", sc.cursor)
	}
	// And an emptied list renders its placeholder without panicking.
	sc.SetEntries(nil)
	if out := sc.Render(110, 40); !strings.Contains(out, "no saves") {
		t.Errorf("empty list missing placeholder\n%s", out)
	}
	if cmd := sc.HandleKey(keyEnter); cmd.Kind != SavesActionNone {
		t.Errorf("enter on empty list returned %v, want None", cmd.Kind)
	}
}

// TestSavesEscCancels — esc in browse state returns Cancel; esc in a
// confirm or naming state only backs out to browse.
func TestSavesEscCancels(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")
	sc.HandleKey(keyEnter) // confirm-load
	if cmd := sc.HandleKey(keyEsc); cmd.Kind != SavesActionNone {
		t.Fatalf("esc in confirm returned %v, want None (back to browse)", cmd.Kind)
	}
	if cmd := sc.HandleKey(keyEsc); cmd.Kind != SavesActionCancel {
		t.Fatalf("esc in browse returned %v, want Cancel", cmd.Kind)
	}
}

// TestSavesClickTargets — mouse path: clicking a row moves the cursor;
// clicking the selected row activates it (same as Enter); the confirm
// [Yes]/[No] buttons and the title-row [Back] hit-test like the menu's.
func TestSavesClickTargets(t *testing.T) {
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, savesFixture(), "default")
	_ = sc.Render(110, 40) // populate click ranges

	// Click the second row: selects it, no action yet.
	btn := sc.rowBtns[1]
	col := (btn.colStart + btn.colEnd) / 2
	if cmd := sc.HandleClick(col, btn.row); cmd.Kind != SavesActionNone {
		t.Fatalf("first row click returned %v, want None", cmd.Kind)
	}
	if sc.cursor != 1 {
		t.Fatalf("cursor = %d after row click, want 1", sc.cursor)
	}
	// Click it again: activates (load confirm opens).
	_ = sc.Render(110, 40)
	if cmd := sc.HandleClick(col, btn.row); cmd.Kind != SavesActionNone {
		t.Fatalf("second row click returned %v, want None (confirm gate)", cmd.Kind)
	}
	if sc.state != savesStateConfirmLoad {
		t.Fatalf("state = %v after activating row click, want confirm-load", sc.state)
	}
	// [Yes] click commits the load of the clicked row.
	_ = sc.Render(110, 40)
	yes := sc.yesBtn
	cmd := sc.HandleClick((yes.colStart+yes.colEnd)/2, yes.row)
	if cmd.Kind != SavesActionLoad || cmd.ID != "autosave-1.json" {
		t.Fatalf("[Yes] click = %+v, want Load autosave-1.json", cmd)
	}
	// [Back] cancels out of the screen.
	_ = sc.Render(110, 40)
	back := sc.backBtn
	if cmd := sc.HandleClick((back.colStart+back.colEnd)/2, back.row); cmd.Kind != SavesActionCancel {
		t.Fatalf("[Back] click = %+v, want Cancel", cmd)
	}
}

// TestSavesPadCellDisplayWidth — finding 8. padCell measures cells by
// display width (not rune count) and always leaves a trailing gap, so a
// wide-rune (CJK/emoji) cell and an overflowing cell both occupy exactly
// width+gap columns instead of shoving the later columns out of line or
// fusing into them.
func TestSavesPadCellDisplayWidth(t *testing.T) {
	want := savesColName + lipgloss.Width(savesColGap) // width + gap, in display cells
	cases := map[string]string{
		"ascii-short": "abc",
		"cjk-wide":    "好好好", // 3 runes, 6 display cells
		"overflow":    strings.Repeat("x", 40),
		"exact-fill":  strings.Repeat("A", savesColName),
	}
	for name, in := range cases {
		got := padCell(in, savesColName)
		if w := lipgloss.Width(got); w != want {
			t.Errorf("%s: padCell width = %d display cells, want %d (width+gap)", name, w, want)
		}
	}
	// Overflow ellipsizes rather than fusing.
	over := strings.TrimRight(padCell(strings.Repeat("x", 40), savesColName), " ")
	if !strings.HasSuffix(over, "…") {
		t.Errorf("overflow padCell should ellipsize, got %q", over)
	}
}

// TestSavesRenderNeverExceedsHeight — a terminal too short for even the
// fixed chrome must still not emit more than `height` lines (else the
// alt-screen scrolls the title off the top). Render clamps as a safety
// net below the windowing floor.
func TestSavesRenderNeverExceedsHeight(t *testing.T) {
	var entries []save.SaveInfo
	for i := 0; i < 12; i++ {
		entries = append(entries, save.SaveInfo{
			ID:   fmt.Sprintf("save-%02d.json", i),
			Meta: save.Meta{Name: fmt.Sprintf("game-%02d", i), SavedAt: time.Now()},
			Lane: save.LaneNamed,
		})
	}
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, entries, "default")
	for _, h := range []int{5, 6, 7, 8, 10} {
		out := sc.Render(80, h)
		if lines := strings.Count(out, "\n") + 1; lines > h {
			t.Errorf("Render(_, %d) = %d lines, want <= %d\n%s", h, lines, h, out)
		}
	}
}

// TestSavesListWindowsToHeight — finding 7. A list taller than the height
// budget renders only a cursor-centred window (so the title/header never
// scroll off the top of the alt-screen), with "more" indicators; the
// cursor row stays visible and off-window rows record no click target.
func TestSavesListWindowsToHeight(t *testing.T) {
	var entries []save.SaveInfo
	for i := 0; i < 20; i++ {
		entries = append(entries, save.SaveInfo{
			ID:   fmt.Sprintf("save-%02d.json", i),
			Meta: save.Meta{Name: fmt.Sprintf("game-%02d", i), SavedAt: time.Now()},
			Lane: save.LaneNamed,
		})
	}
	sc := NewSavesScreen(savesTheme())
	sc.Open(SavesModeLoad, entries, "default")
	for i := 0; i < 10; i++ { // cursor into the middle
		sc.HandleKey(keyDown)
	}

	out := sc.Render(80, 20)
	if lines := strings.Count(out, "\n") + 1; lines > 20 {
		t.Errorf("rendered %d lines, want <= height 20 (windowing failed)\n%s", lines, out)
	}
	if !strings.Contains(out, "game-10") {
		t.Errorf("cursor row game-10 not in the window\n%s", out)
	}
	if strings.Contains(out, "game-00") {
		t.Errorf("far row game-00 rendered despite the window\n%s", out)
	}
	if !strings.Contains(out, "more") {
		t.Errorf("no ↑/↓ more indicator rendered\n%s", out)
	}
	// Off-window rows carry no click target; the cursor row does.
	if sc.rowBtns[0].set {
		t.Error("off-window row 0 recorded a click target — clicks would misfire")
	}
	if !sc.rowBtns[10].set {
		t.Error("cursor row 10 has no click target")
	}
}

