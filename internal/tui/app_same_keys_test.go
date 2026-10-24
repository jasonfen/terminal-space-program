package tui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0052 (#493, slice B1): same keys in every flight view, arrows trim,
// pan moves to shift+arrows, tilt/yaw to < > { }, and D / Y ask first.
// Every key here is driven through the real App.Update path.

func keyType(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t} }

func keyAlt(t tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: t, Alt: true} }

func keyRunes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

// padApp is a 140x40 App with a Saturn V on the pad as the active vessel.
func padApp(t *testing.T) (*App, *spacecraft.Spacecraft) {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c, err := a.world.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.View()
	return a, c
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// TestArrowsTrimInEveryFlightView: left/right pitch west/east, up/down
// heading north/south, identically on the map, the V launch view and every
// projection (decisions 1 and 2). This is also the positive control for the
// modal table below: the instrument can move the trims.
func TestArrowsTrimInEveryFlightView(t *testing.T) {
	for _, vm := range []sim.ViewMode{sim.ViewTilted, sim.ViewTop, sim.ViewLaunch} {
		a, c := padApp(t)
		a.world.ViewMode = vm
		step := spacecraft.PitchTrimStepRad
		hstep := spacecraft.HeadingTrimStepRad

		a.Update(keyType(tea.KeyRight))
		if !near(c.PitchTrim, step) {
			t.Errorf("view %v: right: PitchTrim = %v, want +%v (east)", vm, c.PitchTrim, step)
		}
		a.Update(keyType(tea.KeyLeft))
		a.Update(keyType(tea.KeyLeft))
		if !near(c.PitchTrim, -step) {
			t.Errorf("view %v: left x2: PitchTrim = %v, want -%v (west)", vm, c.PitchTrim, step)
		}
		a.Update(keyType(tea.KeyUp))
		if !near(c.HeadingTrim, -hstep) {
			t.Errorf("view %v: up: HeadingTrim = %v, want -%v (toward north)", vm, c.HeadingTrim, hstep)
		}
		a.Update(keyType(tea.KeyDown))
		a.Update(keyType(tea.KeyDown))
		if !near(c.HeadingTrim, hstep) {
			t.Errorf("view %v: down x2: HeadingTrim = %v, want +%v (toward south)", vm, c.HeadingTrim, hstep)
		}
		a.Update(keyRunes("|"))
		if c.PitchTrim != 0 || c.HeadingTrim != 0 {
			t.Errorf("view %v: | left PitchTrim=%v HeadingTrim=%v, want both 0", vm, c.PitchTrim, c.HeadingTrim)
		}
	}
}

// TestHeadingArrowsReadTheCorrectCompassDirection: the sign guard that used
// to live on { and }: from due east, up reads 085 and down reads 095 on the
// pad's absolute-heading readout.
func TestHeadingArrowsReadTheCorrectCompassDirection(t *testing.T) {
	a, c := padApp(t)
	deg := func() float64 { return (spacecraft.HeadingTrimDueEastRad + c.HeadingTrim) * 180 / math.Pi }
	a.Update(keyType(tea.KeyUp))
	if got := deg(); math.Abs(got-85) > 1e-6 {
		t.Errorf("after up: heading %.3f deg, want 85", got)
	}
	a.Update(keyRunes("|"))
	a.Update(keyType(tea.KeyDown))
	if got := deg(); math.Abs(got-95) > 1e-6 {
		t.Errorf("after down: heading %.3f deg, want 95", got)
	}
}

// TestShiftArrowsPanAndDoNotTrim: pan moved to shift+arrows (decision 3).
// The observable contract is the render: shift+left moves the map, and the
// trims stay put.
func TestShiftArrowsPanAndDoNotTrim(t *testing.T) {
	a, c := padApp(t)
	a.world.ViewMode = sim.ViewTop
	for _, kt := range []tea.KeyType{tea.KeyShiftLeft, tea.KeyShiftRight, tea.KeyShiftUp, tea.KeyShiftDown} {
		before := a.View()
		a.Update(keyType(kt))
		after := a.View()
		if before == after {
			t.Errorf("%v did not pan the map (render unchanged)", kt)
		}
		if c.PitchTrim != 0 || c.HeadingTrim != 0 {
			t.Errorf("%v moved a trim: pitch %v heading %v", kt, c.PitchTrim, c.HeadingTrim)
		}
	}
}

// TestTiltAndYawMovedToAngleAndBraceKeys: < > tilt, { } yaw, camera only
// (decision 4); the trims and the vessel never move.
func TestTiltAndYawMovedToAngleAndBraceKeys(t *testing.T) {
	a, c := padApp(t)
	if a.world.ViewMode != sim.ViewTilted {
		t.Fatalf("want default ViewTilted, got %v", a.world.ViewMode)
	}
	t0 := a.world.ViewTilt.Theta
	a.Update(keyRunes(">"))
	if got := a.world.ViewTilt.Theta; got != t0+sim.ViewTiltThetaStep {
		t.Errorf("> : Theta = %v, want %v", got, t0+sim.ViewTiltThetaStep)
	}
	a.Update(keyRunes("<"))
	a.Update(keyRunes("<"))
	if got := a.world.ViewTilt.Theta; got != t0-sim.ViewTiltThetaStep {
		t.Errorf("< x2 : Theta = %v, want %v", got, t0-sim.ViewTiltThetaStep)
	}
	p0 := a.world.ViewTilt.Phi
	a.Update(keyRunes("}"))
	if got := a.world.ViewTilt.Phi; got != math.Mod(p0+sim.ViewTiltPhiStep+360, 360) {
		t.Errorf("} : Phi = %v, want %v", got, p0+sim.ViewTiltPhiStep)
	}
	a.Update(keyRunes("{"))
	a.Update(keyRunes("{"))
	if got := a.world.ViewTilt.Phi; got != math.Mod(p0-sim.ViewTiltPhiStep+360, 360) {
		t.Errorf("{ x2 : Phi = %v, want %v", got, p0-sim.ViewTiltPhiStep)
	}
	if c.PitchTrim != 0 || c.HeadingTrim != 0 {
		t.Errorf("< > { } moved a trim: pitch %v heading %v", c.PitchTrim, c.HeadingTrim)
	}
	// Outside the tilted view they refuse out loud, as the shift+arrows did.
	a.world.ViewMode = sim.ViewTop
	a.statusMsg = ""
	a.Update(keyRunes(">"))
	if !strings.Contains(a.statusMsg, "tilt: only in the tilted view") {
		t.Errorf("> in ViewTop: statusMsg = %q, want the tilt refusal", a.statusMsg)
	}
}

// TestWarpAndVesselCycleKeysUnchanged: . , warp and [ ] vessel cycle keep
// their keycaps (decision 4).
func TestWarpAndVesselCycleKeysUnchanged(t *testing.T) {
	k := DefaultKeymap()
	for name, got := range map[string][]string{
		"WarpUp": k.WarpUp.Keys(), "WarpDown": k.WarpDown.Keys(),
	} {
		if len(got) != 1 || (got[0] != "." && got[0] != ",") {
			t.Errorf("%s keys = %v, want . or ,", name, got)
		}
	}
	a, c := padApp(t)
	a.Update(keyRunes("."))
	a.Update(keyRunes(","))
	a.Update(keyRunes("["))
	a.Update(keyRunes("]"))
	if c.PitchTrim != 0 || c.HeadingTrim != 0 {
		t.Errorf(". , [ ] moved a trim: pitch %v heading %v", c.PitchTrim, c.HeadingTrim)
	}
}

// modalCase puts the App into one input-owning surface through the real key
// path (or the flag a key sets), returning false when the setup is unusable.
type modalCase struct {
	name  string
	enter func(t *testing.T, a *App)
	// rearm: the surface dismisses itself on any key (the F9 quickload
	// confirm), so it is re-entered before every press to keep each press
	// landing on the modal.
	rearm bool
}

func escThen(r string) func(t *testing.T, a *App) {
	return func(t *testing.T, a *App) {
		a.Update(keyType(tea.KeyEsc))
		if a.active != screenMenu {
			t.Fatalf("esc did not open the menu (active=%v)", a.active)
		}
		if r != "" {
			a.Update(keyRunes(r))
		}
	}
}

// TestModalsOwningArrowsLeaveTrimsAlone: every surface that owns the arrow
// keys claims them before the flight-key switch (ADR 0052 implementation
// notes). Arrow presses (plain and shifted) in each leave both trims at
// zero. A modal that forgot to claim arrows now bleeds into the trims, which
// is worse than bleeding into the camera.
func TestModalsOwningArrowsLeaveTrimsAlone(t *testing.T) {
	cases := []modalCase{
		{name: "pause menu", enter: escThen("")},
		{name: "settings", enter: escThen("t")},
		{name: "keyboard layout", enter: escThen("k")},
		{name: "VAB", enter: escThen("b")},
		{name: "saves (menu Save)", enter: escThen("s")},
		{name: "help overlay", enter: func(t *testing.T, a *App) { a.Update(keyType(tea.KeyF1)) }},
		{name: "spawn form", enter: func(t *testing.T, a *App) { a.Update(keyRunes("n")) }},
		{name: "maneuver planner", enter: func(t *testing.T, a *App) { a.Update(keyRunes("m")) }},
		{name: "session roster", enter: func(t *testing.T, a *App) { a.Update(keyRunes("O")) }},
		{name: "mission ladder", enter: func(t *testing.T, a *App) { a.Update(keyRunes("M")) }},
		{name: "body info", enter: func(t *testing.T, a *App) { a.Update(keyRunes("i")) }},
		{name: "porkchop", enter: func(t *testing.T, a *App) { a.active = screenPorkchop }},
		{name: "chat input", enter: func(t *testing.T, a *App) { a.chatOpen = true }},
		{name: "end-flight confirm", enter: func(t *testing.T, a *App) { a.askEndFlight("wreck") }},
		{name: "quickload confirm", enter: func(t *testing.T, a *App) { a.askQuickload() }},
		{name: "transpose confirm", enter: func(t *testing.T, a *App) { a.askTranspose() }},
		{name: "deploy confirm", enter: func(t *testing.T, a *App) { a.askDeploy("payload") }},
		{name: "release batch confirm", enter: func(t *testing.T, a *App) {
			a.askReleaseBatch("Starlink", 24, "they fan out sideways, 0.8 cm/s a seam, top 19")
		}},
		{name: "quit confirm", enter: func(t *testing.T, a *App) { a.armQuit() }},
		{name: "boss shell", enter: func(t *testing.T, a *App) { a.Update(keyRunes("`")) }},
	}
	arrows := []tea.KeyMsg{keyType(tea.KeyLeft), keyType(tea.KeyRight), keyType(tea.KeyUp), keyType(tea.KeyDown),
		keyType(tea.KeyShiftLeft), keyType(tea.KeyShiftRight), keyType(tea.KeyShiftUp), keyType(tea.KeyShiftDown),
		// #460: the alt FINE trims are claimed by the same modals.
		keyAlt(tea.KeyLeft), keyAlt(tea.KeyRight), keyAlt(tea.KeyUp), keyAlt(tea.KeyDown)}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, c := padApp(t)
			tc.enter(t, a)
			for _, kt := range arrows {
				if tc.rearm {
					tc.enter(t, a)
				}
				if _, _, panicked := safeUpdate(a, kt); panicked {
					t.Fatalf("%v panicked in %s", kt, tc.name)
				}
				// Checked after EVERY press: left/right and up/down would
				// cancel each other in a single end-of-run check.
				if c.PitchTrim != 0 || c.HeadingTrim != 0 {
					t.Fatalf("%s: %v moved a trim: pitch %v heading %v", tc.name, kt, c.PitchTrim, c.HeadingTrim)
				}
			}
		})
	}
}

// TestRendezvousPickerKeepsPlainArrows: moving pan to shift+arrows must not
// move the picker. Plain arrows still walk it (through its own bindings),
// and neither plain nor shifted arrows touch the trims while it is open.
func TestRendezvousPickerKeepsPlainArrows(t *testing.T) {
	a := rendezvousPickerPhaseMismatchApp(t)
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.View()
	pressRune(a, 'K')
	if !a.orbitView.RendezvousPickerOpen() {
		t.Fatal("setup: picker did not open")
	}
	c := a.world.ActiveCraft()
	start := a.orbitView.RendezvousPickerOrbit()
	a.Update(keyType(tea.KeyRight))
	if a.orbitView.RendezvousPickerOrbit() == start {
		t.Error("plain right no longer walks the picker's Rendezvous Orbit")
	}
	for _, kt := range []tea.KeyType{tea.KeyRight, tea.KeyLeft, tea.KeyUp, tea.KeyDown, tea.KeyShiftLeft, tea.KeyShiftUp} {
		a.Update(keyType(kt))
		if c.PitchTrim != 0 || c.HeadingTrim != 0 {
			t.Fatalf("picker: %v moved a trim: pitch %v heading %v", kt, c.PitchTrim, c.HeadingTrim)
		}
	}
}

// TestSpaceStagesInstantlyOnThePad: no prompt, no hold (decision 5). One
// space on the pad drops the first stage and flashes what it dropped.
func TestSpaceStagesInstantlyOnThePad(t *testing.T) {
	a, c := padApp(t)
	n := len(c.Stages)
	a.Update(keyRunes(" "))
	if got := len(a.world.ActiveCraft().Stages); got != n-1 {
		t.Fatalf("space on the pad: %d stages -> %d, want %d (instant drop)", n, got, n-1)
	}
	if !strings.Contains(a.statusMsg, "dropped S-IC") || strings.Contains(a.statusMsg, "—") {
		t.Errorf("flash = %q, want a dash-free 'dropped S-IC ...'", a.statusMsg)
	}
	if a.card != nil || strings.Contains(a.View(), "[y/n]") {
		t.Error("staging raised a y/n prompt")
	}
}

// apolloReadyApp is a pad-less Apollo stack staged down to
// [Descent, Ascent, SM, CM], where D is actionable.
func apolloReadyApp(t *testing.T) *App {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = a.world.Crafts[0].Primary
	stack.State = a.world.Crafts[0].State
	a.world.Crafts[0] = stack
	a.world.ActiveCraftIdx = 0
	for i := 0; i < 3; i++ {
		if _, _, err := a.world.StageActive(0); err != nil {
			t.Fatalf("decouple #%d: %v", i, err)
		}
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.View()
	return a
}

// TestTransposeAsksBeforeActing (decision 7): D raises a card; n and esc keep
// the stack; y, y transposes (the action asks once more); other keys are
// swallowed while asking.
func TestTransposeAsksBeforeActing(t *testing.T) {
	a := apolloReadyApp(t)
	c := a.world.ActiveCraft()
	if c.Stages[0].Name != "Descent" {
		t.Fatalf("setup: Stages[0] = %q, want Descent", c.Stages[0].Name)
	}
	a.Update(keyRunes("D"))
	if c.Stages[0].Name != "Descent" {
		t.Fatal("D transposed on the spot, it must ask first")
	}
	if !cardKindUp(a, cardTranspose) || !strings.Contains(a.View(), "Transpose?") {
		t.Fatalf("D raised no transpose card:\n%s", a.View())
	}
	// A stray flight key is swallowed, the ask stays.
	a.Update(keyRunes("w"))
	if !cardKindUp(a, cardTranspose) {
		t.Error("an unrelated key dismissed the ask")
	}
	a.Update(keyRunes("n"))
	if cardKindUp(a, cardTranspose) || c.Stages[0].Name != "Descent" {
		t.Error("n did not cancel cleanly")
	}
	a.Update(keyRunes("D"))
	a.Update(keyType(tea.KeyEsc))
	if cardKindUp(a, cardTranspose) || c.Stages[0].Name != "Descent" {
		t.Error("esc did not cancel cleanly")
	}
	a.Update(keyRunes("D"))
	pressTwice(a, 'y')
	if got := a.world.ActiveCraft().Stages[0].Name; got != "SM" {
		t.Errorf("y: Stages[0] = %q, want SM (transposed)", got)
	}
}

func deployApp(t *testing.T) (*App, *spacecraft.Spacecraft) {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	sic, _ := spacecraft.BuildStage(spacecraft.StageModuleSICID)
	sivb, _ := spacecraft.BuildStage(spacecraft.StageModuleSIVBID)
	csm, _ := spacecraft.BuildStage(spacecraft.StageModuleCSMID)
	c, err := a.world.SpawnCraft(sim.SpawnSpec{
		CustomStages:    []spacecraft.Stage{sic, sivb, csm},
		NosePayloadPlan: []int{1, 1},
		ParentBodyID:    "earth",
		AltitudeM:       400e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.View()
	return a, c
}

// TestDeployAsksBeforeActing (decision 7): Y asks, n cancels, y deploys.
func TestDeployAsksBeforeActing(t *testing.T) {
	a, _ := deployApp(t)
	n := len(a.world.Crafts)
	a.Update(keyRunes("Y"))
	if len(a.world.Crafts) != n {
		t.Fatal("Y deployed on the spot, it must ask first")
	}
	if !cardKindUp(a, cardDeploy) || !strings.Contains(a.View(), "Deploy ") {
		t.Fatalf("Y raised no deploy card:\n%s", a.View())
	}
	a.Update(keyRunes("n"))
	if len(a.world.Crafts) != n || cardKindUp(a, cardDeploy) {
		t.Error("n did not cancel cleanly")
	}
	a.Update(keyRunes("Y"))
	pressTwice(a, 'y')
	if len(a.world.Crafts) != n+1 {
		t.Errorf("y: %d vessels, want %d", len(a.world.Crafts), n+1)
	}
}

// TestVerbRefusesBeforeAsking: a verb that would refuse is refused first;
// no ask is raised. Deploy with no payload; transpose on a stack that is
// not transpose-ready (Saturn V on the pad, not [Descent Ascent SM CM]).
func TestVerbRefusesBeforeAsking(t *testing.T) {
	for _, c := range []struct{ name, key, refusal string }{
		{"deploy", "Y", "deploy:"},
		{"transpose", "D", "transpose:"},
	} {
		t.Run(c.name, func(t *testing.T) {
			a, _ := padApp(t)
			a.Update(keyRunes(c.key))
			if a.card != nil || strings.Contains(a.View(), "[y/n]") {
				t.Errorf("%s asked about a %s that would refuse", c.key, c.name)
			}
			if !strings.Contains(a.statusMsg, c.refusal) {
				t.Errorf("%s gave no refusal, statusMsg = %q", c.key, a.statusMsg)
			}
		})
	}
}

// TestUndockAndTransferStayInstant: U and J do not ask (decision 7).
func TestUndockAndTransferStayInstant(t *testing.T) {
	a, _ := deployApp(t)
	a.Update(keyRunes("U"))
	if a.card != nil || strings.Contains(a.View(), "[y/n]") {
		t.Error("U raised an ask")
	}
	a.Update(keyRunes("J"))
	if a.card != nil || strings.Contains(a.View(), "[y/n]") {
		t.Error("J raised an ask")
	}
}

// TestAltArrowsAreFineTrims (#460, G6 Q6b; ADR 0052 amendment): alt makes
// any trim fine. Through the real App.Update path: alt+up/down move the
// commanded heading 1 degree (same signs as the plain arrows), alt+left/right
// the pitch trim 1 degree, and the plain 5 degree arrows are unchanged.
func TestAltArrowsAreFineTrims(t *testing.T) {
	a, c := padApp(t)
	deg := func() float64 { return (spacecraft.HeadingTrimDueEastRad + c.HeadingTrim) * 180 / math.Pi }
	a.Update(keyAlt(tea.KeyUp))
	if got := deg(); math.Abs(got-89) > 1e-6 {
		t.Errorf("alt+up: heading %.4f, want 89", got)
	}
	a.Update(keyAlt(tea.KeyDown))
	a.Update(keyAlt(tea.KeyDown))
	if got := deg(); math.Abs(got-91) > 1e-6 {
		t.Errorf("alt+down x2 from 89: heading %.4f, want 91", got)
	}
	a.Update(keyType(tea.KeyUp)) // plain arrow still 5 degrees
	if got := deg(); math.Abs(got-86) > 1e-6 {
		t.Errorf("plain up after fine: heading %.4f, want 86", got)
	}
	a.Update(keyAlt(tea.KeyRight))
	if !near(c.PitchTrim, math.Pi/180) {
		t.Errorf("alt+right: PitchTrim %v, want 1 degree east", c.PitchTrim)
	}
	a.Update(keyAlt(tea.KeyLeft))
	a.Update(keyAlt(tea.KeyLeft))
	if !near(c.PitchTrim, -math.Pi/180) {
		t.Errorf("alt+left x2: PitchTrim %v, want -1 degree", c.PitchTrim)
	}
}

// TestAltZXFineThrottle: Z / X stay 10 percent, alt+Z / alt+X step 1 percent.
func TestAltZXFineThrottle(t *testing.T) {
	a, c := padApp(t)
	c.Throttle = 0.5
	a.Update(keyRunes("Z"))
	if !near(c.Throttle, 0.6) {
		t.Fatalf("Z: throttle %v, want 0.6", c.Throttle)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Z"), Alt: true})
	if !near(c.Throttle, 0.61) {
		t.Errorf("alt+Z: throttle %v, want 0.61", c.Throttle)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X"), Alt: true})
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X"), Alt: true})
	if !near(c.Throttle, 0.59) {
		t.Errorf("alt+X x2: throttle %v, want 0.59", c.Throttle)
	}
	a.Update(keyRunes("X"))
	if !near(c.Throttle, 0.49) {
		t.Errorf("X: throttle %v, want 0.49", c.Throttle)
	}
}

// TestPadWindowHeadingCommandableToTheDegreeAndZeroesDeltaIncl (#460): the
// whole player loop through the real key handler and the rendered frame.
// Read the heading off NAVIGATION's plan: row, command it with the fine
// keys, warp (set the clock) to the pass, and TARGET's Δincl reads ~0.
// Pad pinning is re-done by hand the way integrateLanded does it each tick.
func TestPadWindowHeadingCommandableToTheDegreeAndZeroesDeltaIncl(t *testing.T) {
	a, pad := padApp(t)
	w := a.world
	padIdx := w.ActiveCraftIdx
	if _, err := w.SpawnCraft(sim.SpawnSpec{AltitudeM: 400e3, Inclination: 51.6}); err != nil {
		t.Fatal(err)
	}
	w.ActiveCraftIdx = padIdx
	w.SetTargetCraft(len(w.Crafts) - 1)

	planRe := regexp.MustCompile(`plan:\s+window T-\S+ at (\d{3})°`)
	view := a.View()
	m := planRe.FindStringSubmatch(view)
	if m == nil {
		t.Fatalf("no plan: window row in the 140x40 frame:\n%s", view)
	}
	want, _ := strconv.Atoi(m[1])
	cur := func() int {
		return int(math.Round((spacecraft.HeadingTrimDueEastRad + pad.HeadingTrim) * 180 / math.Pi))
	}
	for i := 0; i < 400 && cur() != want; i++ {
		switch d := want - cur(); {
		case d <= -5:
			a.Update(keyType(tea.KeyUp))
		case d >= 5:
			a.Update(keyType(tea.KeyDown))
		case d < 0:
			a.Update(keyAlt(tea.KeyUp))
		default:
			a.Update(keyAlt(tea.KeyDown))
		}
	}
	if cur() != want {
		t.Fatalf("could not command heading %d (at %d)", want, cur())
	}
	lw, ok := w.LaunchWindow()
	if !ok {
		t.Fatal("no window")
	}
	// Warp to the pass: advance the clock and re-pin the pad.
	w.Clock.SimTime = lw.PassAt
	lat, lon := pad.SurfaceLatLon()
	d := render.BodyFixedToWorld(pad.Primary, lat, lon, lw.PassAt)
	r := pad.Primary.RadiusMeters()
	pad.State.R = orbital.Vec3{X: r * d.X, Y: r * d.Y, Z: r * d.Z}
	om := render.BodySpinOmegaWorld(pad.Primary)
	pad.State.V = orbital.Vec3{X: om.X, Y: om.Y, Z: om.Z}.Cross(pad.State.R)
	out := a.View()
	dm := regexp.MustCompile(`Δincl:\s+([0-9]+\.[0-9]+)°`).FindStringSubmatch(out)
	if dm == nil {
		t.Fatalf("no Δincl in frame:\n%s", out)
	}
	if v, _ := strconv.ParseFloat(dm[1], 64); v > 0.1 {
		t.Errorf("at the pass with heading %d commanded by key, Δincl = %.2f, want ~0", want, v)
	}
}

// TestOptionZXFineThrottleWithoutShift (Jason 2026-10-05, "match the key combo
// we are using for nose trim"): the fine nose trim is Option+arrow, one
// modifier, so the fine throttle is Option+Z / Option+X without shift. A Mac
// terminal sends those as alt+z / alt+x (measured with the key echo); the
// shifted alt+Z / alt+X keep working.
func TestOptionZXFineThrottleWithoutShift(t *testing.T) {
	a, c := padApp(t)
	c.Throttle = 0.5
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z"), Alt: true})
	if !near(c.Throttle, 0.51) {
		t.Errorf("alt+z: throttle %v, want 0.51", c.Throttle)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true})
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x"), Alt: true})
	if !near(c.Throttle, 0.49) {
		t.Errorf("alt+x x2: throttle %v, want 0.49", c.Throttle)
	}
	// Plain z / x keep their meaning: full and cut.
	a.Update(keyRunes("z"))
	if !near(c.Throttle, 1) {
		t.Errorf("z: throttle %v, want 1 (full)", c.Throttle)
	}
}

// batchApp is a 140x40 App with the Falcon 9 + Starlink x24 stack in orbit and
// the stack turned angleDeg off the velocity (90 = radial).
func batchApp(t *testing.T, angleDeg float64) (*App, *spacecraft.Spacecraft) {
	t.Helper()
	testStateDirs(t)
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c, err := a.world.SpawnCraft(sim.SpawnSpec{LoadoutID: "Falcon-9-Starlink-24", ParentBodyID: "earth", AltitudeM: 500e3})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	// The spent F9-S1 goes first, so the carrier is the 8-row F9-S2.
	if _, _, err := a.world.StageActive(a.world.ActiveCraftIdx); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	c = a.world.ActiveCraft()
	c.ReconcileDockedComponents()
	th := angleDeg * math.Pi / 180
	r := c.State.R.Unit()
	c.CurrentAttitudeDir = c.State.V.Unit().Scale(math.Cos(th)).Add(r.Scale(math.Sin(th)))
	c.AttitudeMode = spacecraft.BurnRadialOut
	a.Update(tea.WindowSizeMsg{Width: 140, Height: 40})
	a.View()
	return a, c
}

// U on a batch asks (ADR 0059 decision 3): the card names the count and the
// fan, esc leaves the stack whole, y y releases and the flash counts the slate.
func TestReleaseBatchCardAsksThenReleases(t *testing.T) {
	a, c := batchApp(t, 90)
	n := len(a.world.Crafts)
	a.Update(keyRunes("U"))
	if len(a.world.Crafts) != n {
		t.Fatal("U released the batch on the spot, it must ask first")
	}
	view := a.View()
	for _, want := range []string{"Release all 24 Starlink?", "they fan out sideways, 0.8 cm/s a", "seam, top 19", "release", "cancel"} {
		if !cardKindUp(a, cardReleaseBatch) || !strings.Contains(view, want) {
			t.Fatalf("U card lacks %q:\n%s", want, view)
		}
	}
	a.Update(keyType(tea.KeyEsc))
	if cardKindUp(a, cardReleaseBatch) || len(a.world.Crafts) != n || a.world.ActiveCraft() != c || len(c.DockedComponents) != 25 {
		t.Fatal("esc did not leave the stack whole")
	}
	a.Update(keyRunes("U"))
	pressTwice(a, 'y')
	if want := n - 1 + 25; len(a.world.Crafts) != want {
		t.Fatalf("slate = %d, want %d", len(a.world.Crafts), want)
	}
	if want := fmt.Sprintf("released 24, %d vessels on the slate", n-1+25); a.statusMsg != want {
		t.Errorf("flash = %q, want %q", a.statusMsg, want)
	}
}

// Inside 10 degrees of the velocity U refuses with the reason and asks nothing.
func TestReleaseBatchRefusesAlongTheVelocity(t *testing.T) {
	a, _ := batchApp(t, 9)
	n := len(a.world.Crafts)
	a.Update(keyRunes("U"))
	if cardKindUp(a, cardReleaseBatch) || len(a.world.Crafts) != n {
		t.Fatal("U asked or released with the stack along the velocity")
	}
	if a.statusMsg != "release: stack along the velocity; hold radial (q) first" {
		t.Errorf("flash = %q", a.statusMsg)
	}
}

// starlinkApp is a 140x40 App whose active vessel is a lone Starlink (0.17 N
// argon Hall thruster, 20 kg of argon) on a circular equatorial orbit at altM,
// with a network link, named Starlink-7.
func starlinkApp(t *testing.T, altM float64) (*App, *spacecraft.Spacecraft) {
	t.Helper()
	a, c := batchApp(t, 90)
	c.Stages = []spacecraft.Stage{spacecraft.NewFromLoadout("Falcon-9-Starlink-24").Stages[2]}
	c.Name, c.DockedComponents, c.Nodes = "Starlink-7", nil, nil
	c.SyncFields()
	r := c.Primary.RadiusMeters() + altM
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(c.Primary.GravitationalParameter() / r)}
	c.State.M = c.TotalMass()
	a.world.Crafts = []*spacecraft.Spacecraft{c}
	a.world.EnsureCraftIDs()
	a.world.SetActiveCraftIdx(0)
	a.world.CommGraph = &sim.CommGraph{Connected: map[uint64]bool{c.ID: true}}
	a.View()
	return a, c
}

// typeAltitude presses L, types s into the box, and presses enter.
func typeAltitude(a *App, s string) {
	a.Update(keyRunes("L"))
	for i := 0; i < 12; i++ { // clear the default
		a.Update(keyType(tea.KeyBackspace))
	}
	for _, r := range s {
		a.Update(keyRunes(string(r)))
	}
	a.Update(keyType(tea.KeyEnter))
}

// L opens the altitude box; enter on a number asks with the computed cost; two
// y orders the raise. The 290 km fixture's 147 m/s and 5.0 kg are the ADR's;
// the time is 697,5xx s (8d01h), not the ADR's 8d 02h, which divided the
// rounded 147 m/s (S4 log).
func TestLRaiseBoxThenCardThenOrder(t *testing.T) {
	a, c := starlinkApp(t, 290e3)
	a.Update(keyRunes("L"))
	if !a.altBox.CapturingText() || !a.capturingText() {
		t.Fatal("L did not open the altitude box as a free-text surface")
	}
	if view := a.View(); !strings.Contains(view, "raise to:") || !strings.Contains(view, "290") {
		t.Fatalf("box lacks its label or default:\n%s", view)
	}
	for i := 0; i < 12; i++ {
		a.Update(keyType(tea.KeyBackspace))
	}
	for _, r := range "x5q50" { // letters are not an altitude
		a.Update(keyRunes(string(r)))
	}
	if got := a.altBox.Value(); got != "550" {
		t.Fatalf("box value = %q, want 550", got)
	}
	a.Update(keyType(tea.KeyEnter))
	if a.altBox.CapturingText() || !cardKindUp(a, cardRaise) {
		t.Fatal("enter did not swap the box for the raise card")
	}
	view := a.View()
	for _, want := range []string{"Raise Starlink-7 to 550 km?", "147 m/s · 5.0 kg argon", "8d01h at 0.17 N", "raise", "cancel"} {
		if !strings.Contains(view, want) {
			t.Fatalf("raise card lacks %q:\n%s", want, view)
		}
	}
	a.Update(keyType(tea.KeyEsc))
	if c.Raise != nil || a.card != nil {
		t.Fatal("esc left an order or a card behind")
	}
	typeAltitude(a, "550")
	pressTwice(a, 'y')
	if c.Raise == nil || c.Raise.TargetAltM != 550e3 || !c.Raise.Started {
		t.Fatalf("y y did not order the raise: %+v", c.Raise)
	}
	// The next L opens at the altitude last typed, once the order is stopped.
	c.Raise = nil
	a.Update(keyRunes("L"))
	if got := a.altBox.Value(); got != "550" {
		t.Errorf("default = %q, want the last typed 550", got)
	}
}

// A flight key typed into the box is text, not a flight key.
func TestLRaiseBoxSwallowsFlightKeys(t *testing.T) {
	a, c := starlinkApp(t, 290e3)
	c.Throttle = 0.5
	a.Update(keyRunes("L"))
	a.Update(keyRunes("z"))
	a.Update(keyRunes("x"))
	if c.Throttle != 0.5 {
		t.Errorf("z/x reached the throttle through the open box: %v", c.Throttle)
	}
	a.Update(keyType(tea.KeyEsc))
	if a.altBox.CapturingText() || c.Raise != nil {
		t.Fatal("esc did not close the box cleanly")
	}
}

// L while a raise runs asks to stop; stop leaves the sat coasting where it is,
// keep climbing leaves the order.
func TestLWhileRaisingAsksToStop(t *testing.T) {
	a, c := starlinkApp(t, 290e3)
	if err := a.world.CommandRaise(0, 550e3); err != nil {
		t.Fatal(err)
	}
	a.Update(keyRunes("L"))
	if !cardKindUp(a, cardStopRaise) || a.altBox.CapturingText() {
		t.Fatal("L on a climbing sat did not raise the stop card")
	}
	view := a.View()
	for _, want := range []string{"Stop the raise?", "Starlink-7 coasts where it is", "stop", "keep climbing"} {
		if !strings.Contains(view, want) {
			t.Fatalf("stop card lacks %q:\n%s", want, view)
		}
	}
	a.Update(keyType(tea.KeyEsc))
	if c.Raise == nil {
		t.Fatal("keep climbing dropped the order")
	}
	a.Update(keyRunes("L"))
	pressTwice(a, 'y')
	if c.Raise != nil {
		t.Fatal("stop left the order")
	}
	if got := drainText(a.world); !strings.Contains(got, "Starlink-7: raise stopped at 290 km") {
		t.Errorf("stop queued %q", got)
	}
}

func drainText(w *sim.World) string {
	var out []string
	for _, e := range w.PendingRaiseEvents {
		out = append(out, e.Text)
	}
	return strings.Join(out, " | ")
}

// A chemical vessel is refused before any box: the flash says why.
func TestLRefusedOnAChemicalVessel(t *testing.T) {
	a, c := starlinkApp(t, 290e3)
	c.Stages = []spacecraft.Stage{spacecraft.NewFromLoadout("Falcon-9-Starlink-24").Stages[1]} // F9-S2
	c.SyncFields()
	a.Update(keyRunes("L"))
	if a.altBox.CapturingText() || a.card != nil {
		t.Fatal("L opened a box or a card on a chemical vessel")
	}
	if a.statusMsg != "raise: thrust too high for a spiral" {
		t.Errorf("flash = %q", a.statusMsg)
	}
}

// A refusal that depends on the typed altitude (inside the atmosphere) lands
// after enter, with no card and no order.
func TestLRefusesATargetInTheAtmosphere(t *testing.T) {
	a, c := starlinkApp(t, 290e3)
	typeAltitude(a, "50")
	if a.card != nil || c.Raise != nil {
		t.Fatal("a target inside the atmosphere asked or ordered")
	}
	if a.statusMsg != "raise: below the atmosphere" {
		t.Errorf("flash = %q", a.statusMsg)
	}
}

// The end-of-order flashes the sim queues reach the footer on the next tick.
func TestRaiseEventsDrainToTheFlash(t *testing.T) {
	a, c := starlinkApp(t, 290e3)
	a.world.PendingRaiseEvents = []sim.RaiseEvent{{CraftName: c.Name, Text: "Starlink-7: raise complete, 550 km"}}
	a.Update(sim.TickMsg{})
	if !strings.Contains(a.statusMsg, "Starlink-7: raise complete, 550 km") || len(a.world.PendingRaiseEvents) != 0 {
		t.Errorf("flash = %q, pending = %d", a.statusMsg, len(a.world.PendingRaiseEvents))
	}
}

// spreadApp is batchApp with the stack released and every vessel linked:
// the carrier is active, the 24 Starlinks are on the slate.
func spreadApp(t *testing.T) (*App, uint64, []int) {
	t.Helper()
	a, c := batchApp(t, 90)
	idx := a.world.ActiveCraftIdx
	if !a.world.ReleaseBatch(idx) {
		t.Fatal("ReleaseBatch refused")
	}
	id := a.world.Crafts[idx].BatchID
	a.world.EnsureCraftIDs()
	a.world.CommGraph = &sim.CommGraph{Connected: map[uint64]bool{}}
	for _, x := range a.world.Crafts {
		a.world.CommGraph.Connected[x.ID] = true
	}
	_ = c
	a.View()
	return a, id, a.world.SpreadMembers(id)
}

// L on the carrier: altitude box, then the spread card with the campaign's
// numbers (500 to 550 km: the ADR's 5d 19h and 1d 13h, here 5d19h and 1d12h
// because the climb is 27.53 m/s and the rocket equation, not 28 m/s), then y y
// gives every member an order, one climbing and 23 waiting. A second L asks to
// stop; stop drops the waiting orders and the climber keeps climbing.
func TestLSpreadCardsFromTheCarrier(t *testing.T) {
	a, id, members := spreadApp(t)
	typeAltitude(a, "550")
	if !cardKindUp(a, cardSpread) {
		t.Fatalf("enter did not raise the spread card (flash %q)", a.statusMsg)
	}
	view := a.View()
	for _, want := range []string{"Spread 24 Starlink at 550 km?", "first starts now · last in 5d19h", "each 27 m/s · 0.94 kg · 1d12h"} {
		if !strings.Contains(view, want) {
			t.Fatalf("spread card lacks %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "raise this one") {
		t.Errorf("the carrier's card offers raise this one:\n%s", view)
	}
	a.Update(keyType(tea.KeyEsc))
	if a.world.SpreadMembers(id) == nil || a.world.Crafts[members[0]].Raise != nil {
		t.Fatal("esc left an order")
	}
	typeAltitude(a, "550")
	pressTwice(a, 'y')
	st, ok := a.world.SpreadStatus(id)
	if !ok || st.Climbing != 1 || st.Waiting != 23 {
		t.Fatalf("after y y: %+v ok=%v", st, ok)
	}

	a.Update(keyRunes("L"))
	if !cardKindUp(a, cardStopSpread) || a.altBox.CapturingText() {
		t.Fatal("L on the carrier of a running spread did not ask to stop")
	}
	view = a.View()
	for _, want := range []string{"Stop the spread?", "23 waiting dropped", "1 climbing finish", "stop", "keep spreading"} {
		if !strings.Contains(view, want) {
			t.Fatalf("stop card lacks %q:\n%s", want, view)
		}
	}
	a.Update(keyType(tea.KeyEsc))
	if st, _ := a.world.SpreadStatus(id); st.Waiting != 23 {
		t.Fatalf("esc dropped orders: %+v", st)
	}
	a.Update(keyRunes("L"))
	pressTwice(a, 'y')
	st, ok = a.world.SpreadStatus(id)
	if !ok || st.Waiting != 0 || st.Climbing != 1 {
		t.Errorf("after stop: %+v ok=%v", st, ok)
	}
	if a.statusMsg != "spread stopped, 23 orders dropped" {
		t.Errorf("flash = %q", a.statusMsg)
	}
}

// L on a member offers raise this one above spread all 24. A hand raise on a
// member whose scheduled order is still waiting replaces that order.
func TestLSpreadCardFromAMember(t *testing.T) {
	a, id, members := spreadApp(t)
	a.world.SetActiveCraftIdx(members[3])
	typeAltitude(a, "550")
	if !cardKindUp(a, cardSpread) {
		t.Fatalf("no spread card from a member (flash %q)", a.statusMsg)
	}
	view := a.View()
	for _, want := range []string{"Spread 24 Starlink at 550 km?", "raise this one", "spread all 24"} {
		if !strings.Contains(view, want) {
			t.Fatalf("member card lacks %q:\n%s", want, view)
		}
	}
	a.Update(keyRunes("s"))
	a.Update(keyRunes("y"))
	if st, ok := a.world.SpreadStatus(id); !ok || st.Waiting != 23 {
		t.Fatalf("spread all from a member: %+v ok=%v", st, ok)
	}

	// A member with a waiting scheduled order: L offers the card, not the stop.
	var c *spacecraft.Spacecraft
	for _, i := range members {
		if o := a.world.Crafts[i].Raise; o != nil && !o.Started {
			c = a.world.Crafts[i]
			a.world.SetActiveCraftIdx(i)
			break
		}
	}
	if c == nil {
		t.Fatal("no waiting member after spread all")
	}
	a.Update(keyRunes("L"))
	if cardKindUp(a, cardStopRaise) || !a.altBox.CapturingText() {
		t.Fatal("L on a waiting member did not open the altitude box")
	}
	a.Update(keyType(tea.KeyEsc))
	typeAltitude(a, "600")
	pressTwice(a, 'y') // raise this one
	if o := c.Raise; o == nil || o.BatchID != 0 || !o.Started || o.TargetAltM != 600e3 {
		t.Fatalf("raise this one left %+v", o)
	}
	if st, _ := a.world.SpreadStatus(id); st.Waiting != 22 {
		t.Errorf("the hand-raised member is still in the schedule: %+v", st)
	}
	// ...and now that it climbs, L asks to stop it.
	a.Update(keyRunes("L"))
	if !cardKindUp(a, cardStopRaise) {
		t.Error("L on a climbing member did not ask to stop the raise")
	}
}

// A refused solve flashes the reason after enter and raises no card.
func TestLSpreadRefusalFlashes(t *testing.T) {
	a, id, _ := spreadApp(t)
	typeAltitude(a, "502")
	if a.card != nil || !strings.HasPrefix(a.statusMsg, "spread: too slow from this orbit (") {
		t.Fatalf("card %v, flash %q", a.card != nil, a.statusMsg)
	}
	if _, ok := a.world.SpreadStatus(id); ok {
		t.Error("a refused spread left orders")
	}
}
