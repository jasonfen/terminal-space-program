package serve

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Restarts announce, never gate (ADR 0040 §6).
//
// The production host runs an hourly auto-update timer, so publishing a
// release tag restarts the live server within the hour. Until now that
// arrived as a silent yank: SessionEventServerRestart was built in v0.30 for
// admin-triggered restarts and never wired to the supervisor's own stop
// signal, so a player mid-approach simply lost their connection. The moment
// exists and the renderer is proven — only the emission was missing, and a
// half-wired moment staying dark is the kind of silence that reads as broken.
//
// Deliberately NO adopt gate. Deferring a restart while docks[] is non-empty
// protected something real before the ledger was durable; now it protects
// nothing, and a long docked mission could otherwise pin the live server to
// an old build indefinitely.
//
// SIGTERM only. SIGINT belongs to the host's own bubbletea program, which
// installs its own handling; taking it here would fight the TUI over Ctrl-C.

// watchRestartSignals listens for the supervisor's stop signal for the
// listener's life. Started by Serve, torn down with it.
func (s *Server) watchRestartSignals(stop <-chan struct{}) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM)
	defer signal.Stop(sig)
	s.restartOnSignal(sig, stop)
}

// restartOnSignal is the body, split out so a test can push a signal in
// without one being raised against the test binary.
func (s *Server) restartOnSignal(sig <-chan os.Signal, stop <-chan struct{}) {
	select {
	case <-sig:
	case <-stop:
		return
	}
	s.AnnounceRestart()
	// Let the next tick carry the warning to every connected screen before
	// the listener drains under them. The same grace the admin restart uses.
	time.Sleep(restartAnnounceGrace)
	s.drainAndClose()
	// Plain zero: the supervisor asked for this stop and is already going to
	// relaunch. The 42 marker means "and run the adopt tooling first", which
	// is precisely what has just happened.
	exitFunc(0)
}

// AnnounceRestart broadcasts the server-restart moment to every session. It
// is addressed at nobody in particular — everyone connected is about to be
// dropped, and everyone's progress is about to be persisted.
func (s *Server) AnnounceRestart() {
	s.presence.event(sim.SessionEventServerRestart, "", "", "")
}
