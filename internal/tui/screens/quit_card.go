package screens

// The quit card (Jason 2026-10-05: "when I press q, can a quit menu showing
// the save before quitting? option selections display instead of putting
// the options down in the bottom of the screen?"). It replaces the #474
// bottom-band prompt with a card centred over the screen. Since ADR 0054 it
// is an instance of the shared Card: ↑/↓ + enter pick, y / n / esc answer
// directly, and an action row asks once more before it acts.

// The quit card's row IDs, what the App answers on.
const (
	QuitRowSave   = "save-and-quit"       // [y]: autosave, then quit
	QuitRowNoSave = "quit-without-saving" // [n]: quit writing nothing (host only)
	QuitRowStay   = "stay"                // [esc]: close the card, keep playing
)

// QuitCardW is the quit card's width, the shared card's.
const QuitCardW = CardW

// NewQuitCard returns the quit prompt's card on its first row. A multiplayer
// guest has no "quit without saving": their flight is written on the way out
// whatever they choose, so their card offers quit and stay only.
func NewQuitCard(th Theme, guest bool) *Card {
	stay := CardRow{ID: QuitRowStay, Label: "stay", Key: "esc", Letters: []string{"esc"}, Out: true}
	if guest {
		return NewCard(th, "Quit?", "Your flight saves automatically.", []CardRow{
			{ID: QuitRowSave, Label: "quit", Key: "y", Letters: []string{"y", "Y"}},
			stay,
		}, 0)
	}
	return NewCard(th, "Save before quitting?", "", []CardRow{
		{ID: QuitRowSave, Label: "save and quit", Key: "y", Letters: []string{"y", "Y"}},
		{ID: QuitRowNoSave, Label: "quit without saving", Key: "n", Letters: []string{"n", "N"}},
		stay,
	}, 0)
}
