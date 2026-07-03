package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// S1 / ADR 0032 — in-place row editing: ←/→ swap-within-kind on the fold
// rows (engine leads chemistry), empty-stage placeholder rows, and the
// tab-only column switch. These drive the model methods directly.

// chemVABComps is a two-chemistry catalog (kerolox + hydrolox engines AND
// tanks) so the chemistry-leader rules can be exercised. IDs are chosen so the
// sort order is deterministic: engines [h-eng, k-eng], tanks [h-tank, k-tank].
func chemVABComps() map[string]spacecraft.Component {
	return map[string]spacecraft.Component{
		"k-eng":  {ID: "k-eng", Name: "Kero Engine", Kind: spacecraft.ComponentEngine, DryMassKg: 500, ThrustN: 200_000, IspS: 300, FuelType: spacecraft.FuelTypeKerolox},
		"h-eng":  {ID: "h-eng", Name: "Hydro Engine", Kind: spacecraft.ComponentEngine, DryMassKg: 500, ThrustN: 180_000, IspS: 420, FuelType: spacecraft.FuelTypeHydrolox},
		"k-tank": {ID: "k-tank", Name: "Kero Tank", Kind: spacecraft.ComponentTank, DryMassKg: 100, FuelCapacityKg: 9000, FuelType: spacecraft.FuelTypeKerolox},
		"h-tank": {ID: "h-tank", Name: "Hydro Tank", Kind: spacecraft.ComponentTank, DryMassKg: 100, FuelCapacityKg: 4000, FuelType: spacecraft.FuelTypeHydrolox},
	}
}

func placeholderKindSet(v *VAB, i int) []string { return v.placeholderKinds(i) }

// TestVABPlaceholderKinds — an empty composed stage prompts for both engine
// and tank; once one propulsion kind is present the OTHER stays prompted; a
// non-propulsion-only stage stays clean; a full stage shows none (ADR 0032 §5).
func TestVABPlaceholderKinds(t *testing.T) {
	cases := []struct {
		name  string
		comps []string
		want  []string
	}{
		{"empty", nil, []string{spacecraft.ComponentEngine, spacecraft.ComponentTank}},
		{"engine only", []string{"eng"}, []string{spacecraft.ComponentTank}},
		{"tank only", []string{"tank"}, []string{spacecraft.ComponentEngine}},
		{"engine+tank", []string{"eng", "tank"}, nil},
		{"structure/core only", []string{"core"}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := NewVAB(Theme{})
			v.Reset(testVABComps())
			v.stages = []vabStage{{components: c.comps}}
			got := placeholderKindSet(v, 0)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("placeholderKinds = %v, want %v", got, c.want)
			}
		})
	}
}

// TestVABPlaceholderRowsAppear — a truly-empty composed stage synthesizes two
// navigable placeholder rows (engine — / tank —) in kind order (ADR 0032 §5).
func TestVABPlaceholderRowsAppear(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{}}
	groups := v.rowGroups(0)
	if len(groups) != 2 || !groups[0].placeholder || !groups[1].placeholder {
		t.Fatalf("rowGroups = %+v, want two placeholder groups", groups)
	}
	if groups[0].kind != spacecraft.ComponentEngine || groups[1].kind != spacecraft.ComponentTank {
		t.Errorf("placeholder kinds = %q,%q, want engine,tank", groups[0].kind, groups[1].kind)
	}
	// Header + 2 placeholder rows.
	if rows := v.stackRows(); len(rows) != 3 {
		t.Errorf("stackRows = %d, want 3 (header + 2 placeholders)", len(rows))
	}
}

// TestVABBuildLoopNoPaletteTrip — the ←/→ → ←/→ loop builds an engine+tank
// stage entirely in the vehicle column, no palette add (ADR 0032 §5). Reset
// auto-seeds the first stage with the cursor on its engine placeholder, so the
// loop runs the moment the screen opens. Also checks the tank placeholder
// survives the engine pick.
func TestVABBuildLoopNoPaletteTrip(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	// Reset auto-seeds an empty stage 0 with the cursor already on its engine
	// placeholder — no `n` needed.
	if r, ok := v.currentRow(); !ok || r.isHeader() || !v.rowGroups(r.stageIdx)[r.group].placeholder {
		t.Fatalf("after Reset the cursor is not on a placeholder row: %+v", r)
	}
	v.swapRow(+1) // pick the first engine
	if countComp(v.stages[0], "eng") != 1 {
		t.Fatalf("engine not added by placeholder swap: %v", v.stages[0].components)
	}
	// Engine present, tank absent → tank placeholder must remain for the loop.
	groups := v.rowGroups(0)
	if len(groups) != 2 || groups[0].placeholder || !groups[1].placeholder || groups[1].kind != spacecraft.ComponentTank {
		t.Fatalf("after engine pick, groups = %+v, want [engine real, tank placeholder]", groups)
	}
	v.moveStackCursor(+1) // down onto the tank placeholder
	v.swapRow(+1)         // pick the first compatible tank
	if countComp(v.stages[0], "tank") != 1 {
		t.Fatalf("tank not added by placeholder swap: %v", v.stages[0].components)
	}
	if k := v.placeholderKinds(0); len(k) != 0 {
		t.Errorf("placeholders %v remain after engine+tank, want none", k)
	}
}

// TestVABSwapEngineLeadsAllEngines — ←/→ on an engine row cycles through ALL
// engines regardless of chemistry (the leader, ADR 0032 §4), preserving count.
func TestVABSwapEngineLeadsAllEngines(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(chemVABComps())
	v.stages = []vabStage{{components: []string{"k-eng", "k-eng", "k-tank"}}} // eng ×2
	v.stackCursor = v.headerRowIndex(0) + 1                                    // engine group (rank 0)
	if r, _ := v.currentRow(); v.rowGroups(r.stageIdx)[r.group].compID != "k-eng" {
		t.Fatalf("cursor not on the engine group")
	}
	v.swapRow(+1) // engines sorted [h-eng, k-eng]; k-eng → wrap → h-eng
	if countComp(v.stages[0], "h-eng") != 2 || countComp(v.stages[0], "k-eng") != 0 {
		t.Errorf("engine swap did not replace all instances: %v", v.stages[0].components)
	}
}

// TestVABChemistryCrossingEngineSwap — a chemistry-crossing engine swap lands,
// leaves the tanks in place, and the stage goes soft-invalid; the tank row
// then cycles the NEW chemistry so one ←/→ repairs it (ADR 0032 §4).
func TestVABChemistryCrossingEngineSwap(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(chemVABComps())
	v.stages = []vabStage{{components: []string{"k-eng", "k-tank"}}}
	v.stackCursor = v.headerRowIndex(0) + 1 // engine row
	v.swapRow(+1)                           // k-eng → h-eng (all-engines leader)
	if countComp(v.stages[0], "h-eng") != 1 || countComp(v.stages[0], "k-tank") != 1 {
		t.Fatalf("crossing swap should land engine + keep tank: %v", v.stages[0].components)
	}
	if v.stageEngineChem(0) != spacecraft.FuelTypeHydrolox {
		t.Errorf("stage engine chem = %q, want hydrolox", v.stageEngineChem(0))
	}
	if !hasWarningContaining(v.Warnings(), "invalid") {
		t.Errorf("mixed-chemistry stage produced no invalid warning: %v", v.Warnings())
	}
	// Repair: the tank row now cycles hydrolox-only, so one → fixes the mix.
	v.stackCursor = v.headerRowIndex(0) + 2 // tank row
	v.swapRow(+1)
	if countComp(v.stages[0], "h-tank") != 1 || countComp(v.stages[0], "k-tank") != 0 {
		t.Fatalf("tank row did not cycle to the new chemistry: %v", v.stages[0].components)
	}
	if hasWarningContaining(v.Warnings(), "invalid") {
		t.Errorf("stage still invalid after repair: %v", v.Warnings())
	}
}

// TestVABTankRowCompatibleOnly — a tank row only ever offers chemistry-matching
// tanks, so cycling it can never create a mixed stage (ADR 0032 §4).
func TestVABTankRowCompatibleOnly(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(chemVABComps())
	cands := v.swapCandidates(0, spacecraft.ComponentTank)
	// No engine yet → no chemistry leader → both tanks are candidates.
	if len(cands) != 2 {
		t.Errorf("engineless tank candidates = %v, want both", cands)
	}
	v.stages = []vabStage{{components: []string{"k-eng", "k-tank"}}}
	cands = v.swapCandidates(0, spacecraft.ComponentTank)
	if len(cands) != 1 || cands[0] != "k-tank" {
		t.Errorf("kerolox-stage tank candidates = %v, want [k-tank] only", cands)
	}
}

// TestVABSwapNoOps — ←/→ on a stage header does nothing, and ←/→ while the
// palette is focused does nothing (ADR 0032 §3).
func TestVABSwapNoOps(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{components: []string{"eng", "tank"}}}
	before := strings.Join(v.stages[0].components, ",")

	v.focus = focusStack
	v.stackCursor = v.headerRowIndex(0) // header row
	v.swapRow(+1)
	if strings.Join(v.stages[0].components, ",") != before {
		t.Errorf("header ←/→ mutated the stage: %v", v.stages[0].components)
	}

	v.focus = focusPalette
	v.stackCursor = v.headerRowIndex(0) + 1 // an engine row, but palette focused
	v.swapRow(+1)
	if strings.Join(v.stages[0].components, ",") != before {
		t.Errorf("palette-focused ←/→ mutated the stage: %v", v.stages[0].components)
	}
}

// TestVABQuantityDeltaOnPlaceholder — +/- on a placeholder row is a guided
// no-op (nothing to count yet), not a crash (ADR 0032 §5).
func TestVABQuantityDeltaOnPlaceholder(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.newStage() // cursor on the engine placeholder
	v.quantityDelta(+1)
	if len(v.stages[0].components) != 0 {
		t.Errorf("quantityDelta on a placeholder added a component: %v", v.stages[0].components)
	}
	if v.flash == "" {
		t.Error("expected a guidance flash for +/- on a placeholder")
	}
}

// TestVABCursorWalkPlaceholderTransition — the linear cursor stays valid while
// rows shift from two placeholders to real+placeholder to two reals as the
// stage fills (ADR 0032 §5 — the hardest cursor case).
func TestVABCursorWalkPlaceholderTransition(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.newStage()
	// Walk down past the end and back; must clamp, never panic.
	for i := 0; i < 5; i++ {
		v.moveStackCursor(+1)
	}
	if r, ok := v.currentRow(); !ok || r.stageIdx != 0 {
		t.Fatalf("cursor walked off the stage: %+v ok=%v", r, ok)
	}
	// Fill the stage; each pick must leave the cursor on a real, in-range row.
	v.stackCursor = v.headerRowIndex(0) + 1
	v.swapRow(+1) // engine
	if r, _ := v.currentRow(); r.group >= len(v.rowGroups(r.stageIdx)) {
		t.Fatal("cursor out of range after engine pick")
	}
	v.moveStackCursor(+1)
	v.swapRow(+1) // tank
	if r, _ := v.currentRow(); r.group >= len(v.rowGroups(r.stageIdx)) {
		t.Fatal("cursor out of range after tank pick")
	}
}

// TestVABNoPhantomStage — the auto-seeded empty stage is never orphaned into a
// phantom zero-mass stage: adding a catalog part reuses it, and `n` on an
// already-empty stage does not stack another empty (regression for the
// auto-seed fix).
func TestVABNoPhantomStage(t *testing.T) {
	// Entry → add a catalog part reuses the seed → exactly one catalog stage.
	v := NewVAB(Theme{})
	v.Reset(map[string]spacecraft.Component{}) // palette = catalog parts only
	v.addSelected()
	if len(v.stages) != 1 || !v.stages[0].isCatalog() {
		t.Errorf("catalog-add on entry: stages=%d, want one catalog stage", len(v.stages))
	}

	// Entry → `n` reuses the empty seed rather than stacking a second empty.
	v2 := NewVAB(Theme{})
	v2.Reset(testVABComps())
	v2.newStage()
	if len(v2.stages) != 1 {
		t.Errorf("n on an empty seed: stages=%d, want 1 (no phantom)", len(v2.stages))
	}
	// After building the seed, `n` DOES add a fresh stage.
	v2.addComponentToCurrent("eng")
	v2.newStage()
	if len(v2.stages) != 2 {
		t.Errorf("n on a filled stage: stages=%d, want 2", len(v2.stages))
	}
	if !v2.isEmptyComposed(1) {
		t.Error("the new stage should be an empty composed stage")
	}
}

func hasWarningContaining(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
