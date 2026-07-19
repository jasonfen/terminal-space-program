package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// End-to-end across the reporting seam (two Worlds, one store): a burning
// partner's 10× Effective-warp cap travels on the wire and, via
// CoWarpPeersFrom → ComputeCoWarp, clamps the coasting viewer to 10×.
// The suite's -race run is the concurrency assertion on the store.
func TestCoWarpMinWinsThroughSeam(t *testing.T) {
	store := NewStore()
	wA, wB := newWorld(t), newWorld(t) // identical LEO craft → range 0, v_rel 0
	// Same subspace: align B's clock to A's.
	wB.Clock.SimTime = wA.Clock.SimTime

	// A wants to warp fast; B is burning, so B's Effective warp caps at 10×.
	wA.Clock.WarpIdx = 3 // 1000×
	wB.Clock.WarpIdx = 3 // 1000× selected, but...
	wB.ActiveCraft().ManualBurn = &spacecraft.ManualBurn{StartTime: wB.Clock.SimTime}
	if got := wB.EffectiveWarp(); got != 10 {
		t.Fatalf("burning B EffectiveWarp = %v, want 10× cap", got)
	}

	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	NewReporter(store, ownerA).Tick(wA, time.Now())
	NewReporter(store, ownerB).Tick(wB, time.Now())

	// The wire carried B's Effective warp.
	var seenB CraftReport
	for _, r := range store.Snapshot(ownerA) {
		if r.Owner == ownerB {
			seenB = r
		}
	}
	if seenB.EffWarp != 10 {
		t.Fatalf("B report EffWarp = %v, want 10 on the wire", seenB.EffWarp)
	}

	// A evaluates co-warp against the store and clamps to B's cap.
	handles := map[string]string{ownerB: "bob"}
	peers := CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA)
	res := wA.ComputeCoWarp(peers, nil)
	if !res.State.Coupled || res.State.MinWarp != 10 {
		t.Fatalf("co-warp state = %+v, want coupled at MinWarp 10", res.State)
	}
	wA.CoWarp = res.State
	if got := wA.EffectiveWarp(); got != 10 {
		t.Errorf("coasting A EffectiveWarp = %v, want partner's 10× cap through the seam", got)
	}
}

// Couple then decouple through the seam: when the partner drifts past the
// hysteresis band, the next evaluation releases the couple.
func TestCoWarpCoupleThenDecoupleThroughSeam(t *testing.T) {
	store := NewStore()
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime
	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob"}

	// Coincident craft → couple.
	NewReporter(store, ownerA).Tick(wA, time.Now())
	repB := NewReporter(store, ownerB)
	repB.Tick(wB, time.Now())
	res := wA.ComputeCoWarp(CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA), nil)
	if !res.State.Coupled || len(res.NewlyCoupled) != 1 {
		t.Fatalf("first eval = %+v, want a fresh couple", res)
	}

	// B jumps 50 km down-range and re-reports (a burn changes elements →
	// immediate report). A re-evaluates and releases.
	wB.ActiveCraft().State.R.X += 50_000
	wB.ActiveCraft().State.V.X += 50 // move the elements so the report fires
	repB.Tick(wB, time.Now().Add(time.Second))
	res = wA.ComputeCoWarp(CoWarpPeersFrom(wA, store.Snapshot(ownerA), handles, ownerA), res.CoupledOwners)
	if res.State.Coupled {
		t.Error("still coupled after B drifted 50 km")
	}
	if len(res.Released) != 1 || res.Released[0] != "bob" {
		t.Errorf("Released = %v, want [bob]", res.Released)
	}
}
