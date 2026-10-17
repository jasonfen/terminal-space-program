package tui

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 slice B (#633): the saves screen's load / delete / overwrite and
// the VAB's delete-design are cards. Each guard drives the real App.Update and
// App.View, so it measures what the player is shown and what the keys and
// clicks do. Every one of these verbs destroys or discards data, so each card
// opens on its way-out row and an action row asks once more (Jason 2026-10-12,
// call 1). The roster's cards: app_roster_card_test.go.

func enterKey() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyEnter} }

func rosterApp(t *testing.T) *App {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.world.Session = &sim.SessionInfo{
		IsHost: true, CanAdminister: true, Self: "local",
		Players: []sim.SessionPlayer{
			{Fingerprint: "local", Handle: "jason", Role: "host", Online: true},
			{Fingerprint: "SHA256:guest", Handle: "pilot2", Role: "guest", Online: true},
		},
	}
	a.Update(keyRunes("O"))
	if a.active != screenSession {
		t.Fatalf("setup: O did not open the roster (active=%v)", a.active)
	}
	a.Update(keyRunes("j")) // onto pilot2
	return a
}

func savedGameApp(t *testing.T, item string) (*App, string) {
	t.Helper()
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.Update(tea.KeyMsg{Type: tea.KeyF5}) // quicksave lane: loadable and deletable
	if f := savesDirFiles(t, dir); len(f) != 1 {
		t.Fatalf("setup: quicksave files = %v", f)
	}
	return a, dir
}

func designFileCount() int {
	e, _ := filepath.Glob(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "terminal-space-program", "designs", "*.json"))
	return len(e)
}

// slotAsk is one of the four asks this slice converts.
type slotAsk struct {
	name  string
	setup func(t *testing.T) *App
	title string
	note  string // "" when the card has none
	verb  string // the action row
	out   string // the way-out row
	// acted reports that the verb has landed; before is the keeper save's
	// bytes read when the card was raised (the overwrite's evidence).
	acted func(t *testing.T, a *App, before []byte) bool
}

// keeperBytes is the named save "keeper" on disk, nil when there is none.
func keeperBytes(t *testing.T) []byte {
	t.Helper()
	dir, _ := save.SavesDir()
	m, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	for _, f := range m {
		b, _ := os.ReadFile(f)
		if bytes.Contains(b, []byte("keeper")) {
			return b
		}
	}
	return nil
}

func slotAsks() []slotAsk {
	saves := func(mode string, down int, key string, named bool) func(t *testing.T) *App {
		return func(t *testing.T) *App {
			a, _ := savedGameApp(t, mode)
			if named {
				if _, err := save.WriteNamed(a.world, "keeper"); err != nil {
					t.Fatalf("WriteNamed: %v", err)
				}
			}
			openSavesVia(t, a, mode)
			for i := 0; i < down; i++ {
				press(a, "down")
			}
			a.world.Clock.WarpIdx = 3 // a load resets it; an overwrite writes it
			press(a, key)
			return a
		}
	}
	return []slotAsk{
		{"saves load", saves("l", 0, "enter", false), "Load [QUICKSAVE]?", "discards the current state", "load", "cancel",
			func(_ *testing.T, a *App, _ []byte) bool {
				return a.active == screenOrbit && a.world.Clock.WarpIdx != 3
			}},
		{"saves delete", saves("l", 0, "d", false), "Delete [QUICKSAVE]?", "This cannot be undone.", "delete", "keep it",
			func(_ *testing.T, a *App, _ []byte) bool {
				dir, _ := save.SavesDir()
				m, _ := filepath.Glob(filepath.Join(dir, "*.json"))
				return len(m) == 0
			}},
		{"saves overwrite", saves("s", 1, "enter", true), "Overwrite 'keeper'?", "", "overwrite", "cancel",
			func(t *testing.T, a *App, before []byte) bool {
				b := keeperBytes(t)
				return b != nil && !bytes.Equal(b, before)
			}},
		{"VAB delete design", func(t *testing.T) *App {
			testStateDirs(t)
			a, err := New(nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
			press(a, "esc")
			press(a, "b")
			if a.active != screenVAB {
				t.Fatalf("setup: b did not open the VAB (active=%v)", a.active)
			}
			press(a, "s")
			for _, r := range "Moon Lifter" {
				press(a, string(r))
			}
			press(a, "enter")
			press(a, "o")
			press(a, "x")
			return a
		}, "Delete design 'Moon Lifter'?", "This cannot be undone.", "delete", "keep it",
			func(_ *testing.T, a *App, _ []byte) bool { return designFileCount() == 0 }},
	}
}

// raised sets the ask up and returns the app and the keeper bytes at that
// moment.
func (c slotAsk) raised(t *testing.T) (*App, []byte) {
	t.Helper()
	a := c.setup(t)
	before := keeperBytes(t)
	if _, ok := cardUp(a, c.title); !ok {
		t.Fatalf("%s: setup raised no card titled %q:\n%s", c.name, c.title, ansi.Strip(a.View()))
	}
	return a, before
}

// markerLine is the card line carrying the ▸.
func markerLine(card string) string {
	for _, ln := range strings.Split(card, "\n") {
		if strings.Contains(ln, "▸") {
			return ln
		}
	}
	return ""
}

func TestSavesAndVABAsksAreCards(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, c := range slotAsks() {
			a, _ := c.raised(t)
			a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			out := a.View()
			for _, gone := range []string{"[y/n]", "[Yes]", "[No]", "[y]es", "[y] delete it"} {
				if strings.Contains(out, gone) {
					t.Errorf("%s %dx%d: %q still on screen", c.name, sz[0], sz[1], gone)
				}
			}
			card, ok := cardUp(a, c.title)
			if !ok {
				t.Errorf("%s %dx%d: no card titled %q:\n%s", c.name, sz[0], sz[1], c.title, ansi.Strip(out))
				continue
			}
			for _, want := range []string{c.verb, c.out, "▸", "y", "n  esc"} {
				if !strings.Contains(card, want) {
					t.Errorf("%s %dx%d: card lacks %q:\n%s", c.name, sz[0], sz[1], want, card)
				}
			}
			if c.note != "" && !strings.Contains(card, c.note) {
				t.Errorf("%s %dx%d: card lacks its note %q:\n%s", c.name, sz[0], sz[1], c.note, card)
			}
			if c.note == "" && strings.Contains(card, "discards") || strings.Contains(card, "undone") && c.note == "" {
				t.Errorf("%s %dx%d: a note on a card that has none:\n%s", c.name, sz[0], sz[1], card)
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

// Every verb here destroys or discards data, so the card opens on its way-out
// row: enter on a fresh card closes it and nothing lands.
func TestSavesAndVABCardsOpenOnTheWayOut(t *testing.T) {
	for _, c := range slotAsks() {
		a, before := c.raised(t)
		card, _ := cardUp(a, c.title)
		if ln := markerLine(card); !strings.Contains(ln, c.out) || strings.Contains(ln, c.verb) {
			t.Errorf("%s: the ▸ opens on %q, want the way out %q", c.name, ln, c.out)
		}
		a.Update(enterKey())
		if _, ok := cardUp(a, c.title); ok {
			t.Errorf("%s: enter on the way-out row left the card up", c.name)
		}
		if c.acted(t, a, before) {
			t.Errorf("%s: enter on a fresh card did the verb", c.name)
		}
	}
}

// Call 1: picking the verb asks once more on the same card, opening on the way
// out; only a second pick acts. y, y acts; y then enter does not.
func TestSavesAndVABCardsAskTwice(t *testing.T) {
	for _, c := range slotAsks() {
		a, before := c.raised(t)
		a.Update(keyRunes("y"))
		card, ok := cardUp(a, c.title)
		if !ok || !strings.Contains(card, "yes, "+c.verb) {
			t.Errorf("%s: first y did not show the confirm step:\n%s", c.name, card)
		}
		if ln := markerLine(card); !strings.Contains(ln, c.out) {
			t.Errorf("%s: the confirm step opens on %q, want the way out", c.name, ln)
		}
		if c.acted(t, a, before) {
			t.Errorf("%s: first y acted at once", c.name)
		}
		a.Update(enterKey()) // on the way out
		if _, ok := cardUp(a, c.title); ok || c.acted(t, a, before) {
			t.Errorf("%s: y then enter did not back out cleanly", c.name)
		}

		a, before = c.raised(t)
		a.Update(keyRunes("y"))
		a.Update(keyRunes("y"))
		if _, ok := cardUp(a, c.title); ok {
			t.Errorf("%s: y, y left the card up", c.name)
		}
		if !c.acted(t, a, before) {
			t.Errorf("%s: y, y did not do the verb", c.name)
		}
	}
}

// n and esc are the way out, from either step, and act at once.
func TestSavesAndVABCardsWayOutActsAtOnce(t *testing.T) {
	for _, c := range slotAsks() {
		for _, k := range []tea.KeyMsg{keyRunes("n"), {Type: tea.KeyEsc}} {
			for _, step2 := range []bool{false, true} {
				a, before := c.raised(t)
				if step2 {
					a.Update(keyRunes("y"))
				}
				a.Update(k)
				if _, ok := cardUp(a, c.title); ok {
					t.Errorf("%s %q step2=%v: left the card up", c.name, k.String(), step2)
				}
				if c.acted(t, a, before) {
					t.Errorf("%s %q step2=%v: did the verb", c.name, k.String(), step2)
				}
				if c.name == "VAB delete design" && a.active != screenVAB {
					t.Errorf("%s %q: esc on the card left the VAB (active=%v)", c.name, k.String(), a.active)
				}
				if strings.HasPrefix(c.name, "saves") && a.active != screenSaves && c.name != "saves load" {
					t.Errorf("%s %q: left the saves screen (active=%v)", c.name, k.String(), a.active)
				}
			}
		}
	}
}

// A stray key under a card reaches nothing (the ADR 0052 modal rule).
func TestSavesAndVABCardsSwallowStrayKeys(t *testing.T) {
	for _, c := range slotAsks() {
		a, before := c.raised(t)
		activeBefore := a.active
		for _, k := range []tea.KeyMsg{keyRunes("q"), keyRunes("r"), keyRunes("d"), keyRunes("x"), keyRunes("w"), {Type: tea.KeyTab}, {Type: tea.KeyF5}} {
			a.Update(k)
			if _, ok := cardUp(a, c.title); !ok {
				t.Fatalf("%s: %q dropped the card", c.name, k.String())
			}
			if a.active != activeBefore {
				t.Fatalf("%s: %q left the screen", c.name, k.String())
			}
			if c.acted(t, a, before) {
				t.Fatalf("%s: %q did the verb", c.name, k.String())
			}
		}
	}
}

// A click on a row answers it as enter on that row would; a click anywhere
// else is swallowed.
func TestSavesAndVABCardRowsAreClickTargets(t *testing.T) {
	for _, c := range slotAsks() {
		// Verb row: moves to the confirm step, does not act.
		a, before := c.raised(t)
		cardRowClick(t, a, c.verb)
		card, ok := cardUp(a, c.title)
		if !ok || !strings.Contains(card, "yes, "+c.verb) {
			t.Fatalf("%s: a click on %q did not show the confirm step:\n%s", c.name, c.verb, card)
		}
		if c.acted(t, a, before) {
			t.Errorf("%s: the first click acted at once", c.name)
		}
		// The confirm step's `yes, <verb>` row acts.
		cardRowClick(t, a, "yes, "+c.verb)
		if _, ok := cardUp(a, c.title); ok || !c.acted(t, a, before) {
			t.Errorf("%s: clicking yes, %s did not do it", c.name, c.verb)
		}

		// Way out: closes the card, nothing lands.
		a, before = c.raised(t)
		cardRowClick(t, a, c.out)
		if _, ok := cardUp(a, c.title); ok || c.acted(t, a, before) {
			t.Errorf("%s: clicking %q did not close cleanly", c.name, c.out)
		}

		// Elsewhere: swallowed, the list rows under the card included.
		a, before = c.raised(t)
		a.View()
		for _, p := range [][2]int{{a.backStart, 0}, {0, 0}, {2, 3}, {4, 5}, {138, 20}, {70, 38}} {
			mouseClick(a, p[0], p[1])
			if _, ok := cardUp(a, c.title); !ok {
				t.Fatalf("%s: a click at %v closed the card", c.name, p)
			}
			if c.acted(t, a, before) {
				t.Fatalf("%s: a click at %v did the verb", c.name, p)
			}
		}
	}
}

// Every card swallows every key but its own (ADR 0054): the boss key and
// F1 / ? must not fire over a roster, saves or VAB card.
func TestScreenCardsSwallowBossAndHelpKeys(t *testing.T) {
	keys := []tea.KeyMsg{keyRunes("`"), keyRunes("?"), {Type: tea.KeyF1}}
	type scr struct {
		name, title string
		raise       func(t *testing.T) *App
	}
	scrs := []scr{{"roster", "Kick pilot2?", func(t *testing.T) *App {
		a := rosterApp(t)
		a.Update(keyRunes("x"))
		return a
	}}}
	for _, c := range slotAsks() {
		c := c
		scrs = append(scrs, scr{c.name, c.title, func(t *testing.T) *App { a, _ := c.raised(t); return a }})
	}
	for _, sc := range scrs {
		for _, k := range keys {
			t.Run(sc.name+"/"+k.String(), func(t *testing.T) {
				a := sc.raise(t)
				before := a.active
				a.Update(k)
				if a.active != before {
					t.Errorf("%q moved the screen %v -> %v under a card", k.String(), before, a.active)
				}
				if _, ok := cardUp(a, sc.title); !ok {
					t.Errorf("%q dropped the card", k.String())
				}
			})
		}
	}
}
