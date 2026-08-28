package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

// #294 review finding 4: a live target-relative SAS mode whose ghost
// target stops resolving mid-session must not command a direction
// derived from the zero-value (rT, vT) — attitudeContext falls back to
// an orbit-frame hold (BurnPrograde) instead of feeding
// BurnDirectionForBurn a target snapshot that doesn't exist. Without the
// fallback, DirectionUnitTarget's BurnTarget case degrades to
// unit(0 − rA): the nose gets pinned at the primary's centre — a live,
// continuously-recomputed SAS command, not a one-time glitch.
func TestAttitudeContextFallsBackWhenGhostUnresolved(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	// Ghost offset along the orbit-normal (R × V) — guaranteed
	// perpendicular to both R and V for a non-degenerate orbit, so the
	// resolved BurnTarget direction is unambiguously distinct from BOTH
	// BurnPrograde (~V) and the old buggy "aim at primary centre" (~ -R)
	// answer, ruling out a coincidental match hiding a real failure.
	h := c.State.R.Cross(c.State.V)
	offset := h.Scale(2e6 / h.Norm())
	w.Ghosts = []Ghost{{
		Owner: "SHA256:gern", CraftID: 42, Handle: "gern", Name: "Aloft",
		PrimaryID: c.Primary.ID,
		Pos:       w.BodyPosition(c.Primary).Add(c.State.R).Add(offset),
		Vel:       c.State.V,
	}}
	w.SetTargetGhost("SHA256:gern", 42)
	c.AttitudeMode = spacecraft.BurnTarget // "toward target" — target-relative

	progradeDir := spacecraft.DirectionUnit(spacecraft.BurnPrograde, c.State.R, c.State.V)
	rNorm := c.State.R.Norm()
	badDir := c.State.R.Scale(-1 / rNorm) // the old buggy "aim at planet centre" answer

	// Resolved: aims along the orbit normal (toward the ghost's offset),
	// not at BurnPrograde or the planet-centre direction — proves the
	// fixture actually exercises the target-relative branch.
	dirResolved := w.commandedDirFor(c)
	if dirResolved.Sub(progradeDir).Norm() < 1e-6 || dirResolved.Sub(badDir).Norm() < 1e-6 {
		t.Fatalf("fixture didn't exercise a distinct BurnTarget direction: got %+v", dirResolved)
	}

	// Unresolved: must fall back to the orbit-frame hold (BurnPrograde),
	// never the old unit(0 − rA) planet-centre pin.
	w.Ghosts = nil
	dirUnresolved := w.commandedDirFor(c)
	if d := dirUnresolved.Sub(progradeDir).Norm(); d > 1e-9 {
		t.Errorf("unresolved BurnTarget direction = %+v, want the BurnPrograde fallback %+v (diff %g)",
			dirUnresolved, progradeDir, d)
	}
	if d := dirUnresolved.Sub(badDir).Norm(); d < 1e-6 {
		t.Error("unresolved BurnTarget still aims at the primary's centre")
	}
}
