// New test file: new prod file recording.go (ADR 0062 / #800 S5).
package sim

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/recorder"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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
	w.rec.warp = w.Clock.WarpIdx + 1 // the warp row is not this test's subject
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

// F7 (review): rows go to the Active Vessel's file only. A lifecycle event on
// the other vessel writes to neither file; the same event on the Active
// Vessel does (positive control).
func TestEventsOnNonActiveVesselWriteNothing(t *testing.T) {
	w := mustWorld(t)
	a := w.ActiveCraft()
	b, err := w.SpawnCraft(SpawnSpec{AltitudeM: 600e3})
	if err != nil || w.ActiveCraft() != b {
		t.Fatalf("setup: %v", err)
	}
	dir := recAttach(t, w)
	before := allEvents(t, dir)
	w.recordSOI(a, "Earth")
	w.recordSurface(a, 3)
	w.recordRowFor(a, "stage 1 dropped")
	if after := allEvents(t, dir); len(after) != len(before) {
		t.Fatalf("non-active vessel wrote rows: %q -> %q", before, after)
	}
	w.recordSOI(b, "Earth")
	if after := allEvents(t, dir); len(after) != len(before)+1 {
		t.Fatalf("active vessel's soi row missing: %q -> %q", before, after)
	}
}

// F1 (review): with no liftoff stamp, T+ counts from the first row ever
// recorded, across a close and reopen 3 sim-hours later.
func TestTPlusSurvivesReopenWithoutLiftoff(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.LiftoffSimTime, c.OnPad = time.Time{}, false
	dir := recAttach(t, w)
	w.RecordEvent("first")
	w.Clock.SimTime = w.Clock.SimTime.Add(3 * time.Hour)
	if err := w.OpenActiveRecording(); err != nil {
		t.Fatal(err)
	}
	w.RecordEvent("later")
	b, _ := os.ReadFile(recFile(t, dir))
	var last string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		last = l
	}
	if !strings.HasSuffix(last, "later") || !strings.Contains(last, " 3:00:00 ") {
		t.Fatalf("last row = %q, want T+ 3:00:00", last)
	}
}

// #817 item 1: a vessel that lifts off while another is Active gets its stamp
// but no recordLiftoff; its header must name the liftoff once it is flown
// again, and a header that already agrees is left alone (equal case).
func TestStaleHeaderFixedWhenFlownAfterNonActiveLiftoff(t *testing.T) {
	w := mustWorld(t)
	dir := recAttach(t, w)
	a := w.ActiveCraft()
	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 600e3}); err != nil {
		t.Fatal(err)
	}
	stamp := w.Clock.SimTime.Add(5 * time.Minute)
	a.LiftoffSimTime, a.OnPad = stamp, false
	want := "# T+0 = " + stamp.UTC().Format("2006-01-02 15:04:05") + " (liftoff)"
	var aFile string
	files, _ := filepath.Glob(filepath.Join(dir, padID(a.ID)+"*.txt"))
	aFile = files[0]
	for i := 0; i < 2; i++ { // the second open is the equal case
		w.SetActiveCraftIdx(0)
		b, _ := os.ReadFile(aFile)
		if n := strings.Count(string(b), "# T+0 = "); n != 1 || !strings.Contains(string(b), want) {
			t.Fatalf("open %d: header lacks %q (T+0 lines: %d):\n%s", i, want, n, strings.SplitN(string(b), "\n", 3)[1])
		}
		w.SetActiveCraftIdx(1)
	}
}

// #817 item 2: a write error is surfaced once, not swallowed. Closing the
// file under the recorder makes every Write fail; many failing rows yield one
// fault, and a World whose recording works has none (positive control).
func TestRecordingWriteErrorIsSurfacedOnce(t *testing.T) {
	w, _ := recWorld(t)
	w.RecordEvent("fine")
	if w.RecordingFault != "" {
		t.Fatalf("fault on a healthy recording: %q", w.RecordingFault)
	}
	w.Recording.Close()
	w.RecordEvent("lost")
	first := w.RecordingFault
	if first == "" {
		t.Fatal("write error swallowed: RecordingFault empty")
	}
	w.RecordingFault = "" // the App drained it
	w.RecordEvent("lost again")
	if w.RecordingFault != "" {
		t.Errorf("second failure re-raised the fault: %q", w.RecordingFault)
	}
}

func padID(id uint64) string { return fmt.Sprintf("%04d-", id) }

// allEvents is every event text in every file under dir.
func allEvents(t *testing.T, dir string) []string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	var out []string
	for _, f := range files {
		_, ev := recRead(t, f)
		out = append(out, ev...)
	}
	return out
}

// liftable bumps the pad vessel's thrust until it can leave the pad.
func liftable(w *World, c *spacecraft.Spacecraft) {
	for !w.canLiftOff(c) {
		c.Thrust *= 1.5
	}
}

// inMoonSOI puts the Active Vessel 20,000 km from the Moon, bound to Earth.
func inMoonSOI(t *testing.T, w *World) {
	t.Helper()
	var earth, moon int = -1, -1
	for i, b := range w.System().Bodies {
		switch b.ID {
		case "earth":
			earth = i
		case "moon":
			moon = i
		}
	}
	if earth < 0 || moon < 0 {
		t.Fatal("no earth/moon in the system")
	}
	c := w.ActiveCraft()
	d := w.BodyPosition(w.System().Bodies[moon]).Sub(w.BodyPosition(w.System().Bodies[earth]))
	c.State.R = d.Add(d.Unit().Scale(2e7))
	// Bound and near-circular about Earth, so the analytic coast (not just
	// the Verlet sub-step path) is eligible at warp.
	tang := orbital.Vec3{Z: 1}.Cross(c.State.R).Unit()
	c.State.V = tang.Scale(math.Sqrt(c.Primary.GravitationalParameter() / c.State.R.Norm()))
}

type lifecycleCase struct {
	name  string
	setup func(t *testing.T) *World
	act   func(t *testing.T, w *World)
	want  string
}

// lifecycleCases is the table over the lifecycle sites (guards 5 and 8).
func lifecycleCases() []lifecycleCase {
	burner := func(t *testing.T) *World {
		w := mustWorld(t)
		twoStageBurner(t, w, 200).ActiveBurn.PlannedDV = 200 // as a fired node sets it
		return w
	}
	runBurn := func(w *World) {
		tickUntil(w, 200, func() bool { return w.ActiveCraft().BurnStalled() })
		if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
			panic(err)
		}
		tickUntil(w, 2000, func() bool { return w.ActiveCraft().ActiveBurn == nil })
	}
	return []lifecycleCase{
		{"liftoff", func(t *testing.T) *World { w, c := padSaturn(t, 1); liftable(w, c); return w },
			func(t *testing.T, w *World) { w.StartManualBurn() }, "liftoff"},
		{"soi verlet", func(t *testing.T) *World { w := mustWorld(t); inMoonSOI(t, w); return w },
			func(t *testing.T, w *World) { w.Tick() }, "soi: Earth → Moon"},
		{"soi kepler", func(t *testing.T) *World { w := mustWorld(t); inMoonSOI(t, w); w.Clock.WarpIdx = 3; return w },
			func(t *testing.T, w *World) { w.Tick() }, "soi: Earth → Moon"},
		{"crash", func(t *testing.T) *World {
			w := mustWorld(t)
			c := w.ActiveCraft()
			c.State.R = c.State.R.Unit().Scale(c.Primary.RadiusMeters() - 10)
			c.State.V = c.State.R.Unit().Scale(-300)
			return w
		}, func(t *testing.T, w *World) { w.Tick() }, "crash: Earth"},
		{"stage", func(t *testing.T) *World { w, _ := padSaturn(t, 0); return w },
			func(t *testing.T, w *World) {
				if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
					t.Fatal(err)
				}
			}, "stage: S-IC jettisoned"},
		{"burn start", func(t *testing.T) *World { w := mustWorld(t); crewTendActive(w); return w },
			func(t *testing.T, w *World) {
				w.ActiveCraft().Nodes = append(w.ActiveCraft().Nodes, spacecraft.ManeuverNode{
					TriggerTime: w.Clock.SimTime.Add(time.Second), Mode: spacecraft.BurnPrograde, DV: 50,
					Duration: 5 * time.Second, PrimaryID: w.ActiveCraft().Primary.ID, Throttle: 1})
				ticks(w, 40)
			}, "burn start: node 1, Δv 50"},
		{"burn stalled", burner, func(t *testing.T, w *World) { runBurn(w) }, "burn stalled: fuel out"},
		{"burn complete", burner, func(t *testing.T, w *World) { runBurn(w) }, "burn complete: 200 of 200 m/s"},
		{"bump", func(t *testing.T) *World { w, _, _ := bumpWorld(t, 30, -0.3); return w },
			func(t *testing.T, w *World) { tickUntil(w, 2000, func() bool { return w.LastBumpEvent != nil }) },
			"bump: S-IVB, 0.3 m/s"},
		{"dock", func(t *testing.T) *World { return newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0).w },
			func(t *testing.T, w *World) { w.DockCrafts(0, 1) }, "dock: with "},
		{"rcs pulse", func(t *testing.T) *World {
			w := mustWorld(t)
			w.ActiveCraft().EngineMode = spacecraft.EngineRCS
			return w
		}, func(t *testing.T, w *World) {
			if !w.FireRCSPulse(spacecraft.BurnRadialOut) {
				t.Fatal("pulse refused")
			}
		}, "rcs: Radial+ 0.1 m/s"},
		{"hold change", func(t *testing.T) *World { return mustWorld(t) },
			func(t *testing.T, w *World) {
				w.ActiveCraft().AttitudeMode = spacecraft.BurnTargetRetrograde // a cause that is no key press
				w.Tick()
			}, "hold: Target Retrograde (TGT)"},
		{"warp up", func(t *testing.T) *World { return mustWorld(t) },
			func(t *testing.T, w *World) { w.Clock.WarpUp(); w.Tick() }, "warp: 10x"},
		{"warp down", func(t *testing.T) *World { w := mustWorld(t); w.Clock.WarpIdx = 2; return w },
			func(t *testing.T, w *World) { w.Clock.WarpDown(); w.Tick() }, "warp: 10x"},
		{"warp cancel", func(t *testing.T) *World { w := mustWorld(t); w.Clock.WarpIdx = 2; return w },
			func(t *testing.T, w *World) { w.Clock.WarpIdx = 0; w.Tick() }, "warp: 1x"},
		{"warp auto-warp reset", func(t *testing.T) *World {
			w := mustWorld(t)
			w.Clock.WarpIdx = 3
			w.AutoWarp = &AutoWarpTarget{T: w.Clock.SimTime.Add(-time.Second), Sync: true} // arrived: resolveAutoWarp drops to 1x
			return w
		}, func(t *testing.T, w *World) { w.Tick() }, "warp: 1x"},
		{"proximity on", func(t *testing.T) *World { return proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{}) },
			func(t *testing.T, w *World) {
				if entered, r := w.ToggleProximityView(); !entered {
					t.Fatal(r)
				}
			}, "proximity: on"},
		{"proximity off", func(t *testing.T) *World {
			w := proximityPairWorld(t, orbital.Vec3{X: 1000}, orbital.Vec3{})
			w.ToggleProximityView()
			return w
		}, func(t *testing.T, w *World) { w.ToggleProximityView() }, "proximity: off"},
		{"undock", func(t *testing.T) *World {
			w := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0).w
			w.DockCrafts(0, 1)
			return w
		}, func(t *testing.T, w *World) {
			if !w.Undock(0) {
				t.Fatal("undock refused")
			}
		}, "undock: "},
	}
}

// Guard 5: each lifecycle site writes its own row. Deleting one hook reddens
// exactly its row.
func TestLifecycleRowsName(t *testing.T) {
	for _, tc := range lifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.setup(t)
			dir := recAttach(t, w)
			tc.act(t, w)
			ev := allEvents(t, dir)
			for _, e := range ev {
				if strings.Contains(e, tc.want) {
					return
				}
			}
			t.Errorf("no event containing %q; events = %q", tc.want, ev)
		})
	}
}

// Guard 8: with Recording == nil every hook is silent and nothing panics.
func TestNilRecordingIsSilent(t *testing.T) {
	for _, tc := range lifecycleCases() {
		t.Run(tc.name, func(t *testing.T) {
			w := tc.setup(t)
			tc.act(t, w)
			if w.Recording != nil {
				t.Error("a hook opened a recording by itself")
			}
		})
	}
	w := mustWorld(t)
	for _, a := range []missions.Action{
		missions.ActionThrottleFull, missions.ActionThrottleCut, missions.ActionPlanTransfer, missions.ActionPlanIncl,
		missions.ActionCycleTarget, missions.ActionClearTarget, missions.ActionStage, missions.ActionUndock,
	} {
		w.RecordAction(a)
	}
	w.SetActiveCraftIdx(0)
	w.SetActiveCraftIdx(-1)
	w.SetActiveCraftIdx(len(w.Crafts))
	w.RecordEvent("x")
	w.recordSample(time.Second)
}

// Decision 1 / S3 seam: the liftoff row sits at the stamp (T+ 0:00:00 and the
// stamp's own second) whichever of the three release sites ran, and the header
// learns the liftoff. The clock starts at .99 s so a row written after the
// 0.5 s tick would read the next second.
func TestLiftoffRowIsTPlusZeroAtEverySite(t *testing.T) {
	sites := []struct {
		name string
		run  func(t *testing.T, w *World, c *spacecraft.Spacecraft)
	}{
		{"manual burn", func(t *testing.T, w *World, c *spacecraft.Spacecraft) { w.StartManualBurn() }},
		{"seated then integrate", func(t *testing.T, w *World, c *spacecraft.Spacecraft) {
			full := c.Thrust
			c.Thrust = full * 0.1
			w.StartManualBurn()
			if !c.OnPad {
				t.Fatal("setup: lifted early")
			}
			c.Thrust = full
			w.Clock.WarpIdx = 1
			w.Tick()
		}},
		{"planted node", func(t *testing.T, w *World, c *spacecraft.Spacecraft) {
			c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{TriggerTime: w.Clock.SimTime, Mode: spacecraft.BurnRadialOut, // up, so the pad releases it
				DV: 50, Duration: 5 * time.Second, PrimaryID: c.Primary.ID, Throttle: 1})
			w.Clock.WarpIdx = 1
			w.Tick()
		}},
	}
	for _, s := range sites {
		t.Run(s.name, func(t *testing.T) {
			w, c := padSaturn(t, 1)
			liftable(w, c)
			w.Clock.SimTime = w.Clock.SimTime.Truncate(time.Second).Add(990 * time.Millisecond)
			path := recFile(t, recAttach(t, w))
			s.run(t, w, c)
			if c.OnPad || c.LiftoffSimTime.IsZero() {
				t.Fatalf("setup: did not lift (OnPad=%v stamp=%v)", c.OnPad, c.LiftoffSimTime)
			}
			b, _ := os.ReadFile(path)
			var row string
			for _, l := range strings.Split(string(b), "\n") {
				if strings.HasPrefix(l, "20") && strings.HasSuffix(strings.TrimSpace(l), "liftoff") {
					row = l
				}
			}
			stamp := c.LiftoffSimTime.UTC().Format("2006-01-02 15:04:05")
			f := strings.Fields(row)
			if len(f) < 3 || f[0]+" "+f[1] != stamp || f[2] != "0:00:00" || f[len(f)-3] != "-" || f[len(f)-2] != "-" {
				t.Errorf("liftoff row = %q, want sim %s, T+ 0:00:00 and apsides -", row, stamp)
			}
			if !strings.Contains(string(b), "# T+0 = "+stamp+" (liftoff)") {
				t.Errorf("header does not name the liftoff %s:\n%s", stamp, strings.SplitN(string(b), "\n", 3)[1])
			}
		})
	}
}

// A realistic pad-to-stage flight: the file's columns line up row to row, the
// hold column is the GUIDANCE words, and a no-event row is the 119 B the ADR's
// corrections section measured.
func TestRealisticFlightFile(t *testing.T) {
	w, c := padSaturn(t, 1)
	liftable(w, c)
	path := recFile(t, recAttach(t, w))
	w.RecordAction(missions.ActionThrottleFull)
	w.StartManualBurn()
	w.RecordAction(missions.ActionIgnite)
	ticks(w, 1200) // 60 s of ascent
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	w.RecordAction(missions.ActionStage)
	ticks(w, 400)
	w.RecordAction(missions.ActionThrottleCut)
	b, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	t.Log("\n" + strings.Join(lines, "\n"))
	// Rows from the stage on (mass under 1,000,000 kg, which is all the 7-wide
	// mass column holds): the soi and hold columns must not drift.
	soiCol, holdCol, rows := -1, -1, 0
	for _, l := range lines {
		if !strings.HasPrefix(l, "20") {
			continue
		}
		rows++
		if !strings.Contains(l, "stage: ") && soiCol < 0 {
			continue
		}
		if soiCol < 0 {
			soiCol, holdCol = strings.Index(l, "Earth"), strings.Index(l, "Radial+")
		}
		if strings.Index(l, "Earth") != soiCol || strings.Index(l, "Radial+") != holdCol {
			t.Errorf("columns drift on %q", l)
		}
		if !strings.Contains(l, "  ") || strings.HasSuffix(l, "  ") {
			t.Errorf("bad row %q", l)
		}
	}
	if want := w.holdText(w.ActiveCraft()); want != "Radial+ (ORBIT)" {
		t.Errorf("holdText = %q", want)
	}
	w.ActiveCraft().AttitudeMode = spacecraft.BurnInertial // ADR 0063: the hold column follows
	if want := w.holdText(w.ActiveCraft()); want != "Inertial (ORBIT)" {
		t.Errorf("holdText = %q, want Inertial (ORBIT)", want)
	}
	if rows < 8 {
		t.Errorf("rows = %d, want a flight's worth", rows)
	}
}

// The bump row's speed format: one decimal under 10 m/s judged after rounding
// (9.95 reads "10", not "10.0"), grouped integer from 10.
func TestBumpSpeedText(t *testing.T) {
	for _, c := range []struct {
		s    float64
		want string
	}{{0.3, "0.3"}, {9.94, "9.9"}, {9.95, "10"}, {10.0, "10"}, {1234.4, "1,234"}} {
		if got := bumpSpeedText(c.s); got != c.want {
			t.Errorf("bumpSpeedText(%v) = %q, want %q", c.s, got, c.want)
		}
	}
}

// v0.63.1: no input row when nothing changed (equal case), and a warp press
// saturated at the ladder's end moves nothing.
func TestInputRowsOnlyOnChange(t *testing.T) {
	w := mustWorld(t)
	dir := recAttach(t, w)
	ticks(w, 3)
	w.Clock.WarpDown() // saturated at the floor
	ticks(w, 2)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	w.rec.warp = w.Clock.WarpIdx + 1 // as if already on file
	w.Clock.WarpUp()                 // saturated at the top
	w.ActiveCraft().AttitudeMode = w.ActiveCraft().AttitudeMode
	ticks(w, 3)
	if ev := allEvents(t, dir); len(ev) != 0 {
		t.Errorf("events = %q, want none", ev)
	}
}

// v0.63.1: the hold column is sized to the widest label holdText can produce.
// The table runs every BurnMode; each row's columns start at the header's.
func TestHoldColumnFitsEveryBurnMode(t *testing.T) {
	w, path := recWorld(t)
	c := w.ActiveCraft()
	widest := 0
	for m := spacecraft.BurnPrograde; m <= spacecraft.BurnPortRight; m++ {
		if m.String() == "?" {
			t.Fatalf("BurnMode %d has no label", m)
		}
		c.AttitudeMode = m
		if n := len([]rune(w.holdText(c))); n > widest {
			widest = n
		}
		w.recordRow("x")
	}
	if widest != recorder.HoldWidth {
		t.Errorf("widest hold label = %d runes, HoldWidth = %d", widest, recorder.HoldWidth)
	}
	b, _ := os.ReadFile(path)
	var hdr string
	rows := 0
	for _, l := range strings.Split(string(b), "\n") {
		switch {
		case strings.HasPrefix(l, "sim "):
			hdr = l
		case strings.HasPrefix(l, "20") && strings.HasSuffix(l, "x"):
			rows++
			r := []rune(l)
			h := []rune(hdr)
			if i := strings.Index(string(h), "event"); string(r[i:]) != "x" {
				t.Errorf("event column off\n%s\n%s", hdr, l)
			}
			if i := strings.Index(string(h), "soi"); string(r[i:i+5]) != "Earth" {
				t.Errorf("soi column off\n%s\n%s", hdr, l)
			}
		}
	}
	if rows != int(spacecraft.BurnPortRight)+1 {
		t.Errorf("checked %d rows", rows)
	}
}

// v0.63.1: the soi column is sized to the longest body name in every embedded
// system catalog (Kerbin, Minmus are 6; the table reads the catalogs).
func TestSOIColumnFitsEveryCatalogBody(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no user overlay
	systems, _, err := bodies.LoadAllWithWarnings()
	if err != nil {
		t.Fatal(err)
	}
	longest := ""
	for _, s := range systems {
		for _, b := range s.Bodies {
			if len(b.EnglishName) > len(longest) {
				longest = b.EnglishName
			}
		}
	}
	if len([]rune(longest)) != recorder.SOIWidth {
		t.Errorf("longest body name %q is %d runes, SOIWidth = %d", longest, len([]rune(longest)), recorder.SOIWidth)
	}
	for _, name := range []string{"Moon", "Kerbin", longest} { // longest is the equal case
		w, path := recWorld(t)
		w.ActiveCraft().Primary.EnglishName = name
		w.recordRow("x")
		b, _ := os.ReadFile(path)
		var hdr string
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "sim ") {
				hdr = l
			} else if strings.HasPrefix(l, "20") {
				if i := strings.Index(hdr, "soi"); l[i:i+len(name)] != name {
					t.Errorf("soi column off for %q\n%s\n%s", name, hdr, l)
				}
				if i := strings.Index(hdr, "event"); l[i:] != "x" {
					t.Errorf("event column off for %q\n%s\n%s", name, hdr, l)
				}
			}
		}
	}
}

// v0.63.1: thr reads thrust, not the lever.
func TestThrottleColumnReadsThrust(t *testing.T) {
	w, _ := recWorld(t)
	c := w.ActiveCraft()
	c.Throttle = 1
	if g := w.rowFor(c, "").ThrottlePct; g != 0 {
		t.Errorf("coasting with the lever at 100%%: thr = %v, want 0", g)
	}
	w.StartManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("no burn")
	}
	if g := w.rowFor(c, "").ThrottlePct; g != 100 {
		t.Errorf("burning at 100%%: thr = %v, want 100", g)
	}
}

// Guard 5: the plain minute name when free, -2 then -3 when taken (equal case:
// the free name is used as is).
func TestNewGameFolderCollisions(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 10, 27, 18, 43, 59, 0, time.Local)
	for _, want := range []string{"2026-10-27-1843", "2026-10-27-1843-2", "2026-10-27-1843-3"} {
		got, err := NewGameFolder(root, now)
		if err != nil || got != want {
			t.Fatalf("NewGameFolder = %q, %v; want %q", got, err, want)
		}
		if st, err := os.Stat(filepath.Join(root, got)); err != nil || !st.IsDir() {
			t.Fatalf("folder %q not created: %v", got, err)
		}
	}
}
