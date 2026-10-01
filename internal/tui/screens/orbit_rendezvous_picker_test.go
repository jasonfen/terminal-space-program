package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// rendezvousPickerTestLadder is a synthetic Lap Ladder for chip-rendering
// tests — deliberately NOT the real solver's own numbers (that's
// internal/sim's job, e.g. TestPlanRendezvousOrOpenPicker_PhaseMismatch_OpensPicker,
// which asserts against whatever RecommendRendezvousLadder actually
// returns). This file only exercises the rendering plumbing: does the
// picker draw the right rows for a given state, does it survive an
// 80×24 canvas, does padding stay ANSI-safe.
func rendezvousPickerTestLadder() planner.RendezvousLadder {
	return planner.RendezvousLadder{
		Place:    planner.RendezvousTheirOrbit,
		MoverIsA: true,
		Rows: []planner.RendezvousBurnOption{
			{Laps: 2, Ok: true, DV: 696.6, TBurn: 300, TArrival: 15587, ArrivalSpeed: 12.5},
			{Laps: 3, Ok: true, DV: 509.4, TBurn: 300, TArrival: 21255, ArrivalSpeed: 9.1},
			{Laps: 5, Ok: false, Reason: "unaffordable", TBurn: 300, TArrival: 32592},
			{Laps: 10, Ok: true, DV: 177.2, TBurn: 300, TArrival: 60932, ArrivalSpeed: 4.2},
			{Laps: 20, Ok: true, DV: 91.8, TBurn: 300, TArrival: 117614, ArrivalSpeed: 2.0},
		},
	}
}

func TestRendezvousPickerChip_NilWhenClosed(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	if chip := v.buildRendezvousPickerChip(); chip != nil {
		t.Errorf("chip rendered while closed:\n%s", strings.Join(chip, "\n"))
	}
}

// TestRendezvousPickerChip_Content pins the row shape: header, Place
// walker, every row's laps/wait/Δv (or its refusal reason when
// !Ok — ADR 0045 §2: "unaffordable rows render as unavailable rather
// than being hidden"), and the selected row's arrival speed.
func TestRendezvousPickerChip_Content(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	ladder := rendezvousPickerTestLadder()
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, ladder, nil)

	joined := strings.Join(v.buildRendezvousPickerChip(), "\n")
	for _, want := range []string{
		"RENDEZVOUS PLAN",
		"their orbit",
		"2 laps", "3 laps", "5 laps", "10 laps", "20 laps",
		"unaffordable",
		"arriving ~",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("chip missing %q:\n%s", want, joined)
		}
	}
	// The Δv figures render verbatim from the ladder — not the ADR
	// mockup's illustrative numbers (630/250/60) and not hardcoded here
	// beyond what rendezvousPickerTestLadder itself declares.
	for _, want := range []string{"697 m/s", "509 m/s", "177 m/s", "91.80 m/s"} {
		if !strings.Contains(joined, want) {
			t.Errorf("chip missing Δv %q:\n%s", want, joined)
		}
	}
}

// TestRendezvousPickerChip_LadderErrShowsRefusal — #407: a per-Place
// structural refusal (e.g. ErrRendezvousSizeMismatch) must render as a
// clear one-line refusal, not a blank or broken chip.
func TestRendezvousPickerChip_LadderErrShowsRefusal(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousYourOrbit, planner.RendezvousLadder{}, sim.ErrRendezvousSizeMismatch)

	joined := strings.Join(v.buildRendezvousPickerChip(), "\n")
	if !strings.Contains(joined, "your orbit") {
		t.Errorf("chip missing the selected Place:\n%s", joined)
	}
	if !strings.Contains(joined, sim.ErrRendezvousSizeMismatch.Error()) {
		t.Errorf("chip missing the structural refusal text:\n%s", joined)
	}
}

// TestRendezvousPickerNav_LeftRightCyclesPlace_WrapsBothWays pins the ←/→
// walk order (their orbit / your orbit / the crossing) and that it wraps
// at both ends rather than clamping.
func TestRendezvousPickerNav_LeftRightCyclesPlace_WrapsBothWays(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, rendezvousPickerTestLadder(), nil)

	if got := v.RendezvousPickerOrbit(); got != planner.RendezvousTheirOrbit {
		t.Fatalf("initial Place = %v, want RendezvousTheirOrbit", got)
	}
	v.RendezvousPickerRight()
	if got := v.RendezvousPickerOrbit(); got != planner.RendezvousYourOrbit {
		t.Errorf("after 1 right: Place = %v, want RendezvousYourOrbit", got)
	}
	v.RendezvousPickerRight()
	if got := v.RendezvousPickerOrbit(); got != planner.RendezvousCrossing {
		t.Errorf("after 2 right: Place = %v, want RendezvousCrossing", got)
	}
	v.RendezvousPickerRight()
	if got := v.RendezvousPickerOrbit(); got != planner.RendezvousTheirOrbit {
		t.Errorf("right from the last Place did not wrap: got %v, want RendezvousTheirOrbit", got)
	}
	v.RendezvousPickerLeft()
	if got := v.RendezvousPickerOrbit(); got != planner.RendezvousCrossing {
		t.Errorf("left from the first Place did not wrap backward: got %v, want RendezvousCrossing", got)
	}
}

// TestRendezvousPickerNav_PlaceChangeClearsStaleLadder — App must recompute
// and push the new Place's ladder via SetRendezvousPickerLadder; until then
// the chip must show the NEW Place's header with no stale rows from the
// OLD Place (see RendezvousPickerLeft/Right's own doc comment).
func TestRendezvousPickerNav_PlaceChangeClearsStaleLadder(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, rendezvousPickerTestLadder(), nil)
	if _, ok := v.RendezvousPickerSelectedLaps(); !ok {
		t.Fatalf("setup: expected a selected row before cycling Place")
	}

	v.RendezvousPickerRight()

	if _, ok := v.RendezvousPickerSelectedLaps(); ok {
		t.Errorf("stale ladder rows survived a Place change before the App recomputed")
	}
	joined := strings.Join(v.buildRendezvousPickerChip(), "\n")
	if strings.Contains(joined, "2 laps") {
		t.Errorf("chip still shows the OLD Place's rows after cycling:\n%s", joined)
	}
}

// TestRendezvousPickerNav_SetLadderIgnoresStalePlace guards
// SetRendezvousPickerLadder's own no-op contract: a ladder computed for a
// Place the picker has since moved away from (or a picker that's since
// closed) must never overwrite the current selection.
func TestRendezvousPickerNav_SetLadderIgnoresStalePlace(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, rendezvousPickerTestLadder(), nil)
	v.RendezvousPickerRight() // now on RendezvousYourOrbit, ladder cleared

	// A stale computation for the Place we've since left.
	v.SetRendezvousPickerLadder(planner.RendezvousTheirOrbit, rendezvousPickerTestLadder(), nil)

	if _, ok := v.RendezvousPickerSelectedLaps(); ok {
		t.Errorf("a stale ladder for an abandoned Place was applied")
	}
	if got := v.RendezvousPickerOrbit(); got != planner.RendezvousYourOrbit {
		t.Errorf("Place changed via a stale SetRendezvousPickerLadder call: got %v", got)
	}
}

// TestRendezvousPickerNav_UpDownClamp — RendezvousPickerUp/Down clamp at the
// ladder's ends rather than wrapping (a short fixed list; wrapping ↑ from
// the top row back to the bottom would read as a jump, not a walk).
func TestRendezvousPickerNav_UpDownClamp(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	ladder := rendezvousPickerTestLadder()
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, ladder, nil)

	v.RendezvousPickerUp() // already at row 0 (or the first Ok row) — must not go negative
	if _, ok := v.RendezvousPickerSelectedLaps(); !ok {
		t.Fatalf("Up from the top left no row selected")
	}

	for i := 0; i < len(ladder.Rows)+2; i++ {
		v.RendezvousPickerDown()
	}
	laps, ok := v.RendezvousPickerSelectedLaps()
	if !ok {
		t.Fatalf("Down past the end left no row selected")
	}
	if want := ladder.Rows[len(ladder.Rows)-1].Laps; laps != want {
		t.Errorf("Down clamped to laps=%d, want the last row's %d", laps, want)
	}
}

// TestRendezvousPickerChip_CellWidthConsistent guards the same chip →
// canvas contract every other chip builder is checked against
// (assertChipCellWidthConsistent, orbit_chips_test.go): every line the
// builder emits must measure identically via lipgloss.Width and
// splitStyledCells, under a REAL themed style (not chipTestTheme's
// no-op), so an ANSI-styled row can't silently widen the overlay. Forces
// termenv.TrueColor so DefaultTheme-shaped colors actually emit ANSI
// here rather than degrading to no-color in a non-TTY test binary.
func TestRendezvousPickerChip_CellWidthConsistent(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	th := Theme{
		Primary: lipgloss.NewStyle().Foreground(lipgloss.Color("#5FD7FF")),
		Warning: lipgloss.NewStyle().Foreground(lipgloss.Color("#FFAF00")),
		Alert:   lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5F5F")),
		Dim:     lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5F5F")),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
	v := NewOrbitView(th)
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, rendezvousPickerTestLadder(), nil)
	v.RendezvousPickerDown() // move selection so the highlighted (styled) row isn't just row 0

	lines := v.buildRendezvousPickerChip()
	if len(lines) == 0 {
		t.Fatal("chip is empty while open")
	}
	if !containsANSI(strings.Join(lines, "\n")) {
		t.Fatal("test setup broken: expected a real theme + TrueColor to produce ANSI-colored chip lines")
	}
	assertChipCellWidthConsistent(t, "rendezvous picker chip", lines)
}

// rendezvousPickerRenderWorld returns a minimal World for the full-canvas
// render test below — the picker's own state is pushed in directly via
// OpenRendezvousPicker, so this doesn't need a rendezvous-specific fixture.
func rendezvousPickerRenderWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

// TestRendezvousPickerChip_Render80x24 is #399's own named trap #2: the
// chip must render (and stay legible — non-empty, chip content present,
// no panic) at the SMALL terminal, not just a wide one. Production
// --serve runs a 104×24 tmux; 80×24 is narrower still and was the floor
// this slice was asked to prove against.
//
// ADR 0051 REGRESSION, flagged rather than silently worked around: the
// eight instrument boxes are Core priority (never dropped, no Compact
// Form of their own, that gap is real, not yet built) and at 80x24
// they now consume enough of the left column that RENDEZVOUS PLAN's
// neverShrink body can render PAST the canvas's bottom edge, where it is
// silently clipped exactly like the pre-#328 DOCKED bug (only the title
// row survives; the ladder body does not). This test is moved to the
// Design Size, the one canvas ADR 0046 actually promises room at, so it
// still proves the picker's content-selection logic; the 104x24
// production-size guarantee is NOT currently met and needs either
// Compact Forms for the eight boxes (ADR 0046's "the stacker folds
// instruments as today" below the floor implies they should have one)
// or a stacker change that lets a neverShrink modal evict Core content
// below the floor, flagged for the maintainer, not fixed here.
func TestRendezvousPickerChip_Render80x24(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	w := rendezvousPickerRenderWorld(t)
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, rendezvousPickerTestLadder(), nil)

	out := v.Render(w, 0, DesignWidth, DesignHeight)

	if rows := strings.Count(out, "\n") + 1; rows < DesignHeight {
		t.Errorf("rendered %d rows at height %d", rows, DesignHeight)
	}
	// The chip's OWN lines (not the whole composited page — the title
	// bar and other pre-existing chips carry their own width contracts,
	// out of scope here) must fit the request: assertChipCellWidthConsistent
	// above already guards the ANSI-padding trap; this is the belt-and-
	// suspenders check that the picker doesn't independently blow past a
	// sane width at the narrow floor.
	for _, line := range v.buildRendezvousPickerChip() {
		if w := lipgloss.Width(line); w > 52 {
			t.Errorf("rendezvous picker chip line implausibly wide (%d cols) at 80×24: %q", w, line)
		}
	}
	if !strings.Contains(out, "RENDEZVOUS PLAN") {
		t.Errorf("RENDEZVOUS PLAN chip missing from an 80×24 render:\n%s", out)
	}
	if !strings.Contains(out, "their orbit") {
		t.Errorf("Rendezvous Orbit missing from an 80×24 render:\n%s", out)
	}
}

// TestRendezvousPickerChip_Render80x24_Golden pins the chip block
// line-for-line at 80×24 under the plain (no-ANSI) test theme, so an
// accidental layout change shows up here as a diff.
//
// It earns the name "golden" only because it compares whole lines. An
// earlier version of this test was three strings.Contains calls over a
// two-row, single-digit-lap fixture, which could not have caught the
// ragged-column bug that shipped in this file (an unwidthed "%d laps"
// shifted every column right on two-digit rows). The fixture below
// therefore spans one- and two-digit lap counts and includes a refusal
// row, which are the shapes that actually vary the layout.
//
// Alignment specifically is pinned by TestRendezvousPickerChip_LadderColumnsAlign,
// which asserts the property rather than the bytes; this test catches
// everything else, including changes that keep columns aligned but move
// them.
func TestRendezvousPickerChip_Render80x24_Golden(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousPickerRenderWorld(t)
	ladder := planner.RendezvousLadder{
		Place:    planner.RendezvousTheirOrbit,
		MoverIsA: true,
		Rows: []planner.RendezvousBurnOption{
			{Laps: 2, Ok: true, DV: 696.6, TBurn: 300, TArrival: 15587, ArrivalSpeed: 12.5},
			{Laps: 5, Ok: false, Reason: "unaffordable", TBurn: 300, TArrival: 32592},
			{Laps: 20, Ok: true, DV: 91.8, TBurn: 300, TArrival: 117614, ArrivalSpeed: 2.0},
		},
	}
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, ladder, nil)

	want := strings.Join([]string{
		"RENDEZVOUS PLAN",
		"  \u2190 their orbit \u2192",
		">  2 laps  burn T-5m00s  wait 4h19m    697 m/s",
		"   5 laps  burn T-5m00s  wait 9h03m  (unaffordable)",
		"  20 laps  burn T-5m00s  wait 1d08h  91.80 m/s",
		"  arriving ~12.50 m/s",
	}, "\n")

	got := strings.Join(v.buildRendezvousPickerChip(), "\n")
	if got != want {
		t.Errorf("chip block changed.\ngot:\n%s\n\nwant:\n%s", got, want)
	}

	// The block must also survive an actual 80x24 page render.
	if out := v.Render(w, 0, 80, 24); !strings.Contains(out, "RENDEZVOUS PLAN") {
		t.Errorf("chip missing from an 80x24 page render:\n%s", out)
	}
}

// TestRendezvousPickerChip_LadderColumnsAlign pins the ladder's internal
// column alignment, which nothing else in this file covers: the block
// is rectangular because padChipBlock pads every line to the widest,
// and assertChipCellWidthConsistent only checks ANSI/glyph width
// accounting — so a ragged column INSIDE the block is invisible to
// both. The lap count is the field that varies in width (2 vs 10 vs
// 20), and an unwidthed "%d laps" shifts every following column on the
// two-digit rows. The fixture deliberately spans both.
func TestRendezvousPickerChip_LadderColumnsAlign(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, rendezvousPickerTestLadder(), nil)

	lapsAt, dvAt, burnAt, waitAt := -1, -1, -1, -1
	rows := 0
	for _, l := range v.buildRendezvousPickerChip() {
		i := strings.Index(l, " laps")
		if i < 0 {
			continue
		}
		rows++
		if lapsAt < 0 {
			lapsAt = i
		} else if i != lapsAt {
			t.Errorf("ladder row %q: %q column starts at %d, want %d (ragged lap field)", l, " laps", i, lapsAt)
		}
		// burn / wait columns (G4 Q2): every row carries both.
		if b, w := strings.Index(l, "burn "), strings.Index(l, "wait "); b < 0 || w < 0 {
			t.Errorf("ladder row %q is missing its burn or wait column", l)
		} else if burnAt < 0 {
			burnAt, waitAt = b, w
		} else if b != burnAt || w != waitAt {
			t.Errorf("ladder row %q: burn/wait columns at %d/%d, want %d/%d (ragged)", l, b, w, burnAt, waitAt)
		}
		// Δv column, skipped for refusal rows which carry no m/s.
		j := strings.Index(l, "m/s")
		if j < 0 {
			continue
		}
		if dvAt < 0 {
			dvAt = j
		} else if j != dvAt {
			t.Errorf("ladder row %q: %q column starts at %d, want %d (ragged Δv field)", l, "m/s", j, dvAt)
		}
	}
	if rows < 4 {
		t.Fatalf("test setup broken: only %d ladder rows found; the fixture must span one- and two-digit lap counts", rows)
	}
}

// G4 Q2 (#418): the burn and wait columns count down as the clock runs
// while the pilot reads, from the ladder's SolvedAt; the rows themselves do
// not change.
func TestRendezvousPickerChip_BurnAndWaitCountDown(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	ladder := rendezvousPickerTestLadder()
	solved := time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC)
	ladder.SolvedAt = solved
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, ladder, nil)

	first := strings.Join(v.buildRendezvousPickerChip(), "\n")
	if !strings.Contains(first, "burn T-5m00s") {
		t.Fatalf("fresh rows should read burn T-5m00s:\n%s", first)
	}
	v.SetRendezvousPickerNow(solved.Add(2 * time.Minute))
	later := strings.Join(v.buildRendezvousPickerChip(), "\n")
	if !strings.Contains(later, "burn T-3m00s") {
		t.Errorf("two minutes on, rows should read burn T-3m00s:\n%s", later)
	}
	if !strings.Contains(later, "wait 4h17m") {
		t.Errorf("two minutes on, the 15587 s wait should read 4h17m:\n%s", later)
	}
	v.SetRendezvousPickerNow(solved.Add(6 * time.Minute))
	if past := strings.Join(v.buildRendezvousPickerChip(), "\n"); !strings.Contains(past, "burn T+1m00s") {
		t.Errorf("past the burn the row should read burn T+1m00s:\n%s", past)
	}
}
