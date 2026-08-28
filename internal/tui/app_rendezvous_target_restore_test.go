package tui

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// TestSessionRendezvousRefusalRestoresPriorTarget (PR #392 review
// finding 2): SessionCmdRendezvous calls SetTargetGhost BEFORE
// RendezvousCommit so the commit search has something to aim at, but a
// refusal must not leave that switch behind. Before the fix, a refused
// Engage silently replaced whatever the player was targeting before
// (mid-transfer TargetBody Moon, say) with the rejected ghost ref, and
// left it there — even though the game said no to the rendezvous
// itself.
func TestSessionRendezvousRefusalRestoresPriorTarget(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world

	// Prior target: a body (the finding's own "mid-transfer TargetBody
	// Moon" example) — deliberately NOT a craft/ghost target, so
	// HasRelativeTarget is false and NavMode is pinned to NavOrbit,
	// giving both fields concrete "before" values to check for.
	w.SetTargetBody(1)
	prevTarget := w.Target
	prevNav := w.NavMode
	if prevTarget.Kind != sim.TargetBody {
		t.Fatalf("precondition: target kind = %v, want TargetBody", prevTarget.Kind)
	}

	// No ghost registered for this owner/craftID — RendezvousCommit's
	// primary resolution fails immediately, guaranteeing a refusal
	// without depending on any particular orbital geometry.
	model, _ := a.applySessionCommand(screens.SessionCommand{
		Kind: screens.SessionCmdRendezvous, Owner: "SHA256:nobody", CraftID: 999, Handle: "ghost",
	})
	app := model.(*App)

	if app.world.RendezvousArm != nil {
		t.Fatal("precondition: refusal path must not have armed a rendezvous")
	}
	if app.world.Target != prevTarget {
		t.Errorf("Target after refused Engage = %+v, want restored prior target %+v", app.world.Target, prevTarget)
	}
	if app.world.NavMode != prevNav {
		t.Errorf("NavMode after refused Engage = %v, want restored prior NavMode %v", app.world.NavMode, prevNav)
	}
}

// TestSessionRendezvousSuccessKeepsGhostTarget: the companion positive
// case — a successful arm (RendezvousCommit ok=true) is exactly the
// case where the target SHOULD end up pointed at the partner's ghost;
// the restore added for finding 2 must be scoped to the refusal branch
// only, not swallow a successful Engage's own target switch.
func TestSessionRendezvousSuccessKeepsGhostTarget(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	w := a.world
	w.Ghosts = []sim.Ghost{ghostBesideActive(w, "SHA256:guest", 42)}

	model, _ := a.applySessionCommand(screens.SessionCommand{
		Kind: screens.SessionCmdRendezvous, Owner: "SHA256:guest", CraftID: 42, Handle: "gern",
	})
	app := model.(*App)

	if app.world.RendezvousArm == nil {
		t.Fatal("expected the converging-geometry fixture to arm")
	}
	if app.world.Target.Kind != sim.TargetGhost || app.world.Target.GhostOwner != "SHA256:guest" {
		t.Errorf("Target after a successful Engage = %+v, want gern's ghost", app.world.Target)
	}
}
