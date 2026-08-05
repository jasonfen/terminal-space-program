package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestPlainArrowsAreBoundToPan pins the ADR 0042 keymap split: the plain
// ↑↓←→ bindings belong to Pan, not NextBody/PrevBody. If a future edit puts
// an arrow back on NextBody/PrevBody, key.Matches would route it to body
// browse again ahead of (or instead of) Pan.
func TestPlainArrowsAreBoundToPan(t *testing.T) {
	keys := DefaultKeymap()

	wantPan := map[string][]string{
		"left":  keys.PanLeft.Keys(),
		"right": keys.PanRight.Keys(),
		"up":    keys.PanUp.Keys(),
		"down":  keys.PanDown.Keys(),
	}
	for arrow, bound := range wantPan {
		if len(bound) != 1 || bound[0] != arrow {
			t.Errorf("Pan binding for %q = %v, want exactly [%q]", arrow, bound, arrow)
		}
	}

	// Body-browse gave up its arrow aliases (#347 §2) — h/l alone.
	for _, k := range keys.NextBody.Keys() {
		if k == "right" {
			t.Errorf("NextBody still carries the %q alias; body-browse should live on h/l alone", k)
		}
	}
	for _, k := range keys.PrevBody.Keys() {
		if k == "left" {
			t.Errorf("PrevBody still carries the %q alias; body-browse should live on h/l alone", k)
		}
	}
	if got := keys.NextBody.Keys(); len(got) != 1 || got[0] != "l" {
		t.Errorf("NextBody.Keys() = %v, want exactly [\"l\"]", got)
	}
	if got := keys.PrevBody.Keys(); len(got) != 1 || got[0] != "h" {
		t.Errorf("PrevBody.Keys() = %v, want exactly [\"h\"]", got)
	}
}

// TestArrowKeysNoLongerBrowseBodies exercises the same rule through the
// actual dispatch path in app.go: pressing the plain arrow keys must not
// move the body-browse cursor (a.selectedBody) anymore — that's Pan's job
// now, which lives on the OrbitView and has no visible effect on
// selectedBody.
func TestArrowKeysNoLongerBrowseBodies(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if n := len(a.world.System().Bodies); n < 2 {
		t.Skip("need at least 2 bodies in the loaded system to discriminate")
	}
	start := a.selectedBody

	for _, kt := range []tea.KeyType{tea.KeyRight, tea.KeyLeft, tea.KeyUp, tea.KeyDown} {
		a.Update(tea.KeyMsg{Type: kt})
		if a.selectedBody != start {
			t.Errorf("key %v moved selectedBody: %d -> %d (arrows should no longer alias body-browse)", kt, start, a.selectedBody)
		}
	}
}

// TestBodyBrowseStillWorksOnHL is the flip side: h/l — already bound there
// pre-#347 — still drive the body-browse cursor after the arrow aliases are
// removed.
func TestBodyBrowseStillWorksOnHL(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	n := len(a.world.System().Bodies)
	if n < 2 {
		t.Skip("need at least 2 bodies in the loaded system to discriminate")
	}
	start := a.selectedBody

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	if want := (start + 1) % n; a.selectedBody != want {
		t.Errorf("'l' did not advance selectedBody: got %d, want %d", a.selectedBody, want)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	if a.selectedBody != start {
		t.Errorf("'h' did not step selectedBody back: got %d, want %d", a.selectedBody, start)
	}
}

// TestArrowKeysDispatchToOrbitViewPan is a smoke test for the app.go wiring:
// the plain arrow keys must reach OrbitView.Pan* rather than being dropped
// or misrouted. We can't reach into OrbitView's unexported panOffset from
// this package, so this asserts the observable contract instead — pressing
// an arrow must not panic, must not change Focus/Target, and must leave
// a.active on the orbit screen (Pan is silent view state, not a screen
// transition or a sim mutation).
func TestArrowKeysDispatchToOrbitViewPan(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wantActive := a.active
	wantFocus := a.world.Focus
	wantTarget := a.world.Target

	for _, kt := range []tea.KeyType{tea.KeyRight, tea.KeyLeft, tea.KeyUp, tea.KeyDown} {
		if _, _, panicked := safeUpdate(a, tea.KeyMsg{Type: kt}); panicked {
			t.Fatalf("arrow key %v panicked through Update", kt)
		}
	}
	if a.active != wantActive {
		t.Errorf("arrow keys changed the active screen: %v -> %v", wantActive, a.active)
	}
	if a.world.Focus != wantFocus {
		t.Errorf("arrow keys mutated Focus: %+v -> %+v (Pan is view-only)", wantFocus, a.world.Focus)
	}
	if a.world.Target != wantTarget {
		t.Errorf("arrow keys mutated Target: %+v -> %+v (Pan is view-only)", wantTarget, a.world.Target)
	}
}

// safeUpdate runs a.Update and reports whether it panicked, without letting
// the panic escape (recover), so the caller can assert "no panic" as a
// normal test failure rather than a crashed test binary.
func safeUpdate(a *App, msg tea.Msg) (m tea.Model, cmd tea.Cmd, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			panicked = true
		}
	}()
	m, cmd = a.Update(msg)
	return m, cmd, false
}
