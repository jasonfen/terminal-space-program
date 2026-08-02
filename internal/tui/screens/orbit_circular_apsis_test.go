package screens

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// placeOnConic puts the active craft on the coplanar conic with the given
// apsis radii, at true anomaly nu. Returns the orbital period.
func placeOnConic(w *sim.World, rPeri, rApo, nu float64) float64 {
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	a := (rPeri + rApo) / 2
	e := (rApo - rPeri) / (rApo + rPeri)
	p := a * (1 - e*e)
	r := p / (1 + e*math.Cos(nu))
	// Perifocal position/velocity, then used directly as the equatorial
	// frame (ω = Ω = i = 0) — enough for an apsis-timing readout.
	h := math.Sqrt(mu * p)
	c.State.R.X, c.State.R.Y, c.State.R.Z = r*math.Cos(nu), r*math.Sin(nu), 0
	c.State.V.X = -mu / h * math.Sin(nu)
	c.State.V.Y = mu / h * (e + math.Cos(nu))
	c.State.V.Z = 0
	return 2 * math.Pi * math.Sqrt(a*a*a/mu)
}

// apsisRow pulls the value of the named ORBIT chip row out of a render.
func apsisRow(t *testing.T, out, label string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(label) + `\s*(\S+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no %s row in the render:\n%s", label, out)
	}
	return m[1]
}

// #286: on a perfectly circular orbit every point is at the same radius,
// so there is no apoapsis or periapsis to count down to. The readout used
// to print exactly half a period, frozen — a number that looks live and
// isn't, which players tried to phase off. It has to say so instead.
//
// Spawn presets produce exactly-circular orbits, so this is what a player
// sees on a fresh spawn before any burn adds eccentricity.
func TestOrbitChipApsisTimesDegenerateOnCircularOrbit(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	r := w.ActiveCraft().Primary.RadiusMeters() + 500e3

	// Two craft positions a quarter turn apart on the SAME circular orbit —
	// the live symptom was both reading an identical, unmoving P/2.
	var seen []string
	for _, nu := range []float64{0, math.Pi / 2} {
		period := placeOnConic(w, r, r, nu)
		out := v.Render(w, 0, 200, 60)
		for _, label := range []string{"t→Ap:", "t→Pe:"} {
			val := apsisRow(t, out, label)
			if val != "—" {
				t.Errorf("circular orbit at ν=%.2f: %s %q, want \"—\" (apsides are undefined at e=0)",
					nu, label, val)
			}
		}
		seen = append(seen, apsisRow(t, out, "t→Ap:"))
		_ = period
	}
	if len(seen) == 2 && seen[0] != seen[1] {
		t.Errorf("t→Ap differed between two points on the same circular orbit: %q vs %q", seen[0], seen[1])
	}
}

// The companion guard: the fix must not swallow real apsis timers. The
// live remedy in #286 was a single 0.1 m/s prograde pulse that raised Ap
// by 0.4 km and restored 1 s/s ticking — that orbit still has to read a
// duration, and one that moves as the craft advances.
func TestOrbitChipApsisTimesLiveOnSlightlyEccentricOrbit(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	primaryR := w.ActiveCraft().Primary.RadiusMeters()
	rPeri, rApo := primaryR+500.0e3, primaryR+500.4e3

	placeOnConic(w, rPeri, rApo, 0)
	atPeri := apsisRow(t, v.Render(w, 0, 200, 60), "t→Ap:")
	placeOnConic(w, rPeri, rApo, math.Pi/2)
	quarterOn := apsisRow(t, v.Render(w, 0, 200, 60), "t→Ap:")

	if strings.Contains(atPeri, "—") || strings.Contains(quarterOn, "—") {
		t.Fatalf("0.4 km of apsis separation read as degenerate: %q / %q", atPeri, quarterOn)
	}
	if atPeri == quarterOn {
		t.Errorf("t→Ap frozen at %q across a quarter orbit — the timer is not tracking position", atPeri)
	}
}
