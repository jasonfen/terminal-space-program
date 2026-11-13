package save

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// A vessel that went through a save keeps its hull: the sprite dimensions the
// catalog gave every stage, and so the nose and docking port ADR 0053 derives
// from them. Before this, a loaded vessel had a hull of zero rows (no sprite, a
// port 0.75 m behind its position, a ghost with no known shape).
func TestStageSpriteSurvivesTheWire(t *testing.T) {
	for _, id := range spacecraft.LoadoutOrder {
		c := spacecraft.NewFromLoadout(id)
		back := wireStagesToSim(simStagesToWire(c.Stages))
		if len(back) != len(c.Stages) {
			t.Fatalf("%s: %d stages back, want %d", id, len(back), len(c.Stages))
		}
		for i, want := range c.Stages {
			got := back[i]
			if got.LaunchSpriteRowsPx != want.LaunchSpriteRowsPx ||
				got.LaunchSpriteWidthPx != want.LaunchSpriteWidthPx ||
				got.LaunchSpriteColor != want.LaunchSpriteColor ||
				got.LaunchSpriteHasLegs != want.LaunchSpriteHasLegs ||
				got.DiameterM != want.DiameterM {
				t.Errorf("%s stage %d (%s): sprite %d x %d %q legs=%v diameter %v after the wire, want %d x %d %q legs=%v diameter %v",
					id, i, want.Name,
					got.LaunchSpriteRowsPx, got.LaunchSpriteWidthPx, got.LaunchSpriteColor, got.LaunchSpriteHasLegs, got.DiameterM,
					want.LaunchSpriteRowsPx, want.LaunchSpriteWidthPx, want.LaunchSpriteColor, want.LaunchSpriteHasLegs, want.DiameterM)
			}
		}
	}
}

// The port a docked partner aims at is the same one after a reload: nose,
// width and port offset of a loaded vessel equal the fresh vessel's.
func TestLoadedVesselKeepsItsPortGeometry(t *testing.T) {
	for _, id := range []string{"RCS-tug", "Relay-Tug", "Endpoint-Station", "Apollo-Stack"} {
		c := spacecraft.NewFromLoadout(id)
		c.CurrentAttitudeDir.X = 1
		want := sim.VesselPortGeometry(c)
		loaded := *c
		loaded.Stages = wireStagesToSim(simStagesToWire(c.Stages))
		got := sim.VesselPortGeometry(&loaded)
		if got.NoseM != want.NoseM || got.WidthM != want.WidthM || len(got.Ports) != len(want.Ports) {
			t.Errorf("%s: loaded hull %v x %v ports %d, fresh %v x %v ports %d",
				id, got.NoseM, got.WidthM, len(got.Ports), want.NoseM, want.WidthM, len(want.Ports))
			continue
		}
		for i := range want.Ports {
			if got.Ports[i].OffsetM != want.Ports[i].OffsetM {
				t.Errorf("%s: loaded port offset %v, fresh %v", id, got.Ports[i].OffsetM, want.Ports[i].OffsetM)
			}
		}
	}
}

// A stage the catalog does not know (a parts-built VAB stage) gets the
// dimensions its mass derives, the rule it was built with, rather than none.
func TestUnknownStageGetsDerivedSprite(t *testing.T) {
	custom := spacecraft.Stage{Name: "My Booster", DryMass: 20000, FuelMass: 100000, FuelCapacity: 100000, Thrust: 1e6, Isp: 300}
	got := wireStagesToSim(simStagesToWire([]spacecraft.Stage{custom}))[0]
	if got.LaunchSpriteRowsPx <= 0 || got.LaunchSpriteWidthPx <= 0 {
		t.Errorf("custom stage came back with %d x %d sprite, want the mass-derived hull", got.LaunchSpriteRowsPx, got.LaunchSpriteWidthPx)
	}
	if got.DiameterM != 0 {
		t.Errorf("a parts-built stage restored a diameter of %v, want 0 (the sprite rule)", got.DiameterM)
	}
	empty := wireStagesToSim(simStagesToWire([]spacecraft.Stage{{Name: "Nothing"}}))
	if len(empty) == 1 && empty[0].LaunchSpriteRowsPx != 0 {
		t.Errorf("a massless stage was given a hull: %d rows", empty[0].LaunchSpriteRowsPx)
	}
}

// Catalog modules carry no loadout (a vessel built in the VAB from catalog
// parts, a jettisoned module): they come back by name.
func TestCatalogModuleSpriteSurvivesTheWire(t *testing.T) {
	n := 0
	for id := range spacecraft.StageCatalog {
		want, ok := spacecraft.BuildStage(id)
		if !ok || want.LaunchSpriteRowsPx == 0 {
			continue
		}
		back := wireStagesToSim(simStagesToWire([]spacecraft.Stage{want}))[0]
		if back.LaunchSpriteRowsPx != want.LaunchSpriteRowsPx || back.LaunchSpriteWidthPx != want.LaunchSpriteWidthPx || back.DiameterM != want.DiameterM {
			t.Errorf("part %s (%s): %d x %d diameter %v after the wire, want %d x %d diameter %v", id, want.Name,
				back.LaunchSpriteRowsPx, back.LaunchSpriteWidthPx, back.DiameterM, want.LaunchSpriteRowsPx, want.LaunchSpriteWidthPx, want.DiameterM)
		}
		n++
	}
	if n == 0 {
		t.Fatal("no catalog parts checked")
	}
}

// ADR 0069 decision 10 (S5): the one-burn landing mark survives the wire stage
// by stage for every shipped loadout, and a save written before it existed
// (no landing_throttle key) loads every stage unmarked, with no schema bump.
func TestLandingThrottleSurvivesTheWireAndOldSavesLoadOff(t *testing.T) {
	var marked int
	for _, id := range spacecraft.LoadoutOrder {
		stages := spacecraft.NewFromLoadout(id).Stages
		back := wireStagesToSim(simStagesToWire(stages))
		for i, want := range stages {
			if want.LandingThrottle > 0 {
				marked++
			}
			if back[i].SuperThrust != want.SuperThrust || back[i].SuperIsp != want.SuperIsp {
				t.Errorf("%s stage %d: SuperDraco %v/%v after the wire, want %v/%v", id, i, back[i].SuperThrust, back[i].SuperIsp, want.SuperThrust, want.SuperIsp)
			}
			if back[i].LandingThrottle != want.LandingThrottle {
				t.Errorf("%s stage %d (%s): landing throttle %v after the wire, want %v", id, i, want.Name, back[i].LandingThrottle, want.LandingThrottle)
			}
		}
	}
	if marked == 0 {
		t.Fatal("positive control: no shipped loadout carries the mark, the loop would prove nothing")
	}
	var old []Stage
	if err := json.Unmarshal([]byte(`[{"Name":"F9-S1","thrust":7607000,"can_soft_land":true}]`), &old); err != nil {
		t.Fatal(err)
	}
	if got := wireStagesToSim(old)[0].LandingThrottle; got != 0 {
		t.Errorf("a stage saved without the key loads with landing throttle %v, want 0 (off)", got)
	}
}

// ADR 0069 decision 11 build note (ADR 0064 review F1): a dropped Falcon Heavy
// side booster is one column of the pair, 3 px, and reloads at 3, not the
// pair's 6. The equal case: the whole pair, still in the stack, stays 6.
func TestBoosterHalfSpriteWidthSurvivesTheWire(t *testing.T) {
	fh := spacecraft.NewFromLoadout("Falcon-Heavy")
	pair := fh.Stages[0]
	if pair.LaunchSpriteWidthPx != 6 {
		t.Fatalf("setup: Side Boosters width = %d, want 6", pair.LaunchSpriteWidthPx)
	}
	if got := wireStagesToSim(simStagesToWire([]spacecraft.Stage{pair}))[0].LaunchSpriteWidthPx; got != 6 {
		t.Errorf("the whole pair reloads at %d px, want 6", got)
	}
	half := pair
	half.Thrust /= 2 // what sim.splitBoosterStage does to the stage
	half.LaunchSpriteWidthPx = 3
	if got := wireStagesToSim(simStagesToWire([]spacecraft.Stage{half}))[0].LaunchSpriteWidthPx; got != 3 {
		t.Errorf("a dropped half reloads at %d px, want 3", got)
	}
}

// ADR 0070: the save keeps only the live Thrust and Isp; a loaded stage gets
// its sea-level / vacuum pair and vacuum thrust back from the catalog, so the
// engine goes on following the air after a reload (no schema bump).
func TestStageEngineSurvivesTheWire(t *testing.T) {
	checked := 0
	for _, id := range spacecraft.LoadoutOrder {
		c := spacecraft.NewFromLoadout(id)
		c.SetAmbient(0.3) // a vessel saved partway up
		back := wireStagesToSim(simStagesToWire(c.Stages))
		for i, want := range c.Stages {
			if want.IspSL == want.IspVac {
				continue
			}
			checked++
			got := back[i]
			if got.IspSL != want.IspSL || got.IspVac != want.IspVac || math.Abs(got.ThrustVac-want.ThrustVac) > 1e-9*want.ThrustVac {
				t.Errorf("%s stage %d (%s): engine %v/%v vac thrust %v after the wire, want %v/%v %v",
					id, i, want.Name, got.IspSL, got.IspVac, got.ThrustVac, want.IspSL, want.IspVac, want.ThrustVac)
			}
		}
	}
	if checked < 20 {
		t.Fatalf("only %d pressure-sensitive stages checked: the loop is not reaching the catalog", checked)
	}
}
