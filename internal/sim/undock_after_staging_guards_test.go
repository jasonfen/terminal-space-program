package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

func componentsTile(c *spacecraft.Spacecraft) (sum int, ok bool) {
	for _, dc := range c.DockedComponents {
		if len(dc.Stages) == 0 {
			return 0, false
		}
		sum += len(dc.Stages)
	}
	return sum, sum == len(c.Stages)
}

// `Y` after staging: each press still releases one comsat with its own relay
// antenna, and the carrier's breakdown keeps tiling its stack the whole way.
func TestDeployAfterStagingKeepsRelayAntennasAndTiling(t *testing.T) {
	w, c := spawnStarlinkDesign(t, "moon", 100e3)
	idx := w.ActiveCraftIdx
	if _, _, err := w.StageActive(idx); err != nil {
		t.Fatal(err)
	}
	for press := 1; press <= 2; press++ {
		before := len(w.Crafts)
		if !w.Deploy(idx) {
			t.Fatalf("Y press %d refused", press)
		}
		rel := w.Crafts[before]
		if rel.AntennaKind != spacecraft.AntennaRelay || rel.AntennaRangeM != spacecraft.AntennaRangeRelayCislunar {
			t.Errorf("press %d released %q with antenna %s/%.0e m, want relay/%.0e m",
				press, rel.Name, rel.AntennaKind, rel.AntennaRangeM, spacecraft.AntennaRangeRelayCislunar)
		}
		if _, ok := componentsTile(c); len(c.DockedComponents) > 0 && !ok {
			t.Errorf("after press %d the carrier's components do not tile its stack", press)
		}
	}
	// The third comsat comes off with its antenna too.
	if !w.Deploy(idx) {
		t.Fatal("Y press 3 refused")
	}
	if last := w.Crafts[len(w.Crafts)-1]; last.AntennaKind != spacecraft.AntennaRelay {
		t.Errorf("press 3 released %q with antenna %q, want relay", last.Name, last.AntennaKind)
	}
}

// A Dragon docked to a station, then its trunk dropped (space), then U: the
// capsule comes back crewed and the station keeps its probe core, antenna and
// docking port.
func TestUndockAfterDroppingDragonTrunkKeepsEveryPiecesParts(t *testing.T) {
	station := cargoStation(t, true)
	// A relay antenna the 1e7 m direct backfill would not give back.
	station.Stages[0].AntennaKind = spacecraft.AntennaRelay
	station.Stages[0].AntennaRangeM = spacecraft.AntennaRangeRelayCislunar
	station.SyncFields()
	w, c := cargoWorld(t, station, "Dragon")
	idx := w.ActiveCraftIdx
	stationParts := func(s []spacecraft.Stage) (cmd string, ant float64, ports int) {
		cm, _, ar, po := spacecraft.PartsOfStages(s)
		return cm, ar, len(po)
	}
	wantCmd, wantAnt, wantPorts := stationParts(station.Stages)
	if wantCmd == "" || wantPorts == 0 {
		t.Fatalf("fixture: station has command %q ports %d", wantCmd, wantPorts)
	}
	before := len(c.Stages)
	if _, _, err := w.StageActive(idx); err != nil {
		t.Fatalf("drop trunk: %v", err)
	}
	if len(c.Stages) != before-1 {
		t.Fatalf("staging dropped %d stages, want 1", before-len(c.Stages))
	}
	if _, ok := componentsTile(c); !ok {
		t.Errorf("after the trunk drop the components do not tile the stack")
	}
	if !w.Undock(idx) {
		t.Fatal("Undock refused")
	}
	var gotStation *spacecraft.Spacecraft
	for _, k := range w.Crafts {
		if k.Name == station.Name {
			gotStation = k
		}
	}
	if gotStation == nil {
		t.Fatal("station not released")
	}
	cm, ant, po := stationParts(gotStation.Stages)
	if cm != wantCmd || ant != wantAnt || po != wantPorts {
		t.Errorf("station after U: command %q antenna %.0e ports %d, want %q %.0e %d", cm, ant, po, wantCmd, wantAnt, wantPorts)
	}
	capsule := w.ActiveCraft()
	if !capsule.Crewed || !capsule.Controllable {
		t.Errorf("Dragon capsule after U: crewed=%v controllable=%v", capsule.Crewed, capsule.Controllable)
	}
	if !capsule.Stages[0].HasPort() {
		t.Errorf("Dragon capsule lost its docking port")
	}
}

// The flat fallback is still reachable (a composite whose breakdown the live
// stack outgrew, or a pre-breakdown flat record). It must not strip a piece's
// hardware.
func TestUndockLegacyFallbackCarriesRecordedParts(t *testing.T) {
	station := cargoStation(t, false)
	// A relay antenna the 1e7 m direct backfill would not give back.
	station.Stages[0].CommandSource = spacecraft.CommandProbe
	station.Stages[0].AntennaKind = spacecraft.AntennaRelay
	station.Stages[0].AntennaRangeM = spacecraft.AntennaRangeRelayCislunar
	station.SyncFields()
	w, c := cargoWorld(t, station, "Dragon")
	idx := w.ActiveCraftIdx
	// Make the live stack outgrow the record, which no bottom-drop repair can fix.
	extra := c.Stages[len(c.Stages)-1]
	extra.Name = "Extra"
	c.Stages = append(c.Stages, extra)
	if _, ok := componentsTile(c); ok {
		t.Fatal("fixture should not tile")
	}
	if !w.Undock(idx) {
		t.Fatal("Undock refused")
	}
	for _, k := range w.Crafts {
		if k.Name != station.Name {
			continue
		}
		if !k.Stages[0].HasPort() {
			t.Errorf("legacy fallback dropped the station's docking port")
		}
		if k.Stages[0].CommandSource != spacecraft.CommandProbe {
			t.Errorf("legacy fallback dropped the station's command source: %q", k.Stages[0].CommandSource)
		}
		if k.Stages[0].AntennaKind != spacecraft.AntennaRelay || k.Stages[0].AntennaRangeM != spacecraft.AntennaRangeRelayCislunar {
			t.Errorf("legacy fallback gave the station antenna %s/%.0e m, want relay/%.0e m",
				k.Stages[0].AntennaKind, k.Stages[0].AntennaRangeM, spacecraft.AntennaRangeRelayCislunar)
		}
		return
	}
	t.Fatal("station not released")
}

// A flat pre-breakdown record (no per-stage detail) takes its ports from the
// catalog entry of its loadout (#683).
func TestUndockFlatRecordTakesCatalogPorts(t *testing.T) {
	station := cargoStation(t, false)
	w, c := cargoWorld(t, station, "Dragon")
	idx := w.ActiveCraftIdx
	for i := range c.DockedComponents {
		c.DockedComponents[i].Stages = nil
	}
	want := spacecraft.LookupCatalogPorts(c.DockedComponents[0].LoadoutID, c.DockedComponents[0].Name)
	if len(want) == 0 {
		t.Skip("catalog lists no port for the Dragon lead; nothing to carry")
	}
	if !w.Undock(idx) {
		t.Fatal("Undock refused")
	}
	if got := w.Crafts[idx].Stages[0].Ports(); len(got) != len(want) {
		t.Errorf("flat-record lead ports = %+v, want %+v", got, want)
	}
}
