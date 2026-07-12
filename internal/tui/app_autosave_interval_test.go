package tui

import (
	"sort"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// tickAt drives one physics tick through the real Update path with a
// synthetic wall-clock stamp — sim.TickMsg carries the tea.Tick time,
// so the interval-autosave clock is injected here rather than read via
// time.Now() inside the app.
func tickAt(a *App, at time.Time) {
	a.Update(sim.TickMsg(at))
}

// TestIntervalAutosaveFiresAfterWallClockInterval — with the default
// 5-minute setting, the autosave ring stays untouched until real
// (wall-clock) minutes elapse, then fires save.WriteAutosave (v0.26 S4
// / ADR 0033 §E).
func TestIntervalAutosaveFiresAfterWallClockInterval(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t0 := time.Now()
	tickAt(a, t0) // arms the timer; must not save immediately
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("saves dir = %v after the arming tick, want empty", files)
	}

	tickAt(a, t0.Add(4*time.Minute+59*time.Second))
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("saves dir = %v before the interval elapsed, want empty", files)
	}

	tickAt(a, t0.Add(5*time.Minute))
	files := savesDirFiles(t, dir)
	if len(files) != 1 || files[0] != "autosave-1.json" {
		t.Fatalf("saves dir = %v after the interval elapsed, want exactly [autosave-1.json]", files)
	}

	// The timer re-arms: the very next tick must not save again.
	tickAt(a, t0.Add(5*time.Minute+time.Second))
	if files := savesDirFiles(t, dir); len(files) != 1 {
		t.Fatalf("saves dir = %v right after a fire, want still 1 file", files)
	}
}

// TestIntervalAutosaveRotatesRing — repeated interval fires fill then
// rotate the 3-slot autosave ring; a fourth fire overwrites rather
// than growing the directory.
func TestIntervalAutosaveRotatesRing(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t0 := time.Now()
	tickAt(a, t0) // arm
	for i := 1; i <= 4; i++ {
		tickAt(a, t0.Add(time.Duration(i)*5*time.Minute))
	}

	files := savesDirFiles(t, dir)
	sort.Strings(files)
	want := []string{"autosave-1.json", "autosave-2.json", "autosave-3.json"}
	if len(files) != len(want) {
		t.Fatalf("saves dir = %v after 4 fires, want the 3-slot ring %v", files, want)
	}
	for i := range want {
		if files[i] != want[i] {
			t.Fatalf("saves dir = %v after 4 fires, want the 3-slot ring %v", files, want)
		}
	}
}

// TestIntervalAutosaveSkippedWhilePaused — finding 2. Wall-clock ticks
// keep flowing while the sim is paused, but the frozen world must not
// autosave: three idle intervals must not evict all three ring slots
// with identical snapshots. Unpausing captures exactly one honest
// snapshot on the next due tick.
func TestIntervalAutosaveSkippedWhilePaused(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	t0 := time.Now()
	tickAt(a, t0) // arm
	a.world.Clock.Paused = true

	for i := 1; i <= 3; i++ {
		tickAt(a, t0.Add(time.Duration(i)*5*time.Minute))
	}
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("saves dir = %v after 3 paused intervals, want empty (a frozen world must not autosave)", files)
	}

	a.world.Clock.Paused = false
	tickAt(a, t0.Add(4*5*time.Minute))
	if files := savesDirFiles(t, dir); len(files) != 1 {
		t.Fatalf("saves dir = %v after unpause, want exactly one autosave", files)
	}
}

// TestIntervalAutosaveDisabledByZero — interval 0 ("off") suppresses
// the periodic autosave entirely, while the on-quit autosave still
// fires (S2 behaviour, unaffected by the setting).
func TestIntervalAutosaveDisabledByZero(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s := a.orbitView.Settings()
	s.SetAutosaveIntervalMin(0)
	a.orbitView.SetSettings(s)

	t0 := time.Now()
	tickAt(a, t0)
	tickAt(a, t0.Add(24*time.Hour)) // a full day of wall clock — still off
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("saves dir = %v with interval 0, want empty (interval autosave disabled)", files)
	}

	// Quit-autosave is independent of the interval setting.
	a.autosave()
	files := savesDirFiles(t, dir)
	if len(files) != 1 || files[0] != "autosave-1.json" {
		t.Fatalf("saves dir = %v after quit-autosave with interval 0, want [autosave-1.json]", files)
	}
}

// TestIntervalAutosaveIgnoresGameTime — warp accelerates the sim clock,
// not the autosave timer: hours of game time inside a wall-clock minute
// must not fire an autosave (real minutes, not game minutes — §E).
func TestIntervalAutosaveIgnoresGameTime(t *testing.T) {
	dir := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	a.world.Clock.WarpIdx = len(sim.WarpFactors) - 1 // max warp (100000×)
	t0 := time.Now()
	simT0 := a.world.Clock.SimTime
	// Many ticks, all within a single wall-clock minute.
	for i := 0; i < 50; i++ {
		tickAt(a, t0.Add(time.Duration(i)*time.Second))
	}
	if advanced := a.world.Clock.SimTime.Sub(simT0); advanced < time.Hour {
		t.Fatalf("sim advanced only %v of game time; the warp premise of this test is broken", advanced)
	}
	if files := savesDirFiles(t, dir); len(files) != 0 {
		t.Fatalf("saves dir = %v after warped game-hours in <1 wall minute, want empty", files)
	}
}
