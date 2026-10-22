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

// StackStats computes the VAB readout for a bottom-first stage stack with no
// payload seams: every stage hauls everything above it.
func StackStats(stages []Stage) VehicleStats { return StackStatsSeams(stages, nil) }

// StackStatsSeams is StackStats for a stack with payload (dock) seams.
// seamBelow[i] marks a seam under stage i (index 0 is never a seam). A payload
// group runs from a seamed stage up to the next seam or the top. Per-stage Δv
// stops at the group's top: a payload stage reads the Δv it has on its own
// (what it flies with once released), while stages below the first seam carry
// the payloads as mass. TotalDV is the carrier chain alone (the stages under
// the first seam), not a sum over payloads (ADR 0056 decision 9).
func StackStatsSeams(stages []Stage, seamBelow []bool) VehicleStats {
	seamed := func(i int) bool { return i >= 1 && i < len(seamBelow) && seamBelow[i] }
	// top[i] is the exclusive upper bound of the stack stage i hauls; carrier[i]
	// marks the stages under the first seam.
	top := make([]int, len(stages))
	carrier := make([]bool, len(stages))
	inCarrier, groupStart := true, 0
	for i := range stages {
		if seamed(i) {
			inCarrier, groupStart = false, i
		}
		if inCarrier {
			carrier[i], top[i] = true, len(stages)
			continue
		}
		e := groupStart + 1
		for e < len(stages) && !seamed(e) {
			e++
		}
		top[i] = e
	}
	massUpTo := func(i int) float64 {
		m := 0.0
		for j := i; j < top[i]; j++ {
			m += stages[j].DryMass + stages[j].FuelMass + stages[j].CargoKg + stages[j].HoldKg
		}
		return m
	}
	// A marked stage (ADR 0058) lights with the stage above it: its row is
	// the blended first phase, and the core above arrives at its own phase
	// carrying only what the first phase left in its tank (coreBurn).
	// shortcut: a booster tank that outlasts its core loses the leftover
	// Δv; the grill's pairs (SRB under a core) never do, revisit with the
	// first liquid-booster vehicle.
	coreBurn := make([]float64, len(stages))
	boosterBurn := make([]float64, len(stages))
	for i := range stages {
		if c := i + 1; stages[i].LitWithAbove && (i == 0 || !stages[i-1].LitWithAbove) && c < top[i] && !seamed(c) &&
			stages[i].FuelMass > 0 && stages[c].FuelMass > 0 {
			b := litPhaseBurn(stages, []int{i, c})
			boosterBurn[i], coreBurn[c] = b[0], b[1]
		}
	}
	vs := VehicleStats{StageDV: make([]float64, len(stages))}
	for i, s := range stages {
		fuel := s.FuelMass - coreBurn[i]
		if fuel > 0 && s.Isp > 0 {
			m0 := massUpTo(i) - coreBurn[i]
			m1 := m0 - fuel
			isp := s.Isp
			if boosterBurn[i] > 0 {
				fuel = boosterBurn[i] + coreBurn[i+1]
				m1 = m0 - fuel
				isp = (s.Thrust + stages[i+1].Thrust) / (s.Thrust/s.Isp + stages[i+1].Thrust/stages[i+1].Isp)
			}
			if m1 > 0 {
				dv := isp * g0 * math.Log(m0/m1)
				vs.StageDV[i] = dv
				if carrier[i] {
					vs.TotalDV += dv
				}
			}
		}
		vs.TotalMass += s.DryMass + s.FuelMass + s.MonopropMass + s.CargoKg + s.HoldKg
	}
	if len(stages) > 0 && vs.TotalMass > 0 {
		thrust, _ := litEngine(stages)
		vs.LiftoffTWR = thrust / (vs.TotalMass * g0)
	}
	return vs
}
