package screens

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// S6 / S7 — ADR 0030 — the linear-cursor model, canonical fold, and the new
// build operations (remove-under-cursor, quantity ±, reorder, duplicate, the
// column switch and section jump). These drive the model methods directly.

func countComp(st vabStage, id string) int {
	n := 0
	for _, c := range st.components {
		if c == id {
			n++
		}
	}
	return n
}

// TestVABCanonicalFold — components added in any order fold into kind-ordered
// ×N groups: engines before tanks, identical components counted (ADR 0030 §4).
func TestVABCanonicalFold(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("tank") // tank added FIRST
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("eng") // a 2-engine cluster
	groups := v.componentGroups(0)
	if len(groups) != 2 {
		t.Fatalf("groups = %d, want 2 (one engine group, one tank group)", len(groups))
	}
	if groups[0].kind != spacecraft.ComponentEngine {
		t.Errorf("first group kind = %q, want engine (kind-ordered, not insertion-ordered)", groups[0].kind)
	}
	if groups[0].compID != "eng" || groups[0].count != 2 {
		t.Errorf("engine group = %s ×%d, want eng ×2", groups[0].compID, groups[0].count)
	}
	if groups[1].kind != spacecraft.ComponentTank || groups[1].count != 1 {
		t.Errorf("tank group = %s ×%d (%s), want tank ×1", groups[1].compID, groups[1].count, groups[1].kind)
	}
}

// TestVABStackRowsInterleaved — the flattened cursor list is stage headers and
// their groups interleaved, top stage first (ADR 0030 §3).
func TestVABStackRowsInterleaved(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.newStage()
	v.addComponentToCurrent("core")
	rows := v.stackRows()
	// top stage (1: core) header + 1 group, then bottom stage (0) header + 2 groups
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5 (2 headers + 3 groups)", len(rows))
	}
	if !rows[0].isHeader() || rows[0].stageIdx != 1 {
		t.Errorf("row0 = %+v, want header of top stage 1", rows[0])
	}
	if rows[1].isHeader() || rows[1].stageIdx != 1 {
		t.Errorf("row1 = %+v, want a group of stage 1", rows[1])
	}
	if !rows[2].isHeader() || rows[2].stageIdx != 0 {
		t.Errorf("row2 = %+v, want header of bottom stage 0", rows[2])
	}
}

// TestVABRemoveUnderCursorGroup — with the cursor on a component group, [x]
// removes ONE instance of that group, not the whole stage (ADR 0030 §3).
func TestVABRemoveUnderCursorGroup(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("eng") // eng ×2
	v.addComponentToCurrent("tank")
	// rows: [header, engGroup, tankGroup]; put cursor on the engine group.
	v.stackCursor = 1
	v.removeUnderCursor()
	if got := countComp(v.stages[0], "eng"); got != 1 {
		t.Errorf("engine count = %d, want 1 (one instance removed)", got)
	}
	if len(v.stages) != 1 {
		t.Errorf("stage was removed (%d left), want the stage kept", len(v.stages))
	}
}

// TestVABRemoveUnderCursorHeader — with the cursor on a stage header, [x]
// removes the whole stage.
func TestVABRemoveUnderCursorHeader(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.newStage()
	v.addComponentToCurrent("core")
	// Top stage (1) header is row 0.
	v.stackCursor = 0
	v.removeUnderCursor()
	if len(v.stages) != 1 {
		t.Fatalf("stages = %d, want 1 after removing the top stage", len(v.stages))
	}
	if countComp(v.stages[0], "eng") != 1 {
		t.Error("wrong stage removed — expected the engine stage to survive")
	}
}

// TestVABQuantityDelta — +/- on a component group changes its count and the
// stage's Δv (ADR 0030 §5).
func TestVABQuantityDelta(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.stackCursor = v.headerRowIndex(0) + 2 // the tank group (after engine group)
	if r, _ := v.currentRow(); r.isHeader() {
		t.Fatal("cursor landed on a header, expected a component group")
	}
	dv0 := v.Stats().StageDV[0]
	v.quantityDelta(+1) // a second tank
	if countComp(v.stages[0], "tank") != 2 {
		t.Fatalf("tank count = %d, want 2 after +1", countComp(v.stages[0], "tank"))
	}
	if v.Stats().StageDV[0] <= dv0 {
		t.Error("+1 tank did not raise Δv")
	}
	v.quantityDelta(-1)
	if countComp(v.stages[0], "tank") != 1 {
		t.Errorf("tank count = %d, want 1 after -1", countComp(v.stages[0], "tank"))
	}
}

// TestVABReorderStage — moving a stage swaps its position in the stack
// (ADR 0030 §5).
func TestVABReorderStage(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng") // stage 0
	v.newStage()
	v.addComponentToCurrent("core") // stage 1
	v.focusStageInStack(1)
	v.reorderStage(-1) // move the top stage down to index 0
	if countComp(v.stages[0], "core") != 1 {
		t.Errorf("stage 0 = %v, want the core stage after reorder", v.stages[0].components)
	}
	if countComp(v.stages[1], "eng") != 1 {
		t.Errorf("stage 1 = %v, want the engine stage after reorder", v.stages[1].components)
	}
}

// TestVABDuplicateStage — duplicating clones a stage's components into a new
// adjacent stage (ADR 0030 §5).
func TestVABDuplicateStage(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.focusStageInStack(0)
	v.duplicateStage()
	if len(v.stages) != 2 {
		t.Fatalf("stages = %d, want 2 after duplicate", len(v.stages))
	}
	if countComp(v.stages[0], "eng") != 1 || countComp(v.stages[1], "eng") != 1 ||
		countComp(v.stages[0], "tank") != 1 || countComp(v.stages[1], "tank") != 1 {
		t.Errorf("clone mismatch: s0=%v s1=%v", v.stages[0].components, v.stages[1].components)
	}
	// Mutating the clone must not touch the original (deep copy).
	v.removeOneComponent(1, "tank")
	if countComp(v.stages[0], "tank") != 1 {
		t.Error("editing the clone mutated the original stage (shared slice)")
	}
}

// TestVABColumnSwitchKeys — tab is the ONLY column switch now (ADR 0032 §3);
// the default focus is the vehicle column (vehicle-primary, §2) and ←/→ no
// longer switch columns — they edit the focused row.
func TestVABColumnSwitchKeys(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	if v.focus != focusStack {
		t.Errorf("default focus = %v, want focusStack (vehicle-primary)", v.focus)
	}
	v.HandleKey("tab")
	if v.focus != focusPalette {
		t.Errorf("tab focus = %v, want focusPalette", v.focus)
	}
	v.HandleKey("shift+tab")
	if v.focus != focusStack {
		t.Errorf("shift+tab focus = %v, want focusStack", v.focus)
	}
	// ←/→ must not switch columns anymore.
	v.HandleKey("left")
	if v.focus != focusStack {
		t.Errorf("← switched columns, focus = %v, want focusStack unchanged", v.focus)
	}
}

// TestVABKindJump — PgUp/PgDn jumps the palette cursor between kind sections
// (ADR 0030 §7).
func TestVABKindJump(t *testing.T) {
	v := NewVAB(Theme{}) // real component catalog via the package global
	v.Reset(spacecraft.Components)
	if len(v.palette) == 0 {
		t.Skip("no components in catalog")
	}
	startKind, _ := v.paletteItemLabel(v.palette[v.paletteIdx])
	v.jumpKind(+1)
	nextKind, _ := v.paletteItemLabel(v.palette[v.paletteIdx])
	if startKind == nextKind {
		t.Errorf("jumpKind stayed in the same section %q", startKind)
	}
}

// --- ADR 0056 S1 ---

func selectPalettePart(t *testing.T, v *VAB, id string) {
	t.Helper()
	for i, it := range v.palette {
		if !it.isComponent && it.id == id {
			v.paletteIdx = i
			return
		}
	}
	t.Fatalf("catalog part %q not in palette", id)
}

func catalogIDs(v *VAB) []string {
	var out []string
	for _, s := range v.stages {
		out = append(out, s.catalogPartID)
	}
	return out
}

// `a` on a part with the cursor below the top inserts directly above the
// cursor's stage and the cursor lands on the new stage.
func TestVABAddPartInsertsAboveCursorAndCursorFollows(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{catalogPartID: "s-ic"}, {catalogPartID: "s-ii"}, {catalogPartID: "s-ivb"}}
	v.focusStageInStack(0)
	selectPalettePart(t, v, "csm")
	v.addSelected()
	if got := strings.Join(catalogIDs(v), ","); got != "s-ic,csm,s-ii,s-ivb" {
		t.Fatalf("stack = %s, want the part inserted at index 1", got)
	}
	if v.stageIdx != 1 {
		t.Errorf("stageIdx = %d, want 1 (cursor on the added stage)", v.stageIdx)
	}
	if want := v.headerRowIndex(1); v.stackCursor != want {
		t.Errorf("stackCursor = %d, want %d (header of the added stage)", v.stackCursor, want)
	}
	// a a: stacks upward from where the cursor was looking.
	v.addSelected()
	if got := strings.Join(catalogIDs(v), ","); got != "s-ic,csm,csm,s-ii,s-ivb" || v.stageIdx != 2 {
		t.Errorf("second add: stack = %s idx %d, want csm at 2 with cursor on it", got, v.stageIdx)
	}
}

// With the cursor on the top stage `a` still appends on top.
func TestVABAddPartOnTopStageAppends(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{catalogPartID: "s-ic"}, {catalogPartID: "s-ii"}}
	v.focusStageInStack(1)
	selectPalettePart(t, v, "csm")
	v.addSelected()
	if got := strings.Join(catalogIDs(v), ","); got != "s-ic,s-ii,csm" || v.stageIdx != 2 {
		t.Errorf("stack = %s idx %d, want csm appended with cursor on it", got, v.stageIdx)
	}
}

// An empty vehicle's auto-seed is replaced in place, not stacked under.
func TestVABAddPartOnEmptySeedReplacesIt(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{}}
	v.focusStageInStack(0)
	selectPalettePart(t, v, "csm")
	v.addSelected()
	if got := strings.Join(catalogIDs(v), ","); got != "csm" || v.stageIdx != 0 {
		t.Errorf("stack = %q idx %d, want the seed replaced by csm", got, v.stageIdx)
	}
}

// `a` on a component into an atomic stage starts a fresh composed stage above
// the cursor, not on top of the stack.
func TestVABAddComponentToAtomicStageInsertsAboveCursor(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{catalogPartID: "s-ic"}, {catalogPartID: "s-ii"}}
	v.focusStageInStack(0)
	v.addComponentToCurrent("tank")
	if len(v.stages) != 3 || v.stages[1].isCatalog() || countComp(v.stages[1], "tank") != 1 {
		t.Fatalf("stages = %+v, want a composed [tank] stage at index 1", v.stages)
	}
	if v.stages[2].catalogPartID != "s-ii" || v.stageIdx != 1 {
		t.Errorf("s-ii at 2 (got %q) and cursor on 1 (got %d)", v.stages[2].catalogPartID, v.stageIdx)
	}
}

// `y` copies the stage's marks: a copy of a seamed, fused stage is seamed
// and fused (ADR 0056 decision 10.1).
func TestVABDuplicateStageKeepsMarks(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{
		{catalogPartID: "s-ic"},
		{catalogPartID: "relay-comsat", dockSeamBelow: true, decoupleFused: true},
	}
	v.focusStageInStack(1)
	v.duplicateStage()
	if len(v.stages) != 3 {
		t.Fatalf("stages = %d, want 3", len(v.stages))
	}
	if c := v.stages[2]; !c.dockSeamBelow || !c.decoupleFused {
		t.Errorf("copy flags seam=%v fused=%v, want both true", c.dockSeamBelow, c.decoupleFused)
	}
	if p := v.stages[1]; !p.dockSeamBelow || !p.decoupleFused {
		t.Errorf("original lost its marks: %+v", p)
	}
}

func seamedComsatStack(v *VAB) {
	v.stages = []vabStage{
		{components: []string{"eng", "tank"}},
		{components: []string{"eng2", "tank2"}, dockSeamBelow: true},
		{components: []string{"eng2", "tank2"}, dockSeamBelow: true},
		{components: []string{"eng2", "tank2"}, dockSeamBelow: true},
	}
}

// Per-stage Δv stops at a payload seam: three identical payloads read equal
// Δv (each its own, as after Y), the carrier row carries them as mass, and
// Σ is the carrier chain alone (ADR 0056 conflict 4, confirmed).
func TestVABStatsStopAtSeam(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	seamedComsatStack(v)
	st := v.Stats()
	alone := v.stageDV(v.resolveStage(v.stages[1]))
	for i := 1; i <= 3; i++ {
		if math.Abs(st.StageDV[i]-alone) > 1e-6 {
			t.Errorf("payload stage %d Δv = %.1f, want its own %.1f", i, st.StageDV[i], alone)
		}
	}
	// carrier unchanged from the seamless stack
	for i := range v.stages {
		v.stages[i].dockSeamBelow = false
	}
	flat := v.Stats()
	if math.Abs(st.StageDV[0]-flat.StageDV[0]) > 1e-6 {
		t.Errorf("carrier Δv %.1f, want unchanged %.1f", st.StageDV[0], flat.StageDV[0])
	}
	if math.Abs(st.TotalDV-st.StageDV[0]) > 1e-6 {
		t.Errorf("Σ = %.1f, want the carrier chain %.1f", st.TotalDV, st.StageDV[0])
	}
	if flat.StageDV[1] >= alone-1 {
		t.Fatalf("fixture: seamless stage 1 (%.1f) should read below its own %.1f", flat.StageDV[1], alone)
	}
}

// The seam row and the F1 / controls.md `d` rows name Y and U.
func TestVABSeamRowAndDRowNameYAndU(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	seamedComsatStack(v)
	col := strings.Join(v.renderVehicleColumn(82, 0), "\n")
	if !strings.Contains(col, "── payload seam ([Y] deploys, [U] undocks) ──") {
		t.Errorf("seam row missing Y/U label:\n%s", col)
	}
	if strings.Contains(col, "Undock to release") {
		t.Error("old seam label still rendered")
	}
	const want = "payload from here up: this stage and all above become a payload, released with [Y] (deploy one) or [U] (undock all)"
	var got string
	for _, s := range helpSections {
		if s.header != "VEHICLE ASSEMBLY (VAB)" {
			continue
		}
		for _, r := range s.rows {
			if r[0] == "d" {
				got = r[1]
			}
		}
	}
	if got != want {
		t.Errorf("F1 d row = %q, want %q", got, want)
	}
	doc, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(doc), "released with `Y` (deploy, one at a time) or `U` (undock all)") {
		t.Error("docs/controls.md d row does not name Y and U")
	}
}

// The F1 `a` and `y` rows say above the cursor / marks included.
func TestVABAddAndCopyRowsDescribeNewBehaviour(t *testing.T) {
	rows := map[string]string{}
	for _, s := range helpSections {
		if s.header == "VEHICLE ASSEMBLY (VAB)" {
			for _, r := range s.rows {
				rows[r[0]] = r[1]
			}
		}
	}
	if !strings.Contains(rows["a"], "above the cursor") {
		t.Errorf("a row = %q", rows["a"])
	}
	if !strings.Contains(rows["y"], "marks included") {
		t.Errorf("y row = %q", rows["y"])
	}
}

// The stats strip says Σ is the carrier chain when the stack has a payload
// seam, and says nothing extra when it has none.
func TestVABStatsStripNamesCarrierOnlyWithSeams(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	seamedComsatStack(v)
	if got := v.targetReadout(v.Stats()); !strings.Contains(got, "m/s (carrier)") {
		t.Errorf("seamed strip = %q, want (carrier)", got)
	}
	for i := range v.stages {
		v.stages[i].dockSeamBelow = false
	}
	if got := v.targetReadout(v.Stats()); strings.Contains(got, "carrier") {
		t.Errorf("seamless strip = %q, want no carrier note", got)
	}
}
