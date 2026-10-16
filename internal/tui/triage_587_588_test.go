package tui

// Tests born as red tests in UX cycle 3 Phase 4 triage T1 (#587, #588, #610). Each pins a
// defect reproduced on main through the production seam (App.Update for
// keys and ticks). They are expected to FAIL until the fix lands; they
// assert the player-visible outcome, not a fix shape.

import (
	"regexp"
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
func TestSecondTransferRefusesWhilePlanted(t *testing.T) {
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

	// GS Q6: the second H refuses and names the planted burn with the
	// time ENGINE's node: row shows (the burn's start).
	want := regexp.MustCompile(`^transfer: a burn is already planted \(Prograde 3068 m/s in \d+m\d\ds\); clear it in \[m\] first$`)
	if !want.MatchString(a.statusMsg) {
		t.Errorf("second H flash = %q, want the planted-transfer refusal", a.statusMsg)
	}
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

	if !strings.Contains(a.statusMsg, "burn stopped") {
		t.Errorf("clearing a firing burn said %q, want a flash naming the stopped burn", a.statusMsg)
	}
	if ab := a.world.ActiveCraft().ActiveBurn; ab != nil {
		t.Errorf("plan cleared, but a %v burn is still running with %.0f m/s to go", ab.Mode, ab.DVRemaining)
	}
	if burned > 0 {
		t.Errorf("plan cleared, but the engine burned %.0f kg over the next 10 s", burned)
	}
}

// Found while triaging #587's 23,730 m/s node (#610): H at a moon of
// ANOTHER planet used to fall through to the heliocentric Hohmann branch,
// which read the moon's planet-relative semimajor axis as a distance from
// the Sun: from LEO, Phobos planted a ~24 km/s Retrograde departure at T+0
// (it fired on the next tick) and a ~1.5 million m/s arrival. Jason
// (2026-10-11): planning to another planet's moon is beyond the scope of
// the planning assists, so H refuses with a caption naming the planet and
// plants nothing. Mars (a planet) still plants, as the positive control.
func TestTransferToAnotherPlanetsMoonRefuses(t *testing.T) {
	mars, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	mars.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, mars, "Mars")}
	pressKey(mars, 'H')
	if n, _ := triagePlanTotals(mars); n == 0 {
		t.Fatal("setup: the Mars H planted nothing, so a refusal at Phobos proves nothing")
	}

	for _, moon := range []string{"Phobos", "Deimos", "Io", "Titan"} {
		a, err := New(nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, moon)}
		pressKey(a, 'H')
		if n, dv := triagePlanTotals(a); n != 0 {
			t.Errorf("%s: H planted %d nodes (%.0f m/s); a moon of another planet must plant nothing", moon, n, dv)
		}
		if ab := a.world.ActiveCraft().ActiveBurn; ab != nil {
			t.Errorf("%s: H left a %v burn running", moon, ab.Mode)
		}
		if !strings.HasPrefix(a.statusMsg, "transfer: H plans to a planet or a moon of your own planet; target ") ||
			!strings.HasSuffix(a.statusMsg, " first") {
			t.Errorf("%s: flash = %q, want the other-planet's-moon refusal", moon, a.statusMsg)
		}
	}
	// The caption names the moon's own planet.
	a, _ := New(nil)
	a.world.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: triageBodyIdx(t, a, "Phobos")}
	pressKey(a, 'H')
	if !strings.HasSuffix(a.statusMsg, "target Mars first") {
		t.Errorf("Phobos flash = %q, want it to send the player to Mars", a.statusMsg)
	}
}
