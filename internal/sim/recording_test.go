// New test file: new prod file recording.go (ADR 0062 / #800 S5).
package sim

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// recAttach points the world at a temp recordings dir and opens the Active
// Vessel's file.
func recAttach(t *testing.T, w *World) string {
	t.Helper()
	dir := t.TempDir()
	w.RecordingsDir = dir
	if err := w.OpenActiveRecording(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.CloseRecording)
	return dir
}

// recRead returns the dated lines of every file in dir, split into numbers
// rows (empty event column) and event texts, in file order.
func recRead(t *testing.T, path string) (numbers int, events []string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var off int
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "sim ") {
			off = strings.Index(l, "event")
		}
		if !strings.HasPrefix(l, "20") {
			continue
		}
		if len(l) > off && strings.TrimSpace(l[off:]) != "" {
			events = append(events, strings.TrimSpace(l[off:]))
		} else {
			numbers++
		}
	}
	return
}

func recFile(t *testing.T, dir string) string {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	if len(m) != 1 {
		t.Fatalf("recordings in %s = %v, want one file", dir, m)
	}
	return m[0]
}

func recWorld(t *testing.T) (*World, string) {
	t.Helper()
	w := mustWorld(t)
	crewTendActive(w)
	return w, recFile(t, recAttach(t, w))
}

func ticks(w *World, n int) {
	for i := 0; i < n; i++ {
		w.Tick()
	}
}

// Guard 1: burning samples every second (20 ticks at 1x).
func TestSampleEverySecondWhileBurning(t *testing.T) {
	w, path := recWorld(t)
	w.SetThrottle(1)
	w.StartManualBurn()
	if w.ActiveCraft().ManualBurn == nil {
		t.Fatal("setup: no burn")
	}
	ticks(w, 60)
	if n, _ := recRead(t, path); n != 3 {
		t.Errorf("numbers rows after 60 burning ticks = %d, want 3", n)
	}
}

// Guard 2: coasting samples every 60 sim-s; accrued == 60 s fires (equal case).
func TestSampleEverySixtySecondsCoasting(t *testing.T) {
	w, path := recWorld(t)
	ticks(w, 1199)
	if n, _ := recRead(t, path); n != 0 {
		t.Fatalf("rows after 1,199 ticks = %d, want 0", n)
	}
	w.Tick()
	if n, _ := recRead(t, path); n != 1 {
		t.Errorf("rows after 1,200 ticks = %d, want 1", n)
	}
}

// Guard 3: at 100,000x the 20-tick wall cap holds; the 20th tick fires.
func TestWallCapTwentyTicks(t *testing.T) {
	w, path := recWorld(t)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	ticks(w, 19)
	if n, _ := recRead(t, path); n != 0 {
		t.Fatalf("rows after 19 ticks = %d, want 0", n)
	}
	w.Tick()
	if n, _ := recRead(t, path); n != 1 {
		t.Fatalf("rows after 20 ticks = %d, want 1", n)
	}
	ticks(w, 20)
	if n, _ := recRead(t, path); n != 2 {
		t.Errorf("rows after 40 ticks = %d, want 2", n)
	}
}

// Guard 4: input rows are never capped.
func TestInputRowsNeverCapped(t *testing.T) {
	w, path := recWorld(t)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	for i := 0; i < 5; i++ {
		w.RecordAction(missions.ActionThrottleDown)
	}
	w.Tick()
	n, ev := recRead(t, path)
	if len(ev) != 5 || n > 1 {
		t.Errorf("events = %d numbers = %d, want 5 and at most 1", len(ev), n)
	}
}

// Guard 7: curated verbs carry numbers; a circular orbit prints 250 x 250 even
// when float error leaves the periapsis a hair under (equal case).
func TestCuratedVerbsCarryNumbers(t *testing.T) {
	w, path := recWorld(t)
	c := w.ActiveCraft()
	r := c.Primary.RadiusMeters() + 250e3
	v := math.Sqrt(c.Primary.GravitationalParameter()/r) * (1 - 1e-9)
	c.State.R.X, c.State.R.Y, c.State.R.Z = r, 0, 0
	c.State.V.X, c.State.V.Y, c.State.V.Z = 0, v, 0
	w.RecordAction(missions.ActionThrottleCut)
	_, ev := recRead(t, path)
	if len(ev) != 1 || !strings.Contains(ev[0], "orbit 250 x 250 km") {
		t.Errorf("throttle_cut event = %q, want it to contain %q", ev, "orbit 250 x 250 km")
	}
}

// Guard 6: switching vessels writes a row on both sides and swaps the file.
func TestActiveVesselSwitchRowsOnBothSides(t *testing.T) {
	w := mustWorld(t)
	dir := recAttach(t, w)
	a := w.ActiveCraft()
	b, err := w.SpawnCraft(SpawnSpec{AltitudeM: 600e3})
	if err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	if len(files) != 2 || w.ActiveCraft() != b {
		t.Fatalf("setup: files = %v, active is B = %v", files, w.ActiveCraft() == b)
	}
	var aFile, bFile string
	for _, f := range files {
		if strings.HasPrefix(filepath.Base(f), padID(a.ID)) {
			aFile = f
		}
		if strings.HasPrefix(filepath.Base(f), padID(b.ID)) {
			bFile = f
		}
	}
	_, aEv := recRead(t, aFile)
	_, bEv := recRead(t, bFile)
	if len(aEv) == 0 || aEv[len(aEv)-1] != "active vessel → "+b.Name {
		t.Errorf("outgoing last row = %q, want active vessel → %s", aEv, b.Name)
	}
	if len(bEv) == 0 || bEv[0] != "active vessel → "+b.Name {
		t.Errorf("incoming first row = %q, want active vessel → %s", bEv, b.Name)
	}
	w.RecordAction(missions.ActionThrottleDown)
	if _, bEv = recRead(t, bFile); bEv[len(bEv)-1] != "throttle_down" {
		t.Errorf("rows after the switch went elsewhere: B events = %q", bEv)
	}
}

func padID(id uint64) string { return fmt.Sprintf("%04d-", id) }
