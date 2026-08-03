package serve

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// TestTransferToAbsentPartnerIsRefusedAndSaidOutLoud (ADR 0040 §2): the
// press is refused at the seam AND the seat is told why. The measured
// failure mode this guards is not the refusal, it is the silence — a
// bound key that does nothing reads as a broken game (#308).
func TestTransferToAbsentPartnerIsRefusedAndSaidOutLoud(t *testing.T) {
	const guestFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, guestFP, "gern")

	// A live dock the host owns, with the guest NOT connected.
	srv.dock.SeedFull([]relay.DockSnapshot{{
		ID: 1, Owner: sessiondir.HostFingerprint, OwnerHandle: "jason",
		DockerCraftID: 1, CompositeID: 1,
		GuestOwner: guestFP, GuestHandle: "gern", GuestCraftID: 200,
		Phase: relay.DockActive,
	}}, nil)
	if srv.presence.isOnline(guestFP) {
		t.Fatalf("test setup: the guest must be offline for this gate to bite")
	}

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var model tea.Model = srv.HostModel(app)
	model, _ = model.Update(tui.TransferControlMsg{})

	rm, ok := model.(reportingModel)
	if !ok {
		t.Fatalf("wrapper model = %T", model)
	}
	var told string
	for _, e := range rm.localEvents {
		if e.Kind == sim.SessionEventTransferRefused {
			told = e.Detail
		}
	}
	if told == "" {
		t.Fatalf("[J] to an absent partner produced no moment: %+v", rm.localEvents)
	}
	if !strings.Contains(told, "gern") {
		t.Errorf("refusal %q does not name who isn't there", told)
	}

	// Nothing was armed: a reconcile must not migrate the stack.
	w := app.World()
	before := len(w.Crafts)
	srv.dock.Reconcile(w, sessiondir.HostFingerprint, nil)
	if len(w.Crafts) != before {
		t.Errorf("a refused transfer still moved craft: %d → %d", before, len(w.Crafts))
	}
}
