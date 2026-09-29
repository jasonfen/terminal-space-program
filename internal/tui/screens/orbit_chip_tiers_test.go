// orbit_chip_tiers_test.go (issue #482 S1, ADR 0051 amendment W1): keeps
// the three tier widths honest. Two guards:
//
//   - TestChipTierWidthsAreDerivedFromFixtures measures the widest row of
//     every reading each tier can draw and requires the pinned width to
//     equal it (a longer reading is red, a shortened one is red until the
//     pin is retuned).
//   - TestChipTierEdgesLineUpWhenRendered measures RENDERED rectangles
//     from Render, per phase and size, so it fails if placement ever
//     stops honouring the pin, not just if the pin is wrong.
package screens

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

type tierReading struct {
	tier   chipTier
	width  int // OUTER width: content + 2 border cells
	source string
}

// tierReadings returns every reading fixture, transient alarms included.
func tierReadings(t *testing.T) []tierReading {
	t.Helper()
	var steady []tierReading
	v := NewOrbitView(launchThemeForTest())
	add := func(dst *[]tierReading, tier chipTier, source string, lines []string) {
		for _, l := range lines {
			*dst = append(*dst, tierReading{tier, lipgloss.Width(l) + 2, fmt.Sprintf("%s: %q", source, l)})
		}
	}
	for name, w := range allDensityPhases(t) {
		add(&steady, chipTierTopLeft, "ENGINE/"+name, v.buildEngineBox(w))
		add(&steady, chipTierTopLeft, "PROPELLANT/"+name, v.buildPropellantBox(w))
		add(&steady, chipTierTopLeft, "GUIDANCE/"+name, v.buildGuidanceBox(w))
		add(&steady, chipTierRight, "NAVIGATION/"+name, v.buildNavigationBox(w))
		add(&steady, chipTierRight, "TARGET/"+name, v.buildTargetBox(w))
		add(&steady, chipTierBottomLeft, "COMMS/"+name, v.buildCommsBox(w))
		add(&steady, chipTierBottomLeft, "STAGES/"+name, v.buildStagesBox(w))
		add(&steady, chipTierBottomLeft, "MISSION/"+name, v.buildMissionBox(w))
	}

	// Every mission (Flight School steps, challenge missions), every
	// objective as the current one.
	cat, err := missions.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range cat.Missions {
		for cur := range m.Objectives {
			mm := m
			mm.Objectives = append([]missions.Objective(nil), m.Objectives...)
			for j := range mm.Objectives {
				mm.Objectives[j].Status = missions.InProgress
				if j < cur {
					mm.Objectives[j].Status = missions.Passed
				}
			}
			mm.Status = missions.InProgress
			add(&steady, chipTierBottomLeft, fmt.Sprintf("MISSION/%s obj %d", m.ID, cur), v.missionChipLines("", false, &mm, 0))
		}
		// The failure flash, with each declared fail reason's label.
		for _, reason := range []missions.FailCondition{missions.FailCrashed, missions.FailOutOfFuel, ""} {
			msg := m.Name + " failed"
			if reason != "" {
				msg += ": " + reason.Label()
			}
			add(&steady, chipTierBottomLeft, fmt.Sprintf("MISSION flash/%s/%s", m.ID, reason), v.missionChipLines(msg, true, nil, 0))
		}
	}
	// Ladder sendoffs.
	add(&steady, chipTierBottomLeft, "MISSION sendoff (ladder complete)", v.sendoffChipLines("CHALLENGE LADDER COMPLETE", false))
	add(&steady, chipTierBottomLeft, "MISSION sendoff (school complete)", v.sendoffChipLines("FLIGHT SCHOOL COMPLETE", true))

	// Every loadout's STAGES row.
	for _, id := range spacecraft.LoadoutOrder {
		w := densityCoastingWithPlan(t)
		w.Crafts[w.ActiveCraftIdx] = spacecraft.NewFromLoadout(id)
		add(&steady, chipTierBottomLeft, "STAGES/loadout "+id, v.buildStagesBox(w))
	}

	// COMMS statuses, including the uncrewed NO SIGNAL alarm rows: COMMS is
	// pinned at one row, so they must fit the tier rather than wrap.
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutOrder[0])
	add(&steady, chipTierBottomLeft, "COMMS connected", []string{v.commsBoxStatusLine(c, 3, true, 0)})
	add(&steady, chipTierBottomLeft, "COMMS direct", []string{v.commsBoxStatusLine(c, 1, true, 0)})
	c.Crewed = true
	add(&steady, chipTierBottomLeft, "COMMS crewed no signal", []string{v.commsBoxStatusLine(c, 0, false, 0)})
	c.Crewed, c.Controllable = false, true
	for _, reason := range []sim.CommDisconnectReason{0, sim.CommDisconnectBlocked, sim.CommDisconnectOutOfRange} {
		add(&steady, chipTierBottomLeft, fmt.Sprintf("COMMS uncrewed alarm reason %d", reason), []string{v.commsBoxStatusLine(c, 0, false, reason)})
	}
	return steady
}

func widestPerTier(rs []tierReading) map[chipTier]tierReading {
	out := map[chipTier]tierReading{}
	for _, r := range rs {
		if r.width > out[r.tier].width {
			out[r.tier] = r
		}
	}
	return out
}

func TestChipTierWidthsAreDerivedFromFixtures(t *testing.T) {
	steady := tierReadings(t)
	widest := widestPerTier(steady)
	for _, tier := range []chipTier{chipTierTopLeft, chipTierBottomLeft, chipTierRight} {
		got := widest[tier]
		t.Logf("tier %d: widest steady reading %d cells, %s", tier, got.width, got.source)
		if got.width == 0 {
			t.Fatalf("tier %d measured nothing: the fixtures are not reaching it", tier)
		}
		if pin := tier.outerWidth(); pin != got.width {
			t.Errorf("tier %d pinned at %d but its widest steady reading is %d (%s)", tier, pin, got.width, got.source)
		}
	}
}

var (
	tierTopLeftIDs    = map[settings.Chip]bool{settings.ChipNodes: true, settings.ChipPropellant: true, settings.ChipGuidance: true}
	tierBottomLeftIDs = map[settings.Chip]bool{settings.ChipComms: true, settings.ChipStages: true, settings.ChipMissions: true}
	tierRightIDs      = map[settings.Chip]bool{settings.ChipNavigation: true, settings.ChipTarget: true}
)

func TestChipTierEdgesLineUpWhenRendered(t *testing.T) {
	sizes := [][2]int{{140, 40}, {181, 49}}
	for _, sz := range sizes {
		for name, w := range allDensityPhases(t) {
			t.Run(fmt.Sprintf("%dx%d/%s", sz[0], sz[1], name), func(t *testing.T) {
				v := NewOrbitView(launchThemeForTest())
				v.Resize(sz[0], sz[1])
				v.Render(w, 0, sz[0], sz[1])
				var tl, bl, r []chipRect
				for _, cr := range v.chipRects {
					switch {
					case tierTopLeftIDs[cr.id]:
						tl = append(tl, cr)
					case tierBottomLeftIDs[cr.id]:
						bl = append(bl, cr)
					case tierRightIDs[cr.id]:
						r = append(r, cr)
					}
				}
				if len(tl) != 3 || len(bl) != 3 || len(r) < 1 {
					t.Fatalf("expected 3 top-left, 3 bottom-left and >=1 right boxes, got %d/%d/%d", len(tl), len(bl), len(r))
				}
				for _, g := range []struct {
					name  string
					rects []chipRect
					width int
				}{{"top-left", tl, tierTopLeftWidth}, {"bottom-left", bl, tierBottomLeftWidth}, {"right", r, tierRightWidth}} {
					for _, cr := range g.rects {
						if got := cr.colEnd - cr.colStart + 1; got != g.width {
							t.Errorf("%s box %q is %d wide on screen, want %d (rect %+v)", g.name, cr.id, got, g.width, cr)
						}
						if g.name != "right" && cr.colStart != g.rects[0].colStart {
							t.Errorf("%s box %q left edge %d != %d", g.name, cr.id, cr.colStart, g.rects[0].colStart)
						}
						if g.name == "right" && cr.colEnd != g.rects[0].colEnd {
							t.Errorf("right box %q right edge %d != %d", cr.id, cr.colEnd, g.rects[0].colEnd)
						}
					}
				}
			})
		}
	}
}

// TestChipTierWidthsStableWithinAFlight: pressing t (a target), a node or
// a burn changes numbers only. Same world, two states, identical rects'
// widths.
func TestChipTierWidthsStableWithinAFlight(t *testing.T) {
	widths := func(w *sim.World) map[settings.Chip]int {
		v := NewOrbitView(launchThemeForTest())
		v.Resize(140, 40)
		v.Render(w, 0, 140, 40)
		out := map[settings.Chip]int{}
		for _, cr := range v.chipRects {
			if tierTopLeftIDs[cr.id] || tierBottomLeftIDs[cr.id] || tierRightIDs[cr.id] {
				out[cr.id] = cr.colEnd - cr.colStart + 1
			}
		}
		return out
	}
	phases := allDensityPhases(t)
	base := widths(phases["no target"])
	for _, other := range []string{"target acquired", "coasting with plan", "live burn"} {
		got := widths(phases[other])
		for id, bw := range base {
			if gw, ok := got[id]; ok && gw != bw {
				t.Errorf("%s: box %q is %d wide, was %d with no target", other, id, gw, bw)
			}
		}
	}
	if len(base) < 7 {
		t.Fatalf("expected at least 7 tiered boxes in the baseline, got %d: %v", len(base), base)
	}
}
