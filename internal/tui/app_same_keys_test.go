package tui

import (
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
		{name: "end-flight confirm", enter: func(t *testing.T, a *App) { a.endFlightConfirm = true }},
		{name: "quickload confirm", enter: func(t *testing.T, a *App) { a.quickloadConfirm = true }, rearm: true},
		{name: "quit confirm", enter: func(t *testing.T, a *App) { a.quitConfirm = true }},
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
	if strings.Contains(a.View(), "[y/n]") {
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

// TestTransposeAsksBeforeActing (decision 7): D asks [y/n]; n and esc keep
// the stack; y transposes; other keys are swallowed while asking.
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
	if v := a.View(); !strings.Contains(v, "transpose") || !strings.Contains(v, "[y/n]") {
		t.Fatalf("D raised no [y/n] transpose ask:\n%s", v)
	}
	// A stray flight key is swallowed, the ask stays.
	a.Update(keyRunes("w"))
	if v := a.View(); !strings.Contains(v, "[y/n]") {
		t.Error("an unrelated key dismissed the ask")
	}
	a.Update(keyRunes("n"))
	if strings.Contains(a.View(), "[y/n]") || c.Stages[0].Name != "Descent" {
		t.Error("n did not cancel cleanly")
	}
	a.Update(keyRunes("D"))
	a.Update(keyType(tea.KeyEsc))
	if strings.Contains(a.View(), "[y/n]") || c.Stages[0].Name != "Descent" {
		t.Error("esc did not cancel cleanly")
	}
	a.Update(keyRunes("D"))
	a.Update(keyRunes("y"))
	if got := a.world.ActiveCraft().Stages[0].Name; got != "SM" {
		t.Errorf("y: Stages[0] = %q, want SM (transposed)", got)
	}
}

// TestTransposeRefusesBeforeAsking: a stack that is not transpose-ready is
// refused first; no ask is raised for a verb that would refuse anyway.
func TestTransposeRefusesBeforeAsking(t *testing.T) {
	a, _ := padApp(t) // Saturn V on the pad, not [Descent Ascent SM CM]
	a.Update(keyRunes("D"))
	if strings.Contains(a.View(), "[y/n]") {
		t.Error("D asked about a transpose that would refuse")
	}
	if !strings.Contains(a.statusMsg, "transpose:") {
		t.Errorf("D gave no refusal, statusMsg = %q", a.statusMsg)
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
	if v := a.View(); !strings.Contains(v, "deploy") || !strings.Contains(v, "[y/n]") {
		t.Fatalf("Y raised no [y/n] deploy ask:\n%s", v)
	}
	a.Update(keyRunes("n"))
	if len(a.world.Crafts) != n || strings.Contains(a.View(), "[y/n]") {
		t.Error("n did not cancel cleanly")
	}
	a.Update(keyRunes("Y"))
	a.Update(keyRunes("y"))
	if len(a.world.Crafts) != n+1 {
		t.Errorf("y: %d vessels, want %d", len(a.world.Crafts), n+1)
	}
}

// TestDeployRefusesBeforeAsking: a vessel with no payload is refused first.
func TestDeployRefusesBeforeAsking(t *testing.T) {
	a, _ := padApp(t)
	a.Update(keyRunes("Y"))
	if strings.Contains(a.View(), "[y/n]") {
		t.Error("Y asked about a deploy that would refuse")
	}
	if !strings.Contains(a.statusMsg, "deploy:") {
		t.Errorf("Y gave no refusal, statusMsg = %q", a.statusMsg)
	}
}

// TestUndockAndTransferStayInstant: U and J do not ask (decision 7).
func TestUndockAndTransferStayInstant(t *testing.T) {
	a, _ := deployApp(t)
	a.Update(keyRunes("U"))
	if strings.Contains(a.View(), "[y/n]") {
		t.Error("U raised an ask")
	}
	a.Update(keyRunes("J"))
	if strings.Contains(a.View(), "[y/n]") {
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
