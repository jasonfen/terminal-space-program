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
		if _, shipped := shippedZoneByID(r.SiteID); shipped {
			continue // the Moon's own zones (ADR 0068 decision 9b)
		}
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

// ADR 0066 decision 5 / Q4: a partner's site is a PARTNERS row named
// "handle: name", not ownable (no r/d), and aiming at it resolves THEIR site
// even when its id equals one of yours (ids are per-owner counters).
func TestTargetMenuPartnerSiteRowAndTarget(t *testing.T) {
	w, lander, ownID := siteWorld(t) // own site id 1 at (12.5, -33) on the Moon
	partner := Site{ID: ownID, Body: "Moon", System: w.System().Name, LatDeg: -40, LonDeg: 100, Kind: SiteWreck, Name: "Orion site", Owner: "SHA256:ansi"}
	elsewhere := partner
	elsewhere.ID, elsewhere.Body, elsewhere.Name = 7, "Earth", "Far site"
	w.PartnerSites = []PartnerSite{{Site: partner, Handle: "ansi"}, {Site: elsewhere, Handle: "ansi"}}

	rows, _ := w.TargetMenu()
	var got []TargetRow
	for _, r := range rows {
		if r.Group == GroupPartners && r.Kind == TargetSite {
			got = append(got, r)
		}
	}
	if len(got) != 1 || got[0].Name != "ansi: Orion site" || got[0].Own || !got[0].Wreck {
		t.Fatalf("partner site rows = %+v, want one wreck row 'ansi: Orion site', not own (the Earth one is not on this body)", got)
	}

	w.SetTargetPartnerSite(got[0].GhostOwner, got[0].SiteID)
	if w.Target.Kind != TargetSite || w.Target.GhostOwner != "SHA256:ansi" || w.TargetName() != "ansi: Orion site" {
		t.Fatalf("target %+v name %q", w.Target, w.TargetName())
	}
	st, ok := w.TargetState()
	if !ok {
		t.Fatal("partner site target did not resolve")
	}
	r, _ := surfacePointState(lander.Primary, -40, 100, w.Clock.SimTime)
	if d := st.R.Sub(w.BodyPosition(lander.Primary).Add(r)).Norm(); d > 1e-6 {
		t.Errorf("partner site state is %g m from its own lat/lon, want <= 1e-6 (not your site at the same id)", d)
	}
	w.SetTargetSite(ownID)
	if w.TargetName() != lander.Name+" site" || w.Target.GhostOwner != "" {
		t.Errorf("own site %d after a partner's: %+v name %q", ownID, w.Target, w.TargetName())
	}
	w.SetTargetPartnerSite("SHA256:nobody", ownID)
	if w.Target.Kind != TargetNone {
		t.Errorf("an unknown partner pair left target %+v", w.Target)
	}
}
