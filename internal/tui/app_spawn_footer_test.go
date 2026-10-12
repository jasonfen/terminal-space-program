package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// GK Q5 (#589): the spawn form's bottom edge follows what Enter and Esc do at
// each moment, instead of saying "[enter] spawn · [esc] cancel" while a
// typing box is open. Driven through App.Update and read from App.View at the
// 140x40 Design Size.

const (
	spawnEdgeField  = "[tab] field · [←/→] step · [enter] type a number · [f] system filter · [esc] cancel"
	spawnEdgeBox    = "[0-9] type · [backspace] erase · [enter] keep this number · [esc] put the old one back"
	spawnEdgeLaunch = "[tab] field · [←/→] cycle · [f] system filter · [enter] spawn · [esc] cancel"
)

func spawnApp(t *testing.T) *App {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	press(a, "n")
	if a.active != screenSpawn {
		t.Fatalf("n did not open the spawn form (active=%v)", a.active)
	}
	return a
}

// bottomEdge is the frame's last row (the legend lives in it).
func bottomEdge(a *App) string {
	lines := strings.Split(strings.TrimRight(a.View(), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], "╰") {
			return lines[i]
		}
	}
	return ""
}

func TestSpawnBottomEdgeFollowsEnterAndEsc(t *testing.T) {
	cases := []struct {
		name string
		tabs int
		typ  string
		box  string // the number box as drawn
	}{
		{"altitude", 4, "4400", "[4400_] km"},
		{"inclination", 2, "28.5", "incl: [28.5_]°"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := spawnApp(t)
			// Opposite state first: before focusing the number, the edge is
			// the plain launch legend, so the assertions below cannot pass
			// by starting in the state they check.
			if e := bottomEdge(a); strings.Contains(e, "type a number") {
				t.Fatalf("setup: edge already says type a number off the field: %q", e)
			}
			for i := 0; i < c.tabs; i++ {
				press(a, "tab")
			}
			// Moment 1: on the field, Enter opens the box.
			if e := bottomEdge(a); !strings.Contains(e, spawnEdgeField) {
				t.Errorf("on the field the edge = %q, want %q", e, spawnEdgeField)
			}
			press(a, "enter")
			for _, r := range c.typ {
				press(a, string(r))
			}
			// Moment 2: typing in the box.
			v := a.View()
			if e := bottomEdge(a); !strings.Contains(e, spawnEdgeBox) {
				t.Errorf("in the box the edge = %q, want %q", e, spawnEdgeBox)
			}
			if !strings.Contains(v, c.box) {
				t.Fatalf("setup: box %q not drawn:\n%s", c.box, v)
			}
			// The hints beside the number are gone, and nothing is cut off.
			for _, gone := range []string{"Enter keeps", "Esc reverts", "Esc rever", "Enter to edit"} {
				if strings.Contains(v, gone) {
					t.Errorf("box still carries the hint %q:\n%s", gone, v)
				}
			}
			// Moment 3: after the box closes, the launch edge plus the
			// warning beside the number.
			press(a, "enter")
			v = a.View()
			if e := bottomEdge(a); !strings.Contains(e, spawnEdgeLaunch) {
				t.Errorf("after the box the edge = %q, want %q", e, spawnEdgeLaunch)
			}
			if !strings.Contains(v, "Enter now launches") {
				t.Errorf("after the box 'Enter now launches' is missing:\n%s", v)
			}
			// Esc in the box lands on the same launch edge.
			press(a, "tab")
			press(a, "tab")
			for i := 0; i < 8; i++ { // wrap back onto the number field
				if strings.Contains(bottomEdge(a), "type a number") {
					break
				}
				press(a, "tab")
			}
			press(a, "enter")
			press(a, "esc")
			if e := bottomEdge(a); !strings.Contains(e, spawnEdgeLaunch) {
				t.Errorf("after esc in the box the edge = %q, want %q", e, spawnEdgeLaunch)
			}
		})
	}
}
