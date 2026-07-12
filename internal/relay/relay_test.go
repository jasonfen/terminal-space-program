package relay

import (
	"sync"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func newWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

// Report → Snapshot round-trip: a second player sees the first's
// craft with addressing (system, primary), state vector, and subspace
// timestamp; the viewer's own reports are excluded.
func TestReportSnapshotRoundTrip(t *testing.T) {
	store := NewStore()
	wA := newWorld(t)
	repA := NewReporter(store, "SHA256:alice")
	repA.Tick(wA, time.Now())

	seen := store.Snapshot("SHA256:bob")
	if len(seen) != 1 {
		t.Fatalf("Snapshot for bob: %d reports, want 1", len(seen))
	}
	r := seen[0]
	if r.Owner != "SHA256:alice" {
		t.Errorf("Owner = %q", r.Owner)
	}
	if !r.SubspaceTime.Equal(wA.Clock.SimTime) {
		t.Errorf("SubspaceTime = %v, want %v", r.SubspaceTime, wA.Clock.SimTime)
	}
	if len(r.Crafts) != len(wA.Crafts) {
		t.Fatalf("crafts = %d, want %d", len(r.Crafts), len(wA.Crafts))
	}
	c := r.Crafts[0]
	if c.System != wA.System().Name || c.Primary == "" {
		t.Errorf("addressing: system=%q primary=%q", c.System, c.Primary)
	}
	if c.R.Norm() == 0 || c.V.Norm() == 0 {
		t.Errorf("state vector empty: R=%v V=%v", c.R, c.V)
	}

	if own := store.Snapshot("SHA256:alice"); len(own) != 0 {
		t.Errorf("alice sees her own report: %d", len(own))
	}
}

// Coasting doesn't re-report (elements are constant; state vectors
// move): only element changes or the heartbeat fire.
func TestChangeDetectionAndHeartbeat(t *testing.T) {
	store := NewStore()
	ch, cancel := store.Subscribe()
	defer cancel()
	w := newWorld(t)
	rep := NewReporter(store, "SHA256:alice")

	now := time.Now()
	rep.Tick(w, now) // first tick always reports
	if got := drain(ch); got != 1 {
		t.Fatalf("first tick: %d reports, want 1", got)
	}

	// Coast: advance the sim a few ticks, wall clock inside heartbeat.
	for i := 0; i < 5; i++ {
		w.Tick()
		rep.Tick(w, now.Add(time.Duration(i+1)*100*time.Millisecond))
	}
	if got := drain(ch); got != 0 {
		t.Errorf("coasting fired %d reports, want 0 (element change detection)", got)
	}

	// Heartbeat elapses → one report even while coasting.
	rep.Tick(w, now.Add(Heartbeat+time.Second))
	if got := drain(ch); got != 1 {
		t.Errorf("heartbeat: %d reports, want 1", got)
	}

	// A burn-sized velocity change moves the elements → immediate report.
	c := w.ActiveCraft()
	c.State.V.X += 100 // 100 m/s prograde-ish kick
	rep.Tick(w, now.Add(Heartbeat+1100*time.Millisecond))
	if got := drain(ch); got != 1 {
		t.Errorf("element change: %d reports, want 1", got)
	}
}

func drain(ch <-chan CraftReport) int {
	n := 0
	for {
		select {
		case <-ch:
			n++
		default:
			return n
		}
	}
}

// Frontier under divergence: the store's frontier is the max subspace
// time across players, and a viewer behind it sees it move.
func TestFrontierUnderDivergence(t *testing.T) {
	store := NewStore()
	if _, ok := store.Frontier(); ok {
		t.Fatal("empty store claims a frontier")
	}
	wA, wB := newWorld(t), newWorld(t)
	// A warps 10 days ahead; B stays put — subspaces diverge.
	wA.Clock.SimTime = wA.Clock.SimTime.Add(10 * 24 * time.Hour)
	NewReporter(store, "SHA256:alice").Tick(wA, time.Now())
	NewReporter(store, "SHA256:bob").Tick(wB, time.Now())

	f, ok := store.Frontier()
	if !ok {
		t.Fatal("no frontier with two reports stored")
	}
	if !f.Equal(wA.Clock.SimTime) {
		t.Errorf("frontier = %v, want alice's %v (max of subspaces)", f, wA.Clock.SimTime)
	}
	if !f.After(wB.Clock.SimTime) {
		t.Error("frontier not ahead of the laggard")
	}
}

// Concurrent report/subscribe/snapshot is race-clean (the -race run
// is the assertion; the counts just keep the compiler honest).
func TestConcurrentReportSubscribe(t *testing.T) {
	store := NewStore()
	w := newWorld(t)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			rep := NewReporter(store, string(rune('a'+n)))
			for j := 0; j < 50; j++ {
				rep.Tick(w, time.Now().Add(time.Duration(j)*Heartbeat)) // force reports
			}
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch, cancel := store.Subscribe()
		defer cancel()
		for j := 0; j < 20; j++ {
			select {
			case <-ch:
			case <-time.After(time.Second):
				return
			}
		}
	}()
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				store.Snapshot("nobody")
				store.Frontier()
			}
		}()
	}
	wg.Wait()
	if got := len(store.Snapshot("nobody")); got != 4 {
		t.Errorf("owners stored = %d, want 4", got)
	}
}
