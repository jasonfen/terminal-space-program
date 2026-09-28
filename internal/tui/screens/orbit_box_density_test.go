// orbit_box_density_test.go (issue #476, retuned again for #478): measures
// the eight instrument boxes across a battery of phases and keeps the
// per-box column pins (engineCols/propellantCols/guidanceCols/
// navigationCols/targetCols) honest against that measurement, so a
// future change to a reading's text trips this file rather than
// silently drifting the column back out of alignment.
//
// Phases (the same eight Jason named in the #476 issue): pad, powered
// ascent, coasting with a plan planted, a live (manual) burn, a powered
// descent on an airless world, docked (near-zero range/closing), no
// target, and a target acquired. Guidance's own widest hold: readings
// need three more specific fixtures beyond those eight, since none of
// them alone drive AttitudeMode+NavMode together: "Target Prograde
// (TGT)" (the issue's own illustrative "(TARGET)" turned out to be the
// LONG nav: word, not the abbreviated frame tag attitudeHoldLabel
// actually prints), "Surface Retrograde" (now "Retrograde (SURF)" per
// #478 A2), and "Target Retrograde (TGT)" — #478's own new widest
// possible hold: reading once "Surface " dropped off the surface case
// (23 cells beats surface's post-A2 17).
package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// --- phase fixtures ---------------------------------------------------

// densityPad is the pad phase: sim.NewWorld()'s own default craft
// (Earth, throttle 0, no target), flipped Landed (NewWorld's own seed
// craft spawns already in LEO; TestNavigationTitleShowsLandedSite uses
// this same flip to fake the pad state without a full spawn-on-pad
// routine). This is what actually exercises NAVIGATION's landed-only
// incl:/depart: "(min N°)"/"(best N°)" suffix readings.
func densityPad(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ActiveCraft().Landed = true
	return w
}

// densityPoweredAscent is a sub-orbital climb with the engine lit via a
// player-held manual burn (the widest ENGINE throttle row: "100% ●
// FIRING <elapsed>").
func densityPoweredAscent(t *testing.T) *sim.World {
	t.Helper()
	w := ascendingCraftWorld(t, "earth", 5_000, 150, orbital.Vec3{Z: 1})
	c := w.ActiveCraft()
	c.Throttle = 1
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime.Add(-(12*time.Minute + 34*time.Second))}
	return w
}

// densityCoastingWithPlan parks the craft in a stable inclined orbit
// with a resolved prograde node planted (NAVIGATION's plan: row and the
// Ap/Pe/incl/period -> arrows).
func densityCoastingWithPlan(t *testing.T) *sim.World {
	t.Helper()
	w := inclinedCircularEarthOrbitCraft(t, 45, 500_000)
	c := w.ActiveCraft()
	plantProgradeNode(w, c, 500, "")
	return w
}

// densityLiveBurn is a manual burn in a stable orbit (ENGINE's throttle
// row firing+elapsed and the node: row's "burning ... left" line via a
// live ActiveBurn).
func densityLiveBurn(t *testing.T) *sim.World {
	t.Helper()
	w := inclinedCircularEarthOrbitCraft(t, 20, 400_000)
	c := w.ActiveCraft()
	c.Throttle = 1
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime.Add(-(9*time.Minute + 5*time.Second))}
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 1234,
		EndTime:     w.Clock.SimTime.Add(5 * time.Minute),
	}
	return w
}

// densityPoweredDescentAirless: a powered braking descent over the
// Moon (airless), engine lit, close enough that ENGINE's node: row
// reads the braking-burn-at line instead of a queued node or dash.
func densityPoweredDescentAirless(t *testing.T) *sim.World {
	t.Helper()
	w := descendingMoonCraft(t, 20_000, 120)
	c := w.ActiveCraft()
	c.Throttle = 1
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime.Add(-(2*time.Minute + 3*time.Second))}
	return w
}

// densityDocked: a target vessel at near-zero range and closing speed
// (DOCK READY territory) — TARGET's narrowest common numbers, included
// for coverage completeness even though it never sets a widest cell.
func densityDocked(t *testing.T) *sim.World {
	t.Helper()
	return leadTestWorld(t, 0.01)
}

// densityNoTarget: an orbiting craft with nothing targeted (TARGET's
// all-dash form, exercised alongside the other boxes in their ordinary
// coasting state).
func densityNoTarget(t *testing.T) *sim.World {
	t.Helper()
	w := inclinedCircularEarthOrbitCraft(t, 10, 300_000)
	w.Target = sim.Target{Kind: sim.TargetNone}
	return w
}

// densityTargetAcquired: a real vessel target ahead on the same orbital
// plane (TARGET's full ten-cell common case: range/closing/rel,
// Ap/Pe/incl, Δincl/lead, TCA/approach all populated).
func densityTargetAcquired(t *testing.T) *sim.World {
	t.Helper()
	return leadTestWorld(t, 82)
}

// densityGuidanceHoldTarget: NavTarget with a resolvable relative
// target and a held orbit-frame Prograde, which attitudeHoldLabel
// remaps to "Target Prograde (TGT)" (21 cells, measured — navModeLabel's
// frame tag is the abbreviated "TGT"/"SURF"/"ORBIT", not the long nav:
// word the issue's own illustrative example used). None of the eight
// named phases alone drive AttitudeMode+NavMode together, so this is
// GUIDANCE-specific.
func densityGuidanceHoldTarget(t *testing.T) *sim.World {
	t.Helper()
	w := leadTestWorld(t, 82)
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnPrograde
	w.NavMode = sim.NavTarget
	return w
}

// densityGuidanceHoldSurfaceRetrograde: the rarer combination GUIDANCE's
// own doc comment calls out as a wide hold: reading, "Retrograde (SURF)"
// (17 cells measured, #478 A2 dropped the redundant "Surface " word) —
// never remapped by attitudeHoldLabel's NavTarget branch
// (BurnSurfaceRetrograde already carries its own frame), so it renders
// regardless of NavMode.
func densityGuidanceHoldSurfaceRetrograde(t *testing.T) *sim.World {
	t.Helper()
	w := densityCoastingWithPlan(t)
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnSurfaceRetrograde
	w.NavMode = sim.NavSurface
	return w
}

// densityGuidanceHoldTargetRetrograde: #478 A2 dropped "Surface " from
// the surface-framed hold reading, which used to be GUIDANCE's own
// widest possible value1 reading (25 cells); with that gone, the new
// widest is "Target Retrograde (TGT)" (23 cells) — BurnRetrograde held
// while NavTarget resolves a relative target, remapped by
// attitudeHoldLabel the same way densityGuidanceHoldTarget's Prograde
// case is, just the other axis direction, which happens to be one word
// longer ("Retrograde" vs "Prograde").
func densityGuidanceHoldTargetRetrograde(t *testing.T) *sim.World {
	t.Helper()
	w := leadTestWorld(t, 82)
	c := w.ActiveCraft()
	c.AttitudeMode = spacecraft.BurnRetrograde
	w.NavMode = sim.NavTarget
	return w
}

// allDensityPhases is every phase fixture, named, for the coverage
// sweep (TestBoxDensityNoPhaseOverflowsCanvas and the widest-reading
// report).
func allDensityPhases(t *testing.T) map[string]*sim.World {
	t.Helper()
	return map[string]*sim.World{
		"pad":                              densityPad(t),
		"powered ascent":                   densityPoweredAscent(t),
		"coasting with plan":               densityCoastingWithPlan(t),
		"live burn":                        densityLiveBurn(t),
		"powered descent airless":          densityPoweredDescentAirless(t),
		"docked":                           densityDocked(t),
		"no target":                        densityNoTarget(t),
		"target acquired":                  densityTargetAcquired(t),
		"guidance hold surface retrograde": densityGuidanceHoldSurfaceRetrograde(t),
		"guidance hold target":             densityGuidanceHoldTarget(t),
		"guidance hold target retrograde":  densityGuidanceHoldTargetRetrograde(t),
	}
}

// --- measurement helpers ------------------------------------------------

// labelCol returns the display column at which label first appears in
// row, and whether it appears at all.
func labelCol(row, label string) (int, bool) {
	idx := strings.Index(row, label)
	if idx < 0 {
		return 0, false
	}
	return lipgloss.Width(row[:idx]), true
}

// --- value1: sized to each box's own longest label1 ---------------------

// TestBoxDensityValue1MatchesLongestLabel1 measures each box's own
// label1 strings directly (not a rendered row) and asserts value1 == 2
// (the row's leading indent) + the longest one's width + 1 cell of
// daylight — the rule this retune uses instead of the old shared
// boxValueCol.
func TestBoxDensityValue1MatchesLongestLabel1(t *testing.T) {
	cases := []struct {
		name   string
		cols   boxCols
		label1 []string
	}{
		{"ENGINE", engineCols, []string{"throttle:", "TWR:", "node:"}},
		{"PROPELLANT", propellantCols, []string{"fuel:", "Δv:", "monoprop:"}},
		{"GUIDANCE", guidanceCols, []string{"hold:", "heading:", "fpa:"}},
		{"NAVIGATION", navigationCols, []string{"altitude:", "horiz:", "Ap:", "incl:", "depart:", "impact:", "plan:"}},
		{"TARGET", targetCols, []string{"range:", "Ap:", "Δincl:", "TCA:"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			longest := 0
			for _, l := range c.label1 {
				if w := lipgloss.Width(l); w > longest {
					longest = w
				}
			}
			want := 2 + longest + 1
			if c.cols.value1 != want {
				t.Errorf("%s value1 = %d, want %d (2 + longest label1 %d + 1)", c.name, c.cols.value1, want, longest)
			}
		})
	}
}

// --- stability: the pinned column must not depend on a reading's own width ---

// TestBoxDensitySecondLabelStableAcrossPhases is the sabotage-first
// stability guard (Jason's own ruling: a reading growing mid-burn must
// never step the next column sideways). Sabotage-checked by hand:
// with chipCellAt's pad reverted to the retired "row width + 2" shape
// (label following the first cell instead of a fixed column), 4 of the
// 6 cases below (ENGINE, GUIDANCE, TARGET) went red — the label landed
// at a different column per phase, e.g. ENGINE's mode: at column 23 on
// the pad but column 35 during a live burn. Against the real
// fixed-column chipCellAt every case is green. Each pair below picks
// two phases whose own first reading differs sharply in width for that
// box.
func TestBoxDensitySecondLabelStableAcrossPhases(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	phases := allDensityPhases(t)
	cases := []struct {
		name   string
		build  func(*sim.World) []string
		row    int
		label  string
		phaseA string
		phaseB string
	}{
		{"ENGINE throttle/mode", v.buildEngineBox, 1, "mode:", "pad", "live burn"},
		{"PROPELLANT fuel/mass", v.buildPropellantBox, 1, "mass:", "pad", "powered ascent"},
		{"GUIDANCE hold/nav", v.buildGuidanceBox, 1, "nav:", "pad", "guidance hold target"},
		{"NAVIGATION altitude/vert", v.buildNavigationBox, 1, "vert:", "pad", "powered descent airless"},
		{"NAVIGATION depart/e/dir e:", v.buildNavigationBox, 5, "e:", "no target", "coasting with plan"},
		{"TARGET range/close/rel close:", v.buildTargetBox, 1, "close:", "docked", "target acquired"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rowA := c.build(phases[c.phaseA])[c.row]
			rowB := c.build(phases[c.phaseB])[c.row]
			colA, okA := labelCol(rowA, c.label)
			colB, okB := labelCol(rowB, c.label)
			if !okA || !okB {
				t.Fatalf("%q missing in one phase: %s=%q (found=%v) %s=%q (found=%v)", c.label, c.phaseA, rowA, okA, c.phaseB, rowB, okB)
			}
			if colA != colB {
				t.Errorf("%s label %q lands at column %d in phase %q but column %d in phase %q; the pin must not depend on the first reading's width", c.name, c.label, colA, c.phaseA, colB, c.phaseB)
			}
		})
	}
}

// --- no overflow: worst-case pairs must still fit the 138-column canvas ---

// designCanvasWidth is the Design Size (140x40) canvas width (cCols =
// totalCols - 2, see orbit_chips.go), the floor every box must still
// fit inside per phase.
const designCanvasWidth = 138

// TestBoxDensityNoPhaseOverflowsCanvas: every row, in every measured
// phase, plus its box's own two-column border, must still fit the
// 138-column Design Size canvas. This does not assert the two widest
// sides never coincide (they are structurally on opposite stacks and
// never render in the same phase, per the issue), only that no single
// phase's own row is wide enough to blow the canvas on its own.
func TestBoxDensityNoPhaseOverflowsCanvas(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	phases := allDensityPhases(t)
	boxes := []struct {
		name  string
		build func(*sim.World) []string
	}{
		{"ENGINE", v.buildEngineBox},
		{"PROPELLANT", v.buildPropellantBox},
		{"GUIDANCE", v.buildGuidanceBox},
		{"NAVIGATION", v.buildNavigationBox},
		{"TARGET", v.buildTargetBox},
	}
	const borderWidth = 2 // left + right border column
	for _, b := range boxes {
		for phaseName, w := range phases {
			lines := b.build(w)
			for i, row := range lines {
				if width := lipgloss.Width(row) + borderWidth; width > designCanvasWidth {
					t.Errorf("%s row %d in phase %q = %q, width+border %d exceeds the %d-column canvas", b.name, i, phaseName, row, width, designCanvasWidth)
				}
			}
		}
	}
}

// --- widest-reading report (informational, not asserted) ---------------

// TestBoxDensityWidestReadingReport logs, per box, the widest row
// actually observed across every phase (the table this retune's pins
// were derived from). Not an assertion: the report is Jason's own next
// decision (shortening wording / reclaiming rows), filed but not
// implemented here.
func TestBoxDensityWidestReadingReport(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	phases := allDensityPhases(t)
	boxes := []struct {
		name  string
		build func(*sim.World) []string
	}{
		{"ENGINE", v.buildEngineBox},
		{"PROPELLANT", v.buildPropellantBox},
		{"GUIDANCE", v.buildGuidanceBox},
		{"NAVIGATION", v.buildNavigationBox},
		{"TARGET", v.buildTargetBox},
	}
	for _, b := range boxes {
		widest := 0
		widestRow, widestPhase := "", ""
		for phaseName, w := range phases {
			for _, row := range b.build(w) {
				if width := lipgloss.Width(row); width > widest {
					widest, widestRow, widestPhase = width, row, phaseName
				}
			}
		}
		t.Logf("%s widest row (%d cells, phase %q): %q", b.name, widest, widestPhase, widestRow)
	}
}

// --- right column width with TARGET absent (#480) -----------------------

// rightColumnWidth measures the composed right-column width
// assembleChips/composeChips actually right-aligns against for phase
// world w: the widest padded content width (+2 for the border) among
// whichever cornerTopRight boxes are ACTUALLY present this frame. With
// no target, #480 means TARGET is not among them at all, so this
// reflects NAVIGATION alone rather than max(NAVIGATION, TARGET) the way
// every phase before #480 measured.
func rightColumnWidth(t *testing.T, v *OrbitView, w *sim.World) int {
	t.Helper()
	widest := 0
	for _, c := range v.assembleChips(w) {
		if c.corner != cornerTopRight {
			continue
		}
		_, contentW := padChipBlock(c.lines)
		if bw := contentW + 2; bw > widest {
			widest = bw
		}
	}
	return widest
}

// TestBoxDensityRightColumnWidthNoTargetPhase (#480): the right
// column's composed width is a real, different case now that TARGET
// can be absent. In the "no target" phase, NAVIGATION alone governs the
// right column's width (TARGET contributes nothing, since it isn't
// placed at all); in "target acquired", TARGET is the wider box and
// governs instead (per the #478/#480 vault record: TARGET 56 cols vs.
// NAVIGATION 51). Sabotage-checked by hand: reverting the #480 fix (so
// navigationBoxesInOrder always places TARGET, even with no target)
// turns this test red — see the PR description for the pasted failure.
func TestBoxDensityRightColumnWidthNoTargetPhase(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	v.Resize(181, 49)

	noTargetW := rightColumnWidth(t, v, densityNoTarget(t))
	navOnly := v.buildNavigationBox(densityNoTarget(t))
	_, navContentW := padChipBlock(navOnly)
	wantNoTarget := navContentW + 2
	if noTargetW != wantNoTarget {
		t.Errorf("right column width with no target = %d, want %d (NAVIGATION alone, TARGET not placed)", noTargetW, wantNoTarget)
	}

	targetW := rightColumnWidth(t, v, densityTargetAcquired(t))
	targetOnly := v.buildTargetBox(densityTargetAcquired(t))
	_, targetContentW := padChipBlock(targetOnly)
	wantTargetPhase := targetContentW + 2
	if targetW != wantTargetPhase {
		t.Errorf("right column width with a target acquired = %d, want %d (TARGET governs)", targetW, wantTargetPhase)
	}

	if noTargetW >= targetW {
		t.Errorf("no-target right column width (%d) should be narrower than the target-acquired width (%d): TARGET is the wider box when present", noTargetW, targetW)
	}
}
