package sim

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestSpawnDesignBuildsFromStore — v0.24 / ADR 0029 S4: a saved VAB design
// spawns a craft resolved against the live catalog (composed parts
// aggregated, decouple plan carried). The design references the embedded
// starter components, so it resolves from the shipped catalog alone.
func TestSpawnDesignBuildsFromStore(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// A two-stage design: a composed booster under the composed probe sat.
	design := spacecraft.Design{
		Loadout: spacecraft.LoadoutDef{
			ID:    "test-orbiter",
			Name:  "Test Orbiter",
			Role:  "custom",
			Glyph: spacecraft.VesselGlyph,
			Color: "#5BB3FF",
			Parts: []spacecraft.PartRef{
				{PartID: "_test-orbiter_s0"},
				{PartID: "probe-sat-stage"}, // an embedded composed catalog part
			},
			DecouplePlan: []int{1},
		},
		Parts: []spacecraft.Part{
			{ID: "_test-orbiter_s0", Name: "Booster", Glyph: spacecraft.VesselGlyph, Color: "#FFD93D",
				Components: []string{"probe-engine", "probe-tank-1k"}},
		},
	}
	if err := spacecraft.SaveDesign(design); err != nil {
		t.Fatalf("SaveDesign: %v", err)
	}

	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(SpawnSpec{
		DesignID:     "test-orbiter",
		ParentBodyID: "earth",
		AltitudeM:    400e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(design): %v", err)
	}
	if w.ActiveCraft() != c {
		t.Fatal("design craft not active after spawn")
	}
	if len(c.Stages) != 2 {
		t.Fatalf("design craft stages = %d, want 2", len(c.Stages))
	}
	// Bottom stage is the composed booster (engine + tank → 50 kN, dry 300).
	if c.Stages[0].Thrust != 50000 || c.Stages[0].DryMass != 300 {
		t.Errorf("booster stage = %g N / %g kg dry, want 50000/300", c.Stages[0].Thrust, c.Stages[0].DryMass)
	}
	// Top stage is the embedded composed probe sat (dry 400).
	if c.Stages[1].DryMass != 400 {
		t.Errorf("probe-sat stage dry = %g, want 400", c.Stages[1].DryMass)
	}
	if !strings.HasPrefix(c.Name, "Test Orbiter") {
		t.Errorf("craft name = %q, want the design name (+ spawn suffix)", c.Name)
	}
	if c.Primary.ID != "earth" {
		t.Errorf("primary = %q, want earth", c.Primary.ID)
	}
}

// TestSpawnDesignMissingErrors — an unknown design ID surfaces an error
// rather than silently falling back to a default loadout.
func TestSpawnDesignMissingErrors(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(SpawnSpec{DesignID: "no-such-design"}); err == nil {
		t.Error("spawning an unknown design should error, not fall back")
	}
}
