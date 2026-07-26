package serve

import (
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// defaultAwayAfter is how long a session's peer must be silent before the
// session reads as Away (ADR 0036).
//
// Far shorter than the idle timeout on purpose. activityConn stamps on
// every frame the client drains, so an attended player is normally fresh
// within a second or two; this only has to clear a briefly static screen,
// not the ten minutes a reap needs. The asymmetry with the reclaim test
// is deliberate: this drives display, where being wrong for a moment
// costs nothing and self-corrects on the player's next frame, while
// displacing a session is destructive and keeps the strict test.
const defaultAwayAfter = 60 * time.Second

// isAway reports whether fp's session is still simulating with nobody at
// the controls. False for a player with no session at all — that is
// offline, which is a different thing: an Away player's craft keep
// flying and keep whatever Commitment earned them a Reprieve.
func (s *Server) isAway(fp string) bool {
	sess, ok := s.live.get(fp)
	if !ok || sess.conn == nil {
		return false
	}
	return time.Since(sess.conn.LastIO()) > s.awayAfter
}

// awayWatch remembers who has been told that a player went quiet, so the
// transition chips exactly once however long the silence lasts, and the
// return reaches the same person.
type awayWatch struct {
	mu   sync.Mutex
	told map[string]string // away fingerprint → the partner told about it
}

func newAwayWatch() *awayWatch { return &awayWatch{told: map[string]string{}} }

func (a *awayWatch) partner(fp string) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p, ok := a.told[fp]
	return p, ok
}

func (a *awayWatch) set(fp, partner string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.told[fp] = partner
}

func (a *awayWatch) clear(fp string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.told, fp)
}

// noteAway turns this sweep's away states into transitions, and chips the
// player each one costs something: the partner mid-coast, not the whole
// roster. An uncommitted player going quiet chips nobody — the reap
// already announces them as left, and two chips for one event is noise.
//
// Runs in the sweeper goroutine beside extendReprieves, on the same
// snapshots, and touches no *sim.World.
//
// Records are never cleaned up from here. A reclaim leaves the
// fingerprint out of the registry for the whole teardown-and-payload
// window, so a sweep that dropped absent fingerprints would delete the
// record the returning session needs and the partner who was told
// "went quiet" would never hear it closed. Ending sessions clear their
// own record in persistMiddleware instead.
func (s *Server) noteAway(now time.Time) {
	live := s.live.snapshot()
	if len(live) == 0 {
		return
	}
	reports := s.relay.Snapshot("")
	docks := s.dock.Records()
	for fp, sess := range live {
		away := sess.conn != nil && now.Sub(sess.conn.LastIO()) > s.awayAfter
		partner, told := s.away.partner(fp)
		switch {
		case away && !told:
			c, ok := commitmentFor(reports, docks, fp)
			if !ok {
				continue // nobody is waiting on this session
			}
			s.away.set(fp, c.peer)
			s.presence.eventWith(sim.SessionEventWentQuiet, fp, s.handleOf(fp), c.peer, c.kind.String())
		case !away && told:
			// Whoever was told about the departure hears about the return,
			// even if the Commitment resolved while they were gone —
			// otherwise the chip that opened the story never closes.
			s.away.clear(fp)
			s.presence.event(sim.SessionEventBack, fp, s.handleOf(fp), partner)
		}
	}
}

// handleOf resolves a fingerprint to its roster display name.
func (s *Server) handleOf(fp string) string {
	if p, err := s.store.FindPlayer(fp); err == nil {
		return p.Handle
	}
	return ""
}

// departureKind names what happened to a session that is ending. One that
// was away did not leave — nobody came back for it. Calling that "left"
// blames a player for abandoning a coast they never touched, and the
// rendezvous cancel chip that follows compounds it.
func departureKind(sess *liveSession, awayAfter time.Duration) sim.SessionEventKind {
	if sess == nil || sess.conn == nil {
		return sim.SessionEventLeave
	}
	if time.Since(sess.conn.LastIO()) > awayAfter {
		return sim.SessionEventTimedOut
	}
	return sim.SessionEventLeave
}
