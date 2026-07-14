package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// Through the reporting model: a coincident same-subspace partner couples
// the host's world (min-over-Effective clamp written to World.CoWarp) and
// fires a "warp coupled with <name>" chip. Exercised end-to-end under
// -race across the store seam.
func TestCoWarpChipThroughReportingModel(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = srv.HostModel(hostApp)
	m = tick(m) // establish the host in the store + world

	// Report gern coincident with the host's craft in the same subspace.
	w := hostApp.World()
	hc := w.ActiveCraft()
	srv.relay.Report(relay.CraftReport{
		Owner:        "SHA256:gern",
		SubspaceTime: w.Clock.SimTime,
		EffWarp:      10, // gern is burn-capped
		Crafts: []relay.CraftState{{
			ID:      42,
			Name:    "Gernaut",
			System:  w.System().Name,
			Primary: hc.Primary.ID,
			R:       hc.State.R,
			V:       hc.State.V,
		}},
	})

	// Next tick evaluates co-warp: couples, clamps, and chips.
	m, _ = m.Update(sim.TickMsg(time.Now()))
	_ = m

	if !w.CoWarp.Coupled {
		t.Fatalf("host world not coupled after coincident report: %+v", w.CoWarp)
	}
	if w.CoWarp.MinWarp != 10 {
		t.Errorf("CoWarp.MinWarp = %v, want gern's 10", w.CoWarp.MinWarp)
	}
	var coupled bool
	for _, e := range w.SessionEvents {
		if e.Kind == sim.SessionEventCoWarpCoupled && e.Handle == "gern" {
			coupled = true
		}
	}
	if !coupled {
		t.Errorf("no couple chip emitted: %+v", w.SessionEvents)
	}
}
