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
