package tui

import (
	"strings"
	"testing"

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

// `t` on the map is still the nearest-first cycle (settled, unchanged).
func TestOrbitScreenTargetStillCycles(t *testing.T) {
	a, mars := bodyInfoApp(t, "mars")
	a.active = screenOrbit
	pressKey(a, 't')
	if a.world.Target.Kind == sim.TargetBody && a.world.Target.BodyIdx == mars {
		t.Fatalf("orbit-screen t jumped to the cursor body Mars; must cycle nearest-first")
	}
	if a.world.Target.Kind == sim.TargetNone {
		t.Fatalf("orbit-screen t did not advance the cycle")
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
