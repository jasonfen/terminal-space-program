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
//
// NOTE: this restates the implementation (two calls, nothing else changes
// in between) rather than pinning the interesting case — see #331 and the
// tests below, which change Focus *between* calls the way a player
// actually would.
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

// TestFollowDockGuestStackReleasesOnPlayerFocusChange (#331 case 1): f/g
// are documented CAMERA & VIEW keys. Pressing one while riding a stack
// exits FocusGhost via CycleFocus's Spectate-exit clause. The very next
// tick's FollowDockGuestStack call (unconditional every tick from
// reporting.go's refreshSession) must not stamp the ghost focus straight
// back — that silently defeats the key.
func TestFollowDockGuestStackReleasesOnPlayerFocusChange(t *testing.T) {
	w := riderTestWorld(t)
	g := dockGuestGhost(w)
	w.DockGuest = &DockGuestLink{OwnerFP: g.Owner, OwnerHandle: g.Handle, OwnerActiveCraftID: g.CraftID}
	w.FollowDockGuestStack()
	if w.Focus.Kind != FocusGhost {
		t.Fatalf("setup: expected the follow-stack camera to engage, got %+v", w.Focus)
	}

	// Player presses f/g: CycleFocus's Spectate-exit clause returns to
	// own-craft (or system) framing.
	w.CycleFocus(true)
	afterKey := w.Focus
	if afterKey.Kind == FocusGhost {
		t.Fatalf("setup: CycleFocus did not leave FocusGhost: %+v", afterKey)
	}

	w.FollowDockGuestStack()
	if w.Focus != afterKey {
		t.Errorf("FollowDockGuestStack overrode a player-initiated f/g focus change: got %+v, want %+v", w.Focus, afterKey)
	}
}

// TestFollowDockGuestStackReleasesOnSpectate (#331 case 2): a rider
// Spectating a third player (the Session screen's [v] row action, app.go's
// SessionCmdSpectate handler calling SpectateGhost) must not have the very
// next tick snap the camera back to the stack they're riding — the toast
// promises "spectating carol — [f] to return" and the camera must
// actually move there.
func TestFollowDockGuestStackReleasesOnSpectate(t *testing.T) {
	w := riderTestWorld(t)
	g := dockGuestGhost(w)
	w.DockGuest = &DockGuestLink{OwnerFP: g.Owner, OwnerHandle: g.Handle, OwnerActiveCraftID: g.CraftID}
	w.FollowDockGuestStack()

	third := Ghost{
		Owner: "SHA256:carol", CraftID: 7, Handle: "carol", Name: "carol's ship",
		PrimaryID: g.PrimaryID, Pos: g.Pos, RelPos: g.RelPos, Vel: g.Vel,
	}
	w.Ghosts = append(w.Ghosts, third)
	w.SpectateGhost(third.Owner, third.CraftID)

	w.FollowDockGuestStack()

	if w.Focus.Kind != FocusGhost || w.Focus.GhostOwner != third.Owner || w.Focus.GhostCraftID != third.CraftID {
		t.Errorf("FollowDockGuestStack overrode a Spectate: got %+v, want ghost ref %s/%d", w.Focus, third.Owner, third.CraftID)
	}
}

// TestFollowDockGuestStackReleasesOnSpawnFocus (#331 case 3): a rider who
// spawns a second vessel while still docked gets focusNewCraft (spawn.go)
// pointing Focus at the new craft. The next tick must not force the
// camera back onto the owner's stack — that split-brain (panels on the
// new craft, camera locked on the stack) was unrecoverable by f/g.
func TestFollowDockGuestStackReleasesOnSpawnFocus(t *testing.T) {
	w := riderTestWorld(t)
	g := dockGuestGhost(w)
	w.DockGuest = &DockGuestLink{OwnerFP: g.Owner, OwnerHandle: g.Handle, OwnerActiveCraftID: g.CraftID}
	w.FollowDockGuestStack()

	w.focusNewCraft()
	if w.Focus.Kind != FocusCraft {
		t.Fatalf("setup: focusNewCraft did not set FocusCraft: %+v", w.Focus)
	}

	w.FollowDockGuestStack()
	if w.Focus.Kind != FocusCraft {
		t.Errorf("FollowDockGuestStack overrode a post-spawn focus: got %+v", w.Focus)
	}
}

// TestFollowDockGuestStackReassertsWhenRideChanges pins the other half of
// the #331 ruling: the rider camera is a convenience, not a permanent
// lock — once the ridden stack itself changes (the owner switches active
// craft underneath the rider), the follow may re-assert even though the
// player had released it.
func TestFollowDockGuestStackReassertsWhenRideChanges(t *testing.T) {
	w := riderTestWorld(t)
	g := dockGuestGhost(w)
	w.DockGuest = &DockGuestLink{OwnerFP: g.Owner, OwnerHandle: g.Handle, OwnerActiveCraftID: g.CraftID}
	w.FollowDockGuestStack()

	w.CycleFocus(true) // player releases the follow (f/g)
	if w.Focus.Kind == FocusGhost {
		t.Fatal("setup: expected the player's focus change to leave FocusGhost")
	}

	// The owner moves to a different active craft (e.g. Transfer Control,
	// or switching stacks) — the ride itself changed, so it re-fits.
	newCraftID := g.CraftID + 1
	w.DockGuest.OwnerActiveCraftID = newCraftID
	w.FollowDockGuestStack()

	if w.Focus.Kind != FocusGhost || w.Focus.GhostCraftID != newCraftID {
		t.Errorf("FollowDockGuestStack did not re-assert on a ride change: %+v", w.Focus)
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
