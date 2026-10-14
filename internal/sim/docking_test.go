package sim

import (
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestDockingFusesCloseSlowCraft: a ported pair that holds the port gate
// (ADR 0053) is captured and fuses into one composite once the magnet
// completes. The composite keeps the lead's identity and position, and has
// the summed mass (its position is pinned by TestDockCompositeKeepsLeadPosition).
func TestDockingFusesCloseSlowCraft(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.4, 0.05, 2)
	w, a, b := p.w, p.a, p.b
	wantMass := a.TotalMass() + b.TotalMass()
	tickFor(w, 6)
	if len(w.Crafts) != 1 {
		t.Fatalf("expected 1 composite craft, got %d", len(w.Crafts))
	}
	composite := w.Crafts[0]
	if composite.Name != "Alpha" {
		t.Errorf("composite name = %q, want the active partner's name Alpha", composite.Name)
	}
	if math.Abs(composite.TotalMass()-wantMass) > 1e-3 {
		t.Errorf("composite mass = %v, want %v", composite.TotalMass(), wantMass)
	}
}

// TestDockingAutoFuseStampsPartnerNameAndComponentCount (#421): the dock must
// record the folded-in partner's name and the resulting composite's total
// Docked Component count on LastDockEvent. app.go's flash reads both to
// announce the fusion in the same voice Undock already uses. Captured inside
// DockCrafts (before the drop slot is gone), not reconstructed by the caller.
func TestDockingAutoFuseStampsPartnerNameAndComponentCount(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.4, 0.05, 2)
	w := p.w
	p.b.Name = "Tug-1"
	if w.LastDockEvent != nil {
		t.Fatal("precondition: LastDockEvent already set before any dock")
	}
	tickFor(w, 6)
	if w.LastDockEvent == nil {
		t.Fatal("the pair docked but nothing stamped LastDockEvent")
	}
	if w.LastDockEvent.PartnerName != "Tug-1" {
		t.Errorf("LastDockEvent.PartnerName = %q, want %q", w.LastDockEvent.PartnerName, "Tug-1")
	}
	if w.LastDockEvent.ComponentCount != 2 {
		t.Errorf("LastDockEvent.ComponentCount = %d, want 2 (two plain craft fused)", w.LastDockEvent.ComponentCount)
	}
	if w.LastDockEvent.CompositeName == "" {
		t.Error("LastDockEvent.CompositeName is empty")
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
		R: w.Crafts[0].State.R.Add(orbital.Vec3{X: 10}), // 10 m close
		V: w.Crafts[0].State.V.Add(orbital.Vec3{Y: 5}),  // 5 m/s relative
		M: b.TotalMass(),
	}
	w.Crafts = append(w.Crafts, b)

	if _, _, ok := w.checkDocking(); ok {
		t.Error("expected no docking with 5 m/s relative velocity")
	}
}

// TestDockingSkipsLandedCraft: v0.12 Slice 2 / ADR 0007 (broadened
// post-playtest). A pair sitting in the port gate must NOT start a capture
// while EITHER craft is Landed. This covers both the static surface-staged pair
// (both Landed) and the liftoff moment (ascent clears Landed while still
// co-located with the parked descent stage). Only once NEITHER is Landed does
// the pair begin to capture; that last step is also the positive control that
// shows the instrument can say yes.
func TestDockingSkipsLandedCraft(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "rcs-tug", 0.3, 0, 0)
	w, a, b := p.w, p.a, p.b

	a.Landed, b.Landed = true, true
	if startsMagnet(w) {
		t.Error("both-Landed pair began a capture, guard missing")
	}
	b.Landed = false // a stays Landed: the liftoff case
	if startsMagnet(w) {
		t.Error("one-Landed pair began a capture, either-Landed guard failed (liftoff re-fuse)")
	}
	a.Landed = false
	if !startsMagnet(w) {
		t.Error("neither-Landed pair in the gate did not begin a capture, guard over-broad")
	}
	if len(w.Crafts) != 2 {
		t.Errorf("slate count = %d, want 2 (a capture is under way, nothing fused yet)", len(w.Crafts))
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

	// |v_rel| = 0.09. The dock is a fixture here: this test is about the
	// fuse's momentum arithmetic, not the capture that precedes it.
	b.State.V.Z = -0.04
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("expected one composite")
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

	w.DockCrafts(0, 1) // fixture dock; the capture is covered in docking_port_test.go
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
	// Components part along the seam at the spring release, and neither
	// sits inside the docking gate straight away.
	dv := w.Crafts[0].State.V.Sub(w.Crafts[1].State.V).Norm()
	if math.Abs(dv-UndockSpringMS) > 1e-9 {
		t.Errorf("post-undock relative speed = %.4f m/s, want the %.2f m/s spring release", dv, UndockSpringMS)
	}
	if dockPairGateHolds(w.Crafts[0], w.Crafts[1]) {
		t.Error("the restored pair sits inside the docking gate straight after undock")
	}
}

// TestUndockRestoresMultiStageLMWithLiveFuel — v0.12 / ADR 0009. A
// composite whose DockedComponents carry a per-stage breakdown undocks
// into MULTI-stage craft, and the restored stages carry the composite's
// CURRENT (live) fuel, not the dock-time snapshot. The transposition
// flow is the headline case: after transposing, the SM (Stages[0]) is
// drained by the LOI burn while the LM rides docked — so on undock the
// surviving SM/CM core must reflect the burn, and the LM (never the
// firing stage while docked) must keep its descent/ascent propellant.
func TestUndockRestoresMultiStageLMWithLiveFuel(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	stack := spacecraft.NewFromLoadout(spacecraft.LoadoutApolloStackID)
	stack.Primary = w.Crafts[0].Primary
	stack.State = w.Crafts[0].State
	w.Crafts[0] = stack
	w.ActiveCraftIdx = 0

	for i := 0; i < 3; i++ {
		if _, _, err := w.StageActive(0); err != nil {
			t.Fatalf("decouple #%d: %v", i, err)
		}
	}
	if err := w.Transpose(0); err != nil {
		t.Fatalf("Transpose: %v", err)
	}
	comp := w.Crafts[0]
	if comp.Stages[0].Name != "SM" {
		t.Fatalf("post-transpose Stages[0] = %q, want SM", comp.Stages[0].Name)
	}
	const smStart = 16000.0
	if comp.Stages[0].FuelMass != smStart {
		t.Fatalf("SM start fuel = %.0f, want %.0f", comp.Stages[0].FuelMass, smStart)
	}

	// Burn SM propellant (the LOI burn analogue) — drains Stages[0].
	const burned = 5000.0
	if got := comp.BurnFuel(burned); got != burned {
		t.Fatalf("BurnFuel returned %.0f, want %.0f", got, burned)
	}

	if !w.Undock(0) {
		t.Fatal("Undock after transpose returned false")
	}
	var lm, csm *spacecraft.Spacecraft
	for _, c := range w.Crafts {
		switch {
		case len(c.Stages) == 2 && c.Stages[0].Name == "Descent":
			lm = c
		case len(c.Stages) == 2 && c.Stages[0].Name == "SM":
			csm = c
		}
	}
	if csm == nil || lm == nil {
		t.Fatalf("undock did not produce both an SM/CM core and a 2-stage LM (crafts=%d)", len(w.Crafts))
	}
	// The core's SM carries LIVE fuel (start − burned), not the stale
	// 16000 snapshot — this is the live-partition correctness point.
	if want := smStart - burned; csm.Stages[0].FuelMass != want {
		t.Errorf("restored SM fuel = %.0f, want %.0f (live, post-burn — NOT the snapshot)",
			csm.Stages[0].FuelMass, want)
	}
	// The LM never fired while docked → its tanks are intact at the
	// loadout trim (Descent 6310, Ascent 1269).
	if lm.Stages[0].FuelMass != 6310 || lm.Stages[1].FuelMass != 1269 {
		t.Errorf("restored LM fuel = [%.0f, %.0f], want [6310, 1269]",
			lm.Stages[0].FuelMass, lm.Stages[1].FuelMass)
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

	w.DockCrafts(0, 1) // fixture dock
	if len(w.Crafts) != 1 {
		t.Fatalf("expected one composite")
	}
	composite := w.Crafts[w.ActiveCraftIdx]
	if composite.Name != "LM" {
		t.Errorf("composite name = %q, want LM (active partner)", composite.Name)
	}
}

// TestUndockRefusalIsExhaustiveAndSpeaks (#308): every path on which Undock
// returns false hands back a reason, and a splittable composite hands back
// none. Live, a legitimate refusal on a cross-player stack was indistinguishable
// from a dead key and cost an evening of diagnosis; the pairing is what keeps
// `U` from ever no-opping in silence again.
func TestUndockRefusalIsExhaustiveAndSpeaks(t *testing.T) {
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	a := w.Crafts[0]
	a.State.R = orbital.Vec3{X: earth.RadiusMeters() + 500e3}

	// Out of range, and a plain (non-composite) craft.
	for _, idx := range []int{-1, len(w.Crafts)} {
		if got := w.UndockRefusal(idx); got == "" {
			t.Errorf("UndockRefusal(%d) on an invalid index gave no reason", idx)
		}
	}
	if got := w.UndockRefusal(0); got == "" {
		t.Error("UndockRefusal on a plain craft gave no reason")
	}

	// A cross-player stack: refused (the guest releases it from their seat),
	// and it must say so.
	guest := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	guest.Primary = *earth
	guest.ID = 77
	guest.State = physics.StateVector{R: a.State.R.Add(orbital.Vec3{X: 6}), V: a.State.V, M: guest.TotalMass()}
	if _, _, ok := w.DockGuestCraft(0, guest, "SHA256:someone"); !ok {
		t.Fatal("DockGuestCraft failed")
	}
	reason := w.UndockRefusal(0)
	if reason == "" {
		t.Fatal("UndockRefusal on a cross-player stack gave no reason")
	}
	if !strings.Contains(reason, "guest") {
		t.Errorf("cross-player refusal %q does not point at the guest's seat", reason)
	}
	if w.Undock(0) {
		t.Error("Undock split a cross-player stack locally")
	}

	// A same-player composite: no reason, and it splits. Without this the
	// exhaustiveness above would be satisfied by refusing everything.
	w2, _ := NewWorld()
	earth2 := w2.Systems[0].FindBody("Earth")
	lm := spacecraft.NewFromLoadout(spacecraft.LoadoutLanderID)
	lm.Primary = *earth2
	lm.State = physics.StateVector{
		R: w2.Crafts[0].State.R.Add(orbital.Vec3{X: 10}),
		V: w2.Crafts[0].State.V,
		M: lm.TotalMass(),
	}
	w2.Crafts = append(w2.Crafts, lm)
	w2.DockCrafts(0, 1) // fixture dock
	if len(w2.Crafts) != 1 {
		t.Fatal("fixture dock failed")
	}
	if got := w2.UndockRefusal(0); got != "" {
		t.Errorf("UndockRefusal on a splittable composite = %q, want none", got)
	}
	if !w2.Undock(0) {
		t.Error("Undock refused a composite it reported no reason to refuse")
	}
}
