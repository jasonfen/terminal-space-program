package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// speedRow returns NAVIGATION's horiz:/speed: row.
func speedRow(t *testing.T, v *OrbitView, w *sim.World) string {
	t.Helper()
	for _, l := range v.buildNavigationBox(w) {
		if strings.Contains(l, "speed:") {
			return l
		}
	}
	t.Fatal("NAVIGATION has no speed: row")
	return ""
}

// #601: speed: names the navball frame it is measured in.
func TestNavigationSpeedNamesTheFrame(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)

	// Pad: SURF, ground speed ~0 (main read "speed: 407.9 m/s", no frame).
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth",
		Launchpad: true, Latitude: 28.6083, LongitudeOffset: -80.604,
	}); err != nil {
		t.Fatal(err)
	}
	row := speedRow(t, v, w)
	if !strings.Contains(row, "0.00 m/s (surface)") {
		t.Errorf("pad row = %q, want speed 0.00 m/s (surface)", row)
	}

	// ORBIT frame: the orbit number, named.
	w.NavMode = sim.NavOrbit
	row = speedRow(t, v, w)
	if !strings.Contains(row, "m/s (orbit)") || strings.Contains(row, " 0.00 m/s (orbit)") {
		t.Errorf("orbit row = %q, want the ~408 m/s orbit number named (orbit)", row)
	}

	// TGT frame: relative to the target, named.
	w2 := inclinedCircularEarthOrbitCraft(t, 0, 500e3)
	if _, err := w2.SpawnCraft(sim.SpawnSpec{AltitudeM: 500e3}); err != nil {
		t.Fatal(err)
	}
	w2.ActiveCraftIdx = 0
	w2.Crafts[1].Primary = w2.Crafts[0].Primary
	w2.Crafts[1].State.R = w2.Crafts[0].State.R.Add(orbital.Vec3{X: 5000})
	w2.Crafts[1].State.V = w2.Crafts[0].State.V.Add(orbital.Vec3{X: -3})
	w2.SetTargetCraft(1)
	w2.NavMode = sim.NavTarget
	row = speedRow(t, v, w2)
	if !strings.Contains(row, "3.00 m/s (target)") {
		t.Errorf("target row = %q, want 3.00 m/s (target)", row)
	}
}

// The new word must fit NAVIGATION's pinned width at both design sizes'
// worst common case (fast orbit speed, longest frame word).
func TestNavigationSpeedWordFitsTheWidthPin(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	w := inclinedCircularEarthOrbitCraft(t, 28.6, 200e3)
	for _, nav := range []sim.NavMode{sim.NavSurface, sim.NavOrbit} {
		w.NavMode = nav
		row := speedRow(t, v, w)
		if got := lipgloss.Width(row); got > 62 {
			t.Errorf("%v speed row is %d cells, over the 62-cell tier: %q", nav, got, row)
		}
	}
}
