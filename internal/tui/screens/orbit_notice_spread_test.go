package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// spreadNoticeWorld is a released Falcon 9 + Starlink x24 at 500 km, linked,
// with the carrier active. Returns the world, the batch id and the carrier's slate index.
func spreadNoticeWorld(t *testing.T) (*sim.World, uint64, int) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: "Falcon-9-Starlink-24", ParentBodyID: "earth", AltitudeM: 500e3}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	idx := w.ActiveCraftIdx
	c := w.Crafts[idx]
	c.ReconcileDockedComponents()
	c.CurrentAttitudeDir, c.AttitudeMode = c.State.R.Unit(), spacecraft.BurnRadialOut
	if !w.ReleaseBatch(idx) {
		t.Fatal("ReleaseBatch refused")
	}
	w.EnsureCraftIDs()
	w.CommGraph = &sim.CommGraph{Connected: map[uint64]bool{}}
	for _, x := range w.Crafts {
		w.CommGraph.Connected[x.ID] = true
	}
	return w, w.Crafts[idx].BatchID, idx
}

func spreadFrame(v *OrbitView, w *sim.World) string {
	return ansi.Strip(v.Render(w, 0, 140, 40))
}

// The SPREAD notice at 140x40 (ADR 0059 decision 5): counts and the next start,
// derived from the members' orders, from the carrier and from a member; gone
// when no member has an order, under F2 declutter, and on an unrelated vessel.
func TestSpreadNotice(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, id, lead := spreadNoticeWorld(t)

	if f := spreadFrame(v, w); strings.Contains(f, "SPREAD") {
		t.Fatalf("a notice before any spread:\n%s", f)
	}
	if err := w.SpreadBatch(id, 550e3); err != nil {
		t.Fatal(err)
	}
	f := spreadFrame(v, w)
	for _, want := range []string{"SPREAD", "Starlink x24 → 550 km", "arrived 0 · climbing 1 · waiting 23", "next: Starlink-"} {
		if !strings.Contains(f, want) {
			t.Errorf("carrier frame missing %q:\n%s", want, f)
		}
	}

	members := w.SpreadMembers(id)
	w.SetActiveCraftIdx(members[5])
	if f := spreadFrame(v, w); !strings.Contains(f, "arrived 0 · climbing 1 · waiting 23") {
		t.Errorf("a member's frame lacks the notice:\n%s", f)
	}

	w.SetActiveCraftIdx(lead)
	v.SetDeclutter(true)
	if f := spreadFrame(v, w); strings.Contains(f, "SPREAD") {
		t.Errorf("declutter left the notice up:\n%s", f)
	}
	v.SetDeclutter(false)

	// A member stopped by hand is counted, not lost; the next row goes with the last wait.
	w.StopRaise(members[3])
	if f := spreadFrame(v, w); !strings.Contains(f, "waiting 22 · stopped 1") {
		t.Errorf("a stopped member is not counted:\n%s", f)
	}
	w.StopSpread(id)
	f = spreadFrame(v, w)
	if !strings.Contains(f, "climbing 1 · waiting 0") || strings.Contains(f, "next:") {
		t.Errorf("after Stop the spread:\n%s", f)
	}

	for _, i := range members {
		w.Crafts[i].Raise = nil
	}
	if f := spreadFrame(v, w); strings.Contains(f, "SPREAD") {
		t.Errorf("the notice outlived every order:\n%s", f)
	}
}

// coverageRung makes the lunar coverage rung current: every other mission
// passed, the far-side rung in progress at its deploy step.
func coverageRung(t *testing.T, w *sim.World) *missions.Mission {
	t.Helper()
	var rung *missions.Mission
	for i := range w.Missions {
		m := &w.Missions[i]
		if m.ID == "chal-far-side-relay" {
			rung = m
			continue
		}
		m.Status = missions.Passed
	}
	if rung == nil {
		t.Fatal("no chal-far-side-relay in the catalog")
	}
	rung.Status = missions.InProgress
	return rung
}

// The COVERAGE rows (ADR 0065 decision 6, guard 12): the three second-row
// forms from a synthetic lap, equal cases included. 432 x 60 s = 7h12m, 240
// samples = 4h.
func TestCoverageSecondRow(t *testing.T) {
	lap := func(from, n int) []bool {
		c := make([]bool, 1801)
		for i := range c {
			c[i] = i < from || i >= from+n
		}
		return c
	}
	const r = "2 relays at the Moon"
	for _, tc := range []struct {
		name string
		cov  []bool
		now  int
		want string
	}{
		{"gap ahead", lap(432, 240), 0, "  gap in 7h12m, lasts 4h · " + r},
		{"gap starts this sample", lap(432, 240), 432, "  gap now, 4h left · " + r},
		{"last gap sample", lap(432, 240), 671, "  gap now, 1m left · " + r},
		{"gap over", lap(432, 240), 672, "  no gap this lap · " + r},
		{"gap now with minutes", lap(0, 130), 0, "  gap now, 2h10m left · " + r},
		{"no gap", lap(0, 0), 0, "  no gap this lap · " + r},
		{"gap through the lap end", lap(1790, 100), 0, "  gap in 29h50m, lasts 11m · " + r},
	} {
		if got := coverageSecondRow(tc.cov, tc.now, 60, r); got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
	// The widest form must fit the bay's 56 cells at 140x40.
	if got := coverageSecondRow(lap(1799, 1), 0, 60, "5 relays at the Moon"); len([]rune(got)) > 56 {
		t.Errorf("widest row is %d cells, bay holds 56: %q", len([]rune(got)), got)
	}
}

// The COVERAGE notice at 140x40: shown only while the rung's coverage
// objective is current, below SPREAD when both show, gone on pass and
// under F2 declutter.
func TestCoverageNotice(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(140, 40)
	w, id, _ := spreadNoticeWorld(t)

	if f := spreadFrame(v, w); strings.Contains(f, "COVERAGE") {
		t.Fatalf("a notice before the rung is current:\n%s", f)
	}
	rung := coverageRung(t, w)
	if f := spreadFrame(v, w); strings.Contains(f, "COVERAGE") {
		t.Fatalf("a notice while the deploy step is current:\n%s", f)
	}
	rung.Objectives[0].Status = missions.Passed
	f := spreadFrame(v, w)
	if !strings.Contains(f, "COVERAGE  Moon south pole  0h of 30h") || !strings.Contains(f, "0 relays at the Moon") {
		t.Fatalf("rung current, no notice:\n%s", f)
	}
	if err := w.SpreadBatch(id, 550e3); err != nil {
		t.Fatal(err)
	}
	f = spreadFrame(v, w)
	rowOf := func(s string) int {
		for i, l := range strings.Split(f, "\n") {
			if strings.Contains(l, s) {
				return i
			}
		}
		return -1
	}
	if sp, cv := rowOf("SPREAD"), rowOf("COVERAGE"); sp < 0 || cv < 0 || cv < sp {
		t.Errorf("COVERAGE row %d must sit below SPREAD row %d:\n%s", cv, sp, f)
	}
	v.SetDeclutter(true)
	if f := spreadFrame(v, w); strings.Contains(f, "COVERAGE") {
		t.Errorf("declutter left the notice up:\n%s", f)
	}
	v.SetDeclutter(false)
	rung.Objectives[1].Status = missions.Passed
	rung.Status = missions.Passed
	if f := spreadFrame(v, w); strings.Contains(f, "COVERAGE") {
		t.Errorf("the notice outlived the pass:\n%s", f)
	}
}
