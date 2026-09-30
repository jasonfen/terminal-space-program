package relay

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
)

// TestCraftReportDecodesLegacyMeetingKeys - a report from a build that
// still spoke rendezvous_meeting_place / rendezvous_meeting_laps decodes
// into the renamed fields.
func TestCraftReportDecodesLegacyMeetingKeys(t *testing.T) {
	legacy := `{"owner":"SHA256:bob","subspace_time":"2030-01-01T00:00:00Z","crafts":[],` +
		`"rendezvous_target":"SHA256:alice","rendezvous_meeting_place":"their orbit","rendezvous_meeting_laps":5}`
	var r CraftReport
	if err := json.Unmarshal([]byte(legacy), &r); err != nil {
		t.Fatalf("decode legacy: %v", err)
	}
	if r.RendezvousOrbit != "their orbit" || r.RendezvousLaps != 5 {
		t.Errorf("legacy keys lost: orbit=%q laps=%d", r.RendezvousOrbit, r.RendezvousLaps)
	}
	if r.Owner != "SHA256:bob" || r.RendezvousTarget != "SHA256:alice" {
		t.Errorf("other fields disturbed: %+v", r)
	}
}

// TestCraftReportRoundTripsNewKeys - the new keys encode and decode, and
// the legacy spelling is never written.
func TestCraftReportRoundTripsNewKeys(t *testing.T) {
	in := CraftReport{Owner: "SHA256:bob", SubspaceTime: time.Unix(1e9, 0).UTC(),
		RendezvousTarget: "SHA256:alice", RendezvousOrbit: "your orbit", RendezvousLaps: 3}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"rendezvous_orbit":"your orbit"`) || !strings.Contains(string(b), `"rendezvous_laps":3`) {
		t.Fatalf("new keys not written: %s", b)
	}
	if strings.Contains(string(b), "meeting") {
		t.Fatalf("legacy spelling written: %s", b)
	}
	var out CraftReport
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.RendezvousOrbit != "your orbit" || out.RendezvousLaps != 3 {
		t.Errorf("new keys lost: %+v", out)
	}
}

// TestCraftReportNewKeysWinOverLegacy - a payload carrying both spellings
// takes the new one.
func TestCraftReportNewKeysWinOverLegacy(t *testing.T) {
	var r CraftReport
	src := `{"rendezvous_orbit":"your orbit","rendezvous_laps":2,"rendezvous_meeting_place":"their orbit","rendezvous_meeting_laps":9}`
	if err := json.Unmarshal([]byte(src), &r); err != nil {
		t.Fatal(err)
	}
	if r.RendezvousOrbit != "your orbit" || r.RendezvousLaps != 2 {
		t.Errorf("legacy overrode new: %+v", r)
	}
}

// TestCraftFromWireFoldsParkedLegacyNodeKeys - session.json parks
// save.Craft payloads outside a versioned save envelope; one written by an
// older build carries meeting_* node keys and the "meeting-burn" advisory
// value. The relay's craftFromWire (the one decode point for those
// payloads) must fold them into the live node.
func TestCraftFromWireFoldsParkedLegacyNodeKeys(t *testing.T) {
	w := newWorld(t)
	wc := save.CraftToWire(w.ActiveCraft())
	src := `{"trigger_time_unix_nano":1,"mode":0,"dv":10,"primary_id":"",` +
		`"advisory_key":"meeting-burn","meeting_arrival_sec":28800,"meeting_place_label":"their orbit","meeting_laps":5}`
	var n save.Node
	if err := json.Unmarshal([]byte(src), &n); err != nil {
		t.Fatal(err)
	}
	wc.Nodes = []save.Node{n}
	c := craftFromWire(&wc, w.Systems)
	if c == nil || len(c.Nodes) != 1 {
		t.Fatalf("craftFromWire returned %v", c)
	}
	got := c.Nodes[0]
	if got.RendezvousArrivalSec != 28800 || got.RendezvousOrbitLabel != "their orbit" || got.RendezvousLaps != 5 || got.AdvisoryKey != "rendezvous-burn" {
		t.Errorf("legacy node keys not folded: %+v", got)
	}
}
