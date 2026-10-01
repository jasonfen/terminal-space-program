package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// helpApp opens a fresh app sized to the Design Size floor and presses F1
// through the real Update path (#494).
func helpApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyF1})
	if a.active != screenHelp {
		t.Fatalf("F1 did not open help (active=%v)", a.active)
	}
	return a
}

func helpPress(a *App, s string) {
	switch s {
	case "esc":
		a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	case "enter":
		a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	case "down":
		a.Update(tea.KeyMsg{Type: tea.KeyDown})
	case "F1":
		a.Update(tea.KeyMsg{Type: tea.KeyF1})
	default:
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	}
}

// TestHelpOpensOnIndex (#494, grill G1 Q1): F1 opens on an index whose
// first row is "Your first flight", every section is numbered with a
// when-clause, and the footer carries a position line instead of a bare
// scroll cue.
func TestHelpOpensOnIndex(t *testing.T) {
	a := helpApp(t)
	v := a.View()
	for _, want := range []string{
		"Your first flight", "MANUAL FLIGHT", "READOUT GLOSSARY",
		"you are flying by hand", "INDEX", "16 pages",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("index missing %q:\n%s", want, v)
		}
	}
	first := strings.Index(v, "Your first flight")
	general := strings.Index(v, "GENERAL")
	if first < 0 || general < 0 || first > general {
		t.Errorf("first row is not Your first flight (first=%d general=%d)", first, general)
	}
	if strings.Contains(v, "toggle this help") {
		t.Error("F1 still opens on the flat key list")
	}
}

// TestHelpIndexDigitJumpEscAndReopen drives the real key path: a digit
// jumps to a page, esc returns to the index, esc again closes, and
// reopening lands on the index rather than the page you left.
func TestHelpIndexDigitJumpEscAndReopen(t *testing.T) {
	a := helpApp(t)
	helpPress(a, "6") // 1 = first flight, 2 = GENERAL ... 6 = MANUAL FLIGHT
	v := a.View()
	if !strings.Contains(v, "MANUAL FLIGHT · 6 of 16") || !strings.Contains(v, "throttle full") {
		t.Errorf("digit 6 did not open MANUAL FLIGHT with a position line:\n%s", v)
	}
	helpPress(a, "esc")
	if a.active != screenHelp || !strings.Contains(a.View(), "Your first flight") {
		t.Fatalf("esc from a page should return to the index (active=%v)", a.active)
	}
	helpPress(a, "esc")
	if a.active == screenHelp {
		t.Fatal("esc on the index should close help")
	}
	helpPress(a, "F1")
	helpPress(a, "6")
	helpPress(a, "F1") // F1 closes from a page
	if a.active == screenHelp {
		t.Fatal("F1 on a page should close help")
	}
	helpPress(a, "F1")
	if !strings.Contains(a.View(), "INDEX") {
		t.Error("reopening help did not land on the index")
	}
}

// TestHelpIndexCursorReachesPagesPastNine: only nine digit keys exist,
// so pages 10-16 are reached with down + enter.
func TestHelpIndexCursorReachesPagesPastNine(t *testing.T) {
	a := helpApp(t)
	for i := 0; i < 15; i++ {
		helpPress(a, "down")
	}
	helpPress(a, "enter")
	if v := a.View(); !strings.Contains(v, "READOUT GLOSSARY · 16 of 16") {
		t.Errorf("down x15 + enter did not open the last page:\n%s", v)
	}
}

// TestHelpFirstFlightPage (#494, grill G1 Q2): orbit start first, then the
// pad, each line naming its moment; the pad lights the engine with [b] and
// says [space] only drops stages; no em dashes.
func TestHelpFirstFlightPage(t *testing.T) {
	a := helpApp(t)
	helpPress(a, "1")
	v := a.View()
	if !strings.Contains(v, "Your first flight · 1 of 16") {
		t.Errorf("no position line on the first-flight page:\n%s", v)
	}
	orbit := strings.Index(v, "ORBIT START")
	pad := strings.Index(v, "THE PAD")
	if orbit < 0 || pad < 0 || orbit > pad {
		t.Fatalf("orbit start must precede the pad (orbit=%d pad=%d):\n%s", orbit, pad, v)
	}
	for _, want := range []string{
		"TARGET reads Moon", "[space] only drops stages", "light the engine and lift off",
		"ORBIT READY", "F1 → MANUAL FLIGHT for the rest",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("first-flight page missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "[space] to drop the clamps") {
		t.Error("page repeats the stale space-lights-the-engine claim")
	}
	body := v[strings.Index(v, "Your first flight"):]
	if strings.Contains(body, "—") {
		t.Errorf("em dash in first-flight page text:\n%s", body)
	}
}
