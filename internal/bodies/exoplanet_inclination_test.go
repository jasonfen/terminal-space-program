package bodies

import "testing"

// TestExoplanetInclinationsAreCoplanar (#497): Kepler-452 and TRAPPIST-1
// carried ~89.x degree "transit" inclinations in an ecliptic-relative field,
// standing every planet orbit on its side and sweeping the ADR 0050 depart:
// row 61..118 degrees. Transit inclination is against Earth's line of sight,
// not the system disc, so the honest value here is 0 (coplanar, like Alpha
// Centauri). This pins the eleven planets; a catalog edit that reintroduces
// a tilt fails here.
func TestExoplanetInclinationsAreCoplanar(t *testing.T) {
	systems, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	want := map[string]int{"Kepler-452": 4, "TRAPPIST-1": 7}
	for _, sys := range systems {
		n, watched := want[sys.Name]
		if !watched && sys.Name != "Alpha Centauri" {
			continue
		}
		planets := 0
		for _, b := range sys.Bodies {
			if b.BodyType == "Star" {
				continue
			}
			if b.Inclination != 0 {
				t.Errorf("%s / %s: inclination = %v, want 0 (coplanar)", sys.Name, b.ID, b.Inclination)
			}
			planets++
		}
		if watched && planets != n {
			t.Errorf("%s: %d non-star bodies, want %d (update the pin if the system changed on purpose)", sys.Name, planets, n)
		}
	}
}
