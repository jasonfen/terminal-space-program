package tui

// Red tests from UX cycle 3 Phase 4 triage T1 (#587, #588). Each pins a
// defect reproduced on main through the production seam (App.Update for
// keys and ticks). They are expected to FAIL until the fix lands; they
// assert the player-visible outcome, not a fix shape.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func triageBodyIdx(t *testing.T, a *App, name string) int {
	t.Helper()
	for i, b := range a.world.System().Bodies {
		if b.EnglishName == name {
			return i
		}
	}
	t.Fatalf("no body %q in %s", name, a.world.System().Name)
	return -1
}

func triagePlanTotals(a *App) (n int, dv float64) {
	for _, node := range a.world.ActiveCraft().Nodes {
		dv += node.DV
	}
	return len(a.world.ActiveCraft().Nodes), dv
}

func triageTick(a *App, n int) {
	for i := 0; i < n; i++ {
		a.Update(sim.TickMsg(time.Now()))
	}
}

// #587: a second H with a transfer already planted must not stack a
// second copy of the transfer. Refusing (plan unchanged) and replacing
// (one fresh plan) both leave the node count and total Δv where the
// first H put them; today the second H appends a duplicate of every
// burn (3 nodes / 3785 m/s -> 6 nodes / 7570 m/s on a 6129 m/s vessel).
func TestSecondTransferDoesNotStackOnPlanted(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, "Moon")}

	pressKey(a, 'H')
	n1, dv1 := triagePlanTotals(a)
	if n1 == 0 {
		t.Fatal("setup: first H planted nothing")
	}
	pressKey(a, 'H')
	n2, dv2 := triagePlanTotals(a)

	if n2 != n1 {
		t.Errorf("second H changed the node count %d -> %d: the transfer was stacked", n1, n2)
	}
	if dv2 > dv1*1.01 {
		t.Errorf("second H raised the planned Δv %.0f -> %.0f m/s (budget %.0f m/s)",
			dv1, dv2, a.world.ActiveCraft().RemainingDeltaV())
	}
}

// #588 lead 1: an H at an inward planet plants a centred departure at
// T+0, so it is already burning on the next tick. Clearing the plan in
// the planner (ctrl+k) empties the node list but leaves that burn
// running: the engine keeps spending propellant with nothing in the
// plan (18.8 t in the triage run), across a save/load too. After the
// player clears every node, nothing from that plan may still burn.
func TestPlannerClearAllLeavesNoBurnRunning(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, "Mercury")}
	pressKey(a, 'H')
	triageTick(a, 20) // 1 s at 1x: the T+0 departure ignites (RK4 burn path)
	if a.world.ActiveCraft().ActiveBurn == nil {
		t.Fatal("setup: the Mercury departure did not ignite, so this test proves nothing")
	}

	pressKey(a, 'm')
	if a.active != screenManeuver {
		t.Fatal("setup: planner did not open")
	}
	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyCtrlK})
	if cmd == nil {
		t.Fatal("setup: ctrl+k produced no Cmd")
	}
	a.Update(cmd())
	if n := len(a.world.ActiveCraft().Nodes); n != 0 {
		t.Fatalf("setup: ctrl+k left %d nodes", n)
	}

	m0 := a.world.ActiveCraft().TotalMass()
	triageTick(a, 200) // 10 s at 1x
	burned := m0 - a.world.ActiveCraft().TotalMass()

	if ab := a.world.ActiveCraft().ActiveBurn; ab != nil {
		t.Errorf("plan cleared, but a %v burn is still running with %.0f m/s to go", ab.Mode, ab.DVRemaining)
	}
	if burned > 0 {
		t.Errorf("plan cleared, but the engine burned %.0f kg over the next 10 s", burned)
	}
}

// Found while triaging #587's 23,730 m/s node: H at a moon of ANOTHER
// planet falls through to the heliocentric Hohmann branch, which reads
// the moon's planet-relative semimajor axis as a distance from the Sun.
// From LEO, Phobos plants a ~24 km/s Retrograde departure at T+0 (it
// fires on the next tick) and a ~985 km/s arrival. A Phobos transfer
// leaves Earth like a Mars transfer, so either refuse, or plan one whose
// burns are of the same order as the Mars plan's.
func TestTransferToAnotherPlanetsMoonIsSizedLikeItsPlanet(t *testing.T) {
	mars, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mars.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, mars, "Mars")}
	pressKey(mars, 'H')
	maxMars := 0.0
	for _, n := range mars.world.ActiveCraft().Nodes {
		if n.DV > maxMars {
			maxMars = n.DV
		}
	}
	if maxMars == 0 {
		t.Fatal("setup: the Mars H planted nothing")
	}

	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, "Phobos")}
	pressKey(a, 'H')
	for i, n := range a.world.ActiveCraft().Nodes {
		if n.DV > 2*maxMars {
			t.Errorf("Phobos node #%d is %v %.0f m/s; the Mars plan's largest burn is %.0f m/s",
				i+1, n.Mode, n.DV, maxMars)
		}
	}
}

// #588 lead 2's `downrange 17635.4 km`: the launch view measures
// downrange from (LaunchLatDeg, LaunchLonDeg), which is (0, 0) for a
// vessel that spawned in orbit. The seed vessel has never launched, yet
// at T+ 00:00:00 the launch view reads `downrange: 8607 km`. A vessel
// that never left a pad has no downrange to report: a dash, no row, or
// zero at T+0 are all fine; thousands of km from the Gulf of Guinea are
// not.
func TestLaunchViewDownrangeForAnOrbitSpawnIsNotFromLatLonZero(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	if c := a.world.ActiveCraft(); c.Landed || c.OnPad {
		t.Fatal("setup: the seed vessel should be in orbit, not on a pad")
	}
	pressKey(a, 'V')
	if a.world.ViewMode != sim.ViewLaunch {
		t.Fatalf("setup: V did not open the launch view (ViewMode %v)", a.world.ViewMode)
	}
	frame := a.View()
	m := regexp.MustCompile(`downrange: ([0-9.]+) (m|km|Mm)`).FindStringSubmatch(frame)
	if m == nil {
		return // no downrange shown: acceptable
	}
	v, _ := strconv.ParseFloat(m[1], 64)
	switch m[2] {
	case "km":
		v *= 1e3
	case "Mm":
		v *= 1e6
	}
	if v >= 1000 {
		line := ""
		for _, l := range strings.Split(frame, "\n") {
			if strings.Contains(l, "downrange:") {
				line = strings.TrimSpace(l)
			}
		}
		t.Errorf("never-launched orbital vessel shows downrange %s %s at session start: %q", m[1], m[2], line)
	}
}
