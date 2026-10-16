package relay

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 decision 5: a kicked player's roster row is gone, so the
// handle map cannot name their ghost. The report carries the handle its
// reporter last knew, and the ghost falls back to it.
func TestGhostKeepsReportHandleWhenRosterHasNoRow(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	store := NewStore()
	rp := NewReporter(store, "SHA256:pilot2")
	rp.Handle = "pilot2"
	rp.Tick(w, time.Now())
	rep := store.Snapshot("someone-else")[0]
	if rep.Handle != "pilot2" {
		t.Fatalf("reporter did not stamp its handle on the report: %q", rep.Handle)
	}

	viewer, _ := sim.NewWorld()
	gs := GhostsFor(viewer, []CraftReport{rep}, map[string]string{}) // roster row gone
	if len(gs) == 0 {
		t.Fatal("no ghost built")
	}
	if gs[0].Handle != "pilot2" {
		t.Errorf("ghost handle = %q, want the report's last known %q", gs[0].Handle, "pilot2")
	}

	// The roster still wins when it has a row (a rename shows at once).
	gs = GhostsFor(viewer, []CraftReport{rep}, map[string]string{"SHA256:pilot2": "renamed"})
	if gs[0].Handle != "renamed" {
		t.Errorf("roster handle should win: got %q", gs[0].Handle)
	}

	// A report without a handle draws unnamed, as before.
	rep.Handle = ""
	gs = GhostsFor(viewer, []CraftReport{rep}, map[string]string{})
	if gs[0].Handle != "" {
		t.Errorf("handle-less report should draw unnamed, got %q", gs[0].Handle)
	}
}

// The wire change is additive: omitted when empty, round-trips when set.
func TestCraftReportHandleIsAdditive(t *testing.T) {
	b, _ := json.Marshal(CraftReport{Owner: "o", SubspaceTime: time.Unix(1e9, 0).UTC()})
	if strings.Contains(string(b), `"handle"`) {
		t.Errorf("empty handle should be omitted: %s", b)
	}
	b, _ = json.Marshal(CraftReport{Owner: "o", Handle: "pilot2", SubspaceTime: time.Unix(1e9, 0).UTC()})
	var out CraftReport
	if err := json.Unmarshal(b, &out); err != nil || out.Handle != "pilot2" {
		t.Errorf("handle did not round-trip: %v %+v", err, out)
	}
}
