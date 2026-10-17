package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// cargoStation builds a one-stage station design: a probe core, a Docking Port
// and (when withHold) a Cargo Hold, the way a VAB design composes it.
func cargoStation(t *testing.T, withHold bool) *spacecraft.Spacecraft {
	t.Helper()
	ids := []string{"probe-core", "docking-port"}
	if withHold {
		ids = append(ids, "cargo-hold")
	}
	st, warn := spacecraft.ComposeStage(ids, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	st.Name = "Depot"
	c := spacecraft.NewFromStages([]spacecraft.Stage{st})
	c.Name = "Depot-1"
	return c
}

// cargoWorld puts a Cargo-Dragon (slot 0, active) and a station (slot 1) in the
// same orbit and docks them, so the slate is one composite led by the Dragon.
func cargoWorld(t *testing.T, station *spacecraft.Spacecraft, dragonID string) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	earth := w.Systems[0].FindBody("Earth")
	dragon := spacecraft.NewFromLoadout(dragonID)
	for _, c := range []*spacecraft.Spacecraft{dragon, station} {
		c.Primary = *earth
		r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
		c.State = physics.StateVector{R: r, V: orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}}
		c.State.M = c.TotalMass()
	}
	w.Crafts = []*spacecraft.Spacecraft{dragon, station}
	w.ActiveCraftIdx = 0
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("DockCrafts left %d vessels, want 1", len(w.Crafts))
	}
	w.RecomputeCommGraph()
	return w, w.Crafts[0]
}

func sumCargo(c *spacecraft.Spacecraft) (cargo, hold float64) {
	for _, st := range c.Stages {
		cargo += st.CargoKg
		hold += st.HoldKg
	}
	return
}

// ADR 0057 5.4 guard 1: u moves every cargo load into the hold, mass conserved.
func TestHandOverCargoMovesCargoIntoHold(t *testing.T) {
	w, c := cargoWorld(t, cargoStation(t, true), "Cargo-Dragon")
	before := c.TotalMass()
	kg, refusal := w.HandOverCargo(w.ActiveCraftIdx)
	if refusal != "" {
		t.Fatalf("refused: %q", refusal)
	}
	if kg != 3307 {
		t.Errorf("handed over %g kg, want 3307", kg)
	}
	cargo, hold := sumCargo(c)
	if cargo != 0 || hold != 3307 {
		t.Errorf("after: cargo %g hold %g, want 0 / 3307", cargo, hold)
	}
	if c.Cargo != 3307 {
		t.Errorf("flat Cargo mirror = %g, want 3307 (hold cargo is still mass)", c.Cargo)
	}
	if math.Abs(c.TotalMass()-before) > 1e-6 || math.Abs(c.State.M-before) > 1e-6 {
		t.Errorf("mass %g / State.M %g, want conserved at %g", c.TotalMass(), c.State.M, before)
	}
	// The hold lands on the stage that has the flag.
	for i, st := range c.Stages {
		if (st.HoldKg > 0) != st.CargoHold {
			t.Errorf("stage %d (%s): HoldKg %g with CargoHold %v", i, st.Name, st.HoldKg, st.CargoHold)
		}
	}
	if kg2, ref := w.HandOverCargo(w.ActiveCraftIdx); kg2 != 0 || ref != "no cargo aboard" {
		t.Errorf("second u = %g %q, want 0 / no cargo aboard", kg2, ref)
	}
}

// Guard 2: after Undock the Dragon leaves empty-handed and the station keeps it.
func TestUndockKeepsHandedOverCargo(t *testing.T) {
	w, _ := cargoWorld(t, cargoStation(t, true), "Cargo-Dragon")
	if _, ref := w.HandOverCargo(w.ActiveCraftIdx); ref != "" {
		t.Fatal(ref)
	}
	if !w.Undock(w.ActiveCraftIdx) {
		t.Fatalf("undock refused: %q", w.UndockRefusal(w.ActiveCraftIdx))
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("%d vessels after undock, want 2", len(w.Crafts))
	}
	for _, c := range w.Crafts {
		cargo, hold := sumCargo(c)
		switch c.Name {
		case "Depot-1":
			if hold != 3307 || cargo != 0 {
				t.Errorf("station: cargo %g hold %g, want 0 / 3307", cargo, hold)
			}
			if c.Cargo != 3307 {
				t.Errorf("station flat Cargo = %g, want 3307", c.Cargo)
			}
		default:
			if cargo != 0 || hold != 0 || c.Cargo != 0 {
				t.Errorf("%s left with cargo %g hold %g Cargo %g, want 0", c.Name, cargo, hold, c.Cargo)
			}
		}
	}
}

// Guard 3: a stack with no hold refuses, and nothing moves.
func TestHandOverCargoNoHoldRefuses(t *testing.T) {
	w, c := cargoWorld(t, cargoStation(t, false), "Cargo-Dragon")
	if kg, ref := w.HandOverCargo(w.ActiveCraftIdx); kg != 0 || ref != "no hold aboard" {
		t.Errorf("= %g %q, want 0 / no hold aboard", kg, ref)
	}
	if cargo, hold := sumCargo(c); cargo != 3307 || hold != 0 {
		t.Errorf("refusal moved cargo: cargo %g hold %g", cargo, hold)
	}
}

// Guard 4: not docked.
func TestHandOverCargoNotDocked(t *testing.T) {
	w := mustWorld(t)
	w.Crafts = nil
	w.ActiveCraftIdx = -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Cargo-Dragon", ParentBodyID: "earth", AltitudeM: 500_000})
	if err != nil {
		t.Fatal(err)
	}
	if kg, ref := w.HandOverCargo(w.ActiveCraftIdx); kg != 0 || ref != "not docked" {
		t.Errorf("= %g %q, want 0 / not docked", kg, ref)
	}
	if c.Cargo != 3307 {
		t.Errorf("a refused hand-over changed Cargo to %g", c.Cargo)
	}
	if _, ref := w.HandOverCargo(-1); ref != "not docked" {
		t.Errorf("no vessel: %q", ref)
	}
}

// Guard 5: a crewed Dragon carries the trunk's 800 kg and hands that over.
func TestHandOverCargoCrewedDragonTrunk(t *testing.T) {
	w, c := cargoWorld(t, cargoStation(t, true), "Dragon")
	kg, ref := w.HandOverCargo(w.ActiveCraftIdx)
	if ref != "" || kg != 800 {
		t.Fatalf("= %g %q, want 800", kg, ref)
	}
	if _, hold := sumCargo(c); hold != 800 {
		t.Errorf("hold = %g, want 800", hold)
	}
}

// A hold with nothing to hand over, and a Dragon with no hold above or below,
// both read the fact: a bare hold station docked to an Apollo with no cargo.
func TestHandOverCargoNoCargoAboard(t *testing.T) {
	w, c := cargoWorld(t, cargoStation(t, true), "Cargo-Dragon")
	for i := range c.Stages {
		c.Stages[i].CargoKg = 0
	}
	c.SyncFields()
	if kg, ref := w.HandOverCargo(w.ActiveCraftIdx); kg != 0 || ref != "no cargo aboard" {
		t.Errorf("= %g %q, want 0 / no cargo aboard", kg, ref)
	}
}

// Not verified 4 (ADR 0057): a modern composite never takes the legacy
// single-stage Undock path, because every component records its stages and
// they tile the live stack. Cargo numbers live on stages and would be lost
// through the legacy rebuild.
func TestModernCompositeRecordsStageBreakdown(t *testing.T) {
	_, c := cargoWorld(t, cargoStation(t, true), "Cargo-Dragon")
	var sum int
	for _, dc := range c.DockedComponents {
		if len(dc.Stages) == 0 {
			t.Fatalf("component %s records no stage breakdown", dc.Name)
		}
		sum += len(dc.Stages)
	}
	if sum != len(c.Stages) {
		t.Errorf("components tile %d stages, composite has %d", sum, len(c.Stages))
	}
}

// crossStack docks guest into the docker's World the way the serve layer does
// (DockGuestCraft) and returns the World with the one composite.
func crossStack(t *testing.T, docker, guest *spacecraft.Spacecraft) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	earth := w.Systems[0].FindBody("Earth")
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	for _, c := range []*spacecraft.Spacecraft{docker, guest} {
		c.Primary = *earth
		c.State = physics.StateVector{R: r, V: v, M: c.TotalMass()}
	}
	docker.ID, guest.ID = 7001, 7002
	w.Crafts = []*spacecraft.Spacecraft{docker}
	w.ActiveCraftIdx = 0
	comp, _, ok := w.DockGuestCraft(0, guest, guestFP)
	if !ok {
		t.Fatal("DockGuestCraft failed")
	}
	w.RecomputeCommGraph()
	return w, comp
}

// ADR 0057 5.5 + conflict 2a, case A: the Dragon's pilot docks (is the docker)
// to a friend's station. The Dragon's owner presses u on the owner seat; the
// cargo lands in the friend's hold and goes home with the friend's craft.
func TestCrossPlayerDragonOwnerHandsOverIntoGuestHold(t *testing.T) {
	w, comp := crossStack(t, spacecraft.NewFromLoadout("Cargo-Dragon"), cargoStation(t, true))
	before := comp.TotalMass()
	kg, ref := w.HandOverCargo(0)
	if ref != "" || kg != 3307 {
		t.Fatalf("owner-seat u = %g %q, want 3307", kg, ref)
	}
	if math.Abs(comp.TotalMass()-before) > 1e-6 {
		t.Errorf("mass %g -> %g", before, comp.TotalMass())
	}
	back, ok := w.UndockGuest(0, guestFP, 7002)
	if !ok {
		t.Fatal("UndockGuest refused")
	}
	if cargo, hold := sumCargo(back); cargo != 0 || hold != 3307 {
		t.Errorf("friend's station came home with cargo %g hold %g, want 0 / 3307", cargo, hold)
	}
	if cargo, hold := sumCargo(comp); cargo != 0 || hold != 0 {
		t.Errorf("Dragon stack kept cargo %g hold %g, want 0 / 0", cargo, hold)
	}
}

// Case B: the Dragon is the passive (guest) craft riding in the station
// owner's stack. The owner seat's own u has nothing of the owner's to move; the
// Dragon owner's press (relayed) moves it into the owner's hold, and the
// returned Dragon is empty-handed.
func TestCrossPlayerRiderDragonHandsOverIntoOwnersHold(t *testing.T) {
	w, comp := crossStack(t, cargoStation(t, true), spacecraft.NewFromLoadout("Cargo-Dragon"))
	if kg, ref := w.HandOverCargo(0); kg != 0 || ref != CargoNoCargoAbd {
		t.Fatalf("owner seat u on the friend's cargo = %g %q, want no cargo aboard", kg, ref)
	}
	if c, _ := sumCargo(comp); c != 3307 {
		t.Fatalf("the owner's press moved the guest's cargo: cargo now %g", c)
	}
	before := comp.TotalMass()
	kg, ref := w.HandOverGuestCargo(0, guestFP, 7002)
	if ref != "" || kg != 3307 {
		t.Fatalf("relayed u = %g %q, want 3307", kg, ref)
	}
	if math.Abs(comp.TotalMass()-before) > 1e-6 {
		t.Errorf("mass %g -> %g", before, comp.TotalMass())
	}
	if _, hold := sumCargo(comp); hold != 3307 {
		t.Errorf("owner's stack hold = %g, want 3307", hold)
	}
	back, ok := w.UndockGuest(0, guestFP, 7002)
	if !ok {
		t.Fatal("UndockGuest refused")
	}
	if cargo, hold := sumCargo(back); cargo != 0 || hold != 0 || back.Cargo != 0 {
		t.Errorf("returned Dragon cargo %g hold %g Cargo %g, want 0", cargo, hold, back.Cargo)
	}
	if _, hold := sumCargo(w.Crafts[0]); hold != 3307 {
		t.Errorf("owner's stack after undock hold = %g, want 3307", hold)
	}
}

// The rider's press can never move the owner's cargo: a station-owner rider
// (the Dragon is the owner's) has nothing aboard of theirs.
func TestRiderCannotMoveOwnersCargo(t *testing.T) {
	w, comp := crossStack(t, spacecraft.NewFromLoadout("Cargo-Dragon"), cargoStation(t, true))
	if kg, ref := w.HandOverGuestCargo(0, guestFP, 7002); kg != 0 || ref != CargoNoCargoAbd {
		t.Errorf("rider u on the owner's cargo = %g %q, want no cargo aboard", kg, ref)
	}
	if c, _ := sumCargo(comp); c != 3307 {
		t.Errorf("cargo moved: %g", c)
	}
}

// A hand-over is a command (ADR 0057 "Settled here" 8): an uncrewed composite
// out of contact refuses with the NO SIGNAL transient and moves nothing.
func TestHandOverCargoNeedsSignal(t *testing.T) {
	w, c := cargoWorld(t, cargoStation(t, true), "Cargo-Dragon")
	if c.Crewed {
		t.Fatal("fixture should be uncrewed")
	}
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{}} // force "no connection"
	kg, ref := w.HandOverCargo(w.ActiveCraftIdx)
	if kg != 0 || ref != "NO SIGNAL" {
		t.Errorf("= %g %q, want 0 / NO SIGNAL", kg, ref)
	}
	if _, ok := w.CommBlockedFlash(); !ok {
		t.Error("the blocked command must raise the NO SIGNAL flash")
	}
	if cargo, hold := sumCargo(c); cargo != 3307 || hold != 0 {
		t.Errorf("a refused hand-over moved cargo: %g / %g", cargo, hold)
	}
}
