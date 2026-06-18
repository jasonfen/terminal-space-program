package render

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// center renders the disk-center pixel (dx=dy=0), which maps to the
// sub-observer point (subLat=subLon=0 → body lat/lon 0,0).
func centerColor(ct *compiledTexture) lipgloss.Color {
	return ct.colorAt(0, 0, 30, 0, 0, 0, 1)
}

func TestCompileTextureBaseFallback(t *testing.T) {
	// Empty base → falls back to the body Color passed to compile.
	ct := compileTexture(&bodies.Texture{}, "#445566")
	if got := centerColor(ct); got != lipgloss.Color("#445566") {
		t.Errorf("empty base: got %v, want body color #445566", got)
	}
	// Explicit base wins over body color.
	ct = compileTexture(&bodies.Texture{Base: "#112233"}, "#445566")
	if got := centerColor(ct); got != lipgloss.Color("#112233") {
		t.Errorf("explicit base: got %v, want #112233", got)
	}
}

func TestColorAtContinentFill(t *testing.T) {
	// A continent ellipse centered on the sub-observer point colors the
	// center pixel; a pixel well outside it keeps the base.
	ct := compileTexture(&bodies.Texture{
		Base:       "#101010",
		Continents: []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 20, LonR: 20, Color: "#20A020"}},
	}, "")
	if got := centerColor(ct); got != lipgloss.Color("#20A020") {
		t.Errorf("center inside continent: got %v, want #20A020", got)
	}
	// Pixel near the limb (far from lat/lon 0) should be base.
	if got := ct.colorAt(28, 0, 30, 0, 0, 0, 1); got != lipgloss.Color("#101010") {
		t.Errorf("limb pixel: got %v, want base #101010", got)
	}
}

func TestColorAtBandLookup(t *testing.T) {
	ct := compileTexture(&bodies.Texture{
		Base:  "#000000",
		Bands: []bodies.TextureBand{{LatMin: -10, LatMax: 10, Color: "#D7B98C"}},
	}, "")
	// Center is lat 0 → inside the equatorial band.
	if got := centerColor(ct); got != lipgloss.Color("#D7B98C") {
		t.Errorf("equatorial band: got %v, want #D7B98C", got)
	}
}

func TestColorAtCraterRim(t *testing.T) {
	// A crater with a rim: center pixel is the floor, a pixel in the
	// outer ring is the rim.
	ct := compileTexture(&bodies.Texture{
		Base:    "#202020",
		Craters: []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 30, LonR: 30, Color: "#404040", Rim: "#F0F0F0"}},
	}, "")
	if got := centerColor(ct); got != lipgloss.Color("#404040") {
		t.Errorf("crater floor: got %v, want #404040", got)
	}
	// A pixel offset toward the rim: dx chosen so the projected lat/lon
	// lands in the rim band (rr > craterRimInner) but still inside.
	got := ct.colorAt(0, 12, 30, 0, 0, 0, 1)
	if got != lipgloss.Color("#F0F0F0") {
		t.Errorf("crater rim: got %v, want rim #F0F0F0", got)
	}
}

func TestColorAtCraterDefaultBright(t *testing.T) {
	// Crater with neither color nor rim falls back to the bright default
	// so it stays visible.
	ct := compileTexture(&bodies.Texture{
		Base:    "#202020",
		Craters: []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 10, LonR: 10, Rays: true}},
	}, "")
	if got := centerColor(ct); got != craterDefaultBright {
		t.Errorf("rayed crater default: got %v, want %v", got, craterDefaultBright)
	}
}

// TestColorAtResolutionOrder confirms craters paint over continents,
// which paint over bands, which paint over base — all at the center.
func TestColorAtResolutionOrder(t *testing.T) {
	ct := compileTexture(&bodies.Texture{
		Base:       "#000000",
		Bands:      []bodies.TextureBand{{LatMin: -90, LatMax: 90, Color: "#111111"}},
		Continents: []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 50, LonR: 50, Color: "#222222"}},
		Craters:    []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 50, LonR: 50, Color: "#333333"}},
	}, "")
	if got := centerColor(ct); got != lipgloss.Color("#333333") {
		t.Errorf("resolution order: got %v, want crater #333333 on top", got)
	}
}

func TestCompileTextureDropsZeroRadius(t *testing.T) {
	ct := compileTexture(&bodies.Texture{
		Continents: []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 0, LonR: 5, Color: "#fff"}},
		Spots:      []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 5, LonR: 0, Color: "#fff"}},
	}, "#202020")
	if len(ct.continents) != 0 {
		t.Errorf("zero-latR continent not dropped: %d", len(ct.continents))
	}
	if len(ct.spots) != 0 {
		t.Errorf("zero-lonR spot not dropped: %d", len(ct.spots))
	}
}

func TestColorAtMask(t *testing.T) {
	// A mask region (a big polygon around lat/lon 0) colors the center;
	// outside it stays base.
	ct := compileTexture(&bodies.Texture{
		Base: "#1F5C82",
		Mask: &bodies.TextureMask{
			Biomes: map[string]string{"land": "#4E7A3A"},
			Polys: []bodies.TextureRegion{{
				Kind: "land",
				Vertices: []bodies.LatLonPair{
					{Lat: -20, Lon: -20}, {Lat: 20, Lon: -20},
					{Lat: 20, Lon: 20}, {Lat: -20, Lon: 20},
				},
			}},
		},
	}, "")
	if got := centerColor(ct); got != lipgloss.Color("#4E7A3A") {
		t.Errorf("center inside mask poly: got %v, want land #4E7A3A", got)
	}
	// Pixel projecting to a far longitude (outside the ±20 poly) → base.
	if got := ct.colorAt(20, 0, 30, 0, 0, 0, 1); got != lipgloss.Color("#1F5C82") {
		t.Errorf("pixel outside mask poly: got %v, want base #1F5C82", got)
	}
}

func TestColorAtLimbTint(t *testing.T) {
	ct := compileTexture(&bodies.Texture{Base: "#101010", LimbTint: "#9DC8FF"}, "")
	// Center is well inside → base.
	if got := centerColor(ct); got != lipgloss.Color("#101010") {
		t.Errorf("center: got %v, want base", got)
	}
	// A near-limb pixel (r2 = 0.946 > limbTintThreshold) → tint.
	if got := ct.colorAt(23, 4, 24, 0, 0, 0, 1); got != lipgloss.Color("#9DC8FF") {
		t.Errorf("limb pixel: got %v, want limb tint #9DC8FF", got)
	}
}

func TestStarConcentricBands(t *testing.T) {
	ct := compileTexture(&bodies.Texture{
		Star: &bodies.TextureStar{Core: "#FFF0C0", Surface: "#FFD27F", Limb: "#E89030"},
	}, "")
	if ct.star == nil {
		t.Fatal("star not compiled")
	}
	// Center → core; mid-disk → surface (no granulation); limb → limb.
	if got := ct.colorAt(0, 0, 24, 0, 0, 0, 1); got != lipgloss.Color("#FFF0C0") {
		t.Errorf("core: got %v, want #FFF0C0", got)
	}
	if got := ct.colorAt(0, 18, 24, 0, 0, 0, 1); got != lipgloss.Color("#FFD27F") {
		t.Errorf("surface (r2~0.56): got %v, want #FFD27F", got)
	}
	if got := ct.colorAt(0, 23, 24, 0, 0, 0, 1); got != lipgloss.Color("#E89030") {
		t.Errorf("limb (r2~0.92): got %v, want #E89030", got)
	}
}

func TestStarGranulationDeterministic(t *testing.T) {
	star := &bodies.TextureStar{Surface: "#FFD27F", Granulation: 0.2, Seed: 7}
	a := compileTexture(&bodies.Texture{Star: star}, "")
	b := compileTexture(&bodies.Texture{Star: star}, "")
	// Same seed → identical surface pixel (granulation is reproducible).
	pa := a.colorAt(2, 17, 24, 0, 0, 0, 1)
	pb := b.colorAt(2, 17, 24, 0, 0, 0, 1)
	if pa != pb {
		t.Errorf("granulation not deterministic: %v vs %v", pa, pb)
	}
}

func TestStarExemptFromShading(t *testing.T) {
	// A body declaring a star kind must not be day/night shaded even
	// when a light is supplied — it is the light source.
	b := bodies.CelestialBody{
		ID:      "lumen",
		Color:   "#FFD27F",
		Texture: &bodies.Texture{Star: &bodies.TextureStar{Core: "#FFF0C0", Surface: "#FFD27F", Limb: "#E89030"}},
	}
	light := &SolarLight{} // any non-nil light
	tex := TextureFor(b, 24, 0, 0, 0, 1, light)
	if tex == nil {
		t.Fatal("nil texture for star body")
	}
	// Core pixel should be the raw core color, not a Shade()-darkened one.
	if got := tex(0, 0, 24); got != lipgloss.Color("#FFF0C0") {
		t.Errorf("star core shaded: got %v, want raw #FFF0C0", got)
	}
}

// TestBodyTextureBaseUsesEngine confirms the bodyTextureBase fallthrough
// routes a body carrying a Texture spec through the generic engine
// (a non-Sol ID that would otherwise return nil).
func TestBodyTextureBaseUsesEngine(t *testing.T) {
	b := bodies.CelestialBody{
		ID:    "kern",
		Color: "#5C8C4A",
		Texture: &bodies.Texture{
			Base:       "#1F5F94",
			Continents: []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 30, LonR: 30, Color: "#5C8C4A"}},
		},
	}
	tex := bodyTextureBase(b, 0, 0, 0, 1)
	if tex == nil {
		t.Fatal("bodyTextureBase returned nil for a body with a Texture spec")
	}
	if got := tex(0, 0, 30); got != lipgloss.Color("#5C8C4A") {
		t.Errorf("engine center: got %v, want continent #5C8C4A", got)
	}
	// A body without a texture and an unknown ID still returns nil.
	if bodyTextureBase(bodies.CelestialBody{ID: "kern"}, 0, 0, 0, 1) != nil {
		t.Error("untextured unknown body should return nil")
	}
}
