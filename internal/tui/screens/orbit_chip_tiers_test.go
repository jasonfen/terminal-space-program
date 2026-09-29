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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
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

	// ENGINE's node row and title, every branch at its worst (F1).
	for _, r := range engineWorstCaseFixtures(t) {
		add(&steady, chipTierTopLeft, "ENGINE/"+r.source, r.lines)
	}

	// Worst-case VAB builds: the VAB has no stage cap, so STAGES must hold
	// its tier for any stack depth and any part name, the longest catalog
	// name and an overlay-length one included (#482 review F2).
	for _, r := range vabStagesFixtures(t) {
		add(&steady, chipTierBottomLeft, "STAGES/"+r.source, r.lines)
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

type vabStagesFixture struct {
	source string
	lines  []string
}

// vabStagesFixtures builds STAGES rows for VAB-style stacks: every
// catalog part's name leading, plus a 60-cell overlay-style name, at stack
// depths from 1 to 300.
func vabStagesFixtures(t *testing.T) []vabStagesFixture {
	t.Helper()
	v := NewOrbitView(launchThemeForTest())
	names := []string{strings.Repeat("W", 60)}
	for id, m := range spacecraft.StageCatalog {
		_ = id
		names = append(names, m.Name)
	}
	var out []vabStagesFixture
	for _, name := range names {
		for _, n := range []int{1, 2, 6, 9, 12, 40, 300} {
			w := densityCoastingWithPlan(t)
			c := w.ActiveCraft()
			c.Stages = nil
			for i := 0; i < n; i++ {
				c.Stages = append(c.Stages, spacecraft.Stage{Name: name, FuelCapacity: 1, FuelMass: 1})
			}
			out = append(out, vabStagesFixture{fmt.Sprintf("vab %q x%d", name, n), v.buildStagesBox(w)})
		}
	}
	return out
}

// TestStagesRowNeverExceedsTheTier: the row is bounded by construction
// (truncated name, capped pips), not by luck of the fixtures.
func TestStagesRowNeverExceedsTheTier(t *testing.T) {
	fx := vabStagesFixtures(t)
	if len(fx) < 100 {
		t.Fatalf("only %d fixtures: catalog names not reaching the guard", len(fx))
	}
	for _, r := range fx {
		for _, l := range r.lines {
			if got := lipgloss.Width(l) + 2; got > tierBottomLeftWidth {
				t.Errorf("%s is %d wide, tier is %d: %q", r.source, got, tierBottomLeftWidth, l)
			}
		}
	}
}

// Today's loadouts must render exactly as before: no truncation, no
// pip cap (their widest row is well inside the budget).
func TestStagesRowLoadoutsUntouched(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	for _, id := range spacecraft.LoadoutOrder {
		w := densityCoastingWithPlan(t)
		c := spacecraft.NewFromLoadout(id)
		w.Crafts[w.ActiveCraftIdx] = c
		row := strings.Join(v.buildStagesBox(w), "")
		if strings.Contains(row, "…") {
			t.Errorf("loadout %s STAGES row was cut: %q", id, row)
		}
	}
}

// engineWorstCaseFixtures drives the REAL ENGINE builder through every
// node-row branch at its widest (#482 review F1): every BurnMode (frame
// modes included) x every trigger event (closest approach with the target-relative modes only) x 3-, 4- and 5-digit Δv x
// within / over budget x 1, 2 and 11 queued nodes x the longest
// readout.Duration forms, resolved and unresolved; every live-burn mode,
// STALLED included; and braking-burn descents. ENGINE's title row rides
// along in each set, so the ⚠ / +N [m] suffix is measured too.
func engineWorstCaseFixtures(t *testing.T) []vabStagesFixture {
	t.Helper()
	v := NewOrbitView(launchThemeForTest())
	var modes []spacecraft.BurnMode
	for m := spacecraft.BurnMode(0); m.String() != "?"; m++ {
		modes = append(modes, m)
	}
	events := []spacecraft.TriggerEvent{
		spacecraft.TriggerNextPeri, spacecraft.TriggerNextApo, spacecraft.TriggerNextAN,
		spacecraft.TriggerNextDN, spacecraft.TriggerNextClosestApproach,
	}
	var out []vabStagesFixture
	pad := regexp.MustCompile(` {3,}`)
	add := func(source string, lines []string) {
		// The title row right-aligns its suffix to the pin, so its drawn
		// width is the pin by construction; measure its intrinsic width
		// (title, a 2-cell gap, suffix) or the guard would go circular.
		lines = append([]string(nil), lines...)
		lines[0] = pad.ReplaceAllString(lines[0], "  ")
		out = append(out, vabStagesFixture{source, lines})
	}

	w := densityCoastingWithPlan(t)
	c := w.ActiveCraft()
	base := c.Nodes[0]
	queue := func(n spacecraft.ManeuverNode, count int) {
		c.Nodes = nil
		for i := 0; i < count; i++ {
			c.Nodes = append(c.Nodes, n)
		}
	}
	durs := []time.Duration{45 * time.Minute, 72*time.Hour + 45*time.Minute, 365*24*time.Hour + 23*time.Hour}
	for _, m := range modes {
		for _, dv := range []float64{500, 1200, 12345} {
			for _, count := range []int{1, 2, 11} {
				for _, d := range durs {
					n := base
					n.Mode, n.DV, n.TriggerTime = m, dv, w.Clock.SimTime.Add(d)
					queue(n, count)
					add(fmt.Sprintf("resolved %s dv=%.0f x%d in %s", m, dv, count, d), v.buildEngineBox(w))
				}
				for _, e := range events {
					// The planner only offers closest approach with the
					// target-relative modes (IsTargetRelativeMode).
					if e == spacecraft.TriggerNextClosestApproach && !spacecraft.IsTargetRelativeMode(m) {
						continue
					}
					n := base
					n.Mode, n.DV, n.Event, n.TriggerTime = m, dv, e, time.Time{}
					queue(n, count)
					add(fmt.Sprintf("unresolved %s %s dv=%.0f x%d", e, m, dv, count), v.buildEngineBox(w))
				}
			}
		}
	}

	lw := densityLiveBurn(t)
	lc := lw.ActiveCraft()
	for _, m := range modes {
		for _, dv := range []float64{1234, 12345} {
			for _, d := range durs {
				lc.ActiveBurn.Mode, lc.ActiveBurn.DVRemaining = m, dv
				lc.ActiveBurn.EndTime = lw.Clock.SimTime.Add(d)
				add(fmt.Sprintf("live %s dv=%.0f %s", m, dv, d), v.buildEngineBox(lw))
			}
		}
	}
	for i := range lc.Stages {
		lc.Stages[i].FuelMass = 0
	}
	if !lc.BurnStalled() {
		t.Fatal("setup: draining the stages should stall the live burn")
	}
	for _, m := range modes {
		lc.ActiveBurn.Mode, lc.ActiveBurn.DVRemaining = m, 12345
		add("STALLED "+m.String(), v.buildEngineBox(lw))
	}

	for _, alt := range []float64{20_000, 60_000} {
		bw := descendingMoonCraft(t, alt, 120)
		if v.cachedDescentStop(bw, bw.ActiveCraft()).hasBurnAt {
			add(fmt.Sprintf("braking descent from %.0f m", alt), v.buildEngineBox(bw))
			bw.ActiveCraft().Nodes = []spacecraft.ManeuverNode{base, base, base}
			add(fmt.Sprintf("braking descent from %.0f m with a queue", alt), v.buildEngineBox(bw))
		}
	}
	// Extreme braking readouts through the same format (the fixtures only
	// reach a few of them).
	for _, tc := range []struct{ alt, sec float64 }{{4000, 300}, {999999, 360000}} {
		line := fmt.Sprintf("%s braking burn at %s  %s", hudNodeMarker, readout.Distance(tc.alt), readout.Countdown(secondsToDuration(tc.sec)))
		add(fmt.Sprintf("braking synth alt=%.0f in=%.0fs", tc.alt, tc.sec), []string{chipRowAt("node:", line, engineCols.value1)})
	}
	return out
}

// TestEngineWorstCaseFixturesReachEveryBranch keeps the enumeration
// honest: if a builder change stops a branch from rendering, the guard
// would silently measure the dash row instead.
func TestEngineWorstCaseFixturesReachEveryBranch(t *testing.T) {
	joined := map[string]bool{}
	for _, r := range engineWorstCaseFixtures(t) {
		for _, l := range r.lines {
			for _, want := range []string{"▸ in ", "▸ next approach", "left", "⚠ STALLED", "braking burn", "+10 [m]", "⚠", "Surf ", "Tgt "} {
				if strings.Contains(l, want) {
					joined[want] = true
				}
			}
		}
	}
	for _, want := range []string{"▸ in ", "▸ next approach", "left", "⚠ STALLED", "braking burn", "+10 [m]", "⚠", "Surf ", "Tgt "} {
		if !joined[want] {
			t.Errorf("worst-case ENGINE fixtures never rendered %q", want)
		}
	}
}
