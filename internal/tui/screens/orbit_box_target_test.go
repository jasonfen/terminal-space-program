// ADR 0051 slice 2a, decision 9 rule C: TARGET's encounter half must
// withhold for a Landed or Crashed ACTIVE craft, using `!Landed &&
// !Crashed` — not the old craftHasOrbit, which the audit found true for
// co-rotation wreckage (C32). This is the box's load-bearing rule.

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestTargetBoxNoTargetIsAllDashes: decision 2/11 — with no target, the
// title and every cell read a dash rather than the box vanishing.
func TestTargetBoxNoTargetIsAllDashes(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.Target = sim.Target{Kind: sim.TargetNone}
	lines := v.buildTargetBox(w)
	if len(lines) != 5 {
		t.Fatalf("buildTargetBox returned %d lines, want 5 (title + 4 rows)", len(lines))
	}
	if !strings.Contains(lines[0], "TARGET") || !strings.Contains(lines[0], "—") {
		t.Errorf("title with no target = %q, want TARGET  —", lines[0])
	}
	for i, row := range lines[1:] {
		if !strings.Contains(row, "—") {
			t.Errorf("row %d with no target = %q, want dash cells", i+1, row)
		}
	}
}

// TestTargetBoxRuleCWithholdsEncounterHalfWhenCrashed: a Crashed active
// craft (co-rotation wreckage, audit C32) must not show a closing/rel/
// lead/TCA/approach reading — craftHasOrbit reads true for this state,
// so this test would pass vacuously under the OLD predicate; it only
// passes because rule C explicitly checks Crashed.
func TestTargetBoxRuleCWithholdsEncounterHalfWhenCrashed(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = false
	c.Crashed = true
	// A crashed craft's stale co-rotation-like state still reads
	// craftHasOrbit == true today (audit C32); confirm that premise so
	// this test is actually exercising rule C's OWN Crashed check, not
	// merely re-testing something craftHasOrbit already handled.
	if !craftHasOrbit(c) {
		t.Fatal("setup: expected craftHasOrbit to read true for this crashed craft (audit C32's own premise)")
	}
	// A real vessel target with a non-zero relative state, so the
	// encounter cells would print REAL numbers if rule C didn't withhold
	// them — without this, the row reads a dash for the unrelated reason
	// that nothing is targeted at all, and the test proves nothing.
	targetCopy := *c
	targetCopy.ID = c.ID + 1
	targetCopy.Name = "target vessel"
	targetCopy.Landed = false
	targetCopy.Crashed = false
	targetCopy.State.R.X += 5000
	w.Crafts = append(w.Crafts, &targetCopy)
	w.SetTargetCraft(len(w.Crafts) - 1)
	if w.Target.Kind != sim.TargetCraft {
		t.Fatalf("setup: SetTargetCraft did not set a craft target: %+v", w.Target)
	}

	lines := v.buildTargetBox(w)
	rangeRow := lines[1]
	if !strings.HasSuffix(strings.TrimRight(rangeRow, " "), "—") {
		t.Errorf("range row for a Crashed active craft = %q, want rel: — as the trailing cell (rule C withholds the encounter half)", rangeRow)
	}
	tcaRow := lines[4]
	if !strings.HasSuffix(strings.TrimRight(tcaRow, " "), "—") {
		t.Errorf("TCA/approach row for a Crashed active craft = %q, both must be dashed", tcaRow)
	}
}

// TestTargetBoxOwnOrbitalShapeSurvivesWhileLanded: rule C withholds the
// ENCOUNTER half only — the target's own Ap/Pe/incl/Δincl describe the
// target, not the active craft's trajectory, and must still show while
// the active craft is Landed.
func TestTargetBoxOwnOrbitalShapeSurvivesWhileLanded(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed = true
	w.Target = sim.Target{Kind: sim.TargetBody, BodyIdx: 1}
	lines := v.buildTargetBox(w)
	shapeRow := lines[2] // Ap/Pe/incl row
	if strings.Count(shapeRow, "—") == 3 {
		t.Errorf("target orbital-shape row while Landed = %q, want the target's own Ap/Pe/incl still populated (only the encounter half is withheld)", shapeRow)
	}
}
