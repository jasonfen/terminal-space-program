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
	srv   *Server
	owner string

	// handle cache: fingerprint → display name, refreshed lazily so
	// each tick doesn't re-read session.json. A new enrollee's handle
	// appears within the refresh window.
	handles   map[string]string
	handlesAt time.Time
}

const handleRefresh = 5 * time.Second

// withReporting wraps app so its world reports to the store as owner.
func (s *Server) withReporting(app *tui.App, owner string) tea.Model {
	return reportingModel{
		inner: app, app: app,
		rep: relay.NewReporter(s.relay, owner),
		srv: s, owner: owner,
	}
}

func (m reportingModel) Init() tea.Cmd { return m.inner.Init() }

func (m reportingModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	inner, cmd := m.inner.Update(msg)
	m.inner = inner
	if _, ok := msg.(sim.TickMsg); ok {
		now := time.Now()
		w := m.app.World()
		m.rep.Tick(w, now)
		// v0.27 S5: refresh this world's ghost slate from the store —
		// the screens read w.Ghosts like any other world state.
		if m.handles == nil || now.Sub(m.handlesAt) >= handleRefresh {
			m.handles = m.rosterHandles()
			m.handlesAt = now
		}
		w.Ghosts = relay.GhostsFor(w, m.srv.relay.Snapshot(m.owner), m.handles)
	}
	return m, cmd
}

// rosterHandles reads the fingerprint→handle join from the session
// roster. Failures yield an empty map (ghosts render nameless rather
// than not at all).
func (m reportingModel) rosterHandles() map[string]string {
	meta, err := m.srv.store.Meta()
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(meta.Roster))
	for _, p := range meta.Roster {
		out[p.Fingerprint] = p.Handle
	}
	return out
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
