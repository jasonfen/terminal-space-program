package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// ghostShapedLike builds a ghost whose CONVERTED viewer-frame state (what
// TargetStateRelativeToActivePrimary returns) reproduces exactly the given
// active-primary-relative (r, v) — regardless of which body claims
// provenance. That isolates the #261 gate: identical geometry, different
// primary identity, so an ok/refuse flip can only come from the identity
// check, never from the state math.
func ghostShapedLike(w *World, provenance bodies.CelestialBody, r, v orbital.Vec3, owner, handle string, craftID uint64) Ghost {
	active := w.ActiveCraft()
	return Ghost{
		Owner:     owner,
		Handle:    handle,
		CraftID:   craftID,
		Name:      "Remote",
		PrimaryID: provenance.ID,
		Pos:       w.BodyPosition(active.Primary).Add(r),
		Vel:       w.bodyInertialVelocity(active.Primary).Add(v).Sub(w.bodyInertialVelocity(provenance)),
	}
}

// otherBody returns any body in the active system that is not the active
// craft's primary — the provenance for a partner who burned out of the
// shared SOI.
func otherBody(t *testing.T, w *World) bodies.CelestialBody {
	t.Helper()
	primaryID := w.ActiveCraft().Primary.ID
	for _, b := range w.System().Bodies {
		if b.ID != primaryID {
			return b
		}
	}
	t.Fatal("no second body in the system to stage a cross-primary ghost")
	return bodies.CelestialBody{}
}

// RendezvousCommit's current-course fallback must not manufacture an
// encounter from a cross-primary target (#261): the converted state is not
// on a conic around the viewer's primary, so a closest approach
// Kepler-propagated there is dynamically meaningless. Gate on primary
// identity — the same signal the peer-set fallback already filters on.
//
// The control (same LEO shape, same-primary provenance) pins that the
// refusal is the identity gate, not the geometry failing to close.
func TestRendezvousCommitCrossPrimaryGhostRefuses(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	sister := w.Crafts[1]

	// Control: the sister's LEO shape with same-primary provenance commits.
	same := ghostShapedLike(w, w.ActiveCraft().Primary, sister.State.R, sister.State.V,
		"SHA256:gern", "gern", 77)
	w.Ghosts = []Ghost{same}
	w.SetTargetGhost(same.Owner, same.CraftID)
	if _, _, ok := w.RendezvousCommit(); !ok {
		t.Fatal("control: same-primary ghost with the sister's LEO shape did not commit")
	}

	// Same converted geometry, cross-primary provenance: refuse.
	cross := ghostShapedLike(w, otherBody(t, w), sister.State.R, sister.State.V,
		"SHA256:gern", "gern", 77)
	w.Ghosts = []Ghost{cross}
	if tau, _, ok := w.RendezvousCommit(); ok {
		t.Errorf("RendezvousCommit committed τ=%v against a cross-primary ghost — "+
			"a viewer-primary-propagated encounter with wrong dynamics (#261)", tau)
	}
}

// The standing intent's mid-coast re-derivation (#252) must idle-and-retry
// when the armed partner has left the shared SOI, not adopt a bogus
// waypoint via the ghost path: the ghost path reuses RendezvousCommit,
// whose fallback would otherwise propagate the partner's converted state
// around the VIEWER's primary. With the ghost path gated, derivation falls
// through to the peer-set search — which already filters same-primary —
// finds nothing, and reports not-found (the coast idles at the 1× floor).
func TestRendezvousNextWaypointCrossPrimaryPartnerIdles(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	sister := w.Crafts[1]
	other := otherBody(t, w)

	cross := ghostShapedLike(w, other, sister.State.R, sister.State.V,
		"SHA256:gern", "gern", 77)
	w.Ghosts = []Ghost{cross}
	w.SetTargetGhost(cross.Owner, cross.CraftID)

	partner := &CoWarpPeer{
		Owner: "SHA256:gern", Handle: "gern", SubspaceTime: w.Clock.SimTime,
		ArmedTowardViewer: true,
		Crafts:            []CoWarpCraft{{Primary: other.ID, R: sister.State.R, V: sister.State.V}},
	}
	if tau, _, ok := w.rendezvousNextWaypoint(partner); ok {
		t.Errorf("derived a waypoint (τ=%v) from a partner outside the shared SOI — "+
			"the ghost path leaked past the primary gate (#261)", tau)
	}

	// Control: the same partner back in the shared SOI derives again — the
	// idle above is the identity gate, not the geometry.
	same := ghostShapedLike(w, w.ActiveCraft().Primary, sister.State.R, sister.State.V,
		"SHA256:gern", "gern", 77)
	w.Ghosts = []Ghost{same}
	partner.Crafts[0].Primary = w.ActiveCraft().Primary.ID
	if _, _, ok := w.rendezvousNextWaypoint(partner); !ok {
		t.Error("control: same-primary partner failed to derive a waypoint — " +
			"geometry, not the gate, is failing this test")
	}
}
