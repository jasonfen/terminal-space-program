package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// moveLoadTo puts the o-list cursor on the built-in with the given loadout ID.
func moveLoadTo(t *testing.T, v *VAB, id string) {
	t.Helper()
	for i, d := range v.builtIns {
		if d.ID() == id {
			v.loadIdx = len(v.designs) + len(v.userSubs) + i
			return
		}
	}
	t.Fatalf("built-in %q is not in the o list", id)
}

// o lists BUILT IN with every catalog loadout, in catalog order.
func TestVABOpenListsEveryBuiltIn(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	if len(v.builtIns) != len(spacecraft.LoadoutOrder) {
		t.Fatalf("built-ins = %d, want every LoadoutOrder entry (%d)", len(v.builtIns), len(spacecraft.LoadoutOrder))
	}
	for i, id := range spacecraft.LoadoutOrder {
		if v.builtIns[i].ID() != id {
			t.Fatalf("built-in %d = %q, want %q (catalog order)", i, v.builtIns[i].ID(), id)
		}
	}
	// Every one is reachable: with the cursor on it, the window draws it.
	for i, d := range v.builtIns {
		v.loadIdx = len(v.designs) + len(v.userSubs) + i
		out := v.renderLoad(140, 40)
		if !strings.Contains(out, "▸ "+d.Name()) {
			t.Errorf("cursor on %q is not drawn in the window:\n%s", d.Name(), out)
		}
	}
}

// All three headings show even when the first two groups are empty.
func TestVABOpenShowsAllThreeHeadings(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	out := v.renderLoad(140, 40)
	for _, w := range []string{"YOUR DESIGNS", "SUBASSEMBLIES", "BUILT IN"} {
		if !strings.Contains(out, w) {
			t.Errorf("o list lacks %q:\n%s", w, out)
		}
	}
}

// Every built-in opens unsaved: its stages are the loadout's, its name is
// empty, and its payload seams are the nose-payload plan's.
func TestVABEveryBuiltInOpensUnsaved(t *testing.T) {
	v := subVAB(t)
	for _, id := range spacecraft.LoadoutOrder {
		v.Reset(testVABComps())
		v.name = "stale"
		v.HandleKey("o")
		moveLoadTo(t, v, id)
		v.HandleKey("enter")
		l := spacecraft.Loadouts[id]
		if v.mode != vabModeBuild {
			t.Errorf("%s: mode = %v after enter", id, v.mode)
		}
		if len(v.stages) != len(l.Stages) {
			t.Errorf("%s: %d stages, want %d", id, len(v.stages), len(l.Stages))
		}
		if v.name != "" {
			t.Errorf("%s: design name = %q, want empty ((unsaved))", id, v.name)
		}
		if v.loadedSub != nil {
			t.Errorf("%s: opened as a loaded subassembly", id)
		}
		got := 0
		for _, s := range v.stages {
			if s.dockSeamBelow {
				got++
			}
		}
		if got != len(l.NosePayloadPlan) {
			t.Errorf("%s: %d payload seams, want %d", id, got, len(l.NosePayloadPlan))
		}
	}
}

func TestVABComsatCarrierOpensWithThreeSeams(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	moveLoadTo(t, v, "Comsat-Carrier-3")
	v.HandleKey("enter")
	if len(v.stages) != 4 {
		t.Fatalf("stages = %d, want 4", len(v.stages))
	}
	if got := seams(v); !(!got[0] && got[1] && got[2] && got[3]) {
		t.Errorf("seams = %v, want three above the carrier", got)
	}
	if v.name != "" {
		t.Errorf("name = %q, want empty", v.name)
	}
	if out := v.Render(140, 40); !strings.Contains(out, "(unsaved)") {
		t.Errorf("design line does not read (unsaved):\n%s", out)
	}
}

// s on an opened built-in asks for a name (nothing to overwrite) and saves a
// design under the new name; the catalog loadout is untouched.
func TestVABBuiltInSavesUnderNewName(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	moveLoadTo(t, v, "Comsat-Carrier-3")
	v.HandleKey("enter")
	v.HandleKey("s")
	if v.mode != vabModeNaming || v.name != "" {
		t.Fatalf("s: mode=%v name=%q, want the naming card with an empty name", v.mode, v.name)
	}
	typeText(v, "My Carrier")
	v.HandleKey("enter")
	designs, _ := spacecraft.ListDesigns()
	found := false
	for _, d := range designs {
		if d.Name() == "My Carrier" {
			found = true
			if len(d.Loadout.Parts) != 4 || len(d.Loadout.NosePayloadPlan) != 3 {
				t.Errorf("saved design has %d parts, plan %v", len(d.Loadout.Parts), d.Loadout.NosePayloadPlan)
			}
		}
	}
	if !found {
		t.Fatalf("no design saved as My Carrier: %v", designs)
	}
	if spacecraft.Loadouts["Comsat-Carrier-3"].Name != "Comsat Carrier x3" {
		t.Error("the catalog loadout changed")
	}
}

// x on a built-in is refused: a flash, no card, nothing deleted.
func TestVABXRefusedOnBuiltIn(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	moveLoadTo(t, v, "Comsat-Carrier-3")
	n := len(v.builtIns)
	v.HandleKey("x")
	if v.ActiveCard() != nil || v.confirmDelete {
		t.Fatal("x opened a delete card on a built-in")
	}
	if v.flash != "built-in vessels can't be deleted" {
		t.Errorf("flash = %q", v.flash)
	}
	if !strings.Contains(v.renderLoad(140, 40), "built-in vessels can't be deleted") {
		t.Error("the refusal is not on screen")
	}
	if len(v.builtIns) != n || v.mode != vabModeLoad {
		t.Errorf("built-ins=%d mode=%v after the refusal", len(v.builtIns), v.mode)
	}
}

// x on a design still opens the card (the refusal is only for built-ins).
func TestVABXStillDeletesDesignAfterBuiltIns(t *testing.T) {
	v := subVAB(t)
	stackOf(v, 0, "relay-comsat")
	v.name = "Mine"
	v.HandleKey("s")
	v.HandleKey("enter")
	v.HandleKey("o")
	v.loadIdx = 0
	v.HandleKey("x")
	if c := v.ActiveCard(); c == nil || !strings.Contains(c.Render(), "Delete design 'Mine'?") {
		t.Fatal("x on a design opened no delete card")
	}
}

// The list windows: at 140x40 the render fits the box, keeps the cursor
// visible at both ends, and never overruns the card.
func TestVABOpenWindowsAroundCursor(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	total := len(v.builtIns)
	if total < 20 {
		t.Fatalf("only %d built-ins; the window test needs a long list", total)
	}
	for _, idx := range []int{0, total / 2, total - 1} {
		v.loadIdx = len(v.designs) + len(v.userSubs) + idx
		out := v.renderLoad(140, 40)
		if n := strings.Count(out, "\n") + 1; n > 40 {
			t.Errorf("cursor %d: render is %d rows, box is 40", idx, n)
		}
		name := v.builtIns[idx].Name()
		if !strings.Contains(out, "▸ "+name) {
			t.Errorf("cursor on %q is outside the window:\n%s", name, out)
		}
		if !strings.Contains(out, "more") {
			t.Errorf("cursor %d: a clipped list shows no 'more' cue", idx)
		}
	}
	last := v.builtIns[total-1].Name()
	v.loadIdx = len(v.designs) + len(v.userSubs)
	if strings.Contains(v.renderLoad(140, 40), last) {
		t.Error("the last built-in is drawn with the cursor at the top: no window")
	}
}

// up from the first row wraps to the last built-in.
func TestVABOpenCursorWalksAllGroups(t *testing.T) {
	v := subVAB(t)
	v.HandleKey("o")
	n := v.loadCount()
	if n != len(v.builtIns) {
		t.Fatalf("count = %d, want %d with no designs or blocks", n, len(v.builtIns))
	}
	v.HandleKey("up")
	if v.loadIdx != n-1 {
		t.Errorf("up from the top = %d, want %d", v.loadIdx, n-1)
	}
}
