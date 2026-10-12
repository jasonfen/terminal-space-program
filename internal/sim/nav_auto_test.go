package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// padWorld: a fresh Saturn V on the Kennedy pad (spawn puts it in NavSurface).
func navPadWorld(t *testing.T) *World {
	t.Helper()
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth",
		Launchpad: true, Latitude: 28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if w.NavMode != NavSurface {
		t.Fatalf("pad spawn NavMode = %v, want SURF", w.NavMode)
	}
	return w
}

// liftTo teleports the active vessel radially to altitude altM (keeping its
// velocity) and runs one World.Tick (free-flight Verlet path on a coasting
// vessel; the launchpad vessel leaves Landed via Landed=false so the
// integrator, not the pad clamp, owns it). Teleporting stands in for the
// climb: the auto-switch reads only the altitude side after each Tick.
func liftTo(t *testing.T, w *World, altM float64) {
	t.Helper()
	c := w.ActiveCraft()
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	c.State.R = rHat.Scale(c.Primary.RadiusMeters() + altM)
	c.Landed = false
	c.OnPad = false
	w.Tick()
}

func TestNavSpeedOnPadReadsSurfaceNotOrbit(t *testing.T) {
	w := navPadWorld(t)
	v, frame := w.NavSpeed()
	if frame != NavSurface {
		t.Errorf("frame = %v, want SURF on the pad", frame)
	}
	if v > 1 {
		t.Errorf("pad speed = %.1f m/s, want ~0 (ground speed, not the 408 m/s spin)", v)
	}
}

func TestNavAutoSurfToOrbitIsAnEdge(t *testing.T) {
	w := navPadWorld(t)
	w.Tick() // seeds the edge state on the pad
	up := w.NavAutoAscentAltitudeFor(w.ActiveCraft().Primary)
	if up < 20e3 || up > 80e3 {
		t.Fatalf("Earth ascent threshold = %.0f m, want a few tens of km", up)
	}
	liftTo(t, w, up*0.5)
	if w.NavMode != NavSurface {
		t.Fatalf("below threshold NavMode = %v, want SURF", w.NavMode)
	}
	liftTo(t, w, up*1.1)
	if w.NavMode != NavOrbit {
		t.Fatalf("above threshold NavMode = %v, want ORBIT", w.NavMode)
	}
	// Level, not edge, would re-fire: player goes back to SURF by hand
	// while still above the line; further ticks must leave it.
	w.NavMode = NavSurface
	liftTo(t, w, up*1.2)
	liftTo(t, w, up*1.3)
	if w.NavMode != NavSurface {
		t.Errorf("manual SURF above the line was overridden: NavMode = %v", w.NavMode)
	}
}

func TestNavAutoManualCycleAfterCrossingIsKept(t *testing.T) {
	w := navPadWorld(t)
	w.Tick()
	up := w.NavAutoAscentAltitudeFor(w.ActiveCraft().Primary)
	liftTo(t, w, up*1.1) // auto-switched to ORBIT
	if w.NavMode != NavOrbit {
		t.Fatalf("setup: NavMode = %v, want ORBIT after the crossing", w.NavMode)
	}
	// `;` from ORBIT with no target skips TGT and lands on SURF.
	if got := w.CycleNavMode(); got != NavSurface {
		t.Fatalf("setup: ; gave %v, want SURF", got)
	}
	for i := 0; i < 5; i++ {
		liftTo(t, w, up*(1.2+0.1*float64(i)))
	}
	if w.NavMode != NavSurface {
		t.Errorf("manual ; was fought: NavMode = %v, want SURF kept", w.NavMode)
	}
}

func TestNavAutoDescentReturnsToSurfOnceWithHysteresis(t *testing.T) {
	w := navPadWorld(t)
	w.Tick()
	up := w.NavAutoAscentAltitudeFor(w.ActiveCraft().Primary)
	liftTo(t, w, up*1.5)
	if w.NavMode != NavOrbit {
		t.Fatalf("setup: NavMode = %v, want ORBIT", w.NavMode)
	}
	// Hovering just under the ascent line (inside the hysteresis band)
	// must not flip back.
	liftTo(t, w, up*0.95)
	if w.NavMode != NavOrbit {
		t.Errorf("flipped inside the hysteresis band: NavMode = %v", w.NavMode)
	}
	liftTo(t, w, up*0.5)
	if w.NavMode != NavSurface {
		t.Errorf("descent below the band: NavMode = %v, want SURF", w.NavMode)
	}
}

// Review #629 M1 (Jason, 2026-10-12): the frame never switches itself to TGT.
// Approaching a rendezvous target in ORBIT stays ORBIT at every range, and a
// hand-chosen TGT stays TGT when the vessel moves away. Only `;` reaches TGT.
func TestNavAutoNeverSwitchesToTarget(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 100e3}, orbital.Vec3{})
	w.NavMode = NavOrbit
	w.Tick() // seed outside
	for _, r := range []float64{60e3, 35e3, 20e3, 1e3, 100} {
		setProximityRange(t, w, r)
		w.Tick()
		w.Tick()
		if w.NavMode != NavOrbit {
			t.Fatalf("range %g m: NavMode = %v, want ORBIT (no automatic TGT)", r, w.NavMode)
		}
	}
	// By hand it still gets there, and stays there on the way out.
	w.CycleNavMode()
	if w.NavMode != NavTarget {
		t.Fatalf("manual ; from ORBIT with a target: NavMode = %v, want TGT", w.NavMode)
	}
	for _, r := range []float64{20e3, 38e3, 50e3, 100e3} {
		setProximityRange(t, w, r)
		w.Tick()
		if w.NavMode != NavTarget {
			t.Fatalf("range %g m: manual TGT lost: NavMode = %v", r, w.NavMode)
		}
	}
}

func TestNavSpeedTargetFrameIsRelativeToTarget(t *testing.T) {
	w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{X: -2})
	w.NavMode = NavTarget
	v, frame := w.NavSpeed()
	if frame != NavTarget {
		t.Fatalf("frame = %v, want TGT", frame)
	}
	if math.Abs(v-2) > 1e-6 {
		t.Errorf("target speed = %g, want 2", v)
	}
}

func TestNavSpeedTargetFallsBackToOrbitWithoutTarget(t *testing.T) {
	w := mustWorld(t)
	w.NavMode = NavTarget // stale
	_, frame := w.NavSpeed()
	if frame != NavOrbit {
		t.Errorf("frame = %v, want ORBIT fallback (same as ResolveAttitudeIntent)", frame)
	}
}

func TestNavAutoThresholdsPerBody(t *testing.T) {
	// Atmosphere: 6 scale heights, clamped to the cutoff.
	if got := NavAutoAscentAltitude(6371e3, 8500, 150e3, true); math.Abs(got-51e3) > 1 {
		t.Errorf("Earth-like = %g, want 51000", got)
	}
	if got := NavAutoAscentAltitude(6371e3, 50e3, 100e3, true); got != 100e3 {
		t.Errorf("clamped to cutoff = %g, want 100000", got)
	}
	// Airless: 10 km, but never more than a tenth of the radius.
	if got := NavAutoAscentAltitude(1737e3, 0, 0, false); got != 10e3 {
		t.Errorf("Moon-like = %g, want 10000", got)
	}
	if got := NavAutoAscentAltitude(11e3, 0, 0, false); math.Abs(got-1.1e3) > 1e-6 {
		t.Errorf("Phobos-like = %g, want 1100", got)
	}
}
