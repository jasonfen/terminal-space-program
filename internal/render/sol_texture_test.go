package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestSolBodiesRenderTextured is the re-baselined regression guard for
// the ADR 0024 PR4 Sol migration: the 12 Sol bodies that used to have
// hand-written Go shaders now render through the data-driven engine
// from sol.json. Each must still paint a multi-color disk (catches a
// texture block that failed to land features on the disk or failed to
// compile). Replaces the per-body *PixelColor exact-color tests, which
// were retired with their functions (ADR 0024 §G re-baseline).
func TestSolBodiesRenderTextured(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var sol *bodies.System
	for i := range systems {
		if systems[i].Name == "Sol" {
			sol = &systems[i]
			break
		}
	}
	if sol == nil {
		t.Fatal("Sol system not found")
	}

	want := map[string]bool{
		"sun": true, "earth": true, "moon": true, "mars": true,
		"jupiter": true, "saturn": true, "uranus": true, "neptune": true,
		"io": true, "europa": true, "ganymede": true, "callisto": true,
	}
	const pxRadius = 32
	got := map[string]bool{}
	for _, b := range sol.Bodies {
		if b.Texture == nil {
			continue
		}
		got[b.ID] = true
		tex := TextureFor(b, pxRadius, 0, 0, 0, 1, nil)
		if tex == nil {
			t.Errorf("%s: TextureFor returned nil despite a texture block", b.ID)
			continue
		}
		seen := map[string]bool{}
		for dy := -pxRadius; dy <= pxRadius; dy++ {
			for dx := -pxRadius; dx <= pxRadius; dx++ {
				if dx*dx+dy*dy > pxRadius*pxRadius {
					continue
				}
				seen[string(tex(dx, dy, pxRadius))] = true
			}
		}
		if len(seen) < 2 {
			t.Errorf("%s: rendered %d distinct color(s), want >= 2", b.ID, len(seen))
		}
	}
	for id := range want {
		if !got[id] {
			t.Errorf("expected Sol body %q to carry a texture block", id)
		}
	}
}
