package serve

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// reportingModel wraps a session's game and feeds the relay store on
// every sim tick (v0.27 S4). Reports run inside the session's own
// update loop — same goroutine as the World mutation, so no locking
// enters the sim. The wrapper is transparent to everything else:
// input, rendering, and quit pass straight through.
type reportingModel struct {
	inner tea.Model
	app   *tui.App
	rep   *relay.Reporter
}

// withReporting wraps app so its world reports to the store as owner.
func (s *Server) withReporting(app *tui.App, owner string) tea.Model {
	return reportingModel{inner: app, app: app, rep: relay.NewReporter(s.relay, owner)}
}

func (m reportingModel) Init() tea.Cmd { return m.inner.Init() }

func (m reportingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	inner, cmd := m.inner.Update(msg)
	m.inner = inner
	if _, ok := msg.(sim.TickMsg); ok {
		m.rep.Tick(m.app.World(), time.Now())
	}
	return m, cmd
}

func (m reportingModel) View() string { return m.inner.View() }

// HostModel wraps the host's own in-process game so the host's craft
// enter the store like any guest's (the host is roster entry #1, not
// a special case on the wire). main runs the returned model.
func (s *Server) HostModel(app *tui.App) tea.Model {
	return s.withReporting(app, sessiondir.HostFingerprint)
}

// Relay exposes the session store (S5/S6 read ghosts and roster
// state from it).
func (s *Server) Relay() *relay.Store { return s.relay }
