package serve

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// eventsOf collects the presence moments of one kind addressed at `to`.
func eventsOf(srv *Server, kind sim.SessionEventKind, to string) []sim.SessionEvent {
	var out []sim.SessionEvent
	for _, e := range srv.presence.eventsFor(to) {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// Away is measured on a short leash. activityConn stamps on every frame
// the client drains, so an attended player is normally fresh within a
// second or two — the threshold only has to clear a briefly static
// screen, not the ten minutes the reap needs.
func TestIsAway(t *testing.T) {
	srv := newOfflineServer(t)
	cases := []struct {
		name     string
		lastIO   time.Duration // ago
		register bool
		want     bool
	}{
		{name: "drained a frame a moment ago", lastIO: time.Second, register: true, want: false},
		{name: "silent just under the threshold", lastIO: 55 * time.Second, register: true, want: false},
		{name: "silent past the threshold", lastIO: 90 * time.Second, register: true, want: true},
		{name: "silent for hours, held by a Commitment", lastIO: 3 * time.Hour, register: true, want: true},
		// Not registered at all is offline, not away: there is no session
		// simulating on their behalf.
		{name: "no session at all", register: false, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv.live = newSessionRegistry()
			if tc.register {
				register(t, srv, fpA, time.Now().Add(-tc.lastIO))
			}
			if got := srv.isAway(fpA); got != tc.want {
				t.Errorf("isAway = %v, want %v", got, tc.want)
			}
		})
	}
}

// The roster is what made this slice necessary: a reprieved session stays
// Online for hours, so without a distinct Away state the Session screen
// tells a partner their co-conspirator is at the controls when the chair
// is empty.
func TestRosterMarksAway(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, fpA, "vex")
	srv.presence.markOnline(fpA)
	register(t, srv, fpA, time.Now().Add(-90*time.Second))

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	tick(srv.HostModel(app))

	row := rosterRow(t, app.World(), "vex")
	if !row.Online {
		t.Error("an away player is not offline — their session is still simulating")
	}
	if !row.Away {
		t.Error("a session silent for 90s is not marked Away; the roster still claims someone is there")
	}
}

func TestRosterDoesNotMarkAnAttendedPlayerAway(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, fpA, "vex")
	srv.presence.markOnline(fpA)
	register(t, srv, fpA, time.Now())

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	tick(srv.HostModel(app))

	if rosterRow(t, app.World(), "vex").Away {
		t.Error("a player who just drained a frame was marked Away")
	}
}

func rosterRow(t *testing.T, w *sim.World, handle string) sim.SessionPlayer {
	t.Helper()
	if w.Session == nil {
		t.Fatal("no session slate after a tick")
	}
	for _, p := range w.Session.Players {
		if p.Handle == handle {
			return p
		}
	}
	t.Fatalf("no roster row for %q", handle)
	return sim.SessionPlayer{}
}

// The chip goes to the person it costs something: the partner mid-coast,
// not the whole roster. An uncommitted player going quiet chips nobody —
// the existing reap already announces them as left ten minutes later, and
// two chips for one event is noise.
func TestWentQuietChipsOnlyTheCommittedPartner(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, fpA, "vex")
	enrollDirect(t, srv, fpC, "kes")
	silent := time.Now().Add(-90 * time.Second)
	register(t, srv, fpA, silent) // committed with fpB
	register(t, srv, fpC, silent) // committed with nobody
	publish(srv, mutualArm(fpA, fpB, simNoon.Add(time.Hour)))

	srv.noteAway(time.Now())

	told := eventsOf(srv, sim.SessionEventWentQuiet, fpB)
	if len(told) != 1 {
		t.Fatalf("the committed partner got %d went-quiet chips, want 1", len(told))
	}
	if told[0].Handle != "vex" {
		t.Errorf("chip names %q, want vex", told[0].Handle)
	}
	if told[0].Detail != "rendezvous" {
		t.Errorf("chip detail = %q, want \"rendezvous\" — the partner should learn what is at stake", told[0].Detail)
	}
	if got := eventsOf(srv, sim.SessionEventWentQuiet, fpA); len(got) != 0 {
		t.Errorf("the away player was chipped about themselves (%d)", len(got))
	}
	// kes went quiet uncommitted: nobody is waiting on them.
	for _, viewer := range []string{fpA, fpB, fpC} {
		for _, e := range eventsOf(srv, sim.SessionEventWentQuiet, viewer) {
			if e.Handle == "kes" {
				t.Errorf("an uncommitted player going quiet chipped %q", viewer)
			}
		}
	}
}

// Going quiet is a transition, not a state. Sweeping every 30s for hours
// must not produce a chip every 30s for hours.
func TestWentQuietChipsOnce(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, fpA, "vex")
	register(t, srv, fpA, time.Now().Add(-90*time.Second))
	publish(srv, mutualArm(fpA, fpB, simNoon.Add(time.Hour)))

	for range 5 {
		srv.noteAway(time.Now())
	}
	if got := eventsOf(srv, sim.SessionEventWentQuiet, fpB); len(got) != 1 {
		t.Errorf("%d went-quiet chips after five sweeps, want 1", len(got))
	}
}

// The return must reach whoever was told about the departure, even if the
// Commitment has since resolved — otherwise a partner who saw "vex went
// quiet" never learns they came back.
func TestBackChipReachesWhoeverWasTold(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, fpA, "vex")
	register(t, srv, fpA, time.Now().Add(-90*time.Second))
	publish(srv, mutualArm(fpA, fpB, simNoon.Add(time.Hour)))
	srv.noteAway(time.Now())

	// The coast completes while they are away, and then they wake up.
	publish(srv, []relay.CraftReport{{Owner: fpA, SubspaceTime: simNoon}, {Owner: fpB, SubspaceTime: simNoon}})
	srv.live = newSessionRegistry()
	register(t, srv, fpA, time.Now())
	srv.noteAway(time.Now())

	back := eventsOf(srv, sim.SessionEventBack, fpB)
	if len(back) != 1 {
		t.Fatalf("the partner got %d back chips, want 1 — they were told about the departure", len(back))
	}
	if back[0].Handle != "vex" {
		t.Errorf("back chip names %q, want vex", back[0].Handle)
	}
}

// A session that dies while away did not leave — nobody came back for it.
// Saying "left" would blame a player for abandoning a coast they never
// touched, and the rendezvous cancel chip that follows compounds it.
func TestDepartureKindDistinguishesTimingOut(t *testing.T) {
	srv := newOfflineServer(t)
	cases := []struct {
		name   string
		lastIO time.Duration
		want   sim.SessionEventKind
	}{
		{name: "quit while sitting there", lastIO: time.Second, want: sim.SessionEventLeave},
		{name: "reaped after going silent", lastIO: 90 * time.Second, want: sim.SessionEventTimedOut},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv.live = newSessionRegistry()
			register(t, srv, fpA, time.Now().Add(-tc.lastIO))
			ls, _ := srv.live.get(fpA)
			if got := departureKind(ls, srv.awayAfter); got != tc.want {
				t.Errorf("departureKind = %v, want %v", got, tc.want)
			}
		})
	}
}

// A reclaim is one player resuming, not one leaving and another arriving.
// Mid-coast the leave chip reads as the rendezvous dying — the opposite of
// what a reclaim preserves.
func TestDisplacedSessionAnnouncesNeitherLeaveNorJoin(t *testing.T) {
	srv := newOfflineServer(t)
	registerWithDone(t, srv, fpA, time.Now().Add(-30*time.Minute))
	ls, _ := srv.live.get(fpA)

	if ls.displaced.Load() {
		t.Fatal("a fresh session is already flagged displaced")
	}
	srv.reclaimWait = 50 * time.Millisecond
	go func() {
		time.Sleep(10 * time.Millisecond)
		close(ls.done)
	}()
	if srv.displaceAbsent(fpA) != reclaimTookOver {
		t.Fatal("displaceAbsent refused an absent session")
	}
	if !ls.displaced.Load() {
		t.Error("the displaced session was not flagged, so its teardown will announce a departure " +
			"that reads as the coast dying")
	}
}
