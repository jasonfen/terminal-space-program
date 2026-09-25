package screens

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
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
	// cornerBay is the notice bay (ADR 0051 slice 3, re-grill Q5): a
	// bottom-middle stack centred between the left corner stack and the
	// navball, exempt from both sides' shared budgets (see
	// layoutChipsBySide). Every pop-up notice moves here in slice 3 item
	// 2; this prototype (item 1) carries just enough of the mechanism to
	// measure it before the overflow rule is built.
	cornerBay
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

// bayEntry is one notice queued for the bay (cornerBay), collected by
// composeChips' main placement loop and laid out as a block afterward by
// layoutBayFold / the bay section below; see their doc comments.
type bayEntry struct {
	id    settings.Chip
	lines []string
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

// leftChipFootprint is one top-left/bottom-left chip's placed rectangle,
// reduced to just what the bay's clamp needs: how far right it reached
// and how far down it went. See leftFootprints' doc comment in
// composeChips.
type leftChipFootprint struct {
	rightCol, bottomRow int
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
// Hidden Stub in their place. Chips with no content never participate:
// they have no footprint to budget.
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
		if c.neverShrink || len(c.lines) == 0 || c.corner == cornerBay {
			// exempt / no footprint / bay chip: never shrinks or drops.
			// The bay draws from its own row range above the Hint Strip
			// (composeChips), never either side's shared budget.
			continue
		}
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

	// leftFootprints records the right edge and bottom row of every
	// top-left/bottom-left chip placed this frame, in placement order, so
	// the bay (cornerBay) can centre itself in the gap between the left
	// stack and the navball (re-grill Q5) using only the box it actually
	// sits beside, rather than the widest box anywhere in the left stack
	// (review finding 2, second half, 2026-09-25): GUIDANCE (60 wide)
	// sits well above the bay's row band and its own bottom row is well
	// clear of the bay before the bay ever starts, so it must not shrink
	// the bay's budget just because it happens to be wider than MISSION,
	// the box the bay actually sits beside under decision 10's fixed
	// order. Updated by place() below.
	var leftFootprints []leftChipFootprint
	// bay collects cornerBay chips instead of placing them inline, so
	// they can be laid out after every other corner has claimed its
	// space this frame (leftFootprints is only final once the left stack
	// is done) and so the whole bay can be centred as one block rather
	// than chip-by-chip.
	var bay []bayEntry

	// place lays out one block (bordered chip content, or a bare one-row
	// Hidden Stub when bordered is false) at its corner's stacking cursor,
	// advancing that cursor, splicing it onto the canvas, and recording
	// its screen rect (skipped for a stub — it isn't a real chip a click
	// can route to).
	place := func(id settings.Chip, corner chipCorner, chipLines []string, bordered bool) {
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
			leftFootprints = append(leftFootprints, leftChipFootprint{rightCol: atCol + bw, bottomRow: atRow + bh - 1})
		case cornerTopRight:
			atRow, atCol = topRightRow, cCols-bw
			topRightRow += bh + chipGap
		case cornerBottomLeft:
			atRow, atCol = bottomLeftRow-bh+1, 0
			bottomLeftRow -= bh + chipGap
			leftFootprints = append(leftFootprints, leftChipFootprint{rightCol: atCol + bw, bottomRow: atRow + bh - 1})
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
		if chip.corner == cornerBay {
			// Deferred: laid out as a block below, once every other
			// corner has finished claiming space this frame.
			if len(chip.lines) == 0 {
				continue
			}
			bay = append(bay, bayEntry{id: chip.id, lines: chip.lines})
			continue
		}
		switch forms[i] {
		case chipFormHidden:
			continue
		case chipFormCompact:
			cl := chip.compact
			if cl == nil {
				cl = chip.lines
			}
			place(chip.id, chip.corner, cl, true)
		default:
			place(chip.id, chip.corner, chip.lines, true)
		}
	}

	// The bay (cornerBay, re-grill Q5 + slice 3 ruling 1): stacks upward
	// from the row above the Hint Strip, newest at the bottom, centred
	// between the left stack's right edge beside the bay's own row band
	// (bayLeftBound, below) and the navball's left edge (or the canvas
	// edge when the navball isn't showing this frame). Both its height
	// AND width are clamped against every box on both sides for the rows
	// it occupies (see layoutBayChips's doc comment for why the
	// right-side boxes (NAVIGATION/TARGET) are the real constraint the
	// item 1 measurement found, not the left stack).
	if len(bay) > 0 {
		navballLeft := cCols
		if navballReserved > chipStubHeight {
			navballLeft = cCols - navballPanelW
		}
		// bayLeftBound is the right edge of the left-stack box the bay
		// actually sits beside: the LAST top-left/bottom-left chip
		// placed this frame (MISSION, under decision 10's fixed order),
		// not leftStackMaxCol's widest-box-anywhere (usually GUIDANCE,
		// wider but rows above the bay entirely). bayTop starts at
		// topRightRow (NAVIGATION/TARGET's own bottom) rather than
		// leftStackMaxCol's row cursor, since the left stack's columns
		// don't reach the bay's span; it only widens (moves later) for
		// an earlier left box that both reaches past bayLeftBound in
		// columns AND ends below topRightRow in rows, i.e. actually
		// intersects the bay's rectangle (review finding 2, second
		// half, 2026-09-25: the previous version always took the left
		// stack's own bottom, 32 at 140x40 with Flight School on,
		// leaving only 4 rows of the ADR's 17-row bay budget).
		bayLeftBound := 0
		if n := len(leftFootprints); n > 0 {
			bayLeftBound = leftFootprints[n-1].rightCol
		}
		bayTop := topRightRow
		for _, f := range leftFootprints {
			if f.rightCol > bayLeftBound && f.bottomRow > bayTop {
				bayTop = f.bottomRow
			}
		}
		bayBottom := cRows - 2 // one row above the Hint Strip on cRows-1
		// The fold-and-wrap "stacker" (ruling 1 / ruling 2) is a Design
		// Size (140x40) contract, exactly like layoutChipsBySide's own
		// Graceful Shrink budgets: AT OR ABOVE it, nothing may ever
		// overlap a box. BELOW it, down to the Playable Floor, the bay
		// is exempt from the stacker (re-grill Q5) and may paint over a
		// box, same as the boxes' own budget already tolerates between
		// the Playable Floor and the Design Size. bayBudget/wrapWidth
		// effectively unbounded reproduces the old unclamped, unwrapped
		// bay exactly.
		bayBudget := bayBottom - bayTop + 1
		wrapWidth := navballLeft - bayLeftBound - 2
		// cCols/cRows are CANVAS dimensions (totalCols-2 by totalRows-3,
		// Resize above), not the terminal's own DesignWidth/DesignHeight
		// (140x40): at the Design Size the canvas is 138x37, so this
		// comparison against 140x40 was true at every size up to 141x42
		// and the bay was unclamped and unwrapped in production (review
		// finding 2, 2026-09-25). Compare against the canvas-equivalent
		// floor instead.
		if cCols < DesignWidth-2 || cRows < DesignHeight-3 {
			bayBudget = 1 << 30
			wrapWidth = 1 << 30
		}
		visible, foldedCount := layoutBayFold(bay, bayBudget, wrapWidth)

		bayRow := bayBottom
		rects := make([]*chipRect, len(bay))
		// Walk newest (last appended) first so it claims the bottom row,
		// but record each rect at its ORIGINAL index so chipRects comes
		// back in the same chip order every other corner uses (callers,
		// and tests, rely on that order to identify a chip).
		for i := len(bay) - 1; i >= 0; i-- {
			if !visible[i] {
				continue
			}
			wrapped := wrapBayLines(bay[i].lines, wrapWidth)
			padded, w := padChipBlock(wrapped)
			if len(padded) == 0 || w == 0 {
				continue
			}
			block := wrapBorder(strings.Join(padded, "\n"), w, v.theme.Primary.GetForeground())
			bw, bh := w+2, len(padded)+2
			centre := (bayLeftBound + navballLeft) / 2
			atCol := centre - bw/2
			if atCol < bayLeftBound {
				atCol = bayLeftBound
			}
			if atCol+bw > navballLeft {
				atCol = navballLeft - bw
			}
			if atCol < 0 {
				atCol = 0
			}
			atRow := bayRow - bh + 1
			lines = overlayStyledBlock(lines, block, atRow, atCol, cCols)
			rects[i] = &chipRect{
				id:       bay[i].id,
				colStart: atCol + screenColOffset,
				colEnd:   atCol + bw - 1 + screenColOffset,
				rowStart: atRow + screenRowOffset,
				rowEnd:   atRow + bh - 1 + screenRowOffset,
			}
			bayRow -= bh + chipGap
		}
		for _, r := range rects {
			if r != nil {
				v.chipRects = append(v.chipRects, *r)
			}
		}
		// The fold indicator (ruling 1): a bare one-row "▸ +N more" line
		// at the top of the visible bay stack, summarising every notice
		// that didn't fit: same Hidden-Stub shape as the side budgets'
		// "▸ +N hidden", but bay-specific wording ("more", not "hidden":
		// these will come back on their own once a newer notice clears,
		// where a side-budget drop is permanent for the frame it's on).
		if foldedCount > 0 && bayRow >= bayTop {
			text := v.theme.Dim.Render(fmt.Sprintf("▸ +%d more", foldedCount))
			bw := lipgloss.Width(text)
			centre := (bayLeftBound + navballLeft) / 2
			atCol := centre - bw/2
			if atCol < bayLeftBound {
				atCol = bayLeftBound
			}
			if atCol+bw > navballLeft {
				atCol = navballLeft - bw
			}
			if atCol < 0 {
				atCol = 0
			}
			lines = overlayStyledBlock(lines, text, bayRow, atCol, cCols)
		}
	}

	// Hidden Stubs (ADR 0046 §2): one per side that had a drop, rendered
	// after every admitted chip on that side so it reads as the last item
	// in the TOP stack — the corner where Core/Forced chips (VESSEL,
	// ORBIT) almost always already have a foothold, so the stub has a
	// natural anchor even when every chip that actually dropped lived in
	// the bottom stack.
	if n := stubs[sideLeft]; n > 0 {
		place("", cornerTopLeft, []string{v.theme.Dim.Render(fmt.Sprintf("▸ +%d hidden", n))}, false)
	}
	if n := stubs[sideRight]; n > 0 {
		place("", cornerTopRight, []string{v.theme.Dim.Render(fmt.Sprintf("▸ +%d hidden", n))}, false)
	}

	return strings.Join(lines, "\n")
}

// layoutBayFold (ADR 0051 slice 3 ruling 1) decides which bay entries
// render this frame given a row budget already clamped against every
// instrument box on both sides (composeChips computes that budget as
// bayBottom-bayTop+1, where bayTop is the lower of the left and right
// stacks' own final cursors, see its call site). Each entry's height is
// measured AFTER wrapping its lines to wrapWidth (ruling 2: "any line
// wider than the bay's columns wraps"), since wrapping can make an entry
// taller before it's ever decided whether it fits.
//
// If everything fits, nothing folds. Otherwise entries fold OLDEST FIRST
// (index 0 upward, assembleChips' append order, "newest last" by the
// same convention composeChips' placement already uses) until the
// remaining visible entries plus one row for the "▸ +N more" indicator
// fit the budget. This is deliberately stateless: every frame recomputes
// from whatever's actually present, so a notice that cleared on its own
// (SESSION's TTL, a chip builder returning nil) simply isn't in `bay`
// next frame and everything after it shifts back down without any
// "unfold" bookkeeping: re-grill's "the folded notice returns when a
// newer one clears" falls out of recomputing from scratch, not a special
// case.
func layoutBayFold(bay []bayEntry, budget, wrapWidth int) (visible []bool, folded int) {
	if budget < 0 {
		budget = 0
	}
	heights := make([]int, len(bay))
	for i, e := range bay {
		heights[i] = len(wrapBayLines(e.lines, wrapWidth)) + 2
	}
	visible = make([]bool, len(bay))
	for i := range visible {
		visible[i] = true
	}
	total := func() int {
		h, any := 0, false
		for i := range bay {
			if visible[i] {
				h += heights[i]
			} else {
				any = true
			}
		}
		if any {
			h++ // the "▸ +N more" row
		}
		return h
	}
	for i := 0; i < len(bay) && total() > budget; i++ {
		if visible[i] {
			visible[i] = false
			folded++
		}
	}
	if folded > 0 && total() > budget {
		// Even the one-row fold indicator doesn't fit this budget (an
		// extreme-narrow-canvas edge case): show nothing at all rather
		// than paint a stub that itself overruns the clamp.
		for i := range visible {
			visible[i] = false
		}
		folded = 0
	}
	return visible, folded
}

// wrapBayLines word-wraps every line in lines to at most maxWidth cells
// (ruling 2: "any line wider than the bay's columns wraps, so the picker
// grows taller rather than wider"). Operates cell-by-cell via
// splitStyledCells, so ANSI styling on any line survives the split:
// each cell splitStyledCells returns already carries its own complete
// SGR wrapper, so concatenating any contiguous subset back together is
// always safe (the same property overlayStyledBlock's splice relies on).
func wrapBayLines(lines []string, maxWidth int) []string {
	if maxWidth < 1 {
		maxWidth = 1
	}
	var out []string
	for _, l := range lines {
		out = append(out, wrapBayLine(l, maxWidth)...)
	}
	return out
}

// cellIsBlank reports whether a splitStyledCells cell's own rendered
// character is a plain space, regardless of any SGR wrapper around it.
func cellIsBlank(cell string) bool {
	const sgrReset = "\x1b[0m"
	if strings.HasSuffix(cell, sgrReset) {
		cell = cell[:len(cell)-len(sgrReset)]
	}
	return cell == " "
}

// wrapBayLine wraps one line to at most maxWidth cells, breaking on the
// last blank cell at or before the limit when one exists (never mid-word
// unless a single word alone exceeds maxWidth). Continuation lines repeat
// the original line's own leading indent (its run of leading blank
// cells, capped so it never eats the whole budget) so a wrapped notice
// still reads as one paragraph. Returns the line unchanged, as a single-
// element slice, when it already fits.
func wrapBayLine(line string, maxWidth int) []string {
	cells := splitStyledCells(line)
	if len(cells) <= maxWidth {
		return []string{line}
	}
	indent := 0
	for indent < len(cells) && indent < maxWidth-1 && cellIsBlank(cells[indent]) {
		indent++
	}
	indentStr := strings.Repeat(" ", indent)
	var out []string
	i := 0
	for i < len(cells) {
		avail := maxWidth
		prefix := ""
		if len(out) > 0 {
			prefix = indentStr
			avail -= indent
			if avail < 1 {
				avail = 1
			}
		}
		if len(cells)-i <= avail {
			out = append(out, prefix+strings.Join(cells[i:], ""))
			break
		}
		end := i + avail
		brk := end
		for brk > i && !cellIsBlank(cells[brk]) {
			brk--
		}
		if brk == i {
			brk = end // no blank to break on: hard-break
		}
		out = append(out, prefix+strings.Join(cells[i:brk], ""))
		i = brk
		for i < len(cells) && cellIsBlank(cells[i]) {
			i++
		}
	}
	return out
}

// navballReservedRows reports how many bottom rows the navball panel
// occupies on the canvas this frame, so the bottom-right Nodes chip can
// stack above it. Mirrors the gate in composeNavballOverlay; the +1
// matches the one-row bottom lift there.
//
// The floor is 1, never 0: row cRows-1 carries the Hint Strip
// (paintHintStrip), painted unconditionally regardless of navball state,
// and bottomLeftRow already stays off that row unconditionally (cRows-2,
// "above the view: label"). Before this fix bottom-right chips had no
// equivalent floor whenever the navball itself was absent
// (!CraftVisibleHere, a too-small canvas, or no sub-observer), so a wide
// enough NODES chip in exactly that state could paint over the Hint
// Strip's tail: a real Design Size (140x40) collision the ADR 0049 stage
// A2 gate review measured (the node row's own contract-mandated widening
// was what tipped it over the edge; see impl-notes/item4-A2-migration.md).
func (v *OrbitView) navballReservedRows(w *sim.World, cCols, cRows int) int {
	if !w.CraftVisibleHere() || cCols < navballPanelW+2 || cRows < navballPanelH+2 {
		return 1
	}
	if _, _, ok := w.NavballSubObserver(); !ok {
		return 1
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

// throttleRow renders VESSEL's "throttle:" row (decision 1, grilled
// 2026-09-06: "the throttle row carries the engine state, both ways").
// The number itself is always the throttle *setting*
// (c.EffectiveThrottle()), never gated — only the trailing suffix names
// whether an engine is actually lit: "(idle)" in Dim while neither
// ActiveBurn nor ManualBurn is live on this craft, "● FIRING" in Warning
// the instant either is. Mirrors the same live/idle gate
// PredictedFinalOrbit (internal/sim/maneuver.go) and buildTargetChip use
// for "is this craft actually thrusting right now".
func (v *OrbitView) throttleRow(c *spacecraft.Spacecraft) string {
	base := fmt.Sprintf("  throttle:  %.0f%%", c.EffectiveThrottle()*100)
	// Code-review finding 5: reuse sim.StackMidBurn's exact "is this craft
	// actively thrusting" predicate (ActiveBurn != nil || ManualBurn !=
	// nil) rather than an inline copy, so this row's notion of thrusting
	// can't silently drift from the rest of the codebase's (it already
	// gates Transfer Control refusal, ADR 0034 addendum).
	if !sim.StackMidBurn(c) {
		return base + v.theme.Dim.Render(" (idle)")
	}
	return base + v.theme.Warning.Render(" ● FIRING")
}

// vesselBurnBadge is the VESSEL chip header's `● BURN` badge (decision
// 2, grilled 2026-09-06: "the whole screen says an engine is lit").
// Originally a canvas-border color swap plus a title-bar badge — a
// live playtest found that whole-screen treatment too loud, so it now
// lives here instead: on the one chip that's already universal across
// the map, the launch/chase-cam view (shared via LaunchView.hudSource),
// and a landed vessel, and that already carries the active craft's own
// engine state (throttleRow's "(idle)"/"● FIRING"). Gated on
// AnyCraftThrusting (the whole-slate predicate the 10x burn-warp cap
// also uses), not just the active craft, so it still answers "why is
// warp capped" even when the burning craft isn't the one on screen.
// Returns "" (no leading gap, so it costs nothing appended to a plain
// string) when nothing in the slate is thrusting.
func (v *OrbitView) vesselBurnBadge(w *sim.World) string {
	if !w.AnyCraftThrusting() {
		return ""
	}
	return v.theme.Warning.Render("  ● BURN")
}

// buildEmptySlateChip recovers #310's retired VESSEL-chip messaging (see
// git log -S TestEmptySlateSaysSo: the ADR 0051 box consolidation
// removed the chip carrying it, leaving every box read a bare dash row
// with no explanation and no way out). Renders only while there is no
// active craft at all: a docked-as-guest slate is a known, explained
// situation ("launch a new flight" would be the wrong advice there), a
// genuinely empty slate is not.
func (v *OrbitView) buildEmptySlateChip(w *sim.World) []string {
	if w.ActiveCraft() != nil {
		return nil
	}
	if dg := w.DockGuest; dg != nil {
		return []string{
			v.theme.Primary.Render("VESSEL"),
			"  " + v.theme.Warning.Render("docked in "+dg.OwnerHandle+"'s stack"),
			v.theme.Dim.Render("  [U] release it"),
		}
	}
	return []string{
		v.theme.Primary.Render("NO VESSEL"),
		"  " + v.theme.Warning.Render("your vessel slate is empty"),
		v.theme.Dim.Render("  [n] launch a new flight"),
	}
}

// buildVesselDestroyedChip is the VESSEL DESTROYED Standing Alert (#427 /
// ADR 0048 decision 1): renders only while the active craft is Crashed,
// naming both exits — [E] end flight (removes the wreckage) and [F9]
// quickload (the fastest way back to a flying vessel). Already
// chip-sized (title + one row), so it carries no separate Compact Form —
// layoutChipsBySide treats a nil compact as "full IS compact".
//
// Callers append it OUTSIDE the assembleChips add/addC/addPriority
// helpers (like the live-burn NODES chip) so it bypasses chipEnabled —
// and therefore both the Settings toggle (moot: there is nothing to
// toggle here) and F2 Declutter — entirely. A destroyed vessel with no
// other on-screen notice is precisely the case a Standing Alert exists
// for: CONTEXT.md's own rule is "a Flash must never be the only notice
// of a state that persists."
func (v *OrbitView) buildVesselDestroyedChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || !c.Crashed || !w.CraftVisibleHere() {
		return nil
	}
	return []string{
		v.theme.Alert.Render("VESSEL DESTROYED"),
		"  [E] end flight  [F9] quickload",
	}
}

// deltaVReadout renders the VESSEL chip's Δv row per ADR 0049 decision 7:
// the active stage's remaining Δv, then the whole remaining stack's total
// (spacecraft.StackStats over the stages still attached), sharing one
// trailing unit ("3518 / 9412 m/s"). A single-stage vessel prints one
// number instead ("3518 m/s"); there is no separate vehicle total to name.
func deltaVReadout(c *spacecraft.Spacecraft) string {
	stage := c.RemainingDeltaV()
	if len(c.Stages) <= 1 {
		return readout.DeltaV(stage)
	}
	vehicle := spacecraft.StackStats(c.Stages).TotalDV
	return readout.DeltaVPair(stage, vehicle)
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
