package missions

import (
	"strings"
	"testing"
)

// ADR 0053: only a vessel with a docking port can dock, and the spawn form
// opens on a launch vehicle (no port). The spawn-a-partner steps must say the
// partner needs a port, or a player following them to the letter cannot finish.
func TestDockPartnerStepsNameTheDockingPort(t *testing.T) {
	cat, err := LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, m := range cat.Missions {
		if m.ID != "tut-dock" && m.ID != "chal-dock" {
			continue
		}
		o := m.Objectives[0]
		seen++
		if !strings.Contains(o.Description, "docking port") {
			t.Errorf("%s step 1 %q does not say the partner needs a docking port", m.ID, o.Description)
		}
	}
	if seen != 2 {
		t.Fatalf("found %d dock missions, want 2", seen)
	}
}
