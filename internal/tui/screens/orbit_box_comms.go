package screens

import (
	"fmt"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// orbit_box_comms.go, the COMMS instrument box (ADR 0051 decision 5,
// re-grill Q9, slice 2a): the real link state for every vessel, one row.
// Replaces buildCommsChip, which returned nil (no box at all) for a
// crewed craft: "crewed craft are never gated" doesn't mean COMMS has
// nothing to say about them, per the re-grill: a crewed vessel reads
// DIRECT/CONNECTED like any other, and a disconnect reads "no signal" in
// Dim with no alarm glyph and no reason line, since the link doesn't
// command-gate a crewed craft (yet, the box stays live because a future
// science expansion may gate on it). The Alert colour, the ⚠ glyph, and
// the classified reason stay reserved for a vessel the link actually
// gates (an uncrewed, controllable probe).
func (v *OrbitView) buildCommsBox(w *sim.World) []string {
	title := v.theme.Primary.Render("COMMS")
	c := w.ActiveCraft()
	if c == nil || !w.CraftVisibleHere() {
		return []string{title, "  —"}
	}
	_, hops, connected := w.ActiveCommPath()
	reason := w.CommGraph.Reason(c.ID)
	return []string{title, v.commsBoxStatusLine(c, hops, connected, reason)}
}

// commsBoxStatusLine is the pure content selector behind buildCommsBox,
// split out so every branch is unit-testable without a live World's
// CommGraph. Fixed at exactly one content row (COMMS never grows), so a
// disconnected, command-gated vessel's classified reason folds onto the
// same line as the alarm rather than a separate row the way the retired
// buildCommsChip's Full form used two.
func (v *OrbitView) commsBoxStatusLine(c *spacecraft.Spacecraft, hops int, connected bool, reason sim.CommDisconnectReason) string {
	if connected {
		status := fmt.Sprintf("CONNECTED via %d hops", hops)
		if hops <= 1 {
			status = "DIRECT"
		}
		return "  " + status
	}
	if c.Crewed || !c.Controllable {
		// re-grill Q9: no alarm, no reason, the link doesn't gate this
		// vessel today.
		return "  " + v.theme.Dim.Render("no signal")
	}
	line := "⚠ NO SIGNAL"
	switch reason {
	case sim.CommDisconnectBlocked:
		line += ": no station in view, relay needed"
	case sim.CommDisconnectOutOfRange:
		line += ": out of range, stronger antenna needed"
	}
	return "  " + v.theme.Alert.Render(line)
}
