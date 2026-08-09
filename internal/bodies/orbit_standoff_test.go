package bodies

import "testing"

// TestOrbitStandOffM covers ADR 0044 §3 ("Stars have a floor but no
// ceiling"): a star's Orbit Floor is authored heat stand-off, not the
// flat CutoffAltitude+25km rule, because a star has no atmosphere but
// must not be orbitable just above its photosphere.
func TestOrbitStandOffM(t *testing.T) {
	tests := []struct {
		name string
		b    CelestialBody
		want float64
	}{
		{
			name: "authored star returns authored value in metres",
			b:    CelestialBody{BodyType: "Star", OrbitStandOffKm: 10_000_000, MeanRadius: 695_700},
			want: 10_000_000_000,
		},
		{
			name: "unauthored star falls back to 10x mean radius in metres",
			b:    CelestialBody{BodyType: "Star", MeanRadius: 100_000},
			want: 10 * 100_000 * 1000,
		},
		{
			name: "planet is always zero regardless of authored field",
			b:    CelestialBody{BodyType: "Planet", OrbitStandOffKm: 999, MeanRadius: 6371},
			want: 0,
		},
		{
			name: "moon is always zero",
			b:    CelestialBody{BodyType: "Moon", MeanRadius: 1737},
			want: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.b.OrbitStandOffM(); got != tt.want {
				t.Errorf("OrbitStandOffM() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestOrbitStandOffMFallbackSanity checks the unauthored fallback
// (10x mean radius) lands in the right order of magnitude against
// Sol's authored value, so the fallback isn't wildly off for the five
// stars (Alpha Centauri A/B/Proxima, Kepler-452, TRAPPIST-1) that
// don't carry an authored number.
func TestOrbitStandOffMFallbackSanity(t *testing.T) {
	sol := CelestialBody{BodyType: "Star", MeanRadius: 696_342} // no OrbitStandOffKm authored
	fallback := sol.OrbitStandOffM()
	const solAuthored = 10_000_000_000.0 // 10,000,000 km in metres
	if fallback <= 0 {
		t.Fatalf("fallback OrbitStandOffM() = %v, want > 0", fallback)
	}
	// Same order of magnitude: within a factor of 10 either way.
	if fallback > solAuthored*10 || fallback < solAuthored/10 {
		t.Errorf("fallback %v is not within an order of magnitude of Sol's authored %v", fallback, solAuthored)
	}
}

// TestAuthoredStarStandOffsLoad guards against a JSON typo in the
// shipped catalog: Sol's Sun and Lumen's star must both load with the
// authored ADR 0044 stand-off values.
func TestAuthoredStarStandOffsLoad(t *testing.T) {
	systems, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll() error: %v", err)
	}

	cases := []struct {
		system   string
		bodyID   string
		wantKm   float64
	}{
		{"Sol", "sun", 10_000_000},
		{"Lumen", "lumen", 500_000},
	}

	for _, tc := range cases {
		t.Run(tc.system, func(t *testing.T) {
			var sys *System
			for i := range systems {
				if systems[i].Name == tc.system {
					sys = &systems[i]
					break
				}
			}
			if sys == nil {
				t.Fatalf("system %q not found in loaded catalog", tc.system)
			}
			b := sys.FindBody(tc.bodyID)
			if b == nil {
				t.Fatalf("body %q not found in system %q", tc.bodyID, tc.system)
			}
			if b.OrbitStandOffKm != tc.wantKm {
				t.Errorf("%s.%s OrbitStandOffKm = %v, want %v", tc.system, tc.bodyID, b.OrbitStandOffKm, tc.wantKm)
			}
			if got, want := b.OrbitStandOffM(), tc.wantKm*1000; got != want {
				t.Errorf("%s.%s OrbitStandOffM() = %v, want %v", tc.system, tc.bodyID, got, want)
			}
		})
	}
}
