package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Reaching the committed τ inside the couple RANGE hands the ship back
// regardless of the velocity term (#299): a transfer-orbit encounter
// always arrives fast, and killing that velocity needs the pilot burning
// at closest approach — which needs the driver released. The velocity
// term still gates Proximity Co-Warp coupling itself; arriving fast just
// means released-but-not-coupled.
func TestRendezvousRangeOnlyHandoffAtTau(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	// 5 km out at ~594 m/s relative — the live playtest shape (#299):
	// deep inside the 35 km range gate, 6× over the 100 m/s velocity term.
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{X: 594}, "gern")
	near.ArmedTowardViewer = true
	near.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	w.Clock.WarpIdx = 5
	w.Clock.SimTime = tau // reached the encounter, 5 km out, fast
	near.SubspaceTime = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})

	if w.rendezvousWarpEngaged() {
		t.Error("coast still engaged at τ inside couple range — a fast arrival must still hand the ship back")
	}
	// ADR 0037 §1: the driver goes, the agreement is demoted — the pair
	// stays time-locked through the braking burns that follow.
	if w.RendezvousArm == nil || !w.RendezvousArm.Approach {
		t.Errorf("arm not demoted to the approach phase at τ inside couple range: %+v", w.RendezvousArm)
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("did not drop to 1× at arrival: WarpIdx = %d", w.Clock.WarpIdx)
	}
	if w.LastRendezvousArrival == nil || w.LastRendezvousArrival.Owner != "SHA256:gern" {
		t.Errorf("arrival not recorded for the chip: %+v", w.LastRendezvousArrival)
	}
	if w.LastRendezvousWaypoint != nil {
		t.Error("a range-satisfying τ is an arrival, not a waypoint advance")
	}
}

// Manual thrust supersedes the standing rendezvous intent (#298): the
// pilot igniting the main engine under an engaged coast releases arm +
// driver, mirroring how the driver already yields to planted node burns.
// The two control authorities must never fight over warp.
func TestManualThrustReleasesRendezvousCoast(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 5.4e6)
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 5.4e6}, orbital.Vec3{}, "gern")
	near.ArmedTowardViewer = true
	near.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	w.StartManualBurn()
	if w.ActiveCraft().ManualBurn == nil {
		t.Fatal("precondition: manual burn ignited")
	}
	if w.rendezvousWarpEngaged() {
		t.Error("manual ignition left the rendezvous coast engaged — the driver fights the burn (#298)")
	}
	if w.RendezvousArm != nil {
		t.Error("manual ignition left the standing arm — DriveRendezvousWarp would restart the coast next tick")
	}
}

// An RCS pulse is Δv the same as a main-engine burn (#298): pulsing
// under an engaged coast releases the standing intent too — a velocity
// match flown on RCS must not leave the driver warping against it.
func TestRCSPulseReleasesRendezvousCoast(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 5.4e6)
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 5.4e6}, orbital.Vec3{}, "gern")
	near.ArmedTowardViewer = true
	near.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	w.ActiveCraft().EngineMode = spacecraft.EngineRCS
	if !w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Fatal("precondition: RCS pulse fired")
	}
	if w.rendezvousWarpEngaged() {
		t.Error("RCS pulse left the rendezvous coast engaged (#298)")
	}
	if w.RendezvousArm != nil {
		t.Error("RCS pulse left the standing arm — the coast would restart next tick")
	}
}
