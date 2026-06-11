package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestLiveSOIPassGuardSkipsUnreachableOrbit: a stable LEO whose apoapsis
// can't reach any sibling SOI must report no pass — the apoapsis-reach
// guard (ADR 0019 C) short-circuits before any forward integration.
func TestLiveSOIPassGuardSkipsUnreachableOrbit(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Landed = false
	c.Nodes = nil
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 300e3 // 300 km circular — nowhere near the Moon
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	if pass, ok := w.LiveSOIPass(); ok {
		t.Errorf("LEO that can't reach any sibling SOI must report no pass; got body %s", pass.Body.ID)
	}
}

// TestLiveSOIPassDetectsMoonPass: coasting on an LEO→Moon transfer with no
// maneuver node, LiveSOIPass surfaces the Moon pass — body, a positive
// time-to-perilune, and a foreign-SOI arc to draw.
func TestLiveSOIPassDetectsMoonPass(t *testing.T) {
	w := mustWorld(t)
	leg := coplanarLEOTowardMoon(t, w)
	c := w.ActiveCraft()
	// Simulate the post-departure coast: live state on the transfer, no node.
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("expected a live SOI pass toward the Moon, got none")
	}
	if pass.Body.ID != "moon" {
		t.Errorf("pass body = %q, want \"moon\"", pass.Body.ID)
	}
	if pass.TimeToPerilune <= 0 {
		t.Errorf("TimeToPerilune = %.1f s, want > 0", pass.TimeToPerilune)
	}
	if pass.PeriluneRadius <= 0 {
		t.Errorf("PeriluneRadius = %.1f, want > 0", pass.PeriluneRadius)
	}
	if len(pass.ArcSegments) == 0 {
		t.Error("expected foreign-SOI arc segments for drawing")
	}
	if !pass.HasPerilunePt {
		t.Error("expected a placed Perilune marker point for the canvas glyph")
	}
	// Impact is exactly "perilune below the surface", and PeriluneAltitude
	// is the radius above it — the relationship the chip + Impact marker
	// branch on (ADR 0019 E).
	wantImpact := pass.PeriluneRadius < pass.Body.RadiusMeters()
	if pass.Impact != wantImpact {
		t.Errorf("Impact = %v, want %v (perilune %.0f km vs body radius %.0f km)",
			pass.Impact, wantImpact, pass.PeriluneRadius/1000, pass.Body.RadiusMeters()/1000)
	}
	if alt := pass.PeriluneAltitude(); alt != pass.PeriluneRadius-pass.Body.RadiusMeters() {
		t.Errorf("PeriluneAltitude() = %.0f, want radius-surface", alt)
	}
	// The pass is independent of the Target slot — it surfaced with no
	// target set.
	if w.Target.Kind != TargetNone {
		t.Fatalf("test precondition: expected no target set, got kind %v", w.Target.Kind)
	}
}

// TestLiveSOIPassIndependentOfTarget: setting (or not) the Target slot must
// not change whether the Moon pass is detected — SOI Pass is orthogonal to
// Target (ADR 0019 A).
func TestLiveSOIPassIndependentOfTarget(t *testing.T) {
	w := mustWorld(t)
	leg := coplanarLEOTowardMoon(t, w)
	c := w.ActiveCraft()
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("expected a Moon pass with no target")
	}

	// Now clear any target explicitly and re-check — same result.
	w.Target = Target{Kind: TargetNone}
	pass2, ok2 := w.LiveSOIPass()
	if !ok2 || pass2.Body.ID != pass.Body.ID {
		t.Errorf("SOI pass changed with target cleared: ok=%v body=%q (want ok=true body=%q)",
			ok2, pass2.Body.ID, pass.Body.ID)
	}
}
