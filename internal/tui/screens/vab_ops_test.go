package screens

import (
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
