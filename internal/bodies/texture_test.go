package bodies

import "testing"

func TestTextureValidate(t *testing.T) {
	tests := []struct {
		name    string
		tex     *Texture
		wantErr bool
	}{
		{"nil is valid", nil, false},
		{"empty block is valid", &Texture{}, false},
		{"good full block", &Texture{
			Base:       "#6E5F50",
			Bands:      []TextureBand{{LatMin: -30, LatMax: 30, Color: "#abc"}},
			Continents: []TextureEllipse{{Lat: 15, Lon: 70, LatR: 14, LonR: 18, Color: "#3E7A35"}},
			Craters:    []TextureEllipse{{Lat: -21, Lon: 129, LatR: 3, LonR: 4, Rim: "#fff", Rays: true}},
			Spots:      []TextureEllipse{{Lat: -22, LatR: 8, LonR: 16, Color: "#A03A28"}},
			LimbTint:   "#9DC8FF",
		}, false},
		{"empty color falls back, ok", &Texture{Continents: []TextureEllipse{{LatR: 1, LonR: 1}}}, false},
		{"bad base hex", &Texture{Base: "6E5F50"}, true},
		{"bad base length", &Texture{Base: "#12345"}, true},
		{"bad limb tint", &Texture{LimbTint: "#xyz"}, true},
		{"band empty color", &Texture{Bands: []TextureBand{{LatMin: 0, LatMax: 10}}}, true},
		{"band inverted range", &Texture{Bands: []TextureBand{{LatMin: 30, LatMax: 10, Color: "#fff"}}}, true},
		{"continent zero radius", &Texture{Continents: []TextureEllipse{{Lat: 0, Lon: 0, LatR: 0, LonR: 5, Color: "#fff"}}}, true},
		{"crater bad rim", &Texture{Craters: []TextureEllipse{{LatR: 1, LonR: 1, Rim: "nope"}}}, true},
		{"mask biome bad color", &Texture{Mask: &TextureMask{Biomes: map[string]string{"land": "green"}}}, true},
		{"mask good poly", &Texture{Mask: &TextureMask{
			Biomes: map[string]string{"land": "#4E7A3A"},
			Polys:  []TextureRegion{{Kind: "land", Vertices: []LatLonPair{{0, 0}, {1, 0}, {1, 1}}}},
		}}, false},
		{"mask poly empty kind", &Texture{Mask: &TextureMask{
			Biomes: map[string]string{"land": "#4E7A3A"},
			Polys:  []TextureRegion{{Vertices: []LatLonPair{{0, 0}, {1, 0}, {1, 1}}}},
		}}, true},
		{"mask poly unknown biome", &Texture{Mask: &TextureMask{
			Biomes: map[string]string{"land": "#4E7A3A"},
			Polys:  []TextureRegion{{Kind: "sea", Vertices: []LatLonPair{{0, 0}, {1, 0}, {1, 1}}}},
		}}, true},
		{"mask poly too few vertices", &Texture{Mask: &TextureMask{
			Biomes: map[string]string{"land": "#4E7A3A"},
			Polys:  []TextureRegion{{Kind: "land", Vertices: []LatLonPair{{0, 0}, {1, 0}}}},
		}}, true},
		{"star granulation out of range", &Texture{Star: &TextureStar{Granulation: 2}}, true},
		{"star bad core color", &Texture{Star: &TextureStar{Core: "nope"}}, true},
		{"star granulation ok", &Texture{Star: &TextureStar{Core: "#FFF0C0", Granulation: 0.12, Seed: 42}}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.tex.Validate()
			if (err != nil) != tc.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, tc.wantErr)
			}
		})
	}
}

func TestValidHexColor(t *testing.T) {
	for _, c := range []string{"", "#fff", "#FFFFFF", "#1a2B3c", "#abc"} {
		if !validHexColor(c) {
			t.Errorf("validHexColor(%q) = false, want true", c)
		}
	}
	for _, c := range []string{"fff", "#ff", "#fffff", "#gggggg", "red", "#12 456"} {
		if validHexColor(c) {
			t.Errorf("validHexColor(%q) = true, want false", c)
		}
	}
}

// TestEmbeddedTexturesValidate guards against authoring typos in the
// built-in catalog: every embedded body's texture (once any are
// authored, PR2+) must pass Validate, since the loader only fail-softs
// user overlays — a broken embedded texture should fail CI, not render
// silently flat.
func TestEmbeddedTexturesValidate(t *testing.T) {
	systems, err := loadEmbedded()
	if err != nil {
		t.Fatalf("loadEmbedded: %v", err)
	}
	for _, s := range systems {
		for _, b := range s.Bodies {
			if err := b.Texture.Validate(); err != nil {
				t.Errorf("%s/%s: invalid embedded texture: %v", s.Name, b.ID, err)
			}
		}
	}
}
