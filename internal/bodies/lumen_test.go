package bodies

import (
	"math"
	"testing"
)

// lumenSystem fetches the Lumen system from the full catalog, failing
// the test if it is not wired into LoadAll.
func lumenSystem(t *testing.T) *System {
	t.Helper()
	systems, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for i := range systems {
		if systems[i].Name == "Lumen" {
			return &systems[i]
		}
	}
	t.Fatal("Lumen system not found in catalog")
	return nil
}

// TestLumenLoads is the tracer bullet: Lumen is in the catalog and its
// primary (index 0) is the Lumen star.
func TestLumenLoads(t *testing.T) {
	lumen := lumenSystem(t)
	primary := lumen.Primary()
	if primary == nil {
		t.Fatal("Lumen has no primary")
	}
	if primary.EnglishName != "Lumen" {
		t.Errorf("primary englishName = %q, want Lumen", primary.EnglishName)
	}
	if primary.BodyType != "Star" {
		t.Errorf("primary bodyType = %q, want Star", primary.BodyType)
	}
	// Lumen is the canonical stripped-back system (ADR 0014).
	if got := lumen.Scale(); got != ScaleStrippedBack {
		t.Errorf("Lumen scale class = %q, want stripped-back", got)
	}
}

// TestLumenBodyCount asserts the full 17-body roster: 1 star, 7 planets,
// 9 moons (the KSP-stock analog count).
func TestLumenBodyCount(t *testing.T) {
	lumen := lumenSystem(t)
	if got := len(lumen.Bodies); got != 17 {
		t.Fatalf("body count = %d, want 17", got)
	}
	counts := map[string]int{}
	for _, b := range lumen.Bodies {
		counts[b.BodyType]++
	}
	for typ, want := range map[string]int{"Star": 1, "Planet": 7, "Moon": 9} {
		if counts[typ] != want {
			t.Errorf("%s count = %d, want %d", typ, counts[typ], want)
		}
	}
}

// TestLumenHierarchy checks the SOI tree: every planet orbits the star,
// every moon orbits its named planet (ParentOf resolves correctly).
func TestLumenHierarchy(t *testing.T) {
	lumen := lumenSystem(t)

	// Planets orbit the star (ParentID empty → Primary).
	planets := []string{"kern", "rust", "ember", "bit", "dash", "daemon", "cache"}
	for _, id := range planets {
		b := lumen.FindBody(id)
		if b == nil {
			t.Errorf("planet %q missing", id)
			continue
		}
		if b.ParentID != "" {
			t.Errorf("planet %q parentId = %q, want empty (orbits star)", id, b.ParentID)
		}
		if parent := lumen.ParentOf(*b); parent == nil || parent.ID != "lumen" {
			t.Errorf("planet %q does not resolve to the Lumen star", id)
		}
	}

	// Moons orbit their planet.
	wantParent := map[string]string{
		"cursor": "kern",  // Mun
		"glyph":  "kern",  // Minmus
		"mote":   "ember", // Gilly
		"flag":   "rust",  // Ike
		"shell":  "daemon", "pipe": "daemon", "fork": "daemon",
		"byte": "daemon", "nib": "daemon",
	}
	for moon, planet := range wantParent {
		b := lumen.FindBody(moon)
		if b == nil {
			t.Errorf("moon %q missing", moon)
			continue
		}
		if b.ParentID != planet {
			t.Errorf("moon %q parentId = %q, want %q", moon, b.ParentID, planet)
		}
		parent := lumen.ParentOf(*b)
		if parent == nil || parent.ID != planet {
			t.Errorf("moon %q does not resolve to planet %q", moon, planet)
		}
	}
}

// TestLumenStrippedBackScale guards the ADR 0014 premise: Kern (the
// home planet) sits at the KSP-stock ~3.4 km/s-to-orbit scale, an order
// of magnitude below Earth's ~11.2 km/s escape.
func TestLumenStrippedBackScale(t *testing.T) {
	lumen := lumenSystem(t)
	kern := lumen.FindBody("kern")
	if kern == nil {
		t.Fatal("Kern not found")
	}
	// Kern escape velocity ≈ 3.43 km/s (Kerbin). Sanity-bound it well
	// below the 5 km/s mark to assert it is genuinely stripped-back.
	if kern.Escape < 3.0 || kern.Escape > 4.0 {
		t.Errorf("Kern escape = %.3f km/s, want ~3.43 (stripped-back scale)", kern.Escape)
	}
}

// TestLumenPhysicallyConsistent guards against data-entry typos: every
// body's stated surface gravity and escape velocity must agree with the
// values the integrator derives from mass and radius (G·M/R² and
// √(2GM/R)). Catches a fat-fingered mass exponent or radius.
func TestLumenPhysicallyConsistent(t *testing.T) {
	lumen := lumenSystem(t)
	for _, b := range lumen.Bodies {
		r := b.RadiusMeters()
		mu := b.GravitationalParameter()
		if r == 0 || mu == 0 {
			t.Errorf("%s: zero radius or mass", b.ID)
			continue
		}
		// Surface gravity (m/s²).
		gWant := mu / (r * r)
		if rel := math.Abs(b.Gravity-gWant) / gWant; rel > 0.01 {
			t.Errorf("%s: gravity %.4f m/s² deviates from G·M/R²=%.4f by %.2f%%",
				b.ID, b.Gravity, gWant, rel*100)
		}
		// Escape velocity (km/s).
		escWant := math.Sqrt(2*mu/r) / 1000.0
		if rel := math.Abs(b.Escape-escWant) / escWant; rel > 0.01 {
			t.Errorf("%s: escape %.4f km/s deviates from √(2GM/R)=%.4f by %.2f%%",
				b.ID, b.Escape, escWant, rel*100)
		}
	}
}
