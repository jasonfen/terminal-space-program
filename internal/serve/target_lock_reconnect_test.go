package serve

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/relay"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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
// #294 review round 3 replaced the original give-up trigger ("any
// resolve failure", rounds 1+2's fix: "any resolve failure on a ref
// eligible from session-start timing") with a PRESENCE rule: the
// countdown runs only when the ref's owner is ABSENT from this
// session's roster — not a timing inference at all. See
// reconcileTargetLock's doc comment in reporting.go for the full
// reasoning.
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

// The presence rule's core case: an owner who IS a member of this
// session (present in the roster `handles` derives from) never starts
// the give-up countdown at all, no matter how long their ghost stays
// unresolved — it just waits and re-latches silently once their report
// resumes, exactly the pre-#294 tolerance an ordinary momentarily-stale
// ghost always had.
func TestTargetLockPresentOwnerNeverCountsDownAndReLatches(t *testing.T) {
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
	if !m.targetLockPendingSince.IsZero() {
		t.Fatalf("countdown started for a present owner: %v", m.targetLockPendingSince)
	}

	// The peer's report resumes a few seconds later, and the ghost slate
	// now carries their craft.
	w.Ghosts = []sim.Ghost{{Owner: testPeerFP, CraftID: 99, PrimaryID: c.Primary.ID}}
	m.reconcileTargetLock(w, handles, now.Add(5*time.Second))

	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Errorf("target lock not restored once the report resumed: %+v", w.Target)
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Errorf("loss chip fired despite the target resolving: %+v", e)
		}
	}
}

// Finding A's literal motivating case: the peer is present (enrolled in
// this session) but their craft is Landed — relay.GhostsFor (internal/
// relay/ghosts.go) filters Landed craft out of the ghost slate
// entirely, so the ghost never resolves even though nothing is wrong
// with the lock. The presence rule must survive this indefinitely, not
// merely for one grace window.
func TestTargetLockLandedPeerSurvivesAndReLatches(t *testing.T) {
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

	// Resolve once — an established lock, same as ordinary live play.
	m.reconcileTargetLock(w, handles, now)
	if !m.targetLockResolvedOnce {
		t.Fatalf("ref never marked resolved-once")
	}

	// The peer lands. GhostsFor filters Landed craft out of the slate —
	// reproduce that shape directly rather than asserting against
	// GhostsFor's internals.
	reports := []relay.CraftReport{{
		Owner: testPeerFP, SubspaceTime: w.Clock.SimTime,
		Crafts: []relay.CraftState{{ID: 99, Landed: true, System: w.System().Name, Primary: c.Primary.ID}},
	}}
	w.Ghosts = relay.GhostsFor(w, reports, handles)
	if len(w.Ghosts) != 0 {
		t.Fatalf("test setup: a Landed craft still produced a ghost: %+v", w.Ghosts)
	}

	// Reset resolved-once so this run exercises the PRESENCE path on its
	// own merits, not the (already-covered) resolved-once tolerance.
	m.targetLockResolvedOnce = false
	for i := 1; i <= 10; i++ {
		m.reconcileTargetLock(w, handles, now.Add(time.Duration(i)*targetLockRelatchGrace))
	}

	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Fatalf("lock cleared for a landed but PRESENT peer: %+v", w.Target)
	}
	if !m.targetLockPendingSince.IsZero() {
		t.Errorf("countdown started for a present (landed) peer: %v", m.targetLockPendingSince)
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Fatalf("false loss chip fired for a landed but present peer: %+v", e)
		}
	}

	// The peer lifts off again — the ghost re-latches normally.
	w.Ghosts = []sim.Ghost{{Owner: testPeerFP, CraftID: 99, PrimaryID: c.Primary.ID}}
	m.reconcileTargetLock(w, handles, now.Add(11*targetLockRelatchGrace))
	if !m.targetLockResolvedOnce {
		t.Errorf("ref not marked resolved-once again after re-latching")
	}
}

// Legibility: a ghost target whose owner is genuinely ABSENT from this
// session (never enrolled here — a standalone save loaded outside the
// session it was bound in, or since removed from the roster) is dropped
// for good after targetLockRelatchGrace and the player is told — not
// left with a silently dangling aim.
func TestTargetLockAbsentOwnerGivesUpAfterGraceWindow(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.SetTargetGhost(testPeerFP, 99)

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{} // testPeerFP is not a member of this session
	now := time.Now()

	m.reconcileTargetLock(w, handles, now) // starts the pending timer
	if m.targetLockPendingSince.IsZero() {
		t.Fatalf("countdown never started for an absent owner")
	}

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

	// Past grace, still unresolved and still absent: give up and say so.
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
	// comes up before Bob's does. Bob is still enrolled (present) the
	// whole time, so this must never start a countdown.
	m := srv.withReporting(reloaded, fpA)
	m = tick(m)
	if w2.Target.Kind != sim.TargetGhost {
		t.Fatalf("target dropped on the very first tick: %+v", w2.Target)
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
// was bound. Once a ref has resolved once, the watchdog must retire for
// it for good — a later resolve failure reverts to the old (pre-#294)
// tolerance: keep the lock, re-latch silently whenever it resolves
// again, never clear it, never chip a loss. This holds independently of
// presence — even an ABSENT owner's already-established lock never
// expires.
func TestTargetLockEstablishedLockNeverExpiresAfterResolvingOnce(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.SetTargetGhost(testPeerFP, 99)
	w.Ghosts = []sim.Ghost{{Owner: testPeerFP, CraftID: 99, PrimaryID: c.Primary.ID}}

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{} // absent — resolved-once must still win over presence
	now := time.Now()

	// Ordinary play: resolves on the very first tick, same as any live
	// rendezvous — this is what marks the ref resolved-once.
	m.reconcileTargetLock(w, handles, now)
	if !m.targetLockResolvedOnce {
		t.Fatalf("ref never marked resolved-once after a successful resolve")
	}

	// The ghost now drops out of the slate for reasons that have nothing
	// to do with the lock, held for far longer than targetLockRelatchGrace.
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
// mid-grace inherited the old timer (and a handle cached at
// timer-start), clearing the new lock early and naming the wrong peer.
// The fix keys the watch on (GhostOwner, CraftID): a retarget starts
// fresh tracking — and its own full grace window — for the new ref.
func TestTargetLockRetargetMidGraceResetsTimer(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	const fpA, fpB = "SHA256:peer-a", "SHA256:peer-b"
	w.SetTargetGhost(fpA, 1)
	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{} // both absent — both are countdown cases
	now := time.Now()

	m.reconcileTargetLock(w, handles, now)
	if m.targetLockPendingSince.IsZero() {
		t.Fatalf("watchdog for A never started")
	}
	retargetAt := now.Add(targetLockRelatchGrace - time.Second) // deep into A's own grace
	m.reconcileTargetLock(w, handles, retargetAt)

	// Player retargets to B before A either resolved or timed out.
	w.SetTargetGhost(fpB, 2)
	m.reconcileTargetLock(w, handles, retargetAt)
	if m.targetLockOwner != fpB || m.targetLockCraftID != 2 {
		t.Fatalf("watchdog still tracking the superseded ref: owner=%q craftID=%d", m.targetLockOwner, m.targetLockCraftID)
	}
	if w.Target.Kind != sim.TargetGhost {
		t.Fatalf("retargeting itself cleared the target: %+v", w.Target)
	}

	// A's timer had ~1s left. If B inherited it, this tick (2s later)
	// would already have given up.
	m.reconcileTargetLock(w, handles, retargetAt.Add(2*time.Second))
	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != fpB {
		t.Fatalf("B cleared almost immediately — inherited A's near-expired timer: %+v", w.Target)
	}
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			t.Fatalf("loss chip fired immediately on retarget — inherited A's near-expired timer: %+v", e)
		}
	}

	// B gets its OWN full grace window measured from the retarget moment.
	m.reconcileTargetLock(w, handles, retargetAt.Add(targetLockRelatchGrace+time.Second))
	if w.Target.Kind != sim.TargetNone {
		t.Errorf("B not given its own full grace window: %+v", w.Target)
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

// #294 review finding 4 / round 3: ADR 0038 undock handback (relay/
// dock.go's SetTargetGhost calls at the docker's release/undock paths)
// aims the docker at the guest's departing craft before the guest's own
// next CraftReport can possibly have landed in w.Ghosts — a brand-new
// ref with an empty ghost slate. The guest is always a PRESENT member
// of this session (they have to be, to have had a stack to undock from
// in the first place), so the presence rule alone — with no notion of
// "session's first tick" needed — keeps this from ever starting a
// countdown, closing the false "lost on reconnect" chip a slow or
// disconnecting guest used to cost the host.
func TestUndockHandbackPresentGuestNeverStartsCountdown(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	// The docker was already flying NavTarget before undock — e.g. from
	// an earlier live rendezvous lock this same session.
	w.NavMode = sim.NavTarget

	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{testPeerFP: "guest"} // the guest is enrolled — present
	now := time.Now()

	// Undock handback: SetTargetGhost fires with an EMPTY ghost slate —
	// the guest's departing craft hasn't reported in yet.
	w.SetTargetGhost(testPeerFP, 99)
	if w.NavMode != sim.NavTarget {
		t.Fatalf("NavTarget demoted to NavOrbit by a momentarily-unresolved fresh ghost ref: %v", w.NavMode)
	}

	m.reconcileTargetLock(w, handles, now)
	if !m.targetLockPendingSince.IsZero() {
		t.Fatalf("countdown started for a live undock-handback ref: %v", m.targetLockPendingSince)
	}

	// Even well past what would have been the grace window, still no
	// chip and the lock is still standing, waiting.
	m.reconcileTargetLock(w, handles, now.Add(2*targetLockRelatchGrace))
	if w.Target.Kind != sim.TargetGhost || w.Target.GhostOwner != testPeerFP || w.Target.CraftID != 99 {
		t.Fatalf("undock-handback lock cleared despite the guest being present the whole time: %+v", w.Target)
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

// #294 review finding B (round 3 redesign): the original round 1/2
// "eligibility" scheme inferred whether a ref deserved a countdown from
// SESSION TIMING (was it Craft.Target's value on this hosting session's
// very first tick?) rather than from anything about the ref itself. A
// per-craft ghost target promoted to w.Target LATER — a vessel switch
// (SetActiveCraftIdx) or an F9 quickload swapping a.world without
// resetting the reportingModel — read as a live retarget under that
// scheme and pended silently forever, resurrecting the original #294
// legibility gap. The presence rule fixes this because it has no notion
// of "session's first tick" at all: it applies whenever the ref is the
// active target, however long this session has already been running.
func TestTargetLockLatePromotionGovernedByPresenceNotTiming(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	m := &reportingModel{owner: "SHA256:self"}
	handles := map[string]string{} // the eventual ghost owner is never enrolled — absent
	now := time.Now()

	// The session has already ticked many times under no target at all —
	// standing in for the time elapsed before a vessel switch or
	// quickload brings a ghost-targeted craft to the front.
	for i := 0; i < 20; i++ {
		m.reconcileTargetLock(w, handles, now.Add(time.Duration(i)*time.Second))
	}

	promoteAt := now.Add(30 * time.Second)
	w.SetTargetGhost(testPeerFP, 99)
	m.reconcileTargetLock(w, handles, promoteAt)
	if m.targetLockPendingSince.IsZero() {
		t.Fatalf("late-promoted ref with an absent owner never started a countdown")
	}

	m.reconcileTargetLock(w, handles, promoteAt.Add(targetLockRelatchGrace+time.Second))
	if w.Target.Kind != sim.TargetNone {
		t.Errorf("late-promoted ref with an absent owner never gave up: %+v", w.Target)
	}
	found := false
	for _, e := range m.localEvents {
		if e.Kind == sim.SessionEventTargetLockLost {
			found = true
		}
	}
	if !found {
		t.Errorf("no loss chip fired for the late-promoted absent-owner ref")
	}
}

// TestWithReportingPrimesSessionBeforeFirstTick — #294 second-round
// review finding 1: a reconnecting guest's world used to see its first
// TickMsg dispatched to m.inner.Update (World.Tick → executeDueNodes)
// BEFORE refreshSession had ever run once, so w.Session was still nil
// and World.sessionKnowsOwner reported false for every ghost-ref node —
// a reconnecting guest's own due node could get cancelled outright on
// the very first tick, with none of the grace targetLockRelatchGrace
// gives the active target. withReporting now primes the session at
// attach (mirrors startHosting), so a due ghost-ref node whose target
// owner IS enrolled and present in the roster must survive the very
// first tick, held exactly like an ordinary pending-resolvable stall.
func TestWithReportingPrimesSessionBeforeFirstTick(t *testing.T) {
	srv := newOfflineServer(t)
	enrollDirect(t, srv, "SHA256:gern", "gern")
	enrollDirect(t, srv, testPeerFP, "peer")

	guestApp, err := srv.newGuestApp("SHA256:gern")
	if err != nil {
		t.Fatalf("newGuestApp: %v", err)
	}
	w := guestApp.World()
	c := w.ActiveCraft()
	c.Nodes = []sim.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      w.Clock.SimTime, // due immediately
		TargetGhostOwner: testPeerFP,
		TargetCraftID:    987654,
	}}

	guest := srv.withReporting(guestApp, "SHA256:gern")
	if w.Session == nil {
		t.Fatal("withReporting did not prime w.Session before the first tick")
	}

	guest, _ = guest.Update(sim.TickMsg(time.Now()))
	_ = guest

	if len(c.Nodes) != 1 {
		t.Fatalf("due ghost-ref node cancelled on reconnect's first tick: len(Nodes)=%d, nodes=%+v", len(c.Nodes), c.Nodes)
	}
	if w.LastNodeTargetRefusal != nil && w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("first-tick cancellation notice fired despite the target owner being enrolled and present: %+v", w.LastNodeTargetRefusal)
	}
}
