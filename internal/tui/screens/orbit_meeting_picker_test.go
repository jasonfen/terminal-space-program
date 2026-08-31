package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// meetingPickerTestLadder is a synthetic Lap Ladder for chip-rendering
// tests — deliberately NOT the real solver's own numbers (that's
// internal/sim's job, e.g. TestPlanRendezvousOrOpenMeeting_PhaseMismatch_OpensPicker,
// which asserts against whatever RecommendMeetingLadder actually
// returns). This file only exercises the rendering plumbing: does the
// picker draw the right rows for a given state, does it survive an
// 80×24 canvas, does padding stay ANSI-safe.
func meetingPickerTestLadder() planner.MeetingLadder {
	return planner.MeetingLadder{
		Place:    planner.MeetingTheirOrbit,
		MoverIsA: true,
		Rows: []planner.MeetingBurnOption{
			{Laps: 2, Ok: true, DV: 696.6, TArrival: 15587, ArrivalSpeed: 12.5},
			{Laps: 3, Ok: true, DV: 509.4, TArrival: 21255, ArrivalSpeed: 9.1},
			{Laps: 5, Ok: false, Reason: "unaffordable", TArrival: 32592},
			{Laps: 10, Ok: true, DV: 177.2, TArrival: 60932, ArrivalSpeed: 4.2},
			{Laps: 20, Ok: true, DV: 91.8, TArrival: 117614, ArrivalSpeed: 2.0},
		},
	}
}

func TestMeetingPickerChip_NilWhenClosed(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	if chip := v.buildMeetingPickerChip(); chip != nil {
		t.Errorf("chip rendered while closed:\n%s", strings.Join(chip, "\n"))
	}
}

// TestMeetingPickerChip_Content pins the row shape: header, Place
// walker, every row's laps/wait/Δv (or its refusal reason when
// !Ok — ADR 0045 §2: "unaffordable rows render as unavailable rather
// than being hidden"), and the selected row's arrival speed.
func TestMeetingPickerChip_Content(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	ladder := meetingPickerTestLadder()
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, ladder, nil)

	joined := strings.Join(v.buildMeetingPickerChip(), "\n")
	for _, want := range []string{
		"MEETING PLAN",
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
	// beyond what meetingPickerTestLadder itself declares.
	for _, want := range []string{"697 m/s", "509 m/s", "177 m/s", "92 m/s"} {
		if !strings.Contains(joined, want) {
			t.Errorf("chip missing Δv %q:\n%s", want, joined)
		}
	}
}

// TestMeetingPickerChip_LadderErrShowsRefusal — #407: a per-Place
// structural refusal (e.g. ErrMeetingSizeMismatch) must render as a
// clear one-line refusal, not a blank or broken chip.
func TestMeetingPickerChip_LadderErrShowsRefusal(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenMeetingPicker(planner.MeetingYourOrbit, planner.MeetingLadder{}, sim.ErrMeetingSizeMismatch)

	joined := strings.Join(v.buildMeetingPickerChip(), "\n")
	if !strings.Contains(joined, "your orbit") {
		t.Errorf("chip missing the selected Place:\n%s", joined)
	}
	if !strings.Contains(joined, sim.ErrMeetingSizeMismatch.Error()) {
		t.Errorf("chip missing the structural refusal text:\n%s", joined)
	}
}

// TestMeetingPickerNav_LeftRightCyclesPlace_WrapsBothWays pins the ←/→
// walk order (their orbit / your orbit / the crossing) and that it wraps
// at both ends rather than clamping.
func TestMeetingPickerNav_LeftRightCyclesPlace_WrapsBothWays(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, meetingPickerTestLadder(), nil)

	if got := v.MeetingPickerPlace(); got != planner.MeetingTheirOrbit {
		t.Fatalf("initial Place = %v, want MeetingTheirOrbit", got)
	}
	v.MeetingPickerRight()
	if got := v.MeetingPickerPlace(); got != planner.MeetingYourOrbit {
		t.Errorf("after 1 right: Place = %v, want MeetingYourOrbit", got)
	}
	v.MeetingPickerRight()
	if got := v.MeetingPickerPlace(); got != planner.MeetingCrossing {
		t.Errorf("after 2 right: Place = %v, want MeetingCrossing", got)
	}
	v.MeetingPickerRight()
	if got := v.MeetingPickerPlace(); got != planner.MeetingTheirOrbit {
		t.Errorf("right from the last Place did not wrap: got %v, want MeetingTheirOrbit", got)
	}
	v.MeetingPickerLeft()
	if got := v.MeetingPickerPlace(); got != planner.MeetingCrossing {
		t.Errorf("left from the first Place did not wrap backward: got %v, want MeetingCrossing", got)
	}
}

// TestMeetingPickerNav_PlaceChangeClearsStaleLadder — App must recompute
// and push the new Place's ladder via SetMeetingPickerLadder; until then
// the chip must show the NEW Place's header with no stale rows from the
// OLD Place (see MeetingPickerLeft/Right's own doc comment).
func TestMeetingPickerNav_PlaceChangeClearsStaleLadder(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, meetingPickerTestLadder(), nil)
	if _, ok := v.MeetingPickerSelectedLaps(); !ok {
		t.Fatalf("setup: expected a selected row before cycling Place")
	}

	v.MeetingPickerRight()

	if _, ok := v.MeetingPickerSelectedLaps(); ok {
		t.Errorf("stale ladder rows survived a Place change before the App recomputed")
	}
	joined := strings.Join(v.buildMeetingPickerChip(), "\n")
	if strings.Contains(joined, "2 laps") {
		t.Errorf("chip still shows the OLD Place's rows after cycling:\n%s", joined)
	}
}

// TestMeetingPickerNav_SetLadderIgnoresStalePlace guards
// SetMeetingPickerLadder's own no-op contract: a ladder computed for a
// Place the picker has since moved away from (or a picker that's since
// closed) must never overwrite the current selection.
func TestMeetingPickerNav_SetLadderIgnoresStalePlace(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, meetingPickerTestLadder(), nil)
	v.MeetingPickerRight() // now on MeetingYourOrbit, ladder cleared

	// A stale computation for the Place we've since left.
	v.SetMeetingPickerLadder(planner.MeetingTheirOrbit, meetingPickerTestLadder(), nil)

	if _, ok := v.MeetingPickerSelectedLaps(); ok {
		t.Errorf("a stale ladder for an abandoned Place was applied")
	}
	if got := v.MeetingPickerPlace(); got != planner.MeetingYourOrbit {
		t.Errorf("Place changed via a stale SetMeetingPickerLadder call: got %v", got)
	}
}

// TestMeetingPickerNav_UpDownClamp — MeetingPickerUp/Down clamp at the
// ladder's ends rather than wrapping (a short fixed list; wrapping ↑ from
// the top row back to the bottom would read as a jump, not a walk).
func TestMeetingPickerNav_UpDownClamp(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	ladder := meetingPickerTestLadder()
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, ladder, nil)

	v.MeetingPickerUp() // already at row 0 (or the first Ok row) — must not go negative
	if _, ok := v.MeetingPickerSelectedLaps(); !ok {
		t.Fatalf("Up from the top left no row selected")
	}

	for i := 0; i < len(ladder.Rows)+2; i++ {
		v.MeetingPickerDown()
	}
	laps, ok := v.MeetingPickerSelectedLaps()
	if !ok {
		t.Fatalf("Down past the end left no row selected")
	}
	if want := ladder.Rows[len(ladder.Rows)-1].Laps; laps != want {
		t.Errorf("Down clamped to laps=%d, want the last row's %d", laps, want)
	}
}

// TestMeetingPickerChip_CellWidthConsistent guards the same chip →
// canvas contract every other chip builder is checked against
// (assertChipCellWidthConsistent, orbit_chips_test.go): every line the
// builder emits must measure identically via lipgloss.Width and
// splitStyledCells, under a REAL themed style (not chipTestTheme's
// no-op), so an ANSI-styled row can't silently widen the overlay. Forces
// termenv.TrueColor so DefaultTheme-shaped colors actually emit ANSI
// here rather than degrading to no-color in a non-TTY test binary.
func TestMeetingPickerChip_CellWidthConsistent(t *testing.T) {
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
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, meetingPickerTestLadder(), nil)
	v.MeetingPickerDown() // move selection so the highlighted (styled) row isn't just row 0

	lines := v.buildMeetingPickerChip()
	if len(lines) == 0 {
		t.Fatal("chip is empty while open")
	}
	if !containsANSI(strings.Join(lines, "\n")) {
		t.Fatal("test setup broken: expected a real theme + TrueColor to produce ANSI-colored chip lines")
	}
	assertChipCellWidthConsistent(t, "meeting picker chip", lines)
}

// meetingPickerRenderWorld returns a minimal World for the full-canvas
// render test below — the picker's own state is pushed in directly via
// OpenMeetingPicker, so this doesn't need a rendezvous-specific fixture.
func meetingPickerRenderWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

// TestMeetingPickerChip_Render80x24 is #399's own named trap #2: the
// chip must render (and stay legible — non-empty, chip content present,
// no panic) at the SMALL terminal, not just a wide one. Production
// --serve runs a 104×24 tmux; 80×24 is narrower still and the floor this
// slice is asked to prove against.
func TestMeetingPickerChip_Render80x24(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := meetingPickerRenderWorld(t)
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, meetingPickerTestLadder(), nil)

	out := v.Render(w, 0, 80, 24)

	if rows := strings.Count(out, "\n") + 1; rows < 24 {
		t.Errorf("rendered %d rows at height 24", rows)
	}
	// The chip's OWN lines (not the whole composited page — the title
	// bar and other pre-existing chips carry their own width contracts,
	// out of scope here) must fit the request: assertChipCellWidthConsistent
	// above already guards the ANSI-padding trap; this is the belt-and-
	// suspenders check that the picker doesn't independently blow past a
	// sane width at the narrow floor.
	for _, line := range v.buildMeetingPickerChip() {
		if w := lipgloss.Width(line); w > 40 {
			t.Errorf("meeting picker chip line implausibly wide (%d cols) at 80×24: %q", w, line)
		}
	}
	if !strings.Contains(out, "MEETING PLAN") {
		t.Errorf("MEETING PLAN chip missing from an 80×24 render:\n%s", out)
	}
	if !strings.Contains(out, "their orbit") {
		t.Errorf("Meeting Place missing from an 80×24 render:\n%s", out)
	}
}

// meetingPickerGoldenRender pins the exact chip block at 80×24 with a
// plain (no-ANSI) theme, so a future accidental layout change shows up
// as a diff here instead of only being caught by the content/width
// checks above.
func TestMeetingPickerChip_Render80x24_Golden(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := meetingPickerRenderWorld(t)
	ladder := planner.MeetingLadder{
		Place:    planner.MeetingTheirOrbit,
		MoverIsA: true,
		Rows: []planner.MeetingBurnOption{
			{Laps: 2, Ok: true, DV: 696.6, TArrival: 15587, ArrivalSpeed: 12.5},
			{Laps: 5, Ok: true, DV: 331.6, TArrival: 32592, ArrivalSpeed: 6.3},
		},
	}
	v.OpenMeetingPicker(planner.MeetingTheirOrbit, ladder, nil)

	out := v.Render(w, 0, 80, 24)
	if !strings.Contains(out, "MEETING PLAN") ||
		!strings.Contains(out, "697 m/s") ||
		!strings.Contains(out, "332 m/s") {
		t.Errorf("golden 80×24 render missing expected chip content:\n%s", out)
	}
}
