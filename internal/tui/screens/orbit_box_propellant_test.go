// ADR 0051 slice 2a: PROPELLANT's Δv→circ cell is the load-bearing rule
// here, C4 says it must show on an AIRLESS ascent too (closing #454),
// gated on isSubOrbitalClimb alone, with no atmosphere test at the call
// site. That's exactly the gap the retired buildDescentChip left open.

package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestPropellantBoxDeltaVToCircOnAirlessAscent is the C4/#454 regression:
// a lander climbing off the Moon (airless) must show Δv→circ, not a
// dash. Sabotage-first proof lives in the box comment history (slice 1
// already proved shouldShowLaunchHUD's atmosphere gate is what caused
// this gap); this test proves the NEW box actually closes it.
func TestPropellantBoxDeltaVToCircOnAirlessAscent(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	moonBody := sysBody(t, w, "Moon")
	c.Primary = *moonBody
	c.Landed = false
	// A shallow ellipse (periapsis 2 km below the surface, apoapsis 25 km
	// above it), craft placed just past periapsis and climbing out, a
	// REAL orbit with non-zero angular momentum, matching the
	// isSubOrbitalClimb "climb after a deorbit hop" fixture (a purely
	// radial straight-up velocity is degenerate: zero angular momentum
	// reads as e=1, which craftLiveElements correctly refuses as "no
	// figure worth reading": that would test a fixture bug, not this
	// box).
	mu := moonBody.GravitationalParameter()
	rp := moonBody.RadiusMeters() - 2_000
	ra := moonBody.RadiusMeters() + 25_000
	a := (rp + ra) / 2
	r := rp + 1_000
	vTangential := math.Sqrt(mu * (2/r - 1/a))
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r, Y: 0, Z: 0},
		V: orbital.Vec3{X: 5, Y: vTangential, Z: 0}, // small radial-out (climbing) + tangential
	}
	c.State.M = c.TotalMass()
	if !isSubOrbitalClimb(c) {
		t.Fatal("setup: expected this state to read as a sub-orbital climb")
	}
	lines := v.buildPropellantBox(w)
	if !strings.Contains(lines[2], "Δv→circ:") {
		t.Fatalf("Δv row missing entirely: %v", lines)
	}
	if strings.Contains(lines[2], "Δv→circ:      —") || strings.HasSuffix(strings.TrimSpace(lines[2]), "—") {
		t.Errorf("Δv row = %q, want a real Δv→circ figure on an airless ascent (C4/#454), not a dash", lines[2])
	}
}

// TestPropellantBoxDeltaVToCircDashInStableOrbit: a circular orbit (Pe
// above the surface) is not a sub-orbital climb, so the cell reads a
// dash rather than the stale "0.00 m/s 0s" the first prototype printed
// (audit C65).
func TestPropellantBoxDeltaVToCircDashInStableOrbit(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	moonBody := sysBody(t, w, "Moon")
	c.Primary = *moonBody
	c.Landed = false
	mu := moonBody.GravitationalParameter()
	r := moonBody.RadiusMeters() + 210_000
	vCirc := math.Sqrt(mu / r)
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r, Y: 0, Z: 0},
		V: orbital.Vec3{X: 0, Y: vCirc, Z: 0},
	}
	c.State.M = c.TotalMass()
	if isSubOrbitalClimb(c) {
		t.Fatal("setup: a stable circular orbit must not read as a sub-orbital climb")
	}
	lines := v.buildPropellantBox(w)
	if !strings.HasSuffix(strings.TrimRight(lines[2], " "), "—") {
		t.Errorf("Δv row in a stable circular orbit = %q, want Δv→circ: — (not a stale burn figure)", lines[2])
	}
}

// TestPropellantBoxMonopropDashWithNoRCSTank: decision 2, a craft with
// no monoprop capacity shows the row with dashes, rather than the
// retired buildVesselChip's behaviour of dropping the row outright.
func TestPropellantBoxMonopropDashWithNoRCSTank(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.MonopropCapacity = 0
	lines := v.buildPropellantBox(w)
	if len(lines) != 4 {
		t.Fatalf("buildPropellantBox returned %d lines, want 4 (title, fuel/mass, Δv, monoprop/rcs)", len(lines))
	}
	if !strings.Contains(lines[3], "monoprop:") || !strings.Contains(lines[3], "—") {
		t.Errorf("monoprop row with no RCS tank = %q, want a dash cell, not a dropped row", lines[3])
	}
}
