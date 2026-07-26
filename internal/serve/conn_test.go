package serve

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/ssh"
	cssh "golang.org/x/crypto/ssh"
)

// stubConn is a net.Conn whose I/O results and deadline calls the test
// dictates. Methods the wrapper is not supposed to touch are inherited
// from the nil embedded interface and panic if it does — which is the
// point.
type stubConn struct {
	net.Conn
	readN, writeN     int
	readErr, writeErr error

	mu        sync.Mutex
	deadlines []time.Time
	closed    bool
}

func (c *stubConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *stubConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

func (c *stubConn) Read([]byte) (int, error)  { return c.readN, c.readErr }
func (c *stubConn) Write([]byte) (int, error) { return c.writeN, c.writeErr }

func (c *stubConn) SetDeadline(t time.Time) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deadlines = append(c.deadlines, t)
	return nil
}

func (c *stubConn) deadlinesSet() []time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Time(nil), c.deadlines...)
}

// fakeClock hands out instants the test controls — no sleeps.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// A Reprieve keeps an unattended session alive, which destroys the only
// other signal that its player is gone — the connection dying. What is
// left is this stamp, so it has to mean exactly one thing: the last
// instant bytes actually crossed the wire.
func TestActivityConnStampsIOThatMovedBytes(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		stub *stubConn
		do   func(*activityConn)
		want bool
	}{
		{
			name: "write reaches the peer",
			stub: &stubConn{writeN: 64},
			do:   func(c *activityConn) { _, _ = c.Write(make([]byte, 64)) },
			want: true,
		},
		{
			name: "read delivers input",
			stub: &stubConn{readN: 3},
			do:   func(c *activityConn) { _, _ = c.Read(make([]byte, 8)) },
			want: true,
		},
		{
			// The absent-peer case itself: the send buffer is full, the write
			// blocks and comes back empty. Stamping here would make a sleeping
			// laptop look present for as long as the server kept trying.
			name: "write times out having moved nothing",
			stub: &stubConn{writeErr: os.ErrDeadlineExceeded},
			do:   func(c *activityConn) { _, _ = c.Write(make([]byte, 64)) },
			want: false,
		},
		{
			name: "read fails having moved nothing",
			stub: &stubConn{readErr: io.EOF},
			do:   func(c *activityConn) { _, _ = c.Read(make([]byte, 8)) },
			want: false,
		},
		{
			// Bytes moving is the evidence, not the error being nil: the peer
			// was draining right up until it stopped.
			name: "partial write then error",
			stub: &stubConn{writeN: 12, writeErr: os.ErrDeadlineExceeded},
			do:   func(c *activityConn) { _, _ = c.Write(make([]byte, 64)) },
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := &fakeClock{now: start}
			c := newActivityConn(tc.stub, clk.Now)
			clk.advance(5 * time.Minute)
			tc.do(c)

			moved := !c.LastIO().Equal(start)
			if moved != tc.want {
				t.Errorf("LastIO advanced = %v, want %v (LastIO %v, opened at %v)",
					moved, tc.want, c.LastIO(), start)
			}
		})
	}
}

// A fresh connection is active. Leaving the stamp at the zero time would
// read as a peer absent since 1970, capping every new session's Reprieve
// the instant it opened.
func TestActivityConnOpensStamped(t *testing.T) {
	start := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	clk := &fakeClock{now: start}
	c := newActivityConn(&stubConn{}, clk.Now)
	if got := c.LastIO(); !got.Equal(start) {
		t.Errorf("LastIO = %v on a conn that has done no I/O, want its open instant %v", got, start)
	}
}

// The sweeper extends a Reprieve by calling SetDeadline on a connection
// whose Write is already blocked — that is the whole mechanism (ADR 0036,
// "there is no inline version of this feature"). A wrapper that shadowed
// or swallowed SetDeadline would silently cap every Reprieve at a single
// idle window, with nothing failing loudly.
func TestActivityConnPassesSetDeadlineThrough(t *testing.T) {
	stub := &stubConn{}
	c := newActivityConn(stub, time.Now)
	want := time.Date(2026, 7, 25, 14, 30, 0, 0, time.UTC)
	if err := c.SetDeadline(want); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	got := stub.deadlinesSet()
	if len(got) != 1 || !got[0].Equal(want) {
		t.Errorf("deadlines reaching the connection = %v, want exactly [%v]", got, want)
	}
}

// stubCtx is an ssh.Context carrying only what a callback needs: a value
// bag. The connection metadata accessors are never reached by the code
// under test.
type stubCtx struct {
	context.Context
	*sync.Mutex
	mu     sync.Mutex
	values map[any]any
}

func newStubCtx() *stubCtx {
	return &stubCtx{Context: context.Background(), Mutex: &sync.Mutex{}, values: map[any]any{}}
}

func (c *stubCtx) SetValue(key, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *stubCtx) Value(key any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if v, ok := c.values[key]; ok {
		return v
	}
	return c.Context.Value(key)
}

func (c *stubCtx) User() string                  { return "" }
func (c *stubCtx) SessionID() string             { return "" }
func (c *stubCtx) ClientVersion() string         { return "" }
func (c *stubCtx) ServerVersion() string         { return "" }
func (c *stubCtx) RemoteAddr() net.Addr          { return nil }
func (c *stubCtx) LocalAddr() net.Addr           { return nil }
func (c *stubCtx) Permissions() *ssh.Permissions { return nil }

// Every served connection must be wrapped, and the wrapper must land
// somewhere a session can reach it: ConnCallback is the only place that
// ever sees the raw net.Conn, and its ssh.Context is the same one the
// session later reads through sess.Context().
func TestServerWrapsConnsAndStashesThem(t *testing.T) {
	srv := newOfflineServer(t)
	if srv.ssh.ConnCallback == nil {
		t.Fatal("no ConnCallback — connections are unwrapped, so nothing records when a peer last spoke")
	}
	ctx := newStubCtx()
	stub := &stubConn{}

	wrapped := srv.ssh.ConnCallback(ctx, stub)

	ac, ok := wrapped.(*activityConn)
	if !ok {
		t.Fatalf("ConnCallback returned %T, want *activityConn", wrapped)
	}
	if got := sessionActivity(ctx); got != ac {
		t.Errorf("sessionActivity = %p, want the wrapper handed to the connection (%p)", got, ac)
	}
	// It must still be the same connection underneath — a wrapper that
	// dropped the conn would break every session, loudly.
	if _, err := ac.Write(nil); err != nil && !errors.Is(err, stub.writeErr) {
		t.Errorf("Write through the wrapper: %v", err)
	}
}

// The stash only works because the Context handed to ConnCallback is the
// same object a session later reads through sess.Context() — an ssh
// library property this design leans on, and one a dependency bump could
// quietly break. Pinned against the real server, over a real connection:
// the session appearing on the *captured connection's* Context is the
// proof, and the wrapper found there must be tracking live traffic.
func TestSessionContextCarriesItsConn(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: filepath.Join(t.TempDir(), "ssh_host_ed25519_key"),
		SessionDir:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Decorate the real callback rather than replacing it: the production
	// wrapping still happens, the test just keeps a handle on the Context.
	inner := srv.ssh.ConnCallback
	if inner == nil {
		t.Fatal("no ConnCallback — connections are unwrapped, so no session can find its own")
	}
	ctxs := make(chan ssh.Context, 4)
	srv.ssh.ConnCallback = func(ctx ssh.Context, conn net.Conn) net.Conn {
		ctxs <- ctx
		return inner(ctx, conn)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() { stopServer(t, srv, done) })

	signer, fp := newClientKey(t)
	enrollDirect(t, srv, fp, "vex")
	client, err := cssh.Dial("tcp", srv.Addr(), &cssh.ClientConfig{
		User:            "guest",
		Auth:            []cssh.AuthMethod{cssh.PublicKeys(signer)},
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = client.Close() }()
	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	if err := sess.RequestPty("xterm-256color", 30, 120, cssh.TerminalModes{}); err != nil {
		t.Fatalf("pty: %v", err)
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	go func() { _, _ = io.Copy(io.Discard, stdout) }() // a present peer drains
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}

	var ctx ssh.Context
	select {
	case ctx = <-ctxs:
	case <-time.After(5 * time.Second):
		t.Fatal("ConnCallback never fired for a real connection")
	}
	deadline := time.Now().Add(10 * time.Second)
	for ctx.Value(ssh.ContextKeySession) == nil {
		if time.Now().After(deadline) {
			t.Fatal("no session ever appeared on the connection's Context — " +
				"sess.Context() is no longer the Context ConnCallback saw, so a " +
				"session can no longer find its own connection")
		}
		time.Sleep(25 * time.Millisecond)
	}
	ac := sessionActivity(ctx)
	if ac == nil {
		t.Fatal("no activityConn on the session's Context")
	}
	// And it is tracking this connection's real traffic, not sitting at its
	// open instant: the session is rendering frames the client is draining.
	opened := ac.LastIO()
	for time.Now().Before(deadline) {
		if ac.LastIO().After(opened) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Errorf("LastIO stuck at %v while the session streamed frames — the wrapper is not on the I/O path", opened)
}
