// One-line reason for a new test file: sites.go is a new prod file.
package serve

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

func siteRec(owner string, id uint64, name string, stamp int64) relay.SiteRecord {
	return relay.SiteRecord{Owner: owner, OwnerHandle: "h" + owner, ID: id, BodyID: "moon", System: "Sol",
		LatDeg: 12.5, LonDeg: -33, Name: name, PlantedAtNano: 1, UpdatedAtNano: stamp}
}

func siteNames(srv *Server, owner string) map[uint64]string {
	out := map[uint64]string{}
	for _, r := range srv.sites.Snapshot() {
		if r.Owner == owner {
			out[r.ID] = r.Name
		}
	}
	return out
}

func persistedSiteCount(t *testing.T, srv *Server, owner string) int {
	t.Helper()
	m, err := srv.store.Meta()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, s := range m.Sites {
		if s.Owner == owner {
			n++
		}
	}
	return n
}

// Guard 8: a restart returns every site (the far end is a second Server on the
// same directory), and a solo rename with a newer stamp wins at the next
// connect and is flushed back.
func TestSiteLedgerSurvivesRestartAndSoloRenameWins(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	cfg := Config{Addr: "127.0.0.1:0", HostKeyPath: filepath.Join(t.TempDir(), "k"), SessionDir: dir}
	srv, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	srv.sites.Plant(siteRec("A", 1, "Eagle site", 5))
	srv.sites.Plant(siteRec("B", 1, "Intrepid site", 6))
	if err := srv.persistSites(); err != nil {
		t.Fatal(err)
	}
	srv2, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(srv2.sites.Snapshot()); n != 2 {
		t.Fatalf("restart returned %d sites, want 2", n)
	}
	// A played alone and renamed it (stamp 9 > 5); connecting reconciles.
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.Sites = []sim.Site{{ID: 1, Body: "Moon", System: "Sol", LatDeg: 12.5, LonDeg: -33, Name: "Tranquility", PlantedAtNano: 1, UpdatedAtNano: 9}}
	known := map[uint64]int64{}
	if !srv2.sites.SyncSites(w, "A", "hA", known, true) {
		t.Fatal("the newer solo rename did not change the ledger")
	}
	if err := srv2.persistSites(); err != nil {
		t.Fatal(err)
	}
	if got := siteNames(srv2, "A")[1]; got != "Tranquility" {
		t.Errorf("ledger name = %q, want the newer solo rename", got)
	}
	m, _ := srv2.store.Meta()
	for _, s := range m.Sites {
		if s.Owner == "A" && s.Name != "Tranquility" {
			t.Errorf("session.json name = %q, want Tranquility", s.Name)
		}
	}
}

// Guard 8: a plain kick keeps the player's sites; a kick-and-delete drops
// them, from the ledger and from session.json, and nobody else's.
func TestPlainKickKeepsSitesAndPurgeKickDropsThem(t *testing.T) {
	for _, purge := range []bool{false, true} {
		f := startDropFixture(t)
		waitFor(t, "the guest's first report", func() bool { return reportedBy(f.srv, f.gfp) })
		f.srv.sites.Plant(siteRec(f.gfp, 1, "Guest site", 5))
		f.srv.sites.Plant(siteRec("other", 1, "Other site", 5))
		if err := f.srv.persistSites(); err != nil {
			t.Fatal(err)
		}
		cmd := f.adminCmd(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: f.gfp, Handle: "pilot2", Purge: purge})
		f.waitGone(t, 2*time.Second, "kick")
		want := 1
		if purge {
			if cmd == nil {
				t.Fatal("a purge kick returned no follow-up")
			}
			if done, ok := cmd().(purgeDoneMsg); !ok || done.err != nil {
				t.Fatalf("purge: %+v", done)
			}
			want = 0
		}
		if n := len(siteNames(f.srv, f.gfp)); n != want || persistedSiteCount(t, f.srv, f.gfp) != want {
			t.Errorf("purge=%v: ledger %d, session.json %d sites, want %d each", purge, n, persistedSiteCount(t, f.srv, f.gfp), want)
		}
		if len(siteNames(f.srv, "other")) != 1 || persistedSiteCount(t, f.srv, "other") != 1 {
			t.Errorf("purge=%v: another player's site was dropped", purge)
		}
	}
}

// A seat's World reaches the ledger through the real refresh: a flag in the
// host's World is in the ledger and in session.json after one refresh, a
// rename follows, a delete removes it, and another player's flag arrives as a
// PartnerSite named by its handle.
func TestRefreshSessionPlantsRenamesAndRemovesHostSite(t *testing.T) {
	f := startDropFixture(t)
	hm := f.host.(reportingModel)
	host := sessiondir.HostFingerprint
	w := f.app.World()
	w.Sites = []sim.Site{{ID: 1, Body: "Moon", System: "Sol", LatDeg: 1, LonDeg: 2, Name: "Host site", PlantedAtNano: 1, UpdatedAtNano: 1}}
	w.NextSiteID = 1
	hm.refreshSession(time.Now()) // connect
	w.Sites = append(w.Sites, sim.Site{ID: 2, Body: "Moon", System: "Sol", Name: "Landed", PlantedAtNano: 2, UpdatedAtNano: 2})
	hm.refreshSession(time.Now()) // steady plant
	if got := siteNames(f.srv, host); len(got) != 2 || persistedSiteCount(t, f.srv, host) != 2 {
		t.Fatalf("ledger %v, session.json %d, want both flags in both", got, persistedSiteCount(t, f.srv, host))
	}
	w.RenameSite(1, "Renamed")
	hm.refreshSession(time.Now())
	if got := siteNames(f.srv, host)[1]; got != "Renamed" {
		t.Errorf("ledger name after r = %q", got)
	}
	w.RemoveSite(2)
	hm.refreshSession(time.Now())
	if got := siteNames(f.srv, host); len(got) != 1 || persistedSiteCount(t, f.srv, host) != 1 {
		t.Errorf("after d: ledger %v, session.json %d, want 1 and 1", got, persistedSiteCount(t, f.srv, host))
	}
	f.srv.sites.Plant(siteRec("other", 7, "Orion site", 3))
	hm.refreshSession(time.Now())
	if len(w.PartnerSites) != 1 || w.PartnerSites[0].Owner != "other" || w.PartnerSites[0].Body != "Moon" || w.PartnerSites[0].Name != "Orion site" {
		t.Errorf("PartnerSites = %+v, want other's Orion site on the Moon", w.PartnerSites)
	}
}
