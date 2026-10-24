package screens

import (
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// AltitudeBox is the L key's typed-altitude box (ADR 0059 decision 4): one
// number in km, in the maneuver planner's textinput idiom. App hosts it (it is
// not a screen) and treats it as a free-text surface while it is open.
type AltitudeBox struct {
	theme Theme
	label string
	input textinput.Model
	open  bool
}

// NewAltitudeBox returns a closed box.
func NewAltitudeBox(th Theme) *AltitudeBox {
	in := textinput.New()
	in.CharLimit = 8
	in.Width = 10
	// bubbles deleteWordForward indexes out of range on the last rune and
	// crashes the game; nothing here needs it.
	in.KeyMap.DeleteWordForward.SetEnabled(false)
	return &AltitudeBox{theme: th, input: in}
}

// Open shows the box with label ("raise to:") and def (km) typed in.
func (b *AltitudeBox) Open(label string, defKm float64) {
	b.label, b.open = label, true
	b.input.SetValue(strconv.FormatFloat(defKm, 'f', -1, 64))
	b.input.CursorEnd()
	b.input.Focus()
}

// Value is the text typed so far.
func (b *AltitudeBox) Value() string { return b.input.Value() }

// Close hides the box.
func (b *AltitudeBox) Close() { b.open, b.label = false, ""; b.input.Blur() }

// CapturingText is true while the box is up: App.capturingText must say so.
func (b *AltitudeBox) CapturingText() bool { return b != nil && b.open }

// HandleKey takes a key while the box is up. done is true when the box closed:
// ok means enter on a positive number, altM is that altitude in metres.
func (b *AltitudeBox) HandleKey(m tea.KeyMsg) (done, ok bool, altM float64) {
	switch m.String() {
	case "esc":
		b.Close()
		return true, false, 0
	case "enter":
		km, err := strconv.ParseFloat(strings.TrimSpace(b.input.Value()), 64)
		if err != nil || km <= 0 {
			return false, false, 0 // stays open: nothing typed yet that means an altitude
		}
		b.Close()
		return true, true, km * 1000
	}
	if m.Type == tea.KeyRunes {
		keep := m.Runes[:0:0]
		for _, r := range m.Runes {
			if r >= '0' && r <= '9' || r == '.' {
				keep = append(keep, r)
			}
		}
		if len(keep) == 0 {
			return false, false, 0
		}
		m.Runes = keep
	}
	b.input, _ = b.input.Update(m)
	return false, false, 0
}

// Render is the box: a CardW-wide rounded frame under the label, the typed
// number, its unit, and the legend.
func (b *AltitudeBox) Render() string {
	lines := []string{
		"",
		"  " + b.theme.Dim.Render(b.label) + " " + b.input.View() + " " + b.theme.Dim.Render("km"),
		"",
		"  " + b.theme.Footer.Render("[enter] ok  [esc] cancel"),
	}
	return strings.Join(formBox(b.theme, "  "+b.theme.Title.Render("Orbit altitude"), lines, CardW), "\n")
}

// AltitudeKm is an orbit altitude in metres as the raise text writes it:
// "550 km", "550.5 km", no trailing zero.
func AltitudeKm(altM float64) string {
	return strconv.FormatFloat(math.Round(altM/100)/10, 'f', -1, 64) + " km"
}
