package serve

import (
	"sync"
	"time"
)

// defaultReclaimWait bounds how long a reconnecting player waits for the
// session they are displacing to write its payload. Generous: the write
// is one small file behind a program teardown, and giving up costs the
// player a retry.
const defaultReclaimWait = 10 * time.Second

// reclaimResult is what a reconnecting connection made of the slot it
// found occupied.
type reclaimResult int

const (
	// reclaimRefused: somebody may still be at the controls. Leave them be.
	reclaimRefused reclaimResult = iota
	// reclaimTookOver: the old session is down and its payload is written.
	reclaimTookOver
	// reclaimStalled: the old session was torn down but its payload never
	// landed. The caller must not load the world, but the player is not
	// being told to disconnect something — it is already gone.
	reclaimStalled
)

// admission serialises connections arriving on the same key.
//
// Reclaim reads the registry, tears a session down and then waits up to
// reclaimWait for its payload — a window wide enough for a second
// connection on the same key to pass the same test and be admitted
// alongside the first, which is the divergent-World collision the
// one-session-per-key guard exists to prevent. Held from before the
// guard until after the winner has registered and marked itself online,
// so a latecomer either sees a live session or is refused by it.
//
// One mutex per fingerprint ever seen, never reclaimed: bounded by the
// roster, which is a handful of people.
type admission struct {
	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

func newAdmission() *admission { return &admission{locks: map[string]*sync.Mutex{}} }

// enter blocks until fp is free to admit, and returns the release.
func (a *admission) enter(fp string) func() {
	a.mu.Lock()
	l, ok := a.locks[fp]
	if !ok {
		l = &sync.Mutex{}
		a.locks[fp] = l
	}
	a.mu.Unlock()
	l.Lock()
	return l.Unlock
}

// displaceAbsent hands fp's slot to a reconnecting player (ADR 0036).
// Callers must already hold fp's admission.
//
// Without this the Reprieve reintroduces the very lockout the idle
// timeout was added to fix, and does it worst to the players most likely
// to want back in: the ones mid-encounter, whose lockout would last as
// long as the coast they committed to.
//
// Displacing is only safe because the session being displaced is known
// unattended. The test is that its peer has been silent for longer than
// the idle timeout, which means the deadline its own I/O path set has
// already passed — it is still alive only because the sweeper has been
// holding it open. A session whose peer spoke recently is somebody at the
// controls and is never displaced, so the second connection is refused
// exactly as it was before.
//
// The order is load-bearing: close the connection, then wait for the
// payload write, and only then let the caller load the world. `done` is
// closed at the very end of persistMiddleware, so a reclaiming session
// can never read a payload the displaced one is still about to
// overwrite.
func (s *Server) displaceAbsent(fp string) reclaimResult {
	sess, ok := s.live.get(fp)
	if !ok {
		// Presence says online but nothing is registered, so the absence
		// cannot be verified. Refuse rather than admit on faith — the host
		// plays in-process and is permanently online without ever holding a
		// session, and letting a key in there would fork its world.
		return reclaimRefused
	}
	if sess.conn == nil || time.Since(sess.conn.LastIO()) <= s.idleTimeout {
		return reclaimRefused
	}
	// Out of the registry first, so the sweeper stops reprieving something
	// that is on its way down. Flagged before the close so its teardown
	// reads the flag and stays quiet: the player is resuming, not leaving.
	sess.displaced.Store(true)
	s.live.remove(fp, sess)
	_ = sess.conn.Close()
	if sess.done == nil {
		return reclaimTookOver
	}
	select {
	case <-sess.done:
		return reclaimTookOver
	case <-time.After(s.reclaimWait):
		// Its payload never landed, so this connection must not load the
		// world. But the old session has already been closed and cannot come
		// back: clearing the flag lets its teardown announce the departure
		// and drop its banked interval like any other ending session, rather
		// than leaving a player disconnected, unannounced, and holding a
		// stale bank that would replay at their next connect.
		sess.displaced.Store(false)
		return reclaimStalled
	}
}
