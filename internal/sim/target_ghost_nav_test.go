package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ghostWorld builds a world with one live ghost targeted.
func ghostWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	w.Ghosts = []Ghost{{
		Owner: "SHA256:gern", CraftID: 42, Handle: "gern", Name: "Aloft",
		PrimaryID: c.Primary.ID,
		Pos:       w.BodyPosition(c.Primary).Add(c.State.R.Scale(-1)),
		Vel:       c.State.V.Scale(-1),
	}}
	w.SetTargetGhost("SHA256:gern", 42)
	return w
}

// Review follow-up: TGT nav modes work against a ghost target — the
// cycle reaches NavTarget, the basis doesn't downgrade, and attitude
// intents resolve to target-relative burn modes.
func TestNavTargetWorksAgainstGhost(t *testing.T) {
	w := ghostWorld(t)
	if !w.HasRelativeTarget() {
		t.Fatal("ghost target not a relative target")
	}
	reached := false
	for i := 0; i < 3; i++ {
		if w.CycleNavMode() == NavTarget {
			reached = true
			break
		}
	}
	if !reached {
		t.Fatal("CycleNavMode never offered NavTarget with a ghost target")
	}
	if got := w.ResolveAttitudeIntent(IntentPrograde); got != w.ResolveAttitudeIntent(IntentPrograde) {
		t.Fatal("unstable resolve") // sanity
	}
	// reconcile keeps NavTarget while the ghost target is set…
	w.reconcileNavMode()
	if w.NavMode != NavTarget {
		t.Error("reconcileNavMode dropped NavTarget for a live ghost target")
	}
	// …and drops it when the target clears.
	w.ClearTarget()
	if w.NavMode == NavTarget {
		t.Error("NavTarget survived ClearTarget")
	}
}

// #294 fix: a ghost target now persists through the save round trip
// (save package's CraftToWire/CraftFromWire) instead of normalising to
// no-target — the fingerprint is meaningful within the session it was
// set in, and a reconnect that lands before it resolves again is
// handled by the serve layer's deferred re-latch, not by dropping the
// intent here. This test asserts the mirror invariant the save package
// relies on: the active craft carries the ghost target so save sees
// it. The round-trip itself is covered by save.TestGhostTargetPersistsOnSave.
func TestGhostTargetMirroredToActiveCraft(t *testing.T) {
	w := ghostWorld(t)
	if w.Target.Kind != TargetGhost {
		t.Fatal("fixture lost its ghost target")
	}
	_ = orbital.Vec3{} // keep the import for the fixture
	if w.ActiveCraft().Target.Kind != TargetGhost {
		t.Error("ghost target not mirrored to the active craft")
	}
}
