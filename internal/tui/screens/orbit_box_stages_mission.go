package screens

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// orbit_box_stages_mission.go, the STAGES and MISSION instrument boxes
// (ADR 0051 decisions 1, 5, 5b, 10 of the re-grill / decision 14 here,
// slice 2a). Both sit at the bottom of the left stack in the ruled
// order: STAGES directly under COMMS, MISSION last (sized to its step,
// re-grill Q10) so a rung change never moves anything below it, nothing
// does, it's the last box.

// buildStagesBox summarises the active craft's stage chain as one
// combined title+pips+active-stage line (matching the measured mock,
// `R-40a`: "STAGES  ●●●  ▸ S-IC (1/3)"), fixed at exactly 3 total rows
// (1 content line + 2 borders). Unlike the retired buildStagesChip, this
// renders for every vessel including a single-stage one (decision 5:
// "COMMS, MISSION, STAGES and TARGET are drawn for every vessel"), not
// just craft with more than one stage.
func (v *OrbitView) buildStagesBox(w *sim.World) []string {
	title := v.theme.Primary.Render("STAGES")
	c := w.ActiveCraft()
	if c == nil || len(c.Stages) == 0 {
		return []string{title + "  —"}
	}
	active := c.Stages[0].Name
	if active == "" {
		active = c.Stages[0].LoadoutID
	}
	if active == "" {
		active = "stage 0"
	}
	// The row can never outgrow the bottom-left tier (W1, #482 review F2):
	// the pips are capped, and the stage name takes whatever content
	// width remains after the pips and the "(1/N)" count.
	pips := stagePips(c)
	pips = truncateCells(pips, stagePipsMax)
	count := fmt.Sprintf(" (1/%d)", len(c.Stages))
	// ADR 0052 decision 5: name the key that drops this stage, standing
	// (not a flash), so a thumb on the space bar can see what it is about
	// to do. A lone stage cannot be dropped (space arms a chute instead),
	// so it carries no suffix. The suffix is never truncated; the name
	// gives way instead.
	keyHint := ""
	if len(c.Stages) > 1 {
		keyHint = " [space]"
		// Review LOW 58: under a standing dry order that one press also
		// lights the next engine at the standing throttle; say so.
		if sim.StackDryArmed(c) {
			keyHint = " [space] relights"
		}
	}
	fixed := lipgloss.Width("STAGES") + 2 + lipgloss.Width(pips) + 2 + lipgloss.Width("▸ ") + lipgloss.Width(count) + lipgloss.Width(keyHint)
	active = truncateCells(active, tierBottomLeftWidth-2-fixed)
	return []string{fmt.Sprintf("%s  %s  %s", title, pips,
		v.theme.Warning.Render(fmt.Sprintf("▸ %s%s%s", active, count, keyHint)))}
}

// stagePipsMax is the STAGES pips' width cap in cells; past it the last
// cell becomes "…". Pips run active stage first, so the cap keeps the
// stages nearest the burn; the (1/N) count carries the total.
const stagePipsMax = 8

// truncateCells cuts s to at most n display cells, ending in "…" when it
// had to cut. n < 1 yields "".
func truncateCells(s string, n int) string {
	if n < 1 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	return ansi.Truncate(s, n, "…")
}

// buildMissionBox is MISSION, the last box in the left stack, sized to
// its own step rather than padded to a fixed height (re-grill Q10):
// nothing sits below it, so a rung change (3 to 7 rows) moves nothing
// else on screen. Reuses the retired-chip-era sendoffChipLines/
// missionChipLines content selectors verbatim (their branching, fail
// flash, ladder sendoff, active objective, is unaffected by the box
// migration); the only change is the fallback for "nothing to report",
// which the old chip signalled by returning nil (dropping the chip
// entirely) and this box instead prints as a dash line (decision 2).
func (v *OrbitView) buildMissionBox(w *sim.World) []string {
	flash, flashing := w.MissionFailFlash()
	if !flashing {
		if text, offer, ok := w.LadderSendoff(); ok {
			return v.sendoffChipLines(text, offer)
		}
	}
	if lines := v.missionChipLines(flash, flashing, w.ActiveMission(), w.ConnectedRelayCount()); lines != nil {
		return lines
	}
	return []string{v.theme.Primary.Render("MISSION") + "  —"}
}
