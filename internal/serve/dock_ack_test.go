package serve

import (
	"reflect"
	"testing"

	"github.com/charmbracelet/ssh"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// ADR 0040 amendment (10a, slice A1): the serve layer persists the new
// acknowledgement fields and acknowledges a seat when its World is written.

func settledFor(guestFP string) relay.DockSnapshot {
	return relay.DockSnapshot{
		ID: 4, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 1,
		GuestOwner: guestFP, GuestHandle: "gern", GuestCraftID: 2,
		Phase: relay.DockActive, Done: true,
		GuestDirty: true, GuestCopy: &save.Craft{ID: 2, Name: "x"},
	}
}

func TestDockAckStateSurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	srv := serverOver(t, dir)
	live := relay.DockSnapshot{
		ID: 9, Owner: sessiondir.HostFingerprint, OwnerHandle: "host",
		DockerCraftID: 1, CompositeID: 50,
		GuestOwner: "SHA256:gern", GuestHandle: "gern", GuestCraftID: 2,
		Phase:      relay.DockActive,
		OwnerDirty: true, OwnerCopy: &save.Craft{ID: 2, Name: "guest vessel"},
		GuestDirty: true, FromCompositeID: 33,
	}
	srv.dock.SeedFull([]relay.DockSnapshot{live, settledFor("SHA256:gern2")}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("persistDocks: %v", err)
	}
	want := srv.dock.FullRecords()

	got := serverOver(t, dir).dock.FullRecords()
	byID := func(rs []relay.DockSnapshot) map[uint64]relay.DockSnapshot {
		m := map[uint64]relay.DockSnapshot{}
		for _, r := range rs {
			m[r.ID] = r
		}
		return m
	}
	if len(got) != 2 || !reflect.DeepEqual(byID(got), byID(want)) {
		t.Errorf("ack state lost across a restart:\n got %+v\nwant %+v", got, want)
	}
}

// The guest sink (autosave, quit, admin restart all go through it) is the
// acknowledgement: a successful save clears the mark and, with the record
// settled and the owner clean, the record goes, on disk too.
func TestGuestSinkAcknowledgesOnSave(t *testing.T) {
	const guestFP = "SHA256:gern"
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")
	srv.dock.SeedFull([]relay.DockSnapshot{settledFor(guestFP)}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("persistDocks: %v", err)
	}

	app, err := srv.newGuestApp(guestFP)
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	if err := app.PersistNow(); err != nil {
		t.Fatalf("PersistNow: %v", err)
	}
	if n := len(srv.dock.FullRecords()); n != 0 {
		t.Errorf("settled record still held after the guest saved: %d", n)
	}
	if n := len(serverOver(t, dir).dock.FullRecords()); n != 0 {
		t.Errorf("the acknowledgement was not flushed to session.json: %d records after a restart", n)
	}
}

// persistMiddleware's final SavePlayer is the other acknowledgement.
func TestFinalSaveAcknowledges(t *testing.T) {
	const guestFP = "SHA256:gern"
	dir := t.TempDir()
	srv := serverOver(t, dir)
	enrollDirect(t, srv, guestFP, "gern")
	srv.dock.SeedFull([]relay.DockSnapshot{settledFor(guestFP)}, nil)
	if err := srv.persistDocks(); err != nil {
		t.Fatalf("persistDocks: %v", err)
	}
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	ctx := &hookCtx{vals: map[any]any{
		ctxKeyFingerprint: guestFP, ctxKeyJoined: true, ctxKeyApp: app,
	}}
	srv.persistMiddleware(func(ssh.Session) {})(hookSess{ctx: ctx})

	if n := len(srv.dock.FullRecords()); n != 0 {
		t.Errorf("settled record still held after the final save: %d", n)
	}
	if n := len(serverOver(t, dir).dock.FullRecords()); n != 0 {
		t.Errorf("the acknowledgement was not flushed to session.json: %d records after a restart", n)
	}
}
