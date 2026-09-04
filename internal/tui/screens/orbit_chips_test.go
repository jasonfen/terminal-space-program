package screens

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// chipTestTheme is a no-op styled theme so chip content asserts match raw
// text without ANSI noise. HUDBox gets a border to mirror the real layout.
func chipTestTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// blankCanvas builds a cols×rows grid of '.' so composeChips has a base to
// overlay onto (overlayStyledBlock pads short rows, but a full grid keeps
// the placement math honest).
func blankCanvas(cols, rows int) string {
	row := strings.Repeat(".", cols)
	lines := make([]string, rows)
	for i := range lines {
		lines[i] = row
	}
	return strings.Join(lines, "\n")
}

// assertChipCellWidthConsistent guards the chip → canvas contract:
// padChipBlock measures chip lines in terminal cells (lipgloss.Width)
// while overlayStyledBlock splices them per rune (splitStyledCells).
// A glyph where the two disagree — any width-2 emoji, e.g. the 💤 the
// away line originally used (#253) — makes the overlaid canvas row one
// cell wider than the canvas for every such line. Every line a chip
// builder emits must measure the same both ways.
func assertChipCellWidthConsistent(t *testing.T, context string, lines []string) {
	t.Helper()
	for i, l := range lines {
		if mw, sc := lipgloss.Width(l), len(splitStyledCells(l)); mw != sc {
			t.Errorf("%s line %d: lipgloss.Width=%d cells but splitStyledCells splices %d — width-2 glyph on the chip path? %q",
				context, i, mw, sc, l)
		}
	}
}

func TestPadChipBlockUniformWidth(t *testing.T) {
	in := []string{"NODES", "  ▸ #1 prograde 120 m/s", "  imp"}
	out, w := padChipBlock(in)
	if w != lipgloss.Width(in[1]) {
		t.Fatalf("width = %d, want %d (the widest line)", w, lipgloss.Width(in[1]))
	}
	for i, l := range out {
		if lipgloss.Width(l) != w {
			t.Errorf("line %d width = %d, want %d", i, lipgloss.Width(l), w)
		}
	}
	if !strings.HasPrefix(out[0], "NODES") {
		t.Errorf("content not preserved: %q", out[0])
	}
}

func TestComposeChipsPlacesAndRoutes(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	canvas := blankCanvas(40, 20)
	chips := []builtChip{
		{id: settings.ChipStages, corner: cornerBottomLeft, lines: []string{"STAGES", "  ●●○"}},
	}
	out := v.composeChips(canvas, 40, 20, 0, 0, 0, chips)
	if !strings.Contains(out, "STAGES") {
		t.Fatalf("composited output missing chip content:\n%s", out)
	}
	if len(v.chipRects) != 1 {
		t.Fatalf("recorded %d rects, want 1", len(v.chipRects))
	}
	r := v.chipRects[0]
	// A click inside the recorded rectangle resolves to the chip id.
	id, ok := v.HitChip(r.colStart, r.rowStart)
	if !ok || id != settings.ChipStages {
		t.Errorf("HitChip at rect origin = (%q,%v), want (%q,true)", id, ok, settings.ChipStages)
	}
	// A click well outside misses.
	if _, ok := v.HitChip(r.colEnd+5, r.rowEnd+5); ok {
		t.Errorf("HitChip outside the rect reported a hit")
	}
}

// TestComposeChipsLeftOfPrevSharesRowBand: a leftOfPrev top-right chip
// (PROJECTED ORBIT) sits on the same top row as the previously placed
// top-right chip (ORBIT), immediately to its left — not stacked below it —
// so the column stays short and a following TARGET chip drops below both
// without overlapping. Regression for the right-column overflow that buried
// TARGET under NODES.
func TestComposeChipsLeftOfPrevSharesRowBand(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	chips := []builtChip{
		{id: "", corner: cornerTopRight, lines: []string{"ORBIT", "  a", "  b"}},
		{id: settings.ChipProjectedOrbit, corner: cornerTopRight, lines: []string{"PROJECTED", "  c"}, leftOfPrev: true},
		{id: settings.ChipTarget, corner: cornerTopRight, lines: []string{"TARGET", "  d", "  e"}},
	}
	v.composeChips(blankCanvas(80, 24), 80, 24, 0, 0, 0, chips)
	if len(v.chipRects) != 3 {
		t.Fatalf("recorded %d rects, want 3", len(v.chipRects))
	}
	orbit, proj, target := v.chipRects[0], v.chipRects[1], v.chipRects[2]
	if proj.rowStart != orbit.rowStart {
		t.Errorf("projected rowStart %d != orbit rowStart %d — not side by side", proj.rowStart, orbit.rowStart)
	}
	if proj.colEnd >= orbit.colStart {
		t.Errorf("projected (cols %d–%d) is not left of orbit (cols %d–%d)",
			proj.colStart, proj.colEnd, orbit.colStart, orbit.colEnd)
	}
	maxBottom := orbit.rowEnd
	if proj.rowEnd > maxBottom {
		maxBottom = proj.rowEnd
	}
	if target.rowStart <= maxBottom {
		t.Errorf("target rowStart %d not below the orbit/projected band bottom %d", target.rowStart, maxBottom)
	}
}

func TestComposeChipsClipsOversizeChipWithoutPanic(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	tall := make([]string, 50) // taller than the 20-row canvas
	for i := range tall {
		tall[i] = "row"
	}
	out := v.composeChips(blankCanvas(40, 20), 40, 20, 0, 0, 0,
		[]builtChip{{id: settings.ChipLaunch, corner: cornerTopLeft, lines: tall}})
	if got := strings.Count(out, "\n") + 1; got != 20 {
		t.Errorf("output row count = %d, want 20 (canvas height preserved)", got)
	}
}

// TestComposeChipsBudgetProtectsCriticalChipFromOverflow (#328/#334,
// reworked for #422/ADR 0046): a high-priority chip appended late in a
// corner's stack (the real DOCKED block, in assembleChips' order) must
// never be silently lost or truncated to overflow. Under the Graceful
// Shrink contract this is achieved by sacrificing the lower-priority
// fillers ahead of it (dropped behind a Hidden Stub, latest-added first)
// rather than by ever touching the critical chip: reproduces the #328
// report's numbers — an 80x24 terminal's canvas is 21 rows, and three
// filler chips (mirroring VESSEL/MISSION/SESSION/TIME LOCK — VESSEL
// itself is Core, the rest Normal) consume all 21 rows between them.
func TestComposeChipsBudgetProtectsCriticalChipFromOverflow(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const cCols, cRows = 78, 21 // an 80x24 terminal's canvas (totalRows-3)

	filler := func(n int) []string {
		lines := make([]string, n)
		for i := range lines {
			lines[i] = "row"
		}
		return lines
	}
	chips := []builtChip{
		{corner: cornerTopLeft, lines: filler(6), priority: chipPriorityCore},   // VESSEL-sized
		{corner: cornerTopLeft, lines: filler(3)},                               // MISSION-sized filler
		{corner: cornerTopLeft, lines: filler(2)},                               // SESSION-sized filler
		{corner: cornerTopLeft, lines: filler(2)},                               // TIME LOCK-sized filler
		{corner: cornerTopLeft, lines: []string{
			"DOCKED", "  riding in bob's stack", "  [J] request control", "  [U] ask to undock",
		}, priority: chipPriorityForced},
	}
	out := v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, 0, 0, 0, chips)
	if !strings.Contains(out, "DOCKED") {
		t.Fatalf("critical chip (DOCKED) silently lost to top-left overflow:\n%s", out)
	}
	if !strings.Contains(out, "riding in bob's stack") || !strings.Contains(out, "[U] ask to undock") {
		t.Errorf("critical chip rendered but truncated — its own content was clipped:\n%s", out)
	}
	if !strings.Contains(out, "hidden") {
		t.Errorf("fillers dropped for space but no Hidden Stub says so:\n%s", out)
	}
	assertNoChipRectOverlaps(t, v.chipRects)
}

// TestComposeChipsBudgetDropsBehindStubAboveNavball (#334, reworked for
// #422/ADR 0046): at 80x24 with the navball showing,
// navballReservedRows(w, cCols, 21) returns navballPanelH+1 = 20, leaving
// the whole right side exactly ONE spare row (chipStubHeight) — the real
// geometry at the Playable Floor whenever the navball renders (19 rows)
// alongside the label row. A lone chip needs at least 3 rows (its own
// border alone), so it can never fit there even fully Compact — the old
// "clamp it onto the canvas, accept the overlap" last resort is exactly
// the bug #422 reports (a force-shown NODES chip painting over the Core
// ORBIT chip during a burn). The new contract drops it behind a one-row
// Hidden Stub instead: never present, never overlapping, never silent.
func TestComposeChipsBudgetDropsBehindStubAboveNavball(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const cCols, cRows = 78, 21
	const navballReserved = navballPanelH + 1 // == 20, matches navballReservedRows at this size

	chips := []builtChip{
		{id: settings.ChipNodes, corner: cornerBottomRight, lines: []string{
			"NODES", "  ▸ #1 prograde 42 m/s", "  imp", "  (+1 more → [m])",
		}, priority: chipPriorityForced},
	}
	out := v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, navballReserved, 0, 0, chips)
	if strings.Contains(out, "▸ #1 prograde 42 m/s") {
		t.Fatalf("force-shown NODES chip rendered despite not fitting above the navball reservation — it should have dropped behind a stub instead:\n%s", out)
	}
	if !strings.Contains(out, "hidden") {
		t.Errorf("NODES dropped for space but no Hidden Stub says so:\n%s", out)
	}
	if len(v.chipRects) != 0 {
		t.Errorf("dropped chip left a clickable rect: %+v", v.chipRects)
	}
}

// TestComposeChipsDropsLowerPriorityBeforeHigher exercises the new drop
// order directly: two chips together exceed their side's entire budget
// (both start Full, neither has a Compact Form to shrink into), so the
// LOWER-priority one (SECOND, Forced) must drop behind a Hidden Stub
// while the HIGHER-priority one (FIRST, Core) stays fully on-canvas —
// replacing the pre-#422 "clamp the second one on top of the first,
// accept the overlap" behaviour (ADR 0046).
func TestComposeChipsDropsLowerPriorityBeforeHigher(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const cCols, cRows = 40, 10 // small on purpose: two 8-row blocks won't both fit

	chips := []builtChip{
		{corner: cornerTopLeft, lines: []string{"FIRST", "a", "b", "c", "d", "e"}, priority: chipPriorityCore},
		{corner: cornerTopLeft, lines: []string{"SECOND", "f", "g", "h", "i", "j"}, priority: chipPriorityForced},
	}
	out := v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, 0, 0, 0, chips)
	if got := strings.Count(out, "\n") + 1; got != cRows {
		t.Fatalf("output row count = %d, want %d (canvas height preserved)", got, cRows)
	}
	if !strings.Contains(out, "FIRST") || !strings.Contains(out, "e") {
		t.Errorf("higher-priority FIRST chip lost or truncated:\n%s", out)
	}
	if strings.Contains(out, "SECOND") || strings.Contains(out, "j") {
		t.Errorf("lower-priority SECOND chip should have dropped, not rendered/overlapped:\n%s", out)
	}
	if !strings.Contains(out, "hidden") {
		t.Errorf("SECOND dropped for space but no Hidden Stub says so:\n%s", out)
	}
	assertNoChipRectOverlaps(t, v.chipRects)
}

func TestChipEnabledRespectsSettingsAndDeclutter(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	if !v.chipEnabled(settings.ChipStages) {
		t.Error("default settings should enable a chip")
	}
	s := settings.Default()
	s.SetChip(settings.ChipStages, false)
	v.SetSettings(s)
	if v.chipEnabled(settings.ChipStages) {
		t.Error("disabled chip should not be enabled")
	}
	if !v.chipEnabled("") {
		t.Error("empty-id (always-on) chip should be enabled by default")
	}
	v.SetDeclutter(true)
	if v.chipEnabled(settings.ChipNodes) {
		t.Error("declutter should suppress an otherwise-enabled chip")
	}
	if v.chipEnabled("") {
		t.Error("declutter should suppress even always-on chips")
	}
}

func TestActiveStageFuel(t *testing.T) {
	// Firing (bottom) stage is index 0. The readout reflects it alone, not
	// the whole-stack aggregate — a spent first stage with full uppers must
	// read 0%, not "21% total".
	c := &spacecraft.Spacecraft{
		Stages: []spacecraft.Stage{
			{FuelMass: 40, FuelCapacity: 100}, // firing stage: 40%
			{FuelMass: 100, FuelCapacity: 100},
		},
	}
	pct, kg, ok := activeStageFuel(c)
	if !ok || pct != 40 || kg != 40 {
		t.Errorf("activeStageFuel = (%g%%, %g kg, %v), want (40, 40, true)", pct, kg, ok)
	}

	spent := &spacecraft.Spacecraft{
		Stages: []spacecraft.Stage{
			{FuelMass: 0, FuelCapacity: 2_160_000},     // S-IC burned out → 0%
			{FuelMass: 440_000, FuelCapacity: 440_000}, // full upper stage
		},
	}
	if pct, _, ok := activeStageFuel(spent); !ok || pct != 0 {
		t.Errorf("spent first stage = (%g%%, ok=%v), want 0%% (not the ~21%% aggregate)", pct, ok)
	}

	none := &spacecraft.Spacecraft{Stages: []spacecraft.Stage{{FuelMass: 0, FuelCapacity: 0}}}
	if _, _, ok := activeStageFuel(none); ok {
		t.Error("activeStageFuel ok = true with zero firing-stage capacity, want false")
	}
	if _, _, ok := activeStageFuel(&spacecraft.Spacecraft{}); ok {
		t.Error("activeStageFuel ok = true with no stages, want false")
	}
}

func TestBuildStagesChip(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}

	// Single stage → nil (slim column already covers it).
	c.Stages = []spacecraft.Stage{{Name: "solo", FuelMass: 10, FuelCapacity: 10}}
	if got := v.buildStagesChip(w); got != nil {
		t.Errorf("single-stage chip = %v, want nil", got)
	}

	// Multi stage → one pip per stage, dry stages hollow.
	c.Stages = []spacecraft.Stage{
		{Name: "S-IC", FuelMass: 0, FuelCapacity: 100}, // dry → ○
		{Name: "S-II", FuelMass: 50, FuelCapacity: 100},
		{Name: "S-IVB", FuelMass: 80, FuelCapacity: 100},
	}
	chip := v.buildStagesChip(w)
	if chip == nil {
		t.Fatal("multi-stage chip = nil, want content")
	}
	joined := strings.Join(chip, "\n")
	if !strings.Contains(joined, "STAGES") {
		t.Errorf("chip missing header:\n%s", joined)
	}
	if !strings.Contains(joined, "○") || !strings.Contains(joined, "●") {
		t.Errorf("chip pips missing filled/hollow markers:\n%s", joined)
	}
	if !strings.Contains(joined, "(1/3)") {
		t.Errorf("chip missing active-stage index (1/3):\n%s", joined)
	}
}

func TestBuildNodesChipSummary(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	c.Nodes = nil
	if got := v.buildNodesChip(w); got != nil {
		t.Errorf("no-nodes chip = %v, want nil", got)
	}

	c.Nodes = []spacecraft.ManeuverNode{
		{DV: 120, TriggerTime: w.Clock.SimTime.Add(10 * time.Minute)},
		{DV: 80, TriggerTime: w.Clock.SimTime.Add(30 * time.Minute)},
		{DV: 40, TriggerTime: w.Clock.SimTime.Add(60 * time.Minute)},
	}
	chip := v.buildNodesChip(w)
	joined := strings.Join(chip, "\n")
	if !strings.Contains(joined, "NODES") {
		t.Errorf("chip missing header:\n%s", joined)
	}
	if !strings.Contains(joined, hudNodeMarker) {
		t.Errorf("chip missing click-affordance marker %q:\n%s", hudNodeMarker, joined)
	}
	if !strings.Contains(joined, "(+2 more → [m])") {
		t.Errorf("chip missing overflow count (+2 more → [m]):\n%s", joined)
	}
}

// TestBuildNodesChipMarksOverBudgetNode — ADR 0047 §2 / #428: the
// NODES chip's next-node line carries the same Over-budget Node marker
// as the planner's own PLANNED NODES list, so a plan that can't be
// afforded is visible without opening [m].
func TestBuildNodesChipMarksOverBudgetNode(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	budget := c.RemainingDeltaV()
	c.Nodes = []spacecraft.ManeuverNode{
		{Mode: spacecraft.BurnPrograde, DV: budget + 1521, TriggerTime: w.Clock.SimTime.Add(10 * time.Minute)},
	}
	joined := strings.Join(v.buildNodesChip(w), "\n")
	if !strings.Contains(joined, "exceeds budget by 1521 m/s") {
		t.Errorf("NODES chip missing over-budget marker:\n%s", joined)
	}

	// An affordable node must NOT carry the marker.
	c.Nodes = []spacecraft.ManeuverNode{
		{Mode: spacecraft.BurnPrograde, DV: 10, TriggerTime: w.Clock.SimTime.Add(10 * time.Minute)},
	}
	joined = strings.Join(v.buildNodesChip(w), "\n")
	if strings.Contains(joined, "exceeds budget") {
		t.Errorf("affordable node wrongly marked over-budget:\n%s", joined)
	}
}

// TestWorstCaseFrameDoesNotOverflow is the regression that motivated the
// v0.13 cycle: with a target set, an Apollo stack launching from the pad,
// and planted nodes, the old tall HUD column rendered taller than the
// canvas and the terminal scrolled — hiding the title and orbit view. The
// slim column + canvas chips bound the frame to the terminal height, so
// the title row survives and nothing scrolls off.
func TestWorstCaseFrameDoesNotOverflow(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const cols, rows = 120, 40
	v.Resize(cols, rows)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Apollo stack on the pad (launch in progress, multi-stage).
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutApolloStackID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	// Target the Moon + plant several nodes — the rest of the worst case.
	for i, b := range w.System().Bodies {
		if b.ID == "moon" {
			w.SetTargetBody(i)
		}
	}
	for i := 0; i < 5; i++ {
		c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
			DV:          float64(100 * (i + 1)),
			TriggerTime: w.Clock.SimTime.Add(time.Duration(i+1) * 10 * time.Minute),
		})
	}

	out := v.Render(w, 0, cols, rows)
	if h := strings.Count(out, "\n") + 1; h > rows {
		t.Errorf("frame height = %d rows, want ≤ %d (terminal would scroll, hiding the title)", h, rows)
	}
	// The title row must be the first line (not scrolled off the top).
	if first := strings.SplitN(out, "\n", 2)[0]; !strings.Contains(first, "terminal-space-program") {
		t.Errorf("title row not first; got %q", first)
	}
}

// TestDeclutterHidesChipsKeepsColumn: F2 declutter suppresses every Chip
// (here the always-relevant ATTITUDE chip) while the slim HUD column —
// which it must never hide (CONTEXT.md §Declutter) — keeps rendering.
func TestNzeroSnapsNegativeZero(t *testing.T) {
	cases := []struct {
		x        float64
		decimals int
		want     float64
	}{
		{-0.3, 0, 0},    // rounds to 0 at %.0f → snapped to +0
		{0.3, 0, 0},     // also rounds to 0 → +0 (sign already fine)
		{-0.04, 1, 0},   // rounds to 0.0 at %.1f → +0
		{-0.6, 0, -0.6}, // rounds to -1 → untouched
		{12.3, 1, 12.3}, // non-zero → untouched
	}
	for _, c := range cases {
		got := nzero(c.x, c.decimals)
		if got != c.want {
			t.Errorf("nzero(%g, %d) = %g, want %g", c.x, c.decimals, got, c.want)
		}
		// The snapped value must never format with a negative sign at 0.
		if c.want == 0 && fmt.Sprintf("%+.*f", c.decimals, got)[0] == '-' {
			t.Errorf("nzero(%g, %d) still formats as negative zero", c.x, c.decimals)
		}
	}
}

func TestDeclutterHidesChipsKeepsColumn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "ATTITUDE") {
		t.Fatalf("expected ATTITUDE chip with declutter off:\n%s", out)
	}
	if !strings.Contains(out, "VESSEL") {
		t.Fatalf("expected VESSEL slim column with declutter off")
	}

	v.SetDeclutter(true)
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "ATTITUDE") {
		t.Errorf("declutter on: ATTITUDE chip should be hidden:\n%s", out)
	}
	if !strings.Contains(out, "VESSEL") {
		t.Errorf("declutter on: slim HUD column must still render (never hidden):\n%s", out)
	}

	v.SetDeclutter(false)
	out = v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "ATTITUDE") {
		t.Errorf("declutter off again: ATTITUDE chip should return:\n%s", out)
	}
}

// TestOrbitMetricsAlwaysOnAndLiveBurnForceShows: the ORBIT-metrics readout
// is non-toggleable (renders with every Chip disabled) but F2 declutter
// still clears it. A live burn now folds into the NODES chip and
// force-shows — it renders even with every Chip disabled AND survives F2
// declutter (v0.16: a live burn is safety-critical and can't be hidden).
// Only the pinned VESSEL core also survives declutter.
func TestOrbitMetricsAlwaysOnAndLiveBurnForceShows(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// Disable every toggleable Chip (incl. ChipNodes); the always-on
	// ORBIT readout must persist.
	s := settings.Default()
	for _, c := range settings.AllChips {
		s.SetChip(c, false)
	}
	v.SetSettings(s)

	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "ORBIT") {
		t.Errorf("ORBIT metrics must render with all chips disabled (non-toggleable):\n%s", out)
	}

	// Light an active burn → the firing head force-shows inside NODES even
	// with ChipNodes (and every other chip) disabled.
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 120,
		EndTime:     w.Clock.SimTime.Add(30 * time.Second),
	}
	out = v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "NODES") || !strings.Contains(out, "120 m/s") {
		t.Errorf("a live burn must force-show in the NODES chip with all chips disabled:\n%s", out)
	}

	// F2 declutter clears ORBIT metrics, but the live burn force-shows
	// through it; the pinned VESSEL core also survives.
	v.SetDeclutter(true)
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "ORBIT") {
		t.Errorf("declutter must hide the ORBIT metrics chip:\n%s", out)
	}
	if !strings.Contains(out, "120 m/s") {
		t.Errorf("a live burn must survive declutter (force-shown, safety-critical):\n%s", out)
	}
	if !strings.Contains(out, "VESSEL") {
		t.Errorf("pinned VESSEL core must survive declutter")
	}

	// Cut the burn → with every chip disabled the NODES chip (no nodes
	// planted) returns to hidden.
	c.ActiveBurn = nil
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "120 m/s") {
		t.Errorf("burn readout lingered after the burn ended:\n%s", out)
	}
}

// TestNodesChipForceShowsWhenMultipleNodesQueued — #293: staleness is
// exactly "more than one node queued" (every node after the first fires
// against an orbit it was never computed for), so 2+ queued nodes
// force-show the NODES chip past the ChipNodes toggle AND F2 declutter —
// extending the existing live-burn force-show rationale rather than
// adding a second, differently-gated one. A single queued node must NOT
// force-show; it still honours the toggle/declutter like any chip.
func TestNodesChipForceShowsWhenMultipleNodesQueued(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// Disable every toggleable Chip (incl. ChipNodes) and declutter, same
	// setup as the live-burn force-show test.
	s := settings.Default()
	for _, chipID := range settings.AllChips {
		s.SetChip(chipID, false)
	}
	v.SetSettings(s)
	v.SetDeclutter(true)

	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Nodes = []spacecraft.ManeuverNode{
		{Mode: spacecraft.BurnPrograde, DV: 42, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
	}
	out := v.Render(w, 0, 120, 40)
	if strings.Contains(out, "NODES") {
		t.Errorf("a single queued node must not force-show past the toggle/declutter:\n%s", out)
	}

	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode: spacecraft.BurnRetrograde, DV: 7, TriggerTime: w.Clock.SimTime.Add(2 * time.Minute),
	})
	out = v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "NODES") {
		t.Errorf("2+ queued nodes must force-show the NODES chip past the toggle/declutter:\n%s", out)
	}
}

// TestNodesChipForceShowIsPerCraftNotFleetWide (#333): the staleness
// hazard the force-show gate exists for is per-vessel — every node
// behind the first ON THE SAME CRAFT was computed against an orbit that
// no longer exists once the first one fires. Two different craft each
// carrying exactly one node have no such hazard on either vessel, so
// summing across the fleet must not force the chip past a declutter +
// disabled toggle the player explicitly chose.
func TestNodesChipForceShowIsPerCraftNotFleetWide(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	s := settings.Default()
	for _, chipID := range settings.AllChips {
		s.SetChip(chipID, false)
	}
	v.SetSettings(s)
	v.SetDeclutter(true)

	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("expected an active craft")
	}
	active.Nodes = []spacecraft.ManeuverNode{
		{Mode: spacecraft.BurnPrograde, DV: 42, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
	}
	second := &spacecraft.Spacecraft{
		Name:    "relay",
		Primary: active.Primary,
		State:   active.State,
		Stages:  []spacecraft.Stage{{DryMass: 1000}},
		Nodes: []spacecraft.ManeuverNode{
			{Mode: spacecraft.BurnPrograde, DV: 10, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
		},
	}
	second.SyncFields()
	w.Crafts = append(w.Crafts, second)

	out := v.Render(w, 0, 120, 40)
	if strings.Contains(out, "NODES") {
		t.Errorf("one node each on two different craft force-showed NODES past the toggle/declutter — the hazard is per-craft, not fleet-wide:\n%s", out)
	}
}

// TestNodesChipOverflowCountIsPerCraft (#333): the "(+N more)" overflow
// annotation must count the SAME craft's own remaining queue that the
// "next" node line above it names — folding in another craft's
// unrelated nodes misdescribes whose queue is actually stale.
func TestNodesChipOverflowCountIsPerCraft(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("expected an active craft")
	}
	active.Nodes = []spacecraft.ManeuverNode{
		{DV: 10, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
	}
	other := &spacecraft.Spacecraft{
		Name:    "relay",
		Primary: active.Primary,
		State:   active.State,
		Stages:  []spacecraft.Stage{{DryMass: 1000}},
		Nodes: []spacecraft.ManeuverNode{
			{DV: 5, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
			{DV: 6, TriggerTime: w.Clock.SimTime.Add(2 * time.Minute)},
			{DV: 7, TriggerTime: w.Clock.SimTime.Add(3 * time.Minute)},
		},
	}
	other.SyncFields()
	w.Crafts = append(w.Crafts, other)

	joined := strings.Join(v.buildNodesChip(w), "\n")
	if strings.Contains(joined, "more") {
		t.Errorf("active craft has a single node; overflow count leaked another craft's queue:\n%s", joined)
	}
}

// TestOrbitMetricsShowsDirectionIndicator — issue #63: the ORBIT chip
// carries an explicit prograde/retrograde orbit-direction readout so a
// genuine reversal is never confused with a projection/shading artifact.
// Default LEO reads prograde; flipping the velocity (h sign reverses →
// inclination crosses 90°) flips the readout to retrograde.
func TestOrbitMetricsShowsDirectionIndicator(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	joined := strings.Join(v.buildOrbitMetricsChip(w), "\n")
	if !strings.Contains(joined, "direction:") {
		t.Fatalf("ORBIT chip missing the direction readout:\n%s", joined)
	}
	if !strings.Contains(joined, "prograde") {
		t.Errorf("default LEO should read prograde:\n%s", joined)
	}

	// Reverse the orbit: negating v flips h = r×v, pushing inclination
	// past 90° → retrograde.
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.State.V = c.State.V.Scale(-1)
	joined = strings.Join(v.buildOrbitMetricsChip(w), "\n")
	if !strings.Contains(joined, "retrograde") {
		t.Errorf("reversed orbit should read retrograde:\n%s", joined)
	}
}

// TestOrbitMetricsChipShowsEccentricity — #426 (CONTEXT.md Chip entry): the
// full-form ORBIT chip always carries an `e:` row so the three
// eccentricity-graded challenge rungs have a number on the HUD to check
// against. Full form only; the Compact Form stays the Ap/Pe strip.
func TestOrbitMetricsChipShowsEccentricity(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}

	joined := strings.Join(v.buildOrbitMetricsChip(w), "\n")
	if !strings.Contains(joined, chipRow("e:", "")) {
		t.Fatalf("ORBIT chip missing the e: row at the shared label column:\n%s", joined)
	}

	// Compact Form stays the Ap/Pe strip — no e: row. (Matching on the
	// chipRow-prefixed "  e:" rather than bare "e:" so this doesn't
	// false-positive on "Pe:", which also contains the substring "e:".)
	compact := strings.Join(v.buildOrbitMetricsChipCompact(w), "\n")
	if strings.Contains(compact, "  e:") {
		t.Errorf("Compact Form should not carry the e: row:\n%s", compact)
	}
}

// TestProjectedOrbitIsSeparateChip — issue #63 follow-up: the projected
// post-burn orbit is its own PROJECTED ORBIT chip stacked beneath the
// always-on ORBIT chip, so planting a node shows the current and
// projected orbits simultaneously instead of the projection replacing
// the live readout.
func TestProjectedOrbitIsSeparateChip(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// No node planted: the live ORBIT chip renders, the projected chip
	// is absent.
	if got := v.buildProjectedOrbitChip(w); got != nil {
		t.Errorf("projected chip should be nil with no planted node, got:\n%s", strings.Join(got, "\n"))
	}
	cur := strings.Join(v.buildOrbitMetricsChip(w), "\n")
	if !strings.Contains(cur, "ORBIT") || strings.Contains(cur, "PROJECTED ORBIT") {
		t.Fatalf("expected the live ORBIT chip with no node planted:\n%s", cur)
	}

	// Plant a resolved prograde node → both chips render together.
	w.PlanNode(sim.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(30 * time.Minute),
		Mode:        spacecraft.BurnPrograde,
		DV:          100,
		PrimaryID:   w.ActiveCraft().Primary.ID,
	})
	cur = strings.Join(v.buildOrbitMetricsChip(w), "\n")
	proj := strings.Join(v.buildProjectedOrbitChip(w), "\n")
	if !strings.Contains(cur, "altitude:") || strings.Contains(cur, "PROJECTED ORBIT") {
		t.Errorf("the current ORBIT chip must stay live (not replaced by the projection):\n%s", cur)
	}
	if !strings.Contains(proj, "PROJECTED ORBIT") {
		t.Errorf("the projected chip must render once a node is planted:\n%s", proj)
	}
	// The projected (elliptical) orbit carries a period readout so a
	// comsat insertion burn can be tuned to a target period before firing.
	if !strings.Contains(proj, "period:") {
		t.Errorf("the projected chip must show the resulting orbital period:\n%s", proj)
	}

	// End to end: both headers appear in a rendered frame.
	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "ORBIT") || !strings.Contains(out, "PROJECTED ORBIT") {
		t.Errorf("a rendered frame with a planted node must show both ORBIT and PROJECTED ORBIT:\n%s", out)
	}
}

// TestActiveCraftGlyphWinsOverlappingCell — regression for the lunar-orbit
// staging report ("descent module disappears when I stage it"). A
// just-jettisoned stage spawns ~60 m from the active craft — sub-pixel at
// orbital zoom — so it lands in the same canvas cell. The active craft's
// glyph must win that cell so the player's own vessel never vanishes under
// dropped debris. Pre-fix the non-active craft loop ran after the active
// stamp and overdrew it.
func TestActiveCraftGlyphWinsOverlappingCell(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	if active == nil {
		t.Fatal("expected an active craft")
	}
	active.Glyph = "Ⓐ" // distinctive marks — won't collide with HUD chrome

	// A passive craft at the SAME inertial position with a different glyph,
	// the way a freshly jettisoned stage sits a sub-pixel away.
	debris := &spacecraft.Spacecraft{
		Name:     "debris",
		Glyph:    "Ⓩ",
		Color:    "#FF5F5F",
		Primary:  active.Primary,
		State:    active.State,
		Throttle: 0,
		Stages:   []spacecraft.Stage{{DryMass: 1000, Glyph: "Ⓩ", Color: "#FF5F5F"}},
	}
	debris.SyncFields()
	w.Crafts = append(w.Crafts, debris)

	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "Ⓐ") {
		t.Errorf("active craft glyph Ⓐ missing — overdrawn by an overlapping passive craft:\n%s", out)
	}
}

func TestBuildVesselChipCoreOnly(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := strings.Join(v.buildVesselChip(w), "\n")
	if !strings.Contains(out, "VESSEL") || !strings.Contains(out, "PROPELLANT") {
		t.Errorf("vessel chip missing core headers:\n%s", out)
	}
	if !strings.Contains(out, "velocity") || !strings.Contains(out, "Δv budget") {
		t.Errorf("vessel chip missing core telemetry rows:\n%s", out)
	}
	// Orbit shape lives in the Orbit-metrics chip — the vessel chip must
	// not carry apoapsis/periapsis rows.
	if strings.Contains(out, "apoapsis") || strings.Contains(out, "periapsis") {
		t.Errorf("vessel chip still carries orbit-shape rows (should be a separate chip):\n%s", out)
	}
}

// TestBuildNodesChipMergesActiveBurn — the NODES chip carries an in-flight
// burn as its firing head above the planted-node summary (v0.16), and
// shows the firing head alone when a burn is live with no upcoming nodes.
func TestBuildNodesChipMergesActiveBurn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()

	// Burn in flight + a planted node → both appear, burn first.
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 120,
		EndTime:     w.Clock.SimTime.Add(30 * time.Second),
	}
	c.Nodes = []spacecraft.ManeuverNode{
		{DV: 80, TriggerTime: w.Clock.SimTime.Add(30 * time.Minute)},
	}
	chip := v.buildNodesChip(w)
	joined := strings.Join(chip, "\n")
	if !strings.HasPrefix(chip[0], "NODES") {
		t.Errorf("chip header should be NODES:\n%s", joined)
	}
	if !strings.Contains(joined, "120 m/s") {
		t.Errorf("firing burn missing from merged chip:\n%s", joined)
	}
	if !strings.Contains(joined, "80 m/s") {
		t.Errorf("planted node missing from merged chip:\n%s", joined)
	}
	// Firing head must come before the planted node.
	if strings.Index(joined, "120 m/s") > strings.Index(joined, "80 m/s") {
		t.Errorf("firing burn should head the chip, above planted nodes:\n%s", joined)
	}

	// Burn in flight, no planted nodes → chip still shows the firing head.
	c.Nodes = nil
	chip = v.buildNodesChip(w)
	if chip == nil {
		t.Fatal("chip nil with a live burn and no nodes; want the firing head")
	}
	if !strings.Contains(strings.Join(chip, "\n"), "120 m/s") {
		t.Errorf("firing head missing when no nodes planted:\n%s", strings.Join(chip, "\n"))
	}

	// Nothing burning, no nodes → nil.
	c.ActiveBurn = nil
	if got := v.buildNodesChip(w); got != nil {
		t.Errorf("chip should be nil with no burn and no nodes, got %v", got)
	}
}

// TestEmptySlateSaysSo (#310): with no craft at all the flight view used to
// render nothing where the VESSEL chip goes, while the camera fell through to
// the system origin at the old craft-scale zoom — a hard-zoomed star and no
// explanation. The chip must state the situation and offer the way out, and it
// must distinguish "you have no craft" from "your craft is riding in someone
// else's stack", which are different situations with different next moves.
func TestEmptySlateSaysSo(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Crafts = nil
	w.ActiveCraftIdx = 0

	out := strings.Join(v.buildVesselChip(w), "\n")
	if out == "" {
		t.Fatal("empty craft slate renders nothing — the state the player cannot decode")
	}
	if !strings.Contains(out, "empty") {
		t.Errorf("empty-slate chip does not say the slate is empty:\n%s", out)
	}
	if !strings.Contains(out, "[n]") {
		t.Errorf("empty-slate chip offers no way out:\n%s", out)
	}

	// Docked as guest: the slate is empty for a reason we know, and "launch a
	// new flight" would be the wrong advice.
	w.DockGuest = &sim.DockGuestLink{OwnerFP: "SHA256:bob", OwnerHandle: "bob"}
	out = strings.Join(v.buildVesselChip(w), "\n")
	if !strings.Contains(out, "bob") || !strings.Contains(out, "[U]") {
		t.Errorf("docked-as-guest empty slate does not name the stack or the release key:\n%s", out)
	}
	if strings.Contains(out, "[n]") {
		t.Errorf("docked-as-guest chip offers a new launch as if the craft were gone:\n%s", out)
	}
}

// dockGuestStackGhostWorld builds a World with no local craft, docked as a
// guest in "bob"'s stack, whose ghost carries a real 500 km circular orbit
// around Earth — the fixture both badged-panel tests (VESSEL and ORBIT)
// share (ADR 0038 S4 part 3).
func dockGuestStackGhostWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("Earth")
	w.Crafts = nil
	w.ActiveCraftIdx = 0
	w.DockGuest = &sim.DockGuestLink{OwnerFP: "SHA256:bob", OwnerHandle: "bob", OwnerActiveCraftID: 42}

	mu := earth.GravitationalParameter()
	r := earth.RadiusMeters() + 500e3
	rel := orbital.Vec3{X: r}
	vel := orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.Ghosts = []sim.Ghost{{
		Owner: "SHA256:bob", CraftID: 42, Handle: "bob", Name: "bob's stack",
		PrimaryID: earth.ID,
		Pos:       w.BodyPosition(*earth).Add(rel), RelPos: rel, Vel: vel,
	}}
	return w
}

// TestDockGuestVesselChipShowsBadgedFlightData (ADR 0038 S4 part 3): once
// the stack's ghost report has landed, the VESSEL chip upgrades from the
// bare #310 "why is this empty" placeholder to the stack's real flight
// data — name, primary, velocity — badged as the owner's rather than the
// player's own, so the numbers never read as this player's ship.
func TestDockGuestVesselChipShowsBadgedFlightData(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := dockGuestStackGhostWorld(t)

	out := strings.Join(v.buildVesselChip(w), "\n")
	for _, want := range []string{"bob", "bob's stack", "Earth", "velocity:"} {
		if !strings.Contains(out, want) {
			t.Errorf("badged VESSEL chip missing %q:\n%s", want, out)
		}
	}
}

// TestDockGuestOrbitChipShowsBadgedShape (ADR 0038 S4 part 3): the
// always-on ORBIT chip goes dark whenever !CraftVisibleHere (today,
// riding as a guest) — exactly when there IS a live orbit to show, the
// stack's. It must render the ghost's orbit shape, badged with the
// owner's handle.
func TestDockGuestOrbitChipShowsBadgedShape(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := dockGuestStackGhostWorld(t)

	out := strings.Join(v.buildOrbitMetricsChip(w), "\n")
	if out == "" {
		t.Fatal("ORBIT chip renders nothing while docked as a guest with a live ghost")
	}
	for _, want := range []string{"bob", "Ap:", "Pe:"} {
		if !strings.Contains(out, want) {
			t.Errorf("badged ORBIT chip missing %q:\n%s", want, out)
		}
	}

	// Solo, no craft at all, no DockGuest: still nil — no stack to badge.
	w2, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w2.Crafts = nil
	w2.ActiveCraftIdx = 0
	if got := v.buildOrbitMetricsChip(w2); got != nil {
		t.Errorf("ORBIT chip rendered with no craft and no DockGuest:\n%s", strings.Join(got, "\n"))
	}
}

// TestLosingTheCraftRefits (#310): losing every craft is a framing change even
// though Focus.Kind stays FocusCraft. Without it the centre snaps to the system
// origin (FocusPosition's fall-through) while the scale stays at the craft's
// alt×3 fit — the two halves of "the view jumped to the Sun, zoomed hard in"
// from one cause. The fit must be re-resolved, which lands on the system-wide
// radius instead.
func TestLosingTheCraftRefits(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	v.Render(w, 0, 120, 40) // first frame fits to the craft's altitude
	craftScale := v.baseScale
	if craftScale <= 0 {
		t.Fatalf("craft-focused fit produced no scale (%v)", craftScale)
	}

	w.Crafts = nil // the slate empties — [J] hands the last craft away
	v.Render(w, 0, 120, 40)
	if v.baseScale == craftScale {
		t.Errorf("losing the craft left the camera at the craft-scale fit %v — the Sun at hard zoom", craftScale)
	}
	// The system-wide fall-through is a far coarser scale than an orbit fit.
	if v.baseScale >= craftScale {
		t.Errorf("post-loss fit %v is not zoomed out relative to the craft fit %v", v.baseScale, craftScale)
	}
}

// ---------------------------------------------------------------------
// ADR 0046 / #422: Graceful Shrink — one column per side, Compact Form
// before drop, numbers clip right-only.
// ---------------------------------------------------------------------

// assertNoChipRectOverlaps fails the test if any two recorded chip
// rectangles share a cell. This is the core invariant of the Graceful
// Shrink contract: whatever a side's chips resolve to (Full, Compact, or
// a mix), the placed rectangles must never intersect — the pre-#422 bug
// was exactly two chips (or a chip and its own neighbour) painting into
// the same cells.
func assertNoChipRectOverlaps(t *testing.T, rects []chipRect) {
	t.Helper()
	overlaps := func(a, b chipRect) bool {
		return a.colStart <= b.colEnd && b.colStart <= a.colEnd &&
			a.rowStart <= b.rowEnd && b.rowStart <= a.rowEnd
	}
	for i := 0; i < len(rects); i++ {
		for j := i + 1; j < len(rects); j++ {
			if overlaps(rects[i], rects[j]) {
				t.Errorf("chip rects overlap: %+v and %+v", rects[i], rects[j])
			}
		}
	}
}

// realisticChipSet builds a chip list shaped like a real flight frame —
// VESSEL/ORBIT Core, a couple of Normal top-left transients, TARGET
// Normal, STAGES/CHAT bottom-left, NODES bottom-right (Forced while
// burning) — each with the same kind of Compact Form real builders now
// provide (title + 1-2 rows), sized close to the dumps in the 2026-09-02
// UX review (visual-clarity-04/16/20.txt) that motivated ADR 0046.
func realisticChipSet(burning bool) []builtChip {
	nodesPriority := chipPriorityNormal
	nodesLines := []string{
		"NODES", "  ▸ #1 T+15219s Prograde 3054 m/s", "  fin 109s", "  (+3 more → [m])",
	}
	nodesCompact := []string{"NODES", "  ▸ #1 Prograde 3054 m/s"}
	if burning {
		nodesPriority = chipPriorityForced
		nodesLines = []string{
			"NODES", "  ● vessel 1 (active) — Retrograde, Δv 23626 m/s, T-87s",
			"  ▸ #1 T+15219s  Prograde  3054 m/s", "  fin 109s", "  (+3 more → [m])",
		}
		nodesCompact = []string{"NODES", "  ● Retrograde Δv 23626 m/s"}
	}
	return []builtChip{
		{corner: cornerTopLeft, priority: chipPriorityCore,
			lines:   []string{"VESSEL", "  S-IVB-1", "  primary:   Earth", "  velocity:  7.50 km/s", "PROPELLANT", "  fuel:      89% (35775 kg)", "  mass:      47495 kg", "  Δv budget: 5777 m/s", "  throttle:  100%"},
			compact: []string{"VESSEL  S-IVB-1", "  fuel: 89% (35775 kg)  Δv: 5777 m/s"}},
		{id: settings.ChipFrameTransition, corner: cornerTopLeft,
			lines: []string{"FRAME TRANSITION", "  Earth → Moon", "  at T+5d4h  (node #3)"}},
		{id: settings.ChipMissions, corner: cornerTopLeft,
			lines:   []string{"MISSION  Flight School: Plan a Burn", "  ▸ Warp to the node  0/1", "    Press [G] to auto-warp to the burn."},
			compact: []string{"MISSION  Flight School: Plan a Burn", "  ▸ Warp to the node  0/1"}},
		{corner: cornerTopRight, priority: chipPriorityCore,
			// #426: the Full form grew an `e:` row (eccentricity, always-on,
			// full form only — the Compact Form stays the Ap/Pe strip below).
			lines:   []string{"ORBIT", "  altitude:  500.0 km", "  Ap:        500.0 km", "  t→Ap:      47m", "  Pe:        498.2 km", "  t→Pe:      12m", "  period:    1h34m28s", "  inclin.:   0.00°", "  direction: prograde", "  e:         0.0004"},
			compact: []string{"ORBIT", "  Ap: 500.0 km  Pe: 498.2 km"}},
		{id: settings.ChipTarget, corner: cornerTopRight,
			lines:   []string{"TARGET", "  body:     Moon", "  Δi:       19.44°", "  range:    371639 km", "  TCA:      4.72h"},
			compact: []string{"TARGET  Moon", "  range: 371639 km"}},
		{id: settings.ChipStages, corner: cornerBottomLeft,
			lines:   []string{"STAGES", "  ●●●", "  ▸ S-IC (1/3)"},
			compact: []string{"STAGES  ●●●"}},
		{id: "", corner: cornerBottomLeft, lines: []string{"◇ bob joined"}},
		{id: settings.ChipNodes, corner: cornerBottomRight, priority: nodesPriority,
			lines: nodesLines, compact: nodesCompact},
	}
}

// canvasDimsFor mirrors the real orbit screen's canvas sizing (border +
// title consume 2 cols / 3 rows) for a given terminal size, so tests
// exercise the same cCols/cRows the app actually renders at.
func canvasDimsFor(termW, termH int) (cCols, cRows int) {
	return termW - 2, termH - 3
}

// TestGracefulShrinkNoOverlapAtCoreSizes (test requirement (b)): with a
// realistic chip set and the navball showing, no two admitted chip rects
// ever overlap at the Playable Floor (104×24), an intermediate size
// (120×36), or the Design Size (140×40, ADR 0046).
func TestGracefulShrinkNoOverlapAtCoreSizes(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{104, 24}, {120, 36}, {140, 40}} {
		t.Run(fmt.Sprintf("%dx%d", sz.w, sz.h), func(t *testing.T) {
			v := NewOrbitView(chipTestTheme())
			cCols, cRows := canvasDimsFor(sz.w, sz.h)
			navballReserved := navballPanelH + 1
			v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, navballReserved, 1, 2, realisticChipSet(true))
			assertNoChipRectOverlaps(t, v.chipRects)
		})
	}
}

// TestGracefulShrinkReproducesForcedNodesVsOrbitCollision (test
// requirement (a)): reproduces visual-clarity-04.txt — a force-shown
// (burning) NODES chip stacking bottom-right above a navball that
// dominates the canvas, alongside the Core ORBIT chip top-right. Before
// #422 this NODES chip clamped onto the canvas at row 0 and painted over
// ORBIT's border (composeChips' old last-resort clamp); the fix is that
// neither the ORBIT rect nor the NODES rect (whichever survive Compact/
// drop) may ever overlap.
func TestGracefulShrinkReproducesForcedNodesVsOrbitCollision(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const termW, termH = 104, 25 // canvas rows = 22, matching the dump's geometry
	cCols, cRows := canvasDimsFor(termW, termH)
	navballReserved := navballPanelH + 1
	v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, navballReserved, 1, 2, realisticChipSet(true))
	assertNoChipRectOverlaps(t, v.chipRects)
}

// TestGracefulShrinkReproducesStagesVsProximityCollision (test
// requirement (a)): reproduces visual-clarity-16.txt — in proximity
// view, the bottom-left STAGES stack (growing up) and a top-left chip
// (growing down) shared no budget under the old per-corner scheme, so
// STAGES could paint over the earlier chip's leading digits (`10661 km`
// read `661 km`). One shared left-side budget must keep them apart.
func TestGracefulShrinkReproducesStagesVsProximityCollision(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	cCols, cRows := canvasDimsFor(104, 24)
	chips := []builtChip{
		{corner: cornerTopLeft, priority: chipPriorityCore,
			lines:   []string{"VESSEL", "  Saturn V-2", "  primary:   Earth", "  velocity:  0.41 km/s", "PROPELLANT", "  fuel:      100% (2160000 kg)", "  mass:      2901847 kg", "  Δv budget: 3518 m/s", "  throttle:  100%"},
			compact: []string{"VESSEL  Saturn V-2", "  fuel: 100%  Δv: 3518 m/s"}},
		{id: "", corner: cornerTopLeft,
			lines:   []string{"PROXIMITY  Saturn V-1", "  range:    10661 km", "  |v_rel|:  5518.77 m/s", "  closing:  +3639.71 m/s"},
			compact: []string{"PROXIMITY  Saturn V-1", "  range: 10661 km"}},
		{id: settings.ChipStages, corner: cornerBottomLeft,
			lines:   []string{"STAGES", "  ●●●", "  ▸ S-IC (1/3)"},
			compact: []string{"STAGES  ●●●"}},
	}
	v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, 0, 1, 2, chips)
	assertNoChipRectOverlaps(t, v.chipRects)
}

// TestLayoutChipsBySideCompactsBeforeDropping (test requirement (c),
// shrink half): a side that overflows in Full form but fits once its
// Normal-priority chip shrinks to Compact must land on Compact, not
// drop — the chip's Compact-only content renders and its Full-only
// content does not, and no Hidden Stub appears.
func TestLayoutChipsBySideCompactsBeforeDropping(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const cCols, cRows = 40, 12
	chips := []builtChip{
		{corner: cornerTopLeft, priority: chipPriorityCore, lines: []string{"CORE", "core-a", "core-b", "core-c"}},
		{corner: cornerTopLeft, lines: []string{"WIDE", "full-only-row-1", "full-only-row-2", "full-only-row-3"},
			compact: []string{"WIDE", "compact-row"}},
	}
	out := v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, 0, 0, 0, chips)
	if !strings.Contains(out, "compact-row") {
		t.Errorf("expected the Normal chip to shrink to Compact and render its compact row:\n%s", out)
	}
	if strings.Contains(out, "full-only-row-1") {
		t.Errorf("chip rendered Full when Compact alone already fit the budget:\n%s", out)
	}
	if strings.Contains(out, "hidden") {
		t.Errorf("a chip dropped even though shrinking to Compact was enough to fit:\n%s", out)
	}
	assertNoChipRectOverlaps(t, v.chipRects)
}

// TestLayoutChipsBySideDropsOnlyAfterEverythingIsCompact (test
// requirement (c), drop half): when a side still overflows after every
// chip on it is Compact, the lowest-priority chip drops behind a Hidden
// Stub — never before every chip has already shrunk.
func TestLayoutChipsBySideDropsOnlyAfterEverythingIsCompact(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	const cCols, cRows = 40, 8 // budget = cRows-1 = 7: too tight even fully Compact
	chips := []builtChip{
		{corner: cornerTopLeft, priority: chipPriorityCore, lines: []string{"CORE", "core-a", "core-b"},
			compact: []string{"CORE", "core-compact"}},
		{corner: cornerTopLeft, lines: []string{"WIDE", "full-a", "full-b"},
			compact: []string{"WIDE", "wide-compact"}},
	}
	forms, stubs := layoutChipsBySide(chips, cRows, 0)
	if forms[0] != chipFormCompact && forms[0] != chipFormFull {
		t.Fatalf("higher-priority CORE chip dropped before the lower-priority chip: forms=%v", forms)
	}
	if forms[1] != chipFormHidden {
		t.Fatalf("lower-priority WIDE chip should have dropped once the side was fully Compact and still overflowing: forms=%v", forms)
	}
	if stubs[sideLeft] == 0 {
		t.Errorf("a chip dropped but no Hidden Stub was reserved for its side")
	}
	out := v.composeChips(blankCanvas(cCols, cRows), cCols, cRows, 0, 0, 0, chips)
	if !strings.Contains(out, "hidden") {
		t.Errorf("dropped chip's stub text missing from the composed frame:\n%s", out)
	}
	assertNoChipRectOverlaps(t, v.chipRects)
}
