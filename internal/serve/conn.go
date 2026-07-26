package serve

import (
	"net"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/ssh"
)

// activityConn wraps a session's connection and records the instant it
// last actually moved bytes (ADR 0036, Commitment Reprieve).
//
// A Reprieve keeps an unattended Session alive past the idle timeout,
// which destroys the only other signal that its player has gone away:
// the connection dying. This stamp is what remains. It is what makes the
// Reprieve's cap measurable at all — the cap is counted from when the
// peer went quiet, and nothing else can say when that was — and it is
// the only thing that distinguishes *absent, coasting unattended* from
// *present, on a legitimately long coast*, since a player watching a
// two-hour burn touches no keys but their terminal keeps draining
// frames. So it cannot be dropped as an optimisation.
//
// Bytes moving is the evidence, not an I/O call returning without error:
// the absent-peer case is precisely a write that blocks on a full send
// buffer and comes back having written nothing. A write that moved some
// bytes and *then* failed still proves the peer was draining until it
// stopped. (A write only reaches the kernel's send buffer, not the peer's
// screen — but backpressure from an unacknowledged buffer is exactly the
// signal the idle timeout itself keys on, so the two agree.)
//
// Read lock-free by the sweeper goroutine: a plain atomic, because this
// is written on the I/O path of every session and read from outside it.
//
// Deliberately no SetDeadline or Close of its own. The sweeper extends a
// Reprieve by calling SetDeadline on a connection whose Write is already
// blocked — that call is the entire mechanism (ADR 0036, "there is no
// inline version of this feature"), and a wrapper that shadowed or
// swallowed it would silently cap every Reprieve at one idle window.
type activityConn struct {
	net.Conn

	now    func() time.Time
	lastIO atomic.Int64 // unix nanos
}

// newActivityConn wraps c, taking its instants from now (injected so the
// sweeper's accounting is testable without sleeping).
func newActivityConn(c net.Conn, now func() time.Time) *activityConn {
	ac := &activityConn{Conn: c, now: now}
	// A fresh connection is active. Leaving the stamp at zero would read
	// as a peer absent since 1970 and cap a Reprieve the instant it opened.
	ac.stamp()
	return ac
}

func (c *activityConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.stamp()
	}
	return n, err
}

func (c *activityConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	if n > 0 {
		c.stamp()
	}
	return n, err
}

func (c *activityConn) stamp() { c.lastIO.Store(c.now().UnixNano()) }

// LastIO reports when the connection last moved bytes in either
// direction — the measure of how long this session's player has been
// silent.
func (c *activityConn) LastIO() time.Time { return time.Unix(0, c.lastIO.Load()) }

// sessionActivity returns the wrapper for the connection behind ctx —
// stashed there by the ConnCallback in New, which is the only place the
// raw net.Conn is ever visible. A session reads it through the same
// Context via sess.Context(). Nil if the server was built without the
// callback.
func sessionActivity(ctx ssh.Context) *activityConn {
	ac, _ := ctx.Value(ctxKeyConn).(*activityConn)
	return ac
}

// sessionDone returns the channel persistMiddleware closes once this
// session's payload write has landed — what a reclaiming connection
// waits on before loading the world.
func sessionDone(ctx ssh.Context) chan struct{} {
	done, _ := ctx.Value(ctxKeyDone).(chan struct{})
	return done
}
