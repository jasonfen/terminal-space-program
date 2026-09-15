package screens

import (
	"fmt"
	"math"
	"regexp"
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
		{corner: cornerTopLeft, lines: filler(6), priority: chipPriorityCore}, // VESSEL-sized
		{corner: cornerTopLeft, lines: filler(3)},                             // MISSION-sized filler
		{corner: cornerTopLeft, lines: filler(2)},                             // SESSION-sized filler
		{corner: cornerTopLeft, lines: filler(2)},                             // TIME LOCK-sized filler
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

// TestStagesBoxMultiStagePips migrated from the retired buildStagesChip
// onto buildStagesBox (ADR 0051): the single-stage "nil" case is
// superseded by orbit_box_stages_mission_test.go's own
// TestStagesBoxRendersSingleStageVessel (decision 5: every vessel gets a
// STAGES box now, single-stage included), so only the multi-stage pip
// markers and active-stage index survive here.
func TestStagesBoxMultiStagePips(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}

	c.Stages = []spacecraft.Stage{
		{Name: "S-IC", FuelMass: 0, FuelCapacity: 100}, // dry → ○
		{Name: "S-II", FuelMass: 50, FuelCapacity: 100},
		{Name: "S-IVB", FuelMass: 80, FuelCapacity: 100},
	}
	joined := strings.Join(v.buildStagesBox(w), "\n")
	if !strings.Contains(joined, "STAGES") {
		t.Errorf("box missing header:\n%s", joined)
	}
	if !strings.Contains(joined, "○") || !strings.Contains(joined, "●") {
		t.Errorf("box pips missing filled/hollow markers:\n%s", joined)
	}
	if !strings.Contains(joined, "(1/3)") {
		t.Errorf("box missing active-stage index (1/3):\n%s", joined)
	}
}

// (TestBuildNodesChipSummary retired: the retired NODES chip's overflow
// count and click-affordance marker are covered live on ENGINE's node
// row by TestEngineNodeRowOverflowCountWhenMultipleNodesQueued (this
// file) and TestEngineBoxNodeRowQueuedNode (orbit_box_engine_test.go);
// the "nil when no nodes" case is decision 2's dash cell instead, pinned
// by TestEngineBoxNodeRowDashWithNoCraftActivity.)

// (TestBuildNodesChipMarksOverBudgetNode retired: it pinned the WORDED
// over-budget marker ("exceeds budget by Nm/s") that re-grill Q4
// explicitly replaces with a bare "⚠" glyph on ENGINE's node row, see
// TestEngineBoxNodeRowOverBudgetIsBareGlyph (orbit_box_engine_test.go),
// which sabotage-checks that the words specifically do NOT reappear.)

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

// The negative-zero-snap coverage this used to pin (TestNzeroSnapsNegativeZero,
// pre-ADR-0049) now lives on internal/tui/readout's own TestNzero: the
// local `nzero` helper is deleted, every screens/ formatter routes through
// readout.Nzero instead (ADR 0049 stage A2).

// TestDeclutterHidesChipsKeepsColumn: F2 declutter suppresses every Chip
// (here the eight ADR 0051 instrument boxes, e.g. GUIDANCE, which folded
// in the retired ATTITUDE chip's nav:/hold: rows). 2a gates all eight
// boxes on plain declutter (Settings per-box ids and F2's lit-engine
// exception are slice 2b's, decision 16), so unlike the pre-ADR-0051
// pinned VESSEL chip, F2 now hides EVERY instrument box together,
// including ENGINE/PROPELLANT, that exception lands with 2b.
func TestDeclutterHidesChipsKeepsColumn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "GUIDANCE") {
		t.Fatalf("expected GUIDANCE box with declutter off:\n%s", out)
	}
	if !strings.Contains(out, "ENGINE") {
		t.Fatalf("expected ENGINE box with declutter off")
	}

	v.SetDeclutter(true)
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "GUIDANCE") {
		t.Errorf("declutter on: GUIDANCE box should be hidden:\n%s", out)
	}
	if strings.Contains(out, "ENGINE") {
		t.Errorf("declutter on (2a): ENGINE box should be hidden too — the lit-engine exception (decision 16) is slice 2b's, not wired yet:\n%s", out)
	}

	v.SetDeclutter(false)
	out = v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "GUIDANCE") {
		t.Errorf("declutter off again: GUIDANCE box should return:\n%s", out)
	}
}

// TestNavigationBoxAlwaysOnAndEngineShowsLiveBurn: the NAVIGATION box is
// non-toggleable in 2a (no Settings id exists for it yet, decision 16's
// per-box ids are slice 2b's) so it renders with every Settings chip
// disabled. A live burn shows on ENGINE's node row (folded in from the
// retired NODES chip). 2a does not yet implement decision 16's
// lit-engine declutter exception (slice 2b), so F2 hides ENGINE (and the
// live-burn readout on it) along with everything else, a documented
// interim gap, not the final contract.
func TestNavigationBoxAlwaysOnAndEngineShowsLiveBurn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// Disable every toggleable Chip; the instrument boxes have no
	// Settings id yet (2b's job) so they must persist regardless.
	s := settings.Default()
	for _, c := range settings.AllChips {
		s.SetChip(c, false)
	}
	v.SetSettings(s)

	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "NAVIGATION") {
		t.Errorf("NAVIGATION must render with all chips disabled (no Settings id yet):\n%s", out)
	}

	// Light an active burn → the firing head force-shows on ENGINE's node
	// row even with every Settings chip disabled.
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
	if !strings.Contains(out, "ENGINE") || !strings.Contains(out, "120 m/s") {
		t.Errorf("a live burn must show on ENGINE's node row with all chips disabled:\n%s", out)
	}

	// F2 declutter clears every instrument box, ENGINE included, the
	// lit-engine exception (decision 16) is slice 2b's, not this slice's.
	v.SetDeclutter(true)
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "NAVIGATION") || strings.Contains(out, "ENGINE") {
		t.Errorf("declutter (2a) must hide every instrument box uniformly, including ENGINE mid-burn:\n%s", out)
	}

	// Cut the burn, declutter off again → ENGINE's node row returns to a
	// dash.
	c.ActiveBurn = nil
	v.SetDeclutter(false)
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "120 m/s") {
		t.Errorf("burn readout lingered after the burn ended:\n%s", out)
	}
}

// TestEngineNodeRowOverflowCountWhenMultipleNodesQueued, #293's
// staleness rationale, now on ENGINE's node row (the retired NODES chip
// folded in here, ADR 0051 decision 1): every node after the first fires
// against an orbit it was never computed for, so 2+ queued nodes on the
// active craft carry the "(+N more → [m])" overflow annotation; a single
// queued node does not.
func TestEngineNodeRowOverflowCountWhenMultipleNodesQueued(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("expected an active craft")
	}
	c.Nodes = []spacecraft.ManeuverNode{
		{Mode: spacecraft.BurnPrograde, DV: 42, TriggerTime: w.Clock.SimTime.Add(time.Minute)},
	}
	lines := v.buildEngineBox(w)
	if strings.Contains(lines[3], "more") {
		t.Errorf("a single queued node must not carry an overflow count:\n%s", lines[3])
	}

	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode: spacecraft.BurnRetrograde, DV: 7, TriggerTime: w.Clock.SimTime.Add(2 * time.Minute),
	})
	lines = v.buildEngineBox(w)
	if !strings.Contains(lines[3], "+1 more") {
		t.Errorf("2 queued nodes must show a +1 more overflow count on ENGINE's node row:\n%s", lines[3])
	}
}

// (TestNodesChipForceShowIsPerCraftNotFleetWide retired: it guarded the
// retired NODES chip's "force past declutter" behaviour for a fleet-wide
// staleness hazard. ENGINE's node row (ADR 0051 decision 1) has no
// force-show exception in 2a at all, TestNavigationBoxAlwaysOnAndEngine-
// ShowsLiveBurn (this file) confirms F2 declutter now hides ENGINE
// uniformly with everything else, the lit-engine exception being slice
// 2b's (decision 16), so the fleet-wide-vs-per-craft distinction this
// test drew no longer has a force-show path to guard.)

// TestNodesChipOverflowCountIsPerCraft (#333): the "(+N more)" overflow
// annotation must count the SAME craft's own remaining queue that the
// "next" node line above it names — folding in another craft's
// unrelated nodes misdescribes whose queue is actually stale. Migrated
// onto buildEngineBox (ADR 0051): the box is scoped to w.ActiveCraft()
// by construction now, so this also pins that another craft's queue can
// never leak in via a future refactor.
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

	lines := v.buildEngineBox(w)
	if strings.Contains(lines[3], "more") {
		t.Errorf("active craft has a single node; overflow count leaked another craft's queue:\n%s", lines[3])
	}
}

// TestNavigationBoxShowsDirectionIndicator, issue #63: NAVIGATION
// carries an explicit prograde/retrograde orbit-direction readout (dir:)
// so a genuine reversal is never confused with a projection/shading
// artifact. Default LEO reads prograde; flipping the velocity (h sign
// reverses → inclination crosses 90°) flips the readout to retrograde.
// Migrated from the retired buildOrbitMetricsChip onto buildNavigationBox
// (ADR 0051); the label shortens from "direction:" to "dir:".
func TestNavigationBoxShowsDirectionIndicator(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	joined := strings.Join(v.buildNavigationBox(w), "\n")
	if !strings.Contains(joined, "dir:") {
		t.Fatalf("NAVIGATION box missing the dir: readout:\n%s", joined)
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
	joined = strings.Join(v.buildNavigationBox(w), "\n")
	if !strings.Contains(joined, "retrograde") {
		t.Errorf("reversed orbit should read retrograde:\n%s", joined)
	}
}

// TestNavigationBoxShowsEccentricity, #426 (CONTEXT.md Chip entry):
// NAVIGATION always carries an `e:` row so the three eccentricity-graded
// challenge rungs have a number on the HUD to check against. Migrated
// from the retired buildOrbitMetricsChip(+Compact) onto buildNavigationBox
// (ADR 0051); the Compact-Form assertion is dropped since none of the
// fixed instrument boxes has a Compact Form (decision 2: never resize).
func TestNavigationBoxShowsEccentricity(t *testing.T) {
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

	joined := strings.Join(v.buildNavigationBox(w), "\n")
	// \be: (not the unanchored `e:\s+[0-9]`) so this doesn't false-positive
	// on "Pe:" or "range:", which also end in "e:" followed by a numeric
	// value: an unanchored version of this regex would pass here even if
	// NAVIGATION's own e: cell were sabotaged to a dash, since Pe: alone
	// satisfies it (caught live: sabotaging e: to "—" left this green
	// under the unanchored form).
	if !regexp.MustCompile(`\be:\s+[0-9]`).MatchString(joined) {
		t.Fatalf("NAVIGATION box missing the e: row with a live value:\n%s", joined)
	}
}

// TestNavigationBoxStaysLiveWithPlantedNode, issue #63 follow-up,
// retired-chip-era regression: the projected post-burn orbit used to be
// its own PROJECTED ORBIT chip stacked beneath the always-on ORBIT chip,
// so planting a node showed the current and projected orbits
// simultaneously instead of the projection replacing the live readout.
// ADR 0051 retires the PROJECTED ORBIT chip from assembleChips entirely
// (its content, the plan arrows and the plan: row's world/node-angle
// text, moves to slice 3, not this slice; buildNavigationBox's own doc
// comment confirms plan: is a permanent dash until then); the live orbit
// now renders on NAVIGATION instead of the retired ORBIT chip, with no
// second projected panel to compare against in 2a. This end-to-end check
// is what survives: NAVIGATION's live orbit rows must stay present and
// live once a node is planted, not blank out or get replaced.
func TestNavigationBoxStaysLiveWithPlantedNode(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	before := strings.Join(v.buildNavigationBox(w), "\n")
	if !strings.Contains(before, "altitude:") {
		t.Fatalf("expected NAVIGATION's live altitude: row with no node planted:\n%s", before)
	}

	// Plant a resolved prograde node.
	w.PlanNode(sim.ManeuverNode{
		TriggerTime: w.Clock.SimTime.Add(30 * time.Minute),
		Mode:        spacecraft.BurnPrograde,
		DV:          100,
		PrimaryID:   w.ActiveCraft().Primary.ID,
	})
	after := strings.Join(v.buildNavigationBox(w), "\n")
	if !strings.Contains(after, "altitude:") {
		t.Errorf("NAVIGATION's live altitude: row must survive a planted node (no projection panel replaces it in 2a):\n%s", after)
	}

	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "NAVIGATION") {
		t.Errorf("a rendered frame with a planted node must still show NAVIGATION's live orbit:\n%s", out)
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

// TestPropellantBoxCoreOnly migrated from the retired buildVesselChip
// onto buildPropellantBox (ADR 0051): the core fuel/mass/Δv telemetry
// this pinned now lives on PROPELLANT; orbit shape (Ap:/Pe:) lives on
// NAVIGATION only, so PROPELLANT must never carry it.
func TestPropellantBoxCoreOnly(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := strings.Join(v.buildPropellantBox(w), "\n")
	if !strings.Contains(out, "PROPELLANT") {
		t.Errorf("box missing header:\n%s", out)
	}
	if !strings.Contains(out, "mass:") || !strings.Contains(out, "Δv:") {
		t.Errorf("box missing core telemetry rows:\n%s", out)
	}
	// Orbit shape lives on NAVIGATION, PROPELLANT must not carry Ap:/Pe:
	// rows.
	if strings.Contains(out, "Ap:") || strings.Contains(out, "Pe:") {
		t.Errorf("PROPELLANT box carries orbit-shape rows (should live on NAVIGATION only):\n%s", out)
	}
}

// TestPropellantBoxMassesRideTheLadder pins F7 (gate review): masses
// were never migrated to readout.Mass, so a Saturn V's fuel/mass/
// monoprop rows still printed raw kilograms ("2901847 kg") straight
// through decision 3's contract, the exact number the ADR's own Context
// section names as one of the original findings. A spawned Saturn V's
// fuel and total mass are both well past the 1000 kg kg->t rung, so a
// surviving raw-kg reading fails this immediately. Migrated from the
// retired buildVesselChip onto buildPropellantBox (ADR 0051); exact
// column spacing isn't pinned here (that's fix #1's job), only the
// formatted VALUES.
func TestPropellantBoxMassesRideTheLadder(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	out := strings.Join(v.buildPropellantBox(w), "\n")
	for _, want := range []string{"100% (2160 t)", "2902 t", "11.85 t"} {
		if !strings.Contains(out, want) {
			t.Errorf("PROPELLANT box missing %q (masses should ride the kg/t ladder):\n%s", want, out)
		}
	}
	if strings.Contains(out, " kg") {
		t.Errorf("PROPELLANT box still prints a raw kilogram reading:\n%s", out)
	}
}

// TestPropellantBoxDeltaVPairShowsStageOverVehicle pins F12 (gate
// review): decision 7's "stage / vehicle" two-number Δv row had no
// call-site test: only readout's own DeltaVPair unit tests covered the
// string shape, not that a real multi-stage vessel's box actually
// reaches it instead of printing the active stage's Δv alone. A spawned
// Saturn V has three stages, so its active stage's remaining Δv and the
// whole stack's total are provably different numbers, sharing one
// trailing unit. Migrated from the retired buildVesselChip(+Compact)
// onto buildPropellantBox (ADR 0051); the Compact-Form assertion is
// dropped (no Compact Form on any fixed instrument box, decision 2).
func TestPropellantBoxDeltaVPairShowsStageOverVehicle(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        sim.DefaultLaunchpadLatitude,
		LongitudeOffset: sim.DefaultLaunchpadLongitudeEast,
	}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	c := w.ActiveCraft()
	if len(c.Stages) <= 1 {
		t.Fatalf("test setup broken: Saturn V should spawn with more than one stage, got %d", len(c.Stages))
	}
	out := strings.Join(v.buildPropellantBox(w), "\n")
	if !strings.Contains(out, "3518 / 18872 m/s") {
		t.Errorf("PROPELLANT box missing the stage/vehicle Δv pair:\n%s", out)
	}
}

// (TestBuildNodesChipMergesActiveBurn retired: it pinned the retired
// NODES chip merging a firing head ABOVE a planted-node summary, both
// visible together. ADR 0051 decision 12 replaces this with a strict
// precedence on ENGINE's single node row (live burn always outranks a
// queued node outright, never shown together); see
// TestEngineBoxNodeRowLiveBurnOutranksQueuedNode (orbit_box_engine_test.go),
// which sabotage-checks that the queued node's own wording does NOT
// appear once a burn is live, the opposite of what this test pinned.)

// (TestEmptySlateSaysSo retired (#310): with no craft at all, the retired
// VESSEL chip stated the situation and offered the way out ("[n]" to
// launch, or the DockGuest owner's name + "[U]" to undock). No live
// instrument box reproduces this messaging, every box just reads a dash
// row when w.ActiveCraft() is nil (decision 2), with no player-facing
// explanation or way out. This is a real coverage/feature gap this
// cleanup surfaces rather than papers over: dock_guest_rider_render_test.go's
// own TestDockGuestRenderIncludesDockedBlock already flags the adjacent
// DOCKED-block risk in its ADR 0051 note ("a rider on a genuinely narrow
// terminal can still lose their only exit route to the new box set,
// worth the maintainer's attention, not silently accepted"); the #310
// empty-slate case is the same shape of gap and is flagged here for the
// same reason, not fixed by this slice.)

// (TestDockGuestVesselChipShowsBadgedFlightData retired (ADR 0038 S4
// part 3): same gap as TestEmptySlateSaysSo above, no live instrument
// box badges a DockGuest stack's ghost-reported flight data (name,
// primary, velocity) the way the retired VESSEL chip did. Flagged, not
// fixed, by this cleanup.)

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

// (TestDockGuestOrbitChipShowsBadgedShape retired (ADR 0038 S4 part 3):
// same gap as TestDockGuestVesselChipShowsBadgedFlightData above, no
// live instrument box renders the DockGuest stack's ghost-reported orbit
// shape while riding as a guest (!CraftVisibleHere); buildNavigationBox
// only ever reads w.ActiveCraft(), with no ghost fallback. Flagged, not
// fixed, by this cleanup. dockGuestStackGhostWorld itself stays live,
// dock_guest_rider_render_test.go's own tests still use it.)

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
			lines:   []string{"VESSEL", "  S-IVB-1", "  primary:   Earth", "  velocity:  7.50 km/s", "PROPELLANT", "  fuel:      89% (35.77 t)", "  mass:      47.49 t", "  Δv:        5777 m/s", "  throttle:  100%"},
			compact: []string{"VESSEL  S-IVB-1", "  fuel: 89% (35.77 t)  Δv: 5777 m/s"}},
		{id: settings.ChipFrameTransition, corner: cornerTopLeft,
			// A future frame transition renders T- (readout.Countdown's
			// sign convention, decision 2): "T+5d4h" here pinned the
			// exact inversion this PR exists to remove (F13).
			lines: []string{"FRAME TRANSITION", "  Earth → Moon", "  at T-5d04h  (node #3)"}},
		{id: settings.ChipMissions, corner: cornerTopLeft,
			lines:   []string{"MISSION  Flight School: Plan a Burn", "  ▸ Warp to the node  0/1", "    Press [G] to auto-warp to the burn."},
			compact: []string{"MISSION  Flight School: Plan a Burn", "  ▸ Warp to the node  0/1"}},
		{corner: cornerTopRight, priority: chipPriorityCore,
			// #426: the Full form grew an `e:` row (eccentricity, always-on,
			// full form only — the Compact Form stays the Ap/Pe strip below).
			lines:   []string{"ORBIT", "  altitude:  500.0 km", "  Ap:        500.0 km", "  apo:       T-47m", "  Pe:        498.2 km", "  peri:      T-12m", "  period:    1h34m28s", "  incl:      0.00°", "  direction: prograde", "  e:         0.0004"},
			compact: []string{"ORBIT", "  Ap: 500.0 km  Pe: 498.2 km"}},
		{id: settings.ChipTarget, corner: cornerTopRight,
			lines:   []string{"TARGET", "  body:     Moon", "  Δincl:    19.44°", "  range:    371.6 Mm", "  TCA:      T-4h43m"},
			compact: []string{"TARGET  Moon", "  range: 371.6 Mm"}},
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
			lines:   []string{"VESSEL", "  Saturn V-2", "  primary:   Earth", "  velocity:  0.41 km/s", "PROPELLANT", "  fuel:      100% (2160000 kg)", "  mass:      2901847 kg", "  Δv:        3518 m/s", "  throttle:  100%"},
			compact: []string{"VESSEL  Saturn V-2", "  fuel: 100%  Δv: 3518 m/s"}},
		{id: "", corner: cornerTopLeft,
			lines:   []string{"PROXIMITY  Saturn V-1", "  range:    10661 km", "  rel speed: 5518.77 m/s", "  closing:  +3639.71 m/s"},
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
