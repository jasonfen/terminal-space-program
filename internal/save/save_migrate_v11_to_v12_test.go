package save_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// v11Fixture writes a save whose planted Rendezvous Burn node uses the
// schema v11 spelling: version 11, the meeting_* keys and the "meeting-burn"
// advisory_key value. It starts from a real v12 save and rewrites the
// bytes, then asserts the old spelling is present and the new one is gone,
// so the fixture cannot silently degrade into a v12 file.
func v11Fixture(t *testing.T) string {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	base := w.Clock.SimTime
	w.ActiveCraft().Nodes = []sim.ManeuverNode{{
		TriggerTime:          base.Add(time.Minute),
		DV:                   10,
		Mode:                 spacecraft.BurnVector,
		AdvisoryKey:          sim.AdvisoryKeyRendezvousBurn,
		RendezvousArrivalSec: 8 * 3600,
		RendezvousOrbitLabel: "their orbit",
		RendezvousLaps:       5,
	}}
	w.EnsureNodeIDs()
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, r := range [][2]string{
		{`"rendezvous_arrival_sec"`, `"meeting_arrival_sec"`},
		{`"rendezvous_orbit_label"`, `"meeting_place_label"`},
		{`"rendezvous_laps"`, `"meeting_laps"`},
		{`"rendezvous-burn"`, `"meeting-burn"`},
		{`"version": 12`, `"version": 11`},
	} {
		if !strings.Contains(s, r[0]) {
			t.Fatalf("v12 save lacks %s; cannot build the v11 fixture:\n%s", r[0], s[:min(len(s), 400)])
		}
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	if strings.Contains(s, "rendezvous_") || strings.Contains(s, "rendezvous-burn") {
		t.Fatalf("v11 fixture still carries a v12 spelling")
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMigrateV11ToV12RendezvousBurn - a v11 save with a planted Rendezvous
// Burn node loads into v12 with every value intact and the advisory key
// renamed, and the re-saved file uses only the v12 spelling.
func TestMigrateV11ToV12RendezvousBurn(t *testing.T) {
	path := v11Fixture(t)
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load v11: %v", err)
	}
	nodes := got.ActiveCraft().Nodes
	if len(nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(nodes))
	}
	n := nodes[0]
	if n.RendezvousArrivalSec != 8*3600 || n.RendezvousOrbitLabel != "their orbit" || n.RendezvousLaps != 5 {
		t.Errorf("v11 values lost in migration: %+v", n)
	}
	if n.AdvisoryKey != sim.AdvisoryKeyRendezvousBurn {
		t.Errorf("AdvisoryKey = %q, want %q", n.AdvisoryKey, sim.AdvisoryKeyRendezvousBurn)
	}

	out := filepath.Join(t.TempDir(), "resave.json")
	if err := save.Save(got, out); err != nil {
		t.Fatalf("re-Save: %v", err)
	}
	raw, _ := os.ReadFile(out)
	if strings.Contains(string(raw), "meeting") {
		t.Errorf("re-saved v12 file still carries the old spelling:\n%s", raw)
	}
	again, err := save.Load(out)
	if err != nil {
		t.Fatalf("Load re-saved v12: %v", err)
	}
	if m := again.ActiveCraft().Nodes[0]; m.RendezvousArrivalSec != 8*3600 || m.RendezvousOrbitLabel != "their orbit" || m.RendezvousLaps != 5 || m.AdvisoryKey != sim.AdvisoryKeyRendezvousBurn {
		t.Errorf("v12 round-trip after migration lost values: %+v", m)
	}
}

// TestSchemaVersionBumpedToV12 pins the version number itself: a change
// here without a migration alongside it defeats the repo's bump rule.
func TestSchemaVersionBumpedToV12(t *testing.T) {
	if save.SchemaVersion != 12 {
		t.Errorf("SchemaVersion = %d, want 12", save.SchemaVersion)
	}
}

// TestMigrateV11ToV12ObjectiveName - an in-progress Meet & Dock challenge
// saved under v11 keeps its state but picks up the renamed objective.
func TestMigrateV11ToV12ObjectiveName(t *testing.T) {
	p := &save.Payload{Missions: []missions.Mission{{
		ID:         "x",
		Objectives: []missions.Objective{{Name: "Plant the meeting burn"}, {Name: "Target it"}},
	}}}
	save.MigrateV11PayloadToV12ForTest(p)
	if got := p.Missions[0].Objectives[0].Name; got != "Plant the rendezvous burn" {
		t.Errorf("objective name = %q", got)
	}
	if got := p.Missions[0].Objectives[1].Name; got != "Target it" {
		t.Errorf("unrelated objective renamed to %q", got)
	}
}
