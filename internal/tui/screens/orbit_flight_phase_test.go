// Package screens — FlightPhase derivation tests (v0.16.1).
//
// deriveFlightPhase consolidates the chip-relevance signals (atmosphere,
// altitude bands, orbit eccentricity, radial-velocity sign, the Landed flag)
// into one phase value. Nothing renders against it yet — these tests pin the
// derivation directly so the vocabulary is trustworthy for a later
// chip-timing slice. The atmospheric-ascent and airless-descent phases are
// kept in lockstep with shouldShowLaunchHUD / shouldShowDescentHUD here so a
// future change to those predicates can't silently drift the phase mapping.

package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// phaseCraftOn builds a Lander on the named body of the default system with
// the given inertial position radius and velocity vector, ready for
// deriveFlightPhase. Velocity is supplied directly so each test can dial in
// the eccentricity / radial-velocity sign it needs.
func phaseCraftOn(t *testing.T, w *sim.World, body string, r orbital.Vec3, v orbital.Vec3) *spacecraft.Spacecraft {
	t.Helper()
	sys := w.System()
	b := sys.FindBody(body)
	if b == nil {
		t.Fatalf("%s not in default system", body)
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *b
	c.Landed = false
	c.State = physics.StateVector{R: r, V: v, M: c.TotalMass()}
	return c
}

func TestDeriveFlightPhaseNilIsCoast(t *testing.T) {
	if got := deriveFlightPhase(nil); got != PhaseCoast {
		t.Errorf("nil craft: got %v, want coast", got)
	}
}

func TestDeriveFlightPhaseLandedWins(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// A craft on the Moon's surface — Landed flag set — reads as Landed even
	// though the airless surface-proximity descent predicate would also fire.
	sys := w.System()
	moon := sys.FindBody("Moon")
	c := phaseCraftOn(t, w, "Moon",
		orbital.Vec3{X: moon.RadiusMeters() + 100, Y: 0, Z: 0},
		orbital.Vec3{})
	c.Landed = true
	if got := deriveFlightPhase(c); got != PhaseLanded {
		t.Errorf("landed craft: got %v, want landed", got)
	}
}

func TestDeriveFlightPhasePrelaunchVsAscent(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	earth := sys.FindBody("Earth")
	if earth == nil {
		t.Fatal("Earth not in default system")
	}
	Re := earth.RadiusMeters()
	// Just off the pad: airborne (Landed=false), climbing (radial-out
	// velocity), below tower-clear → Prelaunch.
	low := phaseCraftOn(t, w, "Earth",
		orbital.Vec3{X: Re + 500, Y: 0, Z: 0},
		orbital.Vec3{X: 60, Y: 0, Z: 0})
	if !shouldShowLaunchHUD(low) {
		t.Fatal("setup: expected launch HUD up for a low ascending Earth craft")
	}
	if got := deriveFlightPhase(low); got != PhasePrelaunch {
		t.Errorf("just-off-pad craft: got %v, want prelaunch", got)
	}
	// Climbing through 8 km, still suborbital → Ascent.
	climb := phaseCraftOn(t, w, "Earth",
		orbital.Vec3{X: Re + 8_000, Y: 0, Z: 0},
		orbital.Vec3{X: 200, Y: 400, Z: 0})
	if !shouldShowLaunchHUD(climb) {
		t.Fatal("setup: expected launch HUD up for an 8 km ascending Earth craft")
	}
	if got := deriveFlightPhase(climb); got != PhaseAscent {
		t.Errorf("climbing craft at 8 km: got %v, want ascent", got)
	}
}

func TestDeriveFlightPhaseCoastNearCircular(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	earth := sys.FindBody("Earth")
	Re := earth.RadiusMeters()
	mu := earth.GravitationalParameter()
	// Circular LEO at 400 km — e≈0, well above the atmosphere → Coast.
	r := Re + 400_000
	vCirc := math.Sqrt(mu / r)
	c := phaseCraftOn(t, w, "Earth",
		orbital.Vec3{X: r, Y: 0, Z: 0},
		orbital.Vec3{X: 0, Y: vCirc, Z: 0})
	if got := deriveFlightPhase(c); got != PhaseCoast {
		t.Errorf("circular LEO: got %v, want coast", got)
	}
}

func TestDeriveFlightPhaseTransferVsApproach(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	earth := sys.FindBody("Earth")
	Re := earth.RadiusMeters()
	mu := earth.GravitationalParameter()
	// An eccentric transfer ellipse (e≈0.66): perigee 400 km, apogee
	// 35 786 km. At perigee, an outbound (prograde-climbing) velocity reads
	// Transfer; the same orbit caught inbound (radial velocity negative)
	// reads Approach.
	rPeri := Re + 400_000
	rApo := Re + 35_786_000
	a := (rPeri + rApo) / 2
	vPeri := math.Sqrt(mu * (2/rPeri - 1/a))
	// Outbound just past perigee: mostly tangential with a small radial-out
	// component so vUp > 0 (climbing toward apogee).
	out := phaseCraftOn(t, w, "Earth",
		orbital.Vec3{X: rPeri, Y: 0, Z: 0},
		orbital.Vec3{X: 40, Y: vPeri, Z: 0})
	if got := deriveFlightPhase(out); got != PhaseTransfer {
		t.Errorf("outbound transfer ellipse: got %v, want transfer", got)
	}
	// Inbound approaching perigee: radial-in component so vUp < 0.
	in := phaseCraftOn(t, w, "Earth",
		orbital.Vec3{X: rPeri, Y: 0, Z: 0},
		orbital.Vec3{X: -40, Y: vPeri, Z: 0})
	if got := deriveFlightPhase(in); got != PhaseApproach {
		t.Errorf("inbound transfer ellipse: got %v, want approach", got)
	}
}

func TestDeriveFlightPhaseDescentAirless(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	// 5 km above the airless Moon, slow lateral drift → Descent.
	c := phaseCraftOn(t, w, "Moon",
		orbital.Vec3{X: moon.RadiusMeters() + 5_000, Y: 0, Z: 0},
		orbital.Vec3{X: 0, Y: 30, Z: 0})
	if !shouldShowDescentHUD(c) {
		t.Fatal("setup: expected descent HUD up for a 5 km Moon craft")
	}
	if got := deriveFlightPhase(c); got != PhaseDescent {
		t.Errorf("5 km Moon craft: got %v, want descent", got)
	}
}

func TestFlightPhaseString(t *testing.T) {
	cases := map[FlightPhase]string{
		PhaseCoast:     "coast",
		PhasePrelaunch: "prelaunch",
		PhaseAscent:    "ascent",
		PhaseTransfer:  "transfer",
		PhaseApproach:  "approach",
		PhaseDescent:   "descent",
		PhaseLanded:    "landed",
		FlightPhase(99): "coast", // unknown falls back to coast
	}
	for p, want := range cases {
		if got := p.String(); got != want {
			t.Errorf("FlightPhase(%d).String() = %q, want %q", int(p), got, want)
		}
	}
}
