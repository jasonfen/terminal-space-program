package screens

import "strings"

// The quit card (Jason 2026-10-05: "when I press q, can a quit menu showing
// the save before quitting? option selections display instead of putting
// the options down in the bottom of the screen?"). It replaces the #474
// bottom-band prompt with a card centred over the screen, the same shape as
// the pause card: the question, the choices as rows with their letter keys,
// and the legend. ↑/↓ + enter pick; y / n / esc still answer directly.

// QuitChoice is what the player picked on the quit card.
type QuitChoice int

const (
	QuitSaveAndQuit QuitChoice = iota // [y]: autosave, then quit
	QuitNoSave                        // [n]: quit writing nothing (host only)
	QuitStay                          // [esc]: close the card, keep playing
)

// QuitCardW is the quit card's width, the pause card's.
const QuitCardW = MenuCardW

// QuitCard is the quit prompt's card. A multiplayer guest has no "quit
// without saving": their flight is written on the way out whatever they
// choose, so their card offers Quit and Stay only.
type QuitCard struct {
	theme  Theme
	guest  bool
	cursor int
}

// NewQuitCard returns the quit card.
func NewQuitCard(th Theme) *QuitCard { return &QuitCard{theme: th} }

// Open resets the card to its first row for a host or a guest.
func (q *QuitCard) Open(guest bool) {
	q.guest = guest
	q.cursor = 0
}

type quitRow struct {
	label, key string
	choice     QuitChoice
}

func (q *QuitCard) rows() []quitRow {
	if q.guest {
		return []quitRow{{"Quit", "y", QuitSaveAndQuit}, {"Stay", "esc", QuitStay}}
	}
	return []quitRow{
		{"Save and quit", "y", QuitSaveAndQuit},
		{"Quit without saving", "n", QuitNoSave},
		{"Stay", "esc", QuitStay},
	}
}

// Up / Down move the ▸, wrapping at either end.
func (q *QuitCard) Up() {
	n := len(q.rows())
	q.cursor = (q.cursor - 1 + n) % n
}

func (q *QuitCard) Down() { q.cursor = (q.cursor + 1) % len(q.rows()) }

// Choice is the row under the ▸.
func (q *QuitCard) Choice() QuitChoice { return q.rows()[q.cursor].choice }

// Render returns the card: a rounded QuitCardW-wide box titled with the
// question, a note for a guest, the choices with their keys in a column,
// and the legend.
func (q *QuitCard) Render() string {
	title := "  " + q.theme.Title.Render("Save before quitting?")
	lines := []string{""}
	if q.guest {
		title = "  " + q.theme.Title.Render("Quit?")
		lines = []string{"  " + q.theme.Dim.Render("Your flight saves automatically."), ""}
	}
	for i, r := range q.rows() {
		cursor := "  "
		label := r.label
		if i == q.cursor {
			cursor = q.theme.Primary.Render("▸") + " "
			label = q.theme.Primary.Render(label)
		}
		lines = append(lines, "  "+cursor+padCells(label, 24)+q.theme.Dim.Render(r.key))
	}
	lines = append(lines, "", "  "+q.theme.Footer.Render("[↑/↓] pick  [enter] choose"))
	return strings.Join(formBox(q.theme, title, lines, QuitCardW), "\n")
}
