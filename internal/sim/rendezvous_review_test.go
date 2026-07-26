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

// An invite from a peer in a diverged subspace is never JOINABLE — the
// [y] prompt must not offer a join that could not couple. Since #250 it
// still surfaces, as a Blocked attribution (the silent drop blamed
// nobody); the join affordance stays suppressed.
func TestRendezvousInviteRequiresSameSubspace(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.RendezvousTau = st.Add(72 * time.Hour)
	peer.SubspaceTime = st.Add(-time.Hour) // a real subspace divergence

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if inv := w.RendezvousInvite; inv == nil || !inv.Blocked {
		t.Errorf("invite = %+v across a subspace divergence, want surfaced-but-Blocked (#250)", inv)
	}
}

// #252 review, finding 2: hold-the-leader applies whenever the coast is
// engaged, waypoint resolved or not. The past-τ idle (waypoint derivation
// failing and retrying — a state the standing intent legitimized as
// long-lived) used to early-return before the hold case, so a paused or
// behind-diverged partner no longer froze the ahead side: the viewer
// walked on at the 1× floor, the pair decoupled past the tolerance
// window, and both then sat at 1× with a live arm and a gap that never
// closes — permanent divergence, exit only by manual cancel.
func TestRendezvousHoldAppliesPastTau(t *testing.T) {
	w, _, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	// Present and armed, but with no same-primary craft in the set — every
	// waypoint derivation comes up empty, so the coast idles and retries.
	peer := CoWarpPeer{
		Owner: "SHA256:gern", Handle: "gern", SubspaceTime: st,
		EffWarp: 50, ArmedTowardViewer: true, RendezvousTau: tau,
	}
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	// Past τ, derivation still failing; the partner pauses behind the
	// viewer → the viewer (ahead) must hold, not walk on at the 1× floor.
	w.Clock.SimTime = tau.Add(10 * time.Second)
	peer.Paused = true
	peer.SubspaceTime = tau.Add(-5 * time.Minute)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousArm == nil || !w.rendezvousWarpEngaged() {
		t.Fatal("the past-τ retry released the intent")
	}
	if !w.RendezvousHold {
		t.Error("no hold past τ for a paused, behind partner — the leader walks ahead and the pair diverges for good")
	}
	if got := w.EffectiveWarp(); got != 0 {
		t.Errorf("EffectiveWarp = %v during a past-τ hold, want 0", got)
	}

	// Partner resumes inside the tolerance → hold releases; the coast
	// keeps retrying the waypoint (deadlock-free: only the AHEAD side
	// ever holds, and it just stopped being ahead-of-a-stopped-clock).
	peer.Paused = false
	peer.SubspaceTime = w.Clock.SimTime
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousHold {
		t.Error("hold survived the partner's resume — the pair can't re-lock")
	}
	if w.RendezvousArm == nil || !w.rendezvousWarpEngaged() {
		t.Error("intent lost across the resume")
	}
}

// #252 review, finding 3: a transient peer dropout at the τ-crossing tick
// (all partner craft filtered that tick: landed / cross-system /
// degenerate KeplerStep → the whole peer absent from the set) must not
// destroy the standing intent and mis-chip "cancelled" — every other
// transient gap is held through. The partner has to stay absent for more
// than the tolerance window's worth of sim-time before the release fires
// as a retract.
func TestRendezvousCrossingDropoutGrace(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	// One-tick dropout at exactly the crossing tick.
	w.Clock.SimTime = tau
	w.DriveRendezvousWarp(nil)
	if w.RendezvousArm == nil || !w.rendezvousWarpEngaged() {
		t.Fatal("a one-tick peer dropout at the τ-crossing destroyed the standing intent")
	}
	if w.LastRendezvousWaypoint != nil {
		t.Error("waypoint advanced against a missing peer")
	}

	// Peer back next tick → the coast carries on and the grace clock
	// clears (a later dropout must get the FULL window again).
	w.Clock.SimTime = tau.Add(time.Second)
	peer.SubspaceTime = w.Clock.SimTime
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousArm == nil || !w.rendezvousWarpEngaged() {
		t.Fatal("intent lost after the peer came back")
	}
	if !w.RendezvousArm.peerGoneAt.IsZero() {
		t.Error("grace clock not cleared by the peer's return")
	}

	// Gone again, and STAYING gone past the tolerance window of sim-time
	// → released as a retract.
	w.Clock.SimTime = tau.Add(2 * time.Second)
	w.DriveRendezvousWarp(nil)
	if w.RendezvousArm == nil {
		t.Fatal("released on the first missing tick — no grace at all")
	}
	w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(coWarpSubspaceToleranceSec)*time.Second + time.Second)
	w.DriveRendezvousWarp(nil)
	if w.RendezvousArm != nil || w.rendezvousWarpEngaged() {
		t.Error("partner absent past the grace window still held — the dropout release never fires")
	}
}

// A genuine retract at the crossing tick — peer present, arm withdrawn —
// still releases both immediately, exactly as mid-coast: the finding-3
// grace is for peer DROPOUTS only, never for an explicit cancel.
func TestRendezvousRetractAtCrossingReleasesImmediately(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	w.Clock.SimTime = tau
	peer.SubspaceTime = tau
	peer.ArmedTowardViewer = false // partner cancelled at the waypoint
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousArm != nil || w.rendezvousWarpEngaged() {
		t.Error("a genuine retract at the crossing was held — the cancel must release both immediately")
	}
}

// #252 review, finding 4: a partner-relayed τ inside the waypoint min
// lead is not adopted. Adopting a τ 1 s out would cross it next tick and
// force an immediate re-resolution plus a spurious waypoint chip; our own
// derivation already refuses such a τ (rendezvousWaypointMinLead), so the
// adoption path must apply the same floor.
func TestRendezvousMinTauAdoptionRespectsMinLead(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}

	// Future and earlier than ours, but inside the min lead → skipped.
	peer.RendezvousTau = st.Add(time.Second)
	peer.RendezvousCA = 123
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousArm.Tau.Equal(tau) {
		t.Errorf("adopted a sub-min-lead relayed τ: arm.Tau = %v, want the held %v", w.RendezvousArm.Tau, tau)
	}
	if !w.AutoWarp.T.Equal(tau) {
		t.Errorf("driver re-frozen onto a sub-min-lead τ: T = %v", w.AutoWarp.T)
	}

	// Past the min lead → adopted (min-future-τ authority unchanged).
	adopt := st.Add(time.Minute)
	peer.RendezvousTau = adopt
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousArm.Tau.Equal(adopt) {
		t.Errorf("did not adopt the partner's earlier valid τ: arm.Tau = %v, want %v", w.RendezvousArm.Tau, adopt)
	}
}
