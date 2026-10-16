package serve

import (
	"net"
	"os"
	"strings"
	"testing"
	"time"
)

// ADR 0054 decision 6: stop hosting, restart, SIGTERM and the host's own
// quit share one drop. Each test reads the far end, the guest's ssh session
// and its stderr, not the host's toast.

func (f *dropFixture) wantStopLine(t *testing.T) {
	t.Helper()
	want := "terminal-space-program: " + f.hostHandle(t) + " stopped hosting — reconnect when they host again; your progress is saved"
	if got := f.guest.stderr(); !strings.Contains(got, want) {
		t.Errorf("guest stderr = %q, want it to contain %q", got, want)
	}
}

// h: every guest is dropped with the stop-hosting line, and by the time
// the stop reports back nobody is left unwinding and the host reads offline.
func TestStopHostingDropsEveryGuestBeforeItReports(t *testing.T) {
	f := startDropFixture(t)
	rm, ok := f.host.(reportingModel)
	if !ok {
		t.Fatalf("host model is %T", f.host)
	}
	srv := f.srv
	_, cmd := rm.stopHosting()
	if cmd == nil {
		t.Fatal("stopHosting returned no command: nothing drops the guests or reports back")
	}
	msg := cmd() // runs the drain
	if _, ok := msg.(hostStoppedMsg); !ok {
		t.Fatalf("stop reported %T, want hostStoppedMsg", msg)
	}
	f.waitGone(t, time.Second, "stop hosting") // the client sees the close a beat after the server finished
	if !srv.waitSessionsOn(time.After(50 * time.Millisecond)) {
		t.Error("session handlers were still unwinding when the stop reported back")
	}
	f.wantStopLine(t)
	if srv.presence.isOnline(sessionHostFP) {
		t.Error("the host still reads online after stopping hosting")
	}
	if c, err := net.DialTimeout("tcp", srv.Addr(), 300*time.Millisecond); err == nil {
		c.Close()
		t.Error("the listener still accepts connections after stop hosting")
	}
}

// The host quitting while hosting drops everyone with the same line.
func TestHostQuitDropsEveryGuest(t *testing.T) {
	f := startDropFixture(t)
	f.srv.StopHostingForQuit()
	f.waitGone(t, time.Second, "host quit")
	f.wantStopLine(t)
}

// restartDrop drives a restart through trigger with a controllable clock,
// and reports what was true at the instant the process would have exited.
type restartObs struct {
	code        int
	guestGone   bool
	waits       []time.Duration
	chipBefore  bool
	aliveBefore bool
}

func observeRestart(t *testing.T, trigger func(f *dropFixture)) (*dropFixture, restartObs) {
	t.Helper()
	f := startDropFixture(t)
	clk := &dropClock{}
	f.srv.after = clk.after
	exited := make(chan int, 1)
	oldExit := exitFunc
	exitFunc = func(code int) { exited <- code }
	t.Cleanup(func() { exitFunc = oldExit })

	trigger(f)

	// The chip window: the warning is out and the guest is still there.
	deadline := time.Now().Add(5 * time.Second)
	for clk.fire(restartAnnounceGrace) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the restart never waited its %v chip window; waits: %v", restartAnnounceGrace, clk.requested())
		}
		time.Sleep(time.Millisecond)
		// not fired yet: observe state during the window
	}
	var o restartObs
	select {
	case o.code = <-exited:
	case <-time.After(15 * time.Second):
		t.Fatal("the restart never reached exit")
	}
	select {
	case <-f.guest.exited:
		o.guestGone = true
	case <-time.After(time.Second): // the client sees the close a beat after the server finished
	}
	o.waits = clk.requested()
	return f, o
}

func TestAdminRestartDropsGuestsAfterTheChipWindowThenExits42(t *testing.T) {
	f, o := observeRestart(t, func(f *dropFixture) {
		f.host, _ = f.host.Update(sessionRestartMsg())
		// Inside the window: the warning is posted, the guest is still flying.
		deadline := time.Now().Add(5 * time.Second)
		for len(eventsFor(f.srv, f.gfp, restartKind)) == 0 {
			if time.Now().After(deadline) {
				t.Fatal("no restart warning inside the chip window")
			}
			time.Sleep(time.Millisecond)
		}
		select {
		case <-f.guest.exited:
			t.Fatal("the guest was dropped before the chip window ended")
		default:
		}
	})
	if o.code != restartExitCode {
		t.Errorf("exit code = %d, want %d", o.code, restartExitCode)
	}
	if !o.guestGone {
		t.Error("the process exited while the guest's session was still alive (the old drain waited out 8 s and still left them)")
	}
	if got := f.guest.stderr(); !strings.Contains(got, "terminal-space-program: server restarting — reconnect in a moment; your progress is saved") {
		t.Errorf("guest stderr = %q, want the restart line", got)
	}
	for _, w := range o.waits {
		if w > 2*f.srv.dropWait && w != restartAnnounceGrace {
			t.Errorf("a %v wait survives: the dead 8 s between the chip and the drop must be gone (waits %v)", w, o.waits)
		}
	}
}

func TestSigtermSharesTheRestartDrop(t *testing.T) {
	f := startDropFixture(t)
	clk := &dropClock{}
	f.srv.after = clk.after
	exited := make(chan int, 1)
	oldExit := exitFunc
	exitFunc = func(code int) { exited <- code }
	t.Cleanup(func() { exitFunc = oldExit })

	sig := make(chan os.Signal, 1)
	stop := make(chan struct{})
	go f.srv.restartOnSignal(sig, stop)
	sig <- os.Interrupt

	deadline := time.Now().Add(5 * time.Second)
	for clk.fire(restartAnnounceGrace) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("SIGTERM never waited its chip window; waits: %v", clk.requested())
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case code := <-exited:
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("SIGTERM never reached exit")
	}
	select {
	case <-f.guest.exited:
	case <-time.After(time.Second):
		t.Error("SIGTERM exited the process while the guest's session was still alive")
	}
	if got := f.guest.stderr(); !strings.Contains(got, "terminal-space-program: server restarting — reconnect in a moment; your progress is saved") {
		t.Errorf("guest stderr = %q, want the restart line", got)
	}
}
