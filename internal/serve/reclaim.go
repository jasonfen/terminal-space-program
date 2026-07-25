package serve

import "time"

// defaultReclaimWait bounds how long a reconnecting player waits for the
// session they are displacing to write its payload. Generous: the write
// is one small file behind a program teardown, and giving up costs the
// player a retry.
const defaultReclaimWait = 10 * time.Second

// displaceAbsent hands fp's slot to a reconnecting player, and reports
// whether it managed to (ADR 0036).
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
func (s *Server) displaceAbsent(fp string) bool {
	sess, ok := s.live.get(fp)
	if !ok {
		// Presence says online but nothing is registered, so the absence
		// cannot be verified. Refuse rather than admit on faith — the host
		// plays in-process and is permanently online without ever holding a
		// session, and letting a key in there would fork its world.
		return false
	}
	if sess.conn == nil || time.Since(sess.conn.LastIO()) <= s.idleTimeout {
		return false
	}
	// Out of the registry first, so the sweeper stops reprieving something
	// that is on its way down.
	s.live.remove(fp, sess)
	_ = sess.conn.Close()
	if sess.done == nil {
		return true
	}
	select {
	case <-sess.done:
		return true
	case <-time.After(s.reclaimWait):
		// Its payload never landed. Being told to try again is recoverable;
		// silently resuming a world that is about to be overwritten is not.
		return false
	}
}
