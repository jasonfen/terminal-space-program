package serve

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// awaySession registers the host's own session as silent for d, so the
// wrapper's next tick sees itself unattended.
func awaySession(t *testing.T, srv *Server, d time.Duration) {
	t.Helper()
	srv.live = newSessionRegistry()
	register(t, srv, sessiondir.HostFingerprint, time.Now().Add(-d))
}

// arrival stages a rendezvous arrival, the cheapest real local moment:
// the wrapper turns it into a session chip on its next tick.
func arrival(app *tui.App, handle string) {
	app.World().LastRendezvousArrival = &sim.RendezvousArrival{Handle: handle, Owner: fpB}
}

func kinds(w *sim.World) []sim.SessionEventKind {
	var out []sim.SessionEventKind
	for _, e := range w.SessionEvents {
		out = append(out, e.Kind)
	}
	return out
}

func has(w *sim.World, kind sim.SessionEventKind) bool {
	for _, k := range kinds(w) {
		if k == kind {
			return true
		}
	}
	return false
}

// Moments that fall while nobody is watching are banked, not rendered to
// an empty chair and aged out by a six-second TTL. Without this the coast
// a player committed to can complete, and every trace of it is gone
// before they open the lid.
func TestMomentsHeldWhileAway(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	awaySession(t, srv, 90*time.Second)
	m := srv.HostModel(app)
	arrival(app, "ansi")

	tick(m)

	if has(app.World(), sim.SessionEventRendezvousArrived) {
		t.Error("a moment was rendered to a session nobody is watching, where it will age out unseen")
	}
	held, _, ok := srv.mail.peek(sessiondir.HostFingerprint)
	if !ok || len(held) != 1 {
		t.Fatalf("held = %v (ok %v), want the arrival banked", held, ok)
	}
	if held[0].Kind != sim.SessionEventRendezvousArrived {
		t.Errorf("banked %v, want the rendezvous arrival", held[0].Kind)
	}
}

// And they are replayed when somebody is there to read them — re-stamped,
// because a moment that fell two hours ago is new to the player who just
// arrived and would otherwise be expired on delivery.
func TestHeldMomentsReplayedOnReturn(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	awaySession(t, srv, 90*time.Second)
	m := srv.HostModel(app)
	arrival(app, "ansi")
	tick(m)

	// They open the lid: the connection is answering again.
	awaySession(t, srv, 0)
	// Two hours of sim-time ran while they were gone.
	app.World().Clock.SimTime = app.World().Clock.SimTime.Add(2 * time.Hour)
	tick(m)

	w := app.World()
	if !has(w, sim.SessionEventResumed) {
		t.Errorf("no resumed chip: the player lands in a world whose clock jumped with no account of it\n%v", kinds(w))
	}
	if !has(w, sim.SessionEventRendezvousArrived) {
		t.Errorf("the banked arrival was never replayed\n%v", kinds(w))
	}
	for _, e := range w.SessionEvents {
		if e.Kind == sim.SessionEventResumed && e.Elapsed < 90*time.Minute {
			t.Errorf("resumed reports %v of unattended sim-time, want about 2h", e.Elapsed)
		}
	}
	// Drained, not re-delivered every tick from here on.
	tick(m)
	if has(app.World(), sim.SessionEventResumed) {
		t.Error("the resume replayed a second time — it is a moment, not a state")
	}
}

// A banked moment is hours old by the time anyone can read it, and the
// chip stack expires by wall clock. Delivered with its original
// timestamp it is trimmed on the very tick that replays it — the moment
// survives the whole away interval only to be dropped on arrival.
func TestReplayedMomentsAreRestamped(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	srv.mail.hold(sessiondir.HostFingerprint, simNoon, []sim.SessionEvent{
		{Kind: sim.SessionEventRendezvousArrived, Handle: "ansi", At: time.Now().Add(-2 * time.Hour)},
	})

	tick(srv.HostModel(app)) // present: nothing registered, so not away

	if !has(app.World(), sim.SessionEventRendezvousArrived) {
		t.Errorf("a two-hour-old banked moment did not survive its own replay\n%v", kinds(app.World()))
	}
}

// A session that really ended takes its bank with it. The player's next
// connection is an ordinary reconnect into a saved world, not a
// resumption of an interval they were present for.
func TestEndedSessionDropsItsBank(t *testing.T) {
	srv := newOfflineServer(t)
	srv.mail.hold(fpA, simNoon, []sim.SessionEvent{{Kind: sim.SessionEventRendezvousArrived}})

	srv.mail.drop(fpA)

	if _, _, ok := srv.mail.peek(fpA); ok {
		t.Error("a bank outlived its session; whoever connects on that key next replays a stale interval")
	}
}

// Hours away at a steady drip of moments must not grow without bound.
func TestHeldMomentsCapped(t *testing.T) {
	srv := newOfflineServer(t)
	for i := range heldMomentCap * 3 {
		srv.mail.hold(fpA, simNoon, []sim.SessionEvent{{Kind: sim.SessionEventCoWarpCoupled, Handle: string(rune('a' + i%26))}})
	}
	held, _, ok := srv.mail.peek(fpA)
	if !ok {
		t.Fatal("nothing held")
	}
	if len(held) > heldMomentCap {
		t.Errorf("held %d moments, want at most %d — an away session banks for hours", len(held), heldMomentCap)
	}
}

// The bank opens at the first moment the session is seen away and keeps
// that instant, so the elapsed readout measures the whole interval rather
// than restarting on every tick.
func TestBankKeepsItsOpeningInstant(t *testing.T) {
	srv := newOfflineServer(t)
	srv.mail.hold(fpA, simNoon, nil)
	srv.mail.hold(fpA, simNoon.Add(time.Hour), nil)

	_, since, ok := srv.mail.peek(fpA)
	if !ok {
		t.Fatal("nothing held")
	}
	if !since.Equal(simNoon) {
		t.Errorf("bank opened at %v, want the first sighting %v — a later tick reset the interval", since, simNoon)
	}
}
