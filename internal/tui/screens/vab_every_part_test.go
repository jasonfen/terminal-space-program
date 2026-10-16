package screens

import (
	"os"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0056 S2: every catalog part is in the palette (minus the spawn form's
// two multi-stage picks), grouped by tier; the list fills its box; `/` finds.

var s2Tiers = []string{"booster", "sustainer", "transfer", "payload", "tug"}

func paletteKinds(v *VAB) []string {
	var out []string
	last := "\x00"
	for _, it := range v.palette {
		if k, _ := v.paletteItemLabel(it); k != last {
			out = append(out, k)
			last = k
		}
	}
	return out
}

func paletteHas(v *VAB, id string) bool {
	for _, it := range v.palette {
		if !it.isComponent && it.id == id {
			return true
		}
	}
	return false
}

func TestVABPaletteHoldsEveryCatalogPart(t *testing.T) {
	v := realVAB(t)
	if len(spacecraft.StageCatalog) < 50 {
		t.Fatalf("catalog has %d parts, the real one did not load", len(spacecraft.StageCatalog))
	}
	for id, m := range spacecraft.StageCatalog {
		skipped := id == "lander" || id == "csm-lm"
		if got := paletteHas(v, id); got == skipped {
			t.Errorf("part %q in palette = %v, want %v", id, got, !skipped)
		}
		if skipped {
			continue
		}
		for _, it := range v.palette {
			if !it.isComponent && it.id == id {
				if k, _ := v.paletteItemLabel(it); k != m.Tier {
					t.Errorf("part %q sits under %q, want its tier %q", id, k, m.Tier)
				}
			}
		}
	}
}

func TestVABPaletteSectionOrder(t *testing.T) {
	v := realVAB(t)
	kinds := paletteKinds(v)
	want := append([]string{"engine", "tank", "command-core", "antenna", "structure"}, s2Tiers...)
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Errorf("palette sections = %v, want %v", kinds, want)
	}
	for _, id := range []string{"relay-comsat", "endpoint-station", "command-module", "vector-1"} {
		if !paletteHas(v, id) {
			t.Errorf("%s missing from the palette", id)
		}
	}
}

// A part added to the catalog (a user overlay) is in the palette the next
// time the VAB opens, under its tier, with nobody editing a list.
func TestVABPaletteListsAnOverlayPart(t *testing.T) {
	saved := spacecraft.StageCatalog
	cp := make(map[string]spacecraft.StageModule, len(saved)+1)
	for k, m := range saved {
		cp[k] = m
	}
	cp["zz-overlay"] = spacecraft.StageModule{ID: "zz-overlay", Name: "Zed Overlay", Tier: "payload"}
	spacecraft.StageCatalog = cp
	t.Cleanup(func() { spacecraft.StageCatalog = saved })

	v := realVAB(t)
	found := false
	for _, it := range v.palette {
		if !it.isComponent && it.id == "zz-overlay" {
			found = true
			if k, _ := v.paletteItemLabel(it); k != "payload" {
				t.Errorf("overlay part under %q, want payload", k)
			}
		}
	}
	if !found {
		t.Error("overlay part not in the palette")
	}
}

func TestVABInspectorNamesTierAndFleet(t *testing.T) {
	v := realVAB(t)
	inspect := func(id string) string {
		selectPalettePart(t, v, id)
		v.focus = focusPalette
		return strings.Join(v.renderInspector(52), "\n")
	}
	if got := inspect("vector-1"); !strings.Contains(got, "catalog part · booster · Lumen fleet") {
		t.Errorf("Vector-1 inspector:\n%s", got)
	}
	if got := inspect("s-ic"); !strings.Contains(got, "catalog part · booster") || strings.Contains(got, "Lumen") {
		t.Errorf("S-IC inspector:\n%s", got)
	}
}

// paletteBoxRows renders the real VAB at the Design Size and returns the
// palette column's content rows (between the PALETTE title and the INSPECT
// box's top edge).
func paletteBoxRows(t *testing.T, v *VAB) []string {
	t.Helper()
	lines := strings.Split(v.Render(140, 40), "\n")
	title, insp := -1, -1
	for i, ln := range lines {
		if title < 0 && strings.Contains(ln, "PALETTE") {
			title = i
		}
		if strings.Contains(ln, "INSPECT") {
			insp = i
			break
		}
	}
	if title < 0 || insp < 0 {
		t.Fatalf("no PALETTE/INSPECT titles in the render:\n%s", strings.Join(lines, "\n"))
	}
	var rows []string
	for _, ln := range lines[title+1 : insp-2] { // insp-1 is INSPECT's top edge, insp-2 the palette's bottom edge
		parts := strings.Split(ln, "│")
		if len(parts) < 4 {
			t.Fatalf("palette row has no box columns: %q", ln)
		}
		rows = append(rows, parts[2])
	}
	return rows
}

func TestVABPaletteFillsItsBox(t *testing.T) {
	v := realVAB(t)
	v.focus = focusPalette
	rows := paletteBoxRows(t, v)
	if len(rows) < 18 {
		t.Fatalf("palette box holds %d rows at 140x40, want it to fill the left column (>= 18; it was 13)", len(rows))
	}
	for i, r := range rows {
		if strings.TrimSpace(r) == "" {
			t.Errorf("palette box row %d is blank; the list should fill the box", i)
		}
	}
	// The list outgrows the box, so the cursor still shows after a long walk.
	for i := 0; i < 60; i++ {
		v.moveCursor(+1)
	}
	found := false
	for _, r := range paletteBoxRows(t, v) {
		if strings.HasPrefix(r, "▸ ") {
			found = true
		}
	}
	if !found {
		t.Error("cursor not visible in the palette box after 60 downs")
	}
}

func TestVABPaletteWindowFloorIsNine(t *testing.T) {
	v := realVAB(t)
	for _, c := range []struct{ rows, want int }{{20, 20}, {9, 9}, {4, 9}, {0, 9}} {
		if got := len(v.renderPalette(50, c.rows)); got != c.want {
			t.Errorf("renderPalette rows=%d drew %d lines, want %d", c.rows, got, c.want)
		}
	}
}

// --- find ---

func findRows(v *VAB) []string {
	var out []string
	for _, ln := range v.renderPalette(50, 80) {
		out = append(out, ln)
	}
	return out
}

func typeFind(v *VAB, s string) {
	for _, r := range s {
		v.HandleKey(string(r))
	}
}

func TestVABFindNarrowsByNameOrID(t *testing.T) {
	v := realVAB(t)
	full := len(v.palette)
	v.HandleKey("/")
	if !v.CapturingText() {
		t.Fatal("CapturingText false while the find row is open")
	}
	typeFind(v, "rel")
	if len(v.palette) == 0 || len(v.palette) >= full {
		t.Fatalf("rel left %d of %d rows", len(v.palette), full)
	}
	want := 0
	for _, it := range v.paletteAll {
		_, label := v.paletteItemLabel(it)
		if strings.Contains(strings.ToLower(label), "rel") || strings.Contains(strings.ToLower(it.id), "rel") {
			want++
		}
	}
	if len(v.palette) != want {
		t.Errorf("rel leaves %d rows, want exactly the %d whose name or ID contains it", len(v.palette), want)
	}
	for _, it := range v.palette {
		_, label := v.paletteItemLabel(it)
		if !strings.Contains(strings.ToLower(label+" "+it.id), "rel") {
			t.Errorf("row %q (%s) does not contain rel", label, it.id)
		}
	}
	if !paletteHas(v, "relay-comsat") || !paletteHas(v, "deep-space-relay") {
		t.Error("rel did not keep the relay parts")
	}
	if v.paletteIdx < 0 || v.paletteIdx >= len(v.palette) {
		t.Errorf("cursor %d outside %d rows", v.paletteIdx, len(v.palette))
	}
	// the ID is searched too: f9 finds F9-S1.
	v.HandleKey("esc")
	v.HandleKey("/")
	typeFind(v, "F9")
	if !paletteHas(v, "f9-s1") {
		t.Error("f9 did not find F9-S1")
	}
}

func TestVABFindEscRestoresAndEnterKeeps(t *testing.T) {
	v := realVAB(t)
	full := len(v.palette)
	v.HandleKey("/")
	typeFind(v, "rel")
	v.HandleKey("enter")
	if v.CapturingText() {
		t.Error("enter left the find row capturing text")
	}
	if len(v.palette) >= full {
		t.Error("enter dropped the filter; it should keep it")
	}
	v.HandleKey("/")
	v.HandleKey("esc")
	if v.CapturingText() || len(v.palette) != full || v.find != "" {
		t.Errorf("esc: capturing=%v rows=%d/%d find=%q, want the whole list back", v.CapturingText(), len(v.palette), full, v.find)
	}
	if v.paletteIdx < 0 || v.paletteIdx >= len(v.palette) {
		t.Errorf("cursor %d invalid after esc", v.paletteIdx)
	}
	// esc inside find must not leave the VAB.
	v.HandleKey("/")
	if act := v.HandleKey("esc"); act != VABActionNone {
		t.Errorf("esc in find returned %v, want none", act)
	}
}

func TestVABFindTypedKeysAreNotCommands(t *testing.T) {
	v := realVAB(t)
	stages := len(v.stages)
	v.HandleKey("/")
	typeFind(v, "xadst o?")
	if len(v.stages) != stages || v.mode != vabModeFind {
		t.Errorf("typed keys ran VAB commands: stages %d->%d mode=%v", stages, len(v.stages), v.mode)
	}
	if v.find != "xadst o?" {
		t.Errorf("find = %q, want the typed text", v.find)
	}
	v.HandleKey("backspace")
	if v.find != "xadst o" {
		t.Errorf("backspace left %q", v.find)
	}
}

func TestVABFindNoMatchRow(t *testing.T) {
	v := realVAB(t)
	v.HandleKey("/")
	typeFind(v, "xyz")
	out := strings.Join(v.renderPalette(50, 12), "\n")
	if !strings.Contains(out, `(no part matches "xyz")`) {
		t.Errorf("no-match row missing:\n%s", out)
	}
	if v.paletteIdx != 0 {
		t.Errorf("cursor = %d on an empty view", v.paletteIdx)
	}
	v.HandleKey("a") // typed, and nothing to add
	if len(v.stages) != 1 {
		t.Errorf("a ran a command inside find")
	}
}

func TestVABFindTitleShowsTheField(t *testing.T) {
	v := realVAB(t)
	v.HandleKey("/")
	typeFind(v, "rel")
	if !strings.Contains(v.Render(140, 40), "find: rel") {
		t.Error("PALETTE title does not carry find: rel")
	}
}

func TestVABFindFiltersCatalogPartsAndSelects(t *testing.T) {
	v := realVAB(t)
	v.HandleKey("/")
	typeFind(v, "Relay Comsat")
	v.HandleKey("enter")
	v.focus = focusPalette
	n := len(v.stages)
	v.HandleKey("a")
	if len(v.stages) < n || len(v.stages) == 0 {
		t.Fatal("a did not add")
	}
	found := false
	for _, s := range v.stages {
		if s.catalogPartID == "relay-comsat" {
			found = true
		}
	}
	if !found {
		t.Errorf("a on the found part added %v", catalogIDs(v))
	}
}

// F1 and docs/controls.md carry the rows for the new keys (ADR 0056 "F1 and
// docs/controls.md").
func TestVABFindAndSectionRowsInF1AndDocs(t *testing.T) {
	vabRow := func(tok string) string {
		for _, sec := range helpSections {
			if sec.header != "VEHICLE ASSEMBLY (VAB)" {
				continue
			}
			for _, r := range sec.rows {
				if r[0] == tok {
					return r[1]
				}
			}
		}
		t.Fatalf("no VAB help row %q", tok)
		return ""
	}
	find := vabRow("/")
	if !strings.Contains(find, "find") || !strings.Contains(find, "esc clears") || !strings.Contains(find, "enter keeps the filter") {
		t.Errorf("F1 / row = %q", find)
	}
	pg := vabRow("PgUp / PgDn")
	if !strings.Contains(pg, "tier or kind") {
		t.Errorf("F1 PgUp / PgDn row = %q, want tier or kind", pg)
	}
	raw, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	for _, want := range []string{"| `/` | Find: type part of a name", "previous / next tier or kind section in the palette"} {
		if !strings.Contains(doc, want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}

// Two parts that share a name (the three S-IVBs, the Ascents) never read the
// same in the palette.
func TestVABPaletteRowsAreDistinct(t *testing.T) {
	v := realVAB(t)
	seen := map[string]string{}
	for _, it := range v.palette {
		if it.isComponent {
			continue
		}
		_, label := v.paletteItemLabel(it)
		if prev, dup := seen[label]; dup {
			t.Errorf("parts %q and %q both read %q", prev, it.id, label)
		}
		seen[label] = it.id
	}
}
