package spacecraft

import (
	"math"
	"testing"
)

// TestStackStatsHandWorked — a two-stage stack with hand-computed Δv and
// liftoff TWR. Stage 0: dry 1000, fuel 9000, thrust 200 kN, Isp 300.
// Stage 1: dry 500, fuel 4500, thrust 50 kN, Isp 320. Monoprop 0 so the
// mass convention is unambiguous.
func TestStackStatsHandWorked(t *testing.T) {
	stages := []Stage{
		{DryMass: 1000, FuelMass: 9000, Thrust: 200_000, Isp: 300},
		{DryMass: 500, FuelMass: 4500, Thrust: 50_000, Isp: 320},
	}
	vs := StackStats(stages)

	// Stage 0 hauls the whole stack (m0 = 1000+9000+500+4500 = 15000),
	// burns 9000 → m1 = 6000. Δv0 = 300·g0·ln(15000/6000).
	wantDV0 := 300 * g0 * math.Log(15000.0/6000.0)
	// Stage 1 alone: m0 = 5000, m1 = 500. Δv1 = 320·g0·ln(5000/500).
	wantDV1 := 320 * g0 * math.Log(5000.0/500.0)
	if math.Abs(vs.StageDV[0]-wantDV0) > 1e-6 {
		t.Errorf("StageDV[0] = %g, want %g", vs.StageDV[0], wantDV0)
	}
	if math.Abs(vs.StageDV[1]-wantDV1) > 1e-6 {
		t.Errorf("StageDV[1] = %g, want %g", vs.StageDV[1], wantDV1)
	}
	if math.Abs(vs.TotalDV-(wantDV0+wantDV1)) > 1e-6 {
		t.Errorf("TotalDV = %g, want %g", vs.TotalDV, wantDV0+wantDV1)
	}
	if vs.TotalMass != 15000 {
		t.Errorf("TotalMass = %g, want 15000", vs.TotalMass)
	}
	// Liftoff TWR = 200000 / (15000·g0).
	wantTWR := 200_000.0 / (15000 * g0)
	if math.Abs(vs.LiftoffTWR-wantTWR) > 1e-9 {
		t.Errorf("LiftoffTWR = %g, want %g", vs.LiftoffTWR, wantTWR)
	}
}

// TestStackStatsEnginelessStage — a fuelless / engineless top stage (a
// parachute pod) contributes mass but no Δv, and an empty stack is zero.
func TestStackStatsEnginelessStage(t *testing.T) {
	vs := StackStats([]Stage{
		{DryMass: 2000, FuelMass: 8000, Thrust: 100_000, Isp: 300, MonopropMass: 100},
		{DryMass: 500}, // engineless pod
	})
	if vs.StageDV[1] != 0 {
		t.Errorf("engineless stage Δv = %g, want 0", vs.StageDV[1])
	}
	if vs.TotalMass != 2000+8000+100+500 {
		t.Errorf("TotalMass = %g, want 10600 (incl monoprop)", vs.TotalMass)
	}
	if StackStats(nil).TotalDV != 0 || StackStats(nil).LiftoffTWR != 0 {
		t.Error("empty stack should be all-zero stats")
	}
}
