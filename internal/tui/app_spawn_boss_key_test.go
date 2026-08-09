package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"testing"
)

// Review finding #5: capturingText()'s own doc comment says "Extend here
// as future free-text surfaces (VAB, spawn, search) land" — the ALTITUDE
// typed-edit box (ADR 0044 / S4) landed without that extension, so the
// global boss-key intercept (app.go, above the per-screen dispatch) fired
// on a literal backtick typed mid-altitude, swapping the whole screen to
// the boss shell instead of doing nothing (digits/backspace are the only
// keys handleAltInputKey accepts; a backtick is silently ignored there,
// same as any other letter).

func newSpawnApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if a.active != screenSpawn {
		t.Fatalf("setup: 'n' did not open the spawn form (active=%v)", a.active)
	}
	return a
}

func pressMsg(a *App, msg tea.KeyMsg) {
	a.Update(msg)
}

// openSpawnAltitudeBox drives the spawn form's field focus to ALTITUDE and
// opens its typed-edit box via the real Update path (not by poking
// a.spawn's private state), matching how a player actually gets there.
func openSpawnAltitudeBox(t *testing.T, a *App) {
	t.Helper()
	for i := 0; i < 3; i++ {
		pressMsg(a, tea.KeyMsg{Type: tea.KeyTab})
	}
	pressMsg(a, tea.KeyMsg{Type: tea.KeyEnter})
	if !a.spawn.CapturingText() {
		t.Fatalf("setup: tab x3 + enter did not open the ALTITUDE edit box")
	}
	if a.active != screenSpawn {
		t.Fatalf("setup: opening the altitude box left the spawn screen (active=%v)", a.active)
	}
}

// TestCapturingTextTrueWhileAltitudeBoxOpen pins the new predicate itself
// against the App's own screenSpawn field, mirroring the existing
// screenSaves/screenSession cases in capturingText's switch.
func TestCapturingTextTrueWhileAltitudeBoxOpen(t *testing.T) {
	a := newSpawnApp(t)
	if a.capturingText() {
		t.Fatalf("capturingText() = true before the ALTITUDE box is opened")
	}
	openSpawnAltitudeBox(t, a)
	if !a.capturingText() {
		t.Fatalf("capturingText() = false while the ALTITUDE edit box is open")
	}
	pressMsg(a, tea.KeyMsg{Type: tea.KeyEnter}) // commit + close (empty buffer reverts)
	if a.capturingText() {
		t.Fatalf("capturingText() = true after the ALTITUDE box closed")
	}
}

// TestBossKeyInertWhileTypingAltitude is the end-to-end regression: a
// literal backtick typed into an open ALTITUDE box must not swap the
// screen to the boss shell.
func TestBossKeyInertWhileTypingAltitude(t *testing.T) {
	a := newSpawnApp(t)
	openSpawnAltitudeBox(t, a)

	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'`'}})

	if a.active == screenBoss {
		t.Fatalf("a backtick typed while the ALTITUDE box was open opened the boss shell")
	}
	if a.active != screenSpawn {
		t.Fatalf("active screen = %v after a backtick mid-altitude, want screenSpawn", a.active)
	}
	if !a.spawn.CapturingText() {
		t.Fatalf("the ALTITUDE box closed on a backtick — it should have ignored the key")
	}
}

// TestBossKeyLiveAgainAfterAltitudeBoxCloses — once the box closes, the
// boss key must work as normal (the intercept is only suppressed while
// the box is literally open).
func TestBossKeyLiveAgainAfterAltitudeBoxCloses(t *testing.T) {
	a := newSpawnApp(t)
	openSpawnAltitudeBox(t, a)
	pressMsg(a, tea.KeyMsg{Type: tea.KeyEnter}) // close the box (empty buffer reverts)
	if a.spawn.CapturingText() {
		t.Fatalf("setup: ALTITUDE box still open after Enter on an empty buffer")
	}

	pressMsg(a, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'`'}})
	if a.active != screenBoss {
		t.Fatalf("backtick after the ALTITUDE box closed did not open the boss shell (active=%v)", a.active)
	}
}
