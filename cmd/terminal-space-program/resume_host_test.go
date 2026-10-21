package main

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// ADR 0040 amendment 3: only a --serve start resumes the newest autosave. Solo
// play does not, and --reset-fleet starts the host fresh (the reset aligns a
// fresh host World, ring slot 0).
func TestHostResumesNewestAutosaveOnServe(t *testing.T) {
	cases := []struct {
		name                  string
		serveMode, resetFleet bool
		wantResume            bool
	}{
		{"serve resumes", true, false, true},
		{"solo play does not", false, false, false},
		{"reset-fleet starts fresh", true, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_STATE_HOME", t.TempDir())
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			saved, err := sim.NewWorld()
			if err != nil {
				t.Fatal(err)
			}
			saved.Clock.SimTime = saved.Clock.SimTime.Add(5 * time.Hour)
			if err := save.WriteAutosave(saved); err != nil {
				t.Fatal(err)
			}
			app, err := tui.New(nil)
			if err != nil {
				t.Fatal(err)
			}
			fresh := app.World()
			msg := resumeHostAutosave(app, c.serveMode, c.resetFleet)
			resumed := app.World() != fresh
			if resumed != c.wantResume {
				t.Fatalf("resumed = %v, want %v", resumed, c.wantResume)
			}
			if c.wantResume && !app.World().Clock.SimTime.Equal(saved.Clock.SimTime) {
				t.Errorf("resumed World is at %v, want the autosave's %v", app.World().Clock.SimTime, saved.Clock.SimTime)
			}
			if c.wantResume && msg == "" {
				t.Errorf("no line saying the host resumed")
			}
		})
	}
}

// A --serve start with nothing readable in the ring keeps the fresh World.
func TestHostServeStartWithEmptyRingKeepsFreshWorld(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	app, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	fresh := app.World()
	resumeHostAutosave(app, true, false)
	if app.World() != fresh {
		t.Errorf("the fresh World was replaced with nothing to resume")
	}
}

// F7: after --reset-fleet, a plain restart before the first autosave must not
// bring the pre-reset host back. The reset leaves the fresh host World as the
// newest ring slot, so the next resume picks it.
func TestPlainRestartAfterResetFleetResumesTheFreshHost(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	old, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	old.Clock.SimTime = old.Clock.SimTime.Add(5 * time.Hour)
	if err := save.WriteAutosave(old); err != nil {
		t.Fatal(err)
	}
	app, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	epoch := old.Clock.SimTime.Add(-90 * 24 * time.Hour)
	if msg := resumeHostAutosave(app, true, true); msg != "" {
		t.Fatalf("a reset start resumed: %s", msg)
	}
	alignHostAfterReset(app, epoch)

	restarted, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	resumeHostAutosave(restarted, true, false) // the plain restart
	if got := restarted.World().Clock.SimTime; !got.Equal(epoch) {
		t.Errorf("the restart resumed the pre-reset host at %v, want the reset epoch %v", got, epoch)
	}
}
