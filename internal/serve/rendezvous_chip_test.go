package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// gernArmedReport reports gern coincident with the host's craft, armed
// toward the host with a committed encounter τ.
func gernArmedReport(w *sim.World, target string, tau time.Time) relay.CraftReport {
	hc := w.ActiveCraft()
	return relay.CraftReport{
		Owner:        "SHA256:gern",
		SubspaceTime: w.Clock.SimTime,
		EffWarp:      10,
		Crafts: []relay.CraftState{{
			ID: 42, Name: "Gernaut", System: w.System().Name,
			Primary: hc.Primary.ID, R: hc.State.R, V: hc.State.V,
		}},
		RendezvousTarget: target,
		RendezvousTau:    tau,
		RendezvousCA:     500,
	}
}

// A peer arming toward the host surfaces the invite slate, the
// "wants rendezvous" chip, and the roster-row marker (v0.29 S2).
func TestRendezvousArmChipAndRosterMarker(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = srv.HostModel(hostApp)
	m = tick(m)

	w := hostApp.World()
	tau := w.Clock.SimTime.Add(3 * time.Hour)
	srv.relay.Report(gernArmedReport(w, sessiondir.HostFingerprint, tau))

	m, _ = m.Update(sim.TickMsg(time.Now()))
	_ = m

	inv := w.RendezvousInvite
	if inv == nil || inv.Owner != "SHA256:gern" || !inv.Tau.Equal(tau) || inv.CA != 500 {
		t.Fatalf("invite slate = %+v, want gern's committed τ+CA", inv)
	}
	var armed bool
	for _, e := range w.SessionEvents {
		if e.Kind == sim.SessionEventRendezvousArmed && e.Handle == "gern" {
			armed = true
		}
	}
	if !armed {
		t.Errorf("no wants-rendezvous chip: %+v", w.SessionEvents)
	}
	var marker bool
	for _, p := range w.Session.Players {
		if p.Fingerprint == "SHA256:gern" && p.WantsRendezvous {
			marker = true
		}
	}
	if !marker {
		t.Errorf("roster row missing the WantsRendezvous marker: %+v", w.Session.Players)
	}
}

// The host's own outgoing arm marks the partner row RendezvousOut; a
// partner retract mid-coast releases the host and fires the cancelled
// chip (v0.29 S2).
func TestRendezvousCancelChipOnRetract(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = srv.HostModel(hostApp)
	m = tick(m)

	w := hostApp.World()
	tau := w.Clock.SimTime.Add(3 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 500)
	srv.relay.Report(gernArmedReport(w, sessiondir.HostFingerprint, tau))

	m, _ = m.Update(sim.TickMsg(time.Now()))
	if !w.RendezvousWarpEngaged() {
		t.Fatal("mutual arm did not start the coast through the reporting model")
	}
	var outMarker bool
	for _, p := range w.Session.Players {
		if p.Fingerprint == "SHA256:gern" && p.RendezvousOut {
			outMarker = true
		}
	}
	if !outMarker {
		t.Errorf("roster row missing the RendezvousOut marker: %+v", w.Session.Players)
	}

	// gern retracts (re-reports with no intent) → release + cancelled chip.
	srv.relay.Report(gernArmedReport(w, "", time.Time{}))
	m, _ = m.Update(sim.TickMsg(time.Now()))
	_ = m
	if w.RendezvousWarpEngaged() || w.RendezvousArm != nil {
		t.Error("retract did not release the host's arm/coast")
	}
	var cancelled bool
	for _, e := range w.SessionEvents {
		if e.Kind == sim.SessionEventRendezvousCancelled && e.Handle == "gern" {
			cancelled = true
		}
	}
	if !cancelled {
		t.Errorf("no cancelled chip after the retract: %+v", w.SessionEvents)
	}
}

// Reaching a waypoint outside couple range through the reporting model
// (#252): the arm survives, the waypoint advances, and the wrapper
// consumes the advance slate into the waypoint chip — never a silent
// advance, never an arrival/cancel chip.
func TestRendezvousWaypointChip(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = srv.HostModel(hostApp)
	m = tick(m)

	w := hostApp.World()
	tau := w.Clock.SimTime.Add(2 * time.Minute)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 5.4e6)
	// gern sits ~5,400 km ahead along-track on a slightly longer period —
	// far outside the couple gate at τ, but with a FUTURE closest approach
	// inside the commit horizon so the next waypoint can be derived (a
	// plain radial offset has its closest approach at t=0).
	hc := w.ActiveCraft()
	ahead, ok := physics.KeplerStep(
		physics.StateVector{R: hc.State.R, V: hc.State.V, M: 1},
		hc.Primary.GravitationalParameter(), 735)
	if !ok {
		t.Fatal("kepler step failed placing gern's craft")
	}
	far := gernArmedReport(w, sessiondir.HostFingerprint, tau)
	far.Crafts[0].R, far.Crafts[0].V = ahead.R, ahead.V.Scale(1.002)
	srv.relay.Report(far)
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if !w.RendezvousWarpEngaged() {
		t.Fatal("mutual arm did not start the coast")
	}

	// Cross τ far apart; keep the partner's report in the same subspace.
	w.Clock.SimTime = tau
	far2 := gernArmedReport(w, sessiondir.HostFingerprint, tau)
	far2.Crafts[0].R, far2.Crafts[0].V = ahead.R, ahead.V.Scale(1.002)
	far2.SubspaceTime = tau
	srv.relay.Report(far2)
	m, _ = m.Update(sim.TickMsg(time.Now()))
	_ = m

	if w.RendezvousArm == nil || !w.RendezvousWarpEngaged() {
		t.Fatalf("standing intent did not survive τ outside couple range: arm=%+v autowarp=%+v",
			w.RendezvousArm, w.AutoWarp)
	}
	if !w.RendezvousArm.Tau.After(tau) {
		t.Errorf("waypoint did not advance: τ=%v (committed %v)", w.RendezvousArm.Tau, tau)
	}
	if w.LastRendezvousWaypoint != nil {
		t.Error("waypoint slate not consumed by the wrapper")
	}
	var waypoint, arrivedOrCancelled bool
	for _, e := range w.SessionEvents {
		switch e.Kind {
		case sim.SessionEventRendezvousWaypoint:
			if e.Handle == "gern" {
				waypoint = true
			}
		case sim.SessionEventRendezvousArrived, sim.SessionEventRendezvousCancelled:
			arrivedOrCancelled = true
		}
	}
	if !waypoint {
		t.Errorf("no waypoint chip after the advance: %+v", w.SessionEvents)
	}
	if arrivedOrCancelled {
		t.Errorf("waypoint advance mis-chipped as arrival/cancel: %+v", w.SessionEvents)
	}
}

// Arrival at τ through the reporting model: the coast's arrival slate is
// consumed into the arrived chip (v0.29 S2), mirroring the Sync arrival.
func TestRendezvousArrivalChip(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")

	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = srv.HostModel(hostApp)
	m = tick(m)

	w := hostApp.World()
	// A near τ: the first tick starts the coast, then the sim clock steps
	// past τ and resolveAutoWarp records the arrival. Enough headroom that
	// the arm can't hit the expiry gate before the coast engages (the
	// clock runs in wall time at 1× under -race scheduling).
	tau := w.Clock.SimTime.Add(500 * time.Millisecond)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 500)
	srv.relay.Report(gernArmedReport(w, sessiondir.HostFingerprint, tau))
	m, _ = m.Update(sim.TickMsg(time.Now()))
	if !w.RendezvousWarpEngaged() {
		t.Fatal("coast did not engage on the mutual arm")
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		m, _ = m.Update(sim.TickMsg(time.Now()))
		var arrived bool
		for _, e := range w.SessionEvents {
			if e.Kind == sim.SessionEventRendezvousArrived && e.Handle == "gern" {
				arrived = true
			}
		}
		if arrived {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no arrived chip before deadline: events=%+v arm=%+v autowarp=%+v",
				w.SessionEvents, w.RendezvousArm, w.AutoWarp)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if w.LastRendezvousArrival != nil {
		t.Error("arrival slate not consumed by the wrapper")
	}
}
