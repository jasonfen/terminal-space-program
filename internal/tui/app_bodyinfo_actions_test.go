package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func bodyInfoApp(t *testing.T, id string) (*App, int) {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	idx := bodyIdx(t, a, id)
	a.active = screenBodyInfo
	a.selectedBody = idx
	return a, idx
}

// #495 (B3): on body info `t` targets the SHOWN body directly, not the
// nearest-first cycle step.
func TestBodyInfoTargetsShownBody(t *testing.T) {
	a, mars := bodyInfoApp(t, "mars")
	pressKey(a, 't')
	if a.world.Target.Kind != sim.TargetBody || a.world.Target.BodyIdx != mars {
		t.Fatalf("target = %+v, want Mars body %d (statusMsg %q)", a.world.Target, mars, a.statusMsg)
	}
	if a.statusMsg != "target: Mars" {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, "target: Mars")
	}
	if a.active != screenBodyInfo {
		t.Errorf("active = %v, want body info", a.active)
	}
}

// Then H plans a transfer to that body.
func TestBodyInfoTargetThenTransfer(t *testing.T) {
	a, mars := bodyInfoApp(t, "mars")
	pressKey(a, 't')
	before := len(a.world.ActiveCraft().Nodes)
	pressKey(a, 'H')
	if got := len(a.world.ActiveCraft().Nodes); got <= before {
		t.Fatalf("H planted no node (nodes %d -> %d, statusMsg %q)", before, got, a.statusMsg)
	}
	if a.world.Target.BodyIdx != mars {
		t.Errorf("target moved off Mars: %+v", a.world.Target)
	}
}

// Then P opens the porkchop for that body (was a silent no-op off the map).
func TestBodyInfoTargetThenPorkchop(t *testing.T) {
	a, mars := bodyInfoApp(t, "mars")
	pressKey(a, 't')
	pressKey(a, 'P')
	if a.active != screenPorkchop {
		t.Fatalf("active = %v, want porkchop (statusMsg %q)", a.active, a.statusMsg)
	}
	if got := a.porkchop.TargetIdx(); got != mars {
		t.Errorf("porkchop target = %d, want Mars %d", got, mars)
	}
}

// P with no target on body info refuses out loud instead of doing nothing.
func TestBodyInfoPorkchopRefusesWithoutTarget(t *testing.T) {
	a, _ := bodyInfoApp(t, "mars")
	pressKey(a, 'P')
	want := "porkchop: no target, press t to aim at a planet"
	if a.statusMsg != want || a.active != screenBodyInfo {
		t.Errorf("statusMsg = %q active = %v, want refusal %q on body info", a.statusMsg, a.active, want)
	}
}

// ADR 0066 guards 1 and 2: `t` on the map opens the target menu without
// aiming; `t` again takes the row one press used to aim at (nearest-first),
// and only that commit records cycle_target.
func TestOrbitScreenTargetStillCycles(t *testing.T) {
	a, mars := bodyInfoApp(t, "mars")
	a.active = screenOrbit
	pressKey(a, 't')
	if !a.targetMenuOpen || a.world.Target.Kind != sim.TargetNone || len(a.world.RecentActions()) != 0 {
		t.Fatalf("first t: open=%v target=%v actions=%v, want menu open, no target, no action", a.targetMenuOpen, a.world.Target, a.world.RecentActions())
	}
	pressKey(a, 't')
	if a.targetMenuOpen {
		t.Fatalf("second t left the menu open")
	}
	if a.world.Target.Kind == sim.TargetBody && a.world.Target.BodyIdx == mars {
		t.Fatalf("orbit-screen t t jumped to the cursor body Mars; must take the nearest-first step")
	}
	if a.world.Target.Kind == sim.TargetNone {
		t.Fatalf("orbit-screen t t did not target anything")
	}
	if got := a.world.RecentActions(); len(got) != 1 || got[0] != missions.ActionCycleTarget {
		t.Fatalf("recent actions = %v, want [cycle_target]", got)
	}
}

// esc closes the menu with the target untouched; warp keys still reach the clock.
func TestTargetMenuEscClosesAndWarpKeysPass(t *testing.T) {
	a, _ := bodyInfoApp(t, "mars")
	a.active = screenOrbit
	pressKey(a, 't')
	before := a.world.Clock.WarpIdx
	pressKey(a, '.')
	if a.world.Clock.WarpIdx == before {
		t.Errorf("warp-up key swallowed by the open menu")
	}
	pressKey(a, 'x') // any other flight key is swallowed
	if !a.targetMenuOpen {
		t.Fatalf("menu closed on a stray key")
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.targetMenuOpen || a.world.Target.Kind != sim.TargetNone {
		t.Errorf("esc: open=%v target=%v, want closed and untouched", a.targetMenuOpen, a.world.Target)
	}
}

// Refusals: the star, the body you orbit, and the already-targeted body
// each say why in one phrase; none silently no-ops or changes the target.
func TestBodyInfoTargetRefusals(t *testing.T) {
	cases := []struct {
		id, want string
		pre      bool
	}{
		{"sun", "target: a star can't be targeted", false},
		{"earth", "target: you are orbiting Earth", false},
		{"mars", "target: Mars is already targeted", true},
	}
	for _, c := range cases {
		a, idx := bodyInfoApp(t, c.id)
		if c.pre {
			a.world.SetTargetBody(idx)
		}
		before := a.world.Target
		pressKey(a, 't')
		if a.statusMsg != c.want {
			t.Errorf("%s: statusMsg = %q, want %q", c.id, a.statusMsg, c.want)
		}
		if a.world.Target != before {
			t.Errorf("%s: target changed %+v -> %+v", c.id, before, a.world.Target)
		}
	}
}

// The flash for `t` must not overwrite the footer that advertises it.
func TestBodyInfoFlashKeepsFooter(t *testing.T) {
	a, _ := bodyInfoApp(t, "mars")
	a.width, a.height = 140, 40
	pressKey(a, 't')
	v := a.View()
	for _, want := range []string{"target: Mars", "[t] target  [H] transfer  [P] porkchop"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

// Review LOW 78 (no code change): with the active vessel in ANOTHER system,
// the body shown here is not one you orbit, so `t` must target it instead of
// refusing "you are orbiting Earth". The CraftVisibleHere gate in
// doTargetShownBody is what makes that so; this pins the observed behaviour.
func TestBodyInfoTargetWithVesselInAnotherSystem(t *testing.T) {
	a, earth := bodyInfoApp(t, "earth")
	a.world.ActiveCraft().SystemIdx = a.world.SystemIdx + 1
	if a.world.CraftVisibleHere() {
		t.Fatal("setup: vessel should not be visible here")
	}
	pressKey(a, 't')
	if a.world.Target.Kind != sim.TargetBody || a.world.Target.BodyIdx != earth {
		t.Fatalf("target = %+v, want Earth %d (statusMsg %q)", a.world.Target, earth, a.statusMsg)
	}
}

// moonSiteApp has the active vessel at the Moon with one flag of its own.
func moonSiteApp(t *testing.T) *App {
	t.Helper()
	a, _ := bodyInfoApp(t, "moon")
	a.active = screenOrbit
	c := a.world.ActiveCraft()
	c.Primary = a.world.System().Bodies[bodyIdx(t, a, "moon")]
	a.world.Sites = []sim.Site{{ID: 7, Body: c.Primary.EnglishName, System: a.world.System().Name,
		LatDeg: 1, LonDeg: 2, Kind: sim.SiteFlag, Name: "Lander-1 site"}}
	a.world.NextSiteID = 7
	return a
}

func tapKey(a *App, k tea.KeyType) { a.Update(tea.KeyMsg{Type: k}) }

// pickSiteRow walks the cursor to the site row (down is bounded by the list).
func pickSiteRow(t *testing.T, a *App) {
	t.Helper()
	pressKey(a, 't')
	for i := 0; i < 40; i++ {
		if r, ok := a.targetMenu.Row(); ok && r.Group == sim.GroupSites && r.Own {
			return
		}
		tapKey(a, tea.KeyDown)
	}
	t.Fatalf("no site row in the menu")
}

// ADR 0066 guard 6 (the d half): d on your own site asks delete / keep it;
// yes removes it and clears a target on it, keep it leaves both.
func TestTargetMenuDeleteCardRemovesSiteAndClearsTarget(t *testing.T) {
	a := moonSiteApp(t)
	a.world.SetTargetSite(7)
	pickSiteRow(t, a)
	pressKey(a, 'd')
	if a.card == nil || a.card.kind != cardDeleteSite {
		t.Fatalf("d raised no delete card")
	}
	pressKey(a, 'n') // keep it
	if len(a.world.Sites) != 1 || a.world.Target.Kind != sim.TargetSite {
		t.Fatalf("keep it removed the site or the target")
	}
	pressKey(a, 'd')
	pressKey(a, 'y')
	pressKey(a, 'y') // the second step
	if len(a.world.Sites) != 0 || a.world.Target.Kind != sim.TargetNone {
		t.Errorf("sites=%d target=%v, want none and cleared", len(a.world.Sites), a.world.Target)
	}
}

// r renames: the field swallows letters (a flight key like d is text), enter
// stores the name, esc drops the edit.
func TestTargetMenuRenameTypesText(t *testing.T) {
	a := moonSiteApp(t)
	pickSiteRow(t, a)
	pressKey(a, 'r')
	if !a.capturingText() {
		t.Fatalf("rename field not capturing text")
	}
	for i := 0; i < len("Lander-1 site"); i++ {
		tapKey(a, tea.KeyBackspace)
	}
	for _, r := range "Tranquil d" {
		pressKey(a, r)
	}
	tapKey(a, tea.KeyEnter)
	if got := a.world.Sites[0].Name; got != "Tranquil d" || a.capturingText() {
		t.Errorf("name = %q capturing=%v, want %q and done", got, a.capturingText(), "Tranquil d")
	}
	pressKey(a, 'r')
	pressKey(a, 'z')
	tapKey(a, tea.KeyEsc)
	if a.world.Sites[0].Name != "Tranquil d" || !a.targetMenuOpen {
		t.Errorf("esc in the field changed the name or closed the menu")
	}
}

// ADR 0068 decision 2: + and - on your own flag's row add and drop LZs through
// the real key path, and the menu stays open for the next press.
func TestTargetMenuPlusMinusAddAndDropLZs(t *testing.T) {
	a := moonSiteApp(t)
	pickSiteRow(t, a)
	for i := 0; i < 10; i++ {
		pressKey(a, '+')
	}
	if got := a.world.Sites[0].LZs(); got != sim.SiteMaxLZs || !a.targetMenuOpen {
		t.Fatalf("ten + gave %d LZs (menu open %v), want %d", got, a.targetMenuOpen, sim.SiteMaxLZs)
	}
	for i := 0; i < 20; i++ {
		pressKey(a, '-')
	}
	if got := a.world.Sites[0].LZs(); got != 1 || !a.targetMenuOpen {
		t.Errorf("twenty - left %d LZs (menu open %v), want 1", got, a.targetMenuOpen)
	}
}
