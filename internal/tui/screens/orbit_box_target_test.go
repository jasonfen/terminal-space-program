// ADR 0051 slice 2a, decision 9 rule C: TARGET's encounter half must
// withhold for a Landed or Crashed ACTIVE craft, using `!Landed &&
// !Crashed`, not the old craftHasOrbit, which the audit found true for
// co-rotation wreckage (C32). This is the box's load-bearing rule.

package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// TestTargetBoxNoTargetIsAllDashes tests buildTargetBox itself, not
// whether its output ever reaches the screen: the builder still
// produces the all-dash form on a no-target World, since callers that
// need its width for clearance math (e.g. LaunchView's
// airScaleColumnBound) call it unconditionally. Since #480, the caller
// that assembles the visible frame (navigationBoxesInOrder) no longer
// places this box at all when nothing is targeted, see
// TestTargetBoxAbsentWithNoTargetPresentWhenSet in
// orbit_target_absence_test.go for that rendered-frame guard, and the
// 2026-09-28 amendment to ADR 0051 in the planning vault for why this
// is safe for TARGET alone.
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
// lead/TCA/approach reading, craftHasOrbit reads true for this state,
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
	// them, without this, the row reads a dash for the unrelated reason
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
// ENCOUNTER half only, the target's own Ap/Pe/incl/Δincl describe the
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

// ADR 0063 section 3: after the dock the aim slot still holds the partner's
// retired ID; the box names it with a docked badge and keeps every row dashed.
func TestTargetBoxNamesDockedPartner(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)
	partner := w.Crafts[1].Name
	w.DockCrafts(0, 1)
	v := NewOrbitView(launchThemeForTest())
	lines := v.buildTargetBox(w)
	if !strings.Contains(lines[0], partner+" · docked") {
		t.Errorf("title = %q, want %q", lines[0], partner+" · docked")
	}
	for i, row := range lines[1:] {
		if !strings.Contains(row, "—") {
			t.Errorf("row %d = %q, want dash cells", i+1, row)
		}
	}
}

// stillClosingAt4hWorld targets a copy of the active vessel 10° ahead in an
// orbit 1 km higher: the active vessel gains ~0.08° a lap, so the gap is
// still closing when the 4 h closest-approach search ends.
func stillClosingAt4hWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	c.Landed, c.Crashed = false, false
	k := c.State.R.Cross(c.State.V)
	k = k.Scale(1 / k.Norm())
	ang := 10 * math.Pi / 180
	rot := func(v orbital.Vec3) orbital.Vec3 {
		return v.Scale(math.Cos(ang)).Add(k.Cross(v).Scale(math.Sin(ang))).Add(k.Scale(k.Dot(v) * (1 - math.Cos(ang))))
	}
	r := c.State.R.Norm()
	tc := *c
	tc.ID, tc.Name = c.ID+1, "target vessel"
	tc.State.R = rot(c.State.R).Scale((r + 1000) / r)
	tc.State.V = rot(c.State.V).Scale(math.Sqrt(r / (r + 1000)))
	w.Crafts = append(w.Crafts, &tc)
	w.SetTargetCraft(len(w.Crafts) - 1)
	return w
}

// TestTargetBoxSaysBeyondWhenStillClosingAt4h (2026-11-03): the search's
// minimum on its far edge is not an approach; TCA reads "> 4h" and approach
// a dash instead of 4:00:00 and the gap at the edge.
func TestTargetBoxSaysBeyondWhenStillClosingAt4h(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := stillClosingAt4hWorld(t)
	row := strings.TrimRight(ansi.Strip(v.buildTargetBox(w)[4]), " ")
	if !strings.Contains(row, "> 4h") || !strings.HasSuffix(row, "—") {
		t.Errorf("TCA/approach row = %q, want TCA \"> 4h\" and approach —", row)
	}
}

// ADR 0066 decision 4: a landed ghost's co-rotation state looks like an
// ellipse; TARGET dashes Ap/Pe/incl for it as for a landed local vessel, and
// reads them for the same ghost in orbit (the equal case).
func TestTargetBoxDashesApPeInclForLandedGhost(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	relR := orbital.Vec3{X: 7.1e6}
	relV := orbital.Vec3{Y: 7400}
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:gern", CraftID: 7, Name: "Aloft",
		PrimaryID: c.Primary.ID, Pos: w.BodyPosition(c.Primary).Add(relR), RelPos: relR, Vel: relV}}
	w.SetTargetGhost("SHA256:gern", 7)
	shape := func() string { return v.buildTargetBox(w)[2] }
	if got := shape(); strings.Contains(got, "—") {
		t.Fatalf("control: orbiting ghost shape row has dashes: %q", got)
	}
	w.Ghosts[0].Landed = true
	if got := shape(); strings.Count(got, "—") != 3 {
		t.Errorf("landed ghost Ap/Pe/incl row = %q, want three dashes", got)
	}
}
