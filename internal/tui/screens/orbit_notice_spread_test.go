package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

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
