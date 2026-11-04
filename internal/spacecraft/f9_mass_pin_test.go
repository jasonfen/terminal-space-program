package spacecraft

import (
	"fmt"
	"testing"
)

func usesF9(stages []Stage) bool {
	for _, st := range stages {
		if st.Name == "F9-S1" || st.Name == "F9-S2" {
			return true
		}
	}
	return false
}

// f9Row is one vehicle's mass, delta-v and liftoff TWR as StackStats reads them.
func f9Row(id string, stages []Stage) string {
	s := StackStats(stages)
	dry, fuel := 0.0, 0.0
	for _, st := range stages {
		dry += st.DryMass
		fuel += st.FuelMass
	}
	return fmt.Sprintf("%s|%d|%.3f|%.3f|%.3f|%.6f|%.6f", id, len(stages), dry, fuel, s.TotalMass, s.TotalDV, s.LiftoffTWR)
}

func f9Rows() []string {
	var rows []string
	for _, id := range []string{"f9-s1", "f9-s2"} {
		st, _ := BuildStage(id)
		rows = append(rows, f9Row("part:"+id, []Stage{st}))
	}
	for _, id := range LoadoutOrder {
		l := Loadouts[id]
		if usesF9(l.Stages) {
			rows = append(rows, f9Row("loadout:"+id, l.Stages)+fmt.Sprintf("|craft %.3f", NewFromLoadout(id).TotalMass()))
		}
	}
	subs, _ := ListSubassemblies()
	for _, s := range subs {
		if !s.Shipped() {
			continue
		}
		if l, _ := s.Resolve(); usesF9(l.Stages) {
			rows = append(rows, f9Row("sub:"+s.ID(), l.Stages))
		}
	}
	return rows
}

// Re-captured 2026-11-04 for ADR 0064 decision 3 (f9-s2 4,000 kg / 981 kN, both
// diameters 3.7 m; f9-s1 rows unchanged). Originally taken before the F9 stages gained their flight computer and
// S-band antenna. SpaceX's published dry masses already include the avionics
// and telemetry hardware, so giving the stages a probe command source and a
// basic antenna must add no kilogram: mass, delta-v and liftoff TWR of both
// parts and of every shipped vehicle that stacks them stay identical.
var f9Golden = []string{
	"part:f9-s1|1|25600.000|411000.000|438275.636|7844.063534|1.769886",
	"part:f9-s2|1|4000.000|107500.000|111761.818|11356.592209|0.895066",
	"loadout:Falcon-9|2|29600.000|518500.000|550037.455|15188.841767|1.410264|craft 550037.455",
	"loadout:Falcon-Heavy|3|80800.000|1340500.000|1426588.727|16946.421960|1.631230|craft 1426588.727",
	"loadout:Falcon-9-Dragon|4|39100.000|521062.000|563521.273|11312.494277|1.376520|craft 563521.273",
	"loadout:Falcon-9-Cargo-Dragon|4|39100.000|521062.000|566028.273|10687.875847|1.370423|craft 566028.273",
	"loadout:Falcon-9-Starlink-24|26|47360.000|518980.000|569439.927|11948.111209|1.362212|craft 569439.927",
}

func TestF9ControlAndAntennaAddNoMass(t *testing.T) {
	got := f9Rows()
	if len(got) != len(f9Golden) {
		t.Fatalf("%d F9 rows, want %d:\n%v", len(got), len(f9Golden), got)
	}
	for i := range got {
		if got[i] != f9Golden[i] {
			t.Errorf("F9 row changed:\n got %s\nwant %s", got[i], f9Golden[i])
		}
	}
}

// The F9 stages are control points with the basic near-Earth antenna, so a
// bare F9 is commandable in LEO and needs a relay at the Moon, and the
// default-core backfill has nothing left to add to a stack that has them.
func TestF9StagesCarryProbeCoreAndBasicAntenna(t *testing.T) {
	for _, id := range []string{"f9-s1", "f9-s2"} {
		st, ok := BuildStage(id)
		if !ok {
			t.Fatalf("no stage %s", id)
		}
		if st.CommandSource != CommandProbe {
			t.Errorf("%s command source = %q, want probe", id, st.CommandSource)
		}
		if st.AntennaKind != AntennaDirect || st.AntennaRangeM != AntennaRangeDirectBasic {
			t.Errorf("%s antenna = %s/%.0e m, want direct/%.0e m", id, st.AntennaKind, st.AntennaRangeM, AntennaRangeDirectBasic)
		}
	}
	// The backfill adds nothing: no stage other than the F9's own gains a source.
	before := NewFromLoadout("Falcon-9")
	for i, st := range before.Stages {
		if st.Name == "F9-S1" || st.Name == "F9-S2" {
			continue
		}
		if st.CommandSource != "" || st.AntennaKind != AntennaNone {
			t.Errorf("Falcon-9 stage %d (%s) was backfilled with %q / %s", i, st.Name, st.CommandSource, st.AntennaKind)
		}
	}
	if !before.Controllable || before.AntennaKind != AntennaDirect {
		t.Errorf("Falcon-9: controllable=%v antenna=%s, want controllable with a direct antenna", before.Controllable, before.AntennaKind)
	}
}

// ADR 0064 decision 3: the F9 stages carry SpaceX's published figures. The
// f9-s1 rows that did not move are pinned too, so a swapped source (Wikipedia's
// 22,200 kg core) reads red.
func TestF9StagesTakePublishedFigures(t *testing.T) {
	s1, _ := BuildStage("f9-s1")
	s2, _ := BuildStage("f9-s2")
	if s1.DiameterM != 3.7 || s1.DryMass != 25600 || s1.FuelMass != 411000 || s1.Thrust != 7607000 || s1.Isp != 282 {
		t.Errorf("f9-s1 = %gm dry %g fuel %g thrust %g isp %g", s1.DiameterM, s1.DryMass, s1.FuelMass, s1.Thrust, s1.Isp)
	}
	if s2.DiameterM != 3.7 || s2.DryMass != 4000 || s2.Thrust != 981000 || s2.Isp != 348 || s2.FuelMass != 107500 {
		t.Errorf("f9-s2 = %gm dry %g thrust %g isp %g fuel %g", s2.DiameterM, s2.DryMass, s2.Thrust, s2.Isp, s2.FuelMass)
	}
}
