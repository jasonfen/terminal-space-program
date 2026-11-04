package sim

import "testing"

// New file because orbit_presets.go is a new prod file (ADR 0065 S2).
// Guard 4: every authored preset sits inside its body's Orbit Band, so the
// spawn path can skip the clamp.
func TestOrbitPresetsInsideOrbitBand(t *testing.T) {
	systems := loadOrbitBandCatalog(t)
	for _, sys := range systems {
		for _, b := range sys.Bodies {
			band := OrbitBandFor(sys, b)
			for _, p := range OrbitPresetsFor(sys, b) {
				if p.PeriAltM(b) < band.FloorM {
					t.Errorf("%s/%s: perilune %.0f m below floor %.0f m", b.ID, p.Key, p.PeriAltM(b), band.FloorM)
				}
				if band.HasCeiling && p.ApoAltM(b) > band.CeilingM {
					t.Errorf("%s/%s: apolune %.0f m above ceiling %.0f m", b.ID, p.Key, p.ApoAltM(b), band.CeilingM)
				}
			}
		}
	}
	if len(OrbitPresetsFor(systems[0], *systems[0].FindBody("earth"))) != 0 {
		t.Error("earth should have no presets")
	}
	if len(OrbitPresetsFor(systems[0], *systems[0].FindBody("moon"))) != 1 {
		t.Error("moon should have the frozen preset")
	}
}
