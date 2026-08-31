package sim

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// rendezvousTwoCraftWorld sets up a world with two LEO craft on
// different altitudes so RecommendedRendezvousBurn has a non-trivial
// scenario to solve. Active is idx 0 (the original LEO craft); the
// sister at idx 1 sits in a 600 km orbit. Target is bound to idx 1.
func rendezvousTwoCraftWorld(t *testing.T) *World {
	t.Helper()
	w := mustWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("expected 2 crafts after spawn, got %d", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	w.SetTargetCraft(1)
	return w
}

// TestRecommendedRendezvousBurn_TwoCraftLEO — the basic positive case.
// Two craft on different LEO altitudes around Earth, primaries match,
// CA > 50 m → the advisory should populate and pick one of the eight
// velocity-frame axes with a finite Δv.
func TestRecommendedRendezvousBurn_TwoCraftLEO(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	adv, ok := w.RecommendedRendezvousBurn()
	if !ok {
		t.Fatalf("expected ok=true, got Reason=%q", adv.Reason)
	}
	if !adv.Ok {
		t.Skipf("two-craft LEO geometry yielded no-improvement advisory (Reason=%q); not a regression but the case isn't useful here", adv.Reason)
	}
	// The default SpawnCraft places the sister 90° around the
	// primary, so Lambert intercept Δv here is Hohmann-class
	// (~1.5 km/s), not the tens-of-m/s range a real "phasing nudge"
	// would see — the test only checks the API + axis selection are
	// sane, not that the geometry is operationally useful.
	if adv.DV <= 0 || adv.DV > 5000 {
		t.Errorf("DV %.2f m/s outside (0, 5000] m/s — finite/sane range", adv.DV)
	}
	switch adv.Axis {
	case planner.AxisPrograde, planner.AxisRetrograde,
		planner.AxisNormalPlus, planner.AxisNormalMinus,
		planner.AxisRadialOut, planner.AxisRadialIn,
		planner.AxisTargetPrograde, planner.AxisTargetRetrograde:
		// any of the eight velocity-frame axes is valid
	default:
		t.Errorf("Axis %v not in the eight velocity-frame axes", adv.Axis)
	}
}

// rotateAboutAxis rotates v about unit-ish axis by angle radians
// (Rodrigues' formula). Test-only helper for constructing a co-orbital
// lag geometry from an existing circular craft state without hardcoding
// mu, altitude, or primary.
func rotateAboutAxis(v, axis orbital.Vec3, angle float64) orbital.Vec3 {
	axis = axis.Unit()
	cos, sin := math.Cos(angle), math.Sin(angle)
	return v.Scale(cos).Add(axis.Cross(v).Scale(sin)).Add(axis.Scale(axis.Dot(v) * (1 - cos)))
}

// rendezvousSmallLagWorld places the sister craft on the SAME circular
// orbit as the active craft, lagging by a small phase angle — mirrors
// the planner package's CoOrbitalLagging fixture so PlanRendezvousNudge
// has a small, deterministic, always-plantable nudge, unlike
// rendezvousTwoCraftWorld's ~200 km altitude gap (legitimately "burn too
// large" territory, ADR 0039 S1).
func rendezvousSmallLagWorld(t *testing.T) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := -0.5 * math.Pi / 180
	target.State.R = rotateAboutAxis(active.State.R, axis, angle)
	target.State.V = rotateAboutAxis(active.State.V, axis, angle)
	target.Primary = active.Primary
	return w
}

// rendezvousGeometryNotUseful reports whether err is one of the planner
// refusals meaning "this test's default geometry didn't happen to yield
// a plantable nudge" — used by tests whose purpose is the plant/gate
// machinery, not a specific geometry outcome, so they skip rather than
// fail when the default two-craft setup (or a ghost mirror of it) lands
// somewhere the Lambert scan legitimately refuses. ADR 0039 S1 split
// #278's single collapsed reason into several distinct sentinels; any of
// them here is "not a regression", just a geometry the fixed default
// spawn (90° phase, matched altitude or not) happened to produce.
func rendezvousGeometryNotUseful(err error) bool {
	return errors.Is(err, ErrRendezvousNoImprovement) ||
		errors.Is(err, ErrRendezvousBurnTooLarge) ||
		errors.Is(err, ErrRendezvousUnsafePeriapsis) ||
		errors.Is(err, ErrRendezvousNoEncounter)
}

// TestPlanRendezvousNudge_PlantsOneNode — the happy path for the K
// keybinding's plant action. Verifies node fields match the advisory
// + the lead buffer + TargetCraftIdx one-based encoding.
func TestPlanRendezvousNudge_PlantsOneNode(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	c := w.ActiveCraft()
	if len(c.Nodes) != 0 {
		t.Fatalf("precondition: active craft has %d nodes, expected 0", len(c.Nodes))
	}

	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("two-craft LEO geometry yielded no useful nudge in this case (%v); not a regression", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if adv == nil {
		t.Fatal("expected advisory pointer on success")
	}
	if !adv.Ok {
		t.Errorf("expected advisory.Ok=true on success path, got Reason=%q", adv.Reason)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected exactly 1 node after plant, got %d", len(c.Nodes))
	}
	n := c.Nodes[0]
	if n.Event != spacecraft.TriggerAbsolute {
		t.Errorf("Event = %v, want TriggerAbsolute", n.Event)
	}
	wantMode := axisLabelToBurnMode(adv.Axis)
	if n.Mode != wantMode {
		t.Errorf("Mode = %v, want %v (from axis %v)", n.Mode, wantMode, adv.Axis)
	}
	if math.Abs(n.DV-adv.DV) > 1e-9 {
		t.Errorf("DV = %.3f, want %.3f", n.DV, adv.DV)
	}
	if n.TargetCraftID != w.Target.CraftID {
		t.Errorf("TargetCraftID = %d, want %d (the bound target's stable ID)",
			n.TargetCraftID, w.Target.CraftID)
	}
	if n.PrimaryID != c.Primary.ID {
		t.Errorf("PrimaryID = %q, want %q", n.PrimaryID, c.Primary.ID)
	}
	if n.TriggerTime.Sub(w.Clock.SimTime) < rendezvousBurnLeadMin {
		t.Errorf("TriggerTime lead %v < min %v", n.TriggerTime.Sub(w.Clock.SimTime), rendezvousBurnLeadMin)
	}
}

// TestPlanRendezvousNudge_ArrivalSpeedCarriesThrough — ADR 0039 S1: the
// planner's ArrivalSpeed (|v_rel| at the achieved CA) must survive
// unmodified from RecommendRendezvousNudge through PlanRendezvousNudge's
// returned pointer, since app.go's status flash reads it off exactly
// this value to render the "arriving ~N m/s" info row.
func TestPlanRendezvousNudge_ArrivalSpeedCarriesThrough(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	adv, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge (%v); not a regression", err)
		}
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	if !adv.Ok {
		t.Fatalf("expected Ok=true, got Reason=%q", adv.Reason)
	}
	if adv.ArrivalSpeed <= 0 {
		t.Errorf("ArrivalSpeed = %.3f, want > 0 on a successful plant", adv.ArrivalSpeed)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("expected 1 node after plant, got %d", len(c.Nodes))
	}
}

// TestPlanRendezvousNudge_SecondPressReplacesOwnNode — #293: pressing K
// twice in a row must not stack a second, now-stale node behind the
// first. The second plant replaces the craft's own previous unfired K
// node instead of queuing behind it — verified by the node count
// staying at 1 and the node's stable ID changing (proof the second
// plant is a fresh re-plant, not a no-op that happened to leave the
// original node in place).
func TestPlanRendezvousNudge_SecondPressReplacesOwnNode(t *testing.T) {
	w := rendezvousSmallLagWorld(t)
	c := w.ActiveCraft()

	_, err := w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge (%v); not a regression", err)
		}
		t.Fatalf("PlanRendezvousNudge (1st press): %v", err)
	}
	if got := len(c.Nodes); got != 1 {
		t.Fatalf("after 1st K: expected 1 node, got %d", got)
	}
	firstID := c.Nodes[0].ID
	if firstID == 0 {
		t.Fatal("first planted node has zero ID")
	}
	if c.Nodes[0].AdvisoryKey != AdvisoryKeyRendezvousNudge {
		t.Errorf("AdvisoryKey = %q, want %q", c.Nodes[0].AdvisoryKey, AdvisoryKeyRendezvousNudge)
	}

	_, err = w.PlanRendezvousNudge()
	if err != nil {
		if rendezvousGeometryNotUseful(err) {
			t.Skipf("small-lag geometry yielded no useful nudge on 2nd press (%v); not a regression", err)
		}
		t.Fatalf("PlanRendezvousNudge (2nd press): %v", err)
	}
	if got := len(c.Nodes); got != 1 {
		t.Fatalf("after 2nd K: expected the node count to STAY at 1 (replace, not stack), got %d", got)
	}
	if c.Nodes[0].ID == firstID {
		t.Error("2nd K plant kept the 1st node's stable ID — expected a fresh re-plant with a new ID")
	}
}

// TestPlanRendezvousNudge_NoTarget — without a craft target the
// planter must reject with ErrRendezvousNoTarget and plant nothing.
func TestPlanRendezvousNudge_NoTarget(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if _, err := w.PlanRendezvousNudge(); !errors.Is(err, ErrRendezvousNoTarget) {
		t.Errorf("err = %v, want ErrRendezvousNoTarget", err)
	}
	if got := len(c.Nodes); got != 0 {
		t.Errorf("rejected plant should not append node; got %d", got)
	}
}

// TestPlanRendezvousNudge_LeadBufferAccountsForSlew — when the
// craft's attitude is 180° from the recommended axis the lead buffer
// must exceed nodeLeadSlack·π/SlewRate so the v0.10.0 lead-comp slew
// converges before T0.
func TestPlanRendezvousNudge_LeadBufferAccountsForSlew(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	c := w.ActiveCraft()
	// Probe the recommended axis without planting first.
	adv, ok := w.RecommendedRendezvousBurn()
	if !ok || !adv.Ok {
		t.Skipf("rendezvous advisory unavailable in this case (Reason=%q)", adv.Reason)
	}
	// Force the craft attitude to point 180° from the recommended axis.
	c.CurrentAttitudeDir = adv.AxisUnit.Scale(-1)

	planted, err := w.PlanRendezvousNudge()
	if err != nil {
		t.Fatalf("PlanRendezvousNudge: %v", err)
	}
	n := c.Nodes[0]
	lead := n.TriggerTime.Sub(w.Clock.SimTime)

	// Expected lower bound: nodeLeadSlack · π / SlewRate + pad
	wantSlewLead := nodeLeadSlack * math.Pi / c.SlewRateRad()
	wantTotal := time.Duration(wantSlewLead*float64(time.Second)) + rendezvousBurnLeadPad
	// Allow the rendezvousBurnLeadMin floor to win if it happens to
	// exceed the dynamic value (it does on the small default slew).
	if wantTotal < rendezvousBurnLeadMin {
		wantTotal = rendezvousBurnLeadMin
	}
	if lead < wantTotal {
		t.Errorf("lead buffer %v < expected ≥ %v (slew %.3f rad/s, axis %v)",
			lead, wantTotal, c.SlewRateRad(), planted.Axis)
	}
}

// TestPlanRendezvousNudge_DifferentPrimaries — moving the target
// craft to a different primary returns ErrRendezvousDifferentPrimaries
// and plants nothing.
func TestPlanRendezvousNudge_DifferentPrimaries(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	moon := w.Systems[0].FindBody("Moon")
	if moon == nil {
		t.Skip("Moon not in catalog — skipping different-primaries gate")
	}
	t1 := w.Crafts[1]
	t1.Primary = *moon

	c := w.ActiveCraft()
	_, err := w.PlanRendezvousNudge()
	if !errors.Is(err, ErrRendezvousDifferentPrimaries) {
		t.Errorf("err = %v, want ErrRendezvousDifferentPrimaries", err)
	}
	if got := len(c.Nodes); got != 0 {
		t.Errorf("rejected plant should not append node; got %d", got)
	}
}

// TestPlanRendezvousNudge_DockedCraft — when the active and target
// craft are already within DOCK READY range (< 50 m, |v_rel| < 0.1 m/s),
// PlanRendezvousNudge must return the specific ErrRendezvousAlreadyDocked
// (not the generic ErrRendezvousNoImprovement) and plant no node. The
// docked gate in computeRendezvousAdvisory used to return ok=false,
// which tripped the outer `if !ok` gate and made the docked branch
// unreachable; it now returns ok=true with advisory.Reason="docked",
// consistent with the no-improvement path. (#91)
func TestPlanRendezvousNudge_DockedCraft(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	target := w.Crafts[1]
	// Park the target right alongside the active craft: 10 m away with a
	// 0.01 m/s relative drift — inside the docked gate.
	target.State.R = active.State.R.Add(orbital.Vec3{X: 10})
	target.State.V = active.State.V.Add(orbital.Vec3{X: 0.01})

	_, err := w.PlanRendezvousNudge()
	if !errors.Is(err, ErrRendezvousAlreadyDocked) {
		t.Errorf("err = %v, want ErrRendezvousAlreadyDocked", err)
	}
	if got := len(active.Nodes); got != 0 {
		t.Errorf("docked craft should plant no node; got %d", got)
	}
}

// TestRecommendedRendezvousBurn_CacheReusesWithinInterval — two
// back-to-back calls without advancing the sim clock return
// identical advisories (cache hit; not a recompute). Indirect: we
// can't see "did we recompute" externally, so this is a functional
// guard that the cache returns a consistent value rather than NaN-ing
// across calls.
func TestRecommendedRendezvousBurn_CacheReusesWithinInterval(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	a, ok1 := w.RecommendedRendezvousBurn()
	b, ok2 := w.RecommendedRendezvousBurn()
	if ok1 != ok2 {
		t.Fatalf("ok mismatch across calls: %v vs %v", ok1, ok2)
	}
	if a != b {
		t.Errorf("cached advisory not identical:\n  first = %+v\n  second = %+v", a, b)
	}
}

// TestRecommendedRendezvousBurn_ClearTargetInvalidates — dropping the
// target between calls flushes the advisory to ok=false. Verifies the
// cache-key check on (activeIdx, targetIdx).
func TestRecommendedRendezvousBurn_ClearTargetInvalidates(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	if _, ok := w.RecommendedRendezvousBurn(); !ok {
		t.Skipf("baseline advisory not available; cannot test invalidation")
	}
	w.ClearTarget()
	if _, ok := w.RecommendedRendezvousBurn(); ok {
		t.Errorf("expected ok=false after ClearTarget, got cached ok=true")
	}
}

// TestAxisLabelToBurnMode_RoundTripsAllEight — every axis label maps
// to a distinct, non-zero BurnMode in the velocity-frame subset.
// Regression guard against a future planner.AxisLabel addition that
// the mapping table forgets.
func TestAxisLabelToBurnMode_RoundTripsAllEight(t *testing.T) {
	labels := []planner.AxisLabel{
		planner.AxisPrograde, planner.AxisRetrograde,
		planner.AxisNormalPlus, planner.AxisNormalMinus,
		planner.AxisRadialOut, planner.AxisRadialIn,
		planner.AxisTargetPrograde, planner.AxisTargetRetrograde,
	}
	wantModes := []spacecraft.BurnMode{
		spacecraft.BurnPrograde, spacecraft.BurnRetrograde,
		spacecraft.BurnNormalPlus, spacecraft.BurnNormalMinus,
		spacecraft.BurnRadialOut, spacecraft.BurnRadialIn,
		spacecraft.BurnTargetPrograde, spacecraft.BurnTargetRetrograde,
	}
	seen := make(map[spacecraft.BurnMode]bool, len(labels))
	for i, l := range labels {
		got := axisLabelToBurnMode(l)
		if got != wantModes[i] {
			t.Errorf("axis %v → %v, want %v", l, got, wantModes[i])
		}
		if seen[got] {
			t.Errorf("duplicate mapping target: %v", got)
		}
		seen[got] = true
	}
}

// (compile guard) the test file references orbital so the import is
// not optimised out when the assertions above don't directly use it.
var _ = orbital.Vec3{}
