package serve

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	cssh "golang.org/x/crypto/ssh"
)

// #243: a guest's game runs server-side on its own tick loop, so a client
// whose machine sleeps does not stop the session — it leaves a half-open
// TCP that nothing reaps. The session kept running unattended, presence
// kept counting it, and its owner stayed locked out behind it.

// waitPresence blocks until fp's online state matches want, or fails.
func waitPresence(t *testing.T, srv *Server, fp string, want bool, timeout time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if srv.presence.isOnline(fp) == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestIdleTimeoutConfigured(t *testing.T) {
	srv := newOfflineServer(t)
	if srv.ssh.IdleTimeout != DefaultIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v — without it a dead peer's connection never dies",
			srv.ssh.IdleTimeout, DefaultIdleTimeout)
	}
	// No absolute cap: that would disconnect players who are present.
	if srv.ssh.MaxTimeout != 0 {
		t.Errorf("MaxTimeout = %v, want unset — an absolute cap would evict active players",
			srv.ssh.MaxTimeout)
	}
}

func TestIdleTimeoutOverridable(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: filepath.Join(t.TempDir(), "hostkey"),
		SessionDir:  t.TempDir(),
		IdleTimeout: 42 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = srv.ln.Close() }()
	if srv.ssh.IdleTimeout != 42*time.Second {
		t.Errorf("IdleTimeout = %v, want the configured 42s", srv.ssh.IdleTimeout)
	}
}

// The behaviour that matters: a peer that stops reading is eventually
// dropped, freeing its slot. Simulated by completing the SSH handshake
// and then never draining stdout — the server's frames back up, its
// write blocks, and the deadline tears the connection down. That is the
// same path a sleeping laptop takes.
func TestAbsentPeerIsReaped(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	keyPath := filepath.Join(t.TempDir(), "ssh_host_ed25519_key")
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		HostKeyPath: keyPath,
		SessionDir:  t.TempDir(),
		IdleTimeout: 750 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("host key: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve() }()
	t.Cleanup(func() { _ = srv.ln.Close(); <-done })

	signer, fp := newClientKey(t)
	enrollDirect(t, srv, fp, "gern")

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
	// Never read stdout, never write stdin: from here on this client is
	// indistinguishable from one whose machine went to sleep.
	if _, err := sess.StdoutPipe(); err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	if err := sess.Shell(); err != nil {
		t.Fatalf("shell: %v", err)
	}

	// Presence is the race-free observable here: handlers mutate it under
	// its own mutex, and it is exactly what the lockout depends on. (Not
	// srv.sessions.Wait() — Wait racing a starting session's Add is
	// WaitGroup misuse, which the race detector rightly flags.)
	waitPresence(t, srv, fp, true, 10*time.Second,
		"the session never came online, so there is nothing to reap")
	waitPresence(t, srv, fp, false, 30*time.Second,
		"an absent peer's session was never reaped — it would hold its slot "+
			"and keep running unattended indefinitely")
}
