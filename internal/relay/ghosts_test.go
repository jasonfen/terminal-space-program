package relay

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// reportFor snapshots w as a single-owner report.
func reportFor(t *testing.T, w *sim.World, owner string) CraftReport {
	t.Helper()
	store := NewStore()
	NewReporter(store, owner).Tick(w, time.Now())
	snaps := store.Snapshot("someone-else")
	if len(snaps) != 1 {
		t.Fatalf("expected 1 report, got %d", len(snaps))
	}
	return snaps[0]
}

// A ghost evaluated exactly one orbital period ahead of (or behind)
// the report lands back on the reported position — the analytic
// propagation is periodic, and both signs of the subspace gap work.
func TestGhostPeriodicityAheadAndBehind(t *testing.T) {
	wOwner, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	rep := reportFor(t, wOwner, "SHA256:owner")
	cs := rep.Crafts[0]

	c := wOwner.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	el := orbital.ElementsFromState(c.State.R, c.State.V, mu)
	period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)

	for _, dir := range []float64{+1, -1} {
		viewer, err := sim.NewWorld()
		if err != nil {
			t.Fatalf("NewWorld viewer: %v", err)
		}
		viewer.Clock.SimTime = rep.SubspaceTime.Add(time.Duration(dir * period * float64(time.Second)))
		ghosts := GhostsFor(viewer, []CraftReport{rep}, map[string]string{"SHA256:owner": "gern"})
		if len(ghosts) != len(rep.Crafts) {
			t.Fatalf("dir %v: %d ghosts, want %d", dir, len(ghosts), len(rep.Crafts))
		}
		g := ghosts[0]
		if g.Handle != "gern" {
			t.Errorf("handle join failed: %q", g.Handle)
		}
		primary, ok := bodyByID(viewer.System(), cs.Primary)
		if !ok {
			t.Fatalf("primary %q not found", cs.Primary)
		}
		rel := g.Pos.Sub(viewer.BodyPosition(primary))
		if d := rel.Sub(cs.R).Norm(); d > 1.0 {
			t.Errorf("dir %v: ghost off by %.3f m after one period (rel=%v want=%v)", dir, d, rel, cs.R)
		}
	}
}

// Ghosts gate to the viewer's active system (ADR 0015 composition).
func TestGhostOtherSystemInvisible(t *testing.T) {
	viewer, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	rep := CraftReport{
		Owner:        "SHA256:owner",
		SubspaceTime: viewer.Clock.SimTime,
		Crafts: []CraftState{{
			ID: 1, Name: "far-away", System: "Definitely-Not-" + viewer.System().Name,
			Primary: viewer.System().Bodies[0].ID,
			R:       orbital.Vec3{X: 7e6}, V: orbital.Vec3{Y: 7500},
		}},
	}
	if ghosts := GhostsFor(viewer, []CraftReport{rep}, nil); len(ghosts) != 0 {
		t.Errorf("other-system craft ghosted: %+v", ghosts)
	}
}

// ADR 0066 decision 4 (guard 7): a parked craft is placed by body rotation
// at the viewer's clock, not coasted. The owner reports at t0; the viewer is
// 26 h on; a viewer-side vessel parked at the same spot, pinned by the real
// landed integrator, is the truth the ghost must match to under a metre.
func TestGhostLandedPlacedByRotation(t *testing.T) {
	owner, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	oc := owner.ActiveCraft()
	oc.Landed = true
	oc.LandedLatDeg, oc.LandedLonDeg = 28.5, -80.6
	oc.AntennaKind, oc.AntennaRangeM, oc.Controllable = "relay", 1e9, true
	rep := reportFor(t, owner, "SHA256:owner")
	cs := rep.Crafts[0]
	if !cs.Landed || cs.LatDeg != 28.5 || cs.LonDeg != -80.6 || cs.AntennaKind != "relay" || !cs.Controllable {
		t.Fatalf("report lost the landed fields: %+v", cs)
	}

	viewer, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld viewer: %v", err)
	}
	viewer.Clock.SimTime = rep.SubspaceTime.Add(26 * time.Hour)
	vc := viewer.ActiveCraft()
	vc.Landed = true
	vc.LandedLatDeg, vc.LandedLonDeg = 28.5, -80.6
	viewer.Tick() // integrateLanded pins vc at the viewer's clock
	ghosts := GhostsFor(viewer, []CraftReport{rep}, nil)
	if len(ghosts) != 1 || !ghosts[0].Landed {
		t.Fatalf("want one landed ghost, got %+v", ghosts)
	}
	g := ghosts[0]
	if d := g.RelPos.Sub(vc.State.R).Norm(); d > 1.0 {
		t.Errorf("landed ghost off by %.1f m after 26 h of skew", d)
	}
	if d := g.Vel.Sub(vc.State.V).Norm(); d > 0.01 {
		t.Errorf("landed ghost velocity off by %.3f m/s from omega x R", d)
	}
	if g.LatDeg != 28.5 || g.LonDeg != -80.6 || g.AntennaRangeM != 1e9 || !g.Controllable {
		t.Errorf("landed fields not carried: %+v", g)
	}
}

// A landed report with no lat/lon (an older build) yields no ghost.
func TestGhostLandedSkipped(t *testing.T) {
	viewer, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	rep := CraftReport{
		Owner:        "SHA256:owner",
		SubspaceTime: viewer.Clock.SimTime,
		Crafts: []CraftState{{
			ID: 1, Name: "parked", System: viewer.System().Name,
			Primary: viewer.System().Bodies[0].ID, Landed: true,
		}},
	}
	if ghosts := GhostsFor(viewer, []CraftReport{rep}, nil); len(ghosts) != 0 {
		t.Errorf("landed craft without lat/lon ghosted: %+v", ghosts)
	}
}

// A ghost around a non-home primary (SOI'd, e.g. a moon) rebases onto
// THAT body's position at the viewer's time.
func TestGhostSOIPrimaryOffset(t *testing.T) {
	viewer, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Pick any body with a parent (an orbiting body, not the star).
	var moon string
	for _, b := range viewer.System().Bodies {
		if b.ParentID != "" {
			moon = b.ID
			break
		}
	}
	if moon == "" {
		t.Skip("no orbiting body in system")
	}
	primary, _ := bodyByID(viewer.System(), moon)
	mu := primary.GravitationalParameter()
	r0 := primary.RadiusMeters() * 1.5
	rel := orbital.Vec3{X: r0}
	vel := orbital.Vec3{Y: math.Sqrt(mu / r0)} // circular

	rep := CraftReport{
		Owner:        "SHA256:owner",
		SubspaceTime: viewer.Clock.SimTime, // dt = 0: pure rebase check
		Crafts: []CraftState{{
			ID: 1, Name: "orbiter", System: viewer.System().Name,
			Primary: moon, R: rel, V: vel,
		}},
	}
	ghosts := GhostsFor(viewer, []CraftReport{rep}, nil)
	if len(ghosts) != 1 {
		t.Fatalf("%d ghosts, want 1", len(ghosts))
	}
	want := viewer.BodyPosition(primary).Add(rel)
	if d := ghosts[0].Pos.Sub(want).Norm(); d > 1e-6 {
		t.Errorf("SOI'd ghost off its primary by %.3g m", d)
	}
	if ghosts[0].PrimaryID != moon {
		t.Errorf("PrimaryID = %q, want %q", ghosts[0].PrimaryID, moon)
	}
	// v0.28 S2: the primary-relative state (RelPos, Vel) is retained on the
	// ghost — not discarded into the world-frame sum — so the orbit screen
	// can rebuild the ellipse via ElementsFromState. At dt=0 it equals the
	// reported state exactly.
	if d := ghosts[0].RelPos.Sub(rel).Norm(); d > 1e-6 {
		t.Errorf("RelPos off reported by %.3g m (got %v, want %v)", d, ghosts[0].RelPos, rel)
	}
	if d := ghosts[0].Vel.Sub(vel).Norm(); d > 1e-6 {
		t.Errorf("Vel off reported by %.3g m/s", d)
	}
}

// F5: a crashed craft is on the surface at its crater. It crosses the wire as
// a parked one, so the viewer pins it to the crater by rotation instead of
// Kepler-stepping the frozen impact state under the ground across the skew.
func TestGhostCrashedStaysAtCrater(t *testing.T) {
	owner, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	oc := owner.ActiveCraft()
	oc.Crashed = true
	oc.LandedLatDeg, oc.LandedLonDeg = 10, 20
	oc.State.R = oc.State.R.Scale(oc.Primary.RadiusMeters() / oc.State.R.Norm())
	rep := reportFor(t, owner, "SHA256:owner")

	viewer, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld viewer: %v", err)
	}
	viewer.Clock.SimTime = rep.SubspaceTime.Add(time.Hour)
	ghosts := GhostsFor(viewer, []CraftReport{rep}, nil)
	if len(ghosts) != 1 {
		t.Fatalf("want one ghost, got %d", len(ghosts))
	}
	g := ghosts[0]
	if alt := g.RelPos.Norm() - oc.Primary.RadiusMeters(); math.Abs(alt) > 1 {
		t.Errorf("crashed ghost %.0f m off the surface after 1 h of skew", alt)
	}
	if !g.Landed || g.LatDeg != 10 || g.LonDeg != 20 {
		t.Errorf("crashed ghost not pinned to the crater: %+v", g)
	}
}
