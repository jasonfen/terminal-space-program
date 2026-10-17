package screens

// The quit card (Jason 2026-10-05: "when I press q, can a quit menu showing
// the save before quitting? option selections display instead of putting
// the options down in the bottom of the screen?"). It replaces the #474
// bottom-band prompt with a card centred over the screen. Since ADR 0054 it
// is an instance of the shared Card: ↑/↓ + enter pick, y / n / esc answer
// directly, and an action row asks once more before it acts.

// The quit flow's row IDs, what the App answers on (#699). The flow is two
// one-step cards: "Save before quitting?" and "Are you sure?".
const (
	QuitRowSave   = "save-and-quit" // Save card, Yes: autosave, then quit
	QuitRowNoSave = "no-save"       // Save card, No: ask Are you sure?
	QuitRowStay   = "stay"          // Back on either card, and the guest's stay
	QuitRowSure   = "sure-quit"     // Sure card, Yes: quit writing nothing
	QuitRowUnsure = "unsure"        // Sure card, No: back to the Save card
)

// QuitCardW is the quit card's width, the shared card's.
const QuitCardW = CardW

// quitBack is the Back row both quit cards end on.
func quitBack() CardRow {
	return CardRow{ID: QuitRowStay, Label: "Back", Key: "esc", Letters: []string{"esc"}, Out: true}
}

// NewQuitCard returns the quit prompt's first card. A host gets "Save before
// quitting?" with Yes / No / Back, opening on Yes (keeps the save). A
// multiplayer guest has no "no": their flight is written on the way out
// whatever they choose, so their card offers quit and stay only, and keeps
// the shared two-step confirm.
func NewQuitCard(th Theme, guest bool) *Card {
	if guest {
		return NewCard(th, "Quit?", "Your flight saves automatically.", []CardRow{
			{ID: QuitRowSave, Label: "quit", Key: "y", Letters: []string{"y", "Y"}},
			{ID: QuitRowStay, Label: "stay", Key: "esc", Letters: []string{"esc"}, Out: true},
		}, 0)
	}
	return NewCard(th, "Save before quitting?", "", []CardRow{
		{ID: QuitRowSave, Label: "Yes", Key: "y", Letters: []string{"y", "Y"}},
		{ID: QuitRowNoSave, Label: "No", Key: "n", Letters: []string{"n", "N"}},
		quitBack(),
	}, 0).OneStep()
}

// NewQuitSureCard returns the second card, "Are you sure?": Yes quits without
// saving, No (the default) goes back to the first card, Back returns to where
// the quit was opened.
func NewQuitSureCard(th Theme) *Card {
	return NewCard(th, "Are you sure?", "", []CardRow{
		{ID: QuitRowSure, Label: "Yes", Key: "y", Letters: []string{"y", "Y"}},
		{ID: QuitRowUnsure, Label: "No", Key: "n", Letters: []string{"n", "N"}},
		quitBack(),
	}, 1).OneStep()
}
