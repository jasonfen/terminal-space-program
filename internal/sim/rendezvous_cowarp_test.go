package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// armPeer builds a peer that has itself Engaged a Rendezvous Warp toward
// the viewer (ArmedTowardViewer), offset far outside the proximity gate
// so only the mutual arm — never range — could couple it.
func armPeer(w *World, primary string, st time.Time, ew float64, handle string) CoWarpPeer {
	p := peerAt(w, primary, st, ew, orbital.Vec3{X: 50000}, orbital.Vec3{}, handle) // 50 km out
	p.ArmedTowardViewer = true
	return p
}

// Mutual arm couples the pair BEFORE the proximity gate: both Engaged
// toward each other, same subspace, but 50 km apart — proximity alone
// would never couple, the Rendezvous trigger does.
func TestRendezvousArmCouplesBeforeGate(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := armPeer(w, primary, st, 50, "gern")
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Tau: st.Add(72 * time.Hour)}

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if !res.State.Coupled {
		t.Fatal("mutual arm did not couple at 50 km (pre-proximity trigger missing)")
	}
	if res.State.MinWarp != 50 {
		t.Errorf("MinWarp = %v, want the partner's 50", res.State.MinWarp)
	}
	if len(res.NewlyCoupled) != 1 || res.NewlyCoupled[0] != "gern" {
		t.Errorf("NewlyCoupled = %v, want [gern]", res.NewlyCoupled)
	}
}

// One-sided arm never couples: the viewer Engaged, but the partner has
// not, so the first-to-Engage does not warp/couple (no solo drift).
func TestRendezvousSingleArmDoesNotCouple(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.ArmedTowardViewer = false // partner hasn't Engaged back
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Tau: st.Add(72 * time.Hour)}

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if res.State.Coupled {
		t.Error("coupled on a single-sided arm (partner not Engaged)")
	}
}

// The viewer's arm must target THIS peer: armed toward someone else does
// not couple to a peer who happens to be armed toward the viewer.
func TestRendezvousArmWrongTargetDoesNotCouple(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := armPeer(w, primary, st, 50, "gern")
	w.RendezvousArm = &RendezvousArm{TargetOwner: "SHA256:someone-else", Tau: st.Add(72 * time.Hour)}

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if res.State.Coupled {
		t.Error("coupled despite the viewer's arm targeting a different player")
	}
}

// The arm does not bypass the same-subspace gate: a mutually-armed pair
// still needs Δt within tolerance (they Sync first, then Rendezvous Warp).
func TestRendezvousArmDifferentSubspaceDoesNotCouple(t *testing.T) {
	w, primary, st := anchorWorld(t)
	far := st.Add(10 * 24 * time.Hour)
	peer := armPeer(w, primary, far, 50, "gern")
	w.RendezvousArm = &RendezvousArm{TargetOwner: peer.Owner, Tau: st.Add(72 * time.Hour)}

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if res.State.Coupled {
		t.Error("mutual arm coupled across a 10-day subspace gap (Sync-first invariant broken)")
	}
}

// Per-peer SCOPE of the #248 min-wins exemption: while the shared coast
// is engaged, only the armed PARTNER's stale reported rate is excluded
// from the min — a proximity-coupled third peer must still clamp.
//
// This pins the exemption against the tempting refactor to a blanket
// skip (`if rendezvousWarpEngaged() { skip min accumulation }` before the
// peer loop): every pairwise test still passes under that form, but a
// third player station-keeping next to the viewer would lose min-wins
// entirely — the viewer's max-seeded coast would out-warp them and break
// the couple's step-in-lock invariant. The exemption is a property of
// the peer (clampExempt inside the rendezvous branch), not of the tick.
func TestRendezvousCoastThirdPeerMinStillWins(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)

	// Partner B: mutually armed, shared coast engaged. Reports 1× — the
	// stale post-clamp rate the #248 exemption exists to ignore. If it
	// leaked into the min, MinWarp would come out 1, not 5.
	partner := armPeer(w, primary, st, 1, "gern")
	w.EngageRendezvousWarp(partner.Owner, "gern", tau, 0)
	w.DriveRendezvousWarp([]CoWarpPeer{partner})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: the shared coast did not engage")
	}

	// Peer C: proximity-coupled the ordinary way (5 km, 0 m/s, same
	// subspace, same primary), reporting Effective warp 5.
	third := peerAt(w, primary, st, 5, orbital.Vec3{X: 5000}, orbital.Vec3{}, "cass")

	res := w.ComputeCoWarp([]CoWarpPeer{partner, third}, nil)
	if !res.State.Coupled {
		t.Fatal("not coupled to the armed partner + proximity peer")
	}
	if len(res.State.Partners) != 2 {
		t.Fatalf("Partners = %v, want both gern and cass coupled", res.State.Partners)
	}
	if res.State.MinWarp != 5 {
		t.Errorf("MinWarp = %v, want C's 5 (B's stale 1x excluded per-peer, C still clamps)",
			res.State.MinWarp)
	}
	w.CoWarp = res.State
	if got := w.EffectiveWarp(); got != 5 {
		t.Errorf("EffectiveWarp = %v, want 5 — the third peer's min must cap the "+
			"max-seeded coast through clampedWarp", got)
	}
}

// Seamless Rendezvous→Proximity handoff at arrival: while mutually armed
// AND within the gate the pair is coupled; when the arm clears (AutoWarp
// reached τ) the SAME coupled state continues via the proximity decouple
// band — no Released transition, no uncoupled tick.
func TestRendezvousToProximityHandoff(t *testing.T) {
	w, primary, st := anchorWorld(t)
	// Arrived: within the proximity gate now (5 km, 0 m/s).
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{}, "gern")
	near.ArmedTowardViewer = true
	w.RendezvousArm = &RendezvousArm{TargetOwner: near.Owner, Tau: st.Add(72 * time.Hour)}

	res := w.ComputeCoWarp([]CoWarpPeer{near}, nil)
	if !res.State.Coupled {
		t.Fatal("not coupled at arrival inside the gate")
	}

	// Arm clears (τ reached, warp released) — coupling must persist on the
	// proximity branch, carrying the hysteresis memory forward.
	w.RendezvousArm = nil
	near.ArmedTowardViewer = false
	res2 := w.ComputeCoWarp([]CoWarpPeer{near}, res.CoupledOwners)
	if !res2.State.Coupled {
		t.Error("dropped coupling when the arm cleared (handoff not seamless)")
	}
	if len(res2.Released) != 0 {
		t.Errorf("spurious release on the Rendezvous→Proximity handoff: %v", res2.Released)
	}
}
