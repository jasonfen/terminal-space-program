package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Review LOWs 122/123: the D / Y asks are session-only and used to be
// cleared only by y/n/esc. Anything that makes the ask stale (a different
// screen, a different active vessel, a replaced world) must drop it, so a
// later y never commits a verb the player was not looking at.
//
// Seam note: while an ask is up every KEY is swallowed (the end-flight
// idiom, pinned by TestTransposeAsksBeforeActing), so the stale states are
// reached the way production reaches them without a key: a mouse click on
// the menu, a vessel switch from the sim side, a world swap from a load.
// The tests mutate that state directly and then drive the real Update.

func askShown(a *App) bool { return strings.Contains(a.View(), "[y/n]") }

func TestTransposeAskDropsWhenScreenChanges(t *testing.T) {
	a := apolloReadyApp(t)
	a.Update(keyRunes("D"))
	if !askShown(a) {
		t.Fatal("setup: D raised no ask")
	}
	a.active = screenMenu // a click on the title bar's menu lands here
	a.Update(sim.TickMsg(time.Now()))
	a.active = screenOrbit
	if askShown(a) {
		t.Error("transpose ask survived a screen change")
	}
	a.Update(keyRunes("y"))
	if got := a.world.ActiveCraft().Stages[0].Name; got != "Descent" {
		t.Errorf("a stale y transposed the stack (Stages[0] = %q)", got)
	}
}

func TestTransposeAskDropsWhenWorldReplaced(t *testing.T) {
	a := apolloReadyApp(t)
	a.Update(keyRunes("D"))
	if !askShown(a) {
		t.Fatal("setup: D raised no ask")
	}
	w := *a.world // a loaded world is a different World value, same shape
	a.world = &w
	a.Update(sim.TickMsg(time.Now()))
	if askShown(a) {
		t.Error("transpose ask survived a world replacement (quickload)")
	}
}

func TestDeployAskDropsWhenActiveVesselChanges(t *testing.T) {
	a, _ := deployApp(t)
	a.Update(keyRunes("Y"))
	if !askShown(a) {
		t.Fatal("setup: Y raised no ask")
	}
	n := len(a.world.Crafts)
	if n < 2 {
		t.Fatalf("setup: need two vessels, have %d", n)
	}
	a.world.ActiveCraftIdx = 0 // the deploy carrier was spawned last
	a.Update(sim.TickMsg(time.Now()))
	if askShown(a) {
		t.Error("deploy ask survived a switch of the active vessel")
	}
	a.Update(keyRunes("y"))
	if len(a.world.Crafts) != n {
		t.Error("a stale y deployed from the wrong vessel")
	}
}

// The unchanged case still works: with nothing stale, the ask survives a tick.
func TestAskSurvivesTicksWhileFresh(t *testing.T) {
	a := apolloReadyApp(t)
	a.Update(keyRunes("D"))
	a.Update(sim.TickMsg(time.Now()))
	if !askShown(a) {
		t.Error("a plain tick dropped a fresh ask")
	}
}
