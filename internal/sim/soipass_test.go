package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

	// The drawn arc is the *full* transit (entry → perilune → exit), not
	// truncated at perilune: the closest-approach sample sits mid-arc with
	// exit samples after it that climb back away from the body.
	moonAtTCA := w.BodyPositionAt(pass.Body, w.Clock.SimTime.Add(time.Duration(pass.TimeToPerilune*float64(time.Second))))
	var pts []orbital.Vec3
	for _, s := range pass.ArcSegments {
		pts = append(pts, s.Points...)
	}
	periIdx, periD := 0, math.Inf(1)
	for i, p := range pts {
		if d := p.Sub(moonAtTCA).Norm(); d < periD {
			periD = d
			periIdx = i
		}
	}
	if periIdx >= len(pts)-1 {
		t.Errorf("perilune is the last arc sample (idx %d of %d) — arc truncated at perilune, exit leg not drawn", periIdx, len(pts))
	}
	if pts[len(pts)-1].Sub(moonAtTCA).Norm() <= periD {
		t.Error("arc does not climb away from the body after perilune — exit leg of the transit is missing")
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

// TestCounterfactualSOIPassCapsAtFirstNode: the no-burn counterfactual (ADR
// 0019 D) is capped at the first planted node — suppressed when a node
// fires before the encounter could be reached, intact when the node fires
// after it.
func TestCounterfactualSOIPassCapsAtFirstNode(t *testing.T) {
	w := mustWorld(t)
	leg := coplanarLEOTowardMoon(t, w)
	c := w.ActiveCraft()
	w.Clock.SimTime = leg.StartClock
	c.State = leg.State
	c.Primary = leg.Primary
	c.Nodes = nil

	live, ok := w.LiveSOIPass()
	if !ok || live.Body.ID != "moon" {
		t.Fatalf("precondition: live pass should reach the Moon; ok=%v", ok)
	}

	// A node an hour out — long before the multi-day encounter — caps the
	// counterfactual before it can reach the SOI.
	c.Nodes = []spacecraft.ManeuverNode{{TriggerTime: w.Clock.SimTime.Add(time.Hour)}}
	if pass, ok := w.CounterfactualSOIPass(); ok {
		t.Errorf("counterfactual should be suppressed by a node firing before the encounter; got body %q", pass.Body.ID)
	}

	// A node past the encounter leaves the counterfactual intact.
	c.Nodes = []spacecraft.ManeuverNode{{TriggerTime: w.Clock.SimTime.Add(time.Duration(live.TimeToPerilune*2) * time.Second)}}
	cf, ok := w.CounterfactualSOIPass()
	if !ok || cf.Body.ID != "moon" {
		t.Errorf("counterfactual should reach the Moon when the node fires after the encounter; ok=%v", ok)
	}

	// With no node, the counterfactual is identical to the live pass.
	c.Nodes = nil
	cf2, ok := w.CounterfactualSOIPass()
	if !ok || cf2.Body.ID != live.Body.ID {
		t.Errorf("counterfactual with no node = (ok=%v, body=%q), want the live pass body %q", ok, cf2.Body.ID, live.Body.ID)
	}
}

// TestPlannedSOIPassFromLegs: with a transfer planted, the planned (node-
// modified) path's SOI pass is scanned from the post-burn legs (ADR 0019 D
// bright path). With no node planted there is no planned pass.
func TestPlannedSOIPassFromLegs(t *testing.T) {
	w := mustWorld(t)
	coplanarLEOTowardMoon(t, w) // plants an LEO→Moon transfer

	pass, ok := w.PlannedSOIPass()
	if !ok {
		t.Fatal("expected a planned SOI pass from the transfer legs")
	}
	if pass.Body.ID != "moon" {
		t.Errorf("planned pass body = %q, want \"moon\"", pass.Body.ID)
	}
	if pass.TimeToPerilune <= 0 {
		t.Errorf("planned TimeToPerilune = %.1f s, want > 0 (rebased to now)", pass.TimeToPerilune)
	}

	// No node → no planned pass.
	w.ActiveCraft().Nodes = nil
	if _, ok := w.PlannedSOIPass(); ok {
		t.Error("PlannedSOIPass should be empty with no node planted")
	}
}
