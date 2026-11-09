package relay

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func ledgerWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func moonSite(id uint64, name string, stamp int64) sim.Site {
	return sim.Site{ID: id, Body: "Moon", System: "Sol", LatDeg: 12.5, LonDeg: -33, Name: name, PlantedAtNano: 1, UpdatedAtNano: stamp}
}

// Guard 8: a rename from the wrong fingerprint reaches nothing.
func TestSiteLedgerRenameIsOwnerOnly(t *testing.T) {
	l := NewSiteLedger()
	l.Plant(SiteRecord{Owner: "A", ID: 1, BodyID: "moon", System: "Sol", Name: "old", UpdatedAtNano: 5})
	if l.Rename("B", 1, "mine now", 9) || l.Remove("B", 1) {
		t.Fatal("B reached A's site")
	}
	if r, _ := l.get("A", 1); r.Name != "old" {
		t.Errorf("name = %q after a refused rename", r.Name)
	}
	if !l.Rename("A", 1, "new", 5) { // equal stamp is accepted
		t.Error("owner rename at an equal stamp refused")
	}
	if l.Rename("A", 1, "stale", 4) {
		t.Error("an older stamp renamed over a newer record")
	}
}

// Connect reconcile: newest wins per site, World-only is published,
// ledger-only is put back, and the ledger id floor lifts NextSiteID.
func TestSyncSitesConnectNewestWinsPublishesAndPutsBack(t *testing.T) {
	w := ledgerWorld(t)
	moon := moonSite(1, "world newer", 20)
	w.Sites = []sim.Site{moon, moonSite(2, "world old", 10), moonSite(3, "world only", 7)}
	w.NextSiteID = 3
	l := NewSiteLedger()
	for _, s := range []sim.Site{moonSite(2, "ledger newer", 30), moonSite(1, "ledger old", 10), moonSite(9, "ledger only", 4)} {
		r, ok := SiteRecordOf(w, s, "A", "ann")
		if !ok {
			t.Fatal("moon not in catalog")
		}
		l.Plant(r)
	}
	if !l.SyncSites(w, "A", "ann", map[uint64]int64{}, true) {
		t.Fatal("connect published a site but reported no change")
	}
	got := map[uint64]string{}
	for _, s := range w.Sites {
		got[s.ID] = s.Name
	}
	want := map[uint64]string{1: "world newer", 2: "ledger newer", 3: "world only", 9: "ledger only"}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("world site %d = %q, want %q", id, got[id], n)
		}
	}
	for id, n := range map[uint64]string{1: "world newer", 3: "world only", 9: "ledger only"} {
		if r, _ := l.get("A", id); r.Name != n {
			t.Errorf("ledger site %d = %q, want %q", id, r.Name, n)
		}
	}
	if w.NextSiteID < 9 {
		t.Errorf("NextSiteID = %d, want >= 9 so a new flag cannot reuse a ledger id", w.NextSiteID)
	}
}

// Steady ticks: plant, rename and delete reach the ledger; an idle tick does not.
func TestSyncSitesSteadyPlantRenameRemove(t *testing.T) {
	w := ledgerWorld(t)
	l := NewSiteLedger()
	known := map[uint64]int64{}
	if l.SyncSites(w, "A", "ann", known, true) {
		t.Fatal("empty connect changed the ledger")
	}
	w.Sites = []sim.Site{moonSite(1, "Eagle site", 5)}
	if !l.SyncSites(w, "A", "ann", known, false) {
		t.Fatal("a planted site did not reach the ledger")
	}
	if l.SyncSites(w, "A", "ann", known, false) {
		t.Error("an idle tick reported a change")
	}
	w.Sites[0].Name, w.Sites[0].UpdatedAtNano = "Tranquility", 6
	if !l.SyncSites(w, "A", "ann", known, false) {
		t.Fatal("a rename did not reach the ledger")
	}
	if r, _ := l.get("A", 1); r.Name != "Tranquility" || r.BodyID != "moon" {
		t.Errorf("record = %+v, want renamed with BodyID moon", r)
	}
	w.Sites = nil
	if !l.SyncSites(w, "A", "ann", known, false) {
		t.Fatal("a delete did not reach the ledger")
	}
	if len(l.Snapshot()) != 0 {
		t.Errorf("ledger kept %d sites after the owner deleted", len(l.Snapshot()))
	}
}

func TestSiteLedgerDropSitesOfAndPartnerSitesFor(t *testing.T) {
	w := ledgerWorld(t)
	l := NewSiteLedger()
	for _, o := range []string{"A", "B"} {
		r, _ := SiteRecordOf(w, moonSite(1, o+" site", 5), o, "h"+o)
		l.Plant(r)
	}
	ps := l.PartnerSitesFor(w, "A", map[string]string{"B": "bob"})
	if len(ps) != 1 || ps[0].Owner != "B" || ps[0].Handle != "bob" || ps[0].Body != "Moon" {
		t.Fatalf("partner sites for A = %+v", ps)
	}
	if n := l.DropSitesOf("B"); n != 1 || len(l.Snapshot()) != 1 {
		t.Errorf("DropSitesOf(B) = %d, ledger %d, want 1 and 1", n, len(l.Snapshot()))
	}
}

// #891: ids are per save. A different save joining must not get the old save's
// flags back, and its own flag 1 must not collide with the old flag 1; the same
// save (or a pre-#891 record with no SaveID) still gets its flags back.
func TestSyncSitesConnectFromDifferentSave(t *testing.T) {
	old := ledgerWorld(t)
	old.SaveID = "save-a"
	l := NewSiteLedger()
	for _, s := range []sim.Site{moonSite(1, "old one", 10), moonSite(2, "old two", 10)} {
		r, _ := SiteRecordOf(old, s, "A", "ann")
		l.Plant(r)
	}
	legacy, _ := SiteRecordOf(old, moonSite(3, "legacy", 10), "A", "ann")
	legacy.SaveID = ""
	l.Plant(legacy)

	cases := []struct {
		name, save string
		wantNames  []string
	}{
		{"different save", "save-b", []string{"new one", "legacy"}},
		{"same save rejoins", "save-a", []string{"old one", "old two", "legacy"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ll := NewSiteLedger()
			ll.Seed(l.Snapshot())
			w := ledgerWorld(t)
			w.SaveID = c.save
			if c.save == "save-b" {
				w.Sites = []sim.Site{moonSite(1, "new one", 99)} // newer stamp than the old flag 1
				w.NextSiteID = 1
			}
			ll.SyncSites(w, "A", "ann", map[uint64]int64{}, true)
			var got []string
			for _, s := range w.Sites {
				got = append(got, s.Name)
			}
			if len(got) != len(c.wantNames) {
				t.Fatalf("world sites = %v, want %v", got, c.wantNames)
			}
			for i, n := range c.wantNames {
				if got[i] != n {
					t.Errorf("world sites = %v, want %v", got, c.wantNames)
				}
			}
			if c.save == "save-b" {
				if r, _ := ll.get("A", 1); r.Name != "new one" || r.SaveID != "save-b" {
					t.Errorf("ledger flag 1 = %+v, want the new save's", r)
				}
				if _, ok := ll.get("A", 2); ok {
					t.Error("old save's flag 2 still in the ledger")
				}
			}
		})
	}
}
