package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0040 amendment 3: the hosting App resumes the newest autosave ring slot
// and acknowledges its own autosaves.

func autosaveAt(t *testing.T, offset time.Duration) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.Clock.SimTime = w.Clock.SimTime.Add(offset)
	if err := save.WriteAutosave(w); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond) // ClockT0 is the write time: keep the order strict
	return w
}

func TestResumeNewestAutosave(t *testing.T) {
	testStateDirs(t)
	// Four writes into a ring of three: the fourth overwrites slot 1, so the
	// newest is NOT the highest-numbered slot.
	var last *sim.World
	for i := 1; i <= 4; i++ {
		last = autosaveAt(t, time.Duration(i)*time.Hour)
	}
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.ResumeNewestAutosave(); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if got := a.World().Clock.SimTime; !got.Equal(last.Clock.SimTime) {
		t.Errorf("resumed World is at %v, want the newest slot's %v", got, last.Clock.SimTime)
	}
	if !strings.Contains(a.statusMsg, "resumed autosave") {
		t.Errorf("no footer message saying what happened: %q", a.statusMsg)
	}
}

func TestResumeNewestAutosaveWithNoSlotKeepsTheFreshWorld(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh := a.World()
	if _, err := a.ResumeNewestAutosave(); err == nil {
		t.Errorf("resuming an empty ring reported success")
	}
	if a.World() != fresh {
		t.Errorf("the fresh World was replaced although no slot could be read")
	}
}

func TestPersistNowCallsOnPersistedOnSuccessOnly(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	var got *sim.World
	a.SetOnPersisted(func(w *sim.World) { got = w })
	if err := a.PersistNow(); err != nil {
		t.Fatal(err)
	}
	if got != a.World() {
		t.Errorf("the hook was not called with the World that was written")
	}

	got = nil
	a.guestSave = func(*sim.World) error { return errors.New("disk full") }
	if err := a.PersistNow(); err == nil {
		t.Fatal("PersistNow swallowed the write error")
	}
	if got != nil {
		t.Errorf("the hook fired for a write that failed")
	}
}

// The periodic autosave is the host's usual write; it must acknowledge too.
func TestPeriodicAutosaveCallsOnPersisted(t *testing.T) {
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	called := false
	a.SetOnPersisted(func(*sim.World) { called = true })
	now := time.Now()
	a.lastAutosave = now.Add(-24 * time.Hour)
	a.world.Clock.Paused = false
	a.maybeAutosave(now)
	if !called {
		t.Errorf("a periodic autosave did not reach the hook")
	}
}
