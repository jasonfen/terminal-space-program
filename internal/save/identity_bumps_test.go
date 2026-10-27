package save

import (
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

	path := filepath.Join(t.TempDir(), "save.json")
	if err := Save(w, path); err != nil {
		t.Fatal(err)
	}
	want := readFileForTest(t, path)
	if want.Payload.Crafts[0].BatchID != 9 || want.Payload.Crafts[0].LiftoffSimTimeNano == 0 || want.Payload.NameCounters == nil ||
		want.Payload.Crafts[0].RendezvousPlan == nil || want.Payload.Crafts[0].BoosterThrottle == nil ||
		want.Payload.Crafts[0].HeadingTrim == 0 || want.Payload.Crafts[0].Target == nil {
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
	}
	for _, bump := range []int{10, 11, 14, 16, 17, 18} {
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
