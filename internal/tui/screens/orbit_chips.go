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

// builtChip is one composited overlay: its Settings id (empty = always
// enabled, e.g. the safety-critical BURNS readout), the corner it anchors
// to, and its already-styled content lines (header + rows). Relevance is
// decided by the builder returning nil when the chip has nothing to show.
type builtChip struct {
	id     settings.Chip
	corner chipCorner
	lines  []string
}

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
// corner so adjacent overlays read as distinct panels over the canvas.
const chipGap = 1

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
// "view:" label on the last row.
func (v *OrbitView) composeChips(canvasStr string, cCols, cRows, navballReserved, screenColOffset, screenRowOffset int, chips []builtChip) string {
	v.chipRects = v.chipRects[:0]
	lines := strings.Split(canvasStr, "\n")

	// Per-corner stacking cursors. Top corners grow downward from their
	// start row; bottom corners grow upward from their start row. v0.13:
	// the "focus:" label left (0,0) for the title bar, so top-left chips
	// now start at row 0.
	topLeftRow := 0
	topRightRow := 0
	bottomLeftRow := cRows - 2 // above the "view:" label on row cRows-1
	bottomRightRow := cRows - 1 - navballReserved

	for _, chip := range chips {
		padded, w := padChipBlock(chip.lines)
		h := len(padded)
		if h == 0 || w == 0 {
			continue
		}
		var atRow, atCol int
		switch chip.corner {
		case cornerTopLeft:
			atRow, atCol = topLeftRow, 0
			topLeftRow += h + chipGap
		case cornerTopRight:
			atRow, atCol = topRightRow, cCols-w
			topRightRow += h + chipGap
		case cornerBottomLeft:
			atRow, atCol = bottomLeftRow-h+1, 0
			bottomLeftRow -= h + chipGap
		case cornerBottomRight:
			atRow, atCol = bottomRightRow-h+1, cCols-w
			bottomRightRow -= h + chipGap
		}
		if atCol < 0 {
			atCol = 0
		}
		lines = overlayStyledBlock(lines, strings.Join(padded, "\n"), atRow, atCol, cCols)
		v.chipRects = append(v.chipRects, chipRect{
			id:       chip.id,
			colStart: atCol + screenColOffset,
			colEnd:   atCol + w - 1 + screenColOffset,
			rowStart: atRow + screenRowOffset,
			rowEnd:   atRow + h - 1 + screenRowOffset,
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
// BURNS) report their empty id; callers match against specific ids.
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
		return nil
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

// buildNodesChip summarises the planted-node chain as the next node plus a
// "(+N more → [m])" overflow count, bounding what was an unbounded
// per-node list. The full enumerable/editable list lives on the maneuver
// screen ([m]); clicking this chip opens it (ADR 0010). Returns nil when
// no craft has a node planted.
func (v *OrbitView) buildNodesChip(w *sim.World) []string {
	total := 0
	for _, c := range w.Crafts {
		if c != nil {
			total += len(c.Nodes)
		}
	}
	if total == 0 {
		return nil
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
	lines := []string{v.theme.Primary.Render("NODES")}
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
		lines = append(lines, line, "  "+kind)
	}
	if total > 1 {
		lines = append(lines, v.theme.Dim.Render(fmt.Sprintf("  (+%d more → [m])", total-1)))
	}
	return lines
}
