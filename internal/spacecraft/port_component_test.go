package spacecraft

import (
	"reflect"
	"testing"
)

// ADR 0056 decision 7: a Docking Port component puts a nose port on the
// stage that holds it; a stage without one has none.
func TestComposeStageDockingPortComponent(t *testing.T) {
	c, ok := Components["docking-port"]
	if !ok {
		t.Fatal("components.json has no docking-port")
	}
	if c.Name != "Docking Port" || c.Kind != ComponentStructure || c.DryMassKg != 50 {
		t.Errorf("docking-port = %+v, want Docking Port / structure / 50 kg", c)
	}
	with, warn := ComposeStage([]string{"probe-core", "docking-port"}, Components)
	if warn != "" {
		t.Fatal(warn)
	}
	if ps := with.Ports(); len(ps) != 1 || ps[0].End != PortEndTop || !with.HasPort() {
		t.Errorf("[probe-core, docking-port] ports = %+v, want one top port", ps)
	}
	without, warn := ComposeStage([]string{"probe-core"}, Components)
	if warn != "" {
		t.Fatal(warn)
	}
	if without.HasPort() {
		t.Errorf("[probe-core] ports = %+v, want none", without.Ports())
	}
	if d := with.DryMass - without.DryMass; d != 50 {
		t.Errorf("the port adds %v kg, want 50", d)
	}
}

// A composed catalog part keeps its own Part-level port through composePart
// (probe-sat-stage), and the component union adds to it rather than replacing.
func TestComposePartKeepsPartLevelPorts(t *testing.T) {
	st, ok := BuildStage("probe-sat-stage")
	if !ok || len(st.Ports()) != 1 {
		t.Fatalf("probe-sat-stage ports = %+v ok=%v, want one", st.Ports(), ok)
	}
	p := Part{ID: "x", Ports: []PartPort{{End: PortEndTop}}, Components: []string{"probe-core", "docking-port"}}
	out, err := composePart(p, Components)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Ports) != 2 {
		t.Errorf("union = %+v, want the part's port and the component's", out.Ports)
	}
	if len(p.Ports) != 1 {
		t.Error("composePart mutated the input part's ports")
	}
}

// The trap in ADR 0056 decision 7: a Stage.Ports() that falls back to the
// name lookup would hide a missing field. A stage named like a ported part
// but carrying no ports of its own reads port-less.
func TestStagePortsIsTheFieldNotTheNameLookup(t *testing.T) {
	for _, s := range []Stage{
		{Name: "CM"},
		{LoadoutID: "Apollo-TLI", Name: "CM"},
		{LoadoutID: "RCS-tug", Name: "RCS Tug"},
	} {
		if s.HasPort() {
			t.Errorf("%s/%s reads ported with no stored ports", s.LoadoutID, s.Name)
		}
	}
	if len(LookupCatalogPorts("Apollo-TLI", "CM")) == 0 {
		t.Fatal("instrument check: the lookup should still know the CM's port")
	}
}

// Loadout and BuildStage stages store the same ports the old lookup returned.
func TestBuiltStagesStoreTheirLookupPorts(t *testing.T) {
	for _, id := range LoadoutOrder {
		for i, s := range Loadouts[id].Stages {
			if want := LookupCatalogPorts(s.LoadoutID, s.Name); !reflect.DeepEqual(s.Ports(), want) && (len(want)+len(s.Ports()) > 0) {
				t.Errorf("%s stage %d %q stored %+v, lookup %+v", id, i, s.Name, s.Ports(), want)
			}
		}
	}
}
