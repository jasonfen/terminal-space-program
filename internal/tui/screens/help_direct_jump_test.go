package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// Review #629 L4: from a form's help page, a digit jump leaves the form's own
// page, so esc must stop meaning "back to the form". One rule: a digit jump
// drops direct mode, esc then steps to the index (Back reports true), and the
// footer says so.
func TestHelpDigitJumpFromFormPageDropsDirect(t *testing.T) {
	h := NewHelp(chipTestTheme())
	h.OpenFor("SPAWN FORM")
	if !h.direct {
		t.Fatalf("setup: OpenFor did not set direct (page title changed?)")
	}
	if h.Back() {
		t.Fatalf("direct page: esc must leave help at once")
	}
	h.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if h.page != 2 {
		t.Fatalf("digit 3 did not jump: page = %d", h.page)
	}
	if h.direct {
		t.Errorf("a digit jump left direct set: esc would claim to return to the form from page 3")
	}
	if strings.Contains(h.footer(), "back to where you were") {
		t.Errorf("footer still promises the form after a jump: %q", h.footer())
	}
	if !h.Back() || h.page != helpIndexPage {
		t.Errorf("esc after a jump must step to the index (page = %d)", h.page)
	}
}
