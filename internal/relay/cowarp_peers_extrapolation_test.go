package relay

import (
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"testing"
	"time"
)

// #417: CoWarpPeersFrom hands consumers a peer clock that is current at
// read, not one heartbeat stale.
func TestCoWarpPeersFromExtrapolatesPeerClock(t *testing.T) {
	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob"}
	sent := time.Date(2031, 3, 1, 12, 0, 0, 0, time.UTC)

	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime
	wB.Clock.WarpIdx = 2 // 100x
	store := NewStore()
	NewReporter(store, ownerB).Tick(wB, sent)
	rep := store.Snapshot(ownerA)[0]
	if rep.EffWarp != 100 || rep.ReportedAt.IsZero() {
		t.Fatalf("report EffWarp=%v ReportedAt=%v, want 100 and stamped", rep.EffWarp, rep.ReportedAt)
	}

	at := func(reps []CraftReport, now time.Time) time.Time {
		peers := CoWarpPeersFrom(wA, reps, handles, ownerA, map[string]bool{ownerB: true}, nil, now)
		if len(peers) != 1 {
			t.Fatalf("peers = %d, want 1", len(peers))
		}
		return peers[0].SubspaceTime
	}

	// 4 s of wall since the report at 100x: 400 sim-seconds on.
	if got, want := at([]CraftReport{rep}, sent.Add(4*time.Second)), rep.SubspaceTime.Add(400*time.Second); !got.Equal(want) {
		t.Errorf("4 s after the report: peer clock %v, want %v (+400 s)", got, want)
	}

	// A paused peer's clock is stopped: verbatim.
	paused := rep
	paused.Paused = true
	if got := at([]CraftReport{paused}, sent.Add(4*time.Second)); !got.Equal(rep.SubspaceTime) {
		t.Errorf("paused peer: clock %v, want verbatim %v", got, rep.SubspaceTime)
	}

	// An unstamped report (pre-#417 wire, hand-built) reads verbatim.
	unstamped := rep
	unstamped.ReportedAt = time.Time{}
	if got := at([]CraftReport{unstamped}, sent.Add(4*time.Second)); !got.Equal(rep.SubspaceTime) {
		t.Errorf("unstamped report: clock %v, want verbatim %v", got, rep.SubspaceTime)
	}

	// A long-silent reporter is capped, never extrapolated for an hour.
	want := rep.SubspaceTime.Add(time.Duration(100*10) * time.Second)
	if got := at([]CraftReport{rep}, sent.Add(time.Hour)); !got.Equal(want) {
		t.Errorf("silent peer: clock %v, want capped at %v", got, want)
	}
}

// Stamped reports carry craft state at rep.SubspaceTime, so the Kepler
// step to the viewer's time must be measured from THAT instant, not from
// the extrapolated clock. Both sides coast one heartbeat; the peer craft
// must land on its true propagated state (metres, not tens of km).
func TestCoWarpPeersFromPlacesCraftAtTrueCoastedState(t *testing.T) {
	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob"}
	sent := time.Date(2031, 3, 1, 12, 0, 0, 0, time.UTC)
	for _, warp := range []float64{1, 10, 100} {
		wA, wB := newWorld(t), newWorld(t)
		wB.Clock.SimTime = wA.Clock.SimTime
		store := NewStore()
		NewReporter(store, ownerB).Tick(wB, sent)
		rep := store.Snapshot(ownerA)[0]
		rep.EffWarp = warp
		cs := rep.Crafts[0]
		primary, ok := bodyByID(wA.System(), cs.Primary)
		if !ok {
			t.Fatalf("primary %q not found", cs.Primary)
		}
		coast := time.Duration(Heartbeat.Seconds() * warp * float64(time.Second))
		wA.Clock.SimTime = rep.SubspaceTime.Add(coast) // viewer coasted the same span
		truth, ok := physics.KeplerStep(physics.StateVector{R: cs.R, V: cs.V, M: 1},
			primary.GravitationalParameter(), coast.Seconds())
		if !ok {
			t.Fatal("truth KeplerStep failed")
		}
		peers := CoWarpPeersFrom(wA, []CraftReport{rep}, handles, ownerA,
			map[string]bool{ownerB: true}, nil, sent.Add(Heartbeat))
		if len(peers) != 1 {
			t.Fatalf("peers = %d, want 1", len(peers))
		}
		got := peers[0].Crafts[0].R
		d := got.Sub(truth.R).Norm()
		t.Logf("warp %vx: peer craft error %.6f m", warp, d)
		if d > 1 {
			t.Errorf("warp %vx: peer craft %.1f m from its true coasted state, want < 1 m", warp, d)
		}
	}
}

// A dead or offline owner's report is frozen: extrapolating it by its last
// rate forever invents time that never ran. Verbatim for non-live owners.
func TestCoWarpPeersFromDoesNotExtrapolateDeadOwner(t *testing.T) {
	const ownerA, ownerB = "SHA256:alice", "SHA256:bob"
	handles := map[string]string{ownerB: "bob"}
	sent := time.Date(2031, 3, 1, 12, 0, 0, 0, time.UTC)
	wA, wB := newWorld(t), newWorld(t)
	wB.Clock.SimTime = wA.Clock.SimTime
	store := NewStore()
	NewReporter(store, ownerB).Tick(wB, sent)
	rep := store.Snapshot(ownerA)[0]
	rep.EffWarp = 100
	now := sent.Add(time.Hour)
	for name, live := range map[string]map[string]bool{"nil live": nil, "offline": {ownerB: false}} {
		peers := CoWarpPeersFrom(wA, []CraftReport{rep}, handles, ownerA, live, nil, now)
		if len(peers) != 1 {
			t.Fatalf("%s: peers = %d", name, len(peers))
		}
		if got := peers[0].SubspaceTime; !got.Equal(rep.SubspaceTime) {
			t.Errorf("%s: clock %v, want verbatim %v", name, got, rep.SubspaceTime)
		}
	}
}
