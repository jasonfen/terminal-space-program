package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// This file is the Rendezvous Planner picker's UI half (ADR 0045 S6, #399):
// a walkable chip on the orbit map — ←/→ walks the Rendezvous Orbit, ↑/↓
// walks the Lap Ladder, Enter plants, Esc closes. tui.App owns every call
// into World (RecommendRendezvousLadder / PlanRendezvousBurn); this file is pure
// navigation state plus the chip's rendered lines.

// rendezvousPickerOrbitCycle is the ←/→ walk order ("their orbit / your
// orbit / the crossing", per ADR 0045 §2's own acceptance wording) —
// deliberately NOT planner.RendezvousOrbit's enum order (RendezvousCrossing is
// iota 0 there, for unrelated reasons: see planner/rendezvous_ladder.go). "their orbit" is
// the picker's opening Place (rendezvousKDefaultPlace, sim package), so it
// leads the cycle here too.
var rendezvousPickerOrbitCycle = [...]planner.RendezvousOrbit{
	planner.RendezvousTheirOrbit,
	planner.RendezvousYourOrbit,
	planner.RendezvousCrossing,
}

func rendezvousPickerOrbitCycleIndex(p planner.RendezvousOrbit) int {
	for i, c := range rendezvousPickerOrbitCycle {
		if c == p {
			return i
		}
	}
	return 0
}

// rendezvousPickerState is the picker's UI navigation state: which Place is
// selected, which Lap Ladder row, and the most recently computed ladder
// for that Place (App recomputes and pushes it in via SetRendezvousPickerLadder
// whenever Place changes — RecommendRendezvousLadder needs the live World,
// which this package-internal state deliberately does not hold).
type rendezvousPickerState struct {
	open      bool
	place     planner.RendezvousOrbit
	rowIdx    int
	ladder    planner.RendezvousLadder
	ladderErr error
}

// OpenRendezvousPicker opens the picker at the given Place with its already-
// computed ladder (or structural ladderErr — #407: a per-Place refusal
// like "orbits differ too much in size" is shown, not hidden). Row
// selection starts at the first Ok row when one exists, else row 0, so
// Enter on first open lands on a plantable row whenever the ladder has
// one.
func (v *OrbitView) OpenRendezvousPicker(place planner.RendezvousOrbit, ladder planner.RendezvousLadder, ladderErr error) {
	v.rendezvousPicker = rendezvousPickerState{
		open:      true,
		place:     place,
		ladder:    ladder,
		ladderErr: ladderErr,
		rowIdx:    rendezvousPickerFirstOkRow(ladder),
	}
}

func rendezvousPickerFirstOkRow(ladder planner.RendezvousLadder) int {
	for i, row := range ladder.Rows {
		if row.Ok {
			return i
		}
	}
	return 0
}

// CloseRendezvousPicker closes the picker without planting anything —
// Esc's contract (ADR 0045 §2 acceptance: "Esc plants nothing").
func (v *OrbitView) CloseRendezvousPicker() {
	v.rendezvousPicker = rendezvousPickerState{}
}

// RendezvousPickerOpen reports whether the picker is currently up. Used by
// tui.App both to gate the ←/→/↑/↓/Enter/Esc key intercept (so those keys
// never fall through to camera pan / flight controls while the picker has
// them) and to join capturingText() (the boss key / keyboard-layout
// normalization must not fire while this surface holds input either).
func (v *OrbitView) RendezvousPickerOpen() bool {
	return v.rendezvousPicker.open
}

// RendezvousPickerOrbit returns the picker's currently selected Rendezvous
// Place. Only meaningful while RendezvousPickerOpen().
func (v *OrbitView) RendezvousPickerOrbit() planner.RendezvousOrbit {
	return v.rendezvousPicker.place
}

// RendezvousPickerLeft / RendezvousPickerRight walk the Rendezvous Orbit cycle
// (their orbit / your orbit / the crossing) and clear the stale ladder —
// tui.App must follow with SetRendezvousPickerLadder once it has recomputed
// against the live World for the new Place; until then the chip shows the
// new Place's header with no rows rather than the OLD Place's rows under
// the NEW Place's label (a silent lie a re-render could otherwise let
// slip through for one frame).
func (v *OrbitView) RendezvousPickerLeft() {
	v.rendezvousPickerCyclePlace(-1)
}

func (v *OrbitView) RendezvousPickerRight() {
	v.rendezvousPickerCyclePlace(1)
}

func (v *OrbitView) rendezvousPickerCyclePlace(delta int) {
	if !v.rendezvousPicker.open {
		return
	}
	n := len(rendezvousPickerOrbitCycle)
	i := rendezvousPickerOrbitCycleIndex(v.rendezvousPicker.place)
	i = (i + delta + n) % n
	v.rendezvousPicker.place = rendezvousPickerOrbitCycle[i]
	v.rendezvousPicker.ladder = planner.RendezvousLadder{}
	v.rendezvousPicker.ladderErr = nil
	v.rendezvousPicker.rowIdx = 0
}

// SetRendezvousPickerLadder pushes a freshly computed ladder for the
// picker's CURRENT Place (a no-op if the picker has since closed or
// moved to a different Place than the one this ladder was computed for —
// a stale async-feeling result must never overwrite a newer selection).
func (v *OrbitView) SetRendezvousPickerLadder(place planner.RendezvousOrbit, ladder planner.RendezvousLadder, ladderErr error) {
	if !v.rendezvousPicker.open || v.rendezvousPicker.place != place {
		return
	}
	v.rendezvousPicker.ladder = ladder
	v.rendezvousPicker.ladderErr = ladderErr
	v.rendezvousPicker.rowIdx = rendezvousPickerFirstOkRow(ladder)
}

// RendezvousPickerUp / RendezvousPickerDown walk the Lap Ladder rows. Clamped,
// not wrapping — the ladder is a short fixed list (2/3/5/10/20 laps, see
// planner.rendezvousCandidateLaps) and wrapping ↑ from the top row back to
// the bottom reads as a jump, not a walk.
func (v *OrbitView) RendezvousPickerUp() {
	if v.rendezvousPicker.rowIdx > 0 {
		v.rendezvousPicker.rowIdx--
	}
}

func (v *OrbitView) RendezvousPickerDown() {
	if v.rendezvousPicker.rowIdx < len(v.rendezvousPicker.ladder.Rows)-1 {
		v.rendezvousPicker.rowIdx++
	}
}

// RendezvousPickerSelectedLaps returns the lap count of the currently
// highlighted row. ok=false when the ladder has no rows at all (a
// structural ladderErr, #407) — Enter is then a no-op, not a plant of
// row zero of an empty slice.
func (v *OrbitView) RendezvousPickerSelectedLaps() (int, bool) {
	rows := v.rendezvousPicker.ladder.Rows
	if v.rendezvousPicker.rowIdx < 0 || v.rendezvousPicker.rowIdx >= len(rows) {
		return 0, false
	}
	return rows[v.rendezvousPicker.rowIdx].Laps, true
}

// buildRendezvousPickerChip renders the picker's chip content — nil when
// closed, so it composes into assembleChips exactly like any other
// contextual builder. Unaffordable / unsafe / no-solution rows render
// dimmed with their own reason rather than being hidden (ADR 0045 §2:
// "the trade stays visible"). Arrival speed rides along as a plain info
// row for the SELECTED row only, matching K's own trim-rung ArrivalSpeed
// convention — information, never a gate.
func (v *OrbitView) buildRendezvousPickerChip() []string {
	mp := v.rendezvousPicker
	if !mp.open {
		return nil
	}
	lines := []string{
		v.theme.Primary.Render("RENDEZVOUS PLAN"),
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
		wait := readout.Duration(time.Duration(row.TArrival * float64(time.Second)))
		var body string
		if row.Ok {
			// readout.DeltaV returns "N m/s" as one string; split the
			// number back out and right-align it to a fixed field so the
			// "m/s" column stays aligned across rows regardless of how
			// many digits the contract's own precision rule gives a
			// particular row's figure (TestRendezvousPickerChip_LadderColumnsAlign).
			dvNum, dvUnit, _ := strings.Cut(readout.DeltaV(row.DV), " ")
			body = fmt.Sprintf("%s %2d laps   %-8s %5s %s", marker, row.Laps, wait, dvNum, dvUnit)
		} else {
			body = fmt.Sprintf("%s %2d laps   %-8s (%s)", marker, row.Laps, wait, row.Reason)
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
		lines = append(lines, fmt.Sprintf("  arriving ~%s", readout.Speed(sel.ArrivalSpeed)))
	}
	return lines
}

func (mp rendezvousPickerState) selectedRow() (planner.RendezvousBurnOption, bool) {
	if mp.rowIdx < 0 || mp.rowIdx >= len(mp.ladder.Rows) {
		return planner.RendezvousBurnOption{}, false
	}
	return mp.ladder.Rows[mp.rowIdx], true
}
