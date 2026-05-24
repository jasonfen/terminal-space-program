// Package sim — v0.11.0 Slice 1 regression suite for the ViewLaunch
// route/release state machine. Tests exercise the per-tick handler
// installed in World.Tick that detects active-slot Landed-false→true
// transitions and apo-crossing auto-release.
package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestLaunchRouteOnLandedTransition — tracer bullet for v0.11.0
// Slice 1. After a Launchpad spawn (which sets the new active vessel
// Landed=true), the next World.Tick fires the per-tick route handler:
// ViewMode switches to ViewLaunch, a session opens
// (LaunchSessionActive=true), and the prior ViewMode is stashed in
// PrevViewMode so auto-release can later restore it.
//
// Proves the path end-to-end: the route handler is wired into
// World.Tick, the Landed-transition detection (wasActiveLanded
// false→true) fires, and the three primary observable session-state
// fields are written.
func TestLaunchRouteOnLandedTransition(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// ViewTilted is the zero-value default; capture it explicitly so
	// the assertion below tracks whatever the world started in.
	priorView := w.ViewMode

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}

	// Per-tick route handler runs inside World.Tick, before the render
	// layer reads ViewMode. One tick should be enough — the Landed
	// shadow defaults to false, the spawned active is Landed=true,
	// and the transition fires on first read.
	w.Tick()

	if w.ViewMode != ViewLaunch {
		t.Errorf("after Landed spawn + Tick: ViewMode = %v, want ViewLaunch", w.ViewMode)
	}
	if !w.LaunchSessionActive {
		t.Error("after Landed spawn + Tick: LaunchSessionActive = false, want true")
	}
	if w.PrevViewMode != priorView {
		t.Errorf("PrevViewMode = %v, want %v (the ViewMode at the moment of route)",
			w.PrevViewMode, priorView)
	}
}

// TestLaunchRouteSeedsSessionState — the route handler also writes
// the rest of the session-scoped state: LaunchT0 stamped to current
// sim-time (so the HUD T+ readout has its anchor), LaunchMaxQ zeroed
// (no peak dynamic pressure carried from any prior session), the
// breadcrumb trail cleared, and LaunchZoom reset to 0 (auto-zoom).
//
// Pre-pollutes the four fields with non-default values to verify the
// route actively resets them — not just that a fresh World happens to
// have zeroes there.
func TestLaunchRouteSeedsSessionState(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// Pre-pollute. A real session would never leave these non-zero
	// outside an active session, but a deterministic test must verify
	// the *reset*, not just default initialisation.
	w.LaunchMaxQ = 42.0
	w.LaunchZoom = 99.0
	w.LaunchTrail = []TrailPoint{
		{LatDeg: 10, LonDeg: 20, AltM: 30},
	}

	if _, err := w.SpawnCraft(SpawnSpec{
		LoadoutID:    spacecraft.LoadoutSaturnVID,
		ParentBodyID: "earth",
		Launchpad:    true,
		Latitude:     28.6,
	}); err != nil {
		t.Fatalf("SpawnCraft (launchpad): %v", err)
	}
	w.Tick()

	if w.LaunchT0.IsZero() {
		t.Error("LaunchT0 was not stamped after route")
	}
	if !w.LaunchT0.Equal(w.Clock.SimTime) {
		t.Errorf("LaunchT0 = %v, want %v (the post-tick sim-time anchor)",
			w.LaunchT0, w.Clock.SimTime)
	}
	if w.LaunchMaxQ != 0 {
		t.Errorf("LaunchMaxQ = %v, want 0 (route must reset)", w.LaunchMaxQ)
	}
	if w.LaunchZoom != 0 {
		t.Errorf("LaunchZoom = %v, want 0 (route must reset to auto)", w.LaunchZoom)
	}
	if len(w.LaunchTrail) != 0 {
		t.Errorf("LaunchTrail len = %d, want 0 (route must clear)", len(w.LaunchTrail))
	}
}

// TestLaunchAutoReleaseAtApoFloor — when the active craft is in
// an orbit whose apoapsis exceeds LaunchMissionFloorM (200 km), the
// per-tick handler's auto-release predicate fires: PrevViewMode is
// restored to ViewMode, LaunchSessionActive flips false, and the
// session-scoped state (T0, MaxQ, trail, zoom) clears so the next
// route doesn't see stale data.
//
// Predicate parity with v0.10.7's LaunchAnchorPhi (`apoAlt >
// LaunchMissionFloorM`) is the architectural commitment from ADR
// 0002 — the ORBIT READY callout and the ViewLaunch release fire
// off the same gate.
func TestLaunchAutoReleaseAtApoFloor(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("earth")
	if earth == nil {
		t.Fatal("setup: earth not found in default system")
	}
	mu := earth.GravitationalParameter()
	primaryR := earth.RadiusMeters()

	// 201 km circular orbit — apoAlt = 201 km, just past the 200 km
	// floor. Same construction pattern as launch_anchor_test.go's
	// `craftAt` helper.
	r := primaryR + 201_000
	v := math.Sqrt(mu / r)
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	c.Primary = *earth
	c.State = physics.StateVector{
		R: orbital.Vec3{X: r},
		V: orbital.Vec3{Y: v},
		M: c.TotalMass(),
	}
	// Replace the default LEO craft with our synthesized 201 km craft.
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0

	// Plant the world mid-session: route already fired, ViewMode is
	// ViewLaunch, PrevViewMode remembers ViewTop.
	w.LaunchSessionActive = true
	w.PrevViewMode = ViewTop
	w.ViewMode = ViewLaunch
	w.LaunchT0 = w.Clock.SimTime
	w.LaunchMaxQ = 1234.0
	w.LaunchTrail = []TrailPoint{{LatDeg: 5, LonDeg: 6, AltM: 100_000}}
	w.LaunchZoom = 0.5

	w.Tick()

	if w.LaunchSessionActive {
		t.Error("after apo>floor + Tick: LaunchSessionActive still true, want false (auto-release)")
	}
	if w.ViewMode != ViewTop {
		t.Errorf("ViewMode = %v, want ViewTop (restored from PrevViewMode)", w.ViewMode)
	}
	if w.LaunchMaxQ != 0 {
		t.Errorf("LaunchMaxQ = %v, want 0 (release must clear)", w.LaunchMaxQ)
	}
	if w.LaunchZoom != 0 {
		t.Errorf("LaunchZoom = %v, want 0 (release must clear)", w.LaunchZoom)
	}
	if len(w.LaunchTrail) != 0 {
		t.Errorf("LaunchTrail len = %d, want 0 (release must clear)", len(w.LaunchTrail))
	}
	if !w.LaunchT0.IsZero() {
		t.Errorf("LaunchT0 = %v, want zero (release must clear)", w.LaunchT0)
	}
}
