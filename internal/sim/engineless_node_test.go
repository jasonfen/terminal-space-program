package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Audit 18: a vessel with no engine (Thrust 0 / Isp 0) used to gain the
// full Δv of an impulsive node for free.
func TestExecuteDueNodesEnginelessImpulsiveRefused(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Thrust, c.Isp = 0, 0
	v0 := c.State.V
	c.Nodes = []spacecraft.ManeuverNode{{Mode: spacecraft.BurnPrograde, DV: 1000, TriggerTime: w.Clock.SimTime}}

	w.executeDueNodes()

	if got := c.State.V.Sub(v0).Norm(); got != 0 {
		t.Errorf("engine-less vessel gained %.1f m/s from an impulsive node", got)
	}
	if len(c.Nodes) != 0 {
		t.Errorf("len(Nodes) = %d, want 0 (node dropped)", len(c.Nodes))
	}
	e := w.LastNodeTargetRefusal
	if e == nil || !e.Cancelled || !e.NoEngine || e.CraftName != c.Name {
		t.Errorf("LastNodeTargetRefusal = %+v, want Cancelled+NoEngine for %q", e, c.Name)
	}
}

// An engined vessel's impulsive node still fires (guard must not over-refuse).
func TestExecuteDueNodesEnginedImpulsiveStillFires(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c.Thrust <= 0 || c.Isp <= 0 {
		t.Fatalf("default craft has no engine: Thrust=%v Isp=%v", c.Thrust, c.Isp)
	}
	v0 := c.State.V
	c.Nodes = []spacecraft.ManeuverNode{{Mode: spacecraft.BurnPrograde, DV: 10, TriggerTime: w.Clock.SimTime}}

	w.executeDueNodes()

	if got := c.State.V.Sub(v0).Norm(); got < 1 {
		t.Errorf("engined vessel gained %.3f m/s, want ~10", got)
	}
	if w.LastNodeTargetRefusal != nil {
		t.Errorf("unexpected refusal %+v", w.LastNodeTargetRefusal)
	}
}
