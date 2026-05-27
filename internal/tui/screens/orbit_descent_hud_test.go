// Package screens — v0.11.4-followup DESCENT HUD tests.
//
// The DESCENT block is the airless-body counterpart to LAUNCH: it
// surfaces v_vert / v_horiz / fpa / twr so a player flying a Moon
// approach can see the lateral component of their velocity before the
// surface-arrival predicate (|V| against CrashVCritMps = 10 m/s) flips
// them to Crashed. These tests pin the predicate against the
// atmosphere / altitude / impactor-periapsis conditions and assert the
// rendered block contains the load-bearing rows.

package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// descentHUDTheme is a flat no-op theme — strings round-trip without
// ANSI sequences so the assertions can rely on substring matching.
func descentHUDTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// placeLanderOnMoon parks the active slate's Lander at the requested
// altitude / velocity vector around the Moon. Subsequent tests mutate
// the velocity vector to dial in vertical-vs-horizontal splits.
func placeLanderOnMoon(t *testing.T, w *sim.World, altM, vx, vy, vz float64) *spacecraft.Spacecraft {
	t.Helper()
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("Moon not in default system")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	c.State = physics.StateVector{
		R: orbital.Vec3{X: moon.RadiusMeters() + altM, Y: 0, Z: 0},
		V: orbital.Vec3{X: vx, Y: vy, Z: vz},
		M: c.TotalMass(),
	}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	return c
}

// TestShouldShowDescentHUDAirlessLowAltitude — a Lander at 5 km
// altitude above the Moon (airless, well below the 50 km threshold)
// is exactly the regime the predicate exists for.
func TestShouldShowDescentHUDAirlessLowAltitude(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := placeLanderOnMoon(t, w, 5_000, 0, 100, 0)
	if !shouldShowDescentHUD(c) {
		t.Errorf("shouldShowDescentHUD = false at 5 km Moon altitude; want true")
	}
}

// TestShouldShowDescentHUDImpactorAtHighAltitude — a Lander on a
// bound orbit around the Moon with periapsis below the surface (an
// impactor trajectory) still benefits from the descent readout even
// while transiently above the 50 km altitude threshold. Pins the
// peri-sub-surface arm of the predicate.
func TestShouldShowDescentHUDImpactorAtHighAltitude(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("Moon not in default system")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	mu := moon.GravitationalParameter()
	primaryR := moon.RadiusMeters()
	// Bound impactor: apo at +150 km altitude, peri at -50 km. At
	// apoapsis the current altitude is well above the 50 km
	// straight-altitude threshold, so the test isolates the
	// impactor-periapsis predicate arm.
	rApo := primaryR + 150_000
	rPeri := primaryR - 50_000
	a := (rPeri + rApo) / 2
	vAtApo := math.Sqrt(mu * (2/rApo - 1/a))
	c.State = physics.StateVector{
		R: orbital.Vec3{X: rApo, Y: 0, Z: 0},
		V: orbital.Vec3{X: 0, Y: vAtApo, Z: 0},
		M: c.TotalMass(),
	}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	if c.Altitude() < descentHUDAltitudeM {
		t.Fatalf("setup: expected altitude > %.0f m, got %.0f", descentHUDAltitudeM, c.Altitude())
	}
	if !shouldShowDescentHUD(c) {
		t.Errorf("shouldShowDescentHUD = false for impactor orbit at high altitude; want true")
	}
}

// TestShouldShowDescentHUDHighStableOrbit — a Lander parked in a
// stable 200 km circular Moon orbit isn't approaching the surface;
// the descent block should stay quiet.
func TestShouldShowDescentHUDHighStableOrbit(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("Moon not in default system")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	mu := moon.GravitationalParameter()
	r := moon.RadiusMeters() + 200_000
	vCirc := math.Sqrt(mu / r)
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r, Y: 0, Z: 0},
		V: orbital.Vec3{X: 0, Y: vCirc, Z: 0},
		M: c.TotalMass(),
	}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	if shouldShowDescentHUD(c) {
		t.Errorf("shouldShowDescentHUD = true at 200 km stable circular orbit; want false")
	}
}

// TestShouldShowDescentHUDAtmospheric — atmospheric bodies route
// through the LAUNCH HUD instead. DESCENT must stay quiet on Earth
// approaches so the two blocks don't double-up.
func TestShouldShowDescentHUDAtmospheric(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected default active craft on Earth")
	}
	if c.Primary.Atmosphere == nil {
		t.Fatalf("expected default Earth primary with atmosphere; got nil atmosphere")
	}
	// Push the craft to 5 km altitude above Earth — atmospheric, so
	// LAUNCH owns this regime even at low altitude.
	c.State.R.X = c.Primary.RadiusMeters() + 5_000
	c.State.R.Y = 0
	c.State.R.Z = 0
	c.State.V = orbital.Vec3{X: 0, Y: 100, Z: 0}
	c.Landed = false
	if shouldShowDescentHUD(c) {
		t.Errorf("shouldShowDescentHUD = true for atmospheric primary; want false (LAUNCH owns this)")
	}
}

// TestDescentHUDRendersVHorizAlertOnImpactorApproach — drives the
// HUD into the playtest-report scenario: standalone Lander at 5 km
// Moon altitude with residual orbital lateral velocity (1.5 km/s,
// vastly above CrashVCritMps = 10). The rendered output must
// surface the DESCENT header, the v_horiz row, and the CRASH-on-
// contact alert so the failure mode is legible before touchdown.
func TestDescentHUDRendersVHorizAlertOnImpactorApproach(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := placeLanderOnMoon(t, w, 5_000, 0, 1500, 0)
	// Sanity: the predicate must fire for this state, otherwise the
	// render assertion below would be vacuously satisfied.
	if !shouldShowDescentHUD(c) {
		t.Fatalf("predicate gate didn't fire for 5 km / 1.5 km/s lateral; can't drive render path")
	}
	view := NewOrbitView(descentHUDTheme())
	out := view.Render(w, 0, 200, 60)
	if !strings.Contains(out, "DESCENT") {
		t.Errorf("expected DESCENT section header in render; got:\n%s", out)
	}
	if !strings.Contains(out, "v_vert:") {
		t.Errorf("expected v_vert row")
	}
	if !strings.Contains(out, "v_horiz:") {
		t.Errorf("expected v_horiz row")
	}
	if !strings.Contains(out, "fpa:") {
		t.Errorf("expected fpa row")
	}
	if !strings.Contains(out, "CRASH on contact") {
		t.Errorf("expected v_horiz CRASH-on-contact alert for 1.5 km/s lateral; got:\n%s", out)
	}
}

// TestDescentHUDQuietAtHighStableOrbit — verifies the rendered HUD
// does NOT emit a DESCENT section when the craft is in a stable
// high orbit (predicate returns false). Pins the symmetric case to
// the impactor-approach test above so a future predicate regression
// in either direction surfaces here.
func TestDescentHUDQuietAtHighStableOrbit(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("Moon not in default system")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	c.Primary = *moon
	mu := moon.GravitationalParameter()
	r := moon.RadiusMeters() + 200_000
	vCirc := math.Sqrt(mu / r)
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r, Y: 0, Z: 0},
		V: orbital.Vec3{X: 0, Y: vCirc, Z: 0},
		M: c.TotalMass(),
	}
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	view := NewOrbitView(descentHUDTheme())
	out := view.Render(w, 0, 200, 60)
	if strings.Contains(out, "DESCENT") {
		t.Errorf("DESCENT section should not render for stable 200 km circular Moon orbit; got:\n%s", out)
	}
}
