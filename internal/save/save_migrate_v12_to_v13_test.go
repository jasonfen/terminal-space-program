package save_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

const (
	v12StageRung  = "Stage to lift off"
	v12StageText  = "Press [space] to drop the clamps and light the engine."
	newLiftRung   = "Light the engine"
	newStagingRun = "Stage when the tank runs dry"
)

// v12OldLadder is tut-launch's objective list as schema v12 saves carry it
// (before #525): "Stage to lift off" on the stage action, no staging rung.
func v12OldLadder() []missions.Objective {
	return []missions.Objective{
		{Name: "Spawn on the pad", Kind: missions.KindEvent, Params: missions.Params{Action: "spawn_craft", RequireSpawnLocation: "pad"}},
		{Name: "Throttle up", Kind: missions.KindEvent, Params: missions.Params{Action: "throttle_full"}},
		{Name: v12StageRung, Description: v12StageText, Kind: missions.KindEvent, Params: missions.Params{Action: "stage"}},
		{Name: "Pitch east", Kind: missions.KindReachAltitude, Params: missions.Params{PrimaryID: "earth", MinAltitudeM: 10000}},
		{Name: "Plan the circularising burn", Kind: missions.KindEvent, Params: missions.Params{Action: "plan_circularize"}},
		{Name: "Fly the burn and make orbit", Kind: missions.KindCircularizeFromPad, Params: missions.Params{PrimaryID: "earth"}},
	}
}

// v12Fixture writes a real save, swaps tut-launch for the v12 ladder with
// the given statuses (by index into v12OldLadder), and stamps version 12.
// It asserts the old rung text is on disk so the fixture cannot degrade
// into a current-shape file.
func v12Fixture(t *testing.T, mStatus missions.Status, objStatus []missions.Status) string {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	found := false
	for i := range w.Missions {
		if w.Missions[i].ID != "tut-launch" {
			continue
		}
		found = true
		objs := v12OldLadder()
		for j, s := range objStatus {
			objs[j].Status = s
		}
		w.Missions[i].Objectives = objs
		w.Missions[i].Status = mStatus
	}
	if !found {
		t.Fatal("no tut-launch in the seeded world")
	}
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, _ := os.ReadFile(path)
	s := strings.Replace(string(raw), fmt.Sprintf(`"version": %d`, save.SchemaVersion), `"version": 12`, 1)
	if !strings.Contains(s, `"version": 12`) || !strings.Contains(s, v12StageText) {
		t.Fatalf("fixture is not a v12 save with the old rung")
	}
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func tutLaunch(t *testing.T, w *sim.World) missions.Mission {
	t.Helper()
	for _, m := range w.Missions {
		if m.ID == "tut-launch" {
			return m
		}
	}
	t.Fatal("tut-launch missing after load")
	return missions.Mission{}
}

func objByName(t *testing.T, m missions.Mission, name string) missions.Objective {
	t.Helper()
	for _, o := range m.Objectives {
		if o.Name == name {
			return o
		}
	}
	t.Fatalf("objective %q not in %v", name, m.Objectives)
	return missions.Objective{}
}

// A v12 save with tut-launch not started loads with the current ladder:
// the [b] lift-off rung and the new staging rung, no [space] clamps text.
func TestMigrateV12ToV13TutLaunchNotStarted(t *testing.T) {
	w, err := save.Load(v12Fixture(t, missions.InProgress, nil))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := tutLaunch(t, w)
	lift := objByName(t, m, newLiftRung)
	if !strings.Contains(lift.Description, "[b]") || lift.Params.Action != "ignite" {
		t.Errorf("lift-off rung = %+v", lift)
	}
	if objByName(t, m, newStagingRun).Params.Action != "stage" {
		t.Errorf("staging rung missing or wrong action")
	}
	for _, o := range m.Objectives {
		if strings.Contains(o.Description, "drop the clamps") || o.Name == v12StageRung {
			t.Errorf("stale rung survived: %+v", o)
		}
		if o.Status != missions.InProgress {
			t.Errorf("%q status = %v, want InProgress", o.Name, o.Status)
		}
	}
}

// In progress: rungs keep their status by name; the old lift-off rung's
// Passed carries to its replacement; the new staging rung stays open while
// the player has not passed anything beyond it.
func TestMigrateV12ToV13TutLaunchInProgressKeepsStatus(t *testing.T) {
	P, I := missions.Passed, missions.InProgress
	w, err := save.Load(v12Fixture(t, I, []missions.Status{P, P, P, I}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := tutLaunch(t, w)
	for name, want := range map[string]missions.Status{
		"Spawn on the pad": P, "Throttle up": P, newLiftRung: P,
		"Pitch east": I, newStagingRun: I,
	} {
		if got := objByName(t, m, name).Status; got != want {
			t.Errorf("%q = %v, want %v", name, got, want)
		}
	}
	if !strings.Contains(objByName(t, m, newLiftRung).Description, "[b]") {
		t.Errorf("lift-off rung text not refreshed")
	}
}

// Past the new rung already: a player who passed the circularising-burn
// plan has long since staged, so the new staging rung is not re-opened.
func TestMigrateV12ToV13TutLaunchPastStagingRung(t *testing.T) {
	P, I := missions.Passed, missions.InProgress
	w, err := save.Load(v12Fixture(t, I, []missions.Status{P, P, P, P, P, I}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := tutLaunch(t, w)
	if got := objByName(t, m, newStagingRun).Status; got != P {
		t.Errorf("staging rung = %v, want Passed (player is past it)", got)
	}
	if got := objByName(t, m, "Fly the burn and make orbit").Status; got != I {
		t.Errorf("final rung = %v, want InProgress", got)
	}
}

// A Passed mission stays Passed, every rung included.
func TestMigrateV12ToV13TutLaunchPassedStaysPassed(t *testing.T) {
	P := missions.Passed
	w, err := save.Load(v12Fixture(t, P, []missions.Status{P, P, P, P, P, P}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	m := tutLaunch(t, w)
	if m.Status != P {
		t.Errorf("mission = %v, want Passed", m.Status)
	}
	for _, o := range m.Objectives {
		if o.Status != P {
			t.Errorf("%q = %v, want Passed", o.Name, o.Status)
		}
	}
}

func TestSchemaVersionBumpedToV13(t *testing.T) {
	if save.SchemaVersion < 13 {
		t.Errorf("SchemaVersion = %d, want >= 13", save.SchemaVersion)
	}
}
