package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// hyperbolicInMoonSOI parks the active craft INSIDE the Moon's SOI on an
// inbound hyperbolic trajectory (e = 2, periapsis 500 km above the surface,
// currently at 0.6× the SOI radius) — the post-entry, pre-capture state the
// #157 in-SOI continuation must keep drawing. Constructed directly in the
// Moon's frame: a = −rp gives e = 1 + rp/|a| = 2 and h = √(3·μ·rp); the
// radial component points inward so the perilune is still ahead.
func hyperbolicInMoonSOI(t *testing.T, w *World) (moon bodies.CelestialBody, soi float64) {
	t.Helper()
	_, moon = findMoon(t, w)
	soi = physics.SOIRadius(moon, parentBody(w, moon))
	mu := moon.GravitationalParameter()
	rp := moon.RadiusMeters() + 500e3
	r0 := 0.6 * soi
	v0 := math.Sqrt(mu * (2/r0 + 1/rp)) // vis-viva with a = −rp
	vt := math.Sqrt(3*mu*rp) / r0       // h = √(3·μ·rp) for e = 2
	vr := -math.Sqrt(v0*v0 - vt*vt)     // inbound

	c := w.ActiveCraft()
	c.Primary = moon
	c.Landed = false
	c.Nodes = nil
	c.State.R = orbital.Vec3{X: r0}
	c.State.V = orbital.Vec3{X: vr, Y: vt}
	return moon, soi
}

// TestInSOIPassPersistsAcrossEntry flies the LEO→Moon coast through the
// actual SOI entry and pins the #157 fix: the encounter picture must NOT
// switch off the moment the Moon becomes the primary. The residence pass
// keeps the same Body, drops the (now past) Entry crossing, keeps the Exit,
// and its perilune agrees with the pre-entry prediction.
func TestInSOIPassPersistsAcrossEntry(t *testing.T) {
	w := mustWorld(t)
	moonCoast(t, w)
	c := w.ActiveCraft()

	pre, ok := w.LiveSOIPass()
	if !ok || !pre.HasEntryTime {
		t.Fatalf("precondition: live pass with entry time on the Moon coast (ok=%v hasEntryTime=%v)", ok, pre.HasEntryTime)
	}
	soi := physics.SOIRadius(pre.Body, parentBody(w, pre.Body))

	// Warp the craft to 10 minutes after the predicted SOI entry.
	dt := pre.TimeToEntry + 600
	state, primary := w.propagateStateWithPrimary(c.State, c.Primary, w.Clock.SimTime, dt)
	c.State, c.Primary = state, primary
	w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(dt * float64(time.Second)))
	if c.Primary.ID != pre.Body.ID {
		t.Fatalf("craft did not enter the Moon's SOI: primary = %q after T-entry+600s", c.Primary.ID)
	}

	post, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("LiveSOIPass vanished at SOI entry — the #157 dead zone")
	}
	if post.Body.ID != pre.Body.ID {
		t.Fatalf("residence pass body = %q, want the entered Moon %q", post.Body.ID, pre.Body.ID)
	}
	if post.HasEntry {
		t.Error("entry crossing is in the past — the residence pass must not place an Entry marker")
	}
	if post.HasEntryTime {
		t.Error("residence pass carries an entry time — there is no upcoming entry inside the SOI")
	}
	if !post.HasExit {
		t.Error("flyby leaves the SOI — the residence pass should close on the ring with an Exit crossing")
	}
	if d := post.ExitRel.Norm(); math.Abs(d-soi) > soi*0.05 {
		t.Errorf("ExitRel %.0f km from the Moon, want ring radius %.0f km (±5%%)", d/1e3, soi/1e3)
	}

	// Perilune continuity across the boundary: the pre-entry prediction and
	// the in-SOI analytic perilune describe the same flyby.
	tol := math.Max(0.1*pre.PeriluneRadius, 0.02*soi)
	if d := math.Abs(post.PeriluneRadius - pre.PeriluneRadius); d > tol {
		t.Errorf("perilune radius jumped %.0f km across SOI entry (pre %.0f km, post %.0f km, tol %.0f km)",
			d/1e3, pre.PeriluneRadius/1e3, post.PeriluneRadius/1e3, tol/1e3)
	}
	if post.TimeToPerilune <= 0 {
		t.Errorf("TimeToPerilune = %.0f s inside the SOI before perilune, want > 0", post.TimeToPerilune)
	}
	wantTCA := pre.TimeToPerilune - dt
	if d := math.Abs(post.TimeToPerilune - wantTCA); d > math.Max(0.1*wantTCA, 600) {
		t.Errorf("TimeToPerilune = %.0f s, want ≈ pre-entry TCA minus the warp = %.0f s", post.TimeToPerilune, wantTCA)
	}
}

// TestInSOIPassGeometry pins the synthesized residence pass on a directly
// constructed in-SOI hyperbola: the in-SOI leg draws Local-to-Body within
// ~1× SOI of the Moon's current position (acceptance: ~1–2× SOI), the
// onward continuation exists in the parent's frame, and the homeID trap is
// closed — passing the craft's own primary as homeID would short-circuit
// to inertial points whose tail sits where the Moon WILL be, off the drawn
// ring (the #147 smear, reintroduced).
func TestInSOIPassGeometry(t *testing.T) {
	w := mustWorld(t)
	moon, soi := hyperbolicInMoonSOI(t, w)

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("no residence pass on an in-SOI escape hyperbola")
	}
	if pass.Body.ID != moon.ID {
		t.Fatalf("pass body = %q, want the Moon", pass.Body.ID)
	}
	rp := moon.RadiusMeters() + 500e3
	if math.Abs(pass.PeriluneRadius-rp) > rp*0.01 {
		t.Errorf("PeriluneRadius = %.0f km, want the constructed periapsis %.0f km (±1%%)", pass.PeriluneRadius/1e3, rp/1e3)
	}
	if pass.Impact {
		t.Error("500 km perilune flagged as Impact")
	}
	if pass.TimeToPerilune <= 0 {
		t.Errorf("TimeToPerilune = %.0f s on the inbound leg, want > 0", pass.TimeToPerilune)
	}
	if !pass.HasPerilunePt {
		t.Fatal("pass has no perilune marker point")
	}
	if d := pass.PeriluneRel.Norm(); d < rp*0.9 || d > soi*0.1 {
		t.Errorf("sampled PeriluneRel = %.0f km, want near the %.0f km periapsis (sampling slack allowed)", d/1e3, rp/1e3)
	}
	if !pass.HasExit {
		t.Fatal("escape hyperbola should exit on the ring")
	}
	if len(pass.OnwardSegments) == 0 {
		t.Fatal("no onward continuation past the SOI exit — the path must keep going (#157)")
	}
	if got := pass.OnwardSegments[0].PrimaryID; got != moon.ParentID {
		t.Errorf("first onward segment primary = %q, want the Moon's parent %q", got, moon.ParentID)
	}

	// Local-to-Body anchoring: drawn with the system root as homeID, every
	// in-SOI sample lands within ~1× SOI of the Moon's CURRENT position.
	rootID := w.System().Bodies[0].ID
	moonNow := w.BodyPosition(moon)
	maxRebased := 0.0
	for _, seg := range pass.ArcSegments {
		for _, p := range w.SegmentDrawPoints(seg, rootID) {
			if d := p.Sub(moonNow).Norm(); d > maxRebased {
				maxRebased = d
			}
		}
	}
	if maxRebased > 1.05*soi {
		t.Errorf("rebased in-SOI arc extends %.2f×SOI from the Moon's current position, want ≤ ~1×SOI", maxRebased/soi)
	}

	// The homeID trap (#157 implementation note): with the craft's primary
	// as homeID, SegmentDrawPoints short-circuits to the inertial Points —
	// whose tail rides the Moon's future motion away from the drawn ring.
	last := pass.ArcSegments[0]
	trap := w.SegmentDrawPoints(last, moon.ID)
	if len(trap) != len(last.Points) {
		t.Fatalf("trap draw returned %d points for %d samples", len(trap), len(last.Points))
	}
	for i := range trap {
		if trap[i] != last.Points[i] {
			t.Fatal("primary-as-homeID no longer short-circuits to inertial points — trap premise gone stale")
		}
	}
	rebasedEnd := moonNow.Add(last.RelPoints[len(last.RelPoints)-1])
	inertialEnd := last.Points[len(last.Points)-1]
	if d := inertialEnd.Sub(rebasedEnd).Norm(); d < 0.1*soi {
		t.Errorf("inertial vs rebased exit differ by only %.2f×SOI — the smear premise is stale, trap test proves nothing", d/soi)
	} else {
		t.Logf("homeID trap: inertial exit sits %.2f×SOI off the drawn ring crossing (max rebased extent %.2f×SOI)", d/soi, maxRebased/soi)
	}
}

// TestInSOIPassQuietWhenCaptured pins the quiet case: a captured (bound,
// apoapsis inside the SOI) orbit synthesizes no residence pass — a parked
// low lunar orbit and a stable LEO draw exactly as before, no ring.
// TestInSOIArcIsDenseAndSmooth pins the ADR 0023 D fix for the in-SOI
// residence pass (#157): flown inside a moon's SOI on a sharp perilune, the
// arc is redrawn from its body-relative conic, so consecutive points near the
// fast periapsis stay close together — the equal-time integrated sampling
// left chords ~a full perilune radius long ("a sharp curve around a moon with
// visible angles").
func TestInSOIArcIsDenseAndSmooth(t *testing.T) {
	w := mustWorld(t)
	moon, _ := hyperbolicInMoonSOI(t, w)
	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("no in-SOI residence pass on the escape hyperbola")
	}
	rp := moon.RadiusMeters() + 500e3 // the constructed perilune

	var rel []orbital.Vec3
	for _, s := range pass.ArcSegments {
		rel = append(rel, s.RelPoints...)
	}
	if len(rel) < 50 {
		t.Fatalf("in-SOI arc has only %d points — not densified (want the analytic redraw)", len(rel))
	}
	maxNearPeri := 0.0
	for i := 1; i < len(rel); i++ {
		if rel[i].Norm() < 3*rp { // the sharp turn around the moon
			if d := rel[i].Sub(rel[i-1]).Norm(); d > maxNearPeri {
				maxNearPeri = d
			}
		}
	}
	if maxNearPeri > 0.5*rp {
		t.Errorf("sharpest in-SOI chord near periapsis %.0f km = %.2f×perilune — facets visible, want < 0.5×perilune", maxNearPeri/1e3, maxNearPeri/rp)
	}
}

func TestInSOIPassQuietWhenCaptured(t *testing.T) {
	w := mustWorld(t)
	_, moon := findMoon(t, w)
	c := w.ActiveCraft()
	c.Primary = moon
	c.Landed = false
	c.Nodes = nil
	mu := moon.GravitationalParameter()
	r := moon.RadiusMeters() + 200e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	if _, ok := w.LiveSOIPass(); ok {
		t.Error("captured low lunar orbit produced a pass — the quiet case regressed")
	}
	if _, ok := w.CounterfactualSOIPass(); ok {
		t.Error("captured low lunar orbit produced a counterfactual pass")
	}
}

// TestInSOIPassBoundButEscaping pins the third escape clause: a bound orbit
// whose apoapsis reaches past the parent-relative SOI radius leaves the SOI
// and gets the residence pass (the craft crosses the ring before apoapsis).
func TestInSOIPassBoundButEscaping(t *testing.T) {
	w := mustWorld(t)
	_, moon := findMoon(t, w)
	soi := physics.SOIRadius(moon, parentBody(w, moon))
	c := w.ActiveCraft()
	c.Primary = moon
	c.Landed = false
	c.Nodes = nil
	mu := moon.GravitationalParameter()
	rp := moon.RadiusMeters() + 200e3
	ra := 1.2 * soi
	a := (rp + ra) / 2
	c.State.R = orbital.Vec3{X: rp}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu * (2/rp - 1/a))}

	pass, ok := w.LiveSOIPass()
	if !ok {
		t.Fatal("bound orbit with apoapsis past the SOI radius should synthesize a residence pass")
	}
	if pass.Body.ID != moon.ID {
		t.Fatalf("pass body = %q, want the Moon", pass.Body.ID)
	}
	if !pass.HasExit {
		t.Error("outbound ellipse crosses the ring before apoapsis — expected an Exit crossing")
	}
}

// TestInSOICounterfactualCappedAtNode: with a capture node planted inside
// the SOI, the dim no-burn residence arc truncates at the node (never drawn
// past the burn, ADR 0019 D) so it ends in the interior with no Exit; the
// planned legs keep drawing through the unchanged drawNodes path, where the
// craft's primary as homeID short-circuits in-SOI legs to their inertial
// samples exactly as before this change.
func TestInSOICounterfactualCappedAtNode(t *testing.T) {
	w := mustWorld(t)
	moon, soi := hyperbolicInMoonSOI(t, w)
	c := w.ActiveCraft()

	live, ok := w.LiveSOIPass()
	if !ok || live.TimeToPerilune <= 0 {
		t.Fatalf("precondition: live residence pass with perilune ahead (ok=%v tca=%.0f)", ok, live.TimeToPerilune)
	}

	// Plant a node halfway to perilune — before the exit by construction.
	c.Nodes = []spacecraft.ManeuverNode{{TriggerTime: w.Clock.SimTime.Add(time.Duration(live.TimeToPerilune / 2 * float64(time.Second)))}}
	cf, ok := w.CounterfactualSOIPass()
	if !ok {
		t.Fatal("counterfactual residence pass vanished with a node planted — the ring would drop during capture planning")
	}
	if cf.Body.ID != moon.ID {
		t.Fatalf("counterfactual body = %q, want the Moon", cf.Body.ID)
	}
	if cf.HasExit {
		t.Error("node-capped counterfactual still reports an Exit crossing — arc drawn past the burn")
	}
	for _, seg := range cf.ArcSegments {
		for _, r := range seg.RelPoints {
			if d := r.Norm(); d > soi*1.05 {
				t.Errorf("node-capped counterfactual sample %.2f×SOI from the Moon — escaped past the node", d/soi)
			}
		}
	}

	// Planned legs unchanged: drawNodes draws them with the craft's primary
	// as homeID, and an in-SOI (home) leg short-circuits to its inertial
	// sample positions — the pre-#157 behavior, byte for byte.
	legs := w.PredictedLegs()
	if len(legs) == 0 {
		t.Fatal("no predicted legs with a node planted")
	}
	for _, seg := range w.PredictedSegmentsFrom(legs[0].State, legs[0].Primary, legs[0].StartClock, legs[0].HorizonSecs, legs[0].Samples) {
		if seg.PrimaryID != moon.ID {
			continue
		}
		draw := w.SegmentDrawPoints(seg, c.Primary.ID)
		for i := range draw {
			if draw[i] != seg.Points[i] {
				t.Fatal("planted in-SOI leg no longer draws at its inertial samples — drawNodes behavior changed")
			}
		}
	}
}

// TestFocusZoomRadiusInsideEscapingSOI extends the ADR 0021 F pin to the
// residence case: focusing the body the craft is escaping fits to 1.3× its
// parent-relative SOI — ring, arc, and markers land in frame. Still a
// Framing-Event-only read (the Camera Contract is untouched; this test
// calls FocusZoomRadius directly, as the orbit screen does once per event).
func TestFocusZoomRadiusInsideEscapingSOI(t *testing.T) {
	w := mustWorld(t)
	moonIdx, _ := findMoon(t, w)
	moon, soi := hyperbolicInMoonSOI(t, w)

	w.Focus = Focus{Kind: FocusBody, BodyIdx: moonIdx}
	got := w.FocusZoomRadius()
	want := physics.SOIRadius(moon, parentBody(w, moon)) * 1.3
	if math.Abs(got-want) > want*1e-9 {
		t.Errorf("FocusZoomRadius inside the escaping SOI = %.0f km, want 1.3× parent-relative SOI = %.0f km (soi %.0f km)",
			got/1e3, want/1e3, soi/1e3)
	}
}
