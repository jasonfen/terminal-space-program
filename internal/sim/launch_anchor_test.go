package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLaunchAnchorActivates — Saturn V parked on Earth's launchpad.
// The predicate should fire (a landed craft's body-co-rotation gives
// a bound "orbit" with apoapsis at the surface, well below the 200 km
// floor). Phi should match atan2(-R.X, R.Y) and track local-vertical
// as the body rotates under the pad (verified by advancing simTime
// 6 h and re-integrating the landed bypass).
func TestLaunchAnchorActivates(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6083,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}

	// Landed craft → screens.activeCraftElements returns ok=false,
	// so the caller passes the zero-Elements / false branch.
	phi, active := LaunchAnchorPhi(c, orbital.Elements{}, false)
	if !active {
		t.Fatalf("anchor should be active on the pad (apoAlt ≈ 0 ≤ %v km)", LaunchMissionFloorM/1000)
	}
	wantPhi := math.Atan2(-c.State.R.X, c.State.R.Y)
	if math.Abs(phi-wantPhi) > 1e-9 {
		t.Errorf("phi at spawn: got %v rad, want %v rad", phi, wantPhi)
	}

	// Advance sim time 6 h; the landed integrator regenerates R from
	// (lat, lon, simTime), so R rotates with the body. The anchor
	// phi should re-track local-vertical at the new R.
	startR := c.State.R
	w.Clock.SimTime = w.Clock.SimTime.Add(6 * time.Hour)
	integrateLanded(w, c, time.Hour)
	if c.State.R == startR {
		t.Fatal("integrateLanded should rotate R after 6 h of sim time")
	}
	phi2, active2 := LaunchAnchorPhi(c, orbital.Elements{}, false)
	if !active2 {
		t.Fatal("anchor should still be active after body rotation")
	}
	wantPhi2 := math.Atan2(-c.State.R.X, c.State.R.Y)
	if math.Abs(phi2-wantPhi2) > 1e-9 {
		t.Errorf("phi after 6 h: got %v rad, want %v rad", phi2, wantPhi2)
	}
	if math.Abs(phi2-phi) < 1e-6 {
		t.Errorf("phi should change as body rotates: pre=%v post=%v", phi, phi2)
	}
}

// TestLaunchAnchorReleasesAtOrbitReady — synthesize a craft with a
// circular orbit straddling the 200 km apoapsis floor. Below the
// floor the anchor stays active; above it the predicate releases
// and Phi returns (0, false), matching the exact moment the ORBIT
// READY callout fires.
func TestLaunchAnchorReleasesAtOrbitReady(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	if earth == nil {
		t.Fatal("setup: earth not found")
	}
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	craftAt := func(altM float64) *spacecraft.Spacecraft {
		r := primaryR + altM
		v := math.Sqrt(mu / r)
		c := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
		c.Primary = *earth
		c.State = physics.StateVector{
			R: orbital.Vec3{X: r},
			V: orbital.Vec3{Y: v},
			M: c.TotalMass(),
		}
		return c
	}

	// 199 km circular orbit → apoAlt ≈ 199 km ≤ 200 km → active.
	cBelow := craftAt(199_000)
	elBelow := orbital.ElementsFromState(cBelow.State.R, cBelow.State.V, mu)
	_, active := LaunchAnchorPhi(cBelow, elBelow, true)
	if !active {
		t.Errorf("199 km orbit: anchor should be active (apoAlt = %.1f km)",
			(elBelow.Apoapsis()-primaryR)/1000)
	}

	// 201 km circular orbit → apoAlt ≈ 201 km > 200 km → released.
	cAbove := craftAt(201_000)
	elAbove := orbital.ElementsFromState(cAbove.State.R, cAbove.State.V, mu)
	phi, active := LaunchAnchorPhi(cAbove, elAbove, true)
	if active {
		t.Errorf("201 km orbit: anchor should release (apoAlt = %.1f km)",
			(elAbove.Apoapsis()-primaryR)/1000)
	}
	if phi != 0 {
		t.Errorf("released anchor must return phi=0, got %v", phi)
	}

	_ = w // keep World referenced; useful if the function ever grows world-state deps
}

// TestLaunchAnchorMoon — vacuum body proves the predicate is
// atmosphere-agnostic. Moon launchpad spawn should fire the anchor
// just like Earth's, even though Moon has no atmosphere field.
func TestLaunchAnchorMoon(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "moon",
		Launchpad:    true,
		Latitude:     0,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("setup: launchpad spawn should set Landed=true")
	}
	if c.Primary.Atmosphere != nil {
		t.Fatalf("setup: Moon should have no atmosphere; got %+v", c.Primary.Atmosphere)
	}
	_, active := LaunchAnchorPhi(c, orbital.Elements{}, false)
	if !active {
		t.Errorf("anchor should fire on vacuum-body pad (predicate is atmosphere-agnostic)")
	}
}
