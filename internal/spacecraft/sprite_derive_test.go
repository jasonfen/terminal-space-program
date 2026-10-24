package spacecraft

import "testing"

// sivbLike is a parts-built hydrolox stage close to the catalog S-IVB
// (~113 t): one J-2 and four 25 t bulk tanks.
func sivbLike() []string {
	return []string{"j2-upper", "hydro-tank-25k", "hydro-tank-25k", "hydro-tank-25k", "hydro-tank-25k"}
}

// A stage built from parts must come out of ComposeStage with a drawable
// sprite (#596): before, LaunchSpriteRowsPx was 0 and the launch drawing
// skipped it.
func TestComposeStage_PartsBuiltStageHasSprite(t *testing.T) {
	st, warn := ComposeStage(sivbLike(), Components)
	if warn != "" {
		t.Fatal(warn)
	}
	if st.LaunchSpriteRowsPx <= 0 || st.LaunchSpriteWidthPx <= 0 {
		t.Fatalf("parts-built stage has no sprite: rows=%d width=%d", st.LaunchSpriteRowsPx, st.LaunchSpriteWidthPx)
	}
	if st.LaunchSpriteColor == "" {
		t.Fatal("parts-built stage has no sprite colour")
	}
}

// Calibration: a parts-built stage lands near the catalog stage it imitates.
func TestComposeStage_SpriteSizeTracksCatalog(t *testing.T) {
	st, _ := ComposeStage(sivbLike(), Components)
	cat, _ := BuildStage("s-ivb")
	if d := st.LaunchSpriteRowsPx - cat.LaunchSpriteRowsPx; d < -2 || d > 2 {
		t.Errorf("S-IVB-like rows %d, catalog S-IVB %d (want within 2)", st.LaunchSpriteRowsPx, cat.LaunchSpriteRowsPx)
	}
	// S-IC-like: five F-1 and 22 bulk kerolox tanks (~2.3 kt).
	ic := []string{"f1-booster", "f1-booster", "f1-booster", "f1-booster", "f1-booster"}
	for i := 0; i < 22; i++ {
		ic = append(ic, "kero-tank-100k")
	}
	big, _ := ComposeStage(ic, Components)
	cat, _ = BuildStage("s-ic")
	if d := big.LaunchSpriteRowsPx - cat.LaunchSpriteRowsPx; d < -3 || d > 3 {
		t.Errorf("S-IC-like rows %d, catalog S-IC %d (want within 3)", big.LaunchSpriteRowsPx, cat.LaunchSpriteRowsPx)
	}
}

// Heavier stages are taller, never shorter, and the result is clamped.
func TestDerivedSpriteRows_MonotonicAndClamped(t *testing.T) {
	prev := 0
	for _, m := range []float64{1, 100, 1e3, 1e4, 1e5, 1e6, 1e7, 1e9} {
		r, w := derivedSpriteDims(m)
		if r < prev {
			t.Errorf("rows fell from %d to %d at %g kg", prev, r, m)
		}
		if r < minDerivedRows || r > maxDerivedRows {
			t.Errorf("rows %d outside [%d,%d] at %g kg", r, minDerivedRows, maxDerivedRows, m)
		}
		if w < 2 || w > 5 {
			t.Errorf("width %d outside [2,5] at %g kg", w, m)
		}
		prev = r
	}
}

// Each fuel chemistry gets its own body colour, and a tankless stage a
// neutral one, so a stack of different stages reads as different blocks.
func TestDerivedSpriteColor_DistinctPerChemistry(t *testing.T) {
	seen := map[string]string{}
	for _, ft := range []string{FuelTypeKerolox, FuelTypeHydrolox, FuelTypeHypergolic, FuelTypeSolid, FuelTypeArgon, ""} {
		c := derivedSpriteColor(ft)
		if c == "" {
			t.Fatalf("no colour for %q", ft)
		}
		if other, dup := seen[c]; dup {
			t.Errorf("%q and %q share colour %s", ft, other, c)
		}
		seen[c] = ft
	}
}

// An authored sprite (a composed catalog Part that sets its own) wins.
func TestComposePart_AuthoredSpriteKept(t *testing.T) {
	p, err := composePart(Part{Components: sivbLike(), LaunchSpriteRowsPx: 7, LaunchSpriteWidthPx: 2, LaunchSpriteColor: "#123456"}, Components)
	if err != nil {
		t.Fatal(err)
	}
	if p.LaunchSpriteRowsPx != 7 || p.LaunchSpriteWidthPx != 2 || p.LaunchSpriteColor != "#123456" {
		t.Errorf("authored sprite overwritten: %+v", p)
	}
}

// An empty stage has no mass to size from and stays undrawn.
func TestComposeStage_EmptyStageUndrawn(t *testing.T) {
	st, _ := ComposeStage(nil, Components)
	if st.LaunchSpriteRowsPx != 0 {
		t.Errorf("empty stage drawn with %d rows", st.LaunchSpriteRowsPx)
	}
}
