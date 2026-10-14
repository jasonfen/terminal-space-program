package sim

import (
	"reflect"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

// useBareStage swaps the fresh world's start vessel (Apollo TLI-1 since ADR
// 0053 §4) for the bare S-IVB-1 at the same orbit, name-compatible with the
// fixtures written when that was the default: tests of burn arithmetic, hull
// geometry (an 18 m stage) or a Moon transfer one stage can afford keep their
// numbers. Returns the new active vessel; it must run before the test takes
// a pointer to w.Crafts[0].
func useBareStage(w *World) *spacecraft.Spacecraft {
	old := w.Crafts[0]
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutSIVB1ID)
	c.Name = "S-IVB-1"
	c.Primary = old.Primary
	c.SystemIdx = old.SystemIdx
	c.CurrentAttitudeDir = old.CurrentAttitudeDir
	c.State = old.State
	c.State.M = c.TotalMass()
	c.ID = old.ID
	crewTend(c)
	w.Crafts[0] = c
	return c
}

// Jason's ruling 2026-10-14: the start stack's S-IVB carries the real Apollo
// post-orbit-insertion propellant (~75 t) and flies the whole Moon burn alone;
// the bare S-IVB-1 stays at 40 t.
func TestApolloTLIStageCarriesPostInsertionPropellant(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	s := w.ActiveCraft().Stages[0]
	if s.Name != "S-IVB" || s.FuelCapacity != 75000 || s.FuelMass != 75000 || s.DryMass != 11000 || s.Thrust != 1023000 || s.Isp != 421 {
		t.Errorf("start S-IVB = %+v, want 75 t fuel, 11 t dry, 1023 kN, 421 s", s)
	}
	if bare := spacecraft.NewFromLoadout(spacecraft.LoadoutSIVB1ID).Stages[0]; bare.FuelCapacity != 40000 {
		t.Errorf("bare S-IVB-1 fuel = %v, want 40000", bare.FuelCapacity)
	}
}
