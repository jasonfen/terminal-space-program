package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestExecuteDueNodesFiniteBurnNotOverwritten — GH #88 (#1, HIGH).
// When two finite nodes both come due in the same tick, the dispatcher
// must start the *earlier* one's ActiveBurn and leave the later one
// queued — not overwrite ActiveBurn with the second node and silently
// drop the first node's Δv. Pre-fix the loop set c.ActiveBurn for node
// A, kept iterating, overwrote it with node B, and popped both — so
// node A's burn never executed.
func TestExecuteDueNodesFiniteBurnNotOverwritten(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	now := w.Clock.SimTime

	// Two finite nodes whose centered burn windows both straddle `now`,
	// so both BurnStart() <= now on this tick. A triggers slightly
	// before B; distinct Δv so we can tell which one started.
	nodeA := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          100,
		TriggerTime: now.Add(10 * time.Second),
		Duration:    60 * time.Second, // BurnStart = now-20s
	}
	nodeB := spacecraft.ManeuverNode{
		Mode:        spacecraft.BurnPrograde,
		DV:          200,
		TriggerTime: now.Add(15 * time.Second),
		Duration:    60 * time.Second, // BurnStart = now-15s
	}
	c.Nodes = []spacecraft.ManeuverNode{nodeA, nodeB}
	sortNodes(c.Nodes)

	w.executeDueNodes()

	if c.ActiveBurn == nil {
		t.Fatal("no ActiveBurn started; expected node A's finite burn")
	}
	if c.ActiveBurn.DVRemaining != 100 {
		t.Errorf("ActiveBurn.DVRemaining = %.0f, want 100 (node A) — node B overwrote node A's burn",
			c.ActiveBurn.DVRemaining)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d after dispatch, want 1 (node B must stay queued, not be silently dropped)", len(c.Nodes))
	}
	if c.Nodes[0].DV != 200 {
		t.Errorf("remaining node DV = %.0f, want 200 (node B) — wrong node was retained", c.Nodes[0].DV)
	}
}

// TestExecuteDueNodesDoesNotOverwriteInFlightBurn — GH #88 (#1, HIGH),
// cross-tick variant. A finite burn started on a previous tick must not
// be overwritten when a second finite node comes due while the first is
// still in flight; the due node stays queued until the active burn ends.
func TestExecuteDueNodesDoesNotOverwriteInFlightBurn(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	now := w.Clock.SimTime

	// Simulate an in-flight burn from a prior tick.
	c.ActiveBurn = &spacecraft.ActiveBurn{
		Mode:        spacecraft.BurnPrograde,
		DVRemaining: 500,
		EndTime:     now.Add(30 * time.Second),
	}
	// A second finite node that is already due this tick.
	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:        spacecraft.BurnPrograde,
		DV:          200,
		TriggerTime: now.Add(5 * time.Second),
		Duration:    40 * time.Second, // BurnStart = now-15s
	}}

	w.executeDueNodes()

	if c.ActiveBurn == nil || c.ActiveBurn.DVRemaining != 500 {
		t.Fatalf("in-flight ActiveBurn was overwritten: %+v", c.ActiveBurn)
	}
	if len(c.Nodes) != 1 {
		t.Errorf("len(Nodes) = %d, want 1 — the due node must stay queued behind the in-flight burn", len(c.Nodes))
	}
}

// TestSortNodesOrdersByBurnStart — GH #88 (#2, MEDIUM). executeDueNodesFor
// fires/breaks on BurnStart() (= TriggerTime - Duration/2), so the node
// slice must be sorted by BurnStart, not TriggerTime. A later-trigger but
// longer-duration node has an earlier BurnStart and must dispatch first.
func TestSortNodesOrdersByBurnStart(t *testing.T) {
	base := time.Unix(0, 0).UTC()
	// Node A: impulsive at T+100 (BurnStart = T+100).
	a := spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 10, TriggerTime: base.Add(100 * time.Second)}
	// Node B: finite at T+110, Duration 40s (BurnStart = T+90 < A's).
	b := spacecraft.ManeuverNode{Mode: spacecraft.BurnPrograde, DV: 20, TriggerTime: base.Add(110 * time.Second), Duration: 40 * time.Second}

	nodes := []spacecraft.ManeuverNode{a, b}
	sortNodes(nodes)

	if !nodes[0].BurnStart().Before(nodes[1].BurnStart()) {
		t.Errorf("nodes not ordered by BurnStart: [0]=%v [1]=%v",
			nodes[0].BurnStart(), nodes[1].BurnStart())
	}
	if nodes[0].DV != 20 {
		t.Errorf("node[0].DV = %.0f, want 20 (the earlier-BurnStart finite node B)", nodes[0].DV)
	}
}

// sessionWithOwner stubs a minimal hosted-session slate naming owner as
// a roster member — the presence a ghost-ref node needs to HOLD rather
// than cancel at fire time (#294 review round 3 finding E,
// World.sessionKnowsOwner).
func sessionWithOwner(owner string) *SessionInfo {
	return &SessionInfo{Players: []SessionPlayer{{Fingerprint: owner}}}
}

// TestExecuteDueNodesRefusesUnresolvedTargetRelativeImpulsive — #294
// review finding 5. A due impulsive target-relative node whose bound
// ghost ref doesn't resolve (survived a save/load without a live
// serve session to re-latch it, or hasn't re-latched yet post-
// reconnect) must refuse to fire rather than burn against
// nodeTargetRelState's zero-value fallback (DirectionUnitTarget's
// BurnTarget/AntiTarget cases degrade that to a direction aimed at, or
// away from, the primary's centre — real Δv spent in a bogus
// direction). The node must stay queued (not popped) and
// LastNodeTargetRefusal must fire so the player is told. The ghost's
// owner is present in the session (round 3 finding E) — an absent
// owner is a cancel, not a hold; see
// TestExecuteDueNodesCancelsGhostNodeAbsentOwner below.
func TestExecuteDueNodesRefusesUnresolvedTargetRelativeImpulsive(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime
	startFuel := c.ActiveStageFuel()

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now, // due now
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil // the ghost ref never resolves
	w.Session = sessionWithOwner("SHA256:gern")

	w.executeDueNodes()

	if len(c.Nodes) != 1 {
		t.Fatalf("len(Nodes) = %d after refused fire, want 1 (node stays queued)", len(c.Nodes))
	}
	if c.ActiveStageFuel() != startFuel {
		t.Errorf("fuel changed on a refused fire: before=%.6f after=%.6f", startFuel, c.ActiveStageFuel())
	}
	if w.LastNodeTargetRefusal == nil {
		t.Fatal("LastNodeTargetRefusal not set on a refused fire")
	}
	if w.LastNodeTargetRefusal.CraftName != c.Name {
		t.Errorf("LastNodeTargetRefusal.CraftName = %q, want %q", w.LastNodeTargetRefusal.CraftName, c.Name)
	}
}

// TestExecuteDueNodesRefusesUnresolvedTargetRelativeFinite — same as
// above for a finite (Duration>0) target-relative node: it must not
// start c.ActiveBurn against an unresolved target.
func TestExecuteDueNodesRefusesUnresolvedTargetRelativeFinite(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTargetPrograde,
		DV:               100,
		TriggerTime:      now.Add(10 * time.Second),
		Duration:         60 * time.Second, // BurnStart = now-20s, already due
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.Session = sessionWithOwner("SHA256:gern")

	w.executeDueNodes()

	if c.ActiveBurn != nil {
		t.Fatalf("ActiveBurn started against an unresolved target: %+v", c.ActiveBurn)
	}
	if len(c.Nodes) != 1 {
		t.Errorf("len(Nodes) = %d after refused fire, want 1 (node stays queued)", len(c.Nodes))
	}
	if w.LastNodeTargetRefusal == nil {
		t.Error("LastNodeTargetRefusal not set on a refused finite-burn fire")
	}
}

// TestExecuteDueNodesFiresOnceGhostResolves — the flip side: once the
// bound ghost resolves, the previously-refused node fires normally on
// the next tick, proving the refusal is a hold, not a silent drop.
func TestExecuteDueNodesFiresOnceGhostResolves(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.Session = sessionWithOwner("SHA256:gern")
	w.executeDueNodes()
	if len(c.Nodes) != 1 {
		t.Fatalf("node fired/dropped despite an unresolved target: len=%d", len(c.Nodes))
	}

	// The ghost's report resumes.
	w.Ghosts = []Ghost{{
		Owner: "SHA256:gern", CraftID: 987654, PrimaryID: c.Primary.ID,
		Pos: w.BodyPosition(c.Primary).Add(c.State.R).Add(orbital.Vec3{X: 1e6}),
	}}
	w.executeDueNodes()
	if len(c.Nodes) != 0 {
		t.Errorf("node did not fire once its target resolved: len(Nodes)=%d", len(c.Nodes))
	}
}

// TestExecuteDueNodesDropsNeverResolvableLocalTarget — #294 review
// finding 2, never-resolvable case (a): a node bound to a LOCAL craft
// (no TargetGhostOwner) whose target was removed from the slate (e.g.
// end-flight) can never come back the way a ghost might re-latch. The
// old unconditional `break` wedged the queue behind it forever with an
// every-tick refusal flash; it must instead be dropped in place with a
// one-time cancellation notice.
func TestExecuteDueNodesDropsNeverResolvableLocalTarget(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:          spacecraft.BurnTargetPrograde,
		DV:            50,
		TriggerTime:   now,
		TargetCraftID: 424242, // no TargetGhostOwner — local ref to a craft that isn't in the slate
	}}

	w.executeDueNodes()

	if len(c.Nodes) != 0 {
		t.Fatalf("never-resolvable local-target node not dropped: len(Nodes)=%d, nodes=%+v", len(c.Nodes), c.Nodes)
	}
	if w.LastNodeTargetRefusal == nil {
		t.Fatal("no cancellation notice stamped")
	}
	if !w.LastNodeTargetRefusal.Cancelled {
		t.Error("notice not marked Cancelled — a never-resolvable drop must read differently than a pending hold")
	}
}

// TestExecuteDueNodesNeverResolvableUnblocksQueue — dropping a never-
// resolvable node must free the rest of the queue to dispatch on the
// SAME tick, not wait for the next one. Pre-fix, the unconditional
// break meant an ordinary due node behind a doomed one never fired
// either.
func TestExecuteDueNodesNeverResolvableUnblocksQueue(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{
		{Mode: spacecraft.BurnTargetPrograde, DV: 50, TriggerTime: now.Add(-2 * time.Second), TargetCraftID: 424242},
		{Mode: spacecraft.BurnPrograde, DV: 10, TriggerTime: now.Add(-time.Second)},
	}
	sortNodes(c.Nodes)

	w.executeDueNodes()

	if len(c.Nodes) != 0 {
		t.Fatalf("ordinary node behind the dropped one did not fire: len(Nodes)=%d, nodes=%+v", len(c.Nodes), c.Nodes)
	}
}

// TestExecuteDueNodesRefusalStampedOncePerStall — #294 review finding 2:
// the HUD flash for a pending-resolvable (ghost) refusal must fire once
// per stall, not every tick the node sits wedged at the front of the
// queue.
func TestExecuteDueNodesRefusalStampedOncePerStall(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.Session = sessionWithOwner("SHA256:gern")

	w.executeDueNodes()
	if w.LastNodeTargetRefusal == nil {
		t.Fatal("first stall did not stamp a refusal")
	}
	w.LastNodeTargetRefusal = nil // simulate app.go consuming + clearing the flash

	w.executeDueNodes() // same node, still unresolved — same stall
	if w.LastNodeTargetRefusal != nil {
		t.Errorf("refusal restamped for the SAME stall — want once, not every tick: %+v", w.LastNodeTargetRefusal)
	}
	if len(c.Nodes) != 1 {
		t.Fatalf("node dropped or fired despite staying unresolved: len(Nodes)=%d", len(c.Nodes))
	}
}

// TestExecuteDueNodesCancelsGhostNodeAbsentOwner — #294 review round 3
// finding E: reconcileTargetLock's give-up countdown (internal/serve/
// reporting.go) only ever tracks the world's ACTIVE target — a planted
// node bound to some OTHER ghost, or evaluated with no hosting session
// at all (a dock ledger Parcel, a standalone save), never gets a give-up
// any other way. The fire-time presence rule (World.sessionKnowsOwner)
// must cancel it outright instead of wedging the queue forever.
func TestExecuteDueNodesCancelsGhostNodeAbsentOwner(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.Session = nil // no hosting session at all — evaluated outside one

	w.executeDueNodes()

	if len(c.Nodes) != 0 {
		t.Fatalf("ghost-ref node with no session not cancelled: len(Nodes)=%d, nodes=%+v", len(c.Nodes), c.Nodes)
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancellation notice not stamped: %+v", w.LastNodeTargetRefusal)
	}
}

// TestExecuteDueNodesHoldsGhostNodeOwnerNotEnrolledWithinGrace — #294
// second-round review finding 1: a session that simply doesn't (yet)
// list this ref's owner in its roster must NOT cancel the node on the
// very first tick it notices — that is exactly the shape of this
// player's OWN session having just reconnected before refreshSession
// ever primed w.Session (fixed at attach in internal/serve), and an
// owner mid-reconnect must not cost a planted node. The node holds,
// same as an ordinary pending-resolvable stall, for GhostAbsentGrace
// (mirrors reportingModel.targetLockRelatchGrace).
func TestExecuteDueNodesHoldsGhostNodeOwnerNotEnrolledWithinGrace(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.Session = sessionWithOwner("SHA256:someone-else")

	w.executeDueNodes()

	if len(c.Nodes) != 1 {
		t.Fatalf("ghost-ref node for a not-(yet)-enrolled owner cancelled within grace: len(Nodes)=%d", len(c.Nodes))
	}
	if w.LastNodeTargetRefusal == nil {
		t.Fatal("no hold notice stamped")
	}
	if w.LastNodeTargetRefusal.Cancelled {
		t.Error("within-grace absence read as Cancelled — must be a pending hold, not a permanent drop")
	}
	if c.Nodes[0].GhostAbsentSince.IsZero() {
		t.Error("GhostAbsentSince not stamped once the owner was first found absent")
	}
}

// TestExecuteDueNodesCancelsGhostNodeOwnerNotEnrolledPastGrace — the
// flip side: once GhostAbsentGrace has genuinely elapsed with the
// owner still missing from the roster, the node is cancelled, same as
// before this finding's fix (just deferred instead of instant).
func TestExecuteDueNodesCancelsGhostNodeOwnerNotEnrolledPastGrace(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
		GhostAbsentSince: time.Now().Add(-(GhostAbsentGrace + time.Second)),
	}}
	w.Ghosts = nil
	w.Session = sessionWithOwner("SHA256:someone-else")

	w.executeDueNodes()

	if len(c.Nodes) != 0 {
		t.Fatalf("ghost-ref node past grace not cancelled: len(Nodes)=%d", len(c.Nodes))
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancellation notice not stamped: %+v", w.LastNodeTargetRefusal)
	}
}

// TestExecuteDueNodesGhostAbsenceTimerResetsOnceOwnerPresent — the
// absence timer must not survive the owner becoming a roster member
// again: a later, unrelated absence must start its own fresh grace
// window rather than inherit an old one and cancel immediately.
func TestExecuteDueNodesGhostAbsenceTimerResetsOnceOwnerPresent(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.Session = sessionWithOwner("SHA256:someone-else")
	w.executeDueNodes()
	if c.Nodes[0].GhostAbsentSince.IsZero() {
		t.Fatal("absence timer never stamped")
	}

	// Owner reappears in the roster (e.g. reconnects).
	w.Session = sessionWithOwner("SHA256:gern")
	w.executeDueNodes()
	if !c.Nodes[0].GhostAbsentSince.IsZero() {
		t.Error("absence timer not cleared once the owner was seen present")
	}
}

// TestExecuteDueNodesHoldsLocalTargetTransientlyOnDifferentPrimary —
// #294 review round 3 finding F: nodeTargetRelState's own doc names a
// transient case for a LOCAL target ref — the target craft alive but
// briefly on a DIFFERENT primary than this node's own frame, mid SOI-
// transfer. That must HOLD, exactly like a pending-resolvable ghost
// stall — not be treated as permanently gone the way
// TestExecuteDueNodesDropsNeverResolvableLocalTarget's genuinely-absent
// (end-of-flight) case correctly is.
func TestExecuteDueNodesHoldsLocalTargetTransientlyOnDifferentPrimary(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("expected 2 crafts after spawn, got %d", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	sister := w.Crafts[1]
	w.stampCraftID(sister)

	// Any OTHER body in the system stands in for "mid-SOI-transfer,
	// briefly on a different primary than the node's own frame."
	var other bodies.CelestialBody
	found := false
	for _, b := range w.System().Bodies {
		if b.ID != c.Primary.ID {
			other = b
			found = true
			break
		}
	}
	if !found {
		t.Fatal("system has no second body to use as the sister's transient primary")
	}
	sister.Primary = other

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:          spacecraft.BurnTargetPrograde,
		DV:            50,
		TriggerTime:   now,
		PrimaryID:     c.Primary.ID,
		TargetCraftID: sister.ID,
	}}

	w.executeDueNodes()

	if len(c.Nodes) != 1 {
		t.Fatalf("transiently-unresolvable local target node dropped instead of held: len(Nodes)=%d", len(c.Nodes))
	}
	if w.LastNodeTargetRefusal == nil {
		t.Fatal("no stall notice stamped")
	}
	if w.LastNodeTargetRefusal.Cancelled {
		t.Error("transient hold read as Cancelled — must be a pending stall, not a permanent drop")
	}

	// The sister rebases back onto the shared primary — the node fires
	// normally on the next tick, proving the hold is a pause.
	sister.Primary = c.Primary
	w.executeDueNodes()
	if len(c.Nodes) != 0 {
		t.Errorf("node never fired once both craft shared a primary again: len(Nodes)=%d", len(c.Nodes))
	}
}

// TestExecuteDueNodesCancelNoticeFiresAfterPriorHoldLocalTarget — #294
// second-round review finding 5. Both cancel branches used to be
// gated behind `if !n.RefusalNoticed`, but the HOLD branches set that
// SAME flag and nothing ever clears it once a node moves from holding
// to cancelling — so a node that held at least once (a transient
// different-primary stall, here) and later becomes genuinely
// unresolvable (its target leaves the slate for good) was dropped with
// NO notice at all, silently. The cancel must fire unconditionally.
func TestExecuteDueNodesCancelNoticeFiresAfterPriorHoldLocalTarget(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	if _, err := w.SpawnCraft(SpawnSpec{AltitudeM: 400e3}); err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("expected 2 crafts after spawn, got %d", len(w.Crafts))
	}
	w.ActiveCraftIdx = 0
	sister := w.Crafts[1]
	w.stampCraftID(sister)

	var other bodies.CelestialBody
	found := false
	for _, b := range w.System().Bodies {
		if b.ID != c.Primary.ID {
			other = b
			found = true
			break
		}
	}
	if !found {
		t.Fatal("system has no second body to use as the sister's transient primary")
	}
	sister.Primary = other

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:          spacecraft.BurnTargetPrograde,
		DV:            50,
		TriggerTime:   now,
		PrimaryID:     c.Primary.ID,
		TargetCraftID: sister.ID,
	}}

	// First tick: transient stall — holds, stamps RefusalNoticed.
	w.executeDueNodes()
	if len(c.Nodes) != 1 || !c.Nodes[0].RefusalNoticed {
		t.Fatalf("setup: expected the node to hold with RefusalNoticed set: %+v", c.Nodes)
	}
	w.LastNodeTargetRefusal = nil // simulate the HUD consuming the earlier hold flash

	// The sister craft is now gone from the slate for good (end-flight)
	// — a permanent loss, not a transient stall.
	w.Crafts = []*spacecraft.Spacecraft{c}

	w.executeDueNodes()

	if len(c.Nodes) != 0 {
		t.Fatalf("node not cancelled once its target left the slate for good: %+v", c.Nodes)
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancel notice silently swallowed by the prior hold's RefusalNoticed flag: %+v", w.LastNodeTargetRefusal)
	}
}

// TestExecuteDueNodesCancelNoticeFiresAfterPriorHoldGhostTarget — same
// shape as the local-target case above, for a ghost ref: the node
// holds (owner present, ghost unresolved) at least once, stamping
// RefusalNoticed, then the owner drops out of the roster and
// GhostAbsentGrace expires. The cancel must still fire a notice —
// not be swallowed by the RefusalNoticed flag the earlier hold set.
func TestExecuteDueNodesCancelNoticeFiresAfterPriorHoldGhostTarget(t *testing.T) {
	w := mustWorld(t)
	c := w.ActiveCraft()
	now := w.Clock.SimTime

	c.Nodes = []spacecraft.ManeuverNode{{
		Mode:             spacecraft.BurnTarget,
		DV:               100,
		TriggerTime:      now,
		TargetGhostOwner: "SHA256:gern",
		TargetCraftID:    987654,
	}}
	w.Ghosts = nil
	w.Session = sessionWithOwner("SHA256:gern") // owner present — ordinary pending hold

	w.executeDueNodes()
	if len(c.Nodes) != 1 || !c.Nodes[0].RefusalNoticed {
		t.Fatalf("setup: expected the node to hold with RefusalNoticed set: %+v", c.Nodes)
	}
	w.LastNodeTargetRefusal = nil // simulate the HUD consuming the earlier hold flash

	// The owner drops out of the roster and grace has already expired.
	c.Nodes[0].GhostAbsentSince = time.Now().Add(-(GhostAbsentGrace + time.Second))
	w.Session = sessionWithOwner("SHA256:someone-else")

	w.executeDueNodes()

	if len(c.Nodes) != 0 {
		t.Fatalf("node not cancelled past grace: %+v", c.Nodes)
	}
	if w.LastNodeTargetRefusal == nil || !w.LastNodeTargetRefusal.Cancelled {
		t.Errorf("cancel notice silently swallowed by the prior hold's RefusalNoticed flag: %+v", w.LastNodeTargetRefusal)
	}
}
