package sim

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TickMsg is emitted by the Bubble Tea runtime on each physics step.
// The embedded time.Time is the wall-clock time of the tick, unused by
// the physics integrator but handy for throttling UI updates.
type TickMsg time.Time

// TickCmd returns a tea.Cmd that fires a TickMsg after d wall-time.
// Callers typically re-issue TickCmd from Update() to keep the loop running.
func TickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return TickMsg(t) })
}
