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

// f9Golden was taken before the F9 stages gained their flight computer and
// S-band antenna. SpaceX's published dry masses already include the avionics
// and telemetry hardware, so giving the stages a probe command source and a
// basic antenna must add no kilogram: mass, delta-v and liftoff TWR of both
// parts and of every shipped vehicle that stacks them stay identical.
var f9Golden = []string{
	"part:f9-s1|1|25600.000|411000.000|438275.636|7844.063534|1.769886",
	"part:f9-s2|1|3900.000|107500.000|111655.273|11439.932548|0.852996",
	"loadout:Falcon-9|2|29500.000|518500.000|549930.909|15273.695362|1.410537|craft 549930.909",
	"loadout:Falcon-9-Dragon|4|39000.000|521062.000|563414.727|11331.400032|1.376780|craft 563414.727",
	"loadout:Falcon-9-Cargo-Dragon|4|39000.000|521062.000|565921.727|10704.173493|1.370681|craft 565921.727",
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
