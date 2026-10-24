package screens

import (
	"fmt"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// buildSpreadChip is the SPREAD notice (ADR 0059 decision 5): the active
// vessel's batch, counted off its members' orders each frame. It keeps no
// state, so it is gone the frame no member has an order. Nil otherwise.
func (v *OrbitView) buildSpreadChip(w *sim.World) []string {
	c := w.ActiveCraft()
	if c == nil || c.BatchID == 0 {
		return nil
	}
	st, ok := w.SpreadStatus(c.BatchID)
	if !ok {
		return nil
	}
	counts := fmt.Sprintf("  arrived %d · climbing %d · waiting %d", st.Arrived, st.Climbing, st.Waiting)
	if st.Stopped > 0 {
		counts += fmt.Sprintf(" · stopped %d", st.Stopped)
	}
	lines := []string{
		v.theme.Primary.Render("SPREAD") + v.theme.Dim.Render(fmt.Sprintf("  %s x%d → %s", st.Name, st.Total, AltitudeKm(st.TargetAltM))),
		v.theme.Dim.Render(counts),
	}
	if st.Waiting > 0 {
		lines = append(lines, v.theme.Dim.Render(fmt.Sprintf("  next: %s in %s", st.NextName, readout.Duration(st.NextIn))))
	}
	return lines
}
