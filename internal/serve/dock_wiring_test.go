package serve

import (
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// TestPersistDocksConvergesUnderConcurrency: the guarded, re-snapshotting
// persist keeps the persisted cross-ref equal to the live ledger even when
// sessions race to persist while docks are being added — no concurrently-added
// dock is dropped by a stale snapshot (v0.28 finding 3). Runs under -race.
func TestPersistDocksConvergesUnderConcurrency(t *testing.T) {
	srv := newOfflineServer(t)

	// Re-snapshot semantics: a single persist writes the CURRENT full ledger.
	srv.dock.Seed([]relay.DockRecord{{ID: 1, Owner: "a", GuestOwner: "b", GuestCraftID: 10, Phase: relay.DockActive}})
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("persistDocks: %v", err)
	}

	// Race: goroutines add a dock then persist, exercising the guard while the
	// ledger mutates underneath concurrent snapshots.
	const n = 8
	var wg sync.WaitGroup
	for i := 2; i <= n; i++ {
		wg.Add(1)
		go func(id uint64) {
			defer wg.Done()
			srv.dock.Seed([]relay.DockRecord{{ID: id, Owner: "a", GuestOwner: "b", GuestCraftID: id, Phase: relay.DockActive}})
			_ = srv.persistDocks()
		}(uint64(i))
	}
	wg.Wait()

	// A final persist converges the on-disk set to the live ledger.
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("final persistDocks: %v", err)
	}
	live := srv.dock.Records()
	meta, err := srv.store.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	if len(meta.Docks) != len(live) {
		t.Fatalf("persisted %d docks, live ledger has %d — a concurrent add was dropped", len(meta.Docks), len(live))
	}
	persisted := map[uint64]bool{}
	for _, d := range meta.Docks {
		persisted[d.ID] = true
	}
	for _, r := range live {
		if !persisted[r.ID] {
			t.Errorf("live dock %d missing from persisted cross-ref", r.ID)
		}
	}
}

// hostStackHasGuest reports whether the host world's first craft is a
// cross-player stack (composite carrying a guest component).
func hostStackHasGuest(w *sim.World) bool {
	return len(w.Crafts) > 0 && sim.StackHasGuest(w.Crafts[0])
}

// TestCrossPlayerDockThroughReportingModels drives the full serve seam: two
// sessions (host + guest) share one server (store + relay + dock ledger). With
// the guest's craft coincident and co-warp-coupled, the host's tick detects
// contact and claims; the guest's tick hands its craft over; the host's tick
// fuses one cross-player stack it owns; the guest goes docked-as-guest and the
// roster marker lights up. Exercised end-to-end under -race across the store.
func TestCrossPlayerDockThroughReportingModels(t *testing.T) {
	const guestFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, guestFP, "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New host: %v", err)
	}
	guestApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New guest: %v", err)
	}
	var host tea.Model = srv.HostModel(hostApp)
	var guest tea.Model = srv.withReporting(guestApp, guestFP)

	hw, gw := hostApp.World(), guestApp.World()
	gw.Clock.SimTime = hw.Clock.SimTime
	gw.ActiveCraft().ID = 777
	gw.ActiveCraft().State = hw.ActiveCraft().State

	// Prime the guest first (no host report in the store yet → no couple, no
	// claim), then the host — so the host's priming tick is the one that sees
	// the coupled guest ghost and claims, making the host the docker.
	guest = tick(guest)
	host = tick(host)

	for i := 0; i < 6 && !hostStackHasGuest(hw); i++ {
		// Keep the two in the same subspace and, while the guest still holds
		// its craft, co-located so co-warp couples and contact is detected.
		// The host ticks first each round, so the host is the one that detects
		// contact and claims — it becomes the docker (both sides would
		// otherwise race; the ledger's engaged-craft guard keeps it to one
		// record, but tick order decides the docker).
		gw.Clock.SimTime = hw.Clock.SimTime
		if gc, hc := gw.ActiveCraft(), hw.ActiveCraft(); gc != nil && hc != nil && !hostStackHasGuest(hw) {
			gc.State = hc.State
		}
		host, _ = host.Update(sim.TickMsg(time.Now()))
		guest, _ = guest.Update(sim.TickMsg(time.Now()))
	}

	if !hostStackHasGuest(hw) {
		t.Fatalf("host never fused a cross-player stack (crafts=%d, coupled=%v)", len(hw.Crafts), hw.CoWarp.Coupled)
	}
	if gw.DockGuest == nil || gw.DockGuest.OwnerFP != sessiondir.HostFingerprint {
		t.Errorf("guest world not docked-as-guest: %+v", gw.DockGuest)
	}
	if hw.Session == nil {
		t.Fatalf("host session slate nil")
	}
	var marked bool
	for _, p := range hw.Session.Players {
		if p.Fingerprint == guestFP && p.DockedGuest {
			marked = true
		}
	}
	if !marked {
		t.Errorf("guest not marked docked-as-guest on the roster: %+v", hw.Session.Players)
	}
	meta, _ := srv.store.Meta()
	if len(meta.Docks) != 1 || meta.Docks[0].GuestOwner != guestFP {
		t.Errorf("dock cross-ref not persisted: %+v", meta.Docks)
	}
}
