package screens

import (
	"fmt"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// orbit_box_engine.go — the ENGINE instrument box (ADR 0051 decision 1,
// slice 2a): throttle with the lit state and elapsed time, mode, TWR
// (current with the max-throttle figure, decision 13b), and the node row
// with its three-way precedence (decision 12, re-grill Q2/Q4): a live
// burn, then the latest safe braking start while one exists, then a
// queued node, else a dash. Replaces buildAttitudeChip's engine:/manual:
// rows and buildLaunchChip/buildDescentChip's twr:/engine: rows, and
// folds in the live-burn head buildNodesChip used to carry
// (activeBurnLines) plus its queued-node line (nextQueuedNodeLine) for
// the ACTIVE craft only — a per-vessel instrument box has no reason to
// show a different vessel's queue the way the old fleet-wide NODES chip
// did.
//
// Every row is always present (decision 2): with no active craft at all,
// every cell reads a dash rather than the box vanishing.
func (v *OrbitView) buildEngineBox(w *sim.World) []string {
	title := v.theme.Primary.Render("ENGINE") + v.vesselBurnBadge(w)
	c := w.ActiveCraft()
	if c == nil {
		return []string{
			title,
			chipRow2("throttle:", "—", "mode:", "—"),
			v.engineTWRCell(nil),
			v.engineNodeLine(w, nil),
		}
	}
	return []string{
		title,
		chipRow2("throttle:", v.engineThrottleLabel(w, c), "mode:", c.EngineMode.String()),
		v.engineTWRCell(c),
		v.engineNodeLine(w, c),
	}
}

// engineThrottleLabel renders the throttle row's value: the commanded
// throttle setting is always shown (never gated), with a trailing state
// telling whether an engine is actually lit right now and, while it is,
// how long it has been firing. Mirrors the pre-ADR-0051 throttleRow/
// buildLaunchChip elapsed logic, now unified onto one row instead of
// split across VESSEL's "(idle)"/"● FIRING" and SURFACE's "● LIT".
func (v *OrbitView) engineThrottleLabel(w *sim.World, c *spacecraft.Spacecraft) string {
	base := fmt.Sprintf("%.0f%%", c.EffectiveThrottle()*100)
	if !sim.StackMidBurn(c) {
		return base + v.theme.Dim.Render(" idle")
	}
	firing := v.theme.Warning.Render("● FIRING")
	elapsed := ""
	switch {
	case c.ManualBurn != nil:
		elapsed = " " + readout.Countdown(-(w.Clock.SimTime.Sub(c.ManualBurn.StartTime)))
	case c.ActiveBurn != nil:
		remaining := c.ActiveBurn.EndTime.Sub(w.Clock.SimTime)
		if remaining < 0 {
			remaining = 0
		}
		elapsed = " " + readout.Countdown(remaining)
	}
	return base + " " + firing + elapsed
}

// engineTWRCell renders the TWR row (decision 13b): the current-throttle
// figure, with the max-throttle figure alongside it, and "(will not
// lift)" only when even full throttle can't clear 1.0 — the verdict
// judges the maximum, not the current setting. Whether the "(max N)"
// suffix still prints once the throttle is already at maximum is not
// ruled by the ADR (decision 13b's open note); this build choice omits
// it there (nothing new to say when current equals max), a reversible
// call, not a measured one.
func (v *OrbitView) engineTWRCell(c *spacecraft.Spacecraft) string {
	if c == nil || c.Thrust <= 0 || c.TotalMass() <= 0 {
		return chipRowAt(readout.LabelTWR, "—", boxValueCol)
	}
	g := c.Primary.GravitationalParameter() / (c.Primary.RadiusMeters() * c.Primary.RadiusMeters())
	cur := c.Thrust * c.EffectiveThrottle() / (c.TotalMass() * g)
	max := c.Thrust / (c.TotalMass() * g)
	curLabel := fmt.Sprintf("%.2f", cur)
	maxLabel := fmt.Sprintf("%.2f", max)
	var value string
	switch {
	case max < 1.0:
		value = v.theme.Alert.Render(fmt.Sprintf("%s (max %s, will not lift)", curLabel, maxLabel))
	case curLabel == maxLabel:
		value = curLabel
	default:
		value = fmt.Sprintf("%s (max %s)", curLabel, maxLabel)
	}
	return chipRowAt(readout.LabelTWR, value, boxValueCol)
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
		return chipRowAt("node:", "—", boxValueCol)
	}
	if line, ok := v.engineBurnLine(w, c); ok {
		return chipRowAt("node:", line, boxValueCol)
	}
	if _, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon); descending {
		stopDat := v.cachedDescentStop(w, c)
		if stopDat.hasBurnAt {
			line := fmt.Sprintf("%s braking burn at %s  %s", hudNodeMarker,
				readout.Distance(stopDat.burnAt.AltitudeM),
				readout.Countdown(secondsToDuration(stopDat.burnAt.InSec)))
			return chipRowAt("node:", line, boxValueCol)
		}
	}
	if len(c.Nodes) > 0 {
		return chipRowAt("node:", v.engineQueuedNodeLine(w, c), boxValueCol)
	}
	return chipRowAt("node:", "—", boxValueCol)
}

// engineBurnLine is engineNodeLine's live-burn branch: the active
// craft's OWN ActiveBurn (a manual burn with no node carries no entry
// here — the throttle row already says it's firing). Adapted from the
// retired activeBurnLines/buildNodesChip, dropping the "vessel N" tag
// (this box is already scoped to the active craft, so it would be
// redundant) — ok is false when nothing is burning.
func (v *OrbitView) engineBurnLine(w *sim.World, c *spacecraft.Spacecraft) (string, bool) {
	if c == nil || c.ActiveBurn == nil {
		return "", false
	}
	ab := c.ActiveBurn
	if c.BurnStalled() {
		return v.theme.Warning.Render(fmt.Sprintf("%s %s, Δv %s  ⚠ STALLED (x to cancel)",
			hudNodeMarker, ab.Mode.String(), readout.DeltaV(ab.DVRemaining))), true
	}
	remaining := ab.EndTime.Sub(w.Clock.SimTime).Seconds()
	if remaining < 0 {
		remaining = 0
	}
	return v.theme.Warning.Render(fmt.Sprintf("%s %s, Δv %s, burning, %s left",
		hudNodeMarker, ab.Mode.String(), readout.DeltaV(ab.DVRemaining), readout.Duration(secondsToDuration(remaining)))), true
}

// engineQueuedNodeLine is engineNodeLine's queued-node branch: the
// active craft's first planted node (c.Nodes[0]), with an overflow count
// when more are queued. Adapted from the retired nextQueuedNodeLine, with
// two changes for this box: no per-craft label (always this vessel's own
// node, so "#1" is unambiguous without a "c%d#%d" prefix) and the
// over-budget suffix shortens to a bare "⚠" in the Alert colour
// (re-grill Q4) instead of "exceeds budget by <Δv>" — the words move to
// the F1 glossary and stay in the planner's own list (slice 2b / #maneuver.go).
func (v *OrbitView) engineQueuedNodeLine(w *sim.World, c *spacecraft.Spacecraft) string {
	n := c.Nodes[0]
	over := ""
	if _, isOver := n.OverBudget(c); isOver {
		over = "  " + v.theme.Alert.Render("⚠")
	}
	count := ""
	if len(c.Nodes) > 1 {
		count = v.theme.Dim.Render(fmt.Sprintf("  (+%d more → [m])", len(c.Nodes)-1))
	}
	if !n.IsResolved() {
		return fmt.Sprintf("%s #1 %s  %s  %s", hudNodeMarker, n.Event.String(), n.Mode.String(), readout.DeltaV(n.DV)) + over + count
	}
	dt := n.BurnStart().Sub(w.Clock.SimTime)
	if dt < 0 {
		dt = 0
	}
	return fmt.Sprintf("%s #1 ignition in %s  %s  %s", hudNodeMarker, readout.Duration(dt), n.Mode.String(), readout.DeltaV(n.DV)) + over + count
}
