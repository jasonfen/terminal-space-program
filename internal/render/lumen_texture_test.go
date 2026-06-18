package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// TestLumenBodiesRenderTextured is the PR2 end-to-end guard (ADR 0024):
// every Lumen body that ships a texture block must actually paint a
// multi-color disk through the generic engine — not silently render
// flat because its features landed off-disk, had degenerate radii, or
// failed to compile. Samples the disk on a grid and counts distinct
// colors; a textured body must show at least two.
func TestLumenBodiesRenderTextured(t *testing.T) {
	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	var lumen *bodies.System
	for i := range systems {
		if systems[i].Name == "Lumen" {
			lumen = &systems[i]
			break
		}
	}
	if lumen == nil {
		t.Fatal("Lumen system not found")
	}

	const pxRadius = 24 // comfortably above BodyTextureMinRadius
	textured := 0
	for _, b := range lumen.Bodies {
		if b.Texture == nil {
			continue
		}
		textured++
		tex := TextureFor(b, pxRadius, 0, 0, 0, 1, nil)
		if tex == nil {
			t.Errorf("%s: TextureFor returned nil despite a texture block", b.ID)
			continue
		}
		seen := map[string]bool{}
		for dy := -pxRadius; dy <= pxRadius; dy++ {
			for dx := -pxRadius; dx <= pxRadius; dx++ {
				if dx*dx+dy*dy > pxRadius*pxRadius {
					continue // outside the disk
				}
				seen[string(tex(dx, dy, pxRadius))] = true
			}
		}
		if len(seen) < 2 {
			t.Errorf("%s: rendered %d distinct color(s), want >= 2 (features not landing on disk?)", b.ID, len(seen))
		}
	}
	if textured != 17 {
		t.Errorf("expected all 17 Lumen bodies textured (16 surfaces + the star), got %d", textured)
	}
}
