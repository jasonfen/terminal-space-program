package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestDockingFusesCloseSlowCraft: two craft within DockingDistM
// and below DockingVMS in the same primary frame must fuse on the
// next checkDocking pass. Composite ends up at the mass-weighted
// centroid with momentum-conserving velocity.
func TestDockingFusesCloseSlowCraft(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")

	// Craft A: at +X, moving +Y at orbital speed.
	a := w.Crafts[0]
	a.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := math.Sqrt(earth.GravitationalParameter() / a.State.R.Norm())
	a.State.V = orbital.Vec3{Y: v}

	// Craft B: 10 m further out, also +Y at the same orbital
	// speed (matching velocity).
	b := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	b.Primary = *earth
	b.State = physics.StateVector{
		R: a.State.R.Add(orbital.Vec3{X: 10}),
		V: a.State.V,
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)

	idxA, idxB, ok := w.checkDocking()
	if !ok {
		t.Fatalf("expected docking match, got none")
	}
	if idxA != 0 || idxB != 1 {
		t.Errorf("unexpected pair indices: %d, %d", idxA, idxB)
	}
	if len(w.Crafts) != 1 {
		t.Errorf("expected 1 composite craft, got %d", len(w.Crafts))
	}
	composite := w.Crafts[0]
	if composite.Name != a.Name {
		t.Errorf("composite name = %q, want active partner's name %q", composite.Name, a.Name)
	}
	// Composite mass = sum of both.
	wantMass := a.DryMass + b.DryMass + a.Fuel + b.Fuel + a.Monoprop + b.Monoprop
	if math.Abs(composite.TotalMass()-wantMass) > 1e-3 {
		t.Errorf("composite mass = %v, want %v", composite.TotalMass(), wantMass)
	}
}

// TestDockingSkipsFarApart: craft beyond DockingDistM mustn't fuse.
func TestDockingSkipsFarApart(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	b := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	b.Primary = *earth
	b.State = physics.StateVector{
		R: w.Crafts[0].State.R.Add(orbital.Vec3{X: 1000}), // 1 km away
		V: w.Crafts[0].State.V,
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)

	if _, _, ok := w.checkDocking(); ok {
		t.Error("expected no docking at 1 km separation")
	}
	if len(w.Crafts) != 2 {
		t.Errorf("slate count = %d, want 2", len(w.Crafts))
	}
}

// TestDockingSkipsFastClose: craft within distance but with too
// much relative velocity must not fuse — soft-capture model only.
func TestDockingSkipsFastClose(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	b := spacecraft.NewFromLoadout(spacecraft.LoadoutICPSID)
	b.Primary = *earth
	b.State = physics.StateVector{
		R: w.Crafts[0].State.R.Add(orbital.Vec3{X: 10}),       // 10 m close
		V: w.Crafts[0].State.V.Add(orbital.Vec3{Y: 5}),         // 5 m/s relative
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)

	if _, _, ok := w.checkDocking(); ok {
		t.Error("expected no docking with 5 m/s relative velocity")
	}
}

// TestDockingSkipsBothLanded — v0.12 Slice 2 / ADR 0007. Two
// co-located craft that are BOTH Landed must never auto-fuse, even
// though they sit at the same point with matched velocity (well
// inside both docking gates). This is the structural guard that keeps
// a surface-staged descent + ascent pair from re-merging before
// liftoff. The same pair WITH one craft not Landed still fuses —
// confirming the guard is specifically the both-Landed case, not a
// blanket landed exclusion.
func TestDockingSkipsBothLanded(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")

	a := w.Crafts[0]
	a.Primary = *earth
	a.State.R = orbital.Vec3{X: earth.RadiusMeters()}
	a.State.V = orbital.Vec3{} // co-rotation velocity stand-in (matched)

	b := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	b.Primary = *earth
	b.State = physics.StateVector{R: a.State.R, V: a.State.V, M: b.TotalMass()}
	w.Crafts = append(w.Crafts, b)

	// Both Landed: the guard must skip the pair.
	a.Landed, b.Landed = true, true
	if _, _, ok := w.checkDocking(); ok {
		t.Error("both-Landed co-located pair fused — checkDocking guard missing")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate count = %d, want 2 (no fuse)", len(w.Crafts))
	}

	// Clear one craft's Landed flag — now the guard no longer applies
	// and the close/slow pair fuses (sanity that the guard isn't a
	// blanket exclusion of any landed craft).
	b.Landed = false
	if _, _, ok := w.checkDocking(); !ok {
		t.Error("one-landed co-located pair did not fuse — guard over-broad")
	}
	if len(w.Crafts) != 1 {
		t.Errorf("slate count = %d, want 1 (fused after clearing Landed)", len(w.Crafts))
	}
}

// TestDockingPreservesMomentum: composite velocity = mass-weighted
// average of the partners' velocities. Equal-mass craft moving in
// opposite directions should fuse to zero net velocity.
func TestDockingPreservesMomentum(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")

	a := w.Crafts[0]
	a.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	a.State.V = orbital.Vec3{Z: 0.05} // half the docking gate, +Z

	b := spacecraft.NewFromLoadout(spacecraft.LoadoutSIVB1ID) // same loadout = same mass
	b.Primary = *earth
	b.State = physics.StateVector{
		R: a.State.R.Add(orbital.Vec3{X: 5}),
		V: orbital.Vec3{Z: -0.05}, // -Z, equal magnitude
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)

	// |v_rel| = 0.10, right at the gate edge. Bump down slightly
	// so the gate fires.
	b.State.V.Z = -0.04
	if _, _, ok := w.checkDocking(); !ok {
		t.Fatalf("expected docking at borderline velocity")
	}
	composite := w.Crafts[0]
	// Equal masses, +0.05 and -0.04 → average = +0.005.
	if math.Abs(composite.State.V.Z-0.005) > 1e-9 {
		t.Errorf("composite Vz = %v, want 0.005 (mass-weighted average)", composite.State.V.Z)
	}
}

// TestUndockRestoresComponents: a docked composite split via Undock
// must produce two craft with the original identities, sharing the
// composite's pooled fuel + monoprop proportional to their pre-dock
// capacities, and separated enough not to immediately re-dock.
func TestUndockRestoresComponents(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")

	a := w.Crafts[0]
	a.Name = "Apollo"
	a.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := math.Sqrt(earth.GravitationalParameter() / a.State.R.Norm())
	a.State.V = orbital.Vec3{Y: v}

	b := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	b.Name = "LM"
	b.Primary = *earth
	b.State = physics.StateVector{
		R: a.State.R.Add(orbital.Vec3{X: 10}),
		V: a.State.V,
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)

	if _, _, ok := w.checkDocking(); !ok {
		t.Fatalf("expected dock to fire")
	}
	if len(w.Crafts) != 1 {
		t.Fatalf("expected 1 composite after dock, got %d", len(w.Crafts))
	}
	if got := len(w.Crafts[0].DockedComponents); got != 2 {
		t.Fatalf("composite has %d components, want 2", got)
	}

	if !w.Undock(0) {
		t.Fatal("Undock returned false on a composite")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("expected 2 craft after undock, got %d", len(w.Crafts))
	}
	names := []string{w.Crafts[0].Name, w.Crafts[1].Name}
	hasApollo, hasLM := false, false
	for _, n := range names {
		if n == "Apollo" {
			hasApollo = true
		}
		if n == "LM" {
			hasLM = true
		}
	}
	if !hasApollo || !hasLM {
		t.Errorf("undocked names = %v, want [Apollo, LM]", names)
	}
	// Components shouldn't immediately re-dock — separation must
	// be > DockingDistM.
	dr := w.Crafts[0].State.R.Sub(w.Crafts[1].State.R).Norm()
	if dr <= DockingDistM {
		t.Errorf("post-undock separation = %.1f m, want > %.1f m", dr, DockingDistM)
	}
}

// TestDockingActivePartnerKeepsName: when craft B (non-active)
// docks with craft A (active), the composite inherits A's name.
// When the player flies B and docks with A, B's name is kept
// (since B is the active partner from the player's perspective).
func TestDockingActivePartnerKeepsName(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	a := w.Crafts[0]
	a.Name = "Apollo"

	b := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	b.Name = "LM"
	b.Primary = *earth
	b.State = physics.StateVector{
		R: a.State.R.Add(orbital.Vec3{X: 10}),
		V: a.State.V,
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)
	w.ActiveCraftIdx = 1 // player flying the LM

	if _, _, ok := w.checkDocking(); !ok {
		t.Fatalf("expected docking")
	}
	composite := w.Crafts[w.ActiveCraftIdx]
	if composite.Name != "LM" {
		t.Errorf("composite name = %q, want LM (active partner)", composite.Name)
	}
}
