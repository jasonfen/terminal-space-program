package screens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// chipTier groups the eight instrument boxes into three tiers that each
// draw at one shared width (ADR 0051 amendment 2026-09-28, W1, issue
// #482 S1): the left stack's top three (ENGINE, PROPELLANT, GUIDANCE),
// its bottom three (COMMS, STAGES, MISSION), and the right pair
// (NAVIGATION, TARGET). A tier's width is fixed, never a function of the
// frame, so pressing t, planting a node or starting a burn changes
// numbers and never outlines.
type chipTier int

const (
	chipTierNone chipTier = iota // notices, PROXIMITY, anything not an instrument box
	chipTierTopLeft
	chipTierBottomLeft
	chipTierRight
)

// Tier widths, OUTER (border included). They are pinned to the widest row
// each tier ever draws, and TestChipTierWidthsAreDerivedFromFixtures
// re-derives them from every phase fixture, every Flight School step and
// challenge mission, every loadout's STAGES row and every COMMS reading,
// failing in BOTH directions: a longer reading fails the guard rather
// than poking past the edge, and a shortened one fails it so the pin is
// retuned rather than left stale. Set by:
//   - top-left 56: ENGINE's STALLED live-burn row in a frame mode
//     ("▸ Surf Retrograde, Δv 12345 m/s  ⚠ STALLED"); the queued-node
//     rows are shorter since "⚠" and "+N [m]" moved to the title row
//   - bottom-left 46: MISSION's wrapped tutorial rows (wrap width 40 plus
//     the 4-cell indent, 44 content)
//   - right 64: NAVIGATION with a plan planted
const (
	tierTopLeftWidth    = 56
	tierBottomLeftWidth = 46
	tierRightWidth      = 64
)

// outerWidth is the tier's drawn width including the 2-cell border, 0 for
// chipTierNone.
func (t chipTier) outerWidth() int {
	switch t {
	case chipTierTopLeft:
		return tierTopLeftWidth
	case chipTierBottomLeft:
		return tierBottomLeftWidth
	case chipTierRight:
		return tierRightWidth
	}
	return 0
}

// tierPad right-pads every line to the tier's content width. A row wider
// than the tier is never cut: the block grows to fit it for that frame
// (the guard test names the readings known to do so).
func tierPad(lines []string, t chipTier) []string {
	want := t.outerWidth() - 2
	if want <= 0 {
		return lines
	}
	out := make([]string, len(lines))
	for i, l := range lines {
		if pad := want - lipgloss.Width(l); pad > 0 {
			out[i] = l + strings.Repeat(" ", pad)
		} else {
			out[i] = l
		}
	}
	return out
}
