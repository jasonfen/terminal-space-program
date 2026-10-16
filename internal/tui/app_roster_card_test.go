package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// ADR 0054 slice C: the roster's asks are cards the roster screen owns, drawn
// by App.View and clicked through the App's mouse path. Every guard drives the
// real App.Update / View.

func TestRosterAsksAreCardsOverTheDimmedRoster(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, c := range []struct {
			name  string
			key   tea.KeyMsg
			title string
			rows  []string
		}{
			{"kick", keyRunes("x"), "Kick pilot2?", []string{"kick", "kick and delete vessels", "cancel"}},
			{"disconnect", keyRunes("d"), "Disconnect pilot2?", []string{"disconnect", "cancel"}},
			{"stop hosting", keyRunes("h"), "Stop hosting?", []string{"stop hosting", "cancel"}},
			{"restart", tea.KeyMsg{Type: tea.KeyF4}, "Restart the server?", []string{"restart", "cancel"}},
		} {
			a := rosterApp(t)
			a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			a.Update(c.key)
			out := a.View()
			if strings.Contains(out, "[y/n]") {
				t.Errorf("%s %dx%d: [y/n] on screen", c.name, sz[0], sz[1])
			}
			card, ok := cardUp(a, c.title)
			if !ok {
				t.Errorf("%s %dx%d: no card titled %q:\n%s", c.name, sz[0], sz[1], c.title, ansi.Strip(out))
				continue
			}
			for _, want := range append([]string{"▸"}, c.rows...) {
				if !strings.Contains(card, want) {
					t.Errorf("%s %dx%d: card lacks %q:\n%s", c.name, sz[0], sz[1], want, card)
				}
			}
			lines := strings.Split(card, "\n")
			if n := len(lines); n < 9 || n > 11 {
				t.Errorf("%s %dx%d: card is %d rows tall, want 9..11", c.name, sz[0], sz[1], n)
			}
			col, _, _ := cardGeometry(out, c.title)
			if left, right := col, sz[0]-(col+cardW); abs(left-right) > 1 {
				t.Errorf("%s %dx%d: card not centred: %d left, %d right", c.name, sz[0], sz[1], left, right)
			}
		}
	}
}

// A stray key under the roster card reaches nothing: the screen stays, the
// card stays.
func TestRosterCardSwallowsStrayKeys(t *testing.T) {
	a := rosterApp(t)
	a.Update(keyRunes("x"))
	for _, k := range []tea.KeyMsg{keyRunes("q"), keyRunes("O"), keyRunes("t"), keyRunes("m"), {Type: tea.KeyTab}} {
		a.Update(k)
		if a.active != screenSession {
			t.Fatalf("%q left the roster under a card", k.String())
		}
		if _, ok := cardUp(a, "Kick pilot2?"); !ok {
			t.Fatalf("%q dropped the card", k.String())
		}
	}
}

// cardRowClick clicks the card row whose label is label.
func cardRowClick(t *testing.T, a *App, label string) {
	t.Helper()
	out := a.View()
	y := rowY(out, label)
	if y < 0 {
		t.Fatalf("no row %q on screen:\n%s", label, ansi.Strip(out))
	}
	x := strings.Index(strings.Split(ansi.Strip(out), "\n")[y], label)
	mouseClick(a, len([]rune(strings.Split(ansi.Strip(out), "\n")[y][:x])), y)
}

func TestRosterCardRowsAreClickTargets(t *testing.T) {
	a := rosterApp(t)
	a.Update(keyRunes("x"))
	cardRowClick(t, a, "kick and delete vessels")
	card, ok := cardUp(a, "Kick pilot2?")
	if !ok || !strings.Contains(card, "yes, kick and delete vessels") {
		t.Fatalf("a click on the delete row did not move to its confirm step:\n%s", card)
	}
	// Clicking the way out closes the card and sends nothing.
	cardRowClick(t, a, "cancel")
	if _, ok := cardUp(a, "Kick pilot2?"); ok {
		t.Error("clicking cancel left the card up")
	}
}

func TestRosterCardClickElsewhereIsSwallowed(t *testing.T) {
	a := rosterApp(t)
	a.Update(keyRunes("x"))
	// The Title Row's [Back] button is a live click target on this screen:
	// with a card up it must not fire (it would answer the card with esc).
	a.View()
	if a.backEnd <= a.backStart {
		t.Fatalf("setup: no [Back] button recorded (%d..%d)", a.backStart, a.backEnd)
	}
	for _, p := range [][2]int{{a.backStart, 0}, {0, 0}, {2, 5}, {138, 20}, {70, 38}} {
		mouseClick(a, p[0], p[1])
		if a.active != screenSession {
			t.Fatalf("click at %v left the roster", p)
		}
		if _, ok := cardUp(a, "Kick pilot2?"); !ok {
			t.Fatalf("click at %v closed the card", p)
		}
	}
}

// ADR 0054 slice D wired the seam: each roster answer reaches the serve
// wrapper as its own admin command (the purge flag travels with the kick).
func TestRosterAnswersMapOntoTheServerSeam(t *testing.T) {
	run := func(keys ...tea.KeyMsg) tea.Msg {
		a := rosterApp(t)
		var cmd tea.Cmd
		for _, k := range keys {
			_, cmd = a.Update(k)
		}
		if cmd == nil {
			return nil
		}
		return cmd()
	}
	want := func(name string, msg tea.Msg, kind screens.SessionCommandKind, purge bool) {
		t.Helper()
		am, ok := msg.(screens.SessionAdminMsg)
		if !ok || am.Cmd.Kind != kind || am.Cmd.Purge != purge || am.Cmd.Fingerprint != "SHA256:guest" {
			t.Errorf("%s sent %#v, want kind %v purge %v for pilot2", name, msg, kind, purge)
		}
	}
	want("x,y,y", run(keyRunes("x"), keyRunes("y"), keyRunes("y")), screens.SessionCmdKick, false)
	want("kick and delete", run(keyRunes("x"), tea.KeyMsg{Type: tea.KeyDown}, enterKey(), tea.KeyMsg{Type: tea.KeyUp}, enterKey()), screens.SessionCmdKick, true)
	want("d,y,y", run(keyRunes("d"), keyRunes("y"), keyRunes("y")), screens.SessionCmdDisconnect, false)
}
