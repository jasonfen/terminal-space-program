package serve

import (
	"sync"
	"time"
)

// Sweeper cadence (ADR 0036).
const (
	// defaultSweepEvery is how often the sweeper looks for Commitments.
	defaultSweepEvery = 30 * time.Second

	// defaultReprieveWindow is how far ahead a sweep pushes a reprieved
	// connection's deadline. It must exceed the sweep interval, or a
	// Reprieve would lapse in the gap between two sweeps. Short on purpose:
	// a Reprieve is released by ceasing to extend, so this window is also
	// the tail of unattended simulation after a Commitment resolves.
	defaultReprieveWindow = 2 * time.Minute
)

// liveSession is a connected player's session as seen from outside its
// own goroutine. It carries only what the sweeper may safely touch —
// never a *sim.World, which belongs to the session's tick loop alone
// (internal/relay/reporter.go:35).
type liveSession struct {
	conn *activityConn
}

// sessionRegistry tracks live sessions by fingerprint so the sweeper can
// find the connection behind a Commitment.
type sessionRegistry struct {
	mu   sync.Mutex
	live map[string]*liveSession
}

func newSessionRegistry() *sessionRegistry {
	return &sessionRegistry{live: map[string]*liveSession{}}
}

func (r *sessionRegistry) add(fp string, s *liveSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.live[fp] = s
}

// remove drops fp's entry only if it is still s. A key may hold one live
// session, but a connection refused for that reason still unwinds through
// the same teardown — matching on identity keeps it from evicting the
// session that beat it.
func (r *sessionRegistry) remove(fp string, s *liveSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.live[fp] == s {
		delete(r.live, fp)
	}
}

// snapshot copies the registry so the caller can act on it without
// holding the lock — the copy-then-act idiom relay.Report already uses.
func (r *sessionRegistry) snapshot() map[string]*liveSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]*liveSession, len(r.live))
	for fp, s := range r.live {
		out[fp] = s
	}
	return out
}

// sweepReprieves extends the Reprieve of every committed session until
// stop closes. One goroutine server-wide, started in Serve and stopped on
// return, mirroring s.ver.watch.
//
// It exists because there is no inline version of this feature.
// serverConn.updateDeadline runs at the *start* of Write; then Write
// blocks against the absent peer and nothing on the I/O path runs again,
// so whatever deadline was set last is final. Extending a Reprieve past a
// single idle window requires an outside goroutine calling SetDeadline —
// which is safe concurrently and does affect an already-blocked write.
// Deleting this loop does not merely lose an optimisation: it silently
// caps every Reprieve at one window.
func (s *Server) sweepReprieves(stop <-chan struct{}) {
	tick := time.NewTicker(s.sweepEvery)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-tick.C:
			s.extendReprieves(now)
		}
	}
}

// extendReprieves is one sweep: find the sessions holding a Commitment
// and push their connection deadlines out.
//
// Everything it reads is a snapshot taken under someone else's lock —
// the relay store, the dock ledger, the session registry — and every
// SetDeadline happens outside all of them.
func (s *Server) extendReprieves(now time.Time) {
	live := s.live.snapshot()
	if len(live) == 0 {
		return
	}
	reports := s.relay.Snapshot("") // exclude nobody: every session is a candidate
	docks := s.dock.Records()
	for fp, sess := range live {
		if sess.conn == nil {
			continue // no way to measure this peer's absence, so no Reprieve to grant
		}
		c, ok := commitmentFor(reports, docks, fp)
		if !ok {
			continue // nothing is waiting on this session
		}
		lastIO := sess.conn.LastIO()
		if !now.Before(c.expiry(now, lastIO)) {
			// Capped out. Stop extending and let the standing deadline fire:
			// never force-expire, so exactly one code path writes deadlines
			// and there is no ordering race against teardown.
			continue
		}
		deadline := now.Add(s.reprieveWindow)
		// Never pull a deadline in. The reprieve window is far shorter than
		// the idle timeout, so a bare now+window would cut short a player who
		// is present but rendering nothing — a paused screen produces no
		// frames. lastIO+idleTimeout reconstructs the deadline this session's
		// own I/O path would have set.
		if natural := lastIO.Add(s.idleTimeout); natural.After(deadline) {
			deadline = natural
		}
		_ = sess.conn.SetDeadline(deadline)
	}
}
