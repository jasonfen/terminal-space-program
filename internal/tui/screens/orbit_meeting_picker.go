package screens

import (
	"fmt"

	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// This file is the Meeting Planner picker's UI half (ADR 0045 S6, #399):
// a walkable chip on the orbit map — ←/→ walks the Meeting Place, ↑/↓
// walks the Lap Ladder, Enter plants, Esc closes. tui.App owns every call
// into World (RecommendMeetingLadder / PlanMeetingBurn); this file is pure
// navigation state plus the chip's rendered lines.

// meetingPickerPlaceCycle is the ←/→ walk order ("their orbit / your
// orbit / the crossing", per ADR 0045 §2's own acceptance wording) —
// deliberately NOT planner.MeetingPlace's enum order (MeetingCrossing is
// iota 0 there, for unrelated reasons: see meeting.go). "their orbit" is
// the picker's opening Place (rendezvousKDefaultPlace, sim package), so it
// leads the cycle here too.
var meetingPickerPlaceCycle = [...]planner.MeetingPlace{
	planner.MeetingTheirOrbit,
	planner.MeetingYourOrbit,
	planner.MeetingCrossing,
}

func meetingPickerPlaceCycleIndex(p planner.MeetingPlace) int {
	for i, c := range meetingPickerPlaceCycle {
		if c == p {
			return i
		}
	}
	return 0
}

// meetingPickerState is the picker's UI navigation state: which Place is
// selected, which Lap Ladder row, and the most recently computed ladder
// for that Place (App recomputes and pushes it in via SetMeetingPickerLadder
// whenever Place changes — RecommendMeetingLadder needs the live World,
// which this package-internal state deliberately does not hold).
type meetingPickerState struct {
	open      bool
	place     planner.MeetingPlace
	rowIdx    int
	ladder    planner.MeetingLadder
	ladderErr error
}

// OpenMeetingPicker opens the picker at the given Place with its already-
// computed ladder (or structural ladderErr — #407: a per-Place refusal
// like "orbits differ too much in size" is shown, not hidden). Row
// selection starts at the first Ok row when one exists, else row 0, so
// Enter on first open lands on a plantable row whenever the ladder has
// one.
func (v *OrbitView) OpenMeetingPicker(place planner.MeetingPlace, ladder planner.MeetingLadder, ladderErr error) {
	v.meetingPicker = meetingPickerState{
		open:      true,
		place:     place,
		ladder:    ladder,
		ladderErr: ladderErr,
		rowIdx:    meetingPickerFirstOkRow(ladder),
	}
}

func meetingPickerFirstOkRow(ladder planner.MeetingLadder) int {
	for i, row := range ladder.Rows {
		if row.Ok {
			return i
		}
	}
	return 0
}

// CloseMeetingPicker closes the picker without planting anything —
// Esc's contract (ADR 0045 §2 acceptance: "Esc plants nothing").
func (v *OrbitView) CloseMeetingPicker() {
	v.meetingPicker = meetingPickerState{}
}

// MeetingPickerOpen reports whether the picker is currently up. Used by
// tui.App both to gate the ←/→/↑/↓/Enter/Esc key intercept (so those keys
// never fall through to camera pan / flight controls while the picker has
// them) and to join capturingText() (the boss key / keyboard-layout
// normalization must not fire while this surface holds input either).
func (v *OrbitView) MeetingPickerOpen() bool {
	return v.meetingPicker.open
}

// MeetingPickerPlace returns the picker's currently selected Meeting
// Place. Only meaningful while MeetingPickerOpen().
func (v *OrbitView) MeetingPickerPlace() planner.MeetingPlace {
	return v.meetingPicker.place
}

// MeetingPickerLeft / MeetingPickerRight walk the Meeting Place cycle
// (their orbit / your orbit / the crossing) and clear the stale ladder —
// tui.App must follow with SetMeetingPickerLadder once it has recomputed
// against the live World for the new Place; until then the chip shows the
// new Place's header with no rows rather than the OLD Place's rows under
// the NEW Place's label (a silent lie a re-render could otherwise let
// slip through for one frame).
func (v *OrbitView) MeetingPickerLeft() {
	v.meetingPickerCyclePlace(-1)
}

func (v *OrbitView) MeetingPickerRight() {
	v.meetingPickerCyclePlace(1)
}

func (v *OrbitView) meetingPickerCyclePlace(delta int) {
	if !v.meetingPicker.open {
		return
	}
	n := len(meetingPickerPlaceCycle)
	i := meetingPickerPlaceCycleIndex(v.meetingPicker.place)
	i = (i + delta + n) % n
	v.meetingPicker.place = meetingPickerPlaceCycle[i]
	v.meetingPicker.ladder = planner.MeetingLadder{}
	v.meetingPicker.ladderErr = nil
	v.meetingPicker.rowIdx = 0
}

// SetMeetingPickerLadder pushes a freshly computed ladder for the
// picker's CURRENT Place (a no-op if the picker has since closed or
// moved to a different Place than the one this ladder was computed for —
// a stale async-feeling result must never overwrite a newer selection).
func (v *OrbitView) SetMeetingPickerLadder(place planner.MeetingPlace, ladder planner.MeetingLadder, ladderErr error) {
	if !v.meetingPicker.open || v.meetingPicker.place != place {
		return
	}
	v.meetingPicker.ladder = ladder
	v.meetingPicker.ladderErr = ladderErr
	v.meetingPicker.rowIdx = meetingPickerFirstOkRow(ladder)
}

// MeetingPickerUp / MeetingPickerDown walk the Lap Ladder rows. Clamped,
// not wrapping — the ladder is a short fixed list (2/3/5/10/20 laps, see
// planner.meetingCandidateLaps) and wrapping ↑ from the top row back to
// the bottom reads as a jump, not a walk.
func (v *OrbitView) MeetingPickerUp() {
	if v.meetingPicker.rowIdx > 0 {
		v.meetingPicker.rowIdx--
	}
}

func (v *OrbitView) MeetingPickerDown() {
	if v.meetingPicker.rowIdx < len(v.meetingPicker.ladder.Rows)-1 {
		v.meetingPicker.rowIdx++
	}
}

// MeetingPickerSelectedLaps returns the lap count of the currently
// highlighted row. ok=false when the ladder has no rows at all (a
// structural ladderErr, #407) — Enter is then a no-op, not a plant of
// row zero of an empty slice.
func (v *OrbitView) MeetingPickerSelectedLaps() (int, bool) {
	rows := v.meetingPicker.ladder.Rows
	if v.meetingPicker.rowIdx < 0 || v.meetingPicker.rowIdx >= len(rows) {
		return 0, false
	}
	return rows[v.meetingPicker.rowIdx].Laps, true
}

// buildMeetingPickerChip renders the picker's chip content — nil when
// closed, so it composes into assembleChips exactly like any other
// contextual builder. Unaffordable / unsafe / no-solution rows render
// dimmed with their own reason rather than being hidden (ADR 0045 §2:
// "the trade stays visible"). Arrival speed rides along as a plain info
// row for the SELECTED row only, matching K's own trim-rung ArrivalSpeed
// convention — information, never a gate.
func (v *OrbitView) buildMeetingPickerChip() []string {
	mp := v.meetingPicker
	if !mp.open {
		return nil
	}
	lines := []string{
		v.theme.Primary.Render("MEETING PLAN"),
		fmt.Sprintf("  ← %s →", mp.place.String()),
	}
	if mp.ladderErr != nil {
		lines = append(lines, "  "+v.theme.Warning.Render(mp.ladderErr.Error()))
		return lines
	}
	for i, row := range mp.ladder.Rows {
		marker := " "
		if i == mp.rowIdx {
			marker = ">"
		}
		wait := formatDurationShort(row.TArrival)
		var body string
		if row.Ok {
			body = fmt.Sprintf("%s %d laps   %-8s %5.0f m/s", marker, row.Laps, wait, row.DV)
		} else {
			body = fmt.Sprintf("%s %d laps   %-8s (%s)", marker, row.Laps, wait, row.Reason)
		}
		if i == mp.rowIdx {
			lines = append(lines, v.theme.Primary.Render(body))
		} else if !row.Ok {
			lines = append(lines, v.theme.Dim.Render(body))
		} else {
			lines = append(lines, body)
		}
	}
	if sel, ok := mp.selectedRow(); ok && sel.Ok {
		lines = append(lines, fmt.Sprintf("  arriving ~%.0f m/s", sel.ArrivalSpeed))
	}
	return lines
}

func (mp meetingPickerState) selectedRow() (planner.MeetingBurnOption, bool) {
	if mp.rowIdx < 0 || mp.rowIdx >= len(mp.ladder.Rows) {
		return planner.MeetingBurnOption{}, false
	}
	return mp.ladder.Rows[mp.rowIdx], true
}
