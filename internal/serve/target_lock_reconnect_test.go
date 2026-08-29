package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
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

// #294 review finding 1: the pre-review watchdog ran its 45s countdown on
// EVERY resolve failure, not just a ref that had never resolved since it
// was bound. relay.GhostsFor only emits ghosts for the viewer's VIEWED
// system and skips Landed craft, so a player browsing another system for
// a minute, or whose peer lands/transfers briefly, would get a perfectly
// healthy lock silently and permanently cleared. Once a ref has resolved
// once, the watchdog must retire for it for good — a later resolve
// failure reverts to the old (pre-#294) tolerance: keep the lock, re-latch
// silently whenever it resolves again, never clear it, never chip a loss.
func TestTargetLockEstablishedLockNeverExpiresAfterResolvingOnce(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.SetTargetGhost(testPeerFP, 99)
	w.Ghosts = []sim.Ghost{{Owner: testPeerFP, CraftID: 99, PrimaryID: c.Primary.ID}}

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{testPeerFP: "peer"}
	now := time.Now()

	// Ordinary play: resolves on the very first tick, same as any live
	// rendezvous — this is what marks the ref resolved-once.
	m.reconcileTargetLock(w, handles, now)
	if !m.targetLockResolvedOnce {
		t.Fatalf("ref never marked resolved-once after a successful resolve")
	}

	// The ghost now drops out of the slate for reasons that have nothing
	// to do with the lock — standing in for "browsing another system" /
	// "peer landed for a minute" with the blunt instrument of an empty
	// w.Ghosts, held for far longer than targetLockRelatchGrace.
	w.Ghosts = nil
	for i := 1; i <= 10; i++ {
		m.reconcileTargetLock(w, handles, now.Add(time.Duration(i)*targetLockRelatchGrace))
	}

	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Fatalf("established lock cleared despite having resolved once: %+v", w.Target)
	}
	if !m.targetLockPendingSince.IsZero() {
		t.Errorf("watchdog timer started for an already-established lock: %v", m.targetLockPendingSince)
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Errorf("loss chip fired for an established lock that merely went briefly unresolved: %+v", e)
		}
	}
}

// #294 review finding 2: the pending timer used to key only on
// Kind==TargetGhost, not WHICH ghost — retargeting to a different peer
// mid-grace inherited the old timer (and, pre-fix, a handle cached at
// timer-start), clearing the new lock early and naming the wrong peer.
// The fix (round 1) keyed the watch on (GhostOwner, CraftID): a retarget
// starts fresh tracking for the new ref rather than inheriting the old
// one's near-expired timer or cached handle.
//
// #294 review finding 4 (round 2) narrowed this further: a retarget is a
// LIVE SetTargetGhost call, and round 2 scopes the give-up countdown to
// refs restored at session start only (targetLockEligible) — so B here
// no longer gets "its own grace window" the way round 1 asserted; it
// gets no countdown at all and must never chip a loss, no matter how
// long it stays unresolved. What both rounds agree on: retargeting must
// never inherit A's state (timer, handle, or eligibility) and must never
// chip immediately on the switch itself.
func TestTargetLockRetargetMidGraceResetsTimerAndHandle(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const fpA, fpB = "SHA256:peer-a", "SHA256:peer-b"
	w.SetTargetGhost(fpA, 1)
	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{fpA: "alice", fpB: "bob"}
	now := time.Now()

	// A is this session's very first ref (restored, in the reconnect
	// sense this fixture stands in for) — eligible, its watchdog starts.
	m.reconcileTargetLock(w, handles, now)
	if m.targetLockPendingSince.IsZero() {
		t.Fatalf("watchdog for A never started")
	}
	if !m.targetLockEligible {
		t.Fatalf("A (this session's first ref) not marked eligible")
	}
	retargetAt := now.Add(targetLockRelatchGrace - time.Second) // deep into A's own grace
	m.reconcileTargetLock(w, handles, retargetAt)

	// Player retargets to B before A either resolved or timed out — an
	// ordinary in-session target switch, not a reconnect.
	w.SetTargetGhost(fpB, 2)
	m.reconcileTargetLock(w, handles, retargetAt)
	if m.targetLockOwner != fpB || m.targetLockCraftID != 2 {
		t.Fatalf("watchdog still tracking the superseded ref: owner=%q craftID=%d", m.targetLockOwner, m.targetLockCraftID)
	}
	if w.Target.Kind != sim.TargetGhost {
		t.Fatalf("retargeting itself cleared the target: %+v", w.Target)
	}
	if m.targetLockEligible {
		t.Fatalf("B (a LIVE retarget, not a restored ref) wrongly marked eligible for the countdown")
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Fatalf("loss chip fired immediately on retarget — inherited A's near-expired timer: %+v", e)
		}
	}

	// B never gets a countdown at all (finding 4 round 2) — it just waits,
	// silently, no matter how far past what would have been A's grace
	// window. No pending timer, no chip, target stays locked on B.
	m.reconcileTargetLock(w, handles, retargetAt.Add(5*targetLockRelatchGrace))
	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != fpB || w.Target.CraftID != 2 {
		t.Errorf("B cleared despite being a live (ineligible) ref: %+v", w.Target)
	}
	if !m.targetLockPendingSince.IsZero() {
		t.Errorf("countdown started for a live retarget: %v", m.targetLockPendingSince)
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Errorf("loss chip fired for a live retarget that should never get a countdown: %+v", e)
		}
	}
}

// #294 review finding 3: stopHosting reset every other reporting field but
// not the target-lock watchdog's — since the reportingModel value is reused
// by the next startHosting, a stale hours-old timer would fire an instant
// false "lost on reconnect" chip on the very next hosting session's first
// tick, for a target that session never even had.
func TestStopHostingResetsTargetLockWatch(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	var m tea.Model = WrapHost(app, nil, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 140, Height: 45})
	m, _ = m.Update(screens.SessionHostMsg{Start: true})
	srv := hostServer(t, m)
	if srv == nil {
		t.Fatal("start failed")
	}

	rm, ok := m.(reportingModel)
	if !ok {
		t.Fatalf("model is %T, not reportingModel", m)
	}
	// Simulate a stale watchdog left running from a target lock that
	// never resolved during this hosting session.
	rm.targetLockOwner = "SHA256:stale-peer"
	rm.targetLockCraftID = 42
	rm.targetLockPendingSince = time.Now().Add(-3 * time.Hour)

	stopped, _ := rm.stopHosting()
	sm, ok := stopped.(reportingModel)
	if !ok {
		t.Fatalf("stopped model is %T", stopped)
	}
	if sm.targetLockOwner != "" || sm.targetLockCraftID != 0 || !sm.targetLockPendingSince.IsZero() || sm.targetLockResolvedOnce {
		t.Errorf("stopHosting left stale watchdog state: owner=%q craftID=%d pendingSince=%v resolvedOnce=%v",
			sm.targetLockOwner, sm.targetLockCraftID, sm.targetLockPendingSince, sm.targetLockResolvedOnce)
	}

	// The very next hosting session's first tick must not fire an
	// instant false loss chip from a stale reset-should-have-cleared timer.
	var m2 tea.Model = sm
	m2, _ = m2.Update(screens.SessionHostMsg{Start: true})
	srv2 := hostServer(t, m2)
	if srv2 == nil {
		t.Fatal("restart failed")
	}
	t.Cleanup(func() { _ = srv2.ln.Close() })
	m2 = tick(m2)
	for _, e := range app.World().SessionEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Errorf("false target-lock-lost chip fired on next session's first tick: %+v", e)
		}
	}
}

// #294 review finding 4 (round 2): ADR 0038 undock handback
// (relay/dock.go's SetTargetGhost calls at the docker's release/undock
// paths) aims the docker at the guest's departing craft before the
// guest's own next CraftReport can possibly have landed in w.Ghosts —
// a brand-new ref with an empty ghost slate, which used to be
// indistinguishable from "restored across a reconnect" and so started
// the very same 45s give-up countdown. A slow or disconnecting guest
// then cost the host a false "lost on reconnect" chip though no
// reconnect ever happened. Fixed two ways: (a) HasRelativeTarget no
// longer requires a resolve (internal/sim/target.go), so NavTarget
// survives the momentary gap; (b) the countdown is scoped to refs
// restored at session start only (targetLockEligible), so a live
// SetTargetGhost — undock handback included — never starts it at all.
func TestUndockHandbackLiveSetTargetGhostNeverStartsCountdown(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	// The docker was already flying NavTarget before undock — e.g. from
	// an earlier live rendezvous lock this same session.
	w.NavMode = sim.NavTarget

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{testPeerFP: "guest"}
	now := time.Now()

	// This session has already ticked at least once before the undock —
	// the undock's fresh ref is emphatically NOT this session's first.
	m.reconcileTargetLock(w, handles, now)

	// Undock handback: SetTargetGhost fires with an EMPTY ghost slate —
	// the guest's departing craft hasn't reported in yet.
	w.SetTargetGhost(testPeerFP, 99)
	if w.NavMode != sim.NavTarget {
		t.Fatalf("NavTarget demoted to NavOrbit by a momentarily-unresolved fresh ghost ref: %v", w.NavMode)
	}

	// The next tick's reconcileTargetLock call (ghost slate still empty)
	// must not start a countdown for this fresh LIVE ref.
	m.reconcileTargetLock(w, handles, now.Add(time.Second))
	if !m.targetLockPendingSince.IsZero() {
		t.Fatalf("countdown started for a live undock-handback ref: %v", m.targetLockPendingSince)
	}
	if m.targetLockEligible {
		t.Fatalf("undock-handback ref wrongly marked eligible for the give-up countdown")
	}

	// Even well past what would have been the grace window, still no
	// chip and the lock is still standing, waiting.
	m.reconcileTargetLock(w, handles, now.Add(2*targetLockRelatchGrace))
	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Fatalf("undock-handback lock cleared despite no reconnect ever happening: %+v", w.Target)
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Fatalf("false 'lost on reconnect' chip fired for a live undock handback: %+v", e)
		}
	}

	// The guest's report finally lands: resolves normally, like any
	// ordinary live target.
	w.Ghosts = []sim.Ghost{{Owner: testPeerFP, CraftID: 99, PrimaryID: c.Primary.ID}}
	m.reconcileTargetLock(w, handles, now.Add(2*targetLockRelatchGrace+time.Second))
	if !m.targetLockResolvedOnce {
		t.Errorf("ref never marked resolved-once after the guest's report landed")
	}
	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Errorf("lock lost by the time the guest's report resolved: %+v", w.Target)
	}
}
