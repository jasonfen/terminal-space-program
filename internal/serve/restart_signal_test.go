package serve

import (
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// Audit item 11 / F6: a SIGTERM restart drained the guests and flushed the
// dock ledger but never wrote the host's own in-process world, which only
// the host's tick goroutine may read. The host must save it on a tick during
// the announce grace, before the process exits.
func TestSigtermSavesHostWorldBeforeExit(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := serverOver(t, t.TempDir())

	grace := make(chan time.Time)
	srv.after = func(time.Duration) <-chan time.Time { return grace }
	exited := make(chan int, 1)
	oldExit := exitFunc
	exitFunc = func(code int) { exited <- code }
	t.Cleanup(func() { exitFunc = oldExit })

	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	host := tick(srv.HostModel(app))

	dir, err := save.SavesDir()
	if err != nil {
		t.Fatalf("SavesDir: %v", err)
	}
	// The ticking host app's Flight Recording lives in saves/recordings; it is not a save.
	saved := func() (n int) {
		e, _ := os.ReadDir(dir)
		for _, f := range e {
			if f.Name() != "recordings" {
				n++
			}
		}
		return
	}
	if saved() != 0 {
		t.Fatalf("test setup: saves already exist in %s", dir)
	}

	sig := make(chan os.Signal, 1)
	go srv.restartOnSignal(sig, make(chan struct{}))
	sig <- syscall.SIGTERM

	// The announce grace is the window the host's next tick lands in.
	deadline := time.Now().Add(5 * time.Second)
	for !srv.restarting.Load() {
		if time.Now().After(deadline) {
			t.Fatal("SIGTERM never marked the server as restarting")
		}
		time.Sleep(time.Millisecond)
	}
	host = tick(host)
	_ = host
	close(grace)

	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("SIGTERM never reached exit")
	}
	if saved() == 0 {
		t.Fatalf("the host's own world was not saved before exit (nothing in %s)", dir)
	}
}
