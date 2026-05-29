package save_test

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestRoundtripBurnVectorAndPlaneChange: a BurnVector node (captured
// direction) and a BurnPlaneChange node (rotation angle) round-trip
// through save/load — both fields were dropped before v0.12. Also covers
// the ActiveBurn mirrors for a save mid-burn. v0.12.x (Slice 5).
func TestRoundtripBurnVectorAndPlaneChange(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	dir := orbital.Vec3{X: 0.6, Y: -0.8} // unit
	c.Nodes = append(c.Nodes,
		sim.ManeuverNode{
			TriggerTime: w.Clock.SimTime.Add(time.Minute),
			Mode:        spacecraft.BurnVector,
			DV:          120,
			Duration:    10 * time.Second,
			PrimaryID:   "earth",
			BurnDirUnit: dir,
		},
		sim.ManeuverNode{
			TriggerTime:    w.Clock.SimTime.Add(time.Hour),
			Mode:           spacecraft.BurnPlaneChange,
			DV:             80,
			PrimaryID:      "earth",
			PlaneChangeRad: -0.42,
		},
	)
	c.ActiveBurn = &sim.ActiveBurn{
		Mode:        spacecraft.BurnVector,
		DVRemaining: 60,
		EndTime:     w.Clock.SimTime.Add(8 * time.Second),
		PrimaryID:   "earth",
		BurnDirUnit: dir,
	}

	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	nodes := got.ActiveCraft().Nodes
	if len(nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Mode != spacecraft.BurnVector {
		t.Errorf("node 0 mode: got %v want BurnVector", nodes[0].Mode)
	}
	if d := nodes[0].BurnDirUnit.Sub(dir).Norm(); d > 1e-9 {
		t.Errorf("BurnDirUnit lost in round-trip: got %v want %v", nodes[0].BurnDirUnit, dir)
	}
	if math.Abs(nodes[1].PlaneChangeRad-(-0.42)) > 1e-9 {
		t.Errorf("PlaneChangeRad lost: got %v want -0.42", nodes[1].PlaneChangeRad)
	}
	ab := got.ActiveCraft().ActiveBurn
	if ab == nil || ab.BurnDirUnit.Sub(dir).Norm() > 1e-9 {
		t.Errorf("ActiveBurn.BurnDirUnit lost: got %+v", ab)
	}
}
