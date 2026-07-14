package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// anchorWorld returns a fresh World whose active craft is the co-warp
// anchor, plus that craft's primary ID and its subspace time.
func anchorWorld(t *testing.T) (*World, string, time.Time) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft to anchor")
	}
	return w, c.Primary.ID, w.Clock.SimTime
}

// peerAt builds a one-craft peer offset from the anchor by (dR, dV) in
// the anchor's primary frame, at subspace time st with Effective warp ew.
func peerAt(w *World, primary string, st time.Time, ew float64, dR, dV orbital.Vec3, handle string) CoWarpPeer {
	a := w.ActiveCraft()
	return CoWarpPeer{
		Owner:        "SHA256:" + handle,
		Handle:       handle,
		SubspaceTime: st,
		EffWarp:      ew,
		Crafts: []CoWarpCraft{{
			Primary: primary,
			R:       a.State.R.Add(dR),
			V:       a.State.V.Add(dV),
		}},
	}
}

// Inside 10 km and ≤100 m/s with the same primary + subspace couples,
// emits a couple transition, and reports the partner's Effective warp as
// the min.
func TestComputeCoWarpCouples(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := peerAt(w, primary, st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{}, "bob")

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if !res.State.Coupled {
		t.Fatal("not coupled inside the gate")
	}
	if res.State.MinWarp != 50 {
		t.Errorf("MinWarp = %v, want partner's 50", res.State.MinWarp)
	}
	if len(res.NewlyCoupled) != 1 || res.NewlyCoupled[0] != "bob" {
		t.Errorf("NewlyCoupled = %v, want [bob]", res.NewlyCoupled)
	}
	if !res.CoupledOwners["SHA256:bob"] {
		t.Error("owner not recorded coupled for next tick's hysteresis")
	}
}

// A fast pass through the radius is a crossing, not a rendezvous: within
// 10 km but |v_rel| > 100 m/s does not couple.
func TestComputeCoWarpFlybyDoesNotCouple(t *testing.T) {
	w, primary, st := anchorWorld(t)
	peer := peerAt(w, primary, st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{X: 150}, "bob")

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if res.State.Coupled {
		t.Error("coupled to a 150 m/s flyby")
	}
}

// A peer whose subspace time is beyond the tolerance (a time-shifted
// ghost) does not couple even if its propagated orbit sits in range.
func TestComputeCoWarpDifferentSubspaceDoesNotCouple(t *testing.T) {
	w, primary, st := anchorWorld(t)
	far := st.Add(10 * 24 * time.Hour)
	peer := peerAt(w, primary, far, 50, orbital.Vec3{X: 5000}, orbital.Vec3{}, "bob")

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if res.State.Coupled {
		t.Error("coupled across a 10-day subspace gap")
	}
}

// A neighbour bound to a different SOI primary is not a co-warp candidate.
func TestComputeCoWarpDifferentPrimaryDoesNotCouple(t *testing.T) {
	w, _, st := anchorWorld(t)
	peer := peerAt(w, "Luna", st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{}, "bob")

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, nil)
	if res.State.Coupled {
		t.Error("coupled to a craft in a different primary's SOI")
	}
}

// Hysteresis: in the 10–12 km band, an uncoupled pair stays uncoupled and
// a coupled pair stays coupled — across repeated ticks, with no flap and
// no repeated chips.
func TestComputeCoWarpHysteresisNoFlap(t *testing.T) {
	w, primary, st := anchorWorld(t)
	// 11 km: inside the decouple band, outside the couple band.
	band := peerAt(w, primary, st, 50, orbital.Vec3{X: 11000}, orbital.Vec3{}, "bob")

	// Uncoupled entering the band: never couples, never chips.
	prev := map[string]bool(nil)
	for i := 0; i < 10; i++ {
		res := w.ComputeCoWarp([]CoWarpPeer{band}, prev)
		if res.State.Coupled {
			t.Fatalf("tick %d: coupled at 11 km from uncoupled", i)
		}
		if len(res.NewlyCoupled) != 0 || len(res.Released) != 0 {
			t.Fatalf("tick %d: spurious transition %+v", i, res)
		}
		prev = res.CoupledOwners
	}

	// Coupled entering the band: stays coupled, never re-chips.
	prev = map[string]bool{"SHA256:bob": true}
	for i := 0; i < 10; i++ {
		res := w.ComputeCoWarp([]CoWarpPeer{band}, prev)
		if !res.State.Coupled {
			t.Fatalf("tick %d: released at 11 km from coupled (flap)", i)
		}
		if len(res.NewlyCoupled) != 0 || len(res.Released) != 0 {
			t.Fatalf("tick %d: spurious transition %+v", i, res)
		}
		prev = res.CoupledOwners
	}
}

// Past the decouple band (>12 km) a coupled pair releases and chips once.
func TestComputeCoWarpDecouples(t *testing.T) {
	w, primary, st := anchorWorld(t)
	gone := peerAt(w, primary, st, 50, orbital.Vec3{X: 13000}, orbital.Vec3{}, "bob")

	res := w.ComputeCoWarp([]CoWarpPeer{gone}, map[string]bool{"SHA256:bob": true})
	if res.State.Coupled {
		t.Error("still coupled past 13 km")
	}
	if len(res.Released) != 1 || res.Released[0] != "bob" {
		t.Errorf("Released = %v, want [bob]", res.Released)
	}
}

// Min-wins: the coupled clamp takes the minimum Effective warp across
// coupled peers, and it flows through clampedWarp to cap the anchor.
func TestComputeCoWarpMinWinsClampsWorld(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.Clock.WarpIdx = 3 // selected 1000×
	if got := w.EffectiveWarp(); got != 1000 {
		t.Fatalf("baseline EffectiveWarp = %v, want 1000", got)
	}
	slow := peerAt(w, primary, st, 10, orbital.Vec3{X: 4000}, orbital.Vec3{}, "bob")  // burn-capped partner
	fast := peerAt(w, primary, st, 100, orbital.Vec3{X: 6000}, orbital.Vec3{}, "gwen") // coasting partner

	res := w.ComputeCoWarp([]CoWarpPeer{slow, fast}, nil)
	if res.State.MinWarp != 10 {
		t.Errorf("MinWarp = %v, want 10 (the min over coupled)", res.State.MinWarp)
	}
	w.CoWarp = res.State
	if got := w.EffectiveWarp(); got != 10 {
		t.Errorf("coupled EffectiveWarp = %v, want the partner's 10× cap", got)
	}
}

// A landed/absent anchor couples to nobody, releasing any prior couple.
func TestComputeCoWarpLandedAnchorReleases(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.ActiveCraft().Landed = true
	peer := peerAt(w, primary, st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{}, "bob")

	res := w.ComputeCoWarp([]CoWarpPeer{peer}, map[string]bool{"SHA256:bob": true})
	if res.State.Coupled {
		t.Error("landed anchor coupled")
	}
	if res.CoupledOwners["SHA256:bob"] {
		t.Error("landed anchor kept the couple")
	}
}

// Split guard: Sync to another subspace is refused while co-warped, and
// allowed once separated.
func TestEngageSyncWarpRefusedWhileCoupled(t *testing.T) {
	w, _, _ := anchorWorld(t)
	target := w.Clock.SimTime.Add(time.Hour)

	w.CoWarp = CoWarpState{Coupled: true, MinWarp: 10}
	if w.EngageSyncWarp(target, "SHA256:bob", "bob") {
		t.Error("Sync engaged while co-warped (subspace split not blocked)")
	}
	w.CoWarp = CoWarpState{}
	if !w.EngageSyncWarp(target, "SHA256:bob", "bob") {
		t.Error("Sync refused after separation")
	}
}
