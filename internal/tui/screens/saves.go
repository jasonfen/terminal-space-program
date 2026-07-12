package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/save"
)

// SavesScreen is the unified Saves browser (v0.26 / ADR 0033 §F): one
// list of every entry in the saves directory — named saves plus the
// reserved quicksave/autosave lanes — with metadata columns, reached
// from both pause-menu items. The entry point selects load-mode vs
// save-mode; save-mode prepends a "＋ New save…" row (Save-As) and
// turns Enter-on-a-named-row into an overwrite.
//
// It follows the MenuAction mold: the screen holds only UI state (the
// App passes the []save.SaveInfo listing at Open, the way the spawn
// form receives ListDesigns), and every player intent is returned as
// a SavesCommand for App.Update to dispatch against the save package —
// the screen never touches the filesystem or the world.
type SavesScreen struct {
	theme Theme
	mode  SavesMode
	state savesState

	entries     []save.SaveInfo // as listed by save.List — already newest-first
	cursor      int             // row index; save-mode row 0 is the New-save row
	defaultName string          // Save-As prefill: active vessel + in-game day (§B)

	// pendingID/pendingName capture the row a confirm or rename is
	// about; the display name is echoed in the prompt.
	pendingID   string
	pendingName string

	// input is the shared name-entry field (Save-As + rename), the
	// same bubbles/textinput the maneuver form uses.
	input textinput.Model

	// Click-target ranges, recomputed each Render (menu buttonRange
	// pattern). rowBtns aligns 1:1 with the visible rows.
	backBtn buttonRange
	rowBtns []buttonRange
	yesBtn  buttonRange
	noBtn   buttonRange
}

// SavesMode is the entry-point flag: which pause-menu item opened the
// screen (ADR 0033 §F).
type SavesMode int

const (
	SavesModeLoad SavesMode = iota
	SavesModeSave
)

// savesState tracks the screen's sub-state: browsing the list, one of
// the three confirm gates (§H — load and delete confirm; overwrite is
// destructive the same way), or name entry (Save-As / rename).
type savesState int

const (
	savesStateBrowse savesState = iota
	savesStateConfirmLoad
	savesStateConfirmDelete
	savesStateConfirmOverwrite
	savesStateNameNew
	savesStateRename
)

// SavesActionKind enumerates the intents the screen hands back to the
// App. Every destructive kind is only ever emitted after its confirm
// gate (§H).
type SavesActionKind int

const (
	SavesActionNone      SavesActionKind = iota
	SavesActionCancel                    // esc / [Back] — leave the screen
	SavesActionLoad                      // load ID (post-confirm)
	SavesActionDelete                    // delete ID (post-confirm)
	SavesActionRename                    // rename ID → Name (no gate — benign Meta edit)
	SavesActionSaveNew                   // Save-As: write a NEW named save called Name
	SavesActionOverwrite                 // overwrite ID in place (post-confirm)
)

// SavesCommand is one dispatched intent: the kind plus the target file
// ID (never a display-name match — duplicate names are legal, §B) and
// the entered/display name where relevant.
type SavesCommand struct {
	Kind SavesActionKind
	ID   string
	Name string
}

func NewSavesScreen(th Theme) *SavesScreen {
	in := textinput.New()
	in.CharLimit = 48
	in.Width = 40
	return &SavesScreen{theme: th, input: in}
}

// Open (re)initialises the screen for a fresh visit: the entry-point
// mode, the current directory listing (newest-first, as save.List
// returns it), and the Save-As default name computed by the App from
// the live world (active vessel + in-game day).
func (sc *SavesScreen) Open(mode SavesMode, entries []save.SaveInfo, defaultName string) {
	sc.mode = mode
	sc.entries = entries
	sc.defaultName = defaultName
	sc.cursor = 0
	sc.state = savesStateBrowse
	sc.pendingID, sc.pendingName = "", ""
}

// SetEntries refreshes the listing in place (after a delete / rename /
// Save-As) and clamps the cursor to the new row count.
func (sc *SavesScreen) SetEntries(entries []save.SaveInfo) {
	sc.entries = entries
	if max := sc.rowCount() - 1; sc.cursor > max {
		sc.cursor = max
	}
	if sc.cursor < 0 {
		sc.cursor = 0
	}
}

// Mode reports the entry-point mode the screen was opened in.
func (sc *SavesScreen) Mode() SavesMode { return sc.mode }

// CapturingText reports whether the screen is in a free-text sub-state
// (Save-As / rename name entry) — every keystroke is literal input for
// the name field, so the App must not keyboard-layout-normalize it or
// let global hotkeys (the backtick boss key) intercept it (findings 3 +
// 9). Browse and confirm states are NOT capturing: their keys are
// commands and route normally.
func (sc *SavesScreen) CapturingText() bool {
	return sc.state == savesStateNameNew || sc.state == savesStateRename
}

// EntryCount reports the number of listed saves (excluding the
// save-mode New-save row).
func (sc *SavesScreen) EntryCount() int { return len(sc.entries) }

// hasNewRow: save-mode prepends the persistent "＋ New save…" row.
func (sc *SavesScreen) hasNewRow() bool { return sc.mode == SavesModeSave }

// rowCount is the number of selectable rows (entries + the New-save
// row in save-mode).
func (sc *SavesScreen) rowCount() int {
	n := len(sc.entries)
	if sc.hasNewRow() {
		n++
	}
	return n
}

// entryAt maps a row index to its SaveInfo, accounting for the
// save-mode New-save row at index 0. ok=false on the New-save row or
// an empty list.
func (sc *SavesScreen) entryAt(row int) (save.SaveInfo, bool) {
	if sc.hasNewRow() {
		row--
	}
	if row < 0 || row >= len(sc.entries) {
		return save.SaveInfo{}, false
	}
	return sc.entries[row], true
}

// HandleKey maps a keypress to a SavesCommand. Most keys only mutate
// screen state (cursor, confirm gates, the name input) and return
// Kind == SavesActionNone; a command with any other Kind is a
// finalised intent for the App to dispatch.
func (sc *SavesScreen) HandleKey(msg tea.KeyMsg) SavesCommand {
	key := msg.String()
	switch sc.state {
	case savesStateNameNew, savesStateRename:
		switch key {
		case "esc":
			sc.state = savesStateBrowse
			return SavesCommand{}
		case "enter":
			name := strings.TrimSpace(sc.input.Value())
			naming := sc.state
			sc.state = savesStateBrowse
			if naming == savesStateNameNew {
				// Empty / whitespace falls back to the default rather
				// than minting a nameless save (§B default name).
				if name == "" {
					name = sc.defaultName
				}
				return SavesCommand{Kind: SavesActionSaveNew, Name: name}
			}
			if name == "" {
				return SavesCommand{} // rename to nothing: keep the old name
			}
			return SavesCommand{Kind: SavesActionRename, ID: sc.pendingID, Name: name}
		default:
			var cmd tea.Cmd
			sc.input, cmd = sc.input.Update(msg)
			_ = cmd // cursor-blink command dropped — static cursor is fine here
			return SavesCommand{}
		}

	case savesStateConfirmLoad, savesStateConfirmDelete, savesStateConfirmOverwrite:
		switch key {
		case "y", "Y", "enter":
			return sc.commitConfirm()
		case "n", "N", "esc":
			sc.state = savesStateBrowse
		}
		return SavesCommand{}
	}

	// Browse state.
	switch key {
	case "esc":
		return SavesCommand{Kind: SavesActionCancel}
	case "up":
		if sc.cursor > 0 {
			sc.cursor--
		}
	case "down":
		if sc.cursor < sc.rowCount()-1 {
			sc.cursor++
		}
	case "enter":
		return sc.activateCursor()
	case "d", "D":
		// Delete works on every lane — reserved lanes are managed but
		// deletable (§F); the confirm gate covers the destructiveness.
		if info, ok := sc.entryAt(sc.cursor); ok {
			sc.pendingID, sc.pendingName = info.ID, displaySaveName(info)
			sc.state = savesStateConfirmDelete
		}
	case "r", "R":
		// Rename is a benign Meta edit (no confirm, §H) but is
		// disabled on the reserved lanes (§F) — they carry no player
		// name by design.
		if info, ok := sc.entryAt(sc.cursor); ok && info.Lane == save.LaneNamed && !info.Unreadable {
			sc.pendingID, sc.pendingName = info.ID, displaySaveName(info)
			sc.beginNaming(savesStateRename, info.Meta.Name)
		}
	}
	return SavesCommand{}
}

// HandleClick maps a (col, row) click to the same intents as the
// keyboard: [Back] cancels; a list-row click selects, and a click on
// the already-selected row activates it (Enter-equivalent); the
// confirm [Yes]/[No] buttons commit / back out.
func (sc *SavesScreen) HandleClick(col, row int) SavesCommand {
	if sc.backBtn.Hit(col, row) {
		return SavesCommand{Kind: SavesActionCancel}
	}
	switch sc.state {
	case savesStateBrowse:
		for i, btn := range sc.rowBtns {
			if btn.Hit(col, row) {
				if i == sc.cursor {
					return sc.activateCursor()
				}
				sc.cursor = i
				return SavesCommand{}
			}
		}
	case savesStateConfirmLoad, savesStateConfirmDelete, savesStateConfirmOverwrite:
		switch {
		case sc.yesBtn.Hit(col, row):
			return sc.commitConfirm()
		case sc.noBtn.Hit(col, row):
			sc.state = savesStateBrowse
		}
	}
	return SavesCommand{}
}

// activateCursor is the Enter action on the current row: New-save row
// → open naming prefilled with the default; load-mode row → load
// confirm (§H); save-mode named row → overwrite confirm; save-mode
// reserved row → no-op (§F: never manually overwritable).
func (sc *SavesScreen) activateCursor() SavesCommand {
	if sc.hasNewRow() && sc.cursor == 0 {
		sc.beginNaming(savesStateNameNew, sc.defaultName)
		return SavesCommand{}
	}
	info, ok := sc.entryAt(sc.cursor)
	if !ok {
		return SavesCommand{}
	}
	if info.Unreadable {
		// A corrupt / newer-version file can't be loaded or overwritten
		// in place; it's listed only so it's visible and deletable (§C).
		return SavesCommand{}
	}
	sc.pendingID, sc.pendingName = info.ID, displaySaveName(info)
	if sc.mode == SavesModeSave {
		if info.Lane != save.LaneNamed {
			return SavesCommand{} // reserved lanes: loadable + deletable only (§F)
		}
		sc.state = savesStateConfirmOverwrite
		return SavesCommand{}
	}
	sc.state = savesStateConfirmLoad
	return SavesCommand{}
}

// commitConfirm resolves the active confirm gate into its finalised
// command (shared by the keyboard y/enter path and the [Yes] click).
func (sc *SavesScreen) commitConfirm() SavesCommand {
	st := sc.state
	sc.state = savesStateBrowse
	switch st {
	case savesStateConfirmLoad:
		return SavesCommand{Kind: SavesActionLoad, ID: sc.pendingID, Name: sc.pendingName}
	case savesStateConfirmDelete:
		return SavesCommand{Kind: SavesActionDelete, ID: sc.pendingID, Name: sc.pendingName}
	case savesStateConfirmOverwrite:
		return SavesCommand{Kind: SavesActionOverwrite, ID: sc.pendingID, Name: sc.pendingName}
	}
	return SavesCommand{}
}

// beginNaming enters a name-entry state with the input prefilled and
// focused, cursor at the end so typing appends.
func (sc *SavesScreen) beginNaming(state savesState, prefill string) {
	sc.state = state
	sc.input.SetValue(prefill)
	sc.input.CursorEnd()
	sc.input.Focus()
}

// displaySaveName is the name cell / prompt label for an entry: the
// player-facing Meta.Name for named saves ("(unnamed)" for a Meta-less
// legacy file), and a lane badge for the reserved quicksave/autosave
// lanes — which carry no player name by design (§D).
func displaySaveName(info save.SaveInfo) string {
	if info.Unreadable {
		// Kept within the name column (savesColName=26) so it isn't
		// ellipsised: "unreadable (newer version)" is exactly 26.
		if info.Note != "" {
			return "unreadable (" + info.Note + ")"
		}
		return "unreadable"
	}
	switch info.Lane {
	case save.LaneQuicksave:
		return "[QUICKSAVE]"
	case save.LaneAutosave:
		// "autosave-2.json" → "[AUTOSAVE 2]"
		n := strings.TrimSuffix(strings.TrimPrefix(info.ID, "autosave-"), ".json")
		return "[AUTOSAVE " + n + "]"
	}
	if info.Meta.Name == "" {
		return "(unnamed)"
	}
	return info.Meta.Name
}

// formatInGameDate renders Meta.InGameEpoch with the same layout the
// orbit HUD's clock chip inlines (SimTime.Format("2006-01-02")); a
// zero epoch (Meta-less legacy header, imported save) renders blank —
// the date is unknowable without hydrating the Payload.
func formatInGameDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}

// formatSavedAt renders the wall-clock write time in the player's
// local timezone; blank when unknown.
func formatSavedAt(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// Column layout: marker(2) + name(26) + savedat(18) + ingame(12) + vessel,
// each column followed by savesColGap so adjacent cells never fuse.
const (
	savesColName    = 26
	savesColSavedAt = 18
	savesColInGame  = 12
	savesColGap     = "  " // explicit inter-column gap (finding 8)
)

// padCell renders s in a fixed-width column: display-width-truncated
// (ellipsis on overflow), padded to width, then an explicit 2-space gap.
// Width is measured with lipgloss.Width (via truncWidth), so wide runes
// (CJK, emoji) occupy the right number of display cells instead of being
// over-counted one-per-rune — which previously misaligned every column
// after a wide cell. The trailing gap stops a full-width cell from
// fusing into the next column. Apply to plain, un-styled text.
func padCell(s string, width int) string {
	s = truncWidth(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s + savesColGap
}

// listWindow returns the [start,end) slice of the n rows to render so
// the list plus the screen's fixed chrome fits within height, always
// keeping the cursor row inside the window. A non-positive height (no
// budget) or a list that already fits returns the full range. chrome
// budgets the non-list lines: title + blank + header above, and the
// blank + the tallest state tail (a §H confirm block) plus the two
// "N more" indicators below — so the window never overflows even mid
// confirm.
func (sc *SavesScreen) listWindow(n, height int) (start, end int) {
	if height <= 0 {
		return 0, n
	}
	const chrome = 11
	capRows := height - chrome
	if capRows < 1 {
		capRows = 1
	}
	if capRows >= n {
		return 0, n
	}
	start = sc.cursor - capRows/2
	if start < 0 {
		start = 0
	}
	end = start + capRows
	if end > n {
		end = n
		start = end - capRows
	}
	if start < 0 {
		start = 0
	}
	return start, end
}

// Render composes the screen: title + [Back], the mode header, the
// column header, a cursor-windowed slice of the rows, and the
// state-specific footer (confirm prompt / name input / key legend).
// height bounds the total output so a long list never overflows the
// alt-screen (which truncates from the TOP, hiding the title/header);
// the list is windowed around the cursor to fit. A non-positive height
// disables the clamp (shows everything) — handy for tests and any caller
// with no height budget. Click-target ranges are recorded in absolute
// row terms, menu-style.
func (sc *SavesScreen) Render(width, height int) string {
	var lines []string

	// Row 0: title + right-aligned [Back] (menu pattern).
	titleText := "saves — load a game"
	if sc.mode == SavesModeSave {
		titleText = "saves — save your game"
	}
	const backLabel = "[Back]"
	pad := width - len([]rune(titleText)) - len([]rune(backLabel))
	if pad < 1 {
		pad = 1
	}
	backCol := len([]rune(titleText)) + pad
	sc.backBtn = buttonRange{row: 0, colStart: backCol, colEnd: backCol + len([]rune(backLabel)), set: true}
	lines = append(lines, sc.theme.Title.Render(titleText)+strings.Repeat(" ", pad)+sc.theme.Primary.Render(backLabel))
	lines = append(lines, "")

	// Column header.
	header := "  " + padCell("NAME", savesColName) + padCell("SAVED", savesColSavedAt) +
		padCell("IN-GAME", savesColInGame) + "VESSEL"
	lines = append(lines, sc.theme.Dim.Render(header))

	// Rows. Assemble every selectable row first (New-save row + entries),
	// then render only a cursor-centred window that fits the height so the
	// whole screen stays within the alt-screen (finding 7). rowBtns for
	// off-window rows stay unset, so a click can never land on a row that
	// isn't drawn.
	type savesRow struct {
		body   string
		dimmed bool
	}
	var rows []savesRow
	if sc.hasNewRow() {
		rows = append(rows, savesRow{body: "＋ New save…"})
	}
	for _, info := range sc.entries {
		body := padCell(displaySaveName(info), savesColName) +
			padCell(formatSavedAt(info.Meta.SavedAt), savesColSavedAt) +
			padCell(formatInGameDate(info.Meta.InGameEpoch), savesColInGame) +
			info.Meta.ActiveVesselName
		rows = append(rows, savesRow{body: body, dimmed: info.Unreadable})
	}

	sc.rowBtns = make([]buttonRange, len(rows))
	start, end := sc.listWindow(len(rows), height)
	if start > 0 {
		lines = append(lines, sc.theme.Dim.Render(fmt.Sprintf("  ↑ %d more", start)))
	}
	for i := start; i < end; i++ {
		marker := "  "
		style := sc.theme.Primary
		if rows[i].dimmed {
			style = sc.theme.Dim
		}
		if i == sc.cursor {
			marker = "▸ "
			if !rows[i].dimmed {
				style = sc.theme.Warning
			}
		}
		sc.rowBtns[i] = buttonRange{row: len(lines), colStart: 0, colEnd: width, set: true}
		lines = append(lines, clipLine(marker+style.Render(rows[i].body), width))
	}
	if end < len(rows) {
		lines = append(lines, sc.theme.Dim.Render(fmt.Sprintf("  ↓ %d more", len(rows)-end)))
	}
	if len(rows) == 0 {
		lines = append(lines, sc.theme.Dim.Render("  (no saves yet — F5 quicksaves, or save from the menu)"))
	}
	lines = append(lines, "")

	// State-specific tail: confirm prompt / name input / key legend.
	sc.yesBtn.set, sc.noBtn.set = false, false
	switch sc.state {
	case savesStateConfirmLoad:
		lines = sc.appendConfirm(lines, "Load and discard current state?")
	case savesStateConfirmDelete:
		lines = sc.appendConfirm(lines, fmt.Sprintf("Delete %s? This cannot be undone.", sc.pendingName))
	case savesStateConfirmOverwrite:
		lines = sc.appendConfirm(lines, fmt.Sprintf("Overwrite '%s'?", sc.pendingName))
	case savesStateNameNew, savesStateRename:
		prompt := "name the new save:"
		if sc.state == savesStateRename {
			prompt = "rename to:"
		}
		lines = append(lines, "  "+prompt+" "+sc.input.View())
		lines = append(lines, "")
		lines = append(lines, sc.theme.Footer.Render("[enter] confirm · [esc] cancel"))
	default:
		legend := "[enter] load · [d] delete · [r] rename · [↑/↓] move · [esc] back"
		if sc.mode == SavesModeSave {
			legend = "[enter] new save / overwrite · [d] delete · [r] rename · [↑/↓] move · [esc] back"
		}
		lines = append(lines, sc.theme.Footer.Render(legend))
	}

	for i, ln := range lines {
		lines[i] = clipLine(ln, width)
	}
	return strings.Join(lines, "\n")
}

// appendConfirm appends a §H-style Yes/No confirm block and records
// the button click targets (menu renderConfirm pattern).
func (sc *SavesScreen) appendConfirm(lines []string, prompt string) []string {
	lines = append(lines, "  "+sc.theme.Alert.Render(prompt))
	lines = append(lines, "")
	const indent = "  "
	const yesLabel = "[Yes]"
	const noLabel = "[No]"
	const gap = "   "
	yesCol := len([]rune(indent))
	noCol := yesCol + len([]rune(yesLabel)) + len([]rune(gap))
	row := len(lines)
	sc.yesBtn = buttonRange{row: row, colStart: yesCol, colEnd: yesCol + len([]rune(yesLabel)), set: true}
	sc.noBtn = buttonRange{row: row, colStart: noCol, colEnd: noCol + len([]rune(noLabel)), set: true}
	lines = append(lines, indent+sc.theme.Primary.Render(yesLabel)+gap+sc.theme.Primary.Render(noLabel))
	lines = append(lines, "")
	lines = append(lines, sc.theme.Footer.Render("[y]es / [n]o / [esc] cancel"))
	return lines
}
