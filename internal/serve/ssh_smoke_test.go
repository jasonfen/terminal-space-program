package serve

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	cssh "golang.org/x/crypto/ssh"
)

// The S1 ssh smoke test (v0.27 plan): in-test wish server, real
// x/crypto/ssh client. Key auth accepted, pty granted, first rendered
// frame is the game, WindowChange reflows, and two concurrent
// connections run divergent Worlds — all under -race.
func TestSSHSmoke(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	keyPath := filepath.Join(t.TempDir(), "ssh_host_ed25519_key")

	srv, err := New(Config{Addr: "127.0.0.1:0", HostKeyPath: keyPath})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		if err := <-done; err != nil {
			t.Errorf("Serve returned: %v", err)
		}
	})

	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("host key not generated at %s: %v", keyPath, err)
	}

	// Two concurrent sessions — independent Worlds over real ssh.
	sessA := dialGameSession(t, srv.Addr())
	sessB := dialGameSession(t, srv.Addr())

	// First frame: the orbit screen (system name in the header chip).
	sessA.waitFor(t, "Sol")
	sessB.waitFor(t, "Sol")

	// Warp up session A only; B's clock chip must stay at 1x.
	if _, err := sessA.stdin.Write([]byte(".")); err != nil {
		t.Fatalf("write warp key: %v", err)
	}
	sessA.waitFor(t, "warp 10x")
	if strings.Contains(stripANSI(sessB.output()), "warp 10x") {
		t.Error("session B rendered session A's warp — Worlds are not independent")
	}

	// WindowChange reflows: widen A's pty and expect wider frame rows.
	if err := sessA.sess.WindowChange(45, 180); err != nil {
		t.Fatalf("WindowChange: %v", err)
	}
	sessA.waitUntil(t, "reflow to 180 cols", func(out string) bool {
		return maxLineWidth(stripANSI(out)) > 150
	})
}

// gameSession is one live ssh connection running the game, with its
// stdout captured for frame assertions.
type gameSession struct {
	sess  *cssh.Session
	stdin interface{ Write([]byte) (int, error) }

	mu  sync.Mutex
	buf strings.Builder
}

func (g *gameSession) output() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.buf.String()
}

// waitFor polls the ANSI-stripped output until it contains needle.
func (g *gameSession) waitFor(t *testing.T, needle string) {
	t.Helper()
	g.waitUntil(t, "output containing "+needle, func(out string) bool {
		return strings.Contains(stripANSI(out), needle)
	})
}

func (g *gameSession) waitUntil(t *testing.T, what string, pred func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if pred(g.output()) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s; last frame tail:\n%s", what, tail(stripANSI(g.output()), 400))
}

// dialGameSession connects a fresh ed25519 client key, requests a pty,
// and starts the remote game shell, capturing stdout.
func dialGameSession(t *testing.T, addr string) *gameSession {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate client key: %v", err)
	}
	signer, err := cssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("client signer: %v", err)
	}
	client, err := cssh.Dial("tcp", addr, &cssh.ClientConfig{
		User:            "smoke",
		Auth:            []cssh.AuthMethod{cssh.PublicKeys(signer)},
		HostKeyCallback: cssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh dial %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = client.Close() })

	sess, err := client.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	g := &gameSession{sess: sess}
	stdin, err := sess.StdinPipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	g.stdin = stdin
	stdout, err := sess.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	go func() {
		chunk := make([]byte, 4096)
		for {
			n, err := stdout.Read(chunk)
			if n > 0 {
				g.mu.Lock()
				g.buf.Write(chunk[:n])
				g.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	if err := sess.RequestPty("xterm-256color", 30, 120, cssh.TerminalModes{}); err != nil {
		t.Fatalf("request pty: %v", err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}
	return g
}

func maxLineWidth(s string) int {
	max := 0
	for _, line := range strings.Split(s, "\n") {
		if n := len([]rune(strings.TrimRight(line, "\r"))); n > max {
			max = n
		}
	}
	return max
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
