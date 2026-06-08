package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// retrograde_lunar_burn_test.go is the disambiguation harness for issue
// #63: "retrograde burn at low lunar orbit appears to reverse direction."
// The physics says a 1× retrograde burn lasting a few seconds delivers
// only tens-to-hundreds of m/s — two orders of magnitude short of the
// ~1.6 km/s needed to flip the orbit. These tests fire the REAL burn
// path (ActiveBurn + World.Tick at 1× warp) on a craft deep inside the
// Moon's SOI and assert:
//
//   - a modest retrograde Δv does NOT flip the specific-angular-momentum
//     sign (no reversal) and leaves apo/peri sane — the "this can't be a
//     real bug" guard (issue action 2), which also exercises the live-burn
//     velocity frame for a secondary-SOI craft (issue action 3: the burn
//     must read Moon-relative −v̂, not a frame-mixed velocity);
//   - reversal only appears once the applied Δv approaches the circular
//     speed (~1.66 km/s at 45 km), confirming the order-of-magnitude
//     argument in the report.

// lunarOrbitBurner binds the active craft to the Moon on a prograde
// circular orbit at altKm, gives it a single high-thrust well-fuelled
// stage, and plants a running retrograde finite burn owing dvRemaining.
// The orbit is built in the body-equatorial frame so the
// specific-angular-momentum vector points along +Z_eq (prograde); the
// caller reads it back via equatorialAngularMomentumZ. Returns the craft
// and the equatorial frame used to build the orbit.
func lunarOrbitBurner(t *testing.T, w *World, altKm, dvRemaining float64) (*spacecraft.Spacecraft, orbital.BodyFrame) {
	t.Helper()
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Skip("Moon not in default system")
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	c.Primary = *moon

	mu := moon.GravitationalParameter()
	r := moon.RadiusMeters() + altKm*1000
	vCirc := math.Sqrt(mu / r)

	// Build the circular orbit in the equatorial basis: position along
	// +X_eq, velocity along +Y_eq → h = r×v along +Z_eq (prograde), then
	// rotate into the world inertial basis the state vector lives in.
	frame := orbital.ReferenceFrameForPrimary(*moon)
	rEq := orbital.Vec3{X: r, Y: 0, Z: 0}
	vEq := orbital.Vec3{X: 0, Y: vCirc, Z: 0}

	// Ample thrust + fuel so even the ~1.6 km/s reversal case completes
	// in a bounded number of ticks; the burn path, not the stage, is the
	// subject under test.
	c.Stages = []spacecraft.Stage{
		{Name: "S-IVB", DryMass: 11000, FuelMass: 90000, FuelCapacity: 90000, Thrust: 1_023_000, Isp: 421},
	}
	c.SyncFields()
	c.State = physics.StateVector{
		R: frame.ToWorld(rEq),
		V: frame.ToWorld(vEq),
		M: c.TotalMass(),
	}
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnRetrograde,
		DVRemaining: dvRemaining,
		EndTime:     w.Clock.SimTime.Add(30 * time.Minute),
		PrimaryID:   moon.ID,
		Throttle:    1,
	}
	return c, frame
}

// equatorialAngularMomentumZ returns the Z component of the specific
// angular momentum h = r×v expressed in the equatorial frame. Its sign
// is the orbit-direction invariant: > 0 prograde, < 0 retrograde. The
// burn cannot flip this without driving the tangential speed through
// zero (Δv ≈ v_circ).
func equatorialAngularMomentumZ(c *spacecraft.Spacecraft, frame orbital.BodyFrame) float64 {
	hWorld := c.State.R.Cross(c.State.V)
	return frame.FromWorld(hWorld).Z
}

// TestModestRetrogradeBurnDoesNotReverseLunarOrbit: a 300 m/s retrograde
// burn (well above the few-seconds-at-1× figure in the report, still far
// below v_circ) from a 45 km prograde lunar orbit lowers periapsis but
// must NOT flip the angular-momentum sign, drive an apsis negative, or
// tip the orbit past 90° inclination. This is the core "render artifact,
// not a real reversal" guard.
func TestModestRetrogradeBurnDoesNotReverseLunarOrbit(t *testing.T) {
	w := mustWorld(t)
	c, frame := lunarOrbitBurner(t, w, 45, 300)
	mu := c.Primary.GravitationalParameter()

	hBefore := equatorialAngularMomentumZ(c, frame)
	if hBefore <= 0 {
		t.Fatalf("setup: orbit should start prograde (h_z > 0), got %.3e", hBefore)
	}

	if _, ok := tickUntil(w, 20000, func() bool { return c.ActiveBurn == nil }); !ok {
		t.Fatalf("burn never completed within tick budget (DVRemaining=%.1f)", c.ActiveBurn.DVRemaining)
	}

	hAfter := equatorialAngularMomentumZ(c, frame)
	if hAfter <= 0 {
		t.Errorf("angular-momentum sign flipped after a 300 m/s retrograde burn "+
			"(h_z %.3e → %.3e): orbit reversed, which is physically impossible "+
			"at this Δv — frame or direction bug", hBefore, hAfter)
	}

	el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	if el.I >= math.Pi/2 {
		t.Errorf("inclination tipped retrograde: %.2f° (want < 90°)", el.I*180/math.Pi)
	}
	if el.E >= 1 || math.IsNaN(el.A) || el.A <= 0 {
		t.Fatalf("orbit became unbound/degenerate: a=%.0f e=%.3f", el.A, el.E)
	}
	moonR := c.Primary.RadiusMeters()
	apoAlt := (el.Apoapsis() - moonR) / 1000
	periAlt := (el.Periapsis() - moonR) / 1000
	// A retrograde burn at this point pins it as the high apsis, so
	// apoapsis stays ≈ 45 km and periapsis drops. Both must stay finite
	// and apoapsis must not run away.
	if apoAlt < 40 || apoAlt > 60 {
		t.Errorf("apoapsis swung out of band: %.1f km alt (want ≈45)", apoAlt)
	}
	if periAlt >= apoAlt {
		t.Errorf("periapsis %.1f km not below apoapsis %.1f km after retrograde burn", periAlt, apoAlt)
	}
	t.Logf("after 300 m/s retrograde: apo=%.1f km peri=%.1f km i=%.2f° h_z=%.3e",
		apoAlt, periAlt, el.I*180/math.Pi, hAfter)
}

// TestLunarOrbitReversalNeedsNearCircularDV: reversal IS reachable, but
// only once the applied retrograde Δv reaches the circular speed
// (~1.66 km/s at 45 km) — two orders of magnitude beyond the tens-to-
// hundreds of m/s a few-second 1× burn delivers. (A *sustained* burn
// can't even get there: it drives periapsis into the surface and the
// craft impacts before the orbit reverses, so the threshold is shown
// impulsively.) Each impulse is applied along the production retrograde
// direction — spacecraft.DirectionUnit(BurnRetrograde, r, v) — so this
// also pins that vector to the Moon-relative −v̂ the live burn must use.
func TestLunarOrbitReversalNeedsNearCircularDV(t *testing.T) {
	w := mustWorld(t)
	c, frame := lunarOrbitBurner(t, w, 45, 0) // dv unused; we apply impulses directly
	mu := c.Primary.GravitationalParameter()

	r0, v0 := c.State.R, c.State.V
	vCirc := math.Sqrt(mu / r0.Norm())
	if vCirc < 1500 || vCirc > 1800 {
		t.Fatalf("setup: v_circ at 45 km = %.0f m/s, expected ≈1660 (issue's ~1.6 km/s)", vCirc)
	}

	// h_z(Δv) for a single retrograde impulse of magnitude dv applied to
	// the circular state, read in the equatorial frame. The retrograde
	// unit vector comes from production code, not hand-rolled here.
	hZ := func(dv float64) float64 {
		dir := spacecraft.DirectionUnit(spacecraft.BurnRetrograde, r0, v0)
		v := v0.Add(dir.Scale(dv))
		return frame.FromWorld(r0.Cross(v)).Z
	}

	cases := []struct {
		dv       float64
		wantSign string // "+" prograde (no reversal), "-" retrograde (reversed)
	}{
		{300, "+"},        // the report's worst-case 1× burn — nowhere near reversal
		{vCirc - 50, "+"}, // still prograde right up to the circular speed
		{vCirc + 50, "-"}, // just past v_circ the orbit finally reverses
		{2000, "-"},       // comfortably reversed
	}
	for _, tc := range cases {
		got := hZ(tc.dv)
		sign := "+"
		if got < 0 {
			sign = "-"
		}
		if sign != tc.wantSign {
			t.Errorf("Δv=%.0f m/s (v_circ=%.0f): h_z=%.3e sign %q, want %q",
				tc.dv, vCirc, got, sign, tc.wantSign)
		}
	}
	t.Logf("reversal threshold = v_circ = %.0f m/s; a 300 m/s 1× burn leaves h_z=%.3e (prograde)",
		vCirc, hZ(300))
}
