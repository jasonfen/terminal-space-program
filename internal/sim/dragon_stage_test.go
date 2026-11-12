package sim

import (
	"errors"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0057 decision 3 guard 3: `space` on [Dragon Trunk, Dragon] drops the
// trunk (named Dragon Trunk-1), the survivor keeps the Dracos and carries the
// chute, and the next press is the arm-the-chute path.
func TestDragonSpaceDropsTrunkThenArms(t *testing.T) {
	for _, id := range []string{"Dragon", "Cargo-Dragon"} {
		w := mustWorld(t)
		w.Crafts = nil
		w.ActiveCraftIdx = -1
		c, err := w.SpawnCraft(SpawnSpec{LoadoutID: id, ParentBodyID: "earth", AltitudeM: 500_000})
		if err != nil {
			t.Fatalf("spawn %s: %v", id, err)
		}
		w.RecomputeCommGraph()
		if len(c.Stages) != 2 || c.Stages[0].Name != "Dragon Trunk" {
			t.Fatalf("%s stages = %+v, want [Dragon Trunk, capsule]", id, c.Stages)
		}
		if c.HasParachute {
			t.Errorf("%s: the trunk is the bottom stage; the chute must not read before it drops", id)
		}
		thrust := c.Thrust
		if thrust != 1600 {
			t.Fatalf("%s trunk-on thrust = %g, want 1600", id, thrust)
		}
		idx := w.ActiveCraftIdx
		_, jet, err := w.StageActive(idx)
		if err != nil {
			t.Fatalf("%s first space: %v", id, err)
		}
		if got := w.Crafts[jet].Name; got != "Dragon Trunk-1" {
			t.Errorf("%s: dropped craft = %q, want Dragon Trunk-1", id, got)
		}
		if len(c.Stages) != 1 || !c.HasParachute || c.Thrust != thrust {
			t.Errorf("%s survivor: %d stages, chute %v, thrust %g; want 1 stage, chute, thrust %g",
				id, len(c.Stages), c.HasParachute, c.Thrust, thrust)
		}
		if _, _, err := w.StageActive(idx); !errors.Is(err, ErrStageOnlyOne) {
			t.Fatalf("%s second space: %v, want ErrStageOnlyOne", id, err)
		}
		if !c.ArmParachute() || c.ChuteState != spacecraft.ChuteArmed {
			t.Errorf("%s: the press after the trunk must arm the chute", id)
		}
	}
}

// Guard 4: Falcon-9-Dragon on the pad lifts off (TWR > 1, S1 lit); two
// `space` presses leave [Dragon Trunk, Dragon] with the Dragon lit.
func TestFalcon9DragonPadAndStaging(t *testing.T) {
	for _, id := range []string{"Falcon-9-Dragon", "Falcon-9-Cargo-Dragon"} {
		w := mustWorld(t)
		w.Crafts = nil
		w.ActiveCraftIdx = -1
		c, err := w.SpawnCraft(SpawnSpec{LoadoutID: id, ParentBodyID: "earth", Launchpad: true,
			Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast})
		if err != nil {
			t.Fatalf("spawn %s: %v", id, err)
		}
		if twr := c.Thrust / (c.TotalMass() * 9.80665); twr <= 1 {
			t.Errorf("%s pad TWR = %.3f, want > 1", id, twr)
		}
		if got := spacecraft.LitStageIdx(c.Stages); got != 0 {
			t.Errorf("%s lit stage = %d, want 0", id, got)
		}
		w.RecomputeCommGraph()
		idx := w.ActiveCraftIdx
		for i := 0; i < 2; i++ {
			if _, _, err := w.StageActive(idx); err != nil {
				t.Fatalf("%s space #%d: %v", id, i+1, err)
			}
		}
		if len(c.Stages) != 2 || c.Stages[0].Name != "Dragon Trunk" {
			t.Fatalf("%s after two presses: %+v, want [Dragon Trunk, capsule]", id, c.Stages)
		}
		if got := spacecraft.LitStageIdx(c.Stages); got != 1 || c.Thrust != 1600 {
			t.Errorf("%s after two presses: lit %d thrust %g, want the Dragon (1) at 1600", id, got, c.Thrust)
		}
	}
}

// ADR 0064 guard 6: the Falcon Heavy lifts off the pad, `b` lights the pair
// with the core, and `space` mid-burn (liquid, so no refusal) leaves the core
// stack plus two passive halves that keep their fuel and legs for #726.
func TestFalconHeavyPadAndBoosterDrop(t *testing.T) {
	w := mustWorld(t)
	w.Crafts = nil
	w.ActiveCraftIdx = -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-Heavy", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast})
	if err != nil {
		t.Fatal(err)
	}
	if twr := c.Thrust / (c.TotalMass() * 9.80665); twr <= 1 {
		t.Fatalf("pad TWR = %.3f, want > 1", twr)
	}
	c.Throttle = 1
	c.EngineMode = spacecraft.EngineMain
	w.StartManualBurn()
	if c.ManualBurn == nil || !c.HasLiquidBooster() || BoosterSolidsLit(c) {
		t.Fatalf("after b: burn %v liquid %v solids lit %v, want a liquid pair lit", c.ManualBurn != nil, c.HasLiquidBooster(), BoosterSolidsLit(c))
	}
	parent := c.Name // SpawnCraft numbers it ("Falcon Heavy-1"); halves are named for it
	w.RecomputeCommGraph()
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatalf("space mid-burn: %v", err)
	}
	if len(w.Crafts) != 3 {
		t.Fatalf("slate = %d vessels, want 3", len(w.Crafts))
	}
	for i, name := range []string{parent + "-booster1", parent + "-booster2"} {
		h := w.Crafts[i+1]
		s := h.Stages[0]
		if h.Name != name || s.DryMass != 25600 || s.Thrust != 7607000 || !s.CanSoftLand || s.FuelMass <= 0 ||
			h.Role != spacecraft.RoleJettisonedStage || !h.Controllable { // ADR 0069 decision 11: a half that can land keeps its core
			t.Errorf("half %d = %q dry %g thrust %g soft-land %v fuel %g role %v controllable %v", i, h.Name,
				s.DryMass, s.Thrust, s.CanSoftLand, s.FuelMass, h.Role, h.Controllable)
		}
	}
	if c.Name != "F9-S1" || len(c.Stages) != 2 || c.Stages[0].Name != "F9-S1" || c.Stages[1].Name != "F9-S2" || c.BoosterThrottle != nil {
		t.Errorf("core after drop = %q %+v throttle %v, want F9-S1 [F9-S1 F9-S2] nil", c.Name, c.Stages, c.BoosterThrottle)
	}
}
