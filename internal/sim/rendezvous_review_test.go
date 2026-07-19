package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// v0.29 batch-review regression tests: the arm/coast lifecycle fixes.

// Every disengage path releases the whole rendezvous, not just the
// driver — otherwise DriveRendezvousWarp restarts the coast next tick
// and manual warp keys / [G] become silent no-ops.
func TestDisengageAutoWarpCancelsRendezvousCoast(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(72*time.Hour), 0)
	w.DriveRendezvousWarp([]CoWarpPeer{armPeer(w, primary, st, 50, "gern")})
	if !w.RendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	w.DisengageAutoWarp() // the manual-warp-key path
	if w.AutoWarp != nil {
		t.Error("driver survived DisengageAutoWarp")
	}
	if w.RendezvousArm != nil {
		t.Error("arm survived DisengageAutoWarp — the drive would restart the coast")
	}

	// A non-rendezvous driver leaves an (un-started) arm alone: cancelling
	// a Sync must not silently retract a pending rendezvous intent.
	w.EngageRendezvousWarp("SHA256:gern", "gern", w.Clock.SimTime.Add(72*time.Hour), 0)
	w.AutoWarp = &AutoWarpTarget{T: w.Clock.SimTime.Add(time.Hour), Sync: true}
	w.DisengageAutoWarp()
	if w.RendezvousArm == nil {
		t.Error("cancelling a Sync retracted the pending rendezvous arm")
	}
}

// An un-started arm whose τ has passed expires instead of freezing the
// state machine (stuck "waiting" chip, all invites suppressed).
func TestRendezvousArmExpiresAtTau(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.ArmedTowardViewer = false // partner never responds

	w.Clock.SimTime = tau.Add(time.Second)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousArm != nil {
		t.Error("unanswered arm survived past τ")
	}
}

// Near τ the partner's arm clearing is their own arrival, not a retract:
// the laggard must finish its coast and arrive, not cancel short.
func TestRendezvousArrivalWindowNoCancel(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	// Inside the subspace-tolerance window of τ: partner un-arms (they
	// arrived) → keep coasting.
	w.Clock.SimTime = tau.Add(-time.Minute)
	peer.ArmedTowardViewer = false
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousWarpEngaged() {
		t.Error("laggard cancelled inside the arrival window instead of finishing the coast")
	}
}

// Hold-the-leader: a paused partner (or a divergence with the viewer
// ahead) freezes the viewer's effective warp instead of cancelling the
// encounter; the behind side keeps coasting to close the gap.
func TestRendezvousHoldOnPausedOrDivergedPartner(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}
	if w.RendezvousHold {
		t.Fatal("hold set with a live in-tolerance partner")
	}

	// Partner pauses at the same clock → hold; effective warp freezes.
	peer.Paused = true
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousHold {
		t.Error("no hold for a paused partner")
	}
	if w.EffectiveWarp() != 0 {
		t.Errorf("EffectiveWarp = %v during hold, want 0", w.EffectiveWarp())
	}
	if w.RendezvousWarpEngaged() != true || w.RendezvousArm == nil {
		t.Error("hold released the coast/arm — it must only freeze time")
	}

	// Partner unpauses → hold releases.
	peer.Paused = false
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousHold {
		t.Error("hold survived the partner's unpause")
	}

	// Viewer diverged AHEAD past the tolerance → hold (wait for them).
	peer.SubspaceTime = st.Add(-10 * time.Minute)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousHold {
		t.Error("no hold while ahead-diverged — the leader would sail to τ alone")
	}

	// Viewer BEHIND → no hold: it must keep coasting to catch up.
	peer.SubspaceTime = st.Add(10 * time.Minute)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousHold {
		t.Error("hold set while behind — the laggard could never catch up")
	}
}

// A re-commit must reconcile the engaged coast: same partner + new τ
// re-freezes the driver's T; a new partner re-aims the coast.
func TestRendezvousReArmReconciles(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau1 := st.Add(48 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau1, 0)
	gern := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{gern})
	if !w.AutoWarp.T.Equal(tau1) {
		t.Fatal("precondition: coasting to τ1")
	}

	// Same partner, new τ → the driver re-freezes.
	tau2 := st.Add(60 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau2, 0)
	w.DriveRendezvousWarp([]CoWarpPeer{gern})
	if !w.AutoWarp.T.Equal(tau2) {
		t.Errorf("driver T = %v after re-commit, want τ2 %v (split-brain τ)", w.AutoWarp.T, tau2)
	}

	// New partner → the coast re-aims toward them.
	tau3 := st.Add(72 * time.Hour)
	bob := armPeer(w, primary, st, 50, "bob")
	w.EngageRendezvousWarp("SHA256:bob", "bob", tau3, 0)
	w.DriveRendezvousWarp([]CoWarpPeer{gern, bob})
	if w.AutoWarp == nil || w.AutoWarp.RendezvousOwner != "SHA256:bob" || !w.AutoWarp.T.Equal(tau3) {
		t.Errorf("coast after partner switch = %+v, want aimed at bob/τ3", w.AutoWarp)
	}
}

// The coast start must not clobber an engaged Sync (or node-chase): the
// player's later explicit driver wins, the mutual arm waits its turn.
func TestRendezvousStartWaitsForEngagedSync(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(72*time.Hour), 0)
	syncT := st.Add(time.Hour)
	w.AutoWarp = &AutoWarpTarget{T: syncT, Sync: true, SyncOwner: "SHA256:dave", SyncHandle: "dave"}

	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.AutoWarp.Sync || !w.AutoWarp.T.Equal(syncT) {
		t.Fatalf("mutual arm clobbered the engaged Sync: %+v", w.AutoWarp)
	}

	// Sync released → the waiting mutual arm starts the coast.
	w.AutoWarp = nil
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousWarpEngaged() {
		t.Error("coast did not start once the Sync driver released")
	}
}

// Degrade baselines at the coast-start measure, not the committed
// (post-burn) promise — arming via the advisory without the burn having
// fired yet must not flag "degraded" from the first tick.
func TestRendezvousDegradeBaselinesAtCoastStart(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Minute) // short dt: approach at τ ≈ the offset
	a := w.ActiveCraft()
	at := func(dR orbital.Vec3) CoWarpPeer {
		p := CoWarpPeer{
			Owner: "SHA256:gern", Handle: "gern", SubspaceTime: st, EffWarp: 50,
			ArmedTowardViewer: true,
			Crafts:            []CoWarpCraft{{Primary: primary, R: a.State.R.Add(dR), V: a.State.V}},
		}
		return p
	}

	// Committed CA 0 (the advisory's post-burn promise) but the current
	// course passes 50 km out: no degrade — that IS the coast-start state.
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	w.DriveRendezvousWarp([]CoWarpPeer{at(orbital.Vec3{X: 50_000})})
	if !w.RendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}
	if w.RendezvousDegraded {
		t.Error("degraded on the first tick — the committed post-burn CA must not be the baseline")
	}

	// The encounter actually worsens past a couple-radius → degrade.
	w.DriveRendezvousWarp([]CoWarpPeer{at(orbital.Vec3{X: 65_000})})
	if !w.RendezvousDegraded {
		t.Error("no degrade after the encounter slipped 15 km past the coast-start measure")
	}
}

// The degrade watchdog warns — rather than going silently blind — when
// the armed partner no longer has any craft in the anchor's SOI.
func TestRendezvousDegradeCrossPrimaryWarns(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	// Partner's only craft leaves for another SOI mid-coast.
	peer.Crafts[0].Primary = "elsewhere"
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousDegraded {
		t.Error("watchdog went blind: partner left the SOI and no degrade was flagged")
	}
}

// The approach recompute measures the partner's NEAREST same-primary
// craft, not whichever is first in report order.
func TestRendezvousCAUsesNearestCraft(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Minute)
	a := w.ActiveCraft()
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := CoWarpPeer{
		Owner: "SHA256:gern", Handle: "gern", SubspaceTime: st, EffWarp: 50,
		ArmedTowardViewer: true,
		Crafts: []CoWarpCraft{
			// A far probe listed first must not mask the near partner craft.
			{Primary: primary, R: a.State.R.Add(orbital.Vec3{X: 200_000}), V: a.State.V},
			{Primary: primary, R: a.State.R.Add(orbital.Vec3{X: 5_000}), V: a.State.V},
		},
	}
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousApproachM <= 0 || w.RendezvousApproachM > 20_000 {
		t.Errorf("approach = %.0f m, want the near craft (~5 km), not the far probe", w.RendezvousApproachM)
	}
}

// An invite from a peer in a diverged subspace is not surfaced — the
// [y] prompt must never offer a join that could not couple.
func TestRendezvousInviteRequiresSameSubspace(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.RendezvousTau = st.Add(72 * time.Hour)
	peer.SubspaceTime = st.Add(-time.Hour) // a real subspace divergence

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousInvite != nil {
		t.Errorf("invite surfaced across a subspace divergence: %+v", w.RendezvousInvite)
	}
}
