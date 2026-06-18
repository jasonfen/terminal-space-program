package bodies

import "fmt"

// Texture is the optional data-driven surface-texture spec for a body
// (ADR 0024). It replaces the hardcoded per-body Go shaders in
// internal/render: a single generic engine consumes this block, so any
// system — including user overlays in $XDG_CONFIG_HOME — can be
// textured, which the old `switch b.ID` model structurally could not.
//
// The spec carries *typed* feature kinds rather than one flat list, so
// each kind can render kind-specific detail. The presence of a kind
// selects the look; an empty block (or a body with no Texture at all)
// renders as a flat base-color disk.
//
// PR1 (this slice) renders the ellipse kinds (continents / craters /
// spots) and bands. The mask / limb-tint / star kinds are carried in
// the schema from PR1 — so the field round-trips through JSON and is
// covered by the catalog-hash exclusion (see catalog.go) — but are
// consumed by the engine starting in PR3 (ADR 0024 rollout).
//
// Texture is excluded from bodies.CatalogHash: it is cosmetic, not
// semantic, so it must never bump the hash and reject existing saves.
type Texture struct {
	// Base is the disk's underlying surface color (hex, e.g. "#6E5F50").
	// Empty falls back to the body's Color.
	Base string `json:"base,omitempty"`

	// Bands are latitude sweeps for gas/ice giants — ordered, first
	// match wins per pixel.
	Bands []TextureBand `json:"bands,omitempty"`

	// Continents are filled ellipse features (maria, albedo regions,
	// land masses). Layered in table order — last match wins.
	Continents []TextureEllipse `json:"continents,omitempty"`

	// Craters are ellipse features with optional rim + rayed ejecta —
	// rendered on top of continents/bands. Last match wins.
	Craters []TextureEllipse `json:"craters,omitempty"`

	// Spots are storm/feature ellipses (e.g. a Great Red Spot)
	// layered over bands. Last match wins.
	Spots []TextureEllipse `json:"spots,omitempty"`

	// Mask is the Earth-class polygon land/ocean/biome mask kind.
	// Carried from PR1; rendered starting PR3.
	Mask *TextureMask `json:"mask,omitempty"`

	// LimbTint is an atmospheric-halo color painted over the outer
	// ring of the disk (hex). Carried from PR1; rendered starting PR3.
	LimbTint string `json:"limbTint,omitempty"`

	// Star is the self-luminous star-surface kind (limb darkening +
	// granulation, light-source exempt). Carried from PR1; rendered
	// starting PR3.
	Star *TextureStar `json:"star,omitempty"`
}

// TextureBand is a latitude sweep: every pixel whose body-latitude is
// in [LatMin, LatMax) takes Color.
type TextureBand struct {
	LatMin float64 `json:"latMin"`
	LatMax float64 `json:"latMax"`
	Color  string  `json:"color"`
}

// TextureEllipse is a lat/lon-axis-aligned ellipse feature. Lat/Lon is
// the center (degrees); LatR/LonR are the semi-axes along latitude and
// longitude (degrees). Color fills the ellipse. For craters, Rim (when
// set) colors the outer ring and Rays marks bright ejecta.
type TextureEllipse struct {
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
	LatR  float64 `json:"latR"`
	LonR  float64 `json:"lonR"`
	Color string  `json:"color,omitempty"`
	Rim   string  `json:"rim,omitempty"`
	Rays  bool    `json:"rays,omitempty"`
}

// TextureMask is the Earth-class polygon land/ocean mask kind. Polys
// is a list of named regions (land / desert / ice) given as polygon
// vertex lists; Biomes maps a region kind to a hex color. Carried in
// the schema from PR1; the engine consumes it starting PR3.
type TextureMask struct {
	Polys  []TextureRegion   `json:"polys,omitempty"`
	Biomes map[string]string `json:"biomes,omitempty"`
}

// TextureRegion is one named polygon in a TextureMask.
type TextureRegion struct {
	Kind     string       `json:"kind,omitempty"`
	Vertices []LatLonPair `json:"vertices,omitempty"`
}

// LatLonPair is a single (lat, lon) vertex in degrees.
type LatLonPair struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// TextureStar is the self-luminous star-surface kind. Granulation is
// the granulation amplitude (0..1); Seed seeds the noise. Carried in
// the schema from PR1; the engine consumes it starting PR3.
type TextureStar struct {
	Granulation float64 `json:"granulation,omitempty"`
	Seed        int64   `json:"seed,omitempty"`
}

// Validate reports the first structural problem in a texture spec, or
// nil when it is renderable. It is intentionally lenient: an empty
// block is valid (flat disk), and absent colors fall back at render
// time. It flags only the cases that would otherwise render as silent
// garbage — malformed hex colors, non-positive ellipse radii, and
// inverted band ranges. Used by the loader to fail-soft on user-overlay
// systems (warn + drop the texture → flat disk) rather than hard-error.
func (t *Texture) Validate() error {
	if t == nil {
		return nil
	}
	if !validHexColor(t.Base) {
		return fmt.Errorf("texture: invalid base color %q", t.Base)
	}
	if !validHexColor(t.LimbTint) {
		return fmt.Errorf("texture: invalid limbTint color %q", t.LimbTint)
	}
	for i, b := range t.Bands {
		if !validHexColor(b.Color) || b.Color == "" {
			return fmt.Errorf("texture: band %d has invalid color %q", i, b.Color)
		}
		if b.LatMin >= b.LatMax {
			return fmt.Errorf("texture: band %d has latMin %g >= latMax %g", i, b.LatMin, b.LatMax)
		}
	}
	for kind, set := range map[string][]TextureEllipse{
		"continent": t.Continents,
		"crater":    t.Craters,
		"spot":      t.Spots,
	} {
		for i, e := range set {
			if e.LatR <= 0 || e.LonR <= 0 {
				return fmt.Errorf("texture: %s %d has non-positive radius (latR %g, lonR %g)", kind, i, e.LatR, e.LonR)
			}
			if !validHexColor(e.Color) || !validHexColor(e.Rim) {
				return fmt.Errorf("texture: %s %d has invalid color %q / rim %q", kind, i, e.Color, e.Rim)
			}
		}
	}
	if t.Mask != nil {
		for kind, c := range t.Mask.Biomes {
			if !validHexColor(c) || c == "" {
				return fmt.Errorf("texture: mask biome %q has invalid color %q", kind, c)
			}
		}
	}
	if t.Star != nil && (t.Star.Granulation < 0 || t.Star.Granulation > 1) {
		return fmt.Errorf("texture: star granulation %g out of [0,1]", t.Star.Granulation)
	}
	return nil
}

// validHexColor reports whether s is empty (allowed — falls back) or a
// "#rgb" / "#rrggbb" hex color. ANSI names/indices are intentionally
// not accepted in the texture schema; textures speak hex.
func validHexColor(s string) bool {
	if s == "" {
		return true
	}
	if s[0] != '#' || (len(s) != 4 && len(s) != 7) {
		return false
	}
	for _, c := range s[1:] {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
