package screens

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// bodyInfoFrame renders body info at 140x40 for the named body in the named
// system (index into w.Systems), returning the plain frame text.
func bodyInfoFrame(t *testing.T, w *sim.World, system, bodyID string) string {
	t.Helper()
	for w.System().Name != system {
		w.CycleSystem()
		if w.SystemIdx == 0 && system != w.System().Name {
			t.Fatalf("system %q not found", system)
		}
	}
	for i, b := range w.System().Bodies {
		if b.ID == bodyID {
			return NewBodyInfo(chipTestTheme()).Render(w, i, 140, 40)
		}
	}
	t.Fatalf("body %q not in %s", bodyID, system)
	return ""
}

func newBodyInfoWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

// rowValue returns what follows "label:" on its row, up to the next box
// border, trimmed.
func rowValue(t *testing.T, frame, label string) string {
	t.Helper()
	m := regexp.MustCompile(regexp.QuoteMeta(label) + `:\s+([^│]*?)\s*│`).FindStringSubmatch(frame)
	if m == nil {
		t.Fatalf("no %q row in:\n%s", label, frame)
	}
	return m[1]
}

// TestBodyInfoApsidesRideTheDistanceLadder (#617 a): Perihelion/Aphelion
// (Periapsis/Apoapsis for a moon) print on the same ladder as the semimajor
// axis, never in AU beside a Mm figure.
func TestBodyInfoApsidesRideTheDistanceLadder(t *testing.T) {
	w := newBodyInfoWorld(t)
	moon := bodyInfoFrame(t, w, "Sol", "moon")
	// Instrument check: the label finder reads a row that exists.
	if got := rowValue(t, moon, "Semimajor axis"); got != "384.4 Mm" {
		t.Fatalf("setup: Moon semimajor axis reads %q, want 384.4 Mm", got)
	}
	if got := rowValue(t, moon, "Periapsis"); got != "363.3 Mm" {
		t.Errorf("Moon periapsis = %q, want 363.3 Mm", got)
	}
	if got := rowValue(t, moon, "Apoapsis"); got != "405.5 Mm" {
		t.Errorf("Moon apoapsis = %q, want 405.5 Mm", got)
	}
	// A planet rides the same ladder, which itself goes to AU at heliocentric
	// distances: the rows must equal readout.Distance, not a private format.
	earth := bodyInfoFrame(t, w, "Sol", "earth")
	e := w.Systems[0].FindBody("earth")
	if got, want := rowValue(t, earth, "Perihelion"), readout.Distance(e.SemimajorAxisMeters()*(1-e.Eccentricity)); got != want {
		t.Errorf("Earth perihelion = %q, want %q (distance ladder)", got, want)
	}
	// Kern orbits at 13.6 Gm: the ladder says Gm, the old row said 0.0909 AU.
	kern := bodyInfoFrame(t, w, "Lumen", "kern")
	if got := rowValue(t, kern, "Perihelion"); !strings.HasSuffix(got, " Gm") {
		t.Errorf("Kern perihelion = %q, want a Gm figure beside its Gm semimajor axis", got)
	}
	if strings.Contains(kern, " AU") {
		t.Errorf("Kern body info prints AU:\n%s", kern)
	}
}

// TestBodyInfoMoonsSayPeriapsisApoapsis (#617 b): a body whose parent is not
// a star reads periapsis/apoapsis; a planet keeps perihelion/aphelion.
func TestBodyInfoMoonsSayPeriapsisApoapsis(t *testing.T) {
	w := newBodyInfoWorld(t)
	cases := []struct {
		system, id string
		moon       bool
	}{{"Sol", "moon", true}, {"Sol", "phobos", true}, {"Sol", "earth", false}, {"Lumen", "cursor", true}, {"Lumen", "kern", false}}
	for _, c := range cases {
		f := bodyInfoFrame(t, w, c.system, c.id)
		hasPeri := strings.Contains(f, "Periapsis:") && strings.Contains(f, "Apoapsis:")
		hasHelion := strings.Contains(f, "Perihelion:") && strings.Contains(f, "Aphelion:")
		if c.moon && (!hasPeri || hasHelion) {
			t.Errorf("%s/%s is a moon: want Periapsis/Apoapsis rows only:\n%s", c.system, c.id, f)
		}
		if !c.moon && (hasPeri || !hasHelion) {
			t.Errorf("%s/%s is a planet: want Perihelion/Aphelion rows only:\n%s", c.system, c.id, f)
		}
	}
}

// TestBodyInfoDerivesMissingPhysicalRows (#617 c): the catalog leaves
// Gravity / Escape / Density out for every Sol moon and most Lumen bodies; the
// screen derives them from mass and radius instead of printing zeros.
func TestBodyInfoDerivesMissingPhysicalRows(t *testing.T) {
	w := newBodyInfoWorld(t)
	for _, c := range [][2]string{{"Sol", "moon"}, {"Sol", "sun"}, {"Sol", "earth"}, {"Lumen", "kern"}, {"Lumen", "cursor"}} {
		f := bodyInfoFrame(t, w, c[0], c[1])
		for _, zero := range []string{"0.000 m/s²", "0.00 km/s", "0.000 g/cm³"} {
			if strings.Contains(f, zero) {
				t.Errorf("%s/%s prints a %q that is missing data, not a measurement:\n%s", c[0], c[1], zero, f)
			}
		}
	}
	// Known value: lunar surface gravity is about 1.62 m/s².
	moon := bodyInfoFrame(t, w, "Sol", "moon")
	g, err := strconv.ParseFloat(strings.Fields(rowValue(t, moon, "Gravity"))[0], 64)
	if err != nil || g < 1.58 || g > 1.66 {
		t.Errorf("Moon gravity derived as %v (%v), want about 1.62 m/s²", g, err)
	}
	esc, _ := strconv.ParseFloat(strings.Fields(rowValue(t, moon, "Escape velocity"))[0], 64)
	if esc < 2.3 || esc > 2.45 {
		t.Errorf("Moon escape velocity derived as %v km/s, want about 2.38", esc)
	}
	den, _ := strconv.ParseFloat(strings.Fields(rowValue(t, moon, "Density"))[0], 64)
	if den < 3.2 || den > 3.5 {
		t.Errorf("Moon density derived as %v g/cm³, want about 3.34", den)
	}
	// Catalog values still win where the catalog has them.
	earth := bodyInfoFrame(t, w, "Sol", "earth")
	if got := rowValue(t, earth, "Gravity"); got != "9.807 m/s²" {
		t.Errorf("Earth gravity = %q, want the catalog's 9.807 m/s²", got)
	}
}

// TestBodyInfoMissingDataPrintsDashes: with no radius the derivations are
// impossible, so the rows say "--" rather than 0 or Inf.
func TestBodyInfoMissingDataPrintsDashes(t *testing.T) {
	w := newBodyInfoWorld(t)
	for i := range w.Systems[0].Bodies {
		if w.Systems[0].Bodies[i].ID == "phobos" {
			w.Systems[0].Bodies[i].MeanRadius = 0
		}
	}
	f := bodyInfoFrame(t, w, "Sol", "phobos")
	for _, l := range []string{"Gravity", "Escape velocity", "Density"} {
		if got := rowValue(t, f, l); got != "--" {
			t.Errorf("%s with no radius = %q, want --", l, got)
		}
	}
}

// TestBodyInfoSpellsSidereal (#617 d).
func TestBodyInfoSpellsSidereal(t *testing.T) {
	w := newBodyInfoWorld(t)
	f := bodyInfoFrame(t, w, "Sol", "earth")
	if strings.Contains(f, "Sideral") {
		t.Errorf("body info still spells it Sideral:\n%s", f)
	}
	if !strings.Contains(f, "Sidereal period:") || !strings.Contains(f, "Sidereal rot.:") {
		t.Errorf("body info lacks Sidereal period / Sidereal rot. rows:\n%s", f)
	}
}
