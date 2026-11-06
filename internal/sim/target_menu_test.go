package sim

import "testing"

// ADR 0066 guard 1 (sim half): the row the menu opens on is exactly the
// target one `t` press aimed at before the menu, on three fixtures.
func TestTargetMenuOpensOnWhatOneCyclePressTargeted(t *testing.T) {
	fixtures := map[string]func(t *testing.T) *World{
		"LEO": func(t *testing.T) *World {
			w, err := NewWorld()
			if err != nil {
				t.Fatal(err)
			}
			return w
		},
		"second vessel alongside": func(t *testing.T) *World {
			w, err := NewWorld()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
				t.Fatal(err)
			}
			return w
		},
		"landed with a site": func(t *testing.T) *World {
			w, _, _ := siteWorld(t)
			return w
		},
	}
	for name, mk := range fixtures {
		w := mk(t)
		rows, open := w.TargetMenu()
		if open < 0 || open >= len(rows) {
			t.Fatalf("%s: no opening row (open=%d of %d)", name, open, len(rows))
		}
		w.CycleTarget(true)
		if rows[open].Target != w.Target {
			t.Errorf("%s: menu opens on %q (%+v), one t aimed at %+v", name, rows[open].Name, rows[open].Target, w.Target)
		}
	}
}

func TestTargetMenuGroupsSitesOnThisBodyAndMarksOwn(t *testing.T) {
	w, _, id := siteWorld(t)
	rows, _ := w.TargetMenu()
	var sites int
	last := GroupVessels
	for _, r := range rows {
		if r.Group < last {
			t.Fatalf("groups out of order at %q", r.Name)
		}
		last = r.Group
		if r.Group == GroupSites {
			sites++
			if !r.Own || r.SiteID != id {
				t.Errorf("site row %+v: want own, id %d", r, id)
			}
		}
	}
	if sites != 1 {
		t.Errorf("site rows = %d, want 1", sites)
	}
}
