package serve

import (
	"os"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// ADR 0040 amendment 3 (10a, slice A3): the host's seat is a seat. Its World
// is its durable store, resumed at --serve start and acknowledged by its
// autosaves.

func tickHost(m tea.Model, n int) tea.Model {
	for i := 0; i < n; i++ {
		m, _ = m.Update(sim.TickMsg(time.Now()))
	}
	return m
}

// hostStackWorld is the host's World with a stack it owns: a second Vessel
// (so the Composite's ID, 2, is not the fresh default World's 1) carrying the
// guest's Lander.
func hostStackWorld(t *testing.T) (*sim.World, uint64) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	lead := guestLander(w)
	lead.ID = 0
	w.AdoptCraft(lead, false)
	idx := len(w.Crafts) - 1
	comp, _, ok := w.DockGuestCraft(idx, guestLander(w), repairGuestFP)
	if !ok {
		t.Fatal("DockGuestCraft refused")
	}
	return w, comp.ID
}

func hostDockRecord(compositeID uint64) relay.DockSnapshot {
	return relay.DockSnapshot{
		ID: 3, Owner: sessiondir.HostFingerprint, OwnerHandle: "host", DockerCraftID: compositeID,
		CompositeID: compositeID, GuestOwner: repairGuestFP, GuestHandle: "kip", GuestCraftID: repairXID,
		Phase: relay.DockActive,
	}
}

// restartOver is a server restart over dir with the same saves directory.
func restartOver(t *testing.T, dir, stateHome string) *Server {
	t.Helper()
	srv := serverOver(t, dir)
	t.Setenv("XDG_STATE_HOME", stateHome) // serverOver points it at a new temp dir
	return srv
}

// The probe from the design phase: a host-owned dock across a host restart.
// The host's World at start is the newest autosave, so the record finds its
// stack and is not reaped as a phantom (#309).
func TestHostOwnedDockSurvivesHostRestart(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)
	stateHome := os.Getenv("XDG_STATE_HOME")
	w, compID := hostStackWorld(t)
	if err := save.WriteAutosave(w); err != nil {
		t.Fatal(err)
	}
	srv.dock.SeedFull([]relay.DockSnapshot{hostDockRecord(compID)}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatal(err)
	}
	_ = srv.ln.Close()

	restarted := restartOver(t, dir, stateHome)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.ResumeNewestAutosave(); err != nil {
		t.Fatal(err)
	}
	tickHost(WrapHost(app, restarted, 0), 3)

	got := snapshotByID(t, restarted, 3)
	if got.Aborted {
		t.Errorf("the host-owned dock was reaped as a phantom after a host restart: %+v", got)
	}
	c, _, ok := app.World().CraftByID(compID)
	if !ok {
		t.Fatalf("the host's resumed World lost the stack")
	}
	if cw := save.CraftToWire(c); !carriesPart(&cw, repairXID) {
		t.Errorf("the host's stack no longer carries the guest's Vessel")
	}
}

// A fresh default World's one Vessel has ID 1 and a Composite keeps its lead's
// ID, so a record for a stack that was Composite 1 resolved against the fresh
// pad Vessel: never reaped, undock refusing forever, [J] moving the pad Vessel.
// A Vessel that carries none of the guest's parts is not that stack.
func TestHostDefaultVesselNotMistakenForComposite(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)
	srv.dock.SeedFull([]relay.DockSnapshot{hostDockRecord(1)}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatal(err)
	}
	_ = srv.ln.Close()

	restarted := serverOver(t, dir)
	app, err := tui.New(nil) // no autosave: the fresh default World
	if err != nil {
		t.Fatal(err)
	}
	if c := app.World().ActiveCraft(); c == nil || c.ID != 1 {
		t.Fatalf("test setup: the default Vessel must be ID 1")
	}
	tickHost(WrapHost(app, restarted, 0), 3)

	if got := snapshotByID(t, restarted, 3); !got.Aborted {
		t.Errorf("a fresh ID-1 pad Vessel was taken for the recorded Composite: %+v", got)
	}
	if n := len(app.World().Crafts); n != 1 || app.World().Crafts[0].ID != 1 {
		t.Errorf("the host's own Vessel was disturbed: %d crafts", n)
	}
}

// The host's autosave is its durable write: it acknowledges the host seat, so
// a settled record naming the host goes once the host has saved.
func TestHostAutosaveAcknowledges(t *testing.T) {
	srv := serverOver(t, t.TempDir())
	rec := hostDockRecord(1)
	rec.Done, rec.OwnerDirty = true, true
	srv.dock.SeedFull([]relay.DockSnapshot{rec}, nil)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	WrapHost(app, srv, 0)
	if err := app.PersistNow(); err != nil {
		t.Fatal(err)
	}
	if n := len(srv.dock.FullRecords()); n != 0 {
		t.Errorf("the host saved but the settled record still waits on it: %d", n)
	}
}

// Lazy [h] hosting is unchanged: the live World is the truth, an autosave on
// disk from an earlier run is not swapped in.
func TestLazyHostingDoesNotAutoResume(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	w, _ := hostStackWorld(t)
	if err := save.WriteAutosave(w); err != nil {
		t.Fatal(err)
	}
	app, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	live := app.World()
	rm, ok := WrapHost(app, nil, 0).(reportingModel)
	if !ok {
		t.Fatal("not a reportingModel")
	}
	hosting, _ := rm.startHosting()
	if hs := hostServer(t, hosting); hs != nil {
		t.Cleanup(func() { _ = hs.ln.Close() })
	} else {
		t.Fatal("startHosting did not start hosting")
	}
	if app.World() != live {
		t.Errorf("lazy hosting swapped the live World for the autosave")
	}
}

// --reset-fleet clears the LIVE ledger: serve.New seeded it from session.json
// before the reset ran, and clearing only the on-disk docks let the live
// ledger write the old records back.
func TestResetFleetClearsLedger(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)
	srv.dock.SeedFull([]relay.DockSnapshot{hostDockRecord(1)}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatal(err)
	}
	_ = srv.ln.Close()

	restarted := serverOver(t, dir)
	if n := len(restarted.dock.FullRecords()); n != 1 {
		t.Fatalf("test setup: restarted ledger holds %d records, want 1", n)
	}
	if _, _, err := restarted.ResetFleet(time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := len(restarted.dock.FullRecords()); n != 0 {
		t.Errorf("the live ledger still holds %d records after the reset", n)
	}
	if err := restarted.persistDocks(); err != nil { // the next flush
		t.Fatal(err)
	}
	if n := len(serverOver(t, dir).dock.FullRecords()); n != 0 {
		t.Errorf("the old records came back from session.json: %d", n)
	}
}

// F3/F9: a --serve host with nothing to resume runs tui.New's World, whose pad
// Vessel has ID 1, the same ID a host's first lead had. A host-owned fuse that
// never reached disk must hand the guest's Vessel back as a Parcel, not glue
// it onto the pad Vessel.
func TestFreshHostWorldGetsNoGuestFusedOntoPadVessel(t *testing.T) {
	srv := serverOver(t, t.TempDir())
	app, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c := app.World().ActiveCraft(); c == nil || c.ID != 1 {
		t.Fatalf("test setup: the default Vessel must be ID 1")
	}
	cp := save.CraftToWireForTransfer(guestLander(app.World()))
	rec := hostDockRecord(1)
	rec.OwnerDirty, rec.OwnerCopy = true, &cp
	srv.dock.SeedFull([]relay.DockSnapshot{rec}, nil)
	tickHost(WrapHost(app, srv, 0), 3)

	for _, c := range app.World().Crafts {
		if sim.StackHasGuest(c) {
			t.Errorf("the guest's Vessel was fused onto the host's fresh pad Vessel %d", c.ID)
		}
	}
	if got := snapshotByID(t, srv, 3); got.ReturnPayload == nil && !got.Aborted {
		t.Errorf("the guest's Vessel is not on its way back: %+v", got)
	}
}
