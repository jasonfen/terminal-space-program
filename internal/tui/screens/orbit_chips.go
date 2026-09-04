package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// This file implements the v0.13 (ADR 0010) HUD split: the orbit screen's
// tall conditional-block stack becomes a slim always-on telemetry column
// (buildSlimColumn) plus compact Chips composited onto the canvas corners
// (composeChips), reusing the navball's overlayStyledBlock path. A Chip
// renders iff enabled (Settings) && relevant (state) && !declutter.
//
// The blocks themselves are still formatted by renderHUD's per-block code
// until the extraction step transplants each into a chip builder; this
// file holds the genuinely-new machinery: the corner compositor, the slim
// column, and the bounded-summary Chips (Stages pips, Nodes next+count)
// that replace the old variable-length lists.

// chipCorner is the canvas corner a Chip anchors to. Per the v0.13 corner
// map: Stages bottom-left, Nodes bottom-right (above the navball), Orbit
// metrics top-right, and the phase-transient chips stack top-left.
type chipCorner int

const (
	cornerTopLeft chipCorner = iota
	cornerTopRight
	cornerBottomLeft
	cornerBottomRight
)

// chipSide groups the four corners into the two shared-budget columns
// ADR 0046 ("Graceful Shrink") introduces: sideLeft is cornerTopLeft ∪
// cornerBottomLeft, sideRight is cornerTopRight ∪ cornerBottomRight. A top
// stack growing down and a bottom stack growing up on the SAME side now
// compete for one pool of rows instead of two independently-budgeted
// corners, so they can never grow into each other (#422 — STAGES painting
// over PROXIMITY's leading digits was exactly two independently-fitting
// corners with no shared awareness).
type chipSide int

const (
	sideLeft chipSide = iota
	sideRight
)

func (c chipCorner) side() chipSide {
	if c == cornerTopRight || c == cornerBottomRight {
		return sideRight
	}
	return sideLeft
}

func (c chipCorner) topGroup() bool {
	return c == cornerTopLeft || c == cornerTopRight
}

// builtChip is one composited overlay: its Settings id (empty = always-on,
// non-toggleable — the safety-critical ● BURNS readout and the ORBIT
// metrics chip), the corner it anchors to, its already-styled full-form
// content lines (header + rows), and an optional Compact Form. Relevance is
// decided by the builder returning nil when the chip has nothing to show.
// Always-on chips are still hidden by declutter; only the pinned VESSEL
// core chip survives it.
type builtChip struct {
	id     settings.Chip
	corner chipCorner
	lines  []string
	// compact is this chip's Compact Form (ADR 0046 / CONTEXT.md "Graceful
	// Shrink"): title plus one or two key rows, chosen per chip by its
	// builder (see the compact-builder comments in orbit_chip_builders.go
	// and orbit_proximity.go for which rows each chip keeps and why). nil
	// means the chip has no separate reduction worth making — its full form
	// is already chip-sized (1-3 lines) — so layoutChipsBySide treats
	// `lines` as its own Compact Form: shrinking it is a no-op, and it goes
	// straight from full to dropped if its side still doesn't fit.
	compact []string
	// leftOfPrev places this chip on the same top row as the previously
	// placed chip in the same (top-right) corner, immediately to its left,
	// instead of stacking below it — so two chips share a row band rather
	// than growing the column's height, WHEN that previous chip actually
	// got admitted this frame. Used for PROJECTED ORBIT beside the ORBIT
	// chip. Honoured only for cornerTopRight; falls back to ordinary
	// stacking when there's no admitted prior top-right chip to sit
	// beside (e.g. ORBIT itself dropped for space under Graceful Shrink)
	// — see composeChips' haveTR branch. Because that fallback can happen,
	// layoutChipsBySide still budgets a leftOfPrev chip like any other
	// (ADR 0046): it is NOT exempt from the side's shared budget.
	leftOfPrev bool
	// neverShrink exempts a chip from layoutChipsBySide's budget entirely
	// (always Full, never Compact, never dropped) while it still stacks
	// normally (unlike leftOfPrev, it doesn't ride beside another chip).
	// Reserved for MEETING PLAN (ADR 0045 S6): it's a modal that claims
	// keyboard focus while open, so losing it silently would leave the
	// player's keystrokes going nowhere with no explanation on screen —
	// a different category of thing than an ordinary contextual Chip
	// reflecting world state, closer to the title bar's own Graceful
	// Shrink exemption (ADR 0046 §3) than to a Chip that can gracefully
	// give up its space.
	neverShrink bool
	// priority controls the order chips shrink to Compact Form and then
	// drop when a side's shared budget overflows (ADR 0046 — see
	// layoutChipsBySide). Defaults to chipPriorityNormal; the shared "add"
	// helper in assembleChips never sets it, so every ordinary toggleable
	// chip competes on equal footing and only chipPriorityCore/
	// chipPriorityForced chips are called out explicitly.
	priority int
}

// dropChip removes every chip carrying `id` from an assembled list. The
// escape hatch for a screen that assembles the shared chip set but owns a
// BETTER block for one of them — today only the surface view, whose
// DESCENT CORRIDOR supersedes DESCENT while a descent is live (see the
// call site in launch.go). Filtering the assembled slice rather than
// teaching assembleChips about the caller keeps the shared builder free
// of per-screen conditionals, and keeps the substitution stated where the
// replacement block is built.
func dropChip(chips []builtChip, id settings.Chip) []builtChip {
	out := chips[:0]
	for _, c := range chips {
		if c.id == id {
			continue
		}
		out = append(out, c)
	}
	return out
}

// Priority tiers for layoutChipsBySide (#328/#334, reworked for #422 /
// ADR 0046). Highest first:
//
//   - chipPriorityCore (100): unconditionally pinned core telemetry that
//     predates chip toggles entirely — VESSEL and the top-right ORBIT
//     metrics chip. Never gated by Settings or declutter.
//   - chipPriorityForced (90): chips a game-state rule force-shows past
//     the Settings toggle AND F2 declutter, because losing them
//     silently would hide a safety/continuity fact the player has no
//     other way to see: DOCKED (ADR 0038 S4 — the rider's only
//     surviving route to [J]/[U] once absorbed into another player's
//     stack, #328) and NODES while force-shown (a live burn, or more
//     than one queued node on the active craft, #333/#334).
//   - chipPriorityNormal (0, the zero value): every ordinary toggleable/
//     contextual chip. Shrinks and drops first when a side overflows.
//
// Under the Graceful Shrink contract (ADR 0046) priority no longer buys a
// chip permanent admission — it buys it LAST PLACE in the shrink and drop
// order. A side overflowing its shared budget shrinks every chip to
// Compact Form lowest-priority-tier first (Normal, then Forced, then
// Core); only once every chip on the side is already Compact and it still
// overflows does anything drop, again lowest priority first, leaving a
// one-row Hidden Stub. This replaces the pre-#422 behaviour where a
// critical chip was always admitted and, if it overran its budget,
// composeChips clamped it onto the canvas — accepting an overlap with
// whatever was beneath it — rather than ever let it shrink or drop. That
// clamp is exactly the bug ADR 0046 fixes (the force-shown NODES chip
// painting over the Core ORBIT chip during a burn, #422): a critical chip
// now shrinks and, in the rare case its side genuinely cannot fit it even
// Compact, drops behind a stub like anything else — it is simply the
// last thing asked to give up its space.
const (
	chipPriorityCore   = 100
	chipPriorityForced = 90
	chipPriorityNormal = 0
)

// chipPriorityTiers is the shrink/drop order layoutChipsBySide walks:
// lowest priority first.
var chipPriorityTiers = []int{chipPriorityNormal, chipPriorityForced, chipPriorityCore}

// chipRect is the absolute screen-cell rectangle a composited Chip
// occupied this frame, used by the orbit screen's mouse dispatch to route
// a click on a Chip. Coordinates are screen-space (canvas border + title
// offsets already applied), inclusive of both endpoints.
type chipRect struct {
	id               settings.Chip
	colStart, colEnd int
	rowStart, rowEnd int
}

// chipGap is the blank-row spacing between stacked chips in the same
// corner. Each chip now carries its own single-cell border, which already
// separates adjacent panels, so no extra blank row is needed between them.
const chipGap = 0

// padChipBlock right-pads every line of a chip to the block's widest
// visible width so the overlay paints an opaque rectangle over the busy
// canvas (otherwise braille dots bleed through the ragged right edge).
// Returns the padded lines and that width.
func padChipBlock(lines []string) ([]string, int) {
	width := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > width {
			width = w
		}
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if pad := width - lipgloss.Width(l); pad > 0 {
			out[i] = l + strings.Repeat(" ", pad)
		} else {
			out[i] = l
		}
	}
	return out, width
}

// chipForm is the form a chip resolves to for one frame, decided by
// layoutChipsBySide.
type chipForm int

const (
	chipFormFull chipForm = iota
	chipFormCompact
	chipFormHidden
)

// chipStubHeight is a Hidden Stub's footprint: exactly one bare row, no
// border (CONTEXT.md "Graceful Shrink": "a one-row Hidden Stub"). This
// matters at the tightest real geometry: at the Playable Floor (104×24)
// with the navball showing, the whole right side has exactly ONE spare
// row above the navball's reservation — a normal bordered chip block
// (minimum 3 rows: border + one content row + border) can never fit
// there, so the stub that replaces a dropped chip must not carry the
// usual border overhead either, or it could never fit the one case it
// exists for.
const chipStubHeight = 1

// blockHeight is a chip's footprint in `form`, in canvas rows including
// its own border (chipFormHidden costs 0 — see layoutChipsBySide's stub
// accounting for what a drop actually costs the side).
func (c builtChip) blockHeight(form chipForm) int {
	switch form {
	case chipFormHidden:
		return 0
	case chipFormCompact:
		if c.compact != nil {
			return len(c.compact) + 2
		}
		return len(c.lines) + 2 // no distinct Compact Form: full IS compact
	default:
		return len(c.lines) + 2
	}
}

// layoutChipsBySide is the Graceful Shrink contract (ADR 0046 / CONTEXT.md
// "Graceful Shrink" / "Compact Form"): decide, per SIDE (left = top-left ∪
// bottom-left, right = top-right ∪ bottom-right — see chipSide), which
// chips render Full, which shrink to Compact, and which drop with a
// Hidden Stub in their place. leftOfPrev chips and chips with no content
// never participate — they either ride free beside another chip or have
// no footprint to budget.
//
// The three-phase contract, applied independently to each side:
//
//  1. If every chip on the side already fits Full within its shared
//     budget, nothing changes — composeChips lays it out exactly like a
//     flat, unconditional append (the pre-#422 happy path).
//  2. Otherwise chips shrink to Compact Form one at a time, lowest
//     priority TIER first (Normal, then Forced, then Core — see
//     chipPriorityTiers), stopping the instant the side fits again. A
//     chip with no distinct Compact Form (builtChip.compact == nil)
//     still marks as Compact here — it has nothing further to give, but
//     it's no longer a candidate to shrink again, only to drop.
//  3. If every chip on the side is Compact and it STILL overflows, chips
//     drop outright — again lowest priority tier first — and a single
//     one-row Hidden Stub ("▸ +N hidden") is reserved for that side,
//     summarising every drop on it. One stub per side (not one per
//     corner-that-dropped) is deliberate: at the tightest real geometry
//     (104×24, navball showing) the whole right side has exactly one
//     spare row, so two stubs — one per corner — could themselves
//     overlap. A single side-wide stub always costs exactly
//     chipStubHeight and always fits whenever the side has any budget
//     at all.
//
// Budget: cRows-1 reserves the canvas's last row for the "view:" label
// (shared by both sides — see composeChips). The right side additionally
// subtracts navballReserved, because the navball's own rows are never
// available to Chips on either its top or bottom stack (CONTEXT.md
// "Graceful Shrink": "the Navball keeps its reserved rows"); at 104×24
// with the navball showing this leaves exactly one spare row, which is
// exactly chipStubHeight — by design, not coincidence.
//
// This replaces the old per-CORNER admitChipsByBudget (#328/#334), which
// gave each of the four corners an independent budget with no shared
// awareness — a tall top-left stack and a tall bottom-left stack could
// each fit their own corner's budget and still grow into each other
// (#422: STAGES painting over PROXIMITY's leading digits in proximity
// view). It also replaces composeChips' old last-resort clamp, which let
// a chip at chipPriorityForced or above overrun its budget and get
// pulled back on-canvas over whatever was beneath it (#328/#334's
// original fix, now superseded) — the exact bug that let a force-shown
// NODES chip paint over the Core ORBIT chip during a burn.
func layoutChipsBySide(chips []builtChip, cRows, navballReserved int) (forms []chipForm, stubs map[chipSide]int) {
	forms = make([]chipForm, len(chips))
	stubs = make(map[chipSide]int)

	var leftIdx, rightIdx []int
	for i, c := range chips {
		forms[i] = chipFormFull
		if c.neverShrink || len(c.lines) == 0 {
			continue // exempt / no footprint: never shrinks or drops
		}
		// leftOfPrev chips (PROJECTED ORBIT) usually ride for free beside
		// their anchor and cost the corner nothing — but that's only true
		// AT RENDER TIME, when the anchor (ORBIT) is actually admitted.
		// Under Graceful Shrink the anchor can itself compact or drop, in
		// which case composeChips falls back to placing this chip via
		// ordinary stacking (see its leftOfPrev/haveTR branch) — and it
		// must then fit the side's budget like anything else, or it can
		// run unbudgeted into the navball exactly like the pre-#422 bug
		// this ADR fixes. So it's budgeted here pessimistically (as if it
		// always stacks normally): a small, safe overestimate on the
		// common "anchor admitted" path, in exchange for never overrunning
		// on the "anchor dropped" path.
		if c.corner.side() == sideRight {
			rightIdx = append(rightIdx, i)
		} else {
			leftIdx = append(leftIdx, i)
		}
	}

	layoutSide := func(side chipSide, idxs []int, budget int) {
		if budget < 0 {
			budget = 0
		}
		total := func() int {
			t := 0
			for _, i := range idxs {
				t += chips[i].blockHeight(forms[i])
			}
			if stubs[side] > 0 {
				t += chipStubHeight
			}
			return t
		}
		if total() <= budget {
			return
		}
		// Within a tier, shrink/drop LATEST-added chips first. assembleChips'
		// append order runs "most load-bearing first" (e.g. PROXIMITY is
		// added ahead of the phase-transient chips specifically "so the
		// readout the player is flying the last kilometres on wins the
		// space over the transient chips behind it" — see its doc comment
		// in orbit_proximity.go). Reversing here honours that: a chip
		// earlier in the list survives longer than one added after it, at
		// the same priority tier.
		reversed := make([]int, len(idxs))
		for i, v := range idxs {
			reversed[len(idxs)-1-i] = v
		}
		// Phase 2: shrink to Compact, lowest priority tier first, one chip
		// at a time (latest-added within a tier first), stopping the
		// instant the side fits.
		for _, tier := range chipPriorityTiers {
			for _, i := range reversed {
				if total() <= budget {
					return
				}
				if chips[i].priority != tier || forms[i] != chipFormFull {
					continue
				}
				forms[i] = chipFormCompact
			}
		}
		if total() <= budget {
			return
		}
		// Phase 3: every chip on the side is Compact and it still
		// overflows — drop outright, lowest priority tier first (latest-
		// added within a tier first), behind one shared Hidden Stub for
		// the whole side.
		for _, tier := range chipPriorityTiers {
			for _, i := range reversed {
				if total() <= budget {
					return
				}
				if chips[i].priority != tier || forms[i] == chipFormHidden {
					continue
				}
				forms[i] = chipFormHidden
				stubs[side]++
			}
		}
	}

	// Right budget excludes the navball's own rows entirely but does NOT
	// double the "view:" label reservation on top of that: the navball's
	// placement (composeNavballOverlay: atRow = cRows-navballPanelH-1)
	// already sits one row shy of the bottom, so navballReserved
	// (navballPanelH+1) already accounts for the label row on the right.
	// At the exact Playable Floor with the navball showing this leaves
	// exactly one spare row — see chipStubHeight.
	leftBudget := cRows - 1
	rightBudget := cRows - navballReserved
	layoutSide(sideLeft, leftIdx, leftBudget)
	layoutSide(sideRight, rightIdx, rightBudget)
	return forms, stubs
}

// composeChips paints each chip onto canvasStr at its corner, stacking
// multiple chips in a corner with a one-row gap, and records each chip's
// screen rectangle in v.chipRects for mouse routing. navballReserved is
// the number of bottom rows the navball panel occupies (0 when it isn't
// shown) so the bottom-right Nodes chip stacks above it. screenColOffset
// / screenRowOffset translate canvas-local coordinates to absolute screen
// cells (the canvas sits one col / two rows in, behind the border + title)
// so the recorded rects line up with incoming mouse events.
//
// Top-left chips start one row down so they clear the canvas's "focus:"
// label at (0,0); bottom-left chips stop one row up so they clear the
// "view:" label on the last row. layoutChipsBySide decides, per side,
// which chips render Full, which render Compact, and which drop behind a
// Hidden Stub before this loop ever runs (ADR 0046's Graceful Shrink
// contract) — chips it drops are skipped here entirely, so nothing ever
// gets clamped onto the canvas over a neighbour.
func (v *OrbitView) composeChips(canvasStr string, cCols, cRows, navballReserved, screenColOffset, screenRowOffset int, chips []builtChip) string {
	v.chipRects = v.chipRects[:0]
	lines := strings.Split(canvasStr, "\n")
	forms, stubs := layoutChipsBySide(chips, cRows, navballReserved)

	// Per-corner stacking cursors. Top corners grow downward from their
	// start row; bottom corners grow upward from their start row. v0.13:
	// the "focus:" label left (0,0) for the title bar, so top-left chips
	// now start at row 0.
	topLeftRow := 0
	topRightRow := 0
	bottomLeftRow := cRows - 2 // above the "view:" label on row cRows-1
	bottomRightRow := cRows - 1 - navballReserved

	// Remember the last normally-placed top-right chip so a leftOfPrev chip
	// can sit beside it (same top row, immediately to its left).
	lastTRStartRow, lastTRCol, haveTR := 0, 0, false

	// place lays out one block (bordered chip content, or a bare one-row
	// Hidden Stub when bordered is false) at its corner's stacking cursor,
	// advancing that cursor, splicing it onto the canvas, and recording
	// its screen rect (skipped for a stub — it isn't a real chip a click
	// can route to).
	place := func(id settings.Chip, corner chipCorner, chipLines []string, leftOfPrev, bordered bool) {
		var block string
		var bw, bh int
		if bordered {
			padded, w := padChipBlock(chipLines)
			if len(padded) == 0 || w == 0 {
				return
			}
			// Wrap in a single-cell rounded border so every panel reads as
			// a distinct framed box over the canvas. The frame adds one
			// cell on each side, so the placed block is w+2 × h+2.
			block = wrapBorder(strings.Join(padded, "\n"), w, v.theme.Primary.GetForeground())
			bw, bh = w+2, len(padded)+2
		} else {
			if len(chipLines) == 0 {
				return
			}
			block = chipLines[0]
			bw, bh = lipgloss.Width(block), chipStubHeight
			if bw == 0 {
				return
			}
		}
		var atRow, atCol int
		switch corner {
		case cornerTopLeft:
			atRow, atCol = topLeftRow, 0
			topLeftRow += bh + chipGap
		case cornerTopRight:
			if leftOfPrev && haveTR {
				// Sit beside the previous top-right chip rather than below it.
				atRow, atCol = lastTRStartRow, lastTRCol-bw
				if bottom := atRow + bh + chipGap; bottom > topRightRow {
					topRightRow = bottom
				}
				lastTRCol = atCol // a further leftOfPrev chip chains leftward
			} else {
				atRow, atCol = topRightRow, cCols-bw
				topRightRow += bh + chipGap
				lastTRStartRow, lastTRCol, haveTR = atRow, atCol, true
			}
		case cornerBottomLeft:
			atRow, atCol = bottomLeftRow-bh+1, 0
			bottomLeftRow -= bh + chipGap
		case cornerBottomRight:
			atRow, atCol = bottomRightRow-bh+1, cCols-bw
			bottomRightRow -= bh + chipGap
		}
		if atCol < 0 {
			atCol = 0
		}
		lines = overlayStyledBlock(lines, block, atRow, atCol, cCols)
		if !bordered {
			return // Hidden Stub: no rect, nothing to route a click to
		}
		v.chipRects = append(v.chipRects, chipRect{
			id:       id,
			colStart: atCol + screenColOffset,
			colEnd:   atCol + bw - 1 + screenColOffset,
			rowStart: atRow + screenRowOffset,
			rowEnd:   atRow + bh - 1 + screenRowOffset,
		})
	}

	for i, chip := range chips {
		switch forms[i] {
		case chipFormHidden:
			continue
		case chipFormCompact:
			cl := chip.compact
			if cl == nil {
				cl = chip.lines
			}
			place(chip.id, chip.corner, cl, chip.leftOfPrev, true)
		default:
			place(chip.id, chip.corner, chip.lines, chip.leftOfPrev, true)
		}
	}

	// Hidden Stubs (ADR 0046 §2): one per side that had a drop, rendered
	// after every admitted chip on that side so it reads as the last item
	// in the TOP stack — the corner where Core/Forced chips (VESSEL,
	// ORBIT) almost always already have a foothold, so the stub has a
	// natural anchor even when every chip that actually dropped lived in
	// the bottom stack.
	if n := stubs[sideLeft]; n > 0 {
		place("", cornerTopLeft, []string{v.theme.Dim.Render(fmt.Sprintf("▸ +%d hidden", n))}, false, false)
	}
	if n := stubs[sideRight]; n > 0 {
		place("", cornerTopRight, []string{v.theme.Dim.Render(fmt.Sprintf("▸ +%d hidden", n))}, false, false)
	}

	return strings.Join(lines, "\n")
}

// navballReservedRows reports how many bottom rows the navball panel
// occupies on the canvas this frame (0 when it isn't shown), so the
// bottom-right Nodes chip can stack above it. Mirrors the gate in
// composeNavballOverlay; the +1 matches the one-row bottom lift there.
func (v *OrbitView) navballReservedRows(w *sim.World, cCols, cRows int) int {
	if !w.CraftVisibleHere() || cCols < navballPanelW+2 || cRows < navballPanelH+2 {
		return 0
	}
	if _, _, ok := w.NavballSubObserver(); !ok {
		return 0
	}
	return navballPanelH + 1
}

// HitChip resolves a screen-space click against the Chips composited onto
// the canvas this frame, returning the clicked Chip's id and true when a
// rectangle contains (col, row). Empty-id chips (always-on overlays like
// ● BURNS and ORBIT metrics) report their empty id; callers match against
// specific ids.
func (v *OrbitView) HitChip(col, row int) (settings.Chip, bool) {
	for _, r := range v.chipRects {
		if col >= r.colStart && col <= r.colEnd && row >= r.rowStart && row <= r.rowEnd {
			return r.id, true
		}
	}
	return "", false
}

// chipEnabled reports whether a chip with the given Settings id should
// render given the current preferences and declutter state. The empty id
// is an always-on overlay, suppressed only by declutter.
func (v *OrbitView) chipEnabled(id settings.Chip) bool {
	if v.declutter {
		return false
	}
	if id == "" {
		return true
	}
	return v.settings.ChipEnabled(id)
}

// activeStageFuel reports the firing (bottom) stage's fuel as a percentage
// of its capacity plus its mass in kg — the tank the player is actually
// burning and watches to know when to stage. The whole-stack aggregate is
// misleading on a multi-stage rocket: a spent first stage reads ~21%
// "total" while every upper stage is full, looking alarmingly low even
// though that's normal staging (the S-IC is ~79% of all propellant). ok is
// false when there's no firing stage with capacity, so the caller falls
// back to a kg-only readout from c.Fuel.
func activeStageFuel(c *spacecraft.Spacecraft) (pct, massKg float64, ok bool) {
	if len(c.Stages) == 0 {
		return 0, 0, false
	}
	st := c.Stages[0]
	if st.FuelCapacity <= 0 {
		return 0, 0, false
	}
	return 100 * st.FuelMass / st.FuelCapacity, st.FuelMass, true
}

// buildVesselChip is the pinned core-telemetry chip: vessel identity,
// velocity, and the full propellant readout. v0.13 playtest move — this
// was the slim right-hand column; it now composites onto the canvas's
// top-left corner like every other chip, leaving the orbit map full-width.
// Always rendered: never settings-toggled (core telemetry is fixed, ADR
// 0010) and never hidden by declutter — F2 must not hide fuel/Δv mid-burn.
// Orbit shape (apo/peri/incl) lives in the top-right Orbit-metrics chip,
// attitude in the Attitude chip. Returns a "(in Sol — [tab])" hint when no
// craft is visible here; nil only when there's no active craft at all.
func (v *OrbitView) buildVesselChip(w *sim.World) []string {
	if !w.CraftVisibleHere() {
		if w.ActiveCraft() != nil {
			return []string{v.theme.Dim.Render("VESSEL (in Sol — [tab] to switch)")}
		}
		// #310: an empty slate used to render nothing at all. The camera
		// meanwhile fell through to the system origin, so the player was left
		// staring at a hard-zoomed star with no explanation — live, two people
		// hunting for exactly this state, one with the source open, failed to
		// read it for hours. Say it — and say WHY it is empty when we know:
		// riding in another player's stack is not the same situation as having
		// no craft, and offering "launch a new flight" there would be wrong.
		if dg := w.DockGuest; dg != nil {
			lines := []string{
				v.theme.Primary.Render("VESSEL"),
				"  " + v.theme.Warning.Render("docked in "+dg.OwnerHandle+"'s stack"),
			}
			// ADR 0038 S4 part 3 ("badged panels"): once the stack's ghost
			// report has landed, upgrade from the bare "why is this empty"
			// placeholder to its real flight data — badged with the owner's
			// handle so the numbers are never mistaken for this player's own
			// ship. Ghosts don't carry fuel/mass/Δv (never reported over the
			// wire, ADR 0034), so this shows what IS available: identity,
			// primary, and velocity — the same fields the ORBIT chip's
			// badged sibling (buildDockGuestOrbitChip) also draws from.
			if g, primary, ok := w.DockGuestStackGhost(); ok {
				name := g.Name
				if name == "" {
					name = "(unnamed)"
				}
				lines = append(lines,
					"  "+name,
					"  primary:   "+primary.EnglishName,
					fmt.Sprintf("  velocity:  %.2f km/s", g.Vel.Norm()/1000),
				)
			}
			// #330: [U], matching the actual uppercase Undock binding —
			// see the sibling comment on the DOCKED block's "ask to
			// undock" row in orbit_chip_builders.go.
			lines = append(lines, v.theme.Dim.Render("  [U] release it"))
			return lines
		}
		return []string{
			v.theme.Primary.Render("NO VESSEL"),
			"  " + v.theme.Warning.Render("your vessel slate is empty"),
			v.theme.Dim.Render("  [n] launch a new flight"),
		}
	}
	c := w.ActiveCraft()
	lines := []string{
		v.theme.Primary.Render("VESSEL"),
		"  " + crashedVesselNameLabel(v.theme, c),
		"  primary:   " + c.Primary.EnglishName,
		fmt.Sprintf("  velocity:  %.2f km/s", c.OrbitalSpeed()/1000),
		v.theme.Primary.Render("PROPELLANT"),
	}
	if pct, kg, ok := activeStageFuel(c); ok {
		lines = append(lines, fmt.Sprintf("  fuel:      %.0f%% (%.0f kg)", pct, kg))
	} else {
		lines = append(lines, fmt.Sprintf("  fuel:      %.0f kg", c.Fuel))
	}
	lines = append(lines,
		fmt.Sprintf("  mass:      %.0f kg", c.TotalMass()),
		fmt.Sprintf("  Δv budget: %.0f m/s", c.RemainingDeltaV()),
		fmt.Sprintf("  throttle:  %.0f%%", c.EffectiveThrottle()*100),
	)
	if c.MonopropCapacity > 0 {
		lines = append(lines,
			fmt.Sprintf("  monoprop:  %.0f kg", c.Monoprop),
			fmt.Sprintf("  rcs Δv:    %.0f m/s", c.RCSDeltaV()),
		)
		// In RCS mode, surface the per-pulse step so the player can see
		// the fine-trim level the `p` key cycles. v0.24.5+.
		if c.EngineMode == spacecraft.EngineRCS {
			lines = append(lines, fmt.Sprintf("  rcs pulse: %g m/s", c.RCSPulseDV()))
		}
	}
	return lines
}

// buildVesselChipCompact is VESSEL's Compact Form (ADR 0046 / #422): name
// plus the two figures a pilot needs at a glance mid-emergency — fuel and
// Δv budget — dropping mass, throttle, monoprop and RCS Δv. Mirrors
// buildVesselChip's early-return branches (no craft visible, riding as a
// dock guest, empty slate) unchanged: those are already ≤3 lines, so
// there's nothing further worth shrinking — nil compact there means
// layoutChipsBySide treats the full form as its own Compact Form.
func (v *OrbitView) buildVesselChipCompact(w *sim.World) []string {
	if !w.CraftVisibleHere() {
		return nil
	}
	c := w.ActiveCraft()
	fuelStr := fmt.Sprintf("%.0f kg", c.Fuel)
	if pct, kg, ok := activeStageFuel(c); ok {
		fuelStr = fmt.Sprintf("%.0f%% (%.0f kg)", pct, kg)
	}
	return []string{
		v.theme.Primary.Render("VESSEL") + "  " + crashedVesselNameLabel(v.theme, c),
		fmt.Sprintf("  fuel: %s  Δv: %.0f m/s", fuelStr, c.RemainingDeltaV()),
	}
}

// stagePips renders one glyph per stage — ● firing/fueled, ○ dry — the
// summary both buildStagesChip and its Compact Form share.
func stagePips(c *spacecraft.Spacecraft) string {
	var pips strings.Builder
	for _, st := range c.Stages {
		if st.FuelCapacity > 0 && st.FuelMass <= 0 {
			pips.WriteString("○")
		} else {
			pips.WriteString("●")
		}
	}
	return pips.String()
}

// buildStagesChip summarises the active craft's stage chain as a fuel-pip
// strip plus the active (bottom, firing) stage name and index. Returns nil
// for single-stage craft (the slim column already covers their propellant).
// Replaces the old per-stage NODES-height list (ADR 0010: the Stages chip
// summarises; per-stage detail stays a spawn-time concern).
func (v *OrbitView) buildStagesChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || len(c.Stages) <= 1 {
		return nil
	}
	active := c.Stages[0].Name
	if active == "" {
		active = c.Stages[0].LoadoutID
	}
	if active == "" {
		active = "stage 0"
	}
	return []string{
		v.theme.Primary.Render("STAGES"),
		fmt.Sprintf("  %s", stagePips(c)),
		v.theme.Warning.Render(fmt.Sprintf("  ▸ %s (1/%d)", active, len(c.Stages))),
	}
}

// buildStagesChipCompact is STAGES' Compact Form (ADR 0046 / #422): the
// pip strip alone, dropping the active-stage name/index row — the pips
// already answer "how many stages left, and is the one that's firing dry"
// at a glance.
func (v *OrbitView) buildStagesChipCompact(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || len(c.Stages) <= 1 {
		return nil
	}
	return []string{
		v.theme.Primary.Render("STAGES") + "  " + stagePips(c),
	}
}

// totalQueuedNodes sums every craft's planted-but-unfired node count.
// Used only to decide whether the NODES chip has anything to show at
// all (buildNodesChip's top-of-function relevance check) — a fleet
// with a node planted on ANY craft is a fleet where the chip belongs on
// screen, even if the active craft itself is currently node-free.
//
// #333: this total is deliberately NOT used for the force-show gate or
// the "(+N more)" overflow count anymore — see activeCraftQueuedNodes.
func totalQueuedNodes(w *sim.World) int {
	total := 0
	for _, c := range w.Crafts {
		if c != nil {
			total += len(c.Nodes)
		}
	}
	return total
}

// activeCraftQueuedNodes reports the ACTIVE craft's own planted-but-
// unfired node count (#333). The staleness hazard the NODES chip
// force-show exists to surface is per-vessel: every node behind the
// first ON THE SAME CRAFT was computed against an orbit that no longer
// exists once the first one fires. A different craft's queue doesn't
// stale this one, so summing across the fleet (the old totalQueuedNodes
// use here) force-showed the chip — bypassing a declutter + disabled
// toggle the player explicitly chose — for constellations where no
// single vessel actually carried the hazard. Returns 0 with no active
// craft.
func activeCraftQueuedNodes(w *sim.World) int {
	ac := w.ActiveCraft()
	if ac == nil {
		return 0
	}
	return len(ac.Nodes)
}

// buildNodesChip is the bottom-right burn-schedule chip: any in-flight
// burn(s) as the firing head, then the planted-node chain summarised as
// the next node plus a "(+N more → [m])" overflow count. The full
// enumerable/editable list lives on the maneuver screen ([m]); clicking
// this chip opens it (ADR 0010). Returns nil only when nothing is burning
// and no craft has a node planted.
//
// v0.16 folded the standalone ● BURNS chip in here so a live burn and the
// upcoming nodes read as one schedule. assembleChips force-shows the chip
// whenever a burn is live, so the safety-critical firing head is never
// hidden by the toggle or declutter.
func (v *OrbitView) buildNodesChip(w *sim.World) []string {
	burnLines := v.activeBurnLines(w)
	total := totalQueuedNodes(w)
	if len(burnLines) == 0 && total == 0 {
		return nil
	}
	lines := []string{v.theme.Primary.Render("NODES")}
	lines = append(lines, burnLines...)
	if total == 0 {
		return lines // a burn in flight with no upcoming planted nodes
	}
	nc, nci, ni, ok := nextQueuedNode(w)
	if ok {
		n := nc.Nodes[ni]
		kind := "imp"
		if n.Duration > 0 {
			kind = fmt.Sprintf("fin %.0fs", n.Duration.Seconds())
		}
		lines = append(lines, v.nextQueuedNodeLine(w, nc, nci, ni), "  "+kind)
		// #333: the overflow count is nc's OWN remaining queue, not the
		// fleet-wide total — mixing in another craft's nodes here would
		// describe a different vessel's queue as if it were stale on
		// THIS one.
		if craftTotal := len(nc.Nodes); craftTotal > 1 {
			lines = append(lines, v.theme.Dim.Render(fmt.Sprintf("  (+%d more → [m])", craftTotal-1)))
		}
	}
	return lines
}

// nextQueuedNode picks the node buildNodesChip (and its Compact Form)
// summarise below the firing head: the active craft's first node when it
// has one, else the first node on the first craft that has any. Shared so
// both forms always name the same node.
func nextQueuedNode(w *sim.World) (nc *spacecraft.Spacecraft, nci, ni int, ok bool) {
	if ac := w.ActiveCraft(); ac != nil && len(ac.Nodes) > 0 {
		return ac, w.ActiveCraftIdx, 0, true
	}
	for ci, c := range w.Crafts {
		if c != nil && len(c.Nodes) > 0 {
			return c, ci, 0, true
		}
	}
	return nil, 0, 0, false
}

// nextQueuedNodeLine formats nextQueuedNode's pick as one row: the click-
// affordance marker, a per-craft label when the fleet has more than one
// vessel, the node's event/countdown, its mode, and its Δv.
func (v *OrbitView) nextQueuedNodeLine(w *sim.World, nc *spacecraft.Spacecraft, nci, ni int) string {
	n := nc.Nodes[ni]
	// Over-budget Node (ADR 0047 §2 / #428): same shortfall wording as
	// the planner list, on both the full and Compact forms of the chip.
	over := ""
	if o := n.DV - nc.RemainingDeltaV(); o > 0 {
		over = "  " + v.theme.Alert.Render(fmt.Sprintf("⚠ exceeds budget by %.0f m/s", o))
	}
	label := fmt.Sprintf("#%d", ni+1)
	if len(w.Crafts) > 1 {
		label = fmt.Sprintf("c%d#%d", nci+1, ni+1)
	}
	if !n.IsResolved() {
		return fmt.Sprintf("  %s %s %s  %s  %.0f m/s",
			hudNodeMarker, label, n.Event.String(), n.Mode.String(), n.DV) + over
	}
	dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
	return fmt.Sprintf("  %s %s T%+.0fs  %s  %.0f m/s",
		hudNodeMarker, label, dt, n.Mode.String(), n.DV) + over
}

// buildNodesChipCompact is NODES' Compact Form (ADR 0046 / #422): the
// firing head (if a burn is live) plus the next queued node — dropping
// the burn/node's duration or "imp" kind row and the "(+N more)" overflow
// count. Those two rows are the two facts genuinely safety-critical to a
// pilot glancing at a shrunk chip: what's firing now, and what's next.
func (v *OrbitView) buildNodesChipCompact(w *sim.World) []string {
	burnLines := v.activeBurnLines(w)
	total := totalQueuedNodes(w)
	if len(burnLines) == 0 && total == 0 {
		return nil
	}
	lines := []string{v.theme.Primary.Render("NODES")}
	if len(burnLines) > 0 {
		lines = append(lines, burnLines[0]) // firing head only, drop a STALLED sub-row
	}
	if nc, nci, ni, ok := nextQueuedNode(w); ok {
		lines = append(lines, v.nextQueuedNodeLine(w, nc, nci, ni))
	}
	return lines
}
