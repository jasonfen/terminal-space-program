package screens

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

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

// TestOrbitMetricsAndBurnsAlwaysOn: the ORBIT-metrics readout and an
// active ● BURNS readout are non-toggleable — they render even when every
// Settings Chip is disabled (a player can't permanently hide their current
// orbit or a live burn). F2 declutter still clears them; only the pinned
// VESSEL core survives.
func TestOrbitMetricsAndBurnsAlwaysOn(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	// Disable every toggleable Chip; the always-on readouts must persist.
	s := settings.Default()
	for _, c := range settings.AllChips {
		s.SetChip(c, false)
	}
	v.SetSettings(s)

	out := v.Render(w, 0, 120, 40)
	if !strings.Contains(out, "ORBIT") {
		t.Errorf("ORBIT metrics must render with all chips disabled (non-toggleable):\n%s", out)
	}

	// Light an active burn → ● BURNS must show even with all chips off.
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
	if !strings.Contains(out, "BURNS") {
		t.Errorf("active ● BURNS must render with all chips disabled:\n%s", out)
	}

	// F2 declutter clears both; the pinned VESSEL core survives.
	v.SetDeclutter(true)
	out = v.Render(w, 0, 120, 40)
	if strings.Contains(out, "ORBIT") {
		t.Errorf("declutter must hide the ORBIT metrics chip:\n%s", out)
	}
	if strings.Contains(out, "BURNS") {
		t.Errorf("declutter must hide the ● BURNS chip:\n%s", out)
	}
	if !strings.Contains(out, "VESSEL") {
		t.Errorf("pinned VESSEL core must survive declutter")
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
