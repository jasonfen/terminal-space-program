// Package sim: OrbitFloorForCraft (ADR 0051 slice 1 groundwork), the
// gate helper that reads a vessel's current primary's Orbit Floor (ADR
// 0044), for launch_anchor.go's ViewTilted gate to switch to (this
// slice) and later the ORBIT READY chip gate (slice 2, not touched
// here). Wraps the existing floorFor: this file adds no new floor
// arithmetic, only the craft-shaped entry point.

package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// craftAtPrimary builds a minimal Spacecraft parked at b, just enough
// for OrbitFloorForCraft to read c.Primary.
func craftAtPrimary(b bodies.CelestialBody) *spacecraft.Spacecraft {
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = b
	return c
}

func TestOrbitFloorForCraftEarth(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	_, earth := findBodyIn(t, systems, "Sol", "earth")
	c := craftAtPrimary(earth)
	if got, want := OrbitFloorForCraft(c), 175_000.0; got != want {
		t.Errorf("Earth Orbit Floor via craft = %.0f m, want %.0f m", got, want)
	}
}

func TestOrbitFloorForCraftLumenAtmosphericWorld(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	_, ember := findBodyIn(t, systems, "Lumen", "ember")
	c := craftAtPrimary(ember)
	if got, want := OrbitFloorForCraft(c), 115_000.0; got != want {
		t.Errorf("Ember Orbit Floor via craft = %.0f m, want %.0f m", got, want)
	}
}

func TestOrbitFloorForCraftAirlessWorld(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	_, moon := findBodyIn(t, systems, "Sol", "moon")
	if moon.Atmosphere != nil {
		t.Fatalf("setup: Moon should have no atmosphere; got %+v", moon.Atmosphere)
	}
	c := craftAtPrimary(moon)
	if got, want := OrbitFloorForCraft(c), OrbitFloorMarginM; got != want {
		t.Errorf("Moon Orbit Floor via craft = %.0f m, want %.0f m (margin alone)", got, want)
	}
}

func TestOrbitFloorForCraftNilCraft(t *testing.T) {
	if got := OrbitFloorForCraft(nil); got != 0 {
		t.Errorf("nil craft: got %.0f m, want 0", got)
	}
}
