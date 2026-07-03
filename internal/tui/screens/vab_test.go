package screens

import (
	"math"
	"reflect"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// S3 / ADR 0029 §5 — the VAB screen model. These drive the model operations
// directly (what HandleKey maps onto) so the editor logic is tested without
// the render or the App.

func testVABComps() map[string]spacecraft.Component {
	return map[string]spacecraft.Component{
		"eng":   {ID: "eng", Name: "Big Engine", Kind: spacecraft.ComponentEngine, DryMassKg: 500, ThrustN: 200_000, IspS: 300, FuelType: spacecraft.FuelTypeKerolox},
		"eng2":  {ID: "eng2", Name: "Vac Engine", Kind: spacecraft.ComponentEngine, DryMassKg: 400, ThrustN: 50_000, IspS: 320, FuelType: spacecraft.FuelTypeKerolox},
		"tank":  {ID: "tank", Name: "Big Tank", Kind: spacecraft.ComponentTank, DryMassKg: 100, FuelCapacityKg: 9000, FuelType: spacecraft.FuelTypeKerolox},
		"tank2": {ID: "tank2", Name: "Vac Tank", Kind: spacecraft.ComponentTank, DryMassKg: 50, FuelCapacityKg: 4500, FuelType: spacecraft.FuelTypeKerolox},
		"core":  {ID: "core", Name: "Crew Pod", Kind: spacecraft.ComponentCommandCore, DryMassKg: 400, CommandSource: spacecraft.CommandCrewed},
		"hydro": {ID: "hydro", Name: "H2 Tank", Kind: spacecraft.ComponentTank, DryMassKg: 100, FuelCapacityKg: 2000, FuelType: spacecraft.FuelTypeHydrolox},
	}
}

// TestVABAddRemoveComponentUpdatesStats — adding a tank to a stage raises its
// Δv (more propellant); removing it restores the prior value.
func TestVABAddRemoveComponentUpdatesStats(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	dv1 := v.Stats().StageDV[0]
	if dv1 <= 0 {
		t.Fatalf("StageDV[0] = %g, want > 0 after engine+tank", dv1)
	}
	v.addComponentToCurrent("tank2") // second same-chemistry tank
	dv2 := v.Stats().StageDV[0]
	if dv2 <= dv1 {
		t.Errorf("adding a tank did not raise Δv: %g → %g", dv1, dv2)
	}
	v.removeFromCurrent() // pop the second tank
	if dv := v.Stats().StageDV[0]; math.Abs(dv-dv1) > 1e-6 {
		t.Errorf("remove did not restore Δv: %g, want %g", dv, dv1)
	}
}

// TestVABAddRemoveStageUpdatesTotals — a second stage raises total Δv and
// mass; removing it restores them.
func TestVABAddRemoveStageUpdatesTotals(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	one := v.Stats()
	v.newStage()
	v.addComponentToCurrent("eng2")
	v.addComponentToCurrent("tank2")
	two := v.Stats()
	if two.TotalDV <= one.TotalDV {
		t.Errorf("second stage did not raise total Δv: %g → %g", one.TotalDV, two.TotalDV)
	}
	if two.TotalMass <= one.TotalMass {
		t.Errorf("second stage did not raise total mass: %g → %g", one.TotalMass, two.TotalMass)
	}
	v.stageIdx = 1
	v.removeFromCurrent() // removes tank2
	v.removeFromCurrent() // removes eng2
	v.removeFromCurrent() // removes the now-empty stage
	if got := v.Stats(); math.Abs(got.TotalDV-one.TotalDV) > 1e-6 || len(v.stages) != 1 {
		t.Errorf("removing the second stage did not restore totals: %+v (stages=%d)", got, len(v.stages))
	}
}

// TestVABDeltaVHandWorked — the per-stage Δv from the aggregated stack
// matches the rocket equation, and TWR is consistent with total mass.
func TestVABDeltaVHandWorked(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")  // 500 dry, 200 kN, Isp 300
	v.addComponentToCurrent("tank") // +100 dry, +9000 fuel
	st := v.resolvedStages()[0]
	// Aggregated: dry 600, fuel 9000, thrust 200 kN, Isp 300.
	m0 := st.DryMass + st.FuelMass // 9600
	m1 := st.DryMass               // 600
	wantDV := 300 * 9.80665 * math.Log(m0/m1)
	if got := v.Stats().StageDV[0]; math.Abs(got-wantDV) > 1e-3 {
		t.Errorf("StageDV[0] = %g, want %g (rocket eq over the aggregated stage)", got, wantDV)
	}
	stats := v.Stats()
	wantTWR := st.Thrust / (stats.TotalMass * 9.80665)
	if math.Abs(stats.LiftoffTWR-wantTWR) > 1e-9 {
		t.Errorf("LiftoffTWR = %g, want %g (consistent with total mass)", stats.LiftoffTWR, wantTWR)
	}
}

// TestVABSeamCyclingPlan — toggling dock seams produces the expected
// top-down NosePayloadPlan (the N-seam generalization that closes ADR 0028
// §8).
func TestVABSeamCyclingPlan(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("tank") // fill the auto-seeded stage 0
	for i := 0; i < 2; i++ {         // two more single-component stages
		v.newStage()
		v.addComponentToCurrent("tank")
	}
	if len(v.stages) != 3 {
		t.Fatalf("stages = %d, want 3", len(v.stages))
	}
	if v.nosePayloadPlan() != nil {
		t.Errorf("no seams ⇒ plan should be nil, got %v", v.nosePayloadPlan())
	}
	v.stageIdx = 2
	v.toggleDockSeam() // seam below the top stage → top 1 stage is a payload
	if got := v.nosePayloadPlan(); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("one top seam ⇒ plan %v, want [1]", got)
	}
	v.stageIdx = 1
	v.toggleDockSeam() // second seam → two single-stage payloads
	if got := v.nosePayloadPlan(); !reflect.DeepEqual(got, []int{1, 1}) {
		t.Errorf("two seams ⇒ plan %v, want [1 1]", got)
	}
}

// TestVABMixedChemistryRejected — adding a second fuel chemistry to a stage
// is rejected in the model (not just the view); the component is not added
// and a flash explains why.
func TestVABMixedChemistryRejected(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")   // kerolox
	v.addComponentToCurrent("hydro") // hydrolox — must be rejected
	if n := len(v.stages[0].components); n != 1 {
		t.Errorf("mixed-chemistry component was added: stage has %d components, want 1", n)
	}
	if v.flash == "" {
		t.Error("mixed-chemistry add should set a flash explaining the reject")
	}
}

// TestVABCatalogPartAsStage — adding a catalog-part palette item appends an
// opaque atomic stage that resolves to the catalog part's stats.
func TestVABCatalogPartAsStage(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(map[string]spacecraft.Component{}) // empty comps ⇒ palette is catalog parts only
	if len(v.palette) == 0 {
		t.Fatal("palette empty — expected single-stage catalog parts")
	}
	// Reset auto-seeds an empty composed stage; adding a catalog part appends
	// an opaque atomic stage on top of it.
	v.addSelected()
	top := v.stages[len(v.stages)-1]
	if !top.isCatalog() {
		t.Fatalf("top stage is not an atomic catalog stage: %+v", top)
	}
	if v.resolveStage(top).DryMass <= 0 {
		t.Error("catalog stage resolved to zero dry mass")
	}
}

// TestVABSaveLoadRoundTrip — saving a design through the store and loading it
// back reconstructs an equivalent design (the screen's serialization ↔
// rebuild fidelity).
func TestVABSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.addComponentToCurrent("core")
	v.newStage()
	v.addComponentToCurrent("eng2")
	v.addComponentToCurrent("tank2")
	v.stageIdx = 1
	v.toggleDockSeam() // a nose-payload seam to exercise plan round-trip
	v.name = "Test Hopper"

	before := v.toDesign()
	if err := spacecraft.SaveDesign(before); err != nil {
		t.Fatalf("SaveDesign: %v", err)
	}

	// Wipe and reload through the screen's load path.
	v.Reset(testVABComps())
	v.refreshDesigns()
	if len(v.designs) != 1 {
		t.Fatalf("designs after save = %d, want 1", len(v.designs))
	}
	v.loadDesign(v.designs[0])
	after := v.toDesign()

	if !reflect.DeepEqual(before, after) {
		t.Errorf("save/load round-trip drift:\n before %+v\n  after %+v", before, after)
	}
	if v.nosePayloadPlan() == nil {
		t.Error("dock seam lost across save/load")
	}
}

// TestVABEmptyStageRoundTrip — an empty composed stage (added with [n], no
// components) survives save/load as a composed stage, not a bogus catalog
// reference (the discriminator is design-local presence, not component count).
func TestVABEmptyStageRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.newStage() // an empty composed stage on top
	v.name = "Has Empty"
	if err := spacecraft.SaveDesign(v.toDesign()); err != nil {
		t.Fatalf("SaveDesign: %v", err)
	}
	v.Reset(testVABComps())
	v.refreshDesigns()
	if len(v.designs) != 1 {
		t.Fatalf("designs = %d, want 1", len(v.designs))
	}
	v.loadDesign(v.designs[0])
	if len(v.stages) != 2 {
		t.Fatalf("reloaded stages = %d, want 2", len(v.stages))
	}
	if v.stages[1].isCatalog() {
		t.Error("empty composed stage round-tripped into a catalog reference")
	}
}

// TestVABDecouplePlanRoundTrip — the decouple-fuse flags survive a
// derive→apply round-trip.
func TestVABDecouplePlanRoundTrip(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	for i := 0; i < 4; i++ {
		v.newStage()
		v.addComponentToCurrent("tank")
	}
	v.stageIdx = 1
	v.toggleDecoupleFuse() // stages 0+1 drop together
	plan := v.decouplePlan()
	if plan == nil {
		t.Fatal("decouplePlan nil after fusing a boundary")
	}
	v.applyDecouplePlan(plan)
	if got := v.decouplePlan(); !reflect.DeepEqual(got, plan) {
		t.Errorf("decouple plan round-trip: %v → %v", plan, got)
	}
}

// TestVABWarnings — soft validation flags the common pitfalls.
func TestVABWarnings(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = nil // a truly-empty stack (Reset auto-seeds one stage)
	if w := v.Warnings(); len(w) != 1 {
		t.Errorf("empty stack warnings = %v, want one (empty)", w)
	}
	v.addComponentToCurrent("tank") // tank only: no engine, no command
	w := v.Warnings()
	joined := ""
	for _, s := range w {
		joined += s + "\n"
	}
	if !contains(joined, "no engine") || !contains(joined, "no command source") {
		t.Errorf("warnings = %v, want no-engine + no-command", w)
	}
}

// TestVABRenderSmoke — every mode renders without panicking and surfaces the
// expected anchors (stats line, palette, footer).
func TestVABRenderSmoke(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.addComponentToCurrent("core")
	build := v.Render(100)
	for _, want := range []string{"Vehicle Assembly", "VEHICLE", "Σ Δv", "PALETTE", "inspect", "[a] add"} {
		if !contains(build, want) {
			t.Errorf("build render missing %q", want)
		}
	}
	v.mode = vabModeNaming
	if !contains(v.Render(100), "save design") {
		t.Error("naming render missing title")
	}
	v.mode = vabModeLoad
	if !contains(v.Render(100), "load design") {
		t.Error("load render missing title")
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (indexOf(s, sub) >= 0) }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
