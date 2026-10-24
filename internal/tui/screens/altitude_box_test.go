package screens

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// Only digits and a point are an altitude; enter on a positive number closes
// with metres; enter on nothing keeps the box up; esc closes without a number.
func TestAltitudeBoxTypingAndAnswer(t *testing.T) {
	b := NewAltitudeBox(sessionTheme())
	b.Open("raise to:", 290)
	if !b.CapturingText() || b.Value() != "290" {
		t.Fatalf("open: capturing %v value %q", b.CapturingText(), b.Value())
	}
	for range "290" {
		b.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	if done, ok, _ := b.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}); done || ok || !b.CapturingText() {
		t.Fatal("enter on an empty box must keep it open")
	}
	for _, k := range []string{"5", "x", "5", "-", ".", "5", "e"} {
		b.HandleKey(runes(k))
	}
	if b.Value() != "55.5" {
		t.Fatalf("value = %q, want 55.5 (letters and signs filtered)", b.Value())
	}
	done, ok, altM := b.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !done || !ok || altM != 55500 || b.CapturingText() {
		t.Errorf("enter: done %v ok %v altM %v capturing %v", done, ok, altM, b.CapturingText())
	}
	b.Open("raise to:", 100)
	if done, ok, _ := b.HandleKey(tea.KeyMsg{Type: tea.KeyEsc}); !done || ok || b.CapturingText() {
		t.Error("esc must close without an answer")
	}
	b.Open("raise to:", 0)
	b.HandleKey(runes("0"))
	if done, _, _ := b.HandleKey(tea.KeyMsg{Type: tea.KeyEnter}); done {
		t.Error("an altitude of zero must not close the box")
	}
}

func TestAltitudeBoxRendersLabelAndUnit(t *testing.T) {
	b := NewAltitudeBox(sessionTheme())
	b.Open("raise to:", 550)
	out := b.Render()
	for _, want := range []string{"Orbit altitude", "raise to:", "550", "km", "[enter] ok  [esc] cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("box lacks %q:\n%s", want, out)
		}
	}
}

func TestAltitudeKm(t *testing.T) {
	for in, want := range map[float64]string{550e3: "550 km", 550.5e3: "550.5 km", 412345: "412.3 km", 55500: "55.5 km"} {
		if got := AltitudeKm(in); got != want {
			t.Errorf("AltitudeKm(%v) = %q, want %q", in, got, want)
		}
	}
}
