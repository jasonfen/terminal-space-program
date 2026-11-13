package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// earthLike / moonLike are body fixtures for the surface-launch gate tests —
// real Earth / Moon mass + mean radius, so g = μ/R² is realistic (~9.8 / ~1.6).
func earthLike() bodies.CelestialBody {
	return bodies.CelestialBody{ID: "earth", EnglishName: "Earth",
		Mass: bodies.Mass{Value: 5.9722, Exponent: 24}, MeanRadius: 6371}
}
func moonLike() bodies.CelestialBody {
	return bodies.CelestialBody{ID: "moon", EnglishName: "Moon",
		Mass: bodies.Mass{Value: 7.342, Exponent: 22}, MeanRadius: 1737.4}
}

// superEarthLike is a heavy-gravity body (surface g ≈ 13 m/s²) where a Lumen
// launcher (Vector V, TWR ≈ 1.2) lifts off but the real Saturn V (TWR ≈ 0.93)
// cannot — used to exercise the [f]-toggle launch-gate re-validation.
func superEarthLike() bodies.CelestialBody {
	return bodies.CelestialBody{ID: "superearth", EnglishName: "SuperEarth",
		Mass: bodies.Mass{Value: 7.905, Exponent: 24}, MeanRadius: 6371}
}

func wetMass(l spacecraft.Loadout) float64 {
	return spacecraft.SumDryMass(l.Stages) + spacecraft.SumFuelMass(l.Stages)
}

// loadoutIndex returns a loadout's selectable cursor index in the form's
// (filtered) grouped order.
func loadoutIndex(t *testing.T, s *SpawnCraft, id string) int {
	t.Helper()
	for i, x := range s.orderedLoadoutIDs() {
		if x == id {
			return i
		}
	}
	t.Fatalf("loadout %q not found in grouped order", id)
	return -1
}

// TestCraftLiftsOff — the physics surface-launch gate (ADR 0031 / S9): a craft
// can be surface-spawned iff its bottom-stage TWR ≥ 1 on the body. A launcher
// lifts off Earth; an NTR-tug carrier and an engineless satellite don't; a
// Lander lifts off the Moon but not Earth (body-aware).
func TestCraftLiftsOff(t *testing.T) {
	earth, moon := earthLike(), moonLike()

	sat := spacecraft.LookupLoadout("Saturn-V")
	if !craftLiftsOff(sat.Thrust(), wetMass(sat), &earth) {
		t.Error("Saturn V should lift off Earth (TWR > 1)")
	}

	carrier := spacecraft.LookupLoadout("Comsat-Carrier-3")
	if craftLiftsOff(carrier.Thrust(), wetMass(carrier), &earth) {
		t.Error("Comsat Carrier (NTR tug + payloads) must NOT lift off Earth")
	}

	sat2 := spacecraft.LookupLoadout("Relay-Comsat") // engineless
	if craftLiftsOff(sat2.Thrust(), wetMass(sat2), &earth) {
		t.Error("engineless Relay Comsat must NOT lift off (thrust 0)")
	}

	lander := spacecraft.LookupLoadout("Lander")
	if !craftLiftsOff(lander.Thrust(), wetMass(lander), &moon) {
		t.Error("Lander should lift off the Moon (TWR > 1)")
	}
	if craftLiftsOff(lander.Thrust(), wetMass(lander), &earth) {
		t.Error("Lander must NOT lift off Earth (TWR < 1)")
	}

	// Degenerate guards.
	if craftLiftsOff(1e9, 1000, nil) {
		t.Error("nil body must read as can't-lift-off")
	}
}

// TestLaunchpadGateSkipsOrbitOnlyCraft — POSITION cycling never lands on
// launchpad for an orbit-only craft, but does for a launcher (ADR 0031 / S9).
func TestLaunchpadGateSkipsOrbitOnlyCraft(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil, "", "", nil)

	// Orbit-only: the Comsat Carrier can't lift off Earth.
	s.loadoutIdx = loadoutIndex(t, s, "Comsat-Carrier-3")
	if s.launchpadAllowed() {
		t.Fatal("Comsat Carrier should not allow launchpad on Earth")
	}
	s.fieldIdx = 1 // POSITION
	for i := 0; i < 12; i++ {
		s.HandleKey("right")
		if s.SelectedLaunchpad() {
			t.Fatalf("POSITION cycle landed on launchpad for an orbit-only craft (step %d)", i)
		}
	}

	// Launcher: Saturn V reaches launchpad within a couple of cycles.
	s.loadoutIdx = loadoutIndex(t, s, "Saturn-V")
	if !s.launchpadAllowed() {
		t.Fatal("Saturn V should allow launchpad on Earth")
	}
	reached := false
	for i := 0; i < 4; i++ {
		s.HandleKey("right")
		if s.SelectedLaunchpad() {
			reached = true
			break
		}
	}
	if !reached {
		t.Error("POSITION cycle never reached launchpad for a launch-capable craft")
	}
}

// TestLaunchpadNeverStickyOnOrbitOnlyCraft — once launchpad is selected on a
// launcher, walking the CRAFT TYPE list must never leave launchpad selected on
// a craft that can't lift off the parent: switching craft snaps it back to
// orbit (ADR 0031 / S9). Tests the invariant across the whole list.
func TestLaunchpadNeverStickyOnOrbitOnlyCraft(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil, "", "", nil)
	s.loadoutIdx = loadoutIndex(t, s, "Saturn-V")
	s.fieldIdx = 1
	for i := 0; i < 4 && !s.SelectedLaunchpad(); i++ {
		s.HandleKey("right")
	}
	if !s.SelectedLaunchpad() {
		t.Fatal("setup: could not select launchpad on Saturn V")
	}
	s.fieldIdx = 0 // CRAFT TYPE
	for i := 0; i < s.loadoutChoiceCount()+2; i++ {
		s.HandleKey("right")
		if s.SelectedLaunchpad() && !s.launchpadAllowed() {
			t.Fatalf("launchpad stuck on a craft that can't lift off: idx=%d id=%q",
				s.loadoutIdx, s.SelectedLoadoutID())
		}
	}
}

// TestFilterToggleResnapsLaunchpad — the [f] system-filter toggle re-validates
// the launch gate, like cycleField (ADR 0031 / S11 review fix): if the craft
// the cursor lands on after the toggle can't lift off the selected parent, a
// launchpad selection snaps back to orbit. On a heavy parent, the filtered
// Lumen top row (Vector V) lifts off but the show-all top row (Saturn V) does
// not, so toggling must drop launchpad.
func TestFilterToggleResnapsLaunchpad(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{superEarthLike()}, "superearth", nil, bodies.ScaleStrippedBack, "", nil)
	// Filtered (Lumen) top row is Vector V, which lifts off the heavy parent.
	if got := s.SelectedLoadoutID(); got != "Vector-V" {
		t.Fatalf("setup: filtered top row = %q, want Vector-V", got)
	}
	s.fieldIdx = 1
	for i := 0; i < 4 && !s.SelectedLaunchpad(); i++ {
		s.HandleKey("right")
	}
	if !s.SelectedLaunchpad() {
		t.Fatal("setup: could not select launchpad for Vector V on the heavy parent")
	}
	// Show-all top row becomes Saturn V (TWR < 1 here) → launchpad must drop.
	s.HandleKey("f")
	if s.SelectedLoadoutID() != "Saturn-V" {
		t.Fatalf("after [f], top row = %q, want Saturn-V (show-all)", s.SelectedLoadoutID())
	}
	if s.SelectedLaunchpad() {
		t.Error("[f] left launchpad selected on a craft that can't lift off the parent")
	}
}

// TestRenderShowsCrewTags — the rendered picker tags crewed and uncrewed craft
// (ADR 0031 / S9 render smoke).
func TestRenderShowsCrewTags(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil, "", "", nil)
	out := s.Render(120, 0)
	if !strings.Contains(out, "crewed") {
		t.Error("render missing a 'crewed' tag (e.g. Apollo Stack)")
	}
	if !strings.Contains(out, "uncrewed") {
		t.Error("render missing an 'uncrewed' tag")
	}
}

// padSpawn is the form on the Cape pad with the given craft, LC-13 offered as the
// LZ near the pad (a stub for the world lookup the App injects).
func padSpawn(t *testing.T, loadout string) *SpawnCraft {
	t.Helper()
	s := NewSpawnCraft(chipTestTheme())
	s.Reset([]bodies.CelestialBody{earthLike()}, "earth", nil, bodies.ScaleReal, "Sol", nil)
	s.SetRecoverLZ(func(_ string, lat, lon float64, _ int) (string, bool) { return "LC-13", lat > 28 && lat < 29 })
	s.loadoutIdx = loadoutIndex(t, s, loadout)
	s.posMode, s.latIdx = posLaunchpad, 1
	return s
}

// ADR 0069 decision 12: the pad card's recover: row. Offered on a pad spawn of a
// craft with a recoverable stage only; defaults to the LZ for the first stage and a
// ship for the next; ←/→ cycle LZ / ship / none, space moves between stages, enter on
// a ship types its km, and the choices come out indexed by stage for the spawn.
func TestPadCardRecoverRow(t *testing.T) {
	// not offered: orbit, a craft with no recoverable stage
	s := padSpawn(t, "Falcon-9")
	s.posMode = posOrbit
	if o := s.fieldOrder(); s.recoverOffered() || o[len(o)-1] == recoverFieldIdx {
		t.Error("the row is offered off the pad")
	}
	if s2 := padSpawn(t, "Saturn-V"); s2.recoverOffered() || s2.SelectedRecover() != nil {
		t.Error("Saturn V (nothing that can land) is offered a recovery")
	}
	// Falcon Heavy: boosters then core; defaults LZ and ship 250
	s = padSpawn(t, "Falcon-Heavy")
	order := s.fieldOrder()
	if order[len(order)-1] != recoverFieldIdx {
		t.Fatalf("field order %v, want the recover row last on the pad", order)
	}
	got := s.SelectedRecover()
	if len(got) != 2 || got[0].Mode != spacecraft.RecoverLZ || got[1].Mode != spacecraft.RecoverShip || got[1].ShipKm != 250 {
		t.Fatalf("defaults %+v, want boosters LZ and core ship 250 km", got)
	}
	lines := s.recoverLines()
	if len(lines) != 3 || !strings.Contains(lines[0], "boosters LZ") || !strings.Contains(lines[1], "core ship 250 km") || !strings.Contains(lines[2], "LZ: LC-13") {
		t.Errorf("row = %q, want boosters LZ / core ship 250 km / the LZ's name", lines)
	}
	// keys: focus the row, → on boosters goes LZ -> ship, space to the core, → ship -> none
	s.fieldIdx = recoverFieldIdx
	s.HandleKey("right")
	s.HandleKey(" ")
	s.HandleKey("right")
	if got := s.SelectedRecover(); got[0].Mode != spacecraft.RecoverShip || got[0].ShipKm != 250 || got[1].Mode != spacecraft.RecoverNone {
		t.Errorf("after right, space, right: %+v, want boosters ship 250 and core none", got)
	}
	// enter on a ship types its km; enter keeps; a digit past four is ignored; the next enter launches
	s.HandleKey(" ")
	if a := s.HandleKey("enter"); a != SpawnActionNone || !s.recEditing || !s.CapturingText() {
		t.Fatalf("enter on a ship: action %v editing %v, want the typing box open", a, s.recEditing)
	}
	for _, k := range []string{"4", "5", "0", "0", "9", "backspace"} {
		s.HandleKey(k)
	}
	if s.recInput != "450" {
		t.Errorf("typed %q, want 450 after four digits, a refused fifth and a backspace", s.recInput)
	}
	s.HandleKey("enter")
	if got := s.SelectedRecover(); got[0].ShipKm != 450 || s.recEditing {
		t.Errorf("committed %+v editing %v, want boosters ship 450", got, s.recEditing)
	}
	if a := s.HandleKey("enter"); a != SpawnActionConfirm {
		t.Errorf("the enter after the box launches: action %v", a)
	}
	// the cap, and no LZ choice without a site near the pad
	s.recEditing, s.recInput = true, "9999"
	s.HandleKey("enter")
	if got := s.SelectedRecover(); got[0].ShipKm != 3000 {
		t.Errorf("typed 9999 -> %v km, want the 3000 km cap", got[0].ShipKm)
	}
	s.SetRecoverLZ(func(string, float64, float64, int) (string, bool) { return "", false })
	if got := s.SelectedRecover(); got[0].Mode == spacecraft.RecoverLZ || got[1].Mode == spacecraft.RecoverLZ {
		t.Errorf("no LZ near the pad but the row offers one: %+v", got)
	}
	// 140 x 40: the row is on the card and every line of the frame fits
	s = padSpawn(t, "Falcon-Heavy")
	out := ansi.Strip(s.Render(140, 40))
	for _, want := range []string{"RECOVER", "recover:  boosters LZ", "core ship 250 km", "LZ: LC-13"} {
		if !strings.Contains(out, want) {
			t.Errorf("140x40 card lacks %q:\n%s", want, out)
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if w := lipgloss.Width(l); w > 140 {
			t.Errorf("a card line is %d cells at 140 wide: %q", w, l)
		}
	}
	// R2: the site is near but has one LZ free: a single stage still gets it, the
	// booster pair (two halves) does not, and the row says why
	s = padSpawn(t, "Falcon-Heavy")
	s.SetRecoverLZ(func(_ string, _, _ float64, need int) (string, bool) { return "LC-13", need <= 1 })
	if got := s.SelectedRecover(); got[0].Mode != spacecraft.RecoverNone || got[1].Mode != spacecraft.RecoverShip {
		t.Errorf("one LZ free: %+v, want the pair none and the core ship", got)
	}
	if out := ansi.Strip(s.Render(140, 40)); !strings.Contains(out, "boosters none") || !strings.Contains(out, "LC-13: no free LZ") {
		t.Errorf("140x40 card with a full site lacks the reason:\n%s", out)
	}
	if s := padSpawn(t, "Falcon-9"); s.SelectedRecover()[0].Mode != spacecraft.RecoverLZ {
		t.Error("a free site is not the default for a single stage")
	}
}
