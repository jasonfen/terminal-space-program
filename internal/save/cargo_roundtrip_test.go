package save

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0057 5.6 guard 1: CargoKg, HoldKg and CargoHold survive the wire stage by
// stage, for every loadout and for a composed hold stage.
func TestCargoFieldsSurviveTheWire(t *testing.T) {
	hold, warn := spacecraft.ComposeStage([]string{"probe-core", "cargo-hold"}, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	hold.HoldKg = 1234.5
	hold.CargoKg = 77
	cases := map[string][]spacecraft.Stage{"composed hold": {hold}}
	var loaded int
	for _, id := range spacecraft.LoadoutOrder {
		c := spacecraft.NewFromLoadout(id)
		cases[id] = c.Stages
		for _, s := range c.Stages {
			if s.CargoKg > 0 {
				loaded++
			}
		}
	}
	if loaded == 0 {
		t.Fatal("positive control: no shipped loadout carries cargo, the loop would prove nothing")
	}
	for name, stages := range cases {
		back := wireStagesToSim(simStagesToWire(stages))
		if len(back) != len(stages) {
			t.Fatalf("%s: %d stages back, want %d", name, len(back), len(stages))
		}
		for i, want := range stages {
			got := back[i]
			if got.CargoKg != want.CargoKg || got.HoldKg != want.HoldKg || got.CargoHold != want.CargoHold {
				t.Errorf("%s stage %d (%s): cargo %g hold %g flag %v after the wire, want %g / %g / %v",
					name, i, want.Name, got.CargoKg, got.HoldKg, got.CargoHold, want.CargoKg, want.HoldKg, want.CargoHold)
			}
		}
	}
}

func cargoStack(t *testing.T) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	earth := w.Systems[0].FindBody("Earth")
	st, warn := spacecraft.ComposeStage([]string{"probe-core", "docking-port", "cargo-hold"}, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	st.Name = "Depot"
	station := spacecraft.NewFromStages([]spacecraft.Stage{st})
	station.Name = "Depot-1"
	dragon := spacecraft.NewFromLoadout("Cargo-Dragon")
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	for _, c := range []*spacecraft.Spacecraft{dragon, station} {
		c.Primary = *earth
		c.State = physics.StateVector{R: r, V: v, M: c.TotalMass()}
	}
	w.Crafts = []*spacecraft.Spacecraft{dragon, station}
	w.ActiveCraftIdx = 0
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("dock left %d vessels", len(w.Crafts))
	}
	return w, w.Crafts[0]
}

func sums(c *spacecraft.Spacecraft) (cargo, hold float64) {
	for _, s := range c.Stages {
		cargo += s.CargoKg
		hold += s.HoldKg
	}
	return
}

// 5.6 guard 3: a save mid-dock after a hand-over loads with the hold's kg on
// the hold stage and 0 on the Dragon's; the loaded stack then undocks into the
// right hands and the right numbers.
func TestSaveMidDockAfterHandOverKeepsHold(t *testing.T) {
	w, c := cargoStack(t)
	if kg, ref := w.HandOverCargo(0); ref != "" || kg != 3307 {
		t.Fatalf("hand-over = %g %q", kg, ref)
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	lc := got.Crafts[0]
	if cargo, hold := sums(lc); cargo != 0 || hold != 3307 {
		t.Errorf("loaded stack cargo %g hold %g, want 0 / 3307", cargo, hold)
	}
	if math.Abs(lc.TotalMass()-c.TotalMass()) > 1e-6 {
		t.Errorf("loaded mass %g, saved %g", lc.TotalMass(), c.TotalMass())
	}
	var holdStages int
	for _, s := range lc.Stages {
		if s.CargoHold {
			holdStages++
			if s.HoldKg != 3307 {
				t.Errorf("hold stage HoldKg = %g, want 3307", s.HoldKg)
			}
		} else if s.HoldKg != 0 || s.CargoKg != 0 {
			t.Errorf("stage %s carries cargo %g hold %g after the hand-over", s.Name, s.CargoKg, s.HoldKg)
		}
	}
	if holdStages != 1 {
		t.Errorf("%d hold stages after load, want 1", holdStages)
	}
	if !got.Undock(0) {
		t.Fatal("loaded stack will not undock")
	}
	for _, v := range got.Crafts {
		cargo, hold := sums(v)
		if v.Name == "Depot-1" && hold != 3307 {
			t.Errorf("station hold after load+undock = %g", hold)
		}
		if v.Name != "Depot-1" && (cargo != 0 || hold != 0) {
			t.Errorf("%s left with cargo %g hold %g", v.Name, cargo, hold)
		}
	}
}

// 5.5 guard: the rider's seat. A guest Cargo Dragon hands over via the owner's
// World, is peeled out by UndockGuest, and the craft the guest's own World
// receives (CraftToWire, the ledger's parked-payload form) keeps Cargo 0 while
// the owner's stack, saved and loaded, keeps the hold.
func TestCrossPlayerHandOverSurvivesBothSeatsPersistence(t *testing.T) {
	owner, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	earth := owner.Systems[0].FindBody("Earth")
	st, _ := spacecraft.ComposeStage([]string{"probe-core", "docking-port", "cargo-hold"}, spacecraft.Components)
	station := spacecraft.NewFromStages([]spacecraft.Stage{st})
	station.Name = "Depot-1"
	station.ID = 8001
	guest := spacecraft.NewFromLoadout("Cargo-Dragon")
	guest.ID = 8002
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	for _, c := range []*spacecraft.Spacecraft{station, guest} {
		c.Primary = *earth
		c.State = physics.StateVector{R: r, V: v, M: c.TotalMass()}
	}
	owner.Crafts = []*spacecraft.Spacecraft{station}
	owner.ActiveCraftIdx = 0
	const fp = "SHA256:rider"
	if _, _, ok := owner.DockGuestCraft(0, guest, fp); !ok {
		t.Fatal("DockGuestCraft")
	}
	owner.RecomputeCommGraph()
	if kg, ref := owner.HandOverGuestCargo(0, fp, 8002); ref != "" || kg != 3307 {
		t.Fatalf("rider hand-over = %g %q", kg, ref)
	}
	// Owner seat persists mid-dock.
	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(owner, path); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cargo, hold := sums(reloaded.Crafts[0]); cargo != 0 || hold != 3307 {
		t.Errorf("owner seat reloaded cargo %g hold %g, want 0 / 3307", cargo, hold)
	}
	// Guest's craft goes home through the ledger's wire form.
	back, ok := reloaded.UndockGuest(0, fp, 8002)
	if !ok {
		t.Fatal("UndockGuest after reload")
	}
	wire := CraftToWire(back)
	home, err := CraftFromWire(wire, []bodies.System(owner.Systems))
	if err != nil {
		t.Fatal(err)
	}
	if cargo, hold := sums(home); cargo != 0 || hold != 0 || home.Cargo != 0 {
		t.Errorf("guest's Dragon came home with cargo %g hold %g Cargo %g, want 0", cargo, hold, home.Cargo)
	}
	if _, hold := sums(reloaded.Crafts[0]); hold != 3307 {
		t.Errorf("owner's stack after the guest left: hold %g, want 3307", hold)
	}
}

// 5.6 guard 2: a v14 envelope loads with zero cargo everywhere and re-saves
// without the keys (omitempty), so the additive fields leave old stages
// byte-identical.
func TestV14EnvelopeLoadsWithZeroCargoAndResavesWithoutKeys(t *testing.T) {
	raw, err := os.ReadFile("testdata/v14_main_872bc16.json")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.json")
	if err := os.WriteFile(in, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := Load(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range w.Crafts {
		if c.Cargo != 0 {
			t.Errorf("%s loaded with Cargo %g", c.Name, c.Cargo)
		}
		for _, s := range c.Stages {
			if s.CargoKg != 0 || s.HoldKg != 0 || s.CargoHold {
				t.Errorf("%s/%s loaded with cargo state", c.Name, s.Name)
			}
		}
	}
	out := filepath.Join(dir, "out.json")
	if err := Save(w, out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	for _, key := range []string{`"cargo_kg"`, `"hold_kg"`, `"cargo_hold"`} {
		if strings.Contains(string(b), key) {
			t.Errorf("re-saved v14 envelope contains %s", key)
		}
	}
	if !strings.Contains(string(b), `"dry_mass"`) {
		t.Fatal("positive control: re-saved file has no stage keys at all")
	}
}
