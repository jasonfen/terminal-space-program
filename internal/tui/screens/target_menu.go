package screens

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TargetMenu is the `t` card (ADR 0066 decision 2): the targets grouped as
// VESSELS, SITES ON THE <body>, PARTNERS, BODIES, nearest first inside each.
// It is a new overlay screen: the pause card's shape, but the clock keeps
// running. The App owns when it opens and what a commit does.
type TargetMenu struct {
	theme    Theme
	body     string
	rows     []sim.TargetRow
	expanded bool        // the SITES `… N more` row was opened
	cursor   int         // index into items()
	lz       map[int]int // chosen LZ per row index (0 = auto), ADR 0068 decision 6
	// rename is the text field state; renaming is true while it is up.
	renaming bool
	renameID uint64
	input    []rune
}

// TargetMenuAction is what a key asks the App to do.
type TargetMenuAction int

const (
	TargetMenuNone TargetMenuAction = iota
	TargetMenuClose
	TargetMenuCommit // aim at Row()
	TargetMenuDelete // ask to delete Row() (an own site)
	TargetMenuRename // rename Row()'s site to RenameText()
)

const (
	targetMenuSiteCap   = 5  // sites listed before `… N more`
	targetMenuBodyLines = 14 // list lines before the card scrolls
	targetMenuNameRunes = 24
)

func NewTargetMenu(th Theme) *TargetMenu { return &TargetMenu{theme: th} }

// Open fills the card from the World and puts the cursor on the opening row.
func (m *TargetMenu) Open(w *sim.World) {
	var open int
	m.rows, open = w.TargetMenu()
	m.body = ""
	if c := w.ActiveCraft(); c != nil {
		m.body = strings.ToUpper(c.Primary.EnglishName)
	}
	m.expanded, m.renaming, m.input = false, false, nil
	m.lz = map[int]int{}
	for i, r := range m.rows { // reopening shows the LZ already chosen
		if r.Kind == sim.TargetSite && r.Target.SameAim(w.Target) {
			m.lz[i] = w.Target.LZ
		}
	}
	m.cursor = 0
	if open >= 0 {
		for i, it := range m.items() {
			if it.more == 0 && it.row == open {
				m.cursor = i
			}
		}
	}
	m.reveal(open)
}

// Refresh rebuilds the rows after a delete or rename, keeping the cursor in range.
func (m *TargetMenu) Refresh(w *sim.World) {
	m.rows, _ = w.TargetMenu()
	m.lz = map[int]int{}
	if n := len(m.items()); m.cursor >= n {
		m.cursor = n - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// reveal expands the sites list when the opening row would hide behind `… N more`.
func (m *TargetMenu) reveal(open int) {
	if open < 0 {
		return
	}
	for _, it := range m.items() {
		if it.more == 0 && it.row == open {
			return
		}
	}
	m.expanded = true
	for i, it := range m.items() {
		if it.more == 0 && it.row == open {
			m.cursor = i
		}
	}
}

type targetItem struct {
	row   int // index into rows
	more  int // hidden sites, when this item is the `… N more` row
	group sim.TargetGroup
}

// items is the selectable lines in order: every row, with the sites past the
// cap folded into one `… N more` item until it is opened.
func (m *TargetMenu) items() []targetItem {
	var out []targetItem
	sites := 0
	for i, r := range m.rows {
		if r.Group == sim.GroupSites && !m.expanded {
			if sites == targetMenuSiteCap {
				continue
			}
			sites++
		}
		out = append(out, targetItem{row: i, group: r.Group})
	}
	if !m.expanded {
		total := 0
		last := -1
		for i, r := range m.rows {
			if r.Group == sim.GroupSites {
				total++
				last = i
			}
		}
		if total > targetMenuSiteCap {
			// insert after the last shown site row
			at := 0
			for i, it := range out {
				if it.group == sim.GroupSites {
					at = i + 1
				}
			}
			more := targetItem{row: last, more: total - targetMenuSiteCap, group: sim.GroupSites}
			out = append(out[:at], append([]targetItem{more}, out[at:]...)...)
		}
	}
	return out
}

// CapturingText is true while the rename field is up.
func (m *TargetMenu) CapturingText() bool { return m.renaming }

// Row is the highlighted row, ok=false on a `… N more` item or an empty card.
func (m *TargetMenu) Row() (sim.TargetRow, bool) {
	it := m.items()
	if m.cursor < 0 || m.cursor >= len(it) || it[m.cursor].more > 0 {
		return sim.TargetRow{}, false
	}
	r := m.rows[it[m.cursor].row]
	r.Target.LZ = m.lz[it[m.cursor].row]
	return r, true
}

// pickable is true on a site row with more than one LZ: the arrows act there.
func pickable(r sim.TargetRow) bool {
	return r.Kind == sim.TargetSite && !r.Wreck && r.LZs > 1
}

// RenameText is the typed name.
func (m *TargetMenu) RenameText() string { return strings.TrimSpace(string(m.input)) }

// HandleKey maps a key string to an action. Up/down move, enter and `t`
// take the highlighted row (or open `… N more`), esc closes, `r` renames and
// `d` deletes a site you own. Every other key is swallowed by the App.
func (m *TargetMenu) HandleKey(s string) TargetMenuAction {
	if m.renaming {
		switch {
		case s == "esc":
			m.renaming, m.input = false, nil
		case s == "enter":
			if m.RenameText() == "" {
				return TargetMenuNone
			}
			m.renaming = false
			return TargetMenuRename
		case s == "backspace":
			if len(m.input) > 0 {
				m.input = m.input[:len(m.input)-1]
			}
		case s == "space" || s == " ":
			m.typeRune(' ')
		case utf8.RuneCountInString(s) == 1:
			r, _ := utf8.DecodeRuneInString(s)
			m.typeRune(r)
		}
		return TargetMenuNone
	}
	n := len(m.items())
	switch s {
	case "esc":
		return TargetMenuClose
	case "up":
		if n > 0 {
			m.cursor = (m.cursor - 1 + n) % n
		}
	case "down":
		if n > 0 {
			m.cursor = (m.cursor + 1) % n
		}
	case "left", "right":
		if r, ok := m.Row(); ok && pickable(r) {
			i := m.items()[m.cursor].row
			if s == "right" {
				m.lz[i] = min(m.lz[i]+1, r.LZs)
			} else {
				m.lz[i] = max(m.lz[i]-1, 0)
			}
		}
	case "enter", "t":
		if n == 0 {
			return TargetMenuNone
		}
		if m.items()[m.cursor].more > 0 {
			m.expanded = true
			return TargetMenuNone
		}
		return TargetMenuCommit
	case "r", "d":
		if r, ok := m.Row(); ok && r.Own && r.Group == sim.GroupSites {
			if s == "d" {
				return TargetMenuDelete
			}
			m.renaming, m.renameID, m.input = true, r.SiteID, []rune(r.Name)
		}
	}
	return TargetMenuNone
}

func (m *TargetMenu) typeRune(r rune) {
	if len(m.input) < targetMenuNameRunes && r >= ' ' {
		m.input = append(m.input, r)
	}
}

var targetGroupTitle = map[sim.TargetGroup]string{
	sim.GroupVessels: "VESSELS", sim.GroupPartners: "PARTNERS", sim.GroupBodies: "BODIES",
}

func targetGlyph(r sim.TargetRow) string {
	switch {
	case r.Kind == sim.TargetSite && r.Wreck:
		return "☠"
	case r.Kind == sim.TargetSite:
		return "⚑"
	case r.Group == sim.GroupVessels:
		return "➤"
	case r.Group == sim.GroupPartners:
		return "◌"
	}
	return "●"
}

// targetDist is a distance in metres as the card shows it: m under a km,
// one decimal under 10 km, whole km with thousands commas, then Mkm from a million km.
func targetDist(m float64) string {
	switch km := m / 1000; {
	case m < 1000:
		return fmt.Sprintf("%.0f m", m)
	case km < 10:
		return fmt.Sprintf("%.1f km", km)
	case km >= 1e6:
		return fmt.Sprintf("%.1f Mkm", km/1e6)
	default:
		s := fmt.Sprintf("%.0f", km)
		for i := len(s) - 3; i > 0; i -= 3 {
			s = s[:i] + "," + s[i:]
		}
		return s + " km"
	}
}

// Render draws the card: a rounded CardW box, groups with their rows, the
// footer. The list scrolls to keep the cursor in view past targetMenuBodyLines.
func (m *TargetMenu) Render() string {
	const inner = CardW - 2
	var lines []string // body lines
	curLine := 0
	var group sim.TargetGroup = -1
	for i, it := range m.items() {
		if it.group != group {
			group = it.group
			title := targetGroupTitle[group]
			if group == sim.GroupSites {
				title = "SITES ON THE " + m.body
			}
			lines = append(lines, "  "+m.theme.Dim.Render(title))
		}
		cur := "  "
		if i == m.cursor {
			cur = m.theme.Primary.Render("▸") + " "
			curLine = len(lines)
		}
		if it.more > 0 {
			lines = append(lines, "  "+cur+m.theme.Dim.Render(fmt.Sprintf("… %d more", it.more)))
			continue
		}
		r := m.rows[it.row]
		name := r.Name
		if r.Count > 1 {
			name = fmt.Sprintf("%s (x%d)", name, r.Count)
		}
		if r, _ := m.Row(); i == m.cursor && pickable(r) {
			sel := "auto"
			if n := m.lz[it.row]; n > 0 {
				sel = fmt.Sprintf("LZ-%d", n)
			}
			name = padCells(name, 11) + " ◂ " + sel + " ▸"
		}
		name = padCells(name, 20)
		if i == m.cursor {
			name = m.theme.Primary.Render(name)
		} else if r.Group == sim.GroupPartners {
			name = m.theme.Dim.Render(name)
		}
		d := targetDist(r.DistM)
		lines = append(lines, "  "+cur+name+" "+targetGlyph(r)+" "+strings.Repeat(" ", max(0, 9-len(d)))+m.theme.Dim.Render(d))
	}
	if len(lines) == 0 {
		lines = []string{"  " + m.theme.Dim.Render("no targets")}
	}
	if len(lines) > targetMenuBodyLines {
		top := curLine - targetMenuBodyLines/2
		top = max(0, min(top, len(lines)-targetMenuBodyLines))
		lines = lines[top : top+targetMenuBodyLines]
	}
	footer := "[↑/↓] pick  [enter] or [t] target"
	r, ok := m.Row()
	switch {
	case m.renaming:
		footer = "name: " + string(m.input) + "▏"
	case ok && r.Own && r.Group == sim.GroupSites && pickable(r):
		footer = "[←/→] LZ [r] rename [d] delete"
	case ok && r.Own && r.Group == sim.GroupSites:
		footer = "[r] rename [d] delete [esc] close"
	case ok && pickable(r):
		footer = "[↑/↓] pick [←/→] LZ [enter] target"
	}
	body := append([]string{""}, lines...)
	body = append(body, "", "  "+m.theme.Footer.Render(footer))
	return strings.Join(formBox(m.theme, "  "+m.theme.Title.Render("TARGET"), body, CardW), "\n")
}
