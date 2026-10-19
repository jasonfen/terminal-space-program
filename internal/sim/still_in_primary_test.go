package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

func bodyByID(t *testing.T, sys bodies.System, id string) bodies.CelestialBody {
	t.Helper()
	for _, b := range sys.Bodies {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("no body %q in %s", id, sys.Name)
	return bodies.CelestialBody{}
}

// TestBodyPosRelToMatchesBodyPositionAtDifference: the one-solve shortcut
// for a body relative to its own parent equals the two-ephemeris
// difference it replaces, and the non-parent fallback does too.
func TestBodyPosRelToMatchesBodyPositionAtDifference(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon, earth := bodyByID(t, sys, "moon"), bodyByID(t, sys, "earth")
	for _, pair := range [][2]bodies.CelestialBody{{moon, earth}, {earth, moon}} {
		for d := 0; d < 40; d++ {
			tt := w.Clock.SimTime.Add(time.Duration(d) * 17 * time.Hour)
			got := w.bodyPosRelTo(pair[0], pair[1], tt)
			want := w.BodyPositionAt(pair[0], tt).Sub(w.BodyPositionAt(pair[1], tt))
			if got.Sub(want).Norm() > 1e-9*want.Norm() {
				t.Fatalf("%s rel %s at +%dh: got %v want %v", pair[0].ID, pair[1].ID, d*17, got, want)
			}
		}
	}
}

// TestStillInPrimaryAgreesWithFindPrimary: a craft flown through Earth's
// space, into the Moon's SOI and out again (and, from the Moon's frame,
// out of the Moon's SOI) gets the same primary from the shortcut as from
// the full FindPrimary at every step. The walk must enter the Moon's SOI,
// or the child-SOI check was never exercised.
func TestStillInPrimaryAgreesWithFindPrimary(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon, earth := bodyByID(t, sys, "moon"), bodyByID(t, sys, "earth")
	soiMoon := physics.SOIRadius(moon, earth)
	soiEarth := physics.SOIRadius(earth, sys.Bodies[0])

	check := func(label string, cur bodies.CelestialBody, tt time.Time, r orbital.Vec3) (inside bool) {
		positions := map[string]orbital.Vec3{}
		for _, b := range sys.Bodies {
			positions[b.ID] = w.BodyPositionAt(b, tt)
		}
		full := physics.FindPrimary(sys, positions[cur.ID].Add(r), positions).Body.ID == cur.ID
		if got := w.stillInPrimary(&sys, cur, r, tt); got != full {
			t.Fatalf("%s at %s: stillInPrimary=%v but FindPrimary stays in %s=%v (|r|=%.0f km)",
				label, tt.Format(time.RFC3339), got, cur.ID, full, r.Norm()/1e3)
		}
		return full
	}

	// Earth frame: the craft rides the Moon's position plus an offset that
	// closes from about 3 SOI radii to dead centre and back out.
	const n = 120
	enteredMoon, stayedEarth := 0, 0
	for i := 0; i <= n; i++ {
		tt := w.Clock.SimTime.Add(time.Duration(i) * 6 * time.Hour)
		off := 2.95 * soiMoon * math.Abs(float64(i)-n/2) / (n / 2)
		r := w.bodyPosRelTo(moon, earth, tt).Add(orbital.Vec3{X: off})
		if check("earth frame", earth, tt, r) {
			stayedEarth++
		} else {
			enteredMoon++
		}
	}
	if enteredMoon == 0 || stayedEarth == 0 {
		t.Fatalf("walk never crossed the Moon's SOI (in Moon=%d, in Earth=%d); the check proved nothing", enteredMoon, stayedEarth)
	}

	// Moon frame: from deep inside to well outside the Moon's SOI.
	left := 0
	for i := 0; i <= 40; i++ {
		r := orbital.Vec3{Y: 1.9 * soiMoon * float64(i) / 40}
		if !check("moon frame", moon, w.Clock.SimTime, r) {
			left++
		}
	}
	if left == 0 {
		t.Fatal("walk never left the Moon's SOI")
	}

	// Earth leaving its own SOI for the root.
	if check("earth exit", earth, w.Clock.SimTime, orbital.Vec3{X: 1.5 * soiEarth}) {
		t.Fatal("a craft 1.5 Earth SOI radii out still reads as Earth's")
	}
}
