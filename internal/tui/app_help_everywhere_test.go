package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// GK Q3 (#589): F1 opens help from every screen, on that screen's own page,
// and F1 or esc puts the player back exactly where they were: same screen,
// same focus, any half-typed text kept. Everything runs through App.Update
// and App.View() at 140x40; "exactly" is a byte-equal View.

// typeText sends s one rune at a time, the way a terminal delivers typing.
func typeText(a *App, s string) {
	for _, r := range s {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func f1() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyF1} }

func helpTestApp(t *testing.T) *App {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	return a
}

type helpScreenCase struct {
	name   string
	screen screenID
	// page is the position-line prefix of the help page F1 must open on.
	page  string
	setup func(t *testing.T) *App
	// typed, when set, must be on screen before F1 and after the return.
	typed string
}

func helpScreenCases() []helpScreenCase {
	return []helpScreenCase{
		{"map", screenOrbit, "INDEX · ", helpTestApp, ""},
		{"body info", screenBodyInfo, "INDEX · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "i")
			return a
		}, ""},
		{"missions", screenMissions, "MISSIONS · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "M")
			return a
		}, ""},
		{"pause menu", screenMenu, "PAUSE MENU · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "esc")
			return a
		}, ""},
		{"spawn form on ALTITUDE", screenSpawn, "SPAWN FORM · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "n")
			for i := 0; i < 4; i++ {
				a.Update(tea.KeyMsg{Type: tea.KeyTab})
			}
			return a
		}, ""},
		{"spawn ALTITUDE box with a half-typed number", screenSpawn, "SPAWN FORM · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "n")
			for i := 0; i < 4; i++ {
				a.Update(tea.KeyMsg{Type: tea.KeyTab})
			}
			press(a, "enter")
			press(a, "7")
			press(a, "7")
			press(a, "7")
			if !a.spawn.CapturingText() {
				t.Fatalf("setup: the altitude box is not open")
			}
			return a
		}, "777"},
		{"VAB", screenVAB, "VEHICLE ASSEMBLY · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "esc")
			press(a, "b")
			return a
		}, ""},
		{"VAB name box with a half-typed name", screenVAB, "VEHICLE ASSEMBLY · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "esc")
			press(a, "b")
			press(a, "a") // a design needs a part before it can be saved
			press(a, "s")
			typeText(a, "zq")
			return a
		}, "zq"},
		{"roster", screenSession, "MULTIPLAYER · ", func(t *testing.T) *App { return rosterApp(t) }, ""},
		{"roster invite box with a half-typed handle", screenSession, "MULTIPLAYER · ", func(t *testing.T) *App {
			a := rosterApp(t)
			press(a, "i")
			typeText(a, "zq")
			if !a.session.CapturingText() {
				t.Fatalf("setup: the invite box is not open")
			}
			return a
		}, "zq"},
		{"maneuver planner", screenManeuver, "PLAN BURNS · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "m")
			return a
		}, ""},
		{"maneuver planner dv box with a half-typed number", screenManeuver, "PLAN BURNS · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "m")
			for i := 0; i < 2 && !a.maneuver.TextFieldFocused(); i++ {
				a.Update(tea.KeyMsg{Type: tea.KeyTab})
			}
			if !a.maneuver.TextFieldFocused() {
				t.Fatalf("setup: the planner opened without a text field focused")
			}
			press(a, "4")
			press(a, "2")
			press(a, "9")
			return a
		}, "429"},
		{"porkchop", screenPorkchop, "PLAN BURNS · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			a.world.SetTargetBody(bodyIdx(t, a, "venus"))
			press(a, "P")
			if a.active != screenPorkchop {
				t.Fatalf("setup: P did not open the porkchop (active=%v)", a.active)
			}
			return a
		}, ""},
		{"saves list", screenSaves, "SAVES · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			openSavesVia(t, a, "l")
			return a
		}, ""},
		{"saves name box with a half-typed name", screenSaves, "SAVES · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			openSavesVia(t, a, "s")
			press(a, "enter") // New save... row opens the name box
			if !a.saves.CapturingText() {
				t.Fatalf("setup: the save name box is not open")
			}
			typeText(a, "zq")
			return a
		}, "zq"},
		{"settings", screenSettings, "PAUSE MENU · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "esc")
			press(a, "t")
			return a
		}, ""},
		{"keyboard layout", screenControls, "PAUSE MENU · ", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "esc")
			press(a, "k")
			return a
		}, ""},
	}
}

func TestF1OpensOnTheScreensOwnPageAndReturnsInPlace(t *testing.T) {
	for _, c := range helpScreenCases() {
		for _, closer := range []string{"f1", "esc"} {
			t.Run(c.name+"/"+closer, func(t *testing.T) {
				a := c.setup(t)
				if a.active != c.screen {
					t.Fatalf("setup: active = %v, want %v", a.active, c.screen)
				}
				before := a.View()
				if c.typed != "" && !strings.Contains(before, c.typed) {
					t.Fatalf("setup: %q is not on screen before F1", c.typed)
				}
				fromMenu := a.fromMenu

				a.Update(f1())
				if a.active != screenHelp {
					t.Fatalf("F1 left the player on screen %v, want help", a.active)
				}
				if v := a.View(); !strings.Contains(v, c.page) {
					t.Fatalf("help did not open on %q:\n%s", c.page, v)
				}

				if closer == "f1" {
					a.Update(f1())
				} else {
					a.Update(tea.KeyMsg{Type: tea.KeyEsc})
				}
				if a.active != c.screen {
					t.Fatalf("%s returned to screen %v, want %v", closer, a.active, c.screen)
				}
				if a.fromMenu != fromMenu {
					t.Errorf("fromMenu changed %v -> %v across help", fromMenu, a.fromMenu)
				}
				if after := a.View(); after != before {
					t.Errorf("%s did not restore the screen exactly:\n--- before\n%s\n--- after\n%s", closer, before, after)
				}
				if c.typed != "" && !strings.Contains(a.View(), c.typed) {
					t.Errorf("typed text %q was thrown away", c.typed)
				}
			})
		}
	}
}

// After the round trip the screen keeps working with what it held: the
// half-typed spawn altitude still commits.
func TestF1KeepsTheSpawnBoxLive(t *testing.T) {
	a := helpTestApp(t)
	press(a, "n")
	for i := 0; i < 4; i++ {
		a.Update(tea.KeyMsg{Type: tea.KeyTab})
	}
	press(a, "enter")
	press(a, "6")
	press(a, "6")
	press(a, "6")
	a.Update(f1())
	a.Update(f1())
	if !a.spawn.CapturingText() {
		t.Fatalf("the altitude box closed across F1")
	}
	press(a, "enter")
	if got := a.spawn.SelectedAltitudeM(); got != 666_000 {
		t.Errorf("altitude after F1 and enter = %v m, want 666000", got)
	}
}

// `?` is a character in a name box; only F1 opens help there.
func TestQuestionMarkStaysATypedCharacterInNameBoxes(t *testing.T) {
	cases := []struct {
		name  string
		setup func(t *testing.T) *App
		in    func(a *App) bool
	}{
		{"saves name", func(t *testing.T) *App {
			a := helpTestApp(t)
			openSavesVia(t, a, "s")
			press(a, "enter")
			typeText(a, "zq")
			return a
		}, func(a *App) bool { return a.active == screenSaves && a.saves.CapturingText() }},
		{"VAB name", func(t *testing.T) *App {
			a := helpTestApp(t)
			press(a, "esc")
			press(a, "b")
			press(a, "a") // a design needs a part before it can be saved
			press(a, "s")
			typeText(a, "zq")
			return a
		}, func(a *App) bool { return a.active == screenVAB }},
		{"roster invite", func(t *testing.T) *App {
			a := rosterApp(t)
			press(a, "i")
			typeText(a, "zq")
			return a
		}, func(a *App) bool { return a.active == screenSession && a.session.CapturingText() }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := c.setup(t)
			a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
			if a.active == screenHelp {
				t.Fatalf("? opened help inside a name box")
			}
			if !c.in(a) {
				t.Fatalf("? took the player out of the box (active=%v)", a.active)
			}
			if !strings.Contains(a.View(), "zq?") {
				t.Errorf("? was not typed into the box:\n%s", a.View())
			}
			// Only F1 gets out, and it comes straight back with the text.
			a.Update(f1())
			if a.active != screenHelp {
				t.Fatalf("F1 did not open help from the box")
			}
			a.Update(f1())
			if !strings.Contains(a.View(), "zq?") {
				t.Errorf("the typed name did not survive F1")
			}
		})
	}
}

// Outside a box `?` is still the alias: it opens help on that screen's page.
func TestQuestionMarkOpensHelpOnTheFormsPage(t *testing.T) {
	a := helpTestApp(t)
	press(a, "n")
	press(a, "?")
	if a.active != screenHelp || !strings.Contains(a.View(), "SPAWN FORM · ") {
		t.Fatalf("? on the spawn form: active=%v", a.active)
	}
}
