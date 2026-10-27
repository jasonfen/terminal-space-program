package tui

// New file: app_test.go is over the ~600-line rule (S6 of the Flight Recorder, ADR 0062).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/recorder"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestMain keeps every test in this package off the real saves dir: a ticking
// App now opens a Flight Recording under $XDG_STATE_HOME.
func TestMain(m *testing.M) {
	d, err := os.MkdirTemp("", "tsp-tui-state")
	if err != nil {
		panic(err)
	}
	os.Setenv("XDG_STATE_HOME", d)
	os.Setenv("XDG_CONFIG_HOME", d)
	code := m.Run()
	os.RemoveAll(d)
	os.Exit(code)
}

// recApp is a fresh App on temp dirs, ticked once so its recording is open.
func recApp(t *testing.T) (*App, string) {
	t.Helper()
	saves := testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Update(sim.TickMsg(time.Now()))
	if a.world.Recording == nil {
		t.Fatal("no recording open after the first tick")
	}
	return a, filepath.Join(saves, "recordings")
}

func recFiles(t *testing.T, dir string) []string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	return m
}

// recRows is every dated row of the file, in file order. Tests match a row by
// its event text with HasSuffix (the event is the last column).
func recRows(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "20") {
			out = append(out, l)
		}
	}
	return out
}

func hasRow(rows []string, event string) bool {
	for _, r := range rows {
		if strings.HasSuffix(r, event) {
			return true
		}
	}
	return false
}

func fileOfVessel(t *testing.T, dir string, id uint64) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, strings.Replace("0000-*.txt", "0000", pad4(id), 1)))
	if len(m) != 1 {
		t.Fatalf("files for vessel %d = %v", id, m)
	}
	return m[0]
}

func pad4(id uint64) string { return fmt.Sprintf("%04d", id) }

// Guard 1, and seam 3: the save is dated more than the 7-day window before the
// newest row on file, yet the load row and the abandoned-future row both survive.
func TestLoadWritesLoadedSaveRow(t *testing.T) {
	a, dir := recApp(t)
	info, err := save.WriteNamed(a.world, "snap")
	if err != nil {
		t.Fatal(err)
	}
	a.world.Clock.SimTime = a.world.Clock.SimTime.Add(30 * 24 * time.Hour)
	a.world.RecordEvent("abandoned future")
	if err := a.loadWorldByID(info.ID); err != nil {
		t.Fatal(err)
	}
	files := recFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	rows := recRows(t, files[0])
	want := "loaded save " + info.ID
	if len(rows) == 0 || !strings.HasSuffix(rows[len(rows)-1], want) {
		t.Fatalf("rows = %q, want last to end %q", rows, want)
	}
	if !hasRow(rows, "abandoned future") {
		t.Fatalf("a rewind to before the window pruned the abandoned-future row: %q", rows)
	}
}

// Guard 2: after a load, rows go to the loaded Active Vessel's file.
func TestLoadSwapsRecordingFile(t *testing.T) {
	a, dir := recApp(t)
	first := a.world.ActiveCraft()
	info, err := save.WriteNamed(a.world, "snap")
	if err != nil {
		t.Fatal(err)
	}
	b, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3})
	if err != nil || a.world.ActiveCraft() != b {
		t.Fatalf("spawn: %v", err)
	}
	if err := a.loadWorldByID(info.ID); err != nil {
		t.Fatal(err)
	}
	fa, fb := fileOfVessel(t, dir, first.ID), fileOfVessel(t, dir, b.ID)
	nb := len(recRows(t, fb))
	a.world.RecordAction(missions.ActionThrottleDown)
	if rows := recRows(t, fa); len(rows) == 0 || !strings.HasSuffix(rows[len(rows)-1], "throttle_down") {
		t.Fatalf("loaded vessel's file rows = %q, want to end throttle_down", rows)
	}
	if got := len(recRows(t, fb)); got != nb {
		t.Fatalf("previous vessel's file grew from %d to %d rows after the load", nb, got)
	}
}

// A guest's dir is used, and the host's local dir stays empty.
func TestSetRecordingsDirRoutesTheFile(t *testing.T) {
	saves := testStateDirs(t)
	guest := t.TempDir()
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.SetRecordingsDir(guest)
	a.Update(sim.TickMsg(time.Now()))
	if len(recFiles(t, guest)) != 1 {
		t.Fatalf("guest dir files = %v", recFiles(t, guest))
	}
	if got := recFiles(t, filepath.Join(saves, "recordings")); len(got) != 0 {
		t.Fatalf("local dir got files: %v", got)
	}
}

// Guard 4: quitting closes the file, on both quit answers.
func TestQuitClosesRecording(t *testing.T) {
	for _, c := range []struct {
		name string
		keys []string
	}{{"save and quit", []string{"y"}}, {"quit writing nothing", []string{"n", "y"}}} {
		t.Run(c.name, func(t *testing.T) {
			a, dir := recApp(t)
			rec := a.world.Recording
			a.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
			var cmd tea.Cmd
			for _, k := range c.keys {
				_, cmd = a.Update(keyRunes(k))
			}
			if !isQuitCmd(cmd) {
				t.Fatal("did not quit")
			}
			if err := rec.Write(recorder.Row{Sim: a.world.Clock.SimTime}); err == nil {
				t.Fatal("write after quit succeeded: the file handle is still open")
			}
			if a.world.Recording != nil {
				t.Fatal("World still holds the recording")
			}
			if b, _ := os.ReadFile(recFiles(t, dir)[0]); !strings.HasPrefix(string(b), "# Flight Recording") {
				t.Fatalf("file incomplete: %q", b)
			}
		})
	}
}

// Guard 5: cycling the Settings row reaches the live recorder and the World.
func TestSettingsPushReachesRecorder(t *testing.T) {
	a, dir := recApp(t)
	base := a.world.Clock.SimTime.Truncate(time.Second) // rows print whole seconds
	day := 24 * time.Hour
	// "ancient" is past the recorder's one-day prune slack, so the prune check
	// is worth running once the window is 3 days.
	a.world.Clock.SimTime = base.Add(-3*day - 25*time.Hour)
	a.world.RecordEvent("ancient")
	a.world.Clock.SimTime = base.Add(-3*day - time.Second)
	a.world.RecordEvent("old")
	a.world.Clock.SimTime = base.Add(-3 * day)
	a.world.RecordEvent("edge")
	for i := 0; i < 4; i++ { // 7 -> 14 -> 30 -> 1 -> 3
		a.cycleRecordingWindow()
	}
	if got := a.world.RecordingWindowDays; got != 3 {
		t.Fatalf("World.RecordingWindowDays = %d, want 3 (a later vessel switch would reopen with the old window)", got)
	}
	a.world.Clock.SimTime = base
	a.world.RecordEvent("anchor")
	rows := recRows(t, recFiles(t, dir)[0])
	if hasRow(rows, "ancient") || hasRow(rows, "old") || !hasRow(rows, "edge") || !hasRow(rows, "anchor") {
		t.Fatalf("rows = %q, want edge and anchor only (3-day window: exactly 3 days old stays, one second older goes)", rows)
	}
}
