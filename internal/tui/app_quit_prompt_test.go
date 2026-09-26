package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// isQuitCmd reports whether cmd is (or resolves to) the bubbletea
// QuitMsg — i.e. whether Update actually decided to end the program.
func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

// TestCtrlCRaisesQuitPromptInsteadOfQuitting (#474) — ctrl+c used to
// autosave and quit on the spot. Now it must only arm the quit prompt:
// no autosave write, no tea.Quit, until the player answers.
func TestCtrlCRaisesQuitPromptInsteadOfQuitting(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	if isQuitCmd(cmd) {
		t.Fatal("ctrl+c quit immediately — it must raise the quit prompt instead (#474)")
	}
	if !a.quitConfirm {
		t.Fatal("ctrl+c did not arm the quit prompt")
	}
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("ctrl+c wrote to the saves dir before the prompt was answered: %v", files)
	}
}

// TestQuitPromptNoWritesNothing (#474) — [n] at the quit prompt must
// quit writing nothing at all: the saves dir is untouched, before and
// after. The instrument (savesDirFiles) is proven live by
// TestQuitPromptYesWritesAutosave below, which drives the exact same
// prompt to [y] and shows the same check catching the write.
func TestQuitPromptNoWritesNothing(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	before := savesDirFiles(t, dir)

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	if !isQuitCmd(cmd) {
		t.Fatal("[n] did not quit")
	}
	if a.quitConfirm {
		t.Error("quitConfirm still armed after n")
	}
	after := savesDirFiles(t, dir)
	if len(before) != 0 || len(after) != 0 {
		t.Fatalf("saves dir not empty as the test expects: before=%v after=%v", before, after)
	}
}

// TestQuitPromptYesWritesAutosave (#474) — [y] at the same prompt does
// write into the autosave ring. Proves the savesDirFiles check above
// can catch a real write, so its silence in the [n] test is meaningful.
func TestQuitPromptYesWritesAutosave(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !isQuitCmd(cmd) {
		t.Fatal("[y] did not quit")
	}
	if a.quitConfirm {
		t.Error("quitConfirm still armed after y")
	}
	files := savesDirFiles(t, dir)
	if len(files) != 1 || files[0] != "autosave-1.json" {
		t.Fatalf("saves dir = %v, want exactly [autosave-1.json]", files)
	}
}

// TestQuitPromptEscReturnsExactlyWhereYouWere (#474) — esc cancels the
// prompt without writing anything and without changing the active
// screen or any world state.
func TestQuitPromptEscReturnsExactlyWhereYouWere(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	wantActive := a.active
	wantPaused := a.world.Clock.Paused

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEsc})

	if isQuitCmd(cmd) {
		t.Fatal("esc quit the prompt instead of cancelling it")
	}
	if a.quitConfirm {
		t.Error("quitConfirm still armed after esc")
	}
	if a.active != wantActive {
		t.Errorf("active = %v, want unchanged %v", a.active, wantActive)
	}
	if a.world.Clock.Paused != wantPaused {
		t.Errorf("Clock.Paused = %v, want unchanged %v", a.world.Clock.Paused, wantPaused)
	}
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("esc wrote to the saves dir: %v", files)
	}
}

// TestQuitPromptYesSkipsPausedWorld (#474 follow-up) — the on-quit
// write had no paused-world guard, unlike maybeAutosave's periodic
// lane, so repeated quits from a frozen sim could fill all three ring
// slots with identical snapshots and evict a state worth keeping.
// [y] against a paused world must now skip the write.
func TestQuitPromptYesSkipsPausedWorld(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Clock.Paused = true

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	if !isQuitCmd(cmd) {
		t.Fatal("[y] did not quit")
	}
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("[y] wrote a paused world into the ring: %v", files)
	}
}

// TestPersistNowWritesEvenWhenPaused (#474 follow-up) — PersistNow backs
// the serve layer's admin drain-and-restart, which leaves the process
// via os.Exit and gets exactly one chance to write. It must keep
// meaning "write now" and must NOT inherit the on-quit paused-world
// guard added above, or a restart during a paused session would
// silently stop persisting.
func TestPersistNowWritesEvenWhenPaused(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Clock.Paused = true

	a.PersistNow()

	files := savesDirFiles(t, dir)
	if len(files) != 1 || files[0] != "autosave-1.json" {
		t.Fatalf("PersistNow on a paused world: saves dir = %v, want exactly [autosave-1.json]", files)
	}
}

// TestQuitPromptRendersHostWording (#474) — the armed prompt must
// actually render onscreen, with all three keys visible, or a player
// has no way to know what ctrl+c just did.
func TestQuitPromptRendersHostWording(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	line := quitPromptLine(t, a.View())
	for _, want := range []string{"[y]", "[n]", "[esc]", "Save before quitting"} {
		if !strings.Contains(line, want) {
			t.Errorf("quit prompt line missing %q:\n%s", want, line)
		}
	}
}

// quitPromptLine returns the rendered bottom-border row (where every
// App-level confirm overlay rides — see overlayBottomBorder), the one
// line the quit prompt actually occupies. Scoping assertions to this
// line (rather than the whole screen) avoids false positives from
// unrelated UI that happens to contain the same bracketed tokens, e.g.
// the orbit footer's own "[n] new vessel" hint.
func quitPromptLine(t *testing.T, out string) string {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		t.Fatal("View() produced no lines")
	}
	return lines[len(lines)-1]
}

// TestQuitPromptRendersGuestWording (#474) — a guest can't decline, so
// their prompt must not advertise [n] at all, and must say the flight
// saves automatically.
func TestQuitPromptRendersGuestWording(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.guestSave = func(*sim.World) error { return nil }
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	line := quitPromptLine(t, a.View())
	if strings.Contains(line, "[n]") {
		t.Errorf("guest quit prompt must not offer [n]:\n%s", line)
	}
	for _, want := range []string{"[y]", "[esc]", "saves automatically"} {
		if !strings.Contains(line, want) {
			t.Errorf("guest quit prompt line missing %q:\n%s", want, line)
		}
	}
}
