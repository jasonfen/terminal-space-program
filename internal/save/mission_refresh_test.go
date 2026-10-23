package save_test

import (
	"path/filepath"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// roundTrip lets edit rewrite the seeded world's missions, saves at the
// current schema, and loads the file back.
func roundTrip(t *testing.T, edit func(ms []missions.Mission) []missions.Mission) []missions.Mission {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Missions = edit(w.Missions)
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := save.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return got.Missions
}

func mission(t *testing.T, ms []missions.Mission, id string) *missions.Mission {
	t.Helper()
	for i := range ms {
		if ms[i].ID == id {
			return &ms[i]
		}
	}
	t.Fatalf("mission %q not found", id)
	return nil
}

func catalogMission(t *testing.T, id string) missions.Mission {
	t.Helper()
	cat, err := missions.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	return *mission(t, cat.Missions, id)
}

// Stale text, gate and fail_on in a saved catalog mission come back as the
// catalog's, at the current schema (no version gate involved).
func TestLoadRefreshesTextGateAndFailOnFromCatalog(t *testing.T) {
	cat := catalogMission(t, "tut-dock")
	catLaunch := catalogMission(t, "tut-launch")
	ms := roundTrip(t, func(ms []missions.Mission) []missions.Mission {
		d := mission(t, ms, "tut-dock")
		d.Description = "stale mission text"
		d.Objectives[3].Description = "Match up and dock the two vessels"
		l := mission(t, ms, "tut-launch")
		for i := range l.Objectives {
			if l.Objectives[i].Name == "Pitch east" {
				l.Objectives[i].Params.MinAltitudeM = 123
				l.Objectives[i].FailOn = []missions.FailCondition{"stale"}
			}
		}
		return ms
	})
	d := mission(t, ms, "tut-dock")
	if d.Description != cat.Description || d.Objectives[3].Description != cat.Objectives[3].Description {
		t.Errorf("tut-dock text not refreshed: %q / %q", d.Description, d.Objectives[3].Description)
	}
	l := mission(t, ms, "tut-launch")
	for i, o := range l.Objectives {
		if o.Name == "Pitch east" && (o.Params.MinAltitudeM != catLaunch.Objectives[i].Params.MinAltitudeM || len(o.FailOn) != len(catLaunch.Objectives[i].FailOn)) {
			t.Errorf("Pitch east gate/fail_on not refreshed: %+v", o)
		}
	}
}

// A mission the catalog does not know loads exactly as saved.
func TestLoadLeavesUnknownMissionVerbatim(t *testing.T) {
	custom := missions.Mission{ID: "user-x", Name: "Mine", Status: missions.InProgress,
		Objectives: []missions.Objective{{Name: "A", Kind: missions.KindEvent, Params: missions.Params{Action: "stage"}, Status: missions.Passed}}}
	ms := roundTrip(t, func(ms []missions.Mission) []missions.Mission { return append(ms, custom) })
	got := mission(t, ms, "user-x")
	if got.Name != "Mine" || got.Status != missions.InProgress || len(got.Objectives) != 1 ||
		got.Objectives[0].Status != missions.Passed || got.Objectives[0].Params.Action != "stage" {
		t.Errorf("custom mission changed: %+v", got)
	}
}

// Progress is carried by objective name and never granted or lost.
func TestLoadRefreshCarriesStatus(t *testing.T) {
	P, I, F := missions.Passed, missions.InProgress, missions.Failed
	ms := roundTrip(t, func(ms []missions.Mission) []missions.Mission {
		// tut-dock: first two passed, third in progress, 4th unevaluated.
		d := mission(t, ms, "tut-dock")
		d.Status = I
		for i, s := range []missions.Status{P, P, I, I} {
			d.Objectives[i].Status = s
		}
		// Saved objective the catalog renamed away: its Passed must not
		// leak onto anything; a catalog objective the save never had
		// (inserted before a Passed one) is treated as already done.
		d.Objectives[0].Name = "Old name"
		d.Objectives = append(d.Objectives, missions.Objective{Name: "Retired", Kind: missions.KindEvent, Status: P})
		// chal-dock: mission already complete with a stale shape.
		c := mission(t, ms, "chal-dock")
		c.Status = P
		c.Objectives = c.Objectives[:1]
		c.Objectives[0].Status = P
		// tut-launch: failed objective keeps Failed, mission Failed.
		l := mission(t, ms, "tut-launch")
		l.Status = F
		l.Objectives[3].Status = F
		return ms
	})
	d := mission(t, ms, "tut-dock")
	cat := catalogMission(t, "tut-dock")
	if len(d.Objectives) != len(cat.Objectives) {
		t.Fatalf("objective count %d, want catalog's %d (Retired must be dropped)", len(d.Objectives), len(cat.Objectives))
	}
	want := []missions.Status{I, P, I, I} // renamed first rung is NOT carried, nothing later... see below
	// First rung: name not in save, but a later rung (Target it) is Passed,
	// so ordered evaluation implies it was done: Passed.
	want[0] = P
	for i, o := range d.Objectives {
		if o.Status != want[i] {
			t.Errorf("tut-dock %q = %v, want %v", o.Name, o.Status, want[i])
		}
	}
	if d.Status != I {
		t.Errorf("tut-dock status %v, want InProgress", d.Status)
	}
	c := mission(t, ms, "chal-dock")
	if c.Status != P {
		t.Errorf("completed mission status %v, want Passed", c.Status)
	}
	for _, o := range c.Objectives {
		if o.Status != P {
			t.Errorf("completed mission rung %q = %v, want Passed", o.Name, o.Status)
		}
	}
	l := mission(t, ms, "tut-launch")
	if l.Status != F || l.Objectives[3].Status != F {
		t.Errorf("failed state lost: mission %v rung %v", l.Status, l.Objectives[3].Status)
	}
}

// An open rung must not be granted just because a name is new: with nothing
// Passed after it, a catalog objective the save never had stays open.
func TestLoadRefreshNewObjectiveStaysOpenWhenNothingLaterPassed(t *testing.T) {
	I, P := missions.InProgress, missions.Passed
	ms := roundTrip(t, func(ms []missions.Mission) []missions.Mission {
		d := mission(t, ms, "tut-dock")
		d.Status = I
		d.Objectives[0].Status = P
		d.Objectives[1].Status = I
		d.Objectives[2].Name = "Old name"
		d.Objectives[2].Status = I
		return ms
	})
	d := mission(t, ms, "tut-dock")
	if d.Objectives[2].Status == P || d.Objectives[3].Status == P {
		t.Errorf("unearned Passed: %+v", d.Objectives)
	}
}
