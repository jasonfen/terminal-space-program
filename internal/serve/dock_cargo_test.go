package serve

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// ownerStackWithRiderDragon builds the stack owner's World (ownerFP's seat): a
// station with a Cargo Hold that the host's Cargo Dragon (craft 200) rides in,
// and seeds the matching active dock record.
func ownerStackWithRiderDragon(t *testing.T, srv *Server, ownerFP string) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	st, warn := spacecraft.ComposeStage([]string{"probe-core", "docking-port", "cargo-hold"}, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	station := w.ActiveCraft()
	station.Stages = []spacecraft.Stage{st}
	station.SyncFields()
	dragon := spacecraft.NewFromLoadout("Cargo-Dragon")
	dragon.Primary = station.Primary
	dragon.State = station.State
	dragon.ID = 200
	w.AdoptCraft(dragon, false)
	comp, _, ok := w.DockGuestCraft(0, dragon, sessiondir.HostFingerprint)
	if !ok {
		t.Fatal("DockGuestCraft refused")
	}
	w.RecomputeCommGraph()
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 7, Owner: ownerFP, OwnerHandle: "gern", DockerCraftID: station.ID,
		CompositeID: comp.ID, GuestOwner: sessiondir.HostFingerprint, GuestHandle: "jason",
		GuestCraftID: 200, Phase: relay.DockActive,
	}}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatal(err)
	}
	return w
}

func holdOf(c *spacecraft.Spacecraft) (cargo, hold float64) {
	for _, s := range c.Stages {
		cargo += s.CargoKg
		hold += s.HoldKg
	}
	return
}

// ADR 0057 conflict 2a through the real seams: the host rides a friend's stack
// with their Cargo Dragon, presses the real u key, the intent reaches the
// serve wrapper, the ask is durable across a restart, the owner's reconcile
// moves the cargo into the owner's hold, and the rider's own next tick states
// the mass on their session slate.
func TestRiderUKeyReachesTheOwnersHoldThroughServe(t *testing.T) {
	const ownerFP = "SHA256:gern"
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, ownerFP, "gern")
	ownerWorld := ownerStackWithRiderDragon(t, srv, ownerFP)

	app, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	model := tick(srv.HostModel(app))
	if app.World().DockGuest == nil {
		t.Fatal("setup: the host is not riding as a guest after a tick")
	}

	// The real key, and the real intent it emits, routed through the wrapper.
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd == nil {
		t.Fatal("rider u produced no intent")
	}
	msg := cmd()
	if _, ok := msg.(tui.HandOverCargoMsg); !ok {
		t.Fatalf("rider u emitted %T, want HandOverCargoMsg", msg)
	}
	model, _ = model.Update(msg)

	recs := srv.dock.FullRecords()
	if len(recs) != 1 || !recs[0].HandOverAsk {
		t.Fatalf("the ledger holds no hand-over ask: %+v", recs)
	}
	// Durable before any later transition flushes it.
	if r := serverOver(t, dir).dock.FullRecords(); len(r) != 1 || !r[0].HandOverAsk {
		t.Fatalf("the ask did not survive a restart: %+v", r)
	}

	// The owner's tick performs it in the owner's World.
	srv.dock.Reconcile(ownerWorld, ownerFP, nil)
	if cargo, hold := holdOf(ownerWorld.Crafts[0]); cargo != 0 || hold != 3307 {
		t.Fatalf("owner's stack cargo %g hold %g, want 0 / 3307", cargo, hold)
	}

	// The rider's next tick: the answer shows on their slate.
	model = tick(model)
	_ = model
	var said string
	for _, e := range app.World().SessionEvents {
		if e.Kind == sim.SessionEventHandOver {
			said = e.Detail
		}
	}
	if said != "3.307 t cargo handed over" {
		t.Errorf("the rider heard %q, want 3.307 t cargo handed over (events: %+v)", said, app.World().SessionEvents)
	}
}
