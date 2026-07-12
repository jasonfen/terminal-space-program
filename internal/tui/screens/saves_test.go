package screens

import (
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
	out := sc.Render(110)

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
	if out := sc.Render(110); !strings.Contains(out, "New save") {
		t.Errorf("save-mode render missing the New save row\n%s", out)
	}
	sc.Open(SavesModeLoad, savesFixture(), "default")
	if out := sc.Render(110); strings.Contains(out, "New save") {
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
	if out := sc.Render(110); !strings.Contains(out, "Kern Stack — 2000-03-14") {
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
	if out := sc.Render(110); !strings.Contains(out, "Overwrite") {
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
	if out := sc.Render(110); strings.Contains(out, "Overwrite") {
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
	if out := sc.Render(110); !strings.Contains(out, "Load and discard current state?") {
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
	if out := sc.Render(110); !strings.Contains(out, "Delete") {
		t.Fatalf("missing the delete confirm prompt\n%s", out)
	}
	cmd := sc.HandleKey(keyRunes("y"))
	if cmd.Kind != SavesActionDelete || cmd.ID != "autosave-1.json" {
		t.Fatalf("confirm = %+v, want Delete autosave-1.json", cmd)
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
	if out := sc.Render(110); !strings.Contains(out, "no saves") {
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
	_ = sc.Render(110) // populate click ranges

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
	_ = sc.Render(110)
	if cmd := sc.HandleClick(col, btn.row); cmd.Kind != SavesActionNone {
		t.Fatalf("second row click returned %v, want None (confirm gate)", cmd.Kind)
	}
	if sc.state != savesStateConfirmLoad {
		t.Fatalf("state = %v after activating row click, want confirm-load", sc.state)
	}
	// [Yes] click commits the load of the clicked row.
	_ = sc.Render(110)
	yes := sc.yesBtn
	cmd := sc.HandleClick((yes.colStart+yes.colEnd)/2, yes.row)
	if cmd.Kind != SavesActionLoad || cmd.ID != "autosave-1.json" {
		t.Fatalf("[Yes] click = %+v, want Load autosave-1.json", cmd)
	}
	// [Back] cancels out of the screen.
	_ = sc.Render(110)
	back := sc.backBtn
	if cmd := sc.HandleClick((back.colStart+back.colEnd)/2, back.row); cmd.Kind != SavesActionCancel {
		t.Fatalf("[Back] click = %+v, want Cancel", cmd)
	}
}
