package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// orbit_box_engine.go, the ENGINE instrument box (ADR 0051 decision 1,
// slice 2a): throttle with the lit state and elapsed time, mode, TWR
// (current with the max-throttle figure, decision 13b), and the node row
// with its three-way precedence (decision 12, re-grill Q2/Q4): a live
// burn, then the latest safe braking start while one exists, then a
// queued node, else a dash. Replaces buildAttitudeChip's engine:/manual:
// rows and buildLaunchChip/buildDescentChip's twr:/engine: rows, and
// folds in the live-burn head buildNodesChip used to carry
// (activeBurnLines) plus its queued-node line (nextQueuedNodeLine) for
// the ACTIVE craft only, a per-vessel instrument box has no reason to
// show a different vessel's queue the way the old fleet-wide NODES chip
// did.
//
// Every row is always present (decision 2): with no active craft at all,
// every cell reads a dash rather than the box vanishing.
func (v *OrbitView) buildEngineBox(w *sim.World) []string {
	title := v.theme.Primary.Render("ENGINE") + v.vesselBurnBadge(w)
	c := w.ActiveCraft()
	title += v.engineBoosterBadge(c)
	title = v.engineTitleWithQueue(title, w, c)
	if c == nil {
		return []string{
			title,
			chipRow2(engineCols, "throttle:", "—", "mode:", "—"),
			v.engineTWRCell(nil),
			v.engineNodeLine(w, nil),
		}
	}
	label, throttle := engineThrottleTarget(c)
	return []string{
		title,
		chipRow2(engineCols, label, v.engineThrottleLabelAt(w, c, throttle), "mode:", c.EngineMode.String()),
		v.engineTWRCell(c),
		v.engineNodeLine(w, c),
	}
}

// engineTitleWithQueue right-aligns the queued node's over-budget "⚠" and
// the "+N [m]" overflow count on the title row (#482 review F1): they
// used to trail the node row, where a 4-digit Retrograde node over budget
// with several queued grew the box past its tier. The title row has the
// room, so nothing here can widen the box. Shown only while the node row
// is describing a queued node (no live burn, no braking start), i.e. the
// same conditions the old trailing suffix appeared under.
func (v *OrbitView) engineTitleWithQueue(title string, w *sim.World, c *spacecraft.Spacecraft) string {
	if c == nil || v.engineNodeBranch(w, c) != nodeBranchQueued {
		return title
	}
	suffix := ""
	if _, isOver := c.Nodes[0].OverBudget(c); isOver {
		suffix = v.theme.Alert.Render("⚠")
	}
	if len(c.Nodes) > 1 {
		if suffix != "" {
			suffix += " "
		}
		suffix += v.theme.Dim.Render(fmt.Sprintf("+%d [m]", len(c.Nodes)-1))
	}
	if suffix == "" {
		return title
	}
	pad := tierTopLeftWidth - 2 - lipgloss.Width(title) - lipgloss.Width(suffix)
	if pad < 2 {
		pad = 2
	}
	return title + strings.Repeat(" ", pad) + suffix
}

// engineThrottleLabel renders the throttle row's value: the commanded
// throttle setting is always shown (never gated), with a trailing state
// telling whether an engine is actually lit right now and, while it is,
// how long it has been firing. Mirrors the pre-ADR-0051 throttleRow/
// buildLaunchChip elapsed logic, now unified onto one row instead of
// split across VESSEL's "(idle)"/"● FIRING" and SURFACE's "● LIT".
//
// Only a manual burn carries a clock here (T+, elapsed since ignition):
// a node (ActiveBurn) burn used to print T- (time remaining) instead,
// the opposite direction under the identical "● FIRING" glyph with no
// label saying which convention applied (review finding 5, 2026-09-25).
// A node burn's own remaining time already reads on the node row
// (engineBurnLine's "... left"), so this cell drops its clock there
// rather than showing a second, oppositely-signed one; a manual burn has
// no node row to carry that information, so it keeps its own.
func (v *OrbitView) engineThrottleLabel(w *sim.World, c *spacecraft.Spacecraft) string {
	return v.engineThrottleLabelAt(w, c, c.EffectiveThrottle())
}

// engineThrottleLabelAt is engineThrottleLabel for a chosen group's throttle
// (the throttle target's, ADR 0058 decision 6).
func (v *OrbitView) engineThrottleLabelAt(w *sim.World, c *spacecraft.Spacecraft, throttle float64) string {
	base := fmt.Sprintf("%.0f%%", throttle*100)
	if sim.StackDryArmed(c) {
		// #466 (G5 Q1): the throttle is a standing order; the lit tank is
		// dry, so say so. [space] relights the next engine at this setting.
		return base + " " + v.theme.Warning.Render("✕ DRY")
	}
	if !sim.StackMidBurn(c) {
		return base + v.theme.Dim.Render(" idle")
	}
	firing := v.theme.Warning.Render("● FIRING")
	elapsed := ""
	if c.ManualBurn != nil {
		elapsed = " " + readout.Countdown(-(w.Clock.SimTime.Sub(c.ManualBurn.StartTime)))
	}
	return base + " " + firing + elapsed
}

// engineThrottleTarget is the throttle row's label and value for the
// throttle target (ADR 0058 decision 6): throttle: (all, and every vessel
// with one throttle), core:, boosters:. The one deliberate exception to
// ADR 0051's fixed labels; the label is the selector's state.
func engineThrottleTarget(c *spacecraft.Spacecraft) (label string, throttle float64) {
	switch c.EffectiveThrottleTarget() {
	case spacecraft.ThrottleCore:
		return "core:", c.EffectiveThrottle()
	case spacecraft.ThrottleBoosters:
		return "boosters:", c.BoosterThrottleEff()
	}
	return "throttle:", c.EffectiveThrottle()
}

// engineBoosterBadge is the title badge tail while a booster group burns
// (ADR 0058 decision 6): "· boosters T-1m02s" (solid), "· boosters DRY",
// "· boosters 100% T-1m47s" (liquid, with its throttle). With the target on
// the boosters the badge shows the core instead ("· core 60%  T-1m47s");
// the percentage pads to four cells so the clock keeps its column. The
// clock is the group's lowest lit tank at its own flow; nothing is shown
// for a group at 0%.
func (v *OrbitView) engineBoosterBadge(c *spacecraft.Spacecraft) string {
	if c == nil || !sim.StackMidBurn(c) {
		return ""
	}
	set := spacecraft.LitStages(c.Stages)
	if len(set) < 2 {
		return ""
	}
	idx, label, th, liquid := set[0], "boosters", c.BoosterThrottleEff(), c.Stages[set[0]].FuelType != spacecraft.FuelTypeSolid
	if c.EffectiveThrottleTarget() == spacecraft.ThrottleBoosters {
		idx, label, th, liquid = set[len(set)-1], "core", c.EffectiveThrottle(), true
	}
	if !liquid {
		th = 1 // a solid is pinned at 100% and shows no percentage
	}
	text := " · " + label
	if c.Stages[idx].FuelMass <= 0 {
		return v.theme.Warning.Render(text + " DRY")
	}
	if liquid {
		pct := fmt.Sprintf("%.0f%%", th*100)
		text += " " + pct + strings.Repeat(" ", max(0, 4-len(pct)))
	}
	if secs, ok := c.StageTimeToDry(idx, th); ok {
		text += " " + readout.Countdown(time.Duration(secs*float64(time.Second)))
	}
	return v.theme.Warning.Render(strings.TrimRight(text, " "))
}

// engineTWRCell renders the TWR row (decision 13b): the current-throttle
// figure, with the max-throttle figure alongside it. There is no lift
// verdict (#648): a max below 1.0 is the tell on its own, and a verdict
// read as a fault on every vessel coasting in orbit. Whether the "(max N)"
// suffix still prints once the throttle is already at maximum is not
// ruled by the ADR (decision 13b's open note); this build choice omits
// it there (nothing new to say when current equals max), a reversible
// call, not a measured one.
func (v *OrbitView) engineTWRCell(c *spacecraft.Spacecraft) string {
	if c == nil || c.Thrust <= 0 || c.TotalMass() <= 0 {
		return chipRowAt(readout.LabelTWR, "—", engineCols.value1)
	}
	g := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
	cur := c.Thrust * c.LitThrottle(c.EffectiveThrottle()) / (c.TotalMass() * g)
	max := c.Thrust / (c.TotalMass() * g)
	curLabel := fmt.Sprintf("%.2f", cur)
	maxLabel := fmt.Sprintf("%.2f", max)
	var value string
	switch {
	case curLabel == maxLabel:
		value = curLabel
	default:
		value = fmt.Sprintf("%s (max %s)", curLabel, maxLabel)
	}
	return chipRowAt(readout.LabelTWR, value, engineCols.value1)
}

// engineNodeLine picks ENGINE's node row per its three-way precedence
// (ADR 0051 decision 12, re-grill Q2): a live burn on the active craft
// outranks the latest safe braking start, which outranks a queued node,
// which outranks a dash. cachedDescentStop already returns hasBurnAt ==
// false once the burn is under way (issue #377), so checking the live
// burn first and returning early is enough to make "once the burn is
// under way the braking start hides" true without any extra state here.
func (v *OrbitView) engineNodeLine(w *sim.World, c *spacecraft.Spacecraft) string {
	if c == nil {
		return chipRowAt("node:", "—", engineCols.value1)
	}
	switch v.engineNodeBranch(w, c) {
	case nodeBranchBurn:
		line, _ := v.engineBurnLine(w, c)
		return chipRowAt("node:", line, engineCols.value1)
	case nodeBranchBraking:
		stopDat := v.cachedDescentStop(w, c)
		line := fmt.Sprintf("%s braking burn at %s  %s", hudNodeMarker,
			readout.Distance(stopDat.burnAt.AltitudeM),
			readout.Countdown(secondsToDuration(stopDat.burnAt.InSec)))
		return chipRowAt("node:", line, engineCols.value1)
	case nodeBranchQueued:
		return chipRowAt("node:", v.engineQueuedNodeLine(w, c), engineCols.value1)
	}
	return chipRowAt("node:", "—", engineCols.value1)
}

// nodeBranch names which of the node row's four states is showing.
type nodeBranch int

const (
	nodeBranchDash nodeBranch = iota
	nodeBranchBurn
	nodeBranchBraking
	nodeBranchQueued
)

// engineNodeBranch is the ONE place the precedence lives, shared by the
// node row and the title row's queue suffix so they can never disagree.
func (v *OrbitView) engineNodeBranch(w *sim.World, c *spacecraft.Spacecraft) nodeBranch {
	if c == nil {
		return nodeBranchDash
	}
	if c.ActiveBurn != nil {
		return nodeBranchBurn
	}
	if _, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon); descending {
		if v.cachedDescentStop(w, c).hasBurnAt {
			return nodeBranchBraking
		}
	}
	if len(c.Nodes) > 0 {
		return nodeBranchQueued
	}
	return nodeBranchDash
}

// nodeModeLabel is the burn mode as the node row spells it: "Surface" and
// "Target" abbreviate to "Surf" / "Tgt" here only (#482 review F1, the
// row is width-pinned); every other surface keeps BurnMode.String().
func nodeModeLabel(m spacecraft.BurnMode) string {
	s := m.String()
	s = strings.Replace(s, "Surface", "Surf", 1)
	return strings.Replace(s, "Target", "Tgt", 1)
}

// engineBurnLine is engineNodeLine's live-burn branch: the active
// craft's OWN ActiveBurn (a manual burn with no node carries no entry
// here, the throttle row already says it's firing). Adapted from the
// retired activeBurnLines/buildNodesChip, dropping the "vessel N" tag
// (this box is already scoped to the active craft, so it would be
// redundant), ok is false when nothing is burning.
//
// #478 A1: "Δv" and "burning," both drop from the live-burn line — the
// ▸ glyph and the trailing countdown already say it's firing, so
// spelling that out again cost cells without saying anything new. The
// line stays rendered in the theme's Warning colour (the same amber
// "● FIRING" already uses on the throttle row) so the glyph and the
// colour carry the cue together; never a literal colour, so a future
// theme swap still lands here.
func (v *OrbitView) engineBurnLine(w *sim.World, c *spacecraft.Spacecraft) (string, bool) {
	if c == nil || c.ActiveBurn == nil {
		return "", false
	}
	ab := c.ActiveBurn
	if c.BurnStalled() {
		return v.theme.Warning.Render(fmt.Sprintf("%s %s, Δv %s  ⚠ STALLED",
			hudNodeMarker, nodeModeLabel(ab.Mode), readout.DeltaV(ab.DVRemaining))), true
	}
	remaining := ab.EndTime.Sub(w.Clock.SimTime).Seconds()
	if remaining < 0 {
		remaining = 0
	}
	return v.theme.Warning.Render(fmt.Sprintf("%s %s %s, %s left",
		hudNodeMarker, nodeModeLabel(ab.Mode), readout.DeltaV(ab.DVRemaining), readout.Duration(secondsToDuration(remaining)))), true
}

// engineQueuedNodeLine is engineNodeLine's queued-node branch: the
// active craft's first planted node (c.Nodes[0]). Filler words are cut
// ("in 1h00m", not "#1 ignition in 1h00m"; "next approach"), frame modes
// abbreviate (nodeModeLabel), and the over-budget "⚠" and the "+N [m]"
// queued count live on ENGINE's title row (engineTitleWithQueue), so the
// row stays inside the top-left tier (#482 review F1, ADR 0051 W1).
func (v *OrbitView) engineQueuedNodeLine(w *sim.World, c *spacecraft.Spacecraft) string {
	n := c.Nodes[0]
	if !n.IsResolved() {
		// An apsis node on an orbit with no apsides waits for good reason;
		// say so, as the planner row does (wave 1 known issue 5).
		label := nodeEventLabel(n.Event)
		if sim.ApsisEventUndefined(c, n.Event) {
			label += " (undefined)"
			// The top-left tier is pinned (tierTopLeftWidth, border
			// included) and the reason must stay: in the one frame-mode,
			// five-digit-Δv combination that would not fit, the Δv yields.
			full := fmt.Sprintf("%s %s  %s  %s", hudNodeMarker, label, nodeModeLabel(n.Mode), readout.DeltaV(n.DV))
			if engineCols.value1+lipgloss.Width(full) > tierTopLeftWidth-2 {
				return fmt.Sprintf("%s %s  %s", hudNodeMarker, label, nodeModeLabel(n.Mode))
			}
			return full
		}
		return fmt.Sprintf("%s %s  %s  %s", hudNodeMarker, label, nodeModeLabel(n.Mode), readout.DeltaV(n.DV))
	}
	dt := n.BurnStart().Sub(w.Clock.SimTime)
	if dt < 0 {
		dt = 0
	}
	return fmt.Sprintf("%s in %s  %s  %s", hudNodeMarker, readout.Duration(dt), nodeModeLabel(n.Mode), readout.DeltaV(n.DV))
}

// nodeEventLabel is the trigger event as the node row spells it: the
// long "next closest approach" shortens to "next approach" here only.
func nodeEventLabel(e spacecraft.TriggerEvent) string {
	if e == spacecraft.TriggerNextClosestApproach {
		return "next approach"
	}
	return e.String()
}
