package screens

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

var fgTruecolor = regexp.MustCompile(`38;2;(\d+);(\d+);(\d+)`)

// shadesOf reads the factors at which a frame paints base: every truecolor
// foreground that is base times a factor (a proportional colour, within
// rounding), as a sorted-unique list of factors. Greys and other hues do not
// match, so the list is the body's own day/night shades.
func shadesOf(frame string, base [3]int) []float64 {
	seen := map[float64]bool{}
	for _, m := range fgTruecolor.FindAllStringSubmatch(frame, -1) {
		r, _ := strconv.Atoi(m[1])
		g, _ := strconv.Atoi(m[2])
		b, _ := strconv.Atoi(m[3])
		f := float64(r) / float64(base[0])
		if f < 0.05 || f > 1.001 {
			continue
		}
		if math.Abs(float64(g)-f*float64(base[1])) > 1.6 || math.Abs(float64(b)-f*float64(base[2])) > 1.6 {
			continue
		}
		seen[math.Round(f*50)/50] = true
	}
	var out []float64
	for f := range seen {
		out = append(out, f)
	}
	return out
}

// bodyFocusFrame renders the default system focused on body id (as `f` would
// land) at the Design Size and returns the frame.
func bodyFocusFrame(t *testing.T, id string) string {
	t.Helper()
	return bodyFocusFrameAt(t, id, 0)
}

// bodyFocusFrameAt is bodyFocusFrame with the clock moved on by hours.
func bodyFocusFrameAt(t *testing.T, id string, hours int) string {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	d := time.Duration(hours) * time.Hour
	w.Clock.SimTime = w.Clock.SimTime.Add(d)
	w.Clock.RotationTime = w.Clock.RotationTime.Add(d)
	idx, _ := bodyByID(t, w, id)
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: idx}
	w.ViewMode = sim.ViewTop
	v := NewOrbitView(plainTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.declutter = true // F2: the instrument boxes would cover the canvas centre
	v.Render(w, 0, DesignWidth, DesignHeight)
	return v.Render(w, 0, DesignWidth, DesignHeight)
}

// A small body gets the Sun's day and night like a big one (GM Q3). Phobos has
// no texture data: it used to be one flat colour at every size.
func TestSmallBodyIsTwoTone(t *testing.T) {
	forceTrueColour(t)
	frame := bodyFocusFrame(t, "phobos")
	if !strings.Contains(frame, "38;2;") {
		t.Fatal("setup: no truecolor in the frame, the profile is not forced")
	}
	phobos := [3]int{0x5A, 0x50, 0x48}
	shades := shadesOf(frame, phobos)
	// Instrument: the body is on screen in its own colour at all.
	hasFull := false
	for _, f := range shades {
		if f > 0.97 {
			hasFull = true
		}
	}
	if !hasFull {
		t.Fatalf("setup: Phobos's own colour is not on screen (shades %v)", shades)
	}
	if len(shades) < 2 {
		t.Fatalf("Phobos paints in %d shade(s) %v, want a lit side and a night side", len(shades), shades)
	}
	lo, hi := shades[0], shades[0]
	for _, f := range shades {
		lo, hi = math.Min(lo, f), math.Max(hi, f)
	}
	if hi-lo < 0.3 {
		t.Errorf("Phobos's shades %v span only %.2f, want a visible day/night difference (>= 0.3)", shades, hi-lo)
	}
}

// The night side is lit to at least the floor on screen too, not only in the
// factor (GM Q4): Io at body focus is mostly night.
func TestNightSideStaysAboveFloorOnScreen(t *testing.T) {
	forceTrueColour(t)
	io := [3]int{0xE8, 0xD9, 0x40}
	// Io sits in Jupiter's shadow at the epoch (one shade, 0.10, the eclipse
	// floor), so walk the clock until it is out of eclipse with a lit and a
	// dark side on screen.
	var shades []float64
	var lo, hi float64
	found := false
	for h := 0; h < 24*14 && !found; h += 3 {
		shades = shadesOf(bodyFocusFrameAt(t, "io", h), io)
		if len(shades) < 2 {
			continue
		}
		lo, hi = shades[0], shades[0]
		for _, f := range shades {
			lo, hi = math.Min(lo, f), math.Max(hi, f)
		}
		found = hi > 0.9 && lo > 0.12 && lo < 0.6
	}
	// Positive control: a frame with a day side and a night side exists, so the
	// minimum below is the night side and not a uniformly bright or eclipsed disk.
	if !found {
		t.Fatalf("setup: no Io frame in two weeks with a day and a night side (last shades %v)", shades)
	}
	if lo < 0.38 {
		t.Errorf("Io's night side paints at %.2f of its colour, want at least 0.40", lo)
	}
}

// The centre glyph (○ ● ◉) says "a body" on a 1- or 2-dot disk where shading
// cannot; from 3 dots up the shading says it and the glyph goes (GM Q3).
func TestCentreGlyphOnlyOnTinyBodies(t *testing.T) {
	const moonGlyph = "○"
	// Small disk: Earth framed and zoomed out until the Moon is in view.
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := bodyByID(t, w, "earth")
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: idx}
	w.ViewMode = sim.ViewTop
	v := NewOrbitView(plainTheme())
	v.Resize(DesignWidth, DesignHeight)
	v.declutter = true
	v.Render(w, 0, DesignWidth, DesignHeight)
	_, moonBody := bodyByID(t, w, "moon")
	for i := 0; i < 20; i++ {
		if _, _, on := v.canvas.Project(w.BodyPosition(moonBody)); on {
			break
		}
		v.ZoomOut()
		v.Render(w, 0, DesignWidth, DesignHeight)
	}
	small := v.Render(w, 0, DesignWidth, DesignHeight)
	if m, ok := smallBodyNamed(v, "moon"); !ok || m.r > 2 {
		t.Fatalf("setup: the Moon is not a small disk in this frame (%+v)", m)
	}
	if !strings.Contains(small, moonGlyph) {
		t.Fatalf("setup: no %s on a 2-dot Moon", moonGlyph)
	}
	// A 12-cell Phobos at body focus: lit, no glyph on it.
	big := bodyFocusFrame(t, "phobos")
	if strings.Contains(big, moonGlyph) {
		t.Errorf("a %s sits on a disk that is 12 cells wide: shading should say \"body\", not the glyph", moonGlyph)
	}
	_ = render.GlyphFor
}
