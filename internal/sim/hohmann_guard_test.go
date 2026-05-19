package sim

import (
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

const earthMuTest = 3.986004418e14

// circularState returns a circular orbit at radius r in the XY plane
// (plane normal = +Z) about a primary with GM mu.
func circularState(r, mu float64) (orbital.Vec3, orbital.Vec3) {
	return orbital.Vec3{X: r}, orbital.Vec3{Y: math.Sqrt(mu / r)}
}

// TestHohmannGuardDetailCircularCoplanar — a circular orbit coplanar
// with the target plane is within tolerance: no warning.
func TestHohmannGuardDetailCircularCoplanar(t *testing.T) {
	r, v := circularState(7.0e6, earthMuTest)
	nTarget := orbital.Vec3{Z: 1} // same plane as the XY orbit
	if got := hohmannGuardDetail(r, v, earthMuTest, nTarget, hohmannEccTol, hohmannInclTolDeg); got != "" {
		t.Errorf("circular coplanar should not warn, got %q", got)
	}
}

// TestHohmannGuardDetailEccentric — an eccentric (but coplanar)
// departure orbit trips the eccentricity clause.
func TestHohmannGuardDetailEccentric(t *testing.T) {
	r, v := circularState(7.0e6, earthMuTest)
	v = v.Scale(1.25) // raise apoapsis → e well above 0.02
	nTarget := orbital.Vec3{Z: 1}
	got := hohmannGuardDetail(r, v, earthMuTest, nTarget, hohmannEccTol, hohmannInclTolDeg)
	if !strings.Contains(got, "e=") {
		t.Errorf("eccentric orbit should flag e=, got %q", got)
	}
	if strings.Contains(got, "rel.incl") {
		t.Errorf("coplanar orbit should not flag inclination, got %q", got)
	}
}

// TestHohmannGuardDetailInclined — a circular orbit tilted out of
// the target plane trips the inclination clause (and not e).
func TestHohmannGuardDetailInclined(t *testing.T) {
	r, v := circularState(7.0e6, earthMuTest)
	// Target plane normal tilted 30° off the craft's +Z normal.
	nTarget := orbital.Vec3{Y: math.Sin(30 * math.Pi / 180), Z: math.Cos(30 * math.Pi / 180)}
	got := hohmannGuardDetail(r, v, earthMuTest, nTarget, hohmannEccTol, hohmannInclTolDeg)
	if !strings.Contains(got, "rel.incl 30.0") {
		t.Errorf("30° tilt should flag rel.incl 30.0, got %q", got)
	}
	if strings.Contains(got, "e=") {
		t.Errorf("circular orbit should not flag eccentricity, got %q", got)
	}
}

// TestHohmannGuardDetailDegenerateSilent — degenerate inputs (zero
// target normal, zero velocity) must stay silent, never cry wolf.
func TestHohmannGuardDetailDegenerateSilent(t *testing.T) {
	r, v := circularState(7.0e6, earthMuTest)
	if got := hohmannGuardDetail(r, v, earthMuTest, orbital.Vec3{}, hohmannEccTol, hohmannInclTolDeg); got != "" {
		t.Errorf("zero target normal should be silent, got %q", got)
	}
	if got := hohmannGuardDetail(r, orbital.Vec3{}, earthMuTest, orbital.Vec3{Z: 1}, hohmannEccTol, hohmannInclTolDeg); got != "" {
		t.Errorf("zero craft velocity should be silent, got %q", got)
	}
}

// moonIndex finds the Moon's body index in the active system, or -1.
func moonIndex(w *World) int {
	for i, b := range w.System().Bodies {
		if b.ID == "moon" || b.EnglishName == "Moon" {
			return i
		}
	}
	return -1
}

// TestHohmannDepartureWarningLEOtoLuna — the default equatorial LEO
// is ~circular but Luna's orbit is inclined ~5.1° to it, so the
// intra-primary auto-plant is off-plane: the advisory must fire and
// name the inclination (this is exactly the reported "only works
// from a 0° circular orbit" symptom, now surfaced not silent).
func TestHohmannDepartureWarningLEOtoLuna(t *testing.T) {
	w := mustWorld(t)
	mi := moonIndex(w)
	if mi < 0 {
		t.Skip("Moon missing from Sol")
	}
	warn := w.HohmannDepartureWarning(mi)
	if warn == "" {
		t.Fatal("expected an advisory for equatorial-LEO → inclined-Luna")
	}
	if !strings.Contains(warn, "rel.incl") {
		t.Errorf("advisory should name the plane mismatch, got %q", warn)
	}

	// The preview surfaces the same advisory as a 4th HUD line.
	p := w.HohmannPreviewFor(mi)
	if !p.Valid || p.Warn == "" {
		t.Fatalf("intra-primary preview should be Valid with a Warn, got valid=%v warn=%q", p.Valid, p.Warn)
	}
	if lines := p.Format(); len(lines) != 4 || !strings.Contains(lines[3], "⚠") {
		t.Errorf("Format should append a ⚠ advisory line, got %v", lines)
	}
}

// TestHohmannDepartureWarningOutOfScopeSilent — the guard is scoped
// to the intra-primary path; heliocentric targets and the system
// primary must return no advisory.
func TestHohmannDepartureWarningOutOfScopeSilent(t *testing.T) {
	w := mustWorld(t)
	if got := w.HohmannDepartureWarning(0); got != "" {
		t.Errorf("system primary should be silent, got %q", got)
	}
	marsIdx := -1
	for i, b := range w.System().Bodies {
		if b.ID == "mars" || b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx >= 0 {
		if got := w.HohmannDepartureWarning(marsIdx); got != "" {
			t.Errorf("heliocentric Mars is out of guard scope, want silent, got %q", got)
		}
	}
}
