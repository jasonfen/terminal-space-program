package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// TestRendezvousMutualUnplanned_ResetOnArmClear is finding 4 (batch
// review, LOW): RendezvousMutualUnplanned is assigned inside
// driveRendezvousCoast (arm.Tau.IsZero() && partner != nil) but, unlike
// RendezvousHold / RendezvousPaced / RendezvousPartnerAway, was not
// reset at the top of the function — so the arm==nil early return left
// it stale at whatever it was on the tick the arm was cleared. Currently
// latent (every reader is behind RendezvousUnplanned(), which requires a
// non-nil arm) but it breaks the single-writer/reset-at-top invariant
// the surrounding flags document.
func TestRendezvousMutualUnplanned_ResetOnArmClear(t *testing.T) {
	w, primary, st := anchorWorld(t)
	if !w.EngageRendezvousWarpAs("SHA256:gern", "gern", time.Time{}, 0, true) {
		t.Fatal("Engage should succeed")
	}
	peer := peerAt(w, primary, st, 3, orbital.Vec3{X: 50_000}, orbital.Vec3{}, "gern")
	peer.ArmedTowardViewer = true

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if !w.RendezvousMutualUnplanned {
		t.Fatal("precondition: mutual unplanned should be true once the partner has armed back")
	}

	// Cancel the arm, then drive a tick with no partner report at all —
	// the arm==nil early return in driveRendezvousCoast must not leave
	// RendezvousMutualUnplanned stuck true.
	w.DisengageRendezvousWarp()
	w.DriveRendezvousWarp(nil)

	if w.RendezvousMutualUnplanned {
		t.Error("RendezvousMutualUnplanned stayed true after the arm cleared — reset-at-top invariant broken")
	}
}
