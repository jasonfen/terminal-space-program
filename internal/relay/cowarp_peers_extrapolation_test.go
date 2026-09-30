package relay

import (
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
		peers := CoWarpPeersFrom(wA, reps, handles, ownerA, nil, nil, now)
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
