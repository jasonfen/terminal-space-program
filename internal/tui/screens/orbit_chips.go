package screens

import (
	"fmt"
	"sort"
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

// builtChip is one composited overlay: its Settings id (empty = always-on,
// non-toggleable — the safety-critical ● BURNS readout and the ORBIT
// metrics chip), the corner it anchors to, and its already-styled content
// lines (header + rows). Relevance is decided by the builder returning nil
// when the chip has nothing to show. Always-on chips are still hidden by
// declutter; only the pinned VESSEL core chip survives it.
type builtChip struct {
	id     settings.Chip
	corner chipCorner
	lines  []string
	// leftOfPrev places this chip on the same top row as the previously
	// placed chip in the same (top-right) corner, immediately to its left,
	// instead of stacking below it — so two chips share a row band rather
	// than growing the column's height. Used for PROJECTED ORBIT beside the
	// always-on ORBIT chip so the top-right column stays short enough to
	// leave room for TARGET above the bottom-right NODES chip. Honoured only
	// for cornerTopRight; ignored (normal stacking) when there's no prior
	// top-right chip to sit beside.
	leftOfPrev bool
	// priority controls which chips survive when a corner's stack would
	// otherwise overflow the canvas (#328/#334 — see admitChipsByBudget).
	// Defaults to chipPriorityNormal; the shared "add" helper in
	// assembleChips never sets it, so every ordinary toggleable chip
	// competes on equal footing and only chipPriorityCore/chipPriorityForced
	// chips are called out explicitly.
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

// Priority tiers for admitChipsByBudget (#328/#334). Highest first:
//
//   - chipPriorityCore (100): unconditionally pinned core telemetry that
//     predates chip toggles entirely — VESSEL and the top-right ORBIT
//     metrics chip. Never gated by Settings or declutter, so it must
//     never be silently dropped for space either.
//   - chipPriorityForced (90): chips a game-state rule force-shows past
//     the Settings toggle AND F2 declutter, because losing them
//     silently would hide a safety/continuity fact the player has no
//     other way to see: DOCKED (ADR 0038 S4 — the rider's only
//     surviving route to [J]/[U] once absorbed into another player's
//     stack, #328) and NODES while force-shown (a live burn, or more
//     than one queued node on the active craft, #333/#334).
//   - chipPriorityNormal (0, the zero value): every ordinary toggleable/
//     contextual chip. Competes for whatever budget chipPriorityCore
//     and chipPriorityForced chips leave behind; the first class
//     dropped when a corner would overflow.
//
// chipPriorityForced and up are "critical": admitChipsByBudget never
// drops them for space, and composeChips clamps their placement onto
// the canvas as a last resort (accepting an overlap with whatever's
// beneath) rather than let them run off-canvas and be silently clipped
// by overlayStyledBlock. Everything below that line is dropped whole
// rather than partially rendered — a corner that doesn't fit shows a
// clean, shorter stack, never a lone border fragment.
const (
	chipPriorityCore     = 100
	chipPriorityForced   = 90
	chipPriorityNormal   = 0
	chipPriorityCritical = chipPriorityForced // admission threshold
)

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

// admitChipsByBudget decides, independently per corner, which of chips
// survive this frame without that corner's stack running off the canvas
// (#328/#334). composeChips used to have no height bound at all:
// topLeftRow / bottomRightRow could walk past the canvas edge with
// nothing to stop them, and overlayStyledBlock silently drops any row
// outside [0, cRows) — so an always-on chip late in a corner's stack
// (DOCKED, appended after VESSEL/MISSION/SESSION/TIME LOCK, #328) or one
// squeezed against the navball reservation (NODES, #334) could vanish
// with no signal anything was lost.
//
// Each corner gets a real budget: the number of rows it can stack into
// before hitting the opposite edge of the canvas (or the navball, for
// bottom-right). Candidates are considered highest builtChip.priority
// first — ties keep their original relative order (a stable sort), so a
// corner that fits comfortably within budget is admitted in full and
// composeChips lays it out identically to a flat, unconditional append.
// A chip that would push the running total past budget is dropped
// (not admitted) rather than placed and clipped; a smaller, later chip
// in priority order can still land in whatever budget remains. Chips at
// chipPriorityForced or above ("critical") are always admitted
// regardless of budget — composeChips clamps their placement onto the
// canvas as a last resort instead of dropping them, since losing DOCKED
// or a force-shown NODES chip silently is worse than an overlap.
// leftOfPrev chips don't grow their corner's column (they share a row
// band with the chip beside them), so they bypass the budget check
// entirely and are always admitted.
func admitChipsByBudget(chips []builtChip, cRows, navballReserved int) []bool {
	budget := map[chipCorner]int{
		cornerTopLeft:     cRows,
		cornerTopRight:    cRows,
		cornerBottomLeft:  cRows - 1,
		cornerBottomRight: cRows - navballReserved,
	}
	admitted := make([]bool, len(chips))
	type candidate struct {
		idx      int
		priority int
		bh       int
	}
	byCorner := make(map[chipCorner][]candidate)
	for i, c := range chips {
		if c.leftOfPrev {
			admitted[i] = true
			continue
		}
		if len(c.lines) == 0 {
			admitted[i] = true // no footprint; the render loop skips it too
			continue
		}
		bh := len(c.lines) + 2 // + the chip's own border
		byCorner[c.corner] = append(byCorner[c.corner], candidate{idx: i, priority: c.priority, bh: bh})
	}
	for _, cands := range byCorner {
		sort.SliceStable(cands, func(a, b int) bool { return cands[a].priority > cands[b].priority })
		used := 0
		for _, c := range cands {
			critical := c.priority >= chipPriorityCritical
			if critical || used+c.bh <= budget[chips[c.idx].corner] {
				admitted[c.idx] = true
				used += c.bh
			}
		}
	}
	return admitted
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
// "view:" label on the last row. admitChipsByBudget decides which chips
// survive an overflowing corner before this loop ever runs; chips it
// drops are skipped here, and chips it keeps despite exceeding budget
// (chipPriorityCritical and up) are clamped onto the canvas rather than
// let overlayStyledBlock silently clip them off an edge.
func (v *OrbitView) composeChips(canvasStr string, cCols, cRows, navballReserved, screenColOffset, screenRowOffset int, chips []builtChip) string {
	v.chipRects = v.chipRects[:0]
	lines := strings.Split(canvasStr, "\n")
	admitted := admitChipsByBudget(chips, cRows, navballReserved)

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

	for i, chip := range chips {
		if !admitted[i] {
			continue
		}
		padded, w := padChipBlock(chip.lines)
		if len(padded) == 0 || w == 0 {
			continue
		}
		// Wrap each chip in a single-cell rounded border so every panel
		// reads as a distinct framed box over the canvas. The frame adds
		// one cell on each side, so the placed block is w+2 × h+2.
		block := wrapBorder(strings.Join(padded, "\n"), w, v.theme.Primary.GetForeground())
		bw, bh := w+2, len(padded)+2
		var atRow, atCol int
		switch chip.corner {
		case cornerTopLeft:
			atRow, atCol = topLeftRow, 0
			topLeftRow += bh + chipGap
		case cornerTopRight:
			if chip.leftOfPrev && haveTR {
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
		// Last-resort clamp (#328/#334): a critical chip is always
		// admitted above even when it overruns its corner's budget (two
		// critical chips together can still exceed cRows). Rather than
		// let it run off the canvas edge and have overlayStyledBlock
		// silently drop those rows, pull it back on-canvas — accepting
		// an overlap with whatever's beneath rather than losing it.
		// Non-critical chips never need this: admitChipsByBudget only
		// admits them when they already fit.
		if chip.priority >= chipPriorityCritical {
			switch chip.corner {
			case cornerTopLeft, cornerTopRight:
				if atRow+bh > cRows {
					atRow = cRows - bh
				}
			case cornerBottomLeft, cornerBottomRight:
				if atRow < 0 {
					atRow = 0
				}
			}
		}
		lines = overlayStyledBlock(lines, block, atRow, atCol, cCols)
		v.chipRects = append(v.chipRects, chipRect{
			id:       chip.id,
			colStart: atCol + screenColOffset,
			colEnd:   atCol + bw - 1 + screenColOffset,
			rowStart: atRow + screenRowOffset,
			rowEnd:   atRow + bh - 1 + screenRowOffset,
		})
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
	var pips strings.Builder
	for _, st := range c.Stages {
		if st.FuelCapacity > 0 && st.FuelMass <= 0 {
			pips.WriteString("○")
		} else {
			pips.WriteString("●")
		}
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
		fmt.Sprintf("  %s", pips.String()),
		v.theme.Warning.Render(fmt.Sprintf("  ▸ %s (1/%d)", active, len(c.Stages))),
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
	// The "next" node is the active craft's first node when it has one,
	// else the first node on the first craft that has any.
	var (
		nc      *spacecraft.Spacecraft
		nci, ni int
	)
	if ac := w.ActiveCraft(); ac != nil && len(ac.Nodes) > 0 {
		nc, nci, ni = ac, w.ActiveCraftIdx, 0
	} else {
		for ci, c := range w.Crafts {
			if c != nil && len(c.Nodes) > 0 {
				nc, nci, ni = c, ci, 0
				break
			}
		}
	}
	if nc != nil {
		n := nc.Nodes[ni]
		kind := "imp"
		if n.Duration > 0 {
			kind = fmt.Sprintf("fin %.0fs", n.Duration.Seconds())
		}
		label := fmt.Sprintf("#%d", ni+1)
		if len(w.Crafts) > 1 {
			label = fmt.Sprintf("c%d#%d", nci+1, ni+1)
		}
		var line string
		if !n.IsResolved() {
			line = fmt.Sprintf("  %s %s %s  %s  %.0f m/s",
				hudNodeMarker, label, n.Event.String(), n.Mode.String(), n.DV)
		} else {
			dt := n.TriggerTime.Sub(w.Clock.SimTime).Seconds()
			line = fmt.Sprintf("  %s %s T%+.0fs  %s  %.0f m/s",
				hudNodeMarker, label, dt, n.Mode.String(), n.DV)
		}
		// Over-budget Node (ADR 0047 §2 / #428): same shortfall wording
		// as the planner's PLANNED NODES list, so the plan's
		// affordability is visible without opening [m].
		if over := n.DV - nc.RemainingDeltaV(); over > 0 {
			line += "  " + v.theme.Alert.Render(fmt.Sprintf("⚠ exceeds budget by %.0f m/s", over))
		}
		lines = append(lines, line, "  "+kind)
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
