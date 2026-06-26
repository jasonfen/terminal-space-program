package spacecraft

import "math"

// VehicleStats summarizes a bottom-first stage stack for the VAB readout
// (ADR 0029 §5). It is a pure function of the resolved Stages, recomputed
// on every edit so the builder always shows live numbers.
//
//   - StageDV is the per-stage ideal Δv (m/s), bottom→top: stage i fires
//     hauling itself plus everything above it (the drop-stage chain), then
//     is jettisoned. Mirrors the convention of the existing loadout Δv
//     tests (RCS monoprop excluded — this is main-engine ideal Δv).
//   - TotalDV is the sum of StageDV.
//   - TotalMass is the wet mass on the pad (dry + main fuel + RCS monoprop),
//     across every stage, kg.
//   - LiftoffTWR is the bottom stage's thrust over the full stack weight at
//     g0 (the reference launch gravity — the VAB designs a template, not a
//     spawn on a specific body; the Apollo / Kern liftoff-TWR tests use the
//     same g0 reference).
type VehicleStats struct {
	StageDV    []float64
	TotalDV    float64
	TotalMass  float64
	LiftoffTWR float64
}

// StackStats computes the VAB readout for a bottom-first stage stack.
func StackStats(stages []Stage) VehicleStats {
	massAtOrAbove := func(i int) float64 {
		m := 0.0
		for j := i; j < len(stages); j++ {
			m += stages[j].DryMass + stages[j].FuelMass
		}
		return m
	}
	vs := VehicleStats{StageDV: make([]float64, len(stages))}
	for i, s := range stages {
		if s.FuelMass > 0 && s.Isp > 0 {
			m0 := massAtOrAbove(i)
			m1 := m0 - s.FuelMass
			if m1 > 0 {
				dv := s.Isp * g0 * math.Log(m0/m1)
				vs.StageDV[i] = dv
				vs.TotalDV += dv
			}
		}
		vs.TotalMass += s.DryMass + s.FuelMass + s.MonopropMass
	}
	if len(stages) > 0 && vs.TotalMass > 0 {
		vs.LiftoffTWR = stages[0].Thrust / (vs.TotalMass * g0)
	}
	return vs
}
