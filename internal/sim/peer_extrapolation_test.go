package sim

import (
	"testing"
	"time"
)

// #417: a peer's report clock is made current by its own rate over the
// wall time since it was sent.
func TestExtrapolateSubspaceTime(t *testing.T) {
	base := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		warp    float64
		paused  bool
		elapsed time.Duration
		want    time.Duration // added to base
	}{
		{"coasting 100x for 4 s wall", 100, false, 4 * time.Second, 400 * time.Second},
		{"coasting 1000x for 5 s wall", 1000, false, 5 * time.Second, 5000 * time.Second},
		{"paused peer does not advance", 100, true, 4 * time.Second, 0},
		{"zero rate does not advance", 0, false, 4 * time.Second, 0},
		{"negative rate does not advance", -5, false, 4 * time.Second, 0},
		{"clock skew (negative elapsed) does not rewind", 100, false, -3 * time.Second, 0},
		{"silent peer is capped at two heartbeats", 100, false, time.Hour, time.Duration(100 * PeerExtrapolationMaxWall.Seconds() * float64(time.Second))},
	}
	for _, tc := range cases {
		got := ExtrapolateSubspaceTime(base, tc.warp, tc.paused, tc.elapsed)
		if d := got.Sub(base); d != tc.want {
			t.Errorf("%s: advanced %v, want %v", tc.name, d, tc.want)
		}
	}
}

// ComputeCoWarp's per-peer clamp exemption for the rendezvous coast (#248)
// reads rendezvousWarpEngaged() at the moment it runs, so it is only
// correct when DriveRendezvousWarp has ALREADY run this tick (the comment
// at the exemption says so; internal/serve/reporting.go is where the
// order lives). On the engaging tick the two orders differ: Drive-first
// exempts the partner's stale post-clamp report (MinWarp 0, the coast
// resolves its own rate); Compute-first lets that report into the min and
// ratchets the pair to it (the #248 bug). This pins both halves, so the
// dependency is written down as a fact rather than held in a comment.
func TestComputeCoWarpExemptionNeedsDriveFirst(t *testing.T) {
	newPair := func() (*World, CoWarpPeer) {
		w, primary, st := anchorWorld(t)
		partner := armPeer(w, primary, st, 1, "gern") // stale 1x post-clamp report
		if !w.EngageRendezvousWarp(partner.Owner, "gern", st.Add(72*time.Hour), 0) {
			t.Fatal("precondition: EngageRendezvousWarp failed")
		}
		return w, partner
	}

	// Drive first (production order): exempt.
	w, partner := newPair()
	w.DriveRendezvousWarp([]CoWarpPeer{partner})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: the coast did not engage on the first Drive")
	}
	res := w.ComputeCoWarp([]CoWarpPeer{partner}, nil)
	if !res.State.Coupled {
		t.Fatal("mutually armed pair not coupled")
	}
	if res.State.MinWarp != 0 {
		t.Errorf("Drive-then-Compute: MinWarp = %v, want 0 (partner's stale 1x exempt from the min)", res.State.MinWarp)
	}

	// Compute first (the wrong order): the coast is not yet engaged, so
	// the same report feeds the min.
	w2, partner2 := newPair()
	res2 := w2.ComputeCoWarp([]CoWarpPeer{partner2}, nil)
	if res2.State.MinWarp != 1 {
		t.Errorf("Compute-before-Drive: MinWarp = %v, want 1 (min-wins until the coast engages)", res2.State.MinWarp)
	}
}
