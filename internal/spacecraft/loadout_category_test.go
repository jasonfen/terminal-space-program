package spacecraft

import "testing"

// TestEmbeddedLoadoutsHaveCategory — every shipped loadout must carry a
// non-empty Category (ADR 0031 / S8). The spawn-form CRAFT TYPE picker groups
// by it; a blank category silently drops a loadout into the UI's "Other"
// bucket. Guards against a new loadout landing in data/loadouts.json without a
// category.
func TestEmbeddedLoadoutsHaveCategory(t *testing.T) {
	for _, id := range LoadoutOrder {
		if Loadouts[id].Category == "" {
			t.Errorf("loadout %q has no category (data/loadouts.json)", id)
		}
	}
}

// TestLoadoutCategoryRoundTrips — Category authored in the catalog data
// survives the LoadoutDef → resolveLoadout → Loadout path (the field the spawn
// form reads). A spot-check on one representative loadout plus the
// stripped-back Kern Stack (which also carries scale_class, exercising the two
// display tags together).
func TestLoadoutCategoryRoundTrips(t *testing.T) {
	cases := map[string]string{
		"Saturn-V":         "launch-vehicles",
		"Relay-Comsat":     "satellites-payloads",
		"Kern-Stack":       "mission-stacks",
		"Comsat-Carrier-3": "satellites-payloads",
	}
	for id, want := range cases {
		if got := Loadouts[id].Category; got != want {
			t.Errorf("loadout %q category = %q, want %q", id, got, want)
		}
	}
}

// TestLoadoutCrewed — the spawn-form crew tag predicate (ADR 0031 / S9):
// crewed iff any stage declares a crewed command source. The crewed-pod
// stacks (Apollo / Kern / Capsule) read crewed; launch vehicles, probes,
// carriers, and the probe-defaulted standalone Lander read uncrewed.
func TestLoadoutCrewed(t *testing.T) {
	crewed := []string{"Apollo-Stack", "Kern-Stack", "Capsule"}
	uncrewed := []string{"Saturn-V", "Falcon-9", "Relay-Comsat", "Science-Probe",
		"Comsat-Carrier-3", "Lander"}
	for _, id := range crewed {
		if !Loadouts[id].Crewed() {
			t.Errorf("loadout %q should be crewed", id)
		}
	}
	for _, id := range uncrewed {
		if Loadouts[id].Crewed() {
			t.Errorf("loadout %q should be uncrewed", id)
		}
	}
}
