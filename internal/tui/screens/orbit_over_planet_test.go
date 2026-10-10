package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// lowOrbitTargetingSeed: a vessel in a 200 km orbit targeting the default
// 500 km, 51.6° vessel, the scene from Jason's 2026-10-10 playtest ("the
// target vessel orbit is having trouble rendering against a planet
// backdrop ... across other views too").
func lowOrbitTargetingSeed(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	seed := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth", AltitudeM: 200e3, Inclination: 28.6}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	w.SetTargetCraft(seed)
	w.ViewMode = sim.ViewTop
	return w
}

// On the map the target's orbit crosses the face of Earth. Each braille
// cell takes one colour; Earth's disk filled all 8 dots of a cell and the
// line 1-3, so the disk won and the green line survived only off the disk's
// edges. Lines now win the cells they share with a body.
func TestTargetOrbitShowsAcrossThePlanet(t *testing.T) {
	w := lowOrbitTargetingSeed(t)
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	green := v.canvas.CountColor(render.ColorTarget)
	own := v.canvas.CountColor(render.ColorCurrentOrbit)
	// Measured on this scene: 7 green / 3 own-orbit cells before, 27 / 22
	// after.
	if green < 20 {
		t.Errorf("target orbit shows in %d cells, want >= 20: the planet is outvoting it", green)
	}
	if own < 15 {
		t.Errorf("your orbit shows in %d cells, want >= 15: the planet is outvoting it", own)
	}
}

// Your orbit has to read as yours, not as the atmosphere's edge (Jason
// 2026-10-10: "if the target orbit is bright green, the vessel I am
// controlling should have an equally identifiable orbit color. Right now
// it just looks like the atmosphere edge especially at a low orbit"). The
// old pale slate #A8B8C8 sat 58 from Earth's #9DC8FF haze (this test fails
// on it).
func TestOwnOrbitColourStandsApartFromEarthAndTarget(t *testing.T) {
	w := lowOrbitTargetingSeed(t)
	earth := w.ActiveCraft().Primary
	if earth.Atmosphere == nil || earth.Atmosphere.Color == "" {
		t.Fatal("setup: Earth has no atmosphere colour to compare against")
	}
	for name, other := range map[string]string{
		"Earth's atmosphere": earth.Atmosphere.Color,
		"Earth":              string(render.ColorFor(earth)),
		"TARGET green":       string(render.ColorTarget),
	} {
		if d := hexDistance(t, string(render.ColorCurrentOrbit), other); d < 100 {
			t.Errorf("your orbit %s is %.0f from %s %s, want >= 100", render.ColorCurrentOrbit, d, name, other)
		}
	}
}

func hexDistance(t *testing.T, a, b string) float64 {
	t.Helper()
	ar, ag, ab, ok1 := parseHexColor(a)
	br, bg, bb, ok2 := parseHexColor(b)
	if !ok1 || !ok2 {
		t.Fatalf("unparseable colour %q or %q", a, b)
	}
	dr, dg, db := float64(ar-br), float64(ag-bg), float64(ab-bb)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// The launch view draws the ground at the body's true size but hid an
// orbit's far side behind a radius capped at the canvas reach, so the
// target's far arc drew through the ground as a dotted green line once
// lines stopped losing their cells to the ground. In this framing (200 km,
// looking along the ground) the target's near arc is above the frame, so
// any green on the canvas is far side showing through.
func TestLaunchViewHidesTheTargetOrbitsFarSideBehindTheGround(t *testing.T) {
	w := lowOrbitTargetingSeed(t)
	w.ViewMode = sim.ViewLaunch
	ov := NewOrbitView(launchThemeForTest())
	ov.Resize(DesignWidth, DesignHeight)
	lv := NewLaunchView(launchThemeForTest(), ov)
	lv.Resize(DesignWidth, DesignHeight)
	lv.Render(w, DesignWidth, DesignHeight)
	if n := lv.canvas.CountColor(render.ColorTarget); n != 0 {
		t.Errorf("%d TARGET-green cells in the launch view: the target orbit's far side is drawing through the ground", n)
	}
	if lv.canvas.CountColor(render.ColorCurrentOrbit) < 20 {
		t.Error("setup: your own orbit should be on screen in this framing")
	}
}

// The CommNet beam (a dotted sightline to a ground station) crossing the
// planet's face drew as a wavy teal line once lines won their cells over a
// body (Jason 2026-10-10: "a strange light blue line that renders in an
// wavy shape"). Dotted ink yields over a body again; this zoomed scene had
// 22 beam cells over Earth before.
func TestCommNetBeamDoesNotDrawAcrossThePlanet(t *testing.T) {
	w := lowOrbitTargetingSeed(t)
	if _, _, connected := w.ActiveCommPath(); !connected {
		t.Fatal("setup: no CommNet path to draw")
	}
	v := NewOrbitView(launchThemeForTest())
	v.Resize(DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	for i := 0; i < 6; i++ {
		v.ZoomIn()
	}
	v.Render(w, 0, DesignWidth, DesignHeight)
	if n := v.canvas.CountColor(render.ColorCommLink); n != 0 {
		t.Errorf("%d CommNet-beam cells over the planet, want 0", n)
	}
	// The solid orbits still win over the planet.
	if v.canvas.CountColor(render.ColorTarget) < 10 {
		t.Error("the target's solid orbit no longer shows over the planet")
	}
}
