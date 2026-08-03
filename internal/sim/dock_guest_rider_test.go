package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Rider view (ADR 0038 S4): while one of this player's craft rides in
// another player's stack, the guest's own Crafts slate is empty (#301) —
// there is nothing local left for FocusCraft to track. FollowDockGuestStack
// repoints the camera at the stack's ghost, reusing the existing Spectate
// mechanism (FocusGhost, v0.28 S6) rather than inventing a new Focus kind.

func riderTestWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

// dockGuestGhost builds a Ghost representing the stack owner's active craft
// plus registers it on the world, mirroring what GhostsFor would produce
// from the owner's reported state.
func dockGuestGhost(w *World) Ghost {
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 400e3
	rel := orbital.Vec3{X: r}
	vel := orbital.Vec3{Y: math.Sqrt(mu / r)}
	g := Ghost{
		Owner: "SHA256:host", CraftID: 99, Handle: "jason", Name: "jason's stack",
		PrimaryID: c.Primary.ID,
		Pos:       w.BodyPosition(c.Primary).Add(rel), RelPos: rel, Vel: vel,
	}
	w.Ghosts = []Ghost{g}
	return g
}

// TestFollowDockGuestStackEntersSpectate: entering DockGuest state points
// the camera at the stack's ghost via FocusGhost — the follow-stack camera,
// ADR 0038 §2.
func TestFollowDockGuestStackEntersSpectate(t *testing.T) {
	w := riderTestWorld(t)
	w.Focus = Focus{Kind: FocusCraft}
	g := dockGuestGhost(w)
	w.DockGuest = &DockGuestLink{OwnerFP: g.Owner, OwnerHandle: g.Handle, OwnerActiveCraftID: g.CraftID}

	w.FollowDockGuestStack()

	if w.Focus.Kind != FocusGhost || w.Focus.GhostOwner != g.Owner || w.Focus.GhostCraftID != g.CraftID {
		t.Fatalf("FollowDockGuestStack focus = %+v, want ghost ref %s/%d", w.Focus, g.Owner, g.CraftID)
	}
	if got := w.FocusPosition(); got.Sub(g.Pos).Norm() > 1e-6 {
		t.Errorf("follow-stack camera position = %+v, want ghost Pos %+v", got, g.Pos)
	}
}

// TestFollowDockGuestStackNoopWhenNotDocked: no DockGuest link means no
// camera change at all — solo/live-flying players are unaffected.
func TestFollowDockGuestStackNoopWhenNotDocked(t *testing.T) {
	w := riderTestWorld(t)
	w.Focus = Focus{Kind: FocusCraft}
	w.FollowDockGuestStack()
	if w.Focus.Kind != FocusCraft {
		t.Errorf("Focus changed with no DockGuest: %+v", w.Focus)
	}
}

// TestFollowDockGuestStackIdempotent: calling it again once already
// tracking the same ghost must not reassign Focus — this is what keeps
// player pan/zoom composed over the fit (Camera Contract, ADR 0021): a
// literal reassignment to an unchanged value is harmless by struct
// equality, but the method must not, say, needlessly re-derive a
// different-looking Focus value that would upset a consumer comparing by
// equality for the Framing Event gate.
func TestFollowDockGuestStackIdempotent(t *testing.T) {
	w := riderTestWorld(t)
	g := dockGuestGhost(w)
	w.DockGuest = &DockGuestLink{OwnerFP: g.Owner, OwnerHandle: g.Handle, OwnerActiveCraftID: g.CraftID}
	w.FollowDockGuestStack()
	first := w.Focus
	w.FollowDockGuestStack()
	if w.Focus != first {
		t.Errorf("FollowDockGuestStack changed an already-tracking Focus: %+v -> %+v", first, w.Focus)
	}
}

// TestDockGuestStackGhostResolves: the helper both screens (VESSEL/ORBIT
// badged panels) and the camera use to find the stack's live state.
func TestDockGuestStackGhostResolves(t *testing.T) {
	w := riderTestWorld(t)
	if _, _, ok := w.DockGuestStackGhost(); ok {
		t.Fatal("resolved a ghost with no DockGuest set")
	}
	g := dockGuestGhost(w)
	w.DockGuest = &DockGuestLink{OwnerFP: g.Owner, OwnerHandle: g.Handle, OwnerActiveCraftID: g.CraftID}
	got, primary, ok := w.DockGuestStackGhost()
	if !ok {
		t.Fatal("DockGuestStackGhost refused with a valid DockGuest + matching ghost")
	}
	if got.CraftID != g.CraftID || got.Owner != g.Owner {
		t.Errorf("resolved ghost = %+v, want %+v", got, g)
	}
	if primary == nil || primary.ID != g.PrimaryID {
		t.Errorf("resolved primary = %+v, want ID %s", primary, g.PrimaryID)
	}

	// Stale ref (ghost gone — owner's report hasn't arrived yet, or they
	// left the system): degrades to ok=false rather than a dangling ref.
	w.Ghosts = nil
	if _, _, ok := w.DockGuestStackGhost(); ok {
		t.Error("DockGuestStackGhost resolved a vanished ghost")
	}
}

// TestDockOwnerOnlineReusesPresenceGate: the rider-view standing block's
// empty-seat fork (ADR 0038 amendment 3) reads the SAME presence gate
// ADR 0040 §4's reclaim already reuses (SessionInfo.Players[].Online, itself
// Server.presence.isOnline) rather than inventing new plumbing — a roster
// already carrying this per player.
func TestDockOwnerOnlineReusesPresenceGate(t *testing.T) {
	w := riderTestWorld(t)
	if w.DockOwnerOnline() {
		t.Error("DockOwnerOnline true with no DockGuest at all")
	}
	w.DockGuest = &DockGuestLink{OwnerFP: "SHA256:host", OwnerHandle: "jason"}
	if w.DockOwnerOnline() {
		t.Error("DockOwnerOnline true with no Session roster to consult")
	}
	w.Session = &SessionInfo{Players: []SessionPlayer{
		{Fingerprint: "SHA256:host", Handle: "jason", Online: false},
	}}
	if w.DockOwnerOnline() {
		t.Error("DockOwnerOnline true while the roster says the owner is offline")
	}
	w.Session.Players[0].Online = true
	if !w.DockOwnerOnline() {
		t.Error("DockOwnerOnline false while the roster says the owner is online")
	}
}

// TestAdoptCraftReturnsCameraFromGhostFocus: getting a real, controllable
// craft back (undock handback) while the camera was spectating a ghost
// must return the camera to it — otherwise the safe handback lands the
// player on a live, flyable ship while the view keeps showing the stack
// they just left (ADR 0038 §2 follow-through).
func TestAdoptCraftReturnsCameraFromGhostFocus(t *testing.T) {
	w := riderTestWorld(t)
	g := dockGuestGhost(w)
	w.Focus = Focus{Kind: FocusGhost, GhostOwner: g.Owner, GhostCraftID: g.CraftID}
	w.Crafts = nil
	w.ActiveCraftIdx = 0

	if w.ActiveCraft() != nil {
		t.Fatal("test setup: slate should be empty pre-adopt")
	}
	c := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	w.AdoptCraft(c, true)

	if w.Focus.Kind == FocusGhost {
		t.Errorf("Focus still spectating a ghost after a craft was adopted active: %+v", w.Focus)
	}
}
