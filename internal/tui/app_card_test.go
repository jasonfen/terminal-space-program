package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 slice A (#633 part 1): the four flight asks (E end flight, D
// transpose, Y deploy, F9 quickload) and the quit prompt are cards. Every
// guard here drives the real App.Update / View and reads the screen, so it
// measures what the player sees. Jason's 2026-10-12 confirmations apply: every
// action row asks once more on the same card before acting, the way-out row
// acts at once, enter picks the highlighted row, and the sim keeps running
// under a flight card.

// cardW is the shared card's width (MenuCardW = 40), spelled out here so the
// tests do not depend on the constant they are checking.
const cardW = 40

type flightAsk struct {
	name  string
	key   tea.KeyMsg
	title string // prefix of the card title
	act   string // the action row's label
	out   string // the way-out row's label
	open  string // the row the card opens on
	setup func(t *testing.T) *App
	// landed reports that "yes" has taken effect.
	landed func(a *App) bool
}

func flightAsks() []flightAsk {
	var deployBaseN int
	return []flightAsk{
		{
			name: "end flight", key: keyRunes("E"), title: "End flight for ",
			act: "remove the wreckage", out: "keep it", open: "remove the wreckage",
			setup: func(t *testing.T) *App {
				testStateDirs(t)
				a, err := New(nil)
				if err != nil {
					t.Fatalf("New: %v", err)
				}
				a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
				a.world.ActiveCraft().Crashed = true
				return a
			},
			landed: func(a *App) bool { return a.world.ActiveCraft() == nil },
		},
		{
			name: "transpose", key: keyRunes("D"), title: "Transpose?",
			act: "transpose", out: "cancel", open: "cancel",
			setup:  func(t *testing.T) *App { return apolloReadyApp(t) },
			landed: func(a *App) bool { return a.world.ActiveCraft().Stages[0].Name != "Descent" },
		},
		{
			name: "deploy", key: keyRunes("Y"), title: "Deploy ",
			act: "deploy", out: "cancel", open: "cancel",
			setup: func(t *testing.T) *App {
				a, _ := deployApp(t)
				deployBaseN = len(a.world.Crafts)
				return a
			},
			landed: func(a *App) bool { return len(a.world.Crafts) > deployBaseN },
		},
		{
			name: "quickload", key: tea.KeyMsg{Type: tea.KeyF9}, title: "Load the quicksave?",
			act: "load", out: "cancel", open: "cancel",
			setup: func(t *testing.T) *App {
				a, _ := savedGameApp(t, "l")
				a.world.Clock.WarpIdx = 3 // a load resets this to the quicksaved value
				return a
			},
			landed: func(a *App) bool { return a.world.Clock.WarpIdx != 3 },
		},
	}
}

// cardGeometry finds the card whose title line contains title in a rendered
// frame: its left column, its top edge's line, and its lines (card cells only).
func cardGeometry(out, title string) (col, top int, rows []string) {
	rows, col = quitCardRows(out, title)
	if rows == nil {
		return -1, -1, nil
	}
	for i, ln := range strings.Split(ansi.Strip(out), "\n") {
		if strings.Contains(ln, title) {
			return col, i - 1, rows
		}
	}
	return -1, -1, nil
}

// cardUp reports the card text when a card whose title starts with prefix is
// on screen.
func cardUp(a *App, prefix string) (string, bool) {
	out := a.View()
	var title string
	for _, ln := range strings.Split(ansi.Strip(out), "\n") {
		if k := strings.Index(ln, prefix); k >= 0 {
			title = strings.TrimSpace(strings.Trim(strings.TrimSpace(ln[k:]), "│"))
			break
		}
	}
	if title == "" {
		return "", false
	}
	rows, _ := quitCardRows(out, title)
	if rows == nil {
		return "", false
	}
	return strings.Join(rows, "\n"), true
}

func mouseClick(a *App, x, y int) {
	a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

// rowY is the screen line a card row's label sits on.
func rowY(out, label string) int {
	for i, ln := range strings.Split(ansi.Strip(out), "\n") {
		if strings.Contains(ln, label) && strings.Contains(ln, "│") && i > 0 {
			return i
		}
	}
	return -1
}

// TestFlightAsksAreCards: each ask draws a 40-wide card, centred, with its
// two rows, a ▸, and no [y/n] anywhere on screen, at both design sizes.
func TestFlightAsksAreCards(t *testing.T) {
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		for _, c := range flightAsks() {
			a := c.setup(t)
			a.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
			a.Update(c.key)
			out := a.View()
			if strings.Contains(out, "[y/n]") {
				t.Errorf("%s %dx%d: [y/n] still on screen", c.name, sz[0], sz[1])
			}
			card, ok := cardUp(a, c.title)
			if !ok {
				t.Errorf("%s %dx%d: no card titled %q:\n%s", c.name, sz[0], sz[1], c.title, ansi.Strip(out))
				continue
			}
			for _, want := range []string{c.act, c.out, "▸"} {
				if !strings.Contains(card, want) {
					t.Errorf("%s %dx%d: card lacks %q:\n%s", c.name, sz[0], sz[1], want, card)
				}
			}
			lines := strings.Split(card, "\n")
			if n := len(lines); n < 9 || n > 11 {
				t.Errorf("%s %dx%d: card is %d rows tall, want 9..11", c.name, sz[0], sz[1], n)
			}
			title := strings.TrimSpace(strings.Trim(strings.TrimSpace(lines[1]), "│"))
			col, _, _ := cardGeometry(out, title)
			if left, right := col, sz[0]-(col+cardW); abs(left-right) > 1 {
				t.Errorf("%s %dx%d: card not centred: %d left, %d right", c.name, sz[0], sz[1], left, right)
			}
			for i, ln := range lines {
				if w := len([]rune(ln)); w != cardW {
					t.Errorf("%s %dx%d: card row %d is %d cells wide, want %d: %q", c.name, sz[0], sz[1], i, w, cardW, ln)
				}
			}
		}
	}
}

// TestCardOpensOnTheSafeRow: the ▸ starts on the row ADR 0054 names: the
// action when the verb keeps every save (end flight), the way out when it
// discards or reshapes (transpose, deploy, quickload).
func TestCardOpensOnTheSafeRow(t *testing.T) {
	for _, c := range flightAsks() {
		a := c.setup(t)
		a.Update(c.key)
		card, ok := cardUp(a, c.title)
		if !ok {
			t.Fatalf("%s: no card", c.name)
		}
		if !strings.Contains(card, "▸ "+c.open) {
			t.Errorf("%s: card does not open on %q:\n%s", c.name, c.open, card)
		}
		a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if c.landed(a) {
			t.Errorf("%s: enter on a freshly opened card changed the world", c.name)
		}
	}
}

// TestCardEveryActionAsksTwice (call 1): y picks the action row, which asks
// once more on the same card with the way out under the ▸; only a second y
// acts. The way-out row acts at once, from either step.
func TestCardEveryActionAsksTwice(t *testing.T) {
	for _, c := range flightAsks() {
		t.Run(c.name+"/y,y", func(t *testing.T) {
			a := c.setup(t)
			a.Update(c.key)
			a.Update(keyRunes("y"))
			if c.landed(a) {
				t.Fatalf("y acted at once, it must ask again")
			}
			card, ok := cardUp(a, c.title)
			if !ok {
				t.Fatalf("the card left after the first y")
			}
			if !strings.Contains(card, "yes, "+c.act) || !strings.Contains(card, "▸ "+c.out) {
				t.Errorf("second step must offer 'yes, %s' and sit on %q:\n%s", c.act, c.out, card)
			}
			a.Update(keyRunes("y"))
			if !c.landed(a) {
				t.Errorf("the second y did not act")
			}
			if _, ok := cardUp(a, c.title); ok {
				t.Errorf("the card is still up after the action")
			}
		})
		t.Run(c.name+"/enter,up,enter", func(t *testing.T) {
			a := c.setup(t)
			a.Update(c.key)
			if c.open != c.act {
				a.Update(tea.KeyMsg{Type: tea.KeyUp}) // to the action row
			}
			a.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if c.landed(a) {
				t.Fatalf("enter on the action row acted at once")
			}
			a.Update(tea.KeyMsg{Type: tea.KeyUp}) // to 'yes, ...'
			a.Update(tea.KeyMsg{Type: tea.KeyEnter})
			if !c.landed(a) {
				t.Errorf("enter on the confirm row did not act")
			}
		})
		for _, way := range []string{"n", "esc"} {
			t.Run(c.name+"/way-out "+way, func(t *testing.T) {
				for _, second := range []bool{false, true} {
					a := c.setup(t)
					a.Update(c.key)
					if second {
						a.Update(keyRunes("y"))
					}
					if way == "n" {
						a.Update(keyRunes("n"))
					} else {
						a.Update(tea.KeyMsg{Type: tea.KeyEsc})
					}
					if _, ok := cardUp(a, c.title); ok {
						t.Errorf("%s (second step %v): the way out left the card up", way, second)
					}
					if c.landed(a) {
						t.Errorf("%s (second step %v): the way out acted", way, second)
					}
				}
			})
		}
	}
}

// TestCardEnterOnWayOutActsAtOnce: enter on the way-out row closes the card
// in one press, with nothing changed.
func TestCardEnterOnWayOutActsAtOnce(t *testing.T) {
	for _, c := range flightAsks() {
		a := c.setup(t)
		a.Update(c.key)
		if c.open == c.act {
			a.Update(tea.KeyMsg{Type: tea.KeyDown}) // onto the way out
		}
		a.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if _, ok := cardUp(a, c.title); ok {
			t.Errorf("%s: enter on the way-out row left the card up", c.name)
		}
		if c.landed(a) {
			t.Errorf("%s: the way-out row acted", c.name)
		}
	}
}

// TestStrayKeysUnderACardMoveNothing (the ADR 0052 modal rule): while a card
// is up, flight keys do nothing, the arrows move the ▸ and never a trim, and
// the card stays.
func TestStrayKeysUnderACardMoveNothing(t *testing.T) {
	stray := []tea.KeyMsg{
		keyRunes("."), keyRunes(","), keyRunes("z"), keyRunes("x"), keyRunes("w"),
		keyRunes("~"), keyRunes("`"), keyRunes("m"), keyRunes("O"), keyRunes("i"),
		keyRunes("e"), keyRunes("D"), keyRunes("Y"),
		keyType(tea.KeyLeft), keyType(tea.KeyRight), keyType(tea.KeyUp), keyType(tea.KeyDown),
		keyType(tea.KeyShiftLeft), keyType(tea.KeyShiftRight), keyType(tea.KeyShiftUp), keyType(tea.KeyShiftDown),
		keyAlt(tea.KeyLeft), keyAlt(tea.KeyRight), keyAlt(tea.KeyUp), keyAlt(tea.KeyDown),
	}
	for _, c := range flightAsks() {
		a := c.setup(t)
		cr := a.world.ActiveCraft()
		a.Update(c.key)
		warp, active := a.world.Clock.WarpIdx, a.active
		for _, k := range stray {
			a.Update(k)
			if _, ok := cardUp(a, c.title); !ok {
				t.Fatalf("%s: %q dismissed the card", c.name, k.String())
			}
			if cr != nil && (cr.PitchTrim != 0 || cr.HeadingTrim != 0) {
				t.Fatalf("%s: %q moved a trim", c.name, k.String())
			}
			if a.world.Clock.WarpIdx != warp || a.active != active {
				t.Fatalf("%s: %q changed warp or screen", c.name, k.String())
			}
			if c.landed(a) {
				t.Fatalf("%s: %q answered the card", c.name, k.String())
			}
		}
	}
}

// TestClickOnACardRowAnswersIt: a click on a row picks it (the action row
// goes to its second step; the way out closes the card); a click anywhere
// else, including the Title Row's [Menu], is swallowed.
func TestClickOnACardRowAnswersIt(t *testing.T) {
	for _, c := range flightAsks() {
		a := c.setup(t)
		a.Update(c.key)
		out := a.View()
		col, _, _ := cardGeometry(out, strings.TrimSpace(c.title))
		if col < 0 {
			t.Fatalf("%s: no card", c.name)
		}
		mouseClick(a, col+6, rowY(out, c.act))
		if c.landed(a) {
			t.Fatalf("%s: one click on the action row acted", c.name)
		}
		card, ok := cardUp(a, c.title)
		if !ok || !strings.Contains(card, "yes, "+c.act) {
			t.Fatalf("%s: a click on the action row did not open its second step:\n%s", c.name, card)
		}
		mouseClick(a, col+6, rowY(a.View(), "yes, "+c.act))
		if !c.landed(a) {
			t.Errorf("%s: clicking the confirm row did not act", c.name)
		}

		a = c.setup(t)
		a.Update(c.key)
		out = a.View()
		mouseClick(a, col+6, rowY(out, c.out))
		if _, ok := cardUp(a, c.title); ok {
			t.Errorf("%s: a click on the way-out row left the card up", c.name)
		}
		if c.landed(a) {
			t.Errorf("%s: the way-out click acted", c.name)
		}

		// Outside: swallowed, the card stays, the map's [Menu] does not open.
		a = c.setup(t)
		a.Update(c.key)
		out = a.View()
		menuX := strings.Index(strings.Split(ansi.Strip(out), "\n")[0], "[Menu]")
		for _, p := range [][2]int{{2, 20}, {menuX + 1, 0}, {col + 6, 1}, {col - 3, rowY(out, c.act)}} {
			mouseClick(a, p[0], p[1])
			if _, ok := cardUp(a, c.title); !ok {
				t.Fatalf("%s: a click at %v dismissed the card", c.name, p)
			}
			if a.active != screenOrbit || c.landed(a) {
				t.Fatalf("%s: a click at %v reached what is under the card (screen %v)", c.name, p, a.active)
			}
		}
	}
}

// TestQuitCardClickDoesNotReachThePauseMenu: raised from the pause menu, the
// quit card covers it; a click on the quit card's rows must answer the card,
// never the pause menu's hidden rows (Save Game, Load Game, ...).
func TestQuitCardClickDoesNotReachThePauseMenu(t *testing.T) {
	fresh := func() *App {
		testStateDirs(t)
		a, _ := New(nil)
		a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
		a.Update(tea.KeyMsg{Type: tea.KeyEsc})
		a.Update(keyRunes("q"))
		return a
	}
	a := fresh()
	out := a.View()
	col, _, _ := cardGeometry(out, "Save before quitting?")
	if col < 0 {
		t.Fatalf("no quit card:\n%s", ansi.Strip(out))
	}
	for _, label := range []string{"save and quit", "quit without saving", "stay"} {
		a = fresh()
		mouseClick(a, col+6, rowY(a.View(), label))
		if a.active != screenMenu && a.active != screenOrbit {
			t.Errorf("click on %q reached the pause menu (screen %v)", label, a.active)
		}
	}
	// A click on the quit card's first row is the card's: it asks again.
	a = fresh()
	mouseClick(a, col+6, rowY(a.View(), "save and quit"))
	if card, ok := cardUp(a, "Save before quitting?"); !ok || !strings.Contains(card, "yes, save and quit") {
		t.Errorf("a click on save and quit did not open its second step:\n%s", card)
	}
	// A click that misses the card swallows too.
	a = fresh()
	mouseClick(a, 2, 20)
	if _, ok := cardUp(a, "Save before quitting?"); !ok {
		t.Error("a click outside dismissed the quit card")
	}
}

// TestTheSimRunsUnderAFlightCard (Conflict 1): the card dims the map, it does
// not hold the clock.
func TestTheSimRunsUnderAFlightCard(t *testing.T) {
	for _, c := range flightAsks() {
		a := c.setup(t)
		a.world.Clock.Paused = false
		a.Update(c.key)
		if _, ok := cardUp(a, c.title); !ok {
			t.Fatalf("%s: no card", c.name)
		}
		t0 := a.world.Clock.SimTime
		for i := 0; i < 5; i++ {
			a.Update(sim.TickMsg(time.Now().Add(time.Duration(i+1) * 50 * time.Millisecond)))
		}
		if !a.world.Clock.SimTime.After(t0) {
			t.Errorf("%s: sim time did not move under the card (%v -> %v)", c.name, t0, a.world.Clock.SimTime)
		}
	}
}

// TestCardTitleIsCutToFit: a vessel name has no length limit; the title is
// cut with an ellipsis so the box stays 40 wide.
func TestCardTitleIsCutToFit(t *testing.T) {
	c := flightAsks()[0]
	a := c.setup(t)
	a.world.ActiveCraft().Name = strings.Repeat("Longname", 12)
	a.Update(c.key)
	out := a.View()
	card, ok := cardUp(a, "End flight for ")
	if !ok {
		t.Fatalf("no card:\n%s", ansi.Strip(out))
	}
	for i, ln := range strings.Split(card, "\n") {
		if w := len([]rune(ln)); w != cardW {
			t.Errorf("row %d is %d cells, want %d: %q", i, w, cardW, ln)
		}
	}
	if !strings.Contains(card, "…") {
		t.Errorf("a long name was not cut with an ellipsis:\n%s", card)
	}
}

// cardKindUp reports whether the card of kind k is the one up.
func cardKindUp(a *App, k cardKind) bool { return a.card != nil && a.card.kind == k }

// pressTwice presses the rune r twice and returns the second Update's result:
// an action row asks once more (call 1), so a learned y now takes y, y.
func pressTwice(a *App, r rune) (tea.Model, tea.Cmd) {
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	return a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}
