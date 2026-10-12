package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func bodyByID(t *testing.T, w *sim.World, id string) (int, bodies.CelestialBody) {
	t.Helper()
	for i, b := range w.System().Bodies {
		if b.ID == id {
			return i, b
		}
	}
	t.Fatalf("body %q not in the default system", id)
	return -1, bodies.CelestialBody{}
}

func smallBodyNamed(v *OrbitView, id string) (smallBodyDisk, bool) {
	for _, s := range v.smallBodies {
		if s.id == id {
			return s, true
		}
	}
	return smallBodyDisk{}, false
}

// Zooming in on a small moon has to grow it a dot at a time. The old tier
// fallback drew a moon at 1 dot until its true size reached 4, so four
// presses of + changed nothing and the fifth jumped 1 -> 4 (GM, "fix without
// asking"). A body is never drawn smaller than its true size, and never
// jumps more than a dot between neighbouring zooms.
func TestSmallBodyNeverDrawnBelowTrueSize(t *testing.T) {
	moon := bodies.CelestialBody{MeanRadius: 1737.4} // km, a 1-dot-tier moon
	r := moon.RadiusMeters()
	prev := 0
	for truePx := 0.5; truePx <= 9.0; truePx += 0.05 {
		scale := truePx / r
		got := BodyPixelRadius(moon, false, scale, 0)
		if want := int(math.Round(r * scale)); got < want {
			t.Fatalf("true radius %.2f dots drawn at %d: smaller than true size (%d)", truePx, got, want)
		}
		if got < prev {
			t.Fatalf("true radius %.2f dots drawn at %d after %d: zooming in shrank the body", truePx, got, prev)
		}
		if prev != 0 && got-prev > 1 {
			t.Fatalf("true radius %.2f dots: drawn size jumped %d -> %d", truePx, prev, got)
		}
		prev = got
	}
}

// Every body draws at least a 2-dot disk (GM Q1): the Moon framed from a
// pass used to be one dot under its own glyph, i.e. not on the screen.
func TestSmallBodyHasMinimumDrawnRadius(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"moon", "phobos", "deimos", "enceladus"} {
		_, b := bodyByID(t, w, id)
		for _, scale := range []float64{0, 1e-12, 1e-9, 1e-7} {
			if got := mapBodyPixelRadius(b, false, scale, 1000); got < 2 {
				t.Errorf("%s at scale %g draws %d dots, want at least 2", id, scale, got)
			}
		}
	}

	// Through the real map: Earth framed, the Moon in view.
	earthIdx, _ := bodyByID(t, w, "earth")
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: earthIdx}
	w.ViewMode = sim.ViewTop
	v := NewOrbitView(plainTheme())
	v.Resize(140, 40)
	v.Render(w, 0, 140, 40)
	moon, ok := smallBodyNamed(v, "moon")
	if !ok {
		t.Fatal("setup: the Moon was not drawn in the Earth-focus frame")
	}
	if moon.r < 2 {
		t.Errorf("the Moon draws %d dots in the map, want at least 2", moon.r)
	}
}

// At the whole-system view a moon sits inside its planet's drawn disk, and a
// 2-dot floor there recolours the planet's cell with the moon's colour
// (measured in the GM grill: Mars turned Phobos-brown). A moon inside its
// planet's disk keeps its small size.
func TestMoonInsidePlanetDiskIsExempt(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.Focus = sim.Focus{Kind: sim.FocusSystem}
	w.ViewMode = sim.ViewTop
	v := NewOrbitView(plainTheme())
	v.Resize(140, 40)
	v.Render(w, 0, 140, 40)

	_, phobos := bodyByID(t, w, "phobos")
	_, mars := bodyByID(t, w, "mars")
	scale := v.canvas.Scale()
	// Instrument: the floor applies to Phobos on its own, and Phobos really
	// is inside Mars's drawn disk in this frame.
	if got := mapBodyPixelRadius(phobos, false, scale, 1000); got < 2 {
		t.Fatalf("setup: Phobos floors to %d, the exemption has nothing to exempt", got)
	}
	marsR := mapBodyPixelRadius(mars, false, scale, 1000)
	if d := w.BodyPosition(phobos).Sub(w.BodyPosition(mars)).Norm() * scale; d > float64(marsR) {
		t.Fatalf("setup: Phobos is %.1f dots from Mars, outside its %d-dot disk", d, marsR)
	}

	ph, ok := smallBodyNamed(v, "phobos")
	if !ok {
		t.Fatal("Phobos not drawn at the system view")
	}
	if ph.r >= 2 {
		t.Errorf("Phobos inside Mars's disk draws %d dots: the floor recolours the planet", ph.r)
	}
	// Main already lets a 1-dot moon win a cell or two of its planet (2 cells
	// at this size, measured on d0f0384 before the floor); a floored moon
	// takes more.
	if n := v.canvas.CountColor(render.ColorFor(phobos)); n > 2 {
		t.Errorf("%d cells show Phobos's colour at the system view, main shows 2: the floor recoloured Mars", n)
	}
}
