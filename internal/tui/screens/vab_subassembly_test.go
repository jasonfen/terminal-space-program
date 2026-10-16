package screens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0056 S4: subassemblies. A block lands whole with its marks, S saves from
// the cursor up through a card, a released payload is named after the block,
// and `o` lists the player's blocks with delete.

// subVAB is a VAB on the test component set with an empty store.
func subVAB(t *testing.T) *VAB {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	return v
}

func pickSub(t *testing.T, v *VAB, id string) {
	t.Helper()
	for i, it := range v.palette {
		if it.subassemblyID == id {
			v.paletteIdx = i
			return
		}
	}
	t.Fatalf("subassembly %q is not in the palette", id)
}

// stackOf replaces the vehicle with atomic catalog stages and the cursor on
// stage at.
func stackOf(v *VAB, at int, parts ...string) {
	v.stages = nil
	for _, p := range parts {
		v.stages = append(v.stages, vabStage{catalogPartID: p})
	}
	v.focus = focusStack
	v.focusStageInStack(at)
}

func typeText(v *VAB, s string) {
	for _, r := range s {
		v.HandleKey(string(r))
	}
}

func seams(v *VAB) []bool {
	out := make([]bool, len(v.stages))
	for i, s := range v.stages {
		out[i] = s.dockSeamBelow
	}
	return out
}

func eqBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestVABPaletteHasSubassemblySection(t *testing.T) {
	v := subVAB(t)
	var rows []string
	for _, it := range v.palette {
		if k, label := v.paletteItemLabel(it); k == "subassembly" {
			rows = append(rows, label)
		}
	}
	want := "Comsat [payload]|LM [payload · 2 stages]|CSM+LM [stage · 4 stages]"
	if strings.Join(rows, "|") != want {
		t.Errorf("subassembly section = %q, want %q", strings.Join(rows, "|"), want)
	}
	// The section header is drawn.
	if got := strings.Join(v.renderPalette(46, 400), "\n"); !strings.Contains(got, "· subassembly ·") {
		t.Errorf("palette draws no `· subassembly ·` header:\n%s", got)
	}
}

func TestVABFindFiltersSubassembliesByName(t *testing.T) {
	v := subVAB(t)
	typeFind(v, "csm")
	if len(v.palette) == 0 {
		t.Fatal("find csm matched nothing")
	}
	found := false
	for _, it := range v.palette {
		found = found || it.subassemblyID == "csm-lm"
	}
	if !found {
		t.Errorf("find csm does not list CSM+LM: %v", v.palette)
	}
}

// `a` on Comsat with the cursor on S2 inserts one stage above S2 carrying a
// payload seam and its subassemblyID; three presses give three seams.
func TestVABAddComsatLandsSeamed(t *testing.T) {
	v := subVAB(t)
	stackOf(v, 1, "s-ic", "s-ii", "s-ivb")
	pickSub(t, v, "comsat")
	v.addSelected()
	if len(v.stages) != 4 {
		t.Fatalf("stages = %d, want 4", len(v.stages))
	}
	st := v.stages[2]
	if st.catalogPartID != "relay-comsat" || !st.dockSeamBelow || st.subassemblyID != "comsat" {
		t.Errorf("inserted stage = %+v, want relay-comsat seamed, from comsat", st)
	}
	if v.stageIdx != 2 {
		t.Errorf("cursor on S%d, want S3 (the block)", v.stageIdx+1)
	}
	v.addSelected()
	v.addSelected()
	if got := seams(v); !eqBools(got, []bool{false, false, true, true, true, false}) {
		t.Errorf("seams after three adds = %v, want one under each comsat", got)
	}
}

// A payload added to an empty vehicle is simply a vehicle (no seam under stage 0).
func TestVABAddPayloadToEmptyVehicleHasNoSeam(t *testing.T) {
	v := subVAB(t)
	pickSub(t, v, "comsat")
	v.addSelected()
	if len(v.stages) != 1 || v.stages[0].dockSeamBelow || v.stages[0].catalogPartID != "relay-comsat" {
		t.Errorf("stages = %+v, want the comsat alone, unseamed, replacing the seed", v.stages)
	}
}

func TestVABAddBlocksKeepTheirMarks(t *testing.T) {
	v := subVAB(t)
	stackOf(v, 0, "s-ic")
	pickSub(t, v, "csm-lm")
	v.addSelected()
	if len(v.stages) != 5 {
		t.Fatalf("CSM+LM: stages = %d, want 1 + 4", len(v.stages))
	}
	// Block is S2..S5: SM, CM, Descent, Ascent. Stage role: no seam under its
	// bottom stage; its own nose payload plan [2] puts the seam under Descent.
	if got := seams(v); !eqBools(got, []bool{false, false, false, true, false}) {
		t.Errorf("CSM+LM seams = %v, want only under the LM's descent stage", got)
	}
	for i, s := range v.stages[1:] {
		if s.subassemblyID != "csm-lm" {
			t.Errorf("block stage %d subassemblyID = %q", i, s.subassemblyID)
		}
	}

	v = subVAB(t)
	stackOf(v, 0, "s-ic")
	pickSub(t, v, "lm")
	v.addSelected()
	if got := seams(v); !eqBools(got, []bool{false, true, false}) {
		t.Errorf("LM seams = %v, want one under the LM's bottom stage", got)
	}
}

// S on S3 of a five-stage stack: three parts, IDs and names count from 1, no
// seam under the bottom stage, the role preset from that seam.
func TestVABSaveFromHereUpFragment(t *testing.T) {
	v := subVAB(t)
	v.stages = nil
	for _, c := range [][]string{{"eng", "tank"}, {"eng2", "tank2"}, {"eng", "tank"}, {"eng2", "tank2"}, {"core"}} {
		v.stages = append(v.stages, vabStage{components: c})
	}
	v.stages[2].dockSeamBelow = true // S3 marked `d`
	v.stages[3].decoupleFused = true
	v.focusStageInStack(2)
	v.HandleKey("S")
	if v.mode != vabModeSavingSub || v.subRole != "payload" || v.subFrom != 2 {
		t.Fatalf("S: mode=%v role=%q from=%d, want the card open on S3 with payload", v.mode, v.subRole, v.subFrom)
	}
	frag := v.toFragment(2, len(v.stages), "Comsat x3")
	if len(frag.Parts) != 3 || len(frag.Loadout.Parts) != 3 {
		t.Fatalf("fragment has %d parts / %d refs, want 3", len(frag.Parts), len(frag.Loadout.Parts))
	}
	for i, p := range frag.Parts {
		wantID, wantName := "_comsat-x3_s"+string(rune('1'+i)), "Comsat x3 S"+string(rune('1'+i))
		if p.ID != wantID || p.Name != wantName {
			t.Errorf("part %d = %q %q, want %q %q", i, p.ID, p.Name, wantID, wantName)
		}
	}
	if len(frag.Loadout.NosePayloadPlan) != 0 {
		t.Errorf("nose plan = %v, want none: the bottom stage's seam is the role, not a mark", frag.Loadout.NosePayloadPlan)
	}
	if len(frag.Loadout.DecouplePlan) == 0 {
		t.Errorf("the fuse inside the block was not saved")
	}

	// Without the `d`, the preset is stage.
	v.stages[2].dockSeamBelow = false
	v.HandleKey("esc")
	v.HandleKey("S")
	if v.subRole != "stage" {
		t.Errorf("role preset = %q, want stage", v.subRole)
	}
}

// Save through the card, then add: the round trip reproduces stages, marks and
// role. Also the ←/→ role flip, and that the new block is in the palette.
func TestVABSaveSubassemblyRoundTrip(t *testing.T) {
	v := subVAB(t)
	v.stages = []vabStage{
		{catalogPartID: "s-ic"},
		{components: []string{"eng", "tank"}},
		{components: []string{"eng2", "tank2"}, decoupleFused: true},
		{components: []string{"core"}, dockSeamBelow: true},
	}
	v.focusStageInStack(1)
	v.HandleKey("S")
	typeText(v, "Pair")
	v.HandleKey("left") // stage -> payload
	if v.subRole != "payload" {
		t.Fatalf("← did not flip the role: %q", v.subRole)
	}
	v.HandleKey("enter")
	if v.mode != vabModeBuild || !strings.Contains(v.flash, "saved subassembly") {
		t.Fatalf("mode=%v flash=%q, want saved and back to build", v.mode, v.flash)
	}
	subs, _ := spacecraft.ListSubassemblies()
	var saved *spacecraft.Subassembly
	for i := range subs {
		if subs[i].ID() == "pair" {
			saved = &subs[i]
		}
	}
	if saved == nil || saved.Role() != "payload" || len(saved.Loadout.Parts) != 3 {
		t.Fatalf("store has %+v, want a 3-stage payload 'pair'", saved)
	}
	pickSub(t, v, "pair")
	if !strings.Contains(v.palette[v.paletteIdx].subassemblyID, "pair") {
		t.Fatal("not selected")
	}

	// Add it above S1 of a fresh vehicle.
	w := NewVAB(Theme{})
	w.Reset(testVABComps())
	stackOf(w, 0, "s-ic")
	pickSub(t, w, "pair")
	w.addSelected()
	if len(w.stages) != 4 {
		t.Fatalf("stages = %d, want 1 + 3", len(w.stages))
	}
	blk := w.stages[1:]
	if !eqBools([]bool{blk[0].dockSeamBelow, blk[1].dockSeamBelow, blk[2].dockSeamBelow}, []bool{true, false, true}) {
		t.Errorf("seams after the round trip: %v (want role seam under the block, the inner seam kept)", seams(w))
	}
	if !blk[2].decoupleFused && !blk[1].decoupleFused {
		t.Errorf("the fuse did not survive: %+v", blk)
	}
	if strings.Join(blk[0].components, ",") != "eng,tank" || strings.Join(blk[1].components, ",") != "eng2,tank2" {
		t.Errorf("components changed: %+v", blk)
	}
}

// Saving over an existing name asks first, on a card that asks twice; the card
// says when it replaces a built-in.
func TestVABSaveOverNameAsksTwice(t *testing.T) {
	v := subVAB(t)
	stackOf(v, 0, "relay-comsat")
	v.HandleKey("S")
	typeText(v, "Comsat")
	v.HandleKey("enter")
	c := v.ActiveCard()
	if c == nil {
		t.Fatal("saving over the built-in Comsat opened no card")
	}
	if txt := c.Render(); !strings.Contains(txt, `replace "Comsat"?`) || !strings.Contains(txt, `replaces the built-in "Comsat"`) {
		t.Errorf("card:\n%s", txt)
	}
	if subs, _ := spacecraft.ListSubassemblies(); subs[0].Shipped() == false {
		t.Fatal("replaced before the card was answered")
	}
	v.HandleKey("y")
	if c := v.ActiveCard(); c == nil || !c.Confirming() || !strings.Contains(c.Render(), "yes, replace") {
		t.Fatal("first y did not move to the second step")
	}
	if subs, _ := spacecraft.ListSubassemblies(); subs[0].Shipped() == false {
		t.Fatal("replaced on the first press")
	}
	v.HandleKey("y")
	if v.ActiveCard() != nil || v.mode != vabModeBuild {
		t.Errorf("card still up after the second y (mode %v)", v.mode)
	}
	subs, _ := spacecraft.ListSubassemblies()
	if subs[0].ID() != "comsat" || subs[0].Shipped() {
		t.Errorf("user Comsat did not replace the shipped one: %+v", subs[0].Loadout)
	}

	// esc on the first step keeps the old block.
	stackOf(v, 0, "relay-comsat", "relay-comsat")
	v.HandleKey("S")
	typeText(v, "Comsat")
	v.HandleKey("enter")
	v.HandleKey("esc")
	if subs, _ := spacecraft.ListSubassemblies(); len(subs[0].Loadout.Parts) != 1 {
		t.Errorf("esc replaced the block: %d parts", len(subs[0].Loadout.Parts))
	}
}

func TestVABSaveRefusals(t *testing.T) {
	v := subVAB(t)
	v.stages = nil
	v.HandleKey("S")
	if v.mode != vabModeBuild || v.flash != "nothing to save" {
		t.Errorf("empty vehicle: mode=%v flash=%q", v.mode, v.flash)
	}
	stackOf(v, 0, "s-ic")
	v.HandleKey("S")
	v.HandleKey("enter")
	if v.mode != vabModeSavingSub || v.flash != "name can't be empty" {
		t.Errorf("empty name: mode=%v flash=%q", v.mode, v.flash)
	}
	if !v.CapturingText() {
		t.Error("the S card types, so CapturingText must be true")
	}
}

// o lists the player's blocks under SUBASSEMBLIES (not the shipped three), x
// opens the delete card (asks twice), enter opens one as a vehicle.
func TestVABOpenListsSubassembliesAndDeletes(t *testing.T) {
	v := subVAB(t)
	stackOf(v, 0, "relay-comsat", "relay-comsat")
	v.HandleKey("S")
	typeText(v, "Comsat x2")
	v.HandleKey("right")
	v.HandleKey("enter")
	v.HandleKey("o")
	out := v.renderLoad(100, 30)
	for _, w := range []string{"YOUR DESIGNS", "SUBASSEMBLIES", "Comsat x2", "stage · 2 stages"} {
		if !strings.Contains(out, w) && !strings.Contains(out, strings.ReplaceAll(w, "stage · ", "payload · ")) {
			t.Errorf("o list lacks %q:\n%s", w, out)
		}
	}
	if strings.Contains(out, "CSM+LM") {
		t.Errorf("o lists a shipped block:\n%s", out)
	}

	v.HandleKey("x")
	c := v.ActiveCard()
	if c == nil || !strings.Contains(c.Render(), "Delete subassembly 'Comsat x2'?") {
		t.Fatalf("x opened no subassembly delete card")
	}
	v.HandleKey("y")
	if subs, _ := spacecraft.ListSubassemblies(); len(subs) != 4 {
		t.Fatalf("deleted on the first y: %d blocks", len(subs))
	}
	v.HandleKey("y")
	subs, _ := spacecraft.ListSubassemblies()
	if len(subs) != 3 {
		t.Errorf("blocks after the second y = %d, want the 3 shipped", len(subs))
	}
	for _, it := range v.palette {
		if it.subassemblyID == "comsat-x2" {
			t.Error("deleted block is still in the palette")
		}
	}
	if _, err := os.Stat(filepath.Join(spacecraft.SubassembliesDir(), "comsat-x2.json")); !os.IsNotExist(err) {
		t.Errorf("file still on disk: %v", err)
	}
}

func TestVABOpenSubassemblyLoadsItUnsaved(t *testing.T) {
	v := subVAB(t)
	stackOf(v, 0, "relay-comsat", "relay-comsat", "relay-comsat")
	v.HandleKey("S")
	typeText(v, "Trio")
	v.HandleKey("left")
	v.HandleKey("enter")
	v.Reset(testVABComps())
	v.HandleKey("o")
	v.HandleKey("enter") // no designs: the cursor is on the block
	if len(v.stages) != 3 || v.name != "" {
		t.Fatalf("stages=%d name=%q, want the 3-stage block as an unsaved vehicle", len(v.stages), v.name)
	}
	v.focusStageInStack(0)
	v.HandleKey("S")
	if v.subName != "Trio" || v.subRole != "payload" {
		t.Errorf("S on the block's bottom offers %q / %q, want Trio / payload", v.subName, v.subRole)
	}
}

func TestVABInspectorDescribesSubassembly(t *testing.T) {
	v := subVAB(t)
	v.focus = focusPalette
	pickSub(t, v, "comsat")
	got := strings.Join(v.renderInspector(50), "\n")
	for _, w := range []string{"Comsat", "payload · 1 stage", "600 kg dry", "RCS only"} {
		if !strings.Contains(got, w) {
			t.Errorf("inspector lacks %q:\n%s", w, got)
		}
	}
	stackOf(v, 0, "s-ic")
	pickSub(t, v, "comsat")
	v.addSelected()
	v.focus = focusStack
	v.focusStageInStack(1)
	if got := strings.Join(v.renderInspector(50), "\n"); !strings.Contains(got, `from "Comsat"`) {
		t.Errorf("stage inspector does not say where the stage came from:\n%s", got)
	}
}

// The player path: build in the VAB, save the design with s, spawn it from the
// store, and deploy. A Comsat block releases as Comsat-1; a hand-marked
// Relay Comsat part as Relay Comsat-1; a composed top stage as <design> S<n>-1;
// a payload group inside a stage block keeps its own name.
func TestVABDeployedPayloadIsNamedAfterTheBlock(t *testing.T) {
	deploy := func(t *testing.T, build func(v *VAB)) []string {
		t.Helper()
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		v := NewVAB(Theme{})
		v.Reset(spacecraft.Components)
		v.stages = nil
		v.stages = append(v.stages, vabStage{components: []string{"probe-engine", "probe-tank-1k"}})
		v.focus = focusStack
		v.focusStageInStack(0)
		build(v)
		v.HandleKey("s")
		typeText(v, "Carrier")
		v.HandleKey("enter")
		if !strings.Contains(v.flash, "saved design") {
			t.Fatalf("design not saved: %q", v.flash)
		}
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatal(err)
		}
		c, err := w.SpawnCraft(sim.SpawnSpec{DesignID: "carrier", ParentBodyID: "earth", AltitudeM: 400e3})
		if err != nil {
			t.Fatal(err)
		}
		idx := w.ActiveCraftIdx
		var names []string
		for w.Deploy(idx) {
			names = append(names, w.Crafts[len(w.Crafts)-1].Name)
		}
		_ = c
		return names
	}

	t.Run("three comsat blocks", func(t *testing.T) {
		names := deploy(t, func(v *VAB) {
			pickSub(t, v, "comsat")
			v.addSelected()
			v.addSelected()
			v.addSelected()
		})
		if strings.Join(names, ",") != "Comsat-1,Comsat-2,Comsat-3" {
			t.Errorf("deployed %v, want Comsat-1,Comsat-2,Comsat-3", names)
		}
	})
	t.Run("hand-marked part", func(t *testing.T) {
		names := deploy(t, func(v *VAB) {
			for i, it := range v.palette {
				if it.id == "relay-comsat" {
					v.paletteIdx = i
				}
			}
			v.addSelected()
			v.toggleDockSeam()
		})
		if strings.Join(names, ",") != "Relay Comsat-1" {
			t.Errorf("deployed %v, want Relay Comsat-1", names)
		}
	})
	t.Run("hand-built stage", func(t *testing.T) {
		names := deploy(t, func(v *VAB) {
			v.newStage()
			v.addComponentToCurrent("probe-core")
			v.toggleDockSeam()
		})
		if len(names) != 1 || !strings.HasPrefix(names[0], "Carrier S2-") {
			t.Errorf("deployed %v, want Carrier S2-1", names)
		}
	})
	t.Run("payload inside a stage block", func(t *testing.T) {
		names := deploy(t, func(v *VAB) {
			pickSub(t, v, "csm-lm")
			v.addSelected()
		})
		if strings.Join(names, ",") != "LM-1" {
			t.Errorf("deployed %v, want LM-1: a group inside CSM+LM is not CSM+LM", names)
		}
	})
}

// F1 and docs/controls.md carry the S row and the subassembly words of the
// o / x / a rows (ADR 0056 "F1 and docs/controls.md").
func TestVABSubassemblyRowsInF1AndDocs(t *testing.T) {
	rows := map[string]string{}
	for _, sec := range helpSections {
		if sec.header == "VEHICLE ASSEMBLY (VAB)" {
			for _, r := range sec.rows {
				rows[r[0]] = r[1]
			}
		}
	}
	for tok, want := range map[string]string{"S": "save from here up", "s / o": "subassembly", "a": "subassembly", "x (design list)": "subassembly", "enter (S card)": "payload or stage"} {
		if !strings.Contains(rows[tok], want) {
			t.Errorf("F1 row %q = %q, want it to say %q", tok, rows[tok], want)
		}
	}
	raw, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `S` | Save from here up", "`Delete subassembly 'name'?`", "`· subassembly ·` section"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}
