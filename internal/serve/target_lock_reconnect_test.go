package serve

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// #294: a guest's craft/rendezvous target lock on another player's
// vessel used to be silently dropped across a reconnect (the [u]
// restart-to-adopt flow, most visibly) while a body target survived
// the same round-trip untouched. Root cause: CraftToWire normalised
// any TargetGhost to no-target on save, on the (here, wrong) theory
// that the owner fingerprint was session-local and could never resolve
// again. Fixed in two parts:
//
//  1. The wire form now preserves the ghost ref (save/craft_wire.go),
//     so the intent survives the save/load round trip.
//  2. reportingModel.reconcileTargetLock (reporting.go) watches an
//     unresolved ghost target every tick and either re-latches it
//     silently once the owner's craft reports resume, or gives up
//     after targetLockRelatchGrace and fires a legible
//     SessionEventTargetLockLost chip.
//
// These tests exercise reconcileTargetLock directly, with synthetic
// timestamps, so the grace window doesn't have to be slept through.

const testPeerFP = "SHA256:peer-target-lock"

// A ghost target that resolves before anyone even notices it was
// unresolved (the ordinary live-play case, and the "reconnect landed
// after the peer" case) never starts a pending timer and never chips.
func TestTargetLockResolvesImmediatelyNoOp(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.SetTargetGhost(testPeerFP, 99)
	w.Ghosts = []sim.Ghost{{Owner: testPeerFP, CraftID: 99, PrimaryID: c.Primary.ID}}

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{testPeerFP: "peer"}
	m.reconcileTargetLock(w, handles, time.Now())

	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Errorf("target = %+v, want the ghost lock untouched", w.Target)
	}
	if !m.targetLockPendingSince.IsZero() {
		t.Errorf("pending timer started for a target that already resolved")
	}
	if len(m.localEvents) != 0 {
		t.Errorf("unexpected chip(s) for a target that already resolved: %+v", m.localEvents)
	}
}

// The deferred re-latch: a ghost target that lands unresolved (the
// reconnect case — reports haven't arrived yet) starts a pending timer
// instead of dropping immediately, and once the owner's report resumes
// the lock is restored silently — no chip, because the player ends up
// exactly where they started.
func TestTargetLockRelatchesSilentlyWhenReportResumes(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.SetTargetGhost(testPeerFP, 99)

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{testPeerFP: "peer"}
	now := time.Now()

	// Reconnect lands before the peer's report — nothing to resolve
	// against yet.
	w.Ghosts = nil
	m.reconcileTargetLock(w, handles, now)
	if w.Target.Kind != sim.TargetGhost {
		t.Fatalf("target cleared on the very first unresolved tick: %+v", w.Target)
	}
	if m.targetLockPendingSince.IsZero() {
		t.Fatalf("pending timer did not start for an unresolved ghost target")
	}

	// The peer's report resumes a few seconds later — well inside the
	// grace window — and the ghost slate now carries their craft.
	w.Ghosts = []sim.Ghost{{Owner: testPeerFP, CraftID: 99, PrimaryID: c.Primary.ID}}
	m.reconcileTargetLock(w, handles, now.Add(5*time.Second))

	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Errorf("target lock not restored once the report resumed: %+v", w.Target)
	}
	if !m.targetLockPendingSince.IsZero() {
		t.Errorf("pending timer not cleared after a successful re-latch")
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Errorf("loss chip fired despite the target resolving: %+v", e)
		}
	}
}

// Legibility: a ghost target that never resolves within
// targetLockRelatchGrace is dropped for good and the player is told —
// not left with a silently dangling aim.
func TestTargetLockLostChipAfterGraceWindow(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetGhost(testPeerFP, 99)

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{testPeerFP: "peer"}
	now := time.Now()

	m.reconcileTargetLock(w, handles, now) // starts the pending timer

	// Still inside the grace window: keep waiting, no chip yet.
	m.reconcileTargetLock(w, handles, now.Add(targetLockRelatchGrace/2))
	if w.Target.Kind != sim.TargetGhost {
		t.Fatalf("target cleared before the grace window elapsed: %+v", w.Target)
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Fatalf("loss chip fired before the grace window elapsed")
		}
	}

	// Past grace, still unresolved: give up and say so.
	m.reconcileTargetLock(w, handles, now.Add(targetLockRelatchGrace+time.Second))

	if w.Target.Kind != sim.TargetNone {
		t.Errorf("target not cleared after the grace window elapsed: %+v", w.Target)
	}
	var lost *sim.SessionEvent
	for i, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			lost = &m.localEvents[i]
		}
	}
	if lost == nil {
		t.Fatalf("no target-lock-lost chip fired; events=%+v", m.localEvents)
	}
	if lost.Handle != "peer" {
		t.Errorf("loss chip Handle = %q, want %q", lost.Handle, "peer")
	}
	if !m.targetLockPendingSince.IsZero() {
		t.Errorf("pending timer not cleared after giving up")
	}
}

// Full round trip (#294's actual repro): a guest's world is saved with a
// ghost target, "restarted" (a fresh App loaded from the persisted
// payload — the [u] adopt path's shape, minus the network), and the
// reporting wrapper re-latches the lock once the target owner's craft
// report reaches the relay store. The body-target sibling case already
// worked before this fix (payload-level Target always round-tripped);
// this proves the craft/ghost case now matches it end to end, silently.
func TestTargetLockSurvivesSaveLoadAndReLatchesThroughReportingModel(t *testing.T) {
	srv := newOfflineServer(t)
	const fpA, fpB = "SHA256:alice", "SHA256:bob"
	enrollDirect(t, srv, fpA, "alice")
	enrollDirect(t, srv, fpB, "bob")

	// Alice is mid-session, locked onto Bob's vessel (craft ID 99).
	appA, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	wA := appA.World()
	wA.SetTargetGhost(fpB, 99)
	if err := srv.store.SavePlayer(fpA, wA); err != nil {
		t.Fatalf("SavePlayer: %v", err)
	}

	// "Restart": rebuild Alice's App fresh from what was just persisted —
	// the exact shape newGuestApp takes on a real reconnect.
	reloaded, err := srv.newGuestApp(fpA)
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	w2 := reloaded.World()
	if w2.Target.Kind != sim.TargetGhost || w2.Target.GhostOwner != fpB || w2.Target.CraftID != 99 {
		t.Fatalf("loaded target = %+v, want the ghost lock to have survived the save/load round trip", w2.Target)
	}

	// Bob's report hasn't reached the relay store yet — Alice's session
	// comes up before Bob's does.
	m := srv.withReporting(reloaded, fpA)
	m = tick(m)
	if w2.Target.Kind != sim.TargetGhost {
		t.Fatalf("target dropped on the very first tick, before the grace window: %+v", w2.Target)
	}

	// Bob's report now arrives.
	primary := w2.ActiveCraft().Primary.ID
	srv.relay.Report(relay.CraftReport{
		Owner:        fpB,
		SubspaceTime: w2.Clock.SimTime,
		Crafts: []relay.CraftState{{
			ID:      99,
			Name:    "Bobcraft",
			System:  w2.System().Name,
			Primary: primary,
		}},
	})

	m = tick(m)
	if w2.Target.Kind != sim.TargetGhost || w2.Target.GhostOwner != fpB || w2.Target.CraftID != 99 {
		t.Errorf("target lock not re-latched once Bob's report arrived: %+v", w2.Target)
	}
	for _, e := range w2.SessionEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Errorf("unexpected target-lock-lost chip: %+v", e)
		}
	}
	_ = m
}
