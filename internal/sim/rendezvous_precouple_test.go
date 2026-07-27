package sim

import (
	"testing"
	"time"
)

// #250: the pre-couple window. Between one player Engaging a Rendezvous
// Warp and the partner Engaging back, warping can move the pair out of
// the same-subspace window — the coast then cannot start no matter what
// the partner does. The world classifies WHY the coast has not started
// (RendezvousWait) so the HUD can say so instead of blaming the partner,
// and the responder's invite survives the gap as a Blocked prompt
// instead of silently vanishing.

func gapDuration() time.Duration {
	return time.Duration(coWarpSubspaceToleranceSec+1) * time.Second
}

// An armed viewer whose partner has not Engaged back is genuinely
// waiting; the same arm across a subspace divergence is not — the
// classification must distinguish them, with the signed Δt saying who
// is ahead, and revert to waiting once the pair converges again.
func TestRendezvousWaitClassifiesPartnerVsGap(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.ArmedTowardViewer = false // partner hasn't Engaged back
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Handle: "gern", Tau: tau}

	// Same subspace, partner not armed → genuinely waiting on them.
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if got := w.RendezvousWait; got.Reason != RendezvousWaitPartner {
		t.Fatalf("Wait = %+v, want Reason=RendezvousWaitPartner while in-window", got)
	}

	// Viewer warped past the window → subspace gap, viewer ahead (+Δt).
	gap := gapDuration()
	peer.SubspaceTime = st.Add(-gap)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	got := w.RendezvousWait
	if got.Reason != RendezvousWaitSubspaceGap {
		t.Fatalf("Wait = %+v, want Reason=RendezvousWaitSubspaceGap past the window", got)
	}
	if got.AheadBy != gap {
		t.Errorf("AheadBy = %v, want +%v (viewer ahead)", got.AheadBy, gap)
	}

	// Partner ahead instead → the sign flips.
	peer.SubspaceTime = st.Add(gap)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if got := w.RendezvousWait; got.Reason != RendezvousWaitSubspaceGap || got.AheadBy != -gap {
		t.Errorf("Wait = %+v, want gap with AheadBy=-%v (partner ahead)", got, gap)
	}

	// Back inside the window → plain waiting again.
	peer.SubspaceTime = st.Add(30 * time.Second)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if got := w.RendezvousWait; got.Reason != RendezvousWaitPartner || got.AheadBy != 0 {
		t.Errorf("Wait = %+v, want bare waiting after converging", got)
	}
}

// Even a partner who HAS Engaged back cannot couple across the gap: the
// coast must not start, and the reason is the gap — not waiting.
func TestRendezvousWaitGapWithMutualArm(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	gap := gapDuration()
	peer := armPeer(w, primary, st.Add(-gap), 50, "gern")
	peer.RendezvousTau = tau
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Handle: "gern", Tau: tau}

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousWarpEngaged() {
		t.Fatal("coast started across a subspace gap")
	}
	if got := w.RendezvousWait; got.Reason != RendezvousWaitSubspaceGap || got.AheadBy != gap {
		t.Fatalf("Wait = %+v, want gap with AheadBy=+%v", got, gap)
	}

	// Converged → the coast starts and the wait slate clears.
	peer.SubspaceTime = st
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousWarpEngaged() {
		t.Fatal("coast did not start after converging")
	}
	if got := w.RendezvousWait; got.Reason != RendezvousWaitNone {
		t.Errorf("Wait = %+v while coasting, want RendezvousWaitNone", got)
	}
}

// A partner absent from the peer set entirely (no report) is the bare
// waiting state — there is no Δt to attribute.
func TestRendezvousWaitPartnerAbsent(t *testing.T) {
	w, _, st := anchorWorld(t)
	w.RendezvousArm = &RendezvousArm{TargetOwner: "SHA256:gern", Handle: "gern", Tau: st.Add(72 * time.Hour)}
	w.DriveRendezvousWarp(nil)
	if got := w.RendezvousWait; got.Reason != RendezvousWaitPartner || got.AheadBy != 0 {
		t.Errorf("Wait = %+v with no partner report, want bare waiting", got)
	}
}

// #260: with the partner armed back but the viewer's own Sync engaged,
// driveRendezvousCoast defers the coast start (the don't-clobber rule) —
// the classification must be RendezvousWaitSelf, not Partner: the partner
// did join, and the wait is the viewer's own driver. Once that driver
// releases, the deferred coast starts on the next drive tick.
func TestRendezvousWaitSelfWhileOwnDriverDefers(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	peer := armPeer(w, primary, st, 50, "gern") // partner HAS armed back
	peer.RendezvousTau = tau
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Handle: "gern", Tau: tau}
	if !w.EngageSyncWarp(st.Add(time.Hour), "SHA256:vex", "vex") {
		t.Fatal("Sync refused")
	}

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousWarpEngaged() {
		t.Fatal("coast clobbered the engaged Sync")
	}
	if got := w.RendezvousWait; got.Reason != RendezvousWaitSelf || got.AheadBy != 0 {
		t.Fatalf("Wait = %+v, want RendezvousWaitSelf (own driver defers, partner joined)", got)
	}

	// Own driver releases → the deferred coast starts, wait slate clears.
	w.DisengageAutoWarp()
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousWarpEngaged() {
		t.Fatal("coast did not start after the own driver released")
	}
	if got := w.RendezvousWait; got.Reason != RendezvousWaitNone {
		t.Errorf("Wait = %+v after the coast started, want RendezvousWaitNone", got)
	}
}

// #260 precedence: an own-driver deferral ALSO across a subspace gap
// classifies Self, mirroring the driver's order (the don't-clobber check
// runs before the same-subspace gate) — the player must release their own
// driver first regardless, and that driver may be the very Sync that is
// closing the gap. Once it releases, the gap becomes the live reason.
func TestRendezvousWaitSelfWinsOverGapUntilRelease(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	gap := gapDuration()
	peer := armPeer(w, primary, st.Add(-gap), 50, "gern") // armed back, viewer ahead
	peer.RendezvousTau = tau
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Handle: "gern", Tau: tau}
	w.AutoWarp = &AutoWarpTarget{T: st.Add(time.Hour)} // an engaged node-chase

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if got := w.RendezvousWait; got.Reason != RendezvousWaitSelf || got.AheadBy != 0 {
		t.Fatalf("Wait = %+v, want Self winning over the gap while the own driver defers", got)
	}

	// Driver released, gap still open → the gap is now the reason.
	w.DisengageAutoWarp()
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if got := w.RendezvousWait; got.Reason != RendezvousWaitSubspaceGap || got.AheadBy != gap {
		t.Errorf("Wait = %+v after release across the gap, want gap with AheadBy=+%v", got, gap)
	}
}

// #260 mirror bound: without the partner armed back there is no deferral —
// the driver's don't-clobber branch never runs — so an engaged own driver
// must NOT flip the classification away from Partner: releasing it would
// not start the coast, the partner genuinely hasn't joined.
func TestRendezvousWaitPartnerStillBlamedWhenNotArmedBack(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.ArmedTowardViewer = false // partner hasn't Engaged back
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Handle: "gern", Tau: st.Add(72 * time.Hour)}
	if !w.EngageSyncWarp(st.Add(time.Hour), "SHA256:vex", "vex") {
		t.Fatal("Sync refused")
	}

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if got := w.RendezvousWait; got.Reason != RendezvousWaitPartner {
		t.Errorf("Wait = %+v with the partner not armed back, want Partner", got)
	}
}

// The responder side of #250: an out-of-window invite is kept and
// exposed as Blocked (with the signed Δt) rather than silently dropped,
// and becomes joinable again when the pair converges. A past-τ arm is
// still dropped entirely — Engage would refuse it (forward-only).
func TestRendezvousInviteBlockedOnSubspaceGap(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	gap := gapDuration()
	peer := armPeer(w, primary, st.Add(gap), 50, "gern")
	peer.RendezvousTau = tau
	peer.RendezvousCA = 1234

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	inv := w.RendezvousInvite
	if inv == nil {
		t.Fatal("out-of-window invite vanished — #250 wants it kept, blocked")
	}
	if !inv.Blocked {
		t.Fatal("out-of-window invite not marked Blocked")
	}
	if inv.AheadBy != -gap {
		t.Errorf("AheadBy = %v, want -%v (initiator ahead of the viewer)", inv.AheadBy, gap)
	}
	if inv.Owner != peer.Owner || inv.Handle != "gern" || !inv.Tau.Equal(tau) || inv.CA != 1234 {
		t.Errorf("blocked invite = %+v, want gern's committed τ+CA carried through", inv)
	}

	// Converged → the same arm surfaces joinable.
	peer.SubspaceTime = st
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if inv := w.RendezvousInvite; inv == nil || inv.Blocked {
		t.Errorf("invite = %+v after converging, want joinable (Blocked=false)", inv)
	}

	// Past τ + gap → dropped entirely, as before (nothing to join, ever).
	peer.SubspaceTime = st.Add(gap)
	peer.RendezvousTau = st.Add(-time.Minute)
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.RendezvousInvite != nil {
		t.Errorf("invite = %+v for a past τ, want dropped", w.RendezvousInvite)
	}
}

// A joinable invite always wins over a blocked one when two peers are
// armed toward the viewer (pairwise MVP surfaces one prompt).
func TestRendezvousInviteJoinableWinsOverBlocked(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	blocked := armPeer(w, primary, st.Add(gapDuration()), 50, "far")
	blocked.RendezvousTau = tau
	joinable := armPeer(w, primary, st, 50, "gern")
	joinable.RendezvousTau = tau

	w.DriveRendezvousWarp([]CoWarpPeer{blocked, joinable})
	inv := w.RendezvousInvite
	if inv == nil || inv.Blocked || inv.Handle != "gern" {
		t.Errorf("invite = %+v, want the joinable peer gern over the blocked one", inv)
	}
}
