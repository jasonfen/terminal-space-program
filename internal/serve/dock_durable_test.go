package serve

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestParkedHandoverSurvivesAServerRestart drives #311 through the real
// serve seam: a composite parked on a dock record, persisted to
// session.json, and a BRAND NEW Server built over the same directory —
// the production restart, not a ledger reseed. The measured failure was
// that the composite came back in no player's World and no save while
// the record naming it outlived it; here it must still be on the record,
// ready for the recipient's next connect.
func TestParkedHandoverSurvivesAServerRestart(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)

	// A stack parked mid-[J]: the record owns the only copy.
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := w.ActiveCraft()
	stack.ID = 2
	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	guest.Primary = stack.Primary
	guest.State = stack.State
	guest.ID = 200
	w.AdoptCraft(guest, false)
	if _, _, ok := w.DockGuestCraft(0, guest, "SHA256:gern"); !ok {
		t.Fatalf("DockGuestCraft refused")
	}
	composite, _ := w.RemoveCraftByID(w.Crafts[0].ID)
	wantMass := composite.TotalMass()
	wantR := composite.State.R

	systems, err := bodies.LoadAll()
	if err != nil {
		t.Fatalf("bodies.LoadAll: %v", err)
	}
	parked := save.CraftToWire(composite)
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 3, Owner: "SHA256:gern", OwnerHandle: "gern", DockerCraftID: 200,
		CompositeID: composite.ID, GuestOwner: "local", GuestHandle: "jason",
		GuestCraftID: 2, Phase: relay.DockActive,
		TransferPayload: &parked,
	}}, systems)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("persistDocks: %v", err)
	}
	_ = srv.ln.Close()

	// The restart: a fresh Server over the same session directory.
	restarted := serverOver(t, dir)
	recs := restarted.dock.FullRecords()
	if len(recs) != 1 {
		t.Fatalf("restored records = %d, want 1", len(recs))
	}
	if recs[0].TransferPayload == nil {
		t.Fatalf("the composite did not survive the restart — it exists in no World and no save (#311)")
	}

	// And it delivers: the recipient's first reconcile adopts it.
	wRecipient, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	wRecipient.Crafts = nil
	chips := restarted.dock.Reconcile(wRecipient, "SHA256:gern", nil)
	if len(wRecipient.Crafts) != 1 {
		t.Fatalf("recipient holds %d craft after connecting, want the delivered stack", len(wRecipient.Crafts))
	}
	got := wRecipient.Crafts[0]
	if got.TotalMass() != wantMass || got.State.R != wantR {
		t.Errorf("delivered stack = mass %v at %v, want mass %v at %v", got.TotalMass(), got.State.R, wantMass, wantR)
	}
	var told bool
	for _, c := range chips {
		if c.Kind == sim.SessionEventTransfer {
			told = true
		}
	}
	if !told {
		t.Errorf("delivery after a restart said nothing: %+v", chips)
	}
}

// serverOver builds a Server rooted at an existing session directory, so a
// test can stand one up twice over the same state — what a restart is.
func serverOver(t *testing.T, dir string) *Server {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: filepath.Join(t.TempDir(), "hostkey"),
		SessionDir:  dir,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.ln.Close() })
	return srv
}
