package screens

import (
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// centerlineTitleRange is S3's hook (ADR 0055 A6): the title row's port
// range. Unsigned until S3 prints it negative while their port is behind
// yours.
func centerlineTitleRange(st sim.ProximityState) string {
	return readout.PortDistance(st.PortRangeM)
}
