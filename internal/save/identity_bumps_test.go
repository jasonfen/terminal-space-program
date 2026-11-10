package save

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Schema bumps with no transform (the chain skips them): each one only adds
// omitempty keys whose absent value is the truth, and exists so an older
// binary refuses the file. TestIdentityBumpsLoadUnchanged pins that a save
// declaring the previous schema, carrying every such key, loads to the same
// payload it was written from. Why each bump happened: the SchemaVersion doc.
func TestIdentityBumpsLoadUnchanged(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:x", CraftID: 7, PrimaryID: c.Primary.ID}}
	w.SetTargetGhost("SHA256:x", 7) // v10
	c.HeadingTrim = 1.25            // v11
	c.RendezvousPlan = &spacecraft.RendezvousPlan{
		NodeID: 7, TriggerTime: w.Clock.SimTime.Add(time.Minute),
		ArrivalTime: w.Clock.SimTime.Add(time.Hour), TargetCraftID: 3, Fired: true, // v14
	}
	zero := 0.0
	c.BoosterThrottle = &zero                         // v16
	w.NameCounters = map[string]uint64{"Probe": 4}    // v17
	c.BatchID, c.BatchLead = 9, true                  // v17
	c.LiftoffSimTime = w.Clock.SimTime.Add(time.Hour) // v18
	w.GameFolder = "2026-10-27-1843"                  // v19
	hold := spacecraft.BurnPortIn                     // v20: a docked piece's pre-latch hold
	c.DockedComponents = []spacecraft.DockedComponent{{Name: "Core"}, {Name: "Tug", AttitudeMode: &hold}}
	c.NewestSeam = 1 // v20: where the block docked last begins
	w.Sites = []sim.Site{{ID: 3, Body: "Moon", System: "Sol", LatDeg: 1.5, LonDeg: -2.5, Kind: sim.SiteWreck, Name: "Probe-1 wreck", CraftID: 1}} // v21
	w.NextSiteID = 3

	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	want := readFileForTest(t, path)
	if want.Payload.Crafts[0].BatchID != 9 || want.Payload.Crafts[0].LiftoffSimTimeNano == 0 || want.Payload.GameFolder == "" || want.Payload.NameCounters == nil ||
		want.Payload.Crafts[0].RendezvousPlan == nil || want.Payload.Crafts[0].BoosterThrottle == nil ||
		want.Payload.Crafts[0].HeadingTrim == 0 || want.Payload.Crafts[0].Target == nil ||
		want.Payload.Crafts[0].DockedComponents[1].AttitudeMode == nil || want.Payload.Crafts[0].NewestSeam == 0 || len(want.Payload.Sites) != 1 {
		t.Fatalf("setup: a bump's key is missing: batch=%v counters=%v plan=%v booster=%v trim=%v target=%v", want.Payload.Crafts[0].BatchID, want.Payload.NameCounters, want.Payload.Crafts[0].RendezvousPlan, want.Payload.Crafts[0].BoosterThrottle, want.Payload.Crafts[0].HeadingTrim, want.Payload.Crafts[0].Target)
	}

	// Zeroing a field drops its omitempty key: the save an older binary wrote.
	stripBump := map[int]func(p *Payload){
		10: func(p *Payload) { p.Target, p.Crafts[0].Target = nil, nil },
		11: func(p *Payload) { p.Crafts[0].HeadingTrim = 0 },
		14: func(p *Payload) { p.Crafts[0].RendezvousPlan = nil },
		16: func(p *Payload) { p.Crafts[0].BoosterThrottle = nil },
		17: func(p *Payload) { p.NameCounters, p.Crafts[0].BatchID, p.Crafts[0].BatchLead = nil, 0, false },
		18: func(p *Payload) { p.Crafts[0].LiftoffSimTimeNano = 0 },
		19: func(p *Payload) { p.GameFolder = "" },
		20: func(p *Payload) { p.Crafts[0].DockedComponents[1].AttitudeMode, p.Crafts[0].NewestSeam = nil, 0 },
		21: func(p *Payload) { p.Sites, p.NextSiteID = nil, 0 },
	}
	for _, bump := range []int{10, 11, 14, 16, 17, 18, 19, 20, 21} {
		roundTrip(t, want, bump, "keys present")
		absent := want
		absent.Payload = Payload{}
		b, _ := json.Marshal(want.Payload)
		if err := json.Unmarshal(b, &absent.Payload); err != nil {
			t.Fatal(err)
		}
		stripBump[bump](&absent.Payload)
		roundTrip(t, absent, bump, "keys absent")
	}
}

// roundTrip writes want declared as the schema before bump, loads it, saves
// it again, and requires the same payload back at SchemaVersion.
func roundTrip(t *testing.T, want File, bump int, what string) {
	t.Helper()
	f := want
	f.Version = bump - 1
	out, _ := json.Marshal(f)
	old := filepath.Join(t.TempDir(), "old.json")
	if err := os.WriteFile(old, out, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(old)
	if err != nil {
		t.Fatalf("v%d -> v%d (%s): Load: %v", bump-1, bump, what, err)
	}
	again := filepath.Join(t.TempDir(), "again.json")
	if err := Save(loaded, again); err != nil {
		t.Fatal(err)
	}
	got := readFileForTest(t, again)
	if got.Version != SchemaVersion {
		t.Errorf("v%d -> v%d (%s): lands at v%d, want v%d", bump-1, bump, what, got.Version, SchemaVersion)
	}
	if !reflect.DeepEqual(got.Payload, want.Payload) {
		a, _ := json.Marshal(got.Payload)
		b, _ := json.Marshal(want.Payload)
		t.Errorf("v%d -> v%d (%s) changed the payload:\n got  %s\n want %s", bump-1, bump, what, a, b)
	}
}

func readFileForTest(t *testing.T, path string) File {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	return f
}

// ADR 0063 §7: a pre-latch hold of Prograde (0) survives the wire as present,
// distinct from an absent record (nil, read as Inertial), including through the
// cross-player transfer wire (additive JSON).
func TestDockedComponentHoldRoundTripsPresenceNotJustValue(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	prograde := spacecraft.BurnPrograde
	c.DockedComponents = []spacecraft.DockedComponent{{Name: "Core", AttitudeMode: &prograde}, {Name: "Tug"}, {Name: "Top"}}
	c.NewestSeam = 2
	got, err := CraftFromWire(CraftToWireForTransfer(c), w.Systems)
	if err != nil {
		t.Fatal(err)
	}
	dcs := got.DockedComponents
	if got.NewestSeam != 2 {
		t.Errorf("NewestSeam came back %d, want 2", got.NewestSeam)
	}
	if dcs[0].AttitudeMode == nil || *dcs[0].AttitudeMode != spacecraft.BurnPrograde {
		t.Errorf("Prograde record came back %v, want present Prograde", dcs[0].AttitudeMode)
	}
	if dcs[1].AttitudeMode != nil {
		t.Errorf("absent record came back %v, want nil", *dcs[1].AttitudeMode)
	}
}

// ADR 0066 guard 16: a v21 save with sites round-trips them and a target aimed
// at one; a v20 save (no sites keys) loads with none.
func TestSitesAndSiteTargetRoundTrip(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	c.Landed = true
	for _, k := range []sim.SiteKind{sim.SiteFlag, sim.SiteWreck} {
		w.Sites = append(w.Sites, sim.Site{ID: uint64(len(w.Sites) + 1), Body: "Earth", System: "Sol", LatDeg: 28.5 + float64(k),
			LonDeg: -80.6, Kind: k, Name: c.Name + " site", CraftID: c.ID, PlantedAtNano: 111, UpdatedAtNano: 222})
	}
	w.NextSiteID = 2
	w.SetTargetSite(2)
	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Sites, w.Sites) || got.NextSiteID != 2 {
		t.Errorf("sites came back %+v next %d, want %+v next 2", got.Sites, got.NextSiteID, w.Sites)
	}
	if got.Target.Kind != sim.TargetSite || got.Target.SiteID != 2 || got.ActiveCraft().Target.SiteID != 2 {
		t.Errorf("site target came back %+v, want site 2", got.Target)
	}

	// ADR 0068 guard 7: the shipped zones are in no save's bytes, yet a target
	// on one survives a reload (the id resolves against the Go-side table).
	raw, _ := os.ReadFile(path)
	for _, zone := range []string{"LC-13", "Kern LZ", "Tranquility Base", "Sinus Medii"} {
		if bytes.Contains(raw, []byte(zone)) {
			t.Errorf("save bytes carry the shipped zone %q", zone)
		}
	}
	zoneID := uint64(1)<<62 + 1
	w.SetTargetSite(zoneID)
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	if back, err := Load(path); err != nil || back.Target.SiteID != zoneID || !back.TargetSiteResolves() {
		t.Errorf("a shipped-zone target did not survive: %v %+v", err, back.Target)
	}
	w.SetTargetSite(2)
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}

	f := readFileForTest(t, path)
	f.Version, f.Payload.Sites, f.Payload.NextSiteID = 20, nil, 0
	f.Payload.Crafts[0].Target = nil
	out, _ := json.Marshal(f)
	old := filepath.Join(t.TempDir(), "v20.json")
	if err := os.WriteFile(old, out, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(old)
	if err != nil {
		t.Fatalf("v20 load: %v", err)
	}
	if len(loaded.Sites) != 0 || loaded.Target.Kind != sim.TargetNone {
		t.Errorf("v20 save loaded with sites %+v target %+v, want none", loaded.Sites, loaded.Target)
	}
}
