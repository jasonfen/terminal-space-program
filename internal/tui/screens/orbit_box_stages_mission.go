package screens

import (
	"fmt"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// orbit_box_stages_mission.go — the STAGES and MISSION instrument boxes
// (ADR 0051 decisions 1, 5, 5b, 10 of the re-grill / decision 14 here,
// slice 2a). Both sit at the bottom of the left stack in the ruled
// order: STAGES directly under COMMS, MISSION last (sized to its step,
// re-grill Q10) so a rung change never moves anything below it — nothing
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
	return []string{fmt.Sprintf("%s  %s  %s", title, stagePips(c),
		v.theme.Warning.Render(fmt.Sprintf("▸ %s (1/%d)", active, len(c.Stages))))}
}

// buildMissionBox is MISSION, the last box in the left stack, sized to
// its own step rather than padded to a fixed height (re-grill Q10):
// nothing sits below it, so a rung change (3 to 7 rows) moves nothing
// else on screen. Reuses the retired-chip-era sendoffChipLines/
// missionChipLines content selectors verbatim (their branching — fail
// flash, ladder sendoff, active objective — is unaffected by the box
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
