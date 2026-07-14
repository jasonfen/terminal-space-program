package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const guestFP = "SHA256:guest-fingerprint"

// TestDockGuestCraftFusesWithDockerOwnership: a cross-player dock fuses one
// stack owned by the docker (its identity survives) and tags the guest's
// contributed component with the guest's fingerprint + pre-dock ID.
func TestDockGuestCraftFusesWithDockerOwnership(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")

	docker := w.Crafts[0]
	docker.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	vc := math.Sqrt(earth.GravitationalParameter() / docker.State.R.Norm())
	docker.State.V = orbital.Vec3{Y: vc}
	dockerName, dockerID := docker.Name, docker.ID
	dockerMass := docker.TotalMass()

	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	guest.Primary = *earth
	guest.ID = 4242
	guest.State = physics.StateVector{R: docker.State.R.Add(orbital.Vec3{X: 10}), V: docker.State.V, M: guest.TotalMass()}
	guestMass := guest.TotalMass()

	comp, idx, ok := w.DockGuestCraft(0, guest, guestFP)
	if !ok {
		t.Fatalf("DockGuestCraft failed")
	}
	if len(w.Crafts) != 1 {
		t.Fatalf("slate count = %d, want 1 composite", len(w.Crafts))
	}
	if idx != 0 || comp != w.Crafts[0] {
		t.Errorf("composite index/pointer mismatch: idx=%d", idx)
	}
	// Docker owns: composite keeps the docker's identity.
	if comp.Name != dockerName || comp.ID != dockerID {
		t.Errorf("composite identity = %q/%d, want docker %q/%d", comp.Name, comp.ID, dockerName, dockerID)
	}
	// Mass conserved.
	if got, want := comp.TotalMass(), dockerMass+guestMass; math.Abs(got-want) > 1e-3 {
		t.Errorf("composite mass = %v, want %v", got, want)
	}
	// Exactly one guest-owned component, tagged + carrying the guest's ID.
	if !StackHasGuest(comp) {
		t.Fatal("composite not recognised as a cross-player stack")
	}
	var guestComps int
	for _, dc := range comp.DockedComponents {
		if dc.Owner == guestFP {
			guestComps++
			if dc.CraftID != 4242 {
				t.Errorf("guest component CraftID = %d, want 4242", dc.CraftID)
			}
		}
	}
	if guestComps != 1 {
		t.Errorf("guest-owned components = %d, want 1", guestComps)
	}
	// The docker's own component is untagged.
	for _, dc := range comp.DockedComponents {
		if dc.CraftID == dockerID && dc.Owner != "" {
			t.Errorf("docker component tagged with owner %q, want empty", dc.Owner)
		}
	}
}

// TestUndockGuestReturnsCraftAtMatchingState: undocking the guest's component
// hands back a live craft with the guest's original ID at the composite's
// current state, shrinks the composite to the docker's plain craft, and
// conserves mass across the split.
func TestUndockGuestReturnsCraftAtMatchingState(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")

	docker := w.Crafts[0]
	docker.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	vc := math.Sqrt(earth.GravitationalParameter() / docker.State.R.Norm())
	docker.State.V = orbital.Vec3{Y: vc}
	dockerID := docker.ID
	dockerMass := docker.TotalMass()

	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	guest.Primary = *earth
	guest.ID = 99
	guest.State = physics.StateVector{R: docker.State.R.Add(orbital.Vec3{X: 8}), V: docker.State.V, M: guest.TotalMass()}
	guestMass := guest.TotalMass()

	comp, _, ok := w.DockGuestCraft(0, guest, guestFP)
	if !ok {
		t.Fatalf("DockGuestCraft failed")
	}
	stackR, stackV := comp.State.R, comp.State.V

	restored, ok := w.UndockGuest(0, guestFP, 99)
	if !ok {
		t.Fatalf("UndockGuest failed")
	}
	// Returned home with the same stable ID.
	if restored.ID != 99 {
		t.Errorf("restored ID = %d, want 99", restored.ID)
	}
	// Matching seam: placed exactly at the stack's current state.
	if restored.State.R != stackR || restored.State.V != stackV {
		t.Errorf("restored state %v/%v != stack %v/%v", restored.State.R, restored.State.V, stackR, stackV)
	}
	// Mass conserved: restored ≈ guest, remaining composite ≈ docker.
	if math.Abs(restored.TotalMass()-guestMass) > 1e-3 {
		t.Errorf("restored mass = %v, want guest %v", restored.TotalMass(), guestMass)
	}
	// The restored craft is NOT added to this (the docker's) World — it goes
	// home. Slate still holds only the shrunken composite.
	if len(w.Crafts) != 1 {
		t.Fatalf("docker slate count = %d, want 1 (guest went home)", len(w.Crafts))
	}
	shrunk := w.Crafts[0]
	if shrunk.ID != dockerID {
		t.Errorf("remaining craft ID = %d, want docker %d", shrunk.ID, dockerID)
	}
	if math.Abs(shrunk.TotalMass()-dockerMass) > 1e-3 {
		t.Errorf("remaining mass = %v, want docker %v", shrunk.TotalMass(), dockerMass)
	}
	if StackHasGuest(shrunk) || len(shrunk.DockedComponents) != 0 {
		t.Errorf("docker reverted to composite: components=%d", len(shrunk.DockedComponents))
	}
}

// TestUndockGuestUnknownOwnerNoOp: undocking an owner not present in the
// stack is a no-op (nothing to hand back).
func TestUndockGuestUnknownOwnerNoOp(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	docker := w.Crafts[0]
	docker.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	guest.Primary = *earth
	guest.ID = 7
	guest.State = physics.StateVector{R: docker.State.R.Add(orbital.Vec3{X: 5}), M: guest.TotalMass()}
	if _, _, ok := w.DockGuestCraft(0, guest, guestFP); !ok {
		t.Fatalf("dock failed")
	}
	if _, ok := w.UndockGuest(0, "SHA256:someone-else", 0); ok {
		t.Error("UndockGuest returned ok for an owner not in the stack")
	}
}

// TestWithDockCouplingMinWins: the docked-as-guest coupling fold sets the
// state coupled and clamps to the owner's warp, respecting min-wins against
// any existing range couple, and a paused owner imposes no coupling.
func TestWithDockCouplingMinWins(t *testing.T) {
	// From uncoupled: adopt the owner's warp.
	got := CoWarpState{}.WithDockCoupling("gern", 8)
	if !got.Coupled || got.MinWarp != 8 {
		t.Errorf("uncoupled fold = %+v, want coupled@8", got)
	}
	// Owner slower than an existing range couple → owner wins (min).
	got = CoWarpState{Coupled: true, MinWarp: 100, Partners: []string{"ada"}}.WithDockCoupling("gern", 8)
	if got.MinWarp != 8 {
		t.Errorf("min-wins fold MinWarp = %v, want 8", got.MinWarp)
	}
	// Owner faster than an existing couple → existing min stays.
	got = CoWarpState{Coupled: true, MinWarp: 5, Partners: []string{"ada"}}.WithDockCoupling("gern", 50)
	if got.MinWarp != 5 {
		t.Errorf("faster-owner fold MinWarp = %v, want 5", got.MinWarp)
	}
	// Owner listed in Partners for the HUD.
	found := false
	for _, p := range got.Partners {
		if p == "gern" {
			found = true
		}
	}
	if !found {
		t.Errorf("owner handle absent from Partners: %v", got.Partners)
	}
	// Paused owner: no coupling forced.
	got = CoWarpState{}.WithDockCoupling("gern", 0)
	if got.Coupled {
		t.Error("paused owner (0×) forced a coupling")
	}
}

// TestDockCouplingClampsWorldWarp: with the fold written onto World.CoWarp,
// clampedWarp caps a fast selection to the owner's slow warp — the guest
// can't out-warp the stack (reuses S1's clamp end to end).
func TestDockCouplingClampsWorldWarp(t *testing.T) {
	w, _, _ := anchorWorld(t)
	w.Clock.WarpIdx = 3 // selected 1000×
	if got := w.EffectiveWarp(); got != 1000 {
		t.Fatalf("baseline EffectiveWarp = %v, want 1000", got)
	}
	w.CoWarp = CoWarpState{}.WithDockCoupling("gern", 1)
	if eff := w.EffectiveWarp(); eff != 1 {
		t.Errorf("docked-as-guest EffectiveWarp = %v, want clamped to owner 1×", eff)
	}
}

// TestRetagStackForTransfer: transfer flips ownership — the holder's own
// components become the departing guest's, and the recipient's guest
// components become the new holder's own. Idempotent for a same-owner call.
func TestRetagStackForTransfer(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	docker := w.Crafts[0]
	docker.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	const fromFP, toFP = "SHA256:alice", "SHA256:bob"
	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	guest.Primary = *earth
	guest.ID = 55
	guest.State = physics.StateVector{R: docker.State.R.Add(orbital.Vec3{X: 5}), M: guest.TotalMass()}
	comp, _, _ := w.DockGuestCraft(0, guest, toFP)

	RetagStackForTransfer(comp, fromFP, toFP)
	// The docker's (holder's) component now belongs to the departing owner;
	// the recipient's component is now unowned (the new holder's own).
	for _, dc := range comp.DockedComponents {
		switch dc.CraftID {
		case docker.ID:
			if dc.Owner != fromFP {
				t.Errorf("holder component owner = %q, want %q", dc.Owner, fromFP)
			}
		case 55:
			if dc.Owner != "" {
				t.Errorf("recipient component owner = %q, want empty", dc.Owner)
			}
		}
	}
	// Still a cross-player stack (now owned the other way).
	if !StackHasGuest(comp) {
		t.Error("post-transfer stack lost its cross-player provenance")
	}
}
