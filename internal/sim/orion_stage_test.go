package sim

import (
	"errors"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0060 decision 6 guard 9: SLS + Orion on the pad lifts off, `b` lights
// the solids, three `space` presses (SRBs, core, ICPS) leave [ESM, Orion]
// named ESM with the ICPS left as live-fuelled debris, the fourth drops the
// ESM and leaves the chute capsule, the fifth is the arm-the-chute path.
func TestSLSOrionPadAndStagingChain(t *testing.T) {
	w := mustWorld(t)
	w.Crafts = nil
	w.ActiveCraftIdx = -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "SLS-Orion", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if twr := c.Thrust / (c.TotalMass() * 9.80665); twr <= 1 {
		t.Errorf("pad TWR = %.3f, want > 1", twr)
	}
	if len(c.Stages) != 5 {
		t.Fatalf("stages = %d, want 5", len(c.Stages))
	}
	w.RecomputeCommGraph()
	idx := w.ActiveCraftIdx

	// b: the marked solids light with the core.
	if BoosterSolidsLit(c) {
		t.Error("solids read lit before ignition")
	}
	lightSLS(t, w, c)
	if !BoosterSolidsLit(c) {
		t.Fatal("BoosterSolidsLit false after b")
	}
	// Dry the solids (burnout), then the three drops.
	c.Stages[0].FuelMass = 0
	c.SyncFields()
	var icps *spacecraft.Spacecraft
	for i := 1; i <= 3; i++ {
		_, jet, err := w.StageActive(idx)
		if err != nil {
			t.Fatalf("space #%d: %v", i, err)
		}
		if i == 3 {
			icps = w.Crafts[jet]
		}
	}
	if len(c.Stages) != 2 || c.Stages[0].Name != "ESM" || c.Stages[1].Name != "Orion" {
		t.Fatalf("after three presses: %+v, want [ESM, Orion]", c.Stages)
	}
	if c.Name != "ESM" {
		t.Errorf("vessel name = %q, want ESM", c.Name)
	}
	if c.Thrust != 26_600 {
		t.Errorf("lit engine = %g N, want 26600", c.Thrust)
	}
	if c.HasParachute {
		t.Error("the ESM is the bottom stage; the chute must not read before it drops")
	}
	if icps == nil || icps.Role != spacecraft.RoleJettisonedStage || icps.Controllable {
		t.Fatalf("dropped ICPS = %+v, want RoleJettisonedStage and not Controllable", icps)
	}
	if len(icps.Stages) != 1 || icps.Stages[0].Name != "ICPS" || icps.Stages[0].FuelMass <= 0 {
		t.Errorf("dropped ICPS stages = %+v, want one ICPS stage keeping its leftover fuel", icps.Stages)
	}

	if _, _, err := w.StageActive(idx); err != nil {
		t.Fatalf("space #4: %v", err)
	}
	if len(c.Stages) != 1 || c.Stages[0].Name != "Orion" || !c.HasParachute {
		t.Fatalf("after the fourth press: %d stages (%+v), chute %v; want [Orion] with a chute",
			len(c.Stages), c.Stages, c.HasParachute)
	}
	if _, _, err := w.StageActive(idx); !errors.Is(err, ErrStageOnlyOne) {
		t.Fatalf("fifth space: %v, want ErrStageOnlyOne", err)
	}
	if !c.ArmParachute() || c.ChuteState != spacecraft.ChuteArmed {
		t.Error("the press after the ESM must arm the chute")
	}
}
