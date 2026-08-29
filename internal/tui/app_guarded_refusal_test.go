package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func pressKey(a *App, r rune) (tea.Model, tea.Cmd) {
	return a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

// #282: guarded keys refuse inconsistently — some (the settings host-guard)
// toast a clear reason, others do nothing at all. [E] end-flight on a live
// (non-crashed) vessel was one of the two named cases: it swallowed the key
// with no feedback, so a player couldn't tell "not applicable now" from
// "broken" or "host-gated". Every branch of the end-flight trigger must now
// say something.
func TestEndFlightRefusesLiveVessel(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("fresh world has no active craft")
	}
	c.Crashed = false

	pressKey(a, 'E')

	if a.endFlightConfirm {
		t.Fatal("[E] armed the end-flight confirm on a live vessel")
	}
	if a.statusMsg != "end flight: vessel is not wreckage" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "end flight: vessel is not wreckage")
	}
}

// TestEndFlightArmsConfirmForCrashedVessel is the control: the existing
// happy path (Crashed vessel opens the y/n prompt) must still work
// unchanged by the refusal fix.
func TestEndFlightArmsConfirmForCrashedVessel(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c := a.world.ActiveCraft()
	if c == nil {
		t.Fatal("fresh world has no active craft")
	}
	c.Crashed = true

	pressKey(a, 'E')

	if !a.endFlightConfirm {
		t.Fatal("[E] on a crashed vessel did not arm the end-flight confirm")
	}
}

// TestEndFlightRefusesNoVessel covers the other ActiveCraft() nil edge —
// reachable after end-flight has already removed the last vessel from the
// slate. "vessel is not wreckage" would be a nonsensical thing to say when
// there is no vessel at all, so this gets its own reason.
func TestEndFlightRefusesNoVessel(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.Crafts = nil
	a.world.ActiveCraftIdx = 0

	pressKey(a, 'E')

	if a.endFlightConfirm {
		t.Fatal("[E] armed the end-flight confirm with no active craft")
	}
	if a.statusMsg != "end flight: no vessel" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "end flight: no vessel")
	}
}

// #282 review finding: [K] plan-rendezvous-nudge was originally fixed by
// adding an `a.active != screenOrbit` guard alongside the (correct)
// CraftVisibleHere() guard. That was wrong — on main, K carries no
// `a.active == screenOrbit` check (unlike its sibling planning keys), and
// the KeyMsg dispatcher only lets screenOrbit/screenBodyInfo/screenMissions
// fall through to this switch (every other screen — Menu, Spawn, Settings,
// Controls, VAB, Saves, Session, Maneuver, Help, Porkchop, Boss — early-
// returns above it). So K has always worked from body-info and missions too;
// the "orbit view only" refusal silently narrowed a working keybinding
// instead of fixing a silent no-op. These tests pin that K still reaches
// PlanRendezvousNudge (rather than being refused up front) from those two
// screens — using a fresh world with no target, so the reachable code path
// is proven by getting the domain error ("no vessel target") rather than
// the screen-gate refusal.
func TestPlanRendezvousReachesPlannerFromBodyInfo(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenBodyInfo

	pressKey(a, 'K')

	if a.statusMsg != "rendezvous: no vessel target" {
		t.Errorf("statusMsg = %q, want %q (K must reach PlanRendezvousNudge from body-info, not be screen-gated)", a.statusMsg, "rendezvous: no vessel target")
	}
	if a.active != screenBodyInfo {
		t.Errorf("active screen changed to %v", a.active)
	}
}

func TestPlanRendezvousReachesPlannerFromMissions(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.active = screenMissions

	pressKey(a, 'K')

	if a.statusMsg != "rendezvous: no vessel target" {
		t.Errorf("statusMsg = %q, want %q (K must reach PlanRendezvousNudge from missions, not be screen-gated)", a.statusMsg, "rendezvous: no vessel target")
	}
	if a.active != screenMissions {
		t.Errorf("active screen changed to %v", a.active)
	}
}

// TestPlanRendezvousRefusesCraftNotVisibleFromBodyInfoAndMissions pins that
// the legitimate #282 guard (CraftVisibleHere) still applies uniformly no
// matter which of the three reachable screens (orbit/body-info/missions) K
// is pressed from — that guard was always load-bearing, unlike the removed
// screen gate.
func TestPlanRendezvousRefusesCraftNotVisibleFromBodyInfoAndMissions(t *testing.T) {
	cases := []struct {
		name   string
		screen screenID
	}{
		{"body-info", screenBodyInfo},
		{"missions", screenMissions},
	}
	for _, tc := range cases {
		screen := tc.screen
		t.Run(tc.name, func(t *testing.T) {
			a, err := New(nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if len(a.world.Systems) < 2 {
				t.Skip("need a second loaded system to browse away from the craft's own")
			}
			a.world.CycleSystem() // browse away from the active craft's bound system
			if a.world.CraftVisibleHere() {
				t.Fatal("test setup: craft is still visible after CycleSystem")
			}
			a.active = screen

			pressKey(a, 'K')

			if a.statusMsg != "rendezvous: vessel not in this system" {
				t.Errorf("statusMsg = %q, want %q", a.statusMsg, "rendezvous: vessel not in this system")
			}
		})
	}
}

func TestPlanRendezvousRefusesCraftNotVisible(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(a.world.Systems) < 2 {
		t.Skip("need a second loaded system to browse away from the craft's own")
	}
	a.active = screenOrbit
	a.world.CycleSystem() // browse away from the active craft's bound system
	if a.world.CraftVisibleHere() {
		t.Fatal("test setup: craft is still visible after CycleSystem")
	}

	pressKey(a, 'K')

	if a.statusMsg != "rendezvous: vessel not in this system" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "rendezvous: vessel not in this system")
	}
}

// #282 sweep: PlanTransfer [H], PlanIncl [I], PlanCircularize [C], and
// RefinePlan [R] share PlanRendezvous's exact "if CraftVisibleHere() { ...
// }" no-else shape in the same input path — same silent no-op when the
// player is browsing a system their vessel isn't bound to. Table-driven
// since the fix is identical for each key.
func TestPlanningKeysRefuseWhenCraftNotVisible(t *testing.T) {
	cases := []struct {
		name string
		key  rune
		want string
	}{
		{"PlanTransfer", 'H', "transfer: vessel not in this system"},
		{"PlanIncl", 'I', "inclination: vessel not in this system"},
		{"PlanCircularize", 'C', "circularize: vessel not in this system"},
		{"PlanRendezvous", 'K', "rendezvous: vessel not in this system"},
		{"RefinePlan", 'R', "refine: vessel not in this system"},
		{"Maneuver", 'm', "maneuver: vessel not in this system"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, err := New(nil)
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			if len(a.world.Systems) < 2 {
				t.Skip("need a second loaded system to browse away from the craft's own")
			}
			a.active = screenOrbit
			a.world.CycleSystem()
			if a.world.CraftVisibleHere() {
				t.Fatal("test setup: craft is still visible after CycleSystem")
			}
			startScreen := a.active

			pressKey(a, tc.key)

			if a.statusMsg != tc.want {
				t.Errorf("statusMsg = %q, want %q", a.statusMsg, tc.want)
			}
			if a.active != startScreen {
				t.Errorf("active screen changed to %v on a refused key", a.active)
			}
		})
	}
}

// TestPorkchopRefusesReasons: the porkchop key on the orbit screen has two
// independent state guards (a visible vessel and a selected body) that used
// to fail silently together. Each must name itself.
func TestPorkchopRefusesReasons(t *testing.T) {
	t.Run("craft not visible", func(t *testing.T) {
		a, err := New(nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		if len(a.world.Systems) < 2 {
			t.Skip("need a second loaded system to browse away from the craft's own")
		}
		a.active = screenOrbit
		a.selectedBody = 1
		a.world.CycleSystem()

		pressKey(a, 'P')

		if a.statusMsg != "porkchop: vessel not in this system" {
			t.Errorf("statusMsg = %q, want %q", a.statusMsg, "porkchop: vessel not in this system")
		}
		if a.active != screenOrbit {
			t.Errorf("active screen changed to %v on a refused [P]", a.active)
		}
	})

	t.Run("no body selected", func(t *testing.T) {
		a, err := New(nil)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		a.active = screenOrbit
		a.selectedBody = 0

		pressKey(a, 'P')

		if a.statusMsg != "porkchop: no body selected" {
			t.Errorf("statusMsg = %q, want %q", a.statusMsg, "porkchop: no body selected")
		}
		if a.active != screenOrbit {
			t.Errorf("active screen changed to %v on a refused [P]", a.active)
		}
	})
}

// TestRendezvousRefusalLabelsOnce (see app_rendezvous_refusal_test.go)
// already pins that a K refusal must carry the "rendezvous:" label exactly
// once. Sanity-check here that the surviving visibility guard obeys the
// same rule when triggered from a non-orbit screen (missions), not just
// from orbit.
func TestPlanRendezvousRefusalLabelStillSingular(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(a.world.Systems) < 2 {
		t.Skip("need a second loaded system to browse away from the craft's own")
	}
	a.world.CycleSystem()
	if a.world.CraftVisibleHere() {
		t.Fatal("test setup: craft is still visible after CycleSystem")
	}
	a.active = screenMissions
	pressKey(a, 'K')
	if n := strings.Count(a.statusMsg, "rendezvous:"); n != 1 {
		t.Errorf("status %q carries the rendezvous label %d times, want exactly 1", a.statusMsg, n)
	}
}
