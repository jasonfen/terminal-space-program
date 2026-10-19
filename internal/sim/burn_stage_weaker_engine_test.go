package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Audit 2026-10-18 #6: a planted burn's EndTime is sized at plant time for the
// engine then lit. Staging to a weaker engine mid-burn kept that window, so
// the burn was reaped on duration with most of its Δv unburned and a "burn
// complete" flash. Staging must re-size the window for the engine now lit.
func TestStagingToWeakerEngineKeepsBurnAlive(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	c.Stages = []spacecraft.Stage{
		{Name: "lower", DryMass: 500, FuelMass: 20, FuelCapacity: 20, Thrust: 200000, Isp: 250},
		{Name: "upper", DryMass: 800, FuelMass: 4000, FuelCapacity: 4000, Thrust: 20000, Isp: 300},
	}
	crewTend(c)
	c.SyncFields()
	c.State.M = c.TotalMass()
	const dv = 300.0
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: dv,
		EndTime:     w.Clock.SimTime.Add(c.BurnTimeForDV(dv)), // sized for the 200 kN lower stage
		PrimaryID:   c.Primary.ID,
		Throttle:    1,
		PlannedDV:   dv,
	}

	if _, ok := tickUntil(w, 200, func() bool { return c.ActiveStageFuel() <= 0 }); !ok {
		t.Fatal("lower stage never ran dry")
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	// 20 s at 20 kN is nowhere near enough for ~290 m/s on ~4.8 t.
	for i := 0; i < int(20*time.Second/w.Clock.BaseStep); i++ {
		w.Tick()
	}
	if c.ActiveBurn == nil {
		t.Fatalf("burn reaped after staging to the weaker engine with Δv still owed (finished events: %d)", len(w.PendingBurnFinishedEvents))
	}
	if c.ActiveBurn.DVRemaining <= 0 {
		t.Errorf("DVRemaining = %.1f, want > 0", c.ActiveBurn.DVRemaining)
	}
}
