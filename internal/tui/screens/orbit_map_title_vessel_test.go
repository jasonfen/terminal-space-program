// ADR 0051 decision 6: the map title always names the active vessel,
// whatever the camera is looking at (retiring the VESSEL box, which
// used to be the only place the name showed on the map).

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestMapTitleNamesActiveVesselEvenWithCameraOnABody: the drafter's
// premise that the name "is already in the title bar" was half wrong —
// with the camera on a body, the pre-ADR-0051 title showed the focus
// name but never the vessel's, so a player who tabbed the camera to the
// Moon had no on-screen answer to "what am I flying". Camera focus is
// independent of the active vessel here (single-craft slate), so this
// proves the name comes from the ACTIVE VESSEL, not the focus.
func TestMapTitleNamesActiveVesselEvenWithCameraOnABody(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Name = "Saturn V-1"
	// Focus the camera on a body, not the active vessel.
	moonIdx := -1
	for i, b := range w.System().Bodies {
		if b.EnglishName == "Moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx < 0 {
		t.Fatal("setup: no Moon in the default system")
	}
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: moonIdx}

	out := v.Render(w, 0, DesignWidth, DesignHeight)
	titleRow := strings.Split(out, "\n")[0]
	if !strings.Contains(titleRow, "Saturn V-1") {
		t.Errorf("title row = %q, want the active vessel's name even with the camera on a body", titleRow)
	}
	if !strings.Contains(titleRow, "focus:") {
		t.Errorf("title row = %q, want the camera focus to still be named separately", titleRow)
	}
}
