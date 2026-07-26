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

// Moments that fall while nobody is watching are banked rather than left
// to age out against a six-second TTL. Without this the coast a player
// committed to can complete, and every trace of it is gone before they
// open the lid.
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
	// An hour of away time passes with it banked — long enough that it is
	// no longer on anyone's screen, so the replay is its only delivery.
	held, _, _ := srv.mail.peek(sessiondir.HostFingerprint)
	held[0].At = time.Now().Add(-time.Hour)

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
	register(t, srv, sessiondir.HostFingerprint, time.Now()) // live, and answering

	tick(srv.HostModel(app))

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

// Review finding 1. displaceAbsent pulls the registry entry at the START
// of a reclaim, and closing the connection is exactly what unblocks the
// displaced session's loop — so it reliably ticks again before it exits.
// Judged only on "is this session away", those ticks read as present,
// take the replay branch, and drain the bank into a slice discarded
// seconds later. The returning player then gets nothing, on the one path
// the whole slice exists for.
func TestDisplacedSessionDoesNotDrainTheBank(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	awaySession(t, srv, 90*time.Second)
	m := srv.HostModel(app)
	arrival(app, "ansi")
	tick(m)
	if _, _, ok := srv.mail.peek(sessiondir.HostFingerprint); !ok {
		t.Fatal("nothing banked; the setup is wrong")
	}

	// Exactly what a reclaim does, in order.
	ls, _ := srv.live.get(sessiondir.HostFingerprint)
	ls.displaced.Store(true)
	srv.live.remove(sessiondir.HostFingerprint, ls)

	tick(m) // the dying session, still running

	if _, _, ok := srv.mail.peek(sessiondir.HostFingerprint); !ok {
		t.Fatal("the displaced session drained the bank meant for the connection replacing it — " +
			"the returning player lands in a jumped world with no account of it")
	}
	if has(app.World(), sim.SessionEventResumed) {
		t.Error("the displaced session rendered a resume to a screen nobody will ever see again")
	}
}

// Review finding 4. isAway keys on frames drained, so a player who pauses
// on a static screen reads as away while sitting right there. Moving
// their moments into the bank would take chips off the screen of someone
// watching; a copy costs nothing, which is what the short away threshold
// assumes.
func TestMomentsStillRenderForAMisclassifiedPlayer(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	awaySession(t, srv, 90*time.Second)
	m := srv.HostModel(app)
	arrival(app, "ansi")

	tick(m)

	if !has(app.World(), sim.SessionEventRendezvousArrived) {
		t.Error("a moment was taken off the canvas of a player who may be sitting right there")
	}
	if _, _, ok := srv.mail.peek(sessiondir.HostFingerprint); !ok {
		t.Error("nothing banked — a genuinely away player would lose it")
	}
}

// And the copy must not come back as a duplicate: a moment still inside
// its TTL was on screen a moment ago.
func TestReplaySkipsMomentsStillOnScreen(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	awaySession(t, srv, 90*time.Second)
	m := srv.HostModel(app)
	arrival(app, "ansi")
	tick(m) // seen live, and banked

	awaySession(t, srv, 0) // they were there all along; the screen woke up
	tick(m)

	var arrivals int
	for _, e := range app.World().SessionEvents {
		if e.Kind == sim.SessionEventRendezvousArrived {
			arrivals++
		}
	}
	if arrivals > 1 {
		t.Errorf("the same moment rendered %d times — the banked copy came back on top of the one already read", arrivals)
	}
	if has(app.World(), sim.SessionEventResumed) {
		t.Error("a player who never really left was told they resumed")
	}
}

// Review finding 7. Every replayed moment is re-stamped to the same
// instant, so a full bank renders as one block for the whole chip TTL —
// more rows than a terminal has, burying the orbit view it explains.
func TestReplayIsBounded(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	for i := range heldMomentCap {
		srv.mail.hold(sessiondir.HostFingerprint, simNoon, []sim.SessionEvent{
			{Kind: sim.SessionEventCoWarpCoupled, Handle: string(rune('a' + i%26)), At: old},
		})
	}
	register(t, srv, sessiondir.HostFingerprint, time.Now())
	app.World().Clock.SimTime = simNoon.Add(2 * time.Hour)

	tick(srv.HostModel(app))

	replayed := 0
	var resumed sim.SessionEvent
	for _, e := range app.World().SessionEvents {
		switch e.Kind {
		case sim.SessionEventCoWarpCoupled:
			replayed++
		case sim.SessionEventResumed:
			resumed = e
		}
	}
	if replayed > maxReplayedMoments {
		t.Errorf("%d moments replayed at once, want at most %d", replayed, maxReplayedMoments)
	}
	if resumed.Detail == "" {
		t.Error("the dropped moments were not accounted for — the replay silently truncated")
	}
}

// A brief pause is not an interval worth announcing.
func TestTrivialAwayIntervalIsNotAnnounced(t *testing.T) {
	srv := newOfflineServer(t)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	srv.mail.hold(sessiondir.HostFingerprint, simNoon, nil)
	register(t, srv, sessiondir.HostFingerprint, time.Now())
	app.World().Clock.SimTime = simNoon.Add(5 * time.Second)

	tick(srv.HostModel(app))

	if has(app.World(), sim.SessionEventResumed) {
		t.Error("\"resumed — 5s ran while you were away\" is noise about nothing")
	}
}
