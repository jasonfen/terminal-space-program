package screens

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// poleGuardWorld builds a world with the active craft landed at the
// given Earth latitude (the shipped North Pole preset is 90, see
// internal/sim/launch_sites.go's "North-Pole") and the Moon targeted:
// the review r1 F3 scenario.
func poleGuardWorld(t *testing.T, latitude float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{Launchpad: true, Latitude: latitude})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}
	sys := w.System()
	moonIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "moon" {
			moonIdx = i
			break
		}
	}
	if moonIdx <= 0 {
		t.Fatalf("moon not found in default system")
	}
	w.SetTargetBody(moonIdx)
	return w
}

// TestLaunchChipWithholdsDepartAndDeltaInclAtPole pins review r1 F3: the
// active vessel's own landed co-rotation normal (r x v) is pole-
// degenerate at the shipped North Pole preset: floating-point residue
// leaves it a few times 1e-7 in magnitude, not exactly zero, which the
// pre-fix exact Norm()==0 guards inside spacecraft.HeadingOrbitNormal
// let through as trustworthy. Unguarded, the review measured depart:
// wandering 75.45..108.9° and Δincl: 80.34..72.65° across a day with no
// period. Both rows must withhold at the pole; incl: must not: every
// launch from the pole is genuinely polar, so 90° is an honest number,
// not noise.
func TestLaunchChipWithholdsDepartAndDeltaInclAtPole(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w := poleGuardWorld(t, 90)

	lines := v.buildLaunchChip(w)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "depart:") {
		t.Errorf("expected no depart: row for a vessel landed at the pole, got:\n%s", joined)
	}
	if strings.Contains(joined, "Δincl:") {
		t.Errorf("expected no Δincl: row for a vessel landed at the pole, got:\n%s", joined)
	}
	if !regexp.MustCompile(`\bincl:\s+90\.\d\d°`).MatchString(joined) {
		t.Errorf("expected incl: ~90.00° at the pole (every launch there is polar), got:\n%s", joined)
	}
}

// TestLaunchChipShowsDepartAndDeltaInclOneMetreOffPole is the F3 sibling
// case: ADR 0050 decision 8's own trip radius is ~6mm on Earth, so a
// vessel landed one metre off the pole keeps an honest, real, sweeping
// depart:/Δincl: figure rather than losing it to an over-eager guard.
func TestLaunchChipShowsDepartAndDeltaInclOneMetreOffPole(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	const earthRadiusM = 6.371e6 // matches PlaneNormalOK's own doc comment scale.
	colatDeg := (1.0 / earthRadiusM) * 180 / math.Pi
	w := poleGuardWorld(t, 90-colatDeg)

	lines := v.buildLaunchChip(w)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "depart:") {
		t.Errorf("expected a depart: row one metre off the pole, got:\n%s", joined)
	}
	if !strings.Contains(joined, "Δincl:") {
		t.Errorf("expected a Δincl: row one metre off the pole, got:\n%s", joined)
	}
}
