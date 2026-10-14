package sim

import (
	"reflect"
	"testing"
)

// ADR 0053 §4 (slice F): a new game starts in Apollo TLI-1, the S-IVB with
// the Lunar Module and CSM still on top, port on the Command Module's nose.
func TestNewWorldStartsInApolloTLI(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active vessel")
	}
	if c.Name != "Apollo TLI-1" || c.LoadoutID != "Apollo-TLI" {
		t.Errorf("start vessel = %q / %q, want Apollo TLI-1 / Apollo-TLI", c.Name, c.LoadoutID)
	}
	var names []string
	for _, s := range c.Stages {
		names = append(names, s.Name)
	}
	want := []string{"S-IVB", "Descent", "Ascent", "SM", "CM"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("stages = %v, want %v", names, want)
	}
	if !reflect.DeepEqual(c.DecouplePlan, []int{1, 2}) {
		t.Errorf("decouple plan = %v, want [1 2]", c.DecouplePlan)
	}
	g := VesselPortGeometry(c)
	if !g.HasPort() || g.TopName != "CM" {
		t.Errorf("port geometry: HasPort=%v top=%q, want a port on CM", g.HasPort(), g.TopName)
	}
}

// One space drops the S-IVB; D (Transpose) is then legal.
func TestApolloTLITransposesAfterOneSpace(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	crewTendActive(w)
	if err := w.TransposeRefusal(w.ActiveCraftIdx); err == nil {
		t.Fatal("transposition must be refused while the S-IVB is still attached")
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatalf("space: %v", err)
	}
	if err := w.Transpose(w.ActiveCraftIdx); err != nil {
		t.Fatalf("D after one space: %v", err)
	}
}
