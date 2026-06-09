package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// applyOrFatal runs ApplyStartScenario on a fresh world, failing the test on
// error, and returns the world.
func applyOrFatal(t *testing.T, s StartScenario) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if err := w.ApplyStartScenario(s); err != nil {
		t.Fatalf("ApplyStartScenario(%+v): %v", s, err)
	}
	return w
}

func TestApplyStartScenarioLunarOrbit(t *testing.T) {
	w := applyOrFatal(t, StartScenario{BodyID: "moon", AltitudeM: 100_000})
	if len(w.Crafts) != 1 {
		t.Fatalf("want 1 craft, got %d", len(w.Crafts))
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	if c.Primary.ID != "moon" {
		t.Errorf("primary = %q, want moon", c.Primary.ID)
	}
	alt := c.State.R.Norm() - c.Primary.RadiusMeters()
	if math.Abs(alt-100_000) > 1_000 {
		t.Errorf("altitude = %.0f m, want ~100 km", alt)
	}
	if w.Focus.Kind != FocusCraft {
		t.Errorf("focus = %v, want FocusCraft", w.Focus.Kind)
	}
}

func TestApplyStartScenarioLumenBinding(t *testing.T) {
	w := applyOrFatal(t, StartScenario{SystemName: "Lumen", BodyID: "kern", AltitudeM: 400_000, Loadout: "Kern-Stack"})
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	// The craft must be bound to Lumen and the camera must follow it there
	// (ADR 0015), so the flight HUD shows rather than staying suppressed.
	if c.SystemIdx == 0 {
		t.Errorf("craft SystemIdx = 0 (Sol), want Lumen's index")
	}
	if w.SystemIdx != c.SystemIdx {
		t.Errorf("viewed system %d != craft system %d (camera not following)", w.SystemIdx, c.SystemIdx)
	}
	if !w.CraftVisibleHere() {
		t.Error("CraftVisibleHere() = false — flight HUD would be suppressed")
	}
}

func TestApplyStartScenarioLaunchpad(t *testing.T) {
	w := applyOrFatal(t, StartScenario{
		BodyID: "earth", Surface: true,
		LatDeg: DefaultLaunchpadLatitude, LonDeg: DefaultLaunchpadLongitudeEast,
		Loadout: "Saturn-V",
	})
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	if !c.Landed {
		t.Error("launchpad spawn should be Landed")
	}
	if c.Primary.ID != "earth" {
		t.Errorf("primary = %q, want earth", c.Primary.ID)
	}
}

func TestApplyStartScenarioInclination(t *testing.T) {
	w := applyOrFatal(t, StartScenario{BodyID: "earth", AltitudeM: 400_000, InclDeg: 51.6})
	c := w.ActiveCraft()
	// Read the orbit in the body-equatorial frame, where inclination is
	// measured relative to Earth's equator.
	mu := c.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	if el.E > 1e-3 {
		t.Errorf("inclined spawn should stay circular, got e=%.4f", el.E)
	}
	incDeg := el.I * 180 / math.Pi
	if math.Abs(incDeg-51.6) > 0.5 {
		t.Errorf("inclination = %.2f°, want ~51.6°", incDeg)
	}
}

func TestApplyStartScenarioEquatorialUnchanged(t *testing.T) {
	// inclination 0 must reduce to the pre-v0.17 equatorial placement.
	w := applyOrFatal(t, StartScenario{BodyID: "earth", AltitudeM: 400_000})
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	el := orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	if incDeg := el.I * 180 / math.Pi; incDeg > 0.01 {
		t.Errorf("i=0 spawn inclination = %.4f°, want ~0", incDeg)
	}
}

func TestApplyStartScenarioUnknownSystem(t *testing.T) {
	w, _ := NewWorld()
	if err := w.ApplyStartScenario(StartScenario{SystemName: "Nowhere"}); err == nil {
		t.Error("unknown system should error")
	}
}

func TestApplyStartScenarioUnknownBody(t *testing.T) {
	w, _ := NewWorld()
	if err := w.ApplyStartScenario(StartScenario{BodyID: "tatooine"}); err == nil {
		t.Error("unknown body should error")
	}
}

func TestApplyStartScenarioUnknownLoadout(t *testing.T) {
	w, _ := NewWorld()
	if err := w.ApplyStartScenario(StartScenario{BodyID: "earth", Loadout: "Millennium-Falcon"}); err == nil {
		t.Error("unknown loadout should error")
	}
}

func TestLaunchSiteByName(t *testing.T) {
	if s, ok := LaunchSiteByName("ksc"); !ok || s.Key != "KSC" {
		t.Errorf("LaunchSiteByName(ksc) = %+v ok=%v", s, ok)
	}
	if s, ok := LaunchSiteByName("Cape Canaveral (KSC LC-39A)"); !ok || s.Key != "KSC" {
		t.Errorf("lookup by display name failed: %+v ok=%v", s, ok)
	}
	if _, ok := LaunchSiteByName("nope"); ok {
		t.Error("unknown site should return ok=false")
	}
}
