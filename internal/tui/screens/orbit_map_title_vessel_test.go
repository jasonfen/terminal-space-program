// ADR 0051 decision 6 as amended by #1019: the map title always names the
// active vessel, once. `focus:` appears only when the camera is NOT on it.

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestMapTitleNamesActiveVesselOnceAndFocusOnlyOffIt pins every row of the
// #1019 table. Camera focus is independent of the active vessel (a camera
// on a body, the system, or a ghost), so the name must come from the ACTIVE
// VESSEL, not the focus. The own-vessel row compares by focus kind (the
// camera is on the active vessel), never by name: "Falcon Heavy-1" is a
// prefix of the active "Falcon Heavy-1-booster2".
func TestMapTitleNamesActiveVesselOnceAndFocusOnlyOffIt(t *testing.T) {
	const active = "Falcon Heavy-1-booster2"
	tests := []struct {
		name      string
		focus     func(w *sim.World) sim.Focus
		wantFocus string // "" = no `focus:` at all
	}{
		{"camera on your vessel", func(*sim.World) sim.Focus { return sim.Focus{Kind: sim.FocusCraft} }, ""},
		{"camera on a body", func(w *sim.World) sim.Focus {
			for i, b := range w.System().Bodies {
				if b.EnglishName == "Moon" {
					return sim.Focus{Kind: sim.FocusBody, BodyIdx: i}
				}
			}
			t.Fatal("setup: no Moon in the default system")
			return sim.Focus{}
		}, "focus: Moon"},
		{"camera on the whole system", func(*sim.World) sim.Focus { return sim.Focus{Kind: sim.FocusSystem} }, "focus: System-wide"},
		{"camera on another player's vessel", func(w *sim.World) sim.Focus {
			w.Ghosts = []sim.Ghost{{Owner: "SHA256:gern", CraftID: 7, Handle: "gern", Name: "gern's ship"}}
			return sim.Focus{Kind: sim.FocusGhost, GhostOwner: "SHA256:gern", GhostCraftID: 7}
		}, "focus: spectating gern"},
		{"camera on a body while flying a second local vessel", func(w *sim.World) sim.Focus {
			// The vessel you are not flying keeps `focus:` (the camera is on a
			// body); the active one is named once and is the second slate entry.
			other := *w.ActiveCraft()
			other.Name = "Falcon Heavy-1"
			w.Crafts = append([]*spacecraft.Spacecraft{&other}, w.Crafts...)
			w.SetActiveCraftIdx(1)
			return sim.Focus{Kind: sim.FocusBody, BodyIdx: 1}
		}, "focus: "},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewOrbitView(launchThemeForTest())
			v.Resize(200, DesignHeight)
			w, err := sim.NewWorld()
			if err != nil {
				t.Fatalf("NewWorld: %v", err)
			}
			w.ActiveCraft().Name = active
			w.Focus = tc.focus(w)

			// 200 wide so the left field is not cut (the 140-column cut is left as decided in #1019).
			title := strings.Split(v.Render(w, 0, 200, DesignHeight), "\n")[0]
			t.Logf("title: %s", strings.TrimRight(title, " "))
			v.Resize(DesignWidth, DesignHeight)
			t.Logf("at 140: %s", strings.TrimRight(strings.Split(v.Render(w, 0, DesignWidth, DesignHeight), "\n")[0], " "))
			if n := strings.Count(title, active); n != 1 {
				t.Errorf("active vessel named %d times, want once: %q", n, title)
			}
			if tc.wantFocus == "" {
				if strings.Contains(title, "focus:") {
					t.Errorf("camera on your vessel must show no focus: %q", title)
				}
			} else if !strings.Contains(title, tc.wantFocus) {
				t.Errorf("title lacks %q: %q", tc.wantFocus, title)
			}
		})
	}
}
