package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// earthLike / moonLike are body fixtures for the surface-launch gate tests —
// real Earth / Moon mass + mean radius, so g = μ/R² is realistic (~9.8 / ~1.6).
func earthLike() bodies.CelestialBody {
	return bodies.CelestialBody{ID: "earth", EnglishName: "Earth",
		Mass: bodies.Mass{Value: 5.9722, Exponent: 24}, MeanRadius: 6371}
}
func moonLike() bodies.CelestialBody {
	return bodies.CelestialBody{ID: "moon", EnglishName: "Moon",
		Mass: bodies.Mass{Value: 7.342, Exponent: 22}, MeanRadius: 1737.4}
}

func wetMass(l spacecraft.Loadout) float64 {
	return spacecraft.SumDryMass(l.Stages) + spacecraft.SumFuelMass(l.Stages)
}

// loadoutIndex returns a loadout's selectable cursor index in grouped order.
func loadoutIndex(t *testing.T, id string) int {
	t.Helper()
	for i, x := range orderedLoadoutIDs() {
		if x == id {
			return i
		}
	}
	t.Fatalf("loadout %q not found in grouped order", id)
	return -1
}

// TestCraftLiftsOff — the physics surface-launch gate (ADR 0031 / S9): a craft
// can be surface-spawned iff its bottom-stage TWR ≥ 1 on the body. A launcher
// lifts off Earth; an NTR-tug carrier and an engineless satellite don't; a
// Lander lifts off the Moon but not Earth (body-aware).
func TestCraftLiftsOff(t *testing.T) {
	earth, moon := earthLike(), moonLike()

	sat := spacecraft.LookupLoadout("Saturn-V")
	if !craftLiftsOff(sat.Thrust(), wetMass(sat), &earth) {
		t.Error("Saturn V should lift off Earth (TWR > 1)")
	}

	carrier := spacecraft.LookupLoadout("Comsat-Carrier-3")
	if craftLiftsOff(carrier.Thrust(), wetMass(carrier), &earth) {
		t.Error("Comsat Carrier (NTR tug + payloads) must NOT lift off Earth")
	}

	sat2 := spacecraft.LookupLoadout("Relay-Comsat") // engineless
	if craftLiftsOff(sat2.Thrust(), wetMass(sat2), &earth) {
		t.Error("engineless Relay Comsat must NOT lift off (thrust 0)")
	}

	lander := spacecraft.LookupLoadout("Lander")
	if !craftLiftsOff(lander.Thrust(), wetMass(lander), &moon) {
		t.Error("Lander should lift off the Moon (TWR > 1)")
	}
	if craftLiftsOff(lander.Thrust(), wetMass(lander), &earth) {
		t.Error("Lander must NOT lift off Earth (TWR < 1)")
	}

	// Degenerate guards.
	if craftLiftsOff(1e9, 1000, nil) {
		t.Error("nil body must read as can't-lift-off")
	}
}

// TestLaunchpadGateSkipsOrbitOnlyCraft — POSITION cycling never lands on
// launchpad for an orbit-only craft, but does for a launcher (ADR 0031 / S9).
func TestLaunchpadGateSkipsOrbitOnlyCraft(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil)

	// Orbit-only: the Comsat Carrier can't lift off Earth.
	s.loadoutIdx = loadoutIndex(t, "Comsat-Carrier-3")
	if s.launchpadAllowed() {
		t.Fatal("Comsat Carrier should not allow launchpad on Earth")
	}
	s.fieldIdx = 1 // POSITION
	for i := 0; i < 12; i++ {
		s.HandleKey("right")
		if s.SelectedLaunchpad() {
			t.Fatalf("POSITION cycle landed on launchpad for an orbit-only craft (step %d)", i)
		}
	}

	// Launcher: Saturn V reaches launchpad within a couple of cycles.
	s.loadoutIdx = loadoutIndex(t, "Saturn-V")
	if !s.launchpadAllowed() {
		t.Fatal("Saturn V should allow launchpad on Earth")
	}
	reached := false
	for i := 0; i < 4; i++ {
		s.HandleKey("right")
		if s.SelectedLaunchpad() {
			reached = true
			break
		}
	}
	if !reached {
		t.Error("POSITION cycle never reached launchpad for a launch-capable craft")
	}
}

// TestLaunchpadNeverStickyOnOrbitOnlyCraft — once launchpad is selected on a
// launcher, walking the CRAFT TYPE list must never leave launchpad selected on
// a craft that can't lift off the parent: switching craft snaps it back to
// orbit (ADR 0031 / S9). Tests the invariant across the whole list.
func TestLaunchpadNeverStickyOnOrbitOnlyCraft(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil)
	s.loadoutIdx = loadoutIndex(t, "Saturn-V")
	s.fieldIdx = 1
	for i := 0; i < 4 && !s.SelectedLaunchpad(); i++ {
		s.HandleKey("right")
	}
	if !s.SelectedLaunchpad() {
		t.Fatal("setup: could not select launchpad on Saturn V")
	}
	s.fieldIdx = 0 // CRAFT TYPE
	for i := 0; i < s.loadoutChoiceCount()+2; i++ {
		s.HandleKey("right")
		if s.SelectedLaunchpad() && !s.launchpadAllowed() {
			t.Fatalf("launchpad stuck on a craft that can't lift off: idx=%d id=%q",
				s.loadoutIdx, s.SelectedLoadoutID())
		}
	}
}

// TestRenderShowsCrewTags — the rendered picker tags crewed and uncrewed craft
// (ADR 0031 / S9 render smoke).
func TestRenderShowsCrewTags(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil)
	out := s.Render(120)
	if !strings.Contains(out, "crewed") {
		t.Error("render missing a 'crewed' tag (e.g. Apollo Stack)")
	}
	if !strings.Contains(out, "uncrewed") {
		t.Error("render missing an 'uncrewed' tag")
	}
}
