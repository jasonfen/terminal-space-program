package screens

import (
	"math"
	"slices"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// rowSpans reports, per colour, the lowest and highest sprite row (stride
// units above the anchor, fractional across a dock seam) its pixels occupy,
// for a stack drawn straight up.
func rowSpans(px []SpritePixel, basis widgets.Basis) map[lipgloss.Color][2]float64 {
	out := map[lipgloss.Color][2]float64{}
	for _, p := range px {
		row := p.OffsetWorld.Dot(basis.Y)
		sp, ok := out[p.Color]
		if !ok {
			sp = [2]float64{row, row}
		}
		sp[0], sp[1] = math.Min(sp[0], row), math.Max(sp[1], row)
		out[p.Color] = sp
	}
	return out
}

// spanNear is whether two row spans agree to a micro-row.
func spanNear(a, b [2]float64) bool {
	return math.Abs(a[0]-b[0]) < 1e-9 && math.Abs(a[1]-b[1]) < 1e-9
}

// ADR 0053 "Settled here" item 1: a docked component above a seam is drawn
// nose to the seam, flipped, with the gap at the seam shrunk to 0.6 m (0.4 of
// a row). A lead A (4 rows) with a docked block [B 6 rows, C 4 rows] sits,
// unflipped, A 0-3, separator 4, B 5-10, separator 11, C 12-15. Flipped, the
// block mirrors: C 5-8 (its nose to the seam), separator 9, B 10-15; the
// one-row gap then gives up 0.6 of a row, so everything above it is 0.6 lower:
// C 4.4-7.4, B 9.4-14.4. The lead does not move.
func TestComposeLaunchSpriteFlipsDockedBlockNoseToSeam(t *testing.T) {
	a := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#FF0000"}
	b := spacecraft.Stage{LaunchSpriteRowsPx: 6, Color: "#00FF00"}
	c := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#0000FF"}
	cmd := orbital.Vec3{Z: 1}
	basis := widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Z: 1}}
	stages := []spacecraft.Stage{a, b, c}

	plain := rowSpans(ComposeLaunchSprite(stages, cmd, basis, 1), basis)
	want := map[string][2]float64{"#FF0000": {0, 3}, "#00FF00": {5, 10}, "#0000FF": {12, 15}}
	for col, sp := range want {
		if !spanNear(plain[lipgloss.Color(col)], sp) {
			t.Fatalf("control (no seams): %s rows %v, want %v", col, plain[lipgloss.Color(col)], sp)
		}
	}

	flipped := rowSpans(ComposeLaunchSprite(stages, cmd, basis, 1, 1), basis)
	wantFlipped := map[string][2]float64{"#FF0000": {0, 3}, "#0000FF": {4.4, 7.4}, "#00FF00": {9.4, 14.4}}
	for col, sp := range wantFlipped {
		if !spanNear(flipped[lipgloss.Color(col)], sp) {
			t.Errorf("seam at stage 1: %s rows %v, want %v", col, flipped[lipgloss.Color(col)], sp)
		}
	}
	// The gap between the hulls' facing edges, in metres (one row = 1.5 m at
	// pxSize 1.5): C's bottom edge less A's top edge.
	gapM := (flipped["#0000FF"][0] - 0.5 - (flipped["#FF0000"][1] + 0.5)) * sim.DockStrideM
	if math.Abs(gapM-0.6) > 1e-9 {
		t.Errorf("gap between the docked hulls = %.4f m, want 0.6", gapM)
	}
	// The drawn stack is as long as sim says: bottom edge to nose edge.
	topM := (flipped["#00FF00"][1] + 0.5 - (flipped["#FF0000"][0] - 0.5)) * sim.DockStrideM
	if got := sim.StackLengthM(stages, []int{1}); math.Abs(got-topM) > 1e-9 {
		t.Errorf("sim.StackLengthM = %.4f m but the flipped sprite is %.4f m tall", got, topM)
	}
	// No seams: the sim length is the old row count, to the bit.
	if got, want := sim.StackLengthM(stages, nil), float64(sim.StackRows(stages))*sim.DockStrideM; got != want {
		t.Errorf("no-seam StackLengthM = %v, want StackRows x stride %v", got, want)
	}
}

// The gap at a dock seam is drawn as one thin line in the dock seam colour at
// the centre of the 0.6 m gap, so it reads as the joint, not as empty space; a
// separator inside a block, and every separator of an undocked stack, stays
// the stage separator colour. Layout as above: seam line at row 3.7 (A's top
// edge 3.5 plus half the 0.4-row gap), in-block separator at row 8.4 once
// flipped and shifted.
func TestDockSeamGapRowIsDrawnInTheSeamColour(t *testing.T) {
	a := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#FF0000"}
	b := spacecraft.Stage{LaunchSpriteRowsPx: 6, Color: "#00FF00"}
	c := spacecraft.Stage{LaunchSpriteRowsPx: 4, Color: "#0000FF"}
	basis := widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Z: 1}}
	stages := []spacecraft.Stage{a, b, c}
	// colourRows maps colour -> the sorted distinct rows drawn in it.
	colourRows := func(stages []spacecraft.Stage, seams ...int) map[lipgloss.Color][]float64 {
		out := map[lipgloss.Color][]float64{}
		for _, p := range ComposeLaunchSprite(stages, orbital.Vec3{Z: 1}, basis, 1, seams...) {
			row := p.OffsetWorld.Dot(basis.Y)
			if !slices.ContainsFunc(out[p.Color], func(x float64) bool { return math.Abs(x-row) < 1e-9 }) {
				out[p.Color] = append(out[p.Color], row)
			}
		}
		return out
	}
	if dockSeamColor() == stageSeparatorColor() {
		t.Fatal("dock seam colour equals the stage separator colour: nothing to tell apart")
	}
	for _, c := range []struct {
		name  string
		seams []int
		col   lipgloss.Color
		want  float64
	}{
		{"seam line", []int{1}, dockSeamColor(), 3.7},
		{"separator inside the docked block", []int{1}, stageSeparatorColor(), 8.4},
		{"same gap row, no dock", nil, stageSeparatorColor(), 4},
	} {
		rows := colourRows(stages, c.seams...)[c.col]
		if !slices.ContainsFunc(rows, func(x float64) bool { return math.Abs(x-c.want) < 1e-9 }) {
			t.Errorf("%s: rows drawn in %q are %v, want one at %.1f", c.name, c.col, rows, c.want)
		}
	}
	if got := colourRows(stages, 1)[dockSeamColor()]; len(got) != 1 {
		t.Errorf("seam colour is drawn on %d rows, want exactly 1 (a line)", len(got))
	}

	// A two-row gap (both stages tall enough for a taper row, a 0-5, taper 6,
	// separator 7, b 8-13): the joint is still one thin line, and the stage
	// above comes down 1.6 rows, so the gap is the same 0.4 row (0.6 m): line
	// at 5.7, b from 6.4.
	tall := []spacecraft.Stage{{LaunchSpriteRowsPx: 6, Color: "#FF0000"}, {LaunchSpriteRowsPx: 6, Color: "#00FF00"}}
	rows := colourRows(tall, 1)
	if got := rows[dockSeamColor()]; len(got) != 1 || math.Abs(got[0]-5.7) > 1e-9 {
		t.Errorf("two-row seam gap: joint rows %v, want one at 5.7", got)
	}
	if lo := slices.Min(rows["#00FF00"]); math.Abs(lo-6.4) > 1e-9 {
		t.Errorf("two-row seam gap: upper hull starts at row %.3f, want 6.4", lo)
	}
	if _, drawn := rows[stageSeparatorColor()]; drawn {
		t.Error("two-row seam gap: a taper or separator row survives inside the gap")
	}
}

// Two docked blocks flip each on their own, the gap between them staying put
// and each giving up its share (0.6 of a row for a one-row gap).
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
	// Seam 1 (gap row 3) lowers everything above it by 0.6, seam 2 (gap row 10)
	// lowers what is above that by another 0.6.
	want := map[string][2]float64{"#100000": {0, 2}, "#300000": {3.4, 5.4}, "#200000": {7.4, 8.4}, "#500000": {9.8, 11.8}, "#400000": {13.8, 14.8}}
	for col, sp := range want {
		if !spanNear(got[lipgloss.Color(col)], sp) {
			t.Errorf("%s rows %v, want %v", col, got[lipgloss.Color(col)], sp)
		}
	}
	// Both gaps are 0.6 m (0.4 of a row): the second runs P's top edge 8.9 to S's bottom edge 9.3.
	if gap := (got["#500000"][0] - 0.5 - (got["#200000"][1] + 0.5)) * sim.DockStrideM; math.Abs(gap-0.6) > 1e-9 {
		t.Errorf("second seam gap = %.4f m, want 0.6", gap)
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
