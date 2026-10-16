package spacecraft

import (
	"math"
	"testing"
)

// Two payload groups ([2,3] and [4]) over a carrier [0,1]: a group's stage
// hauls only to its group's top, the carrier hauls everything.
func TestStackStatsSeamsGroupBoundaries(t *testing.T) {
	mk := func(dry, fuel float64) Stage { return Stage{DryMass: dry, FuelMass: fuel, Isp: 300} }
	stages := []Stage{mk(100, 900), mk(100, 900), mk(50, 100), mk(50, 100), mk(50, 100)}
	seams := []bool{false, false, true, false, true}
	st := StackStatsSeams(stages, seams)
	want2 := 300 * g0 * math.Log((50+100+50+100)/(50+100.0+50))
	want3 := 300 * g0 * math.Log((50+100)/50.0)
	if math.Abs(st.StageDV[2]-want2) > 1e-6 || math.Abs(st.StageDV[3]-want3) > 1e-6 {
		t.Errorf("group [2,3] Δv = %.1f/%.1f, want %.1f/%.1f", st.StageDV[2], st.StageDV[3], want2, want3)
	}
	if math.Abs(st.StageDV[4]-want3) > 1e-6 {
		t.Errorf("group [4] Δv = %.1f, want its own %.1f", st.StageDV[4], want3)
	}
	if math.Abs(st.TotalDV-(st.StageDV[0]+st.StageDV[1])) > 1e-6 {
		t.Errorf("TotalDV %.1f, want carrier rows %.1f", st.TotalDV, st.StageDV[0]+st.StageDV[1])
	}
	if flat := StackStats(stages); math.Abs(flat.StageDV[0]-st.StageDV[0]) > 1e-6 {
		t.Errorf("carrier row changed: %.1f vs %.1f", st.StageDV[0], flat.StageDV[0])
	}
}
