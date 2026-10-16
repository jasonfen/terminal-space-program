package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The card (ADR 0054, #633). Every question the game asks is a card centred
// over the dimmed screen: the question as the title, the choices as short
// rows with their keys in a column, a ▸ on one row. It is the quit card's
// shape (#565) as a shared widget, so every ask draws, moves and answers the
// same way.
//
// Two steps (Jason 2026-10-12, call 1): picking any action row asks once more
// on the same card, with `yes, <verb>` over the way out and the ▸ on the way
// out; only a second pick acts. The way-out row (cancel / stay / keep it)
// acts at once, from either step.

// CardW is every card's width: the pause card's.
const CardW = MenuCardW

const (
	cardTitleW  = CardW - 4 // the title's room: two indent cells and the box edges
	cardNoteW   = CardW - 6
	cardLabelW  = 26 // the label column when the row has a key; a row with no key may use the whole line
	cardRowW    = CardW - 6
	cardMinRows = 9 // a two- or three-row card is 9 rows tall
)

// CardRow is one choice.
type CardRow struct {
	// ID names the row for the caller's answer; the Card never reads it.
	ID string
	// Label is the verb: one to four words, lower case except a proper name.
	Label string
	// Key is the key column's text ("y", "n  esc"); "" for a letterless row.
	Key string
	// Letters are the keys that pick this row at once ("y", "Y"; "n", "N",
	// "esc"). A destructive extra row has none: it takes ↓/click and enter.
	Letters []string
	// Out marks the way-out row (cancel / stay / keep it), which acts at once.
	Out bool
}

// Card is one question. The zero value is not usable; build it with NewCard.
type Card struct {
	theme  Theme
	title  string
	note   string
	rows   []CardRow
	cursor int  // index into visible()
	asking bool // the second step is showing
	picked int  // the action row (an index into rows) the second step confirms
}

// NewCard returns a card on its first step with the ▸ on rows[open]. title is
// the question (it is cut to fit); note is at most two dim lines and may be
// empty.
func NewCard(th Theme, title, note string, rows []CardRow, open int) *Card {
	if open < 0 || open >= len(rows) {
		open = 0
	}
	return &Card{theme: th, title: title, note: note, rows: rows, cursor: open}
}

// Confirming reports whether the second step is showing.
func (c *Card) Confirming() bool { return c.asking }

// visible is the rows on screen now: all of them on the first step; on the
// second, `yes, <verb>` (carrying the picked row's letters, so a key pressed
// twice confirms) over the way out.
func (c *Card) visible() []CardRow {
	if !c.asking {
		return c.rows
	}
	p := c.rows[c.picked]
	out := c.rows[c.wayOut()]
	return []CardRow{
		{ID: p.ID, Label: "yes, " + p.Label, Key: p.Key, Letters: p.Letters},
		out,
	}
}

// wayOut is the index of the way-out row (the last row marked Out, else the
// last row).
func (c *Card) wayOut() int {
	for i := len(c.rows) - 1; i >= 0; i-- {
		if c.rows[i].Out {
			return i
		}
	}
	return len(c.rows) - 1
}

// Up / Down move the ▸, wrapping at either end.
func (c *Card) Up() {
	n := len(c.visible())
	c.cursor = (c.cursor - 1 + n) % n
}

func (c *Card) Down() { c.cursor = (c.cursor + 1) % len(c.visible()) }

// Choice is the row under the ▸, as an index into the rows the card was built
// with. On the second step it is the picked row (the ▸ on `yes, ...`) or the
// way out.
func (c *Card) Choice() int { return c.original(c.cursor) }

func (c *Card) original(vis int) int {
	if !c.asking {
		return vis
	}
	if vis == 0 {
		return c.picked
	}
	return c.wayOut()
}

// RowID is the ID of row i of the rows the card was built with.
func (c *Card) RowID(i int) string { return c.rows[i].ID }

// Pick picks the visible row vis. done is false when the pick only moved the
// card to its second step; otherwise row is the index (into the rows the card
// was built with) the caller must answer, and the card is finished.
func (c *Card) Pick(vis int) (row int, done bool) {
	vs := c.visible()
	if vis < 0 || vis >= len(vs) {
		return -1, false
	}
	idx := c.original(vis)
	if c.rows[idx].Out || c.asking {
		return idx, true
	}
	c.asking, c.picked = true, idx
	c.cursor = 1 // the second step opens on the way out
	return idx, false
}

// Enter picks the row under the ▸.
func (c *Card) Enter() (row int, done bool) { return c.Pick(c.cursor) }

// Letter picks the visible row that carries key s. ok is false when no
// visible row does.
func (c *Card) Letter(s string) (row int, done, ok bool) {
	for i, r := range c.visible() {
		for _, l := range r.Letters {
			if l == s {
				row, done = c.Pick(i)
				return row, done, true
			}
		}
	}
	return -1, false, false
}

// noteLines is the note wrapped to the card, at most two lines.
func (c *Card) noteLines() []string {
	ls := wrapText(c.note, cardNoteW)
	if len(ls) > 2 {
		ls = ls[:2]
	}
	return ls
}

// firstRowLine is the line, counted from the box's top edge (line 0), the
// first row sits on: the edge, the title, then the note and its blank, or a
// blank.
func (c *Card) firstRowLine() int {
	if n := len(c.noteLines()); n > 0 {
		return 2 + n + 1
	}
	return 3
}

// Height is the card's height in lines. It holds between the two steps, and a
// two-row card with no note is padded to the 9 a three-row card has.
func (c *Card) Height() int {
	h := c.firstRowLine() + len(c.rows) + 3 // rows, a blank, the legend, the bottom edge
	if h < cardMinRows {
		h = cardMinRows
	}
	return h
}

// HitRow maps a click, counted from the card's top-left corner, to the
// visible row under it. A click on the box's edges, its text or its blank
// lines is not a hit.
func (c *Card) HitRow(col, row int) (int, bool) {
	if col < 1 || col >= CardW-1 {
		return -1, false
	}
	i := row - c.firstRowLine()
	if i < 0 || i >= len(c.visible()) {
		return -1, false
	}
	return i, true
}

// Render returns the card: a rounded CardW-wide box titled with the question,
// an optional dim note, the choices with their keys in a column, and the
// legend.
func (c *Card) Render() string {
	title := "  " + c.theme.Title.Render(cardTitle(c.title))
	var lines []string
	if ns := c.noteLines(); len(ns) > 0 {
		for _, n := range ns {
			lines = append(lines, "  "+c.theme.Dim.Render(n))
		}
	}
	lines = append(lines, "")
	for i, r := range c.visible() {
		cursor := "  "
		w := cardLabelW
		if r.Key == "" {
			w = cardRowW
		}
		label := truncWidth(r.Label, w-1)
		if i == c.cursor {
			cursor = c.theme.Primary.Render("▸") + " "
			label = c.theme.Primary.Render(label)
		}
		lines = append(lines, "  "+cursor+padCells(label, w)+c.theme.Dim.Render(r.Key))
	}
	for len(lines)+5 < c.Height() { // rows pad to the height of the first step
		lines = append(lines, "")
	}
	lines = append(lines, "", "  "+c.theme.Footer.Render("[↑/↓] pick  [enter] choose"))
	return strings.Join(formBox(c.theme, title, lines, CardW), "\n")
}

// cardTitle cuts a question to the room the title row has (36 cells), keeping
// the "?" it ends in: handles and vessel names have no length limit.
func cardTitle(t string) string {
	room := cardTitleW - 2
	if lipgloss.Width(t) <= room {
		return t
	}
	if stem, ok := strings.CutSuffix(t, "?"); ok {
		return truncWidth(stem, room-1) + "?"
	}
	return truncWidth(t, room)
}
