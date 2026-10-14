package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// alongsideWorld is a world whose active vessel is a ported RCS Tug in orbit,
// nose set, ready to spawn a second ported vessel alongside.
func alongsideWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	old := w.Crafts[0]
	tug := spacecraft.NewFromLoadout("RCS-tug")
	tug.Primary = old.Primary
	tug.State = old.State
	tug.State.M = tug.TotalMass()
	tug.CurrentAttitudeDir = tug.State.V.Unit()
	tug.Name = "Home"
	w.Crafts[0] = tug
	w.stampCraftID(tug)
	return w
}

// An Alongside spawn used to fuse 0.6 s after `enter`: 25 m is inside the old
// 50 m click. It now lands port to port 25 m apart, outside the gate, and
// stays two vessels.
func TestAlongsideSpawnStaysTwoVesselsForTenSeconds(t *testing.T) {
	w := alongsideWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "RCS-tug", Alongside: true}); err != nil {
		t.Fatal(err)
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("spawn left %d vessels", len(w.Crafts))
	}
	tickFor(w, 10)
	if len(w.Crafts) != 2 {
		t.Fatalf("slate = %d vessels 10 s after an Alongside spawn, want 2: it fused", len(w.Crafts))
	}
}

// The partner spawns ahead of the active vessel's nose: ports 25 m apart,
// noses facing, same velocity.
func TestAlongsideSpawnLandsPortToPortStraight(t *testing.T) {
	w := alongsideWorld(t)
	home := w.Crafts[0]
	if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: "RCS-tug", Alongside: true}); err != nil {
		t.Fatal(err)
	}
	partner := w.ActiveCraft() // the new vessel becomes active
	if partner == home {
		t.Fatal("active vessel did not move to the spawn")
	}
	ga, gb := VesselPortGeometry(home), VesselPortGeometry(partner)
	if !ga.HasPort() || !gb.HasPort() {
		t.Fatal("fixture: both vessels should carry a port")
	}
	gap := gb.PortPoint(partner.State.R, 0).Sub(ga.PortPoint(home.State.R, 0)).Norm()
	if gap < 24.999 || gap > 25.001 {
		t.Errorf("port to port = %.4f m, want 25", gap)
	}
	if al := PortAlignmentDeg(ga.Axis, gb.Axis); al > 1e-6 {
		t.Errorf("noses are %.4f deg off facing each other, want 0", al)
	}
	if d := partner.State.V.Sub(home.State.V).Norm(); d > 1e-9 {
		t.Errorf("spawn drifts at %.4f m/s relative, want matched velocity", d)
	}
	// Straight: the line between the ports runs along home's axis.
	dir := gb.PortPoint(partner.State.R, 0).Sub(ga.PortPoint(home.State.R, 0)).Unit()
	if d := dir.Sub(ga.Axis).Norm(); d > 1e-9 {
		t.Errorf("ports not in line with the active vessel's nose (off by %.2e)", d)
	}
	_ = orbital.Vec3{}
}
