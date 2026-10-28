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
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/version"
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
// The dir is the game's folder under saves/recordings.
func recApp(t *testing.T) (*App, string) {
	t.Helper()
	testStateDirs(t)
	a := newGameApp(t)
	return a, a.world.RecordingsDir
}

// newGameApp starts a new game on the current state dirs and ticks it once.
func newGameApp(t *testing.T) *App {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	a.Update(sim.TickMsg(time.Now()))
	if a.world.Recording == nil {
		t.Fatal("no recording open after the first tick")
	}
	return a
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
	a.world.RecordEvent("history before save")
	a.world.Clock.SimTime = a.world.Clock.SimTime.Add(3 * 24 * time.Hour)
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
	if !hasRow(rows, "history before save") {
		t.Fatalf("a load pruned the history that led to the save: %q", rows)
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
	if got, _ := filepath.Glob(filepath.Join(saves, "recordings", "*")); len(got) != 0 {
		t.Fatalf("local dir got files or a game folder: %v", got)
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

// TestFirstTickWritesSessionStart: a game start is a row of its own, so a
// fresh start that rewinds the clock to the same scenario reads as a new
// session rather than a jump back in time.
func TestFirstTickWritesSessionStart(t *testing.T) {
	a, dir := recApp(t)
	a.Update(sim.TickMsg(time.Now()))
	files := recFiles(t, dir)
	if len(files) != 1 {
		t.Fatalf("recordings = %v, want one", files)
	}
	rows := recRows(t, files[0])
	want := "session start: " + version.Version
	n := 0
	for _, r := range rows {
		if strings.HasSuffix(r, want) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%q rows = %d after two ticks, want 1; rows %q", want, n, rows)
	}
}

// recordingsRoot is saves/recordings under the test's state dirs.
func recordingsRoot(t *testing.T) string {
	t.Helper()
	d, err := save.SavesDir()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(d, "recordings")
}

// Guard 1: two new games in a row write to two folders, and the second game's
// vessel file starts with its own header, free of the first game's rows.
func TestNewGamesGetOwnFolders(t *testing.T) {
	testStateDirs(t)
	a1 := newGameApp(t)
	a1.world.RecordEvent("game one only")
	a1.world.CloseRecording()
	a2 := newGameApp(t)
	d1, d2 := a1.world.RecordingsDir, a2.world.RecordingsDir
	if d1 == d2 || filepath.Dir(d1) != recordingsRoot(t) || filepath.Dir(d2) != recordingsRoot(t) {
		t.Fatalf("game folders %q and %q, want two different folders under %q", d1, d2, recordingsRoot(t))
	}
	f2 := recFiles(t, d2)
	if len(f2) != 1 {
		t.Fatalf("second game files = %v", f2)
	}
	b, _ := os.ReadFile(f2[0])
	if strings.Contains(string(b), "game one only") || strings.Count(string(b), "# Flight Recording") != 1 {
		t.Fatalf("second game's file carries the first game's history:\n%s", b)
	}
}

// Guard 2: save in game A, start game B, load the save: rows go to A's folder.
func TestLoadContinuesSavedGameFolder(t *testing.T) {
	testStateDirs(t)
	a1 := newGameApp(t)
	dirA := a1.world.RecordingsDir
	info, err := save.WriteNamed(a1.world, "snap")
	if err != nil {
		t.Fatal(err)
	}
	a1.world.CloseRecording()
	b := newGameApp(t)
	if b.world.RecordingsDir == dirA {
		t.Fatal("setup: game B shares game A's folder")
	}
	if err := b.loadWorldByID(info.ID); err != nil {
		t.Fatal(err)
	}
	if b.world.RecordingsDir != dirA {
		t.Fatalf("after load RecordingsDir = %q, want %q", b.world.RecordingsDir, dirA)
	}
	b.world.RecordEvent("after the load")
	if rows := recRows(t, recFiles(t, dirA)[0]); !hasRow(rows, "after the load") {
		t.Fatalf("A's file rows = %q, want the post-load row", rows)
	}
}

// Guard 3 (App side): a save with no folder (a pre-19 save) loads into a fresh one.
func TestLoadWithoutFolderStartsNewFolder(t *testing.T) {
	testStateDirs(t)
	a := newGameApp(t)
	old := a.world.RecordingsDir
	a.world.GameFolder = "" // what a v18 save decodes to
	info, err := save.WriteNamed(a.world, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.loadWorldByID(info.ID); err != nil {
		t.Fatal(err)
	}
	if got := a.world.RecordingsDir; got == old || filepath.Dir(got) != recordingsRoot(t) {
		t.Fatalf("RecordingsDir = %q, want a new folder besides %q", got, old)
	}
}

// Guard 4: every vessel's file gets the session marker once, the first time it
// is opened; after a load the marker is `loaded save <id>`.
func TestMarkerOncePerFilePerSession(t *testing.T) {
	a, dir := recApp(t)
	first := a.world.ActiveCraft()
	second, err := a.world.SpawnCraft(sim.SpawnSpec{AltitudeM: 600e3})
	if err != nil {
		t.Fatal(err)
	}
	if a.world.ActiveCraft() != second {
		t.Fatal("setup: spawn did not switch to the new vessel")
	}
	count := func(id uint64, ev string) int {
		n := 0
		for _, r := range recRows(t, fileOfVessel(t, dir, id)) {
			if strings.HasSuffix(r, ev) {
				n++
			}
		}
		return n
	}
	start := "session start: " + version.Version
	for i := 0; i < 2; i++ { // back and forth: a second visit writes none
		a.world.SetActiveCraftIdx(0)
		a.world.SetActiveCraftIdx(1)
	}
	for _, c := range []*spacecraft.Spacecraft{first, second} {
		if n := count(c.ID, start); n != 1 {
			t.Errorf("vessel %d: %q rows = %d, want 1", c.ID, start, n)
		}
	}
	info, err := save.WriteNamed(a.world, "snap")
	if err != nil {
		t.Fatal(err)
	}
	if err := a.loadWorldByID(info.ID); err != nil {
		t.Fatal(err)
	}
	a.world.SetActiveCraftIdx(0)
	a.world.SetActiveCraftIdx(1)
	a.world.SetActiveCraftIdx(0)
	loaded := "loaded save " + info.ID
	for _, c := range []*spacecraft.Spacecraft{first, second} {
		if n := count(c.ID, loaded); n != 1 {
			t.Errorf("vessel %d: %q rows = %d after the load, want 1", c.ID, loaded, n)
		}
	}
}

// #817 item 4: start a game, then load a save at once: the fresh game's folder
// holds only its marker row and goes. A game that did anything keeps its
// folder, and loading the save of the folder being left removes nothing.
func TestLoadRightAfterStartRemovesOrphanFolder(t *testing.T) {
	cases := []struct {
		name     string
		act      func(b *App)
		wantGone bool
	}{
		{"marker only", func(b *App) {}, true},
		{"one real row (boundary)", func(b *App) { b.world.RecordEvent("did something") }, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			testStateDirs(t)
			a1 := newGameApp(t)
			dirA := a1.world.RecordingsDir
			info, err := save.WriteNamed(a1.world, "snap")
			if err != nil {
				t.Fatal(err)
			}
			a1.world.CloseRecording()
			b := newGameApp(t)
			dirB := b.world.RecordingsDir
			c.act(b)
			if err := b.loadWorldByID(info.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dirB); (err != nil) != c.wantGone {
				t.Errorf("fresh game's folder gone = %v, want %v", err != nil, c.wantGone)
			}
			if _, err := os.Stat(dirA); err != nil {
				t.Errorf("the loaded game's own folder vanished: %v", err)
			}
			// Same-folder load: nothing may be removed.
			if err := b.loadWorldByID(info.ID); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(dirA); err != nil {
				t.Errorf("reloading the same game removed its folder: %v", err)
			}
		})
	}
}

// #817 item 2 (App side): a failing recording is flashed to the pilot.
func TestRecordingFaultIsFlashed(t *testing.T) {
	a, _ := recApp(t)
	a.world.Recording.Close() // every write now fails
	a.world.RecordEvent("lost")
	a.Update(sim.TickMsg(time.Now()))
	if !strings.Contains(a.statusMsg, "flight recording") {
		t.Fatalf("status = %q, want a flight recording failure notice", a.statusMsg)
	}
	if a.world.RecordingFault != "" {
		t.Errorf("fault not drained: %q", a.world.RecordingFault)
	}
}
