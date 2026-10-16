package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// ADR 0056 S2: the palette find row is a free-text surface, so it joins
// capturingText(): global keys (the boss-key backtick, `?` help, flight
// letters) type into it instead of firing.
func TestVABFindCapturesGlobalKeys(t *testing.T) {
	a := helpTestApp(t)
	press(a, "esc")
	press(a, "b")
	if a.active != screenVAB {
		t.Fatalf("setup: active = %v, want the VAB", a.active)
	}
	if a.capturingText() {
		t.Fatal("capturingText() true before / is pressed")
	}
	press(a, "/")
	if !a.capturingText() {
		t.Fatal("capturingText() false with the find row open")
	}
	typeText(a, "`?qm")
	if a.active != screenVAB {
		t.Fatalf("a global key left the VAB while typing in find (active=%v)", a.active)
	}
	if !strings.Contains(a.View(), "find: `?qm") {
		t.Errorf("global keys did not type into the find field:\n%s", a.View())
	}
	press(a, "esc")
	if a.capturingText() || a.active != screenVAB {
		t.Errorf("esc: capturing=%v active=%v, want find closed and still in the VAB", a.capturingText(), a.active)
	}
}

// A terminal hands fast typing over as one multi-rune message; the find field
// takes it as characters, not as an unknown named key.
func TestVABFindTakesRunsOfTypedCharacters(t *testing.T) {
	a := helpTestApp(t)
	press(a, "esc")
	press(a, "b")
	press(a, "/")
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("rel")})
	if !strings.Contains(a.View(), "find: rel") {
		t.Errorf("a multi-rune message did not type into find:\n%s", a.View())
	}
}
