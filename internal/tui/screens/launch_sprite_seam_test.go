package screens

import (
	"math"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// rowSpans reports, per colour, the lowest and highest sprite row (stride
// units above the anchor) its pixels occupy, for a stack drawn straight up.
func rowSpans(px []SpritePixel, basis widgets.Basis) map[lipgloss.Color][2]int {
	out := map[lipgloss.Color][2]int{}
	for _, p := range px {
		row := int(math.Round(p.OffsetWorld.Dot(basis.Y)))
		sp, ok := out[p.Color]
		if !ok {
			sp = [2]int{row, row}
		}
		if row < sp[0] {
			sp[0] = row
		}
		if row > sp[1] {
			sp[1] = row
		}
		out[p.Color] = sp
	}
	return out
}

// ADR 0053 "Settled here" item 1: a docked component above a seam is drawn
// nose to the seam, flipped. A lead A (4 rows) with a docked block [B 6 rows,
// C 4 rows] sits, unflipped, A 0-3, separator 4, B 5-10, separator 11, C 12-15.
// Flipped, the block mirrors: C 5-8 (its nose to the seam), separator 9, B 10-15.
// The lead and the total height do not change.
func TestComposeLaunchSpriteFlipsDockedBlockNoseToSeam(t *testing.T) {
	a := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#FF0000"}
	b := spacecraft.Stage{LaunchSpriteRowsPx: 6, Color: "#00FF00"}
	c := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#0000FF"}
	cmd := orbital.Vec3{Z: 1}
	basis := widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Z: 1}}
	stages := []spacecraft.Stage{a, b, c}

	plain := rowSpans(ComposeLaunchSprite(stages, cmd, basis, 1), basis)
	want := map[string][2]int{"#FF0000": {0, 3}, "#00FF00": {5, 10}, "#0000FF": {12, 15}}
	for col, sp := range want {
		if plain[lipgloss.Color(col)] != sp {
			t.Fatalf("control (no seams): %s rows %v, want %v", col, plain[lipgloss.Color(col)], sp)
		}
	}

	flipped := rowSpans(ComposeLaunchSprite(stages, cmd, basis, 1, 1), basis)
	wantFlipped := map[string][2]int{"#FF0000": {0, 3}, "#0000FF": {5, 8}, "#00FF00": {10, 15}}
	for col, sp := range wantFlipped {
		if flipped[lipgloss.Color(col)] != sp {
			t.Errorf("seam at stage 1: %s rows %v, want %v", col, flipped[lipgloss.Color(col)], sp)
		}
	}
	// Same hull height either way: the nose, and the port, do not move.
	var maxPlain, maxFlipped int
	for _, sp := range plain {
		if sp[1] > maxPlain {
			maxPlain = sp[1]
		}
	}
	for _, sp := range flipped {
		if sp[1] > maxFlipped {
			maxFlipped = sp[1]
		}
	}
	if maxPlain != maxFlipped {
		t.Errorf("flipping changed the stack height: %d -> %d rows", maxPlain, maxFlipped)
	}
	if got, want := sim.StackRows(stages), maxFlipped+1; got != want {
		t.Errorf("sim.StackRows = %d but the flipped sprite is %d rows tall", got, want)
	}
}

// Two docked blocks flip each on their own, the gap between them staying put.
func TestComposeLaunchSpriteFlipsEachDockedBlock(t *testing.T) {
	st := func(rows int, col string) spacecraft.Stage {
		return spacecraft.Stage{LaunchSpriteRowsPx: rows, Color: col}
	}
	// Lead L(3); block 1 = [P(2), Q(3)]; block 2 = [R(2), S(3)].
	stages := []spacecraft.Stage{st(3, "#100000"), st(2, "#200000"), st(3, "#300000"), st(2, "#400000"), st(3, "#500000")}
	basis := widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Z: 1}}
	got := rowSpans(ComposeLaunchSprite(stages, orbital.Vec3{Z: 1}, basis, 1, 1, 3), basis)
	// Unflipped: L 0-2, sep 3, P 4-5, sep 6, Q 7-9, sep 10, R 11-12, sep 13, S 14-16.
	// Block 1 occupies 4-9 and mirrors: Q 4-6, sep 7, P 8-9. The seam gap
	// (sep 10) stays; block 2 occupies 11-16 and mirrors: S 11-13, sep 14, R 15-16.
	want := map[string][2]int{"#100000": {0, 2}, "#300000": {4, 6}, "#200000": {8, 9}, "#500000": {11, 13}, "#400000": {15, 16}}
	for col, sp := range want {
		if got[lipgloss.Color(col)] != sp {
			t.Errorf("%s rows %v, want %v", col, got[lipgloss.Color(col)], sp)
		}
	}
}

// sim.DockSeams reads the composite's component stage counts.
func TestDockSeamsFromComponents(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	tug := func(name string) *spacecraft.Spacecraft {
		c := spacecraft.NewFromStages(stagesOf(t, "rcs-tug"))
		c.Primary = w.Crafts[0].Primary
		c.State = w.Crafts[0].State
		c.Name = name
		return c
	}
	lander := spacecraft.NewFromStages(stagesOf(t, "lander-descent", "lander-ascent"))
	lander.Primary = w.Crafts[0].Primary
	lander.State = w.Crafts[0].State
	w.Crafts = []*spacecraft.Spacecraft{tug("A"), lander}
	if got := sim.DockSeams(w.Crafts[0]); got != nil {
		t.Errorf("a lone vessel has seams %v", got)
	}
	w.DockCrafts(0, 1)
	if got := sim.DockSeams(w.Crafts[0]); len(got) != 1 || got[0] != 1 {
		t.Errorf("tug + two-stage lander: seams %v, want [1]", got)
	}
}

// colourColumns is the lowest and highest canvas column holding cells of col.
func colourColumns(v *OrbitView, col lipgloss.Color) (lo, hi int, ok bool) {
	lo, hi = math.MaxInt, -1
	for c := 0; c < v.canvas.Cols(); c++ {
		if v.canvas.CountColorIn(col, c, 0, c, 1<<20) > 0 {
			ok = true
			if c < lo {
				lo = c
			}
			if c > hi {
				hi = c
			}
		}
	}
	return lo, hi, ok
}

// The Proximity View draws a docked composite target through the seam: the
// two-stage lander riding on a tug's nose has its Ascent stage (the one that
// is the block's TOP) next to the tug and its Descent stage at the far end,
// the real Apollo picture. Without the wiring the block draws upright, Descent
// first.
func TestProximityDrawsDockedComponentNoseToSeam(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"rcs-tug"}, orbital.Vec3{X: 30}, 0)
	lander := spacecraft.NewFromStages(stagesOf(t, "lander-descent", "lander-ascent"))
	lander.Primary, lander.State = w.Crafts[1].Primary, w.Crafts[1].State
	w.Crafts = append(w.Crafts, lander)
	w.DockCrafts(1, 2) // the target (tug) leads; the lander docks onto it
	tc := w.Crafts[1]
	if got := sim.DockSeams(tc); len(got) != 1 || got[0] != 1 {
		t.Fatalf("fixture: seams %v, want [1]", got)
	}
	w.SetTargetCraft(1)

	v := newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	if !v.proxTargetSpriteShown {
		t.Fatal("fixture: the composite target must be drawn as a sprite")
	}
	descent, ascent := stageSpriteColor(tc.Stages[1]), stageSpriteColor(tc.Stages[2])
	if descent == ascent {
		t.Fatalf("fixture: Descent and Ascent share colour %v; the order is unreadable", descent)
	}
	dLo, _, dOK := colourColumns(v, descent)
	aLo, _, aOK := colourColumns(v, ascent)
	if !dOK || !aOK {
		t.Fatalf("Descent drawn=%v Ascent drawn=%v", dOK, aOK)
	}
	// The target's nose runs +along-track, so columns grow up the stack: the
	// Ascent (nose to the seam) starts at a lower column than the Descent.
	if aLo >= dLo {
		t.Errorf("Ascent starts at column %d, Descent at %d: the lander is drawn upright, not nose to the tug", aLo, dLo)
	}
}
