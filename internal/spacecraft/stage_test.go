package spacecraft

import (
	"math"
	"testing"
)

// TestSyncFieldsSumsAcrossStages — flat fields should reflect the
// total across every stage for mass / propellant; engine fields
// (Thrust / Isp / RCSThrust / RCSIsp) should pull from the BOTTOM
// stage (Stages[0]). Saturn-V is the canonical test case.
func TestSyncFieldsSumsAcrossStages(t *testing.T) {
	c := NewFromLoadout(LoadoutSaturnVID)
	if len(c.Stages) != 3 {
		t.Fatalf("Saturn-V should have 3 stages, got %d", len(c.Stages))
	}
	wantDry := c.Stages[0].DryMass + c.Stages[1].DryMass + c.Stages[2].DryMass
	if c.DryMass != wantDry {
		t.Errorf("DryMass: got %.0f, want %.0f (sum across stages)", c.DryMass, wantDry)
	}
	wantFuel := c.Stages[0].FuelMass + c.Stages[1].FuelMass + c.Stages[2].FuelMass
	if c.Fuel != wantFuel {
		t.Errorf("Fuel: got %.0f, want %.0f (sum across stages)", c.Fuel, wantFuel)
	}
	if c.Thrust != c.Stages[0].Thrust {
		t.Errorf("Thrust: got %.0f, want %.0f (bottom stage)", c.Thrust, c.Stages[0].Thrust)
	}
	if c.Isp != c.Stages[0].Isp {
		t.Errorf("Isp: got %.0f, want %.0f (bottom stage)", c.Isp, c.Stages[0].Isp)
	}
}

// TestBurnFuelOnlyDecrementsBottomStage — burning fuel must only
// touch Stages[0].FuelMass; upper stages stay full. The flat Fuel
// field reflects the new sum.
func TestBurnFuelOnlyDecrementsBottomStage(t *testing.T) {
	c := NewFromLoadout(LoadoutSaturnVID)
	beforeBottom := c.Stages[0].FuelMass
	beforeMid := c.Stages[1].FuelMass
	beforeTop := c.Stages[2].FuelMass
	const burn = 1000.0
	burned := c.BurnFuel(burn)
	if math.Abs(burned-burn) > 1e-9 {
		t.Errorf("BurnFuel returned %.3f, want %.3f", burned, burn)
	}
	if math.Abs(c.Stages[0].FuelMass-(beforeBottom-burn)) > 1e-9 {
		t.Errorf("bottom stage fuel: got %.3f, want %.3f",
			c.Stages[0].FuelMass, beforeBottom-burn)
	}
	if c.Stages[1].FuelMass != beforeMid {
		t.Errorf("middle stage fuel changed: %.3f → %.3f", beforeMid, c.Stages[1].FuelMass)
	}
	if c.Stages[2].FuelMass != beforeTop {
		t.Errorf("top stage fuel changed: %.3f → %.3f", beforeTop, c.Stages[2].FuelMass)
	}
	if math.Abs(c.Fuel-(beforeBottom+beforeMid+beforeTop-burn)) > 1e-9 {
		t.Errorf("flat Fuel field: got %.3f, want %.3f",
			c.Fuel, beforeBottom+beforeMid+beforeTop-burn)
	}
}

// TestBurnFuelClampsToBottomStageCapacity — asking for more fuel
// than the bottom stage holds returns only what's there; upper
// stages aren't touched.
func TestBurnFuelClampsToBottomStageCapacity(t *testing.T) {
	c := NewFromLoadout(LoadoutSaturnVID)
	bottomFuel := c.Stages[0].FuelMass
	burned := c.BurnFuel(bottomFuel * 2)
	if math.Abs(burned-bottomFuel) > 1e-9 {
		t.Errorf("BurnFuel returned %.3f, want %.3f (clamp at bottom-stage capacity)",
			burned, bottomFuel)
	}
	if c.Stages[0].FuelMass != 0 {
		t.Errorf("bottom stage fuel after over-burn: %.3f, want 0", c.Stages[0].FuelMass)
	}
}

// TestSingleStageLoadoutHasOneStage — pre-v0.9.1 loadouts (S-IVB-1
// etc.) wrap into single-element Stages; flat fields equal
// Stages[0]'s values.
func TestSingleStageLoadoutHasOneStage(t *testing.T) {
	c := NewFromLoadout(LoadoutSIVB1ID)
	if len(c.Stages) != 1 {
		t.Errorf("S-IVB-1 should have 1 stage, got %d", len(c.Stages))
	}
	if c.DryMass != c.Stages[0].DryMass {
		t.Errorf("flat DryMass != Stages[0].DryMass for single-stage craft")
	}
	if c.Fuel != c.Stages[0].FuelMass {
		t.Errorf("flat Fuel != Stages[0].FuelMass for single-stage craft")
	}
}

// TestSyncFieldsNoOpOnEmptyStages — legacy literal Spacecraft{} test
// fixtures with no Stages must keep their flat-field values intact.
// SyncFields must not zero them out.
func TestSyncFieldsNoOpOnEmptyStages(t *testing.T) {
	s := &Spacecraft{
		DryMass: 1000,
		Fuel:    500,
		Thrust:  10000,
		Isp:     300,
	}
	s.SyncFields()
	if s.DryMass != 1000 || s.Fuel != 500 || s.Thrust != 10000 || s.Isp != 300 {
		t.Errorf("SyncFields with empty Stages clobbered flat fields: %+v", s)
	}
}
