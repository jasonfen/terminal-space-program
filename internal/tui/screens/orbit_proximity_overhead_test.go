package screens

// One-line reason for a new test file: the OVERHEAD inset's prod file is new
// (orbit_proximity_overhead.go), so its guards have no sibling to join.

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// The ladder holds its rung until the need leaves the band: the equal case sits
// on the out gate (95 %) and does not step, a hair over steps up; stepping
// back needs the need under 45 % of the rung below; an entry takes the
// smallest rung that holds.
func TestOverheadPick(t *testing.T) {
	const per = 2.0
	idx := func(m float64) int { return overheadFloorRung(m) }
	out := overheadOutFrac * 100 * per // rung 100 m's out gate
	cases := []struct {
		name   string
		prev   int
		ranged bool
		need   float64
		want   float64
	}{
		{"entry smallest holding", 0, false, 150 * per * overheadOutFrac, 200},
		{"equal case holds", idx(100), true, out, 100},
		{"a hair over steps up", idx(100), true, out + 1e-6, 200},
		{"inside the band holds", idx(200), true, 0.5 * 100 * per, 200},
		{"under 45% of the rung below steps back", idx(200), true, 0.45*100*per - 1e-6, 100},
		{"equal case of the step back holds", idx(200), true, 0.45 * 100 * per, 200},
	}
	for _, c := range cases {
		if got := overheadLadder[overheadPick(c.prev, c.ranged, c.need, per)]; got != c.want {
			t.Errorf("%s: rung %g, want %g", c.name, got, c.want)
		}
	}
}

// The inset is on in the landing view at 140x40 with its marks, F2 hides it,
// and the cross bar never reads under 50 m (the floor's equal case).
func TestOverheadInsetShowsAndDeclutterHides(t *testing.T) {
	v, w, out := landingFrame(t, func(w *sim.World) { w.Sites[0].LatDeg = 0 }) // on the orbit's track: nothing across
	panel := insetRows(out)
	if !strings.Contains(out, "OVERHEAD  Test site") || !strings.Contains(out, "↑L  →track") {
		t.Fatalf("no OVERHEAD inset in the landing view\n%s", panel)
	}
	if !strings.Contains(panel, "➤") || !strings.Contains(panel, string(flagGlyph)) {
		t.Errorf("inset lacks ➤ or ⚑:\n%s", panel)
	}
	if want := readout.Distance(overheadCrossFloorM); !strings.Contains(panel, want) {
		t.Errorf("zero cross does not read the %s floor bar:\n%s", want, panel)
	}
	v.declutter = true
	if got := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight)); strings.Contains(got, "OVERHEAD") {
		t.Errorf("F2 left the inset up")
	}
}

// insetRows is the OVERHEAD panel's text: the 34 cells right of the panel's
// left edge on the rows it covers.
func insetRows(out string) string {
	var rows []string
	for i, l := range strings.Split(out, "\n") {
		if i < 2 || i > 2+overheadPanelH {
			continue
		}
		r := []rune(l)
		if len(r) >= overheadPanelW {
			rows = append(rows, string(r[len(r)-1-overheadPanelW:len(r)-1]))
		}
	}
	return strings.Join(rows, "\n")
}

// A cross offset widens the cross bar (its own scale, P19) and stretches the
// drawn point; along stays at true scale.
func TestOverheadCrossScaleStretches(t *testing.T) {
	p := overheadPoint(100, 30, 2.5)
	if p.X != 100 || p.Y != 75 {
		t.Errorf("overheadPoint = %v, want X 100 (true) and Y 75 (stretched 2.5x)", p)
	}
	// 700 m across needs a 1 km bar: 2*700 over 0.95*rung*(12/6) -> 1000.
	if got := overheadLadder[overheadPick(0, false, 2*700, float64(overheadDiscRows)/overheadBarRows)]; got != 1000 {
		t.Errorf("700 m across ranges the bar to %g, want 1000", got)
	}
}

// The thrust pointer is the nose's horizontal part over the vessel's own
// ground: none straight up, sin(lean) along the track when leaned that way.
func TestOverheadNoseLean(t *testing.T) {
	st := sim.ProximityState{Frame: orbital.LVLHFrame{AlongTrack: orbital.Vec3{Y: 1}}}
	up := orbital.Vec3{X: 7}
	if _, _, ok := overheadNoseLean(orbital.Vec3{X: 1}, up, st); ok {
		t.Errorf("a nose straight up draws a pointer")
	}
	lean := 14 * math.Pi / 180
	nose := orbital.Vec3{X: math.Cos(lean), Y: math.Sin(lean)}
	a, l, ok := overheadNoseLean(nose, up, st)
	if !ok || math.Abs(a-math.Sin(lean)) > 1e-9 || math.Abs(l) > 1e-9 {
		t.Errorf("lean along = %g left = %g ok=%v, want sin(14 deg) and 0", a, l, ok)
	}
	// up x along = left: tipping the nose that way reads as + left
	if _, l, _ := overheadNoseLean(orbital.Vec3{X: math.Cos(lean), Z: math.Sin(lean)}, up, st); l <= 0 {
		t.Errorf("a nose tipped to +Z (up x along) reads left = %g, want > 0", l)
	}
}

// V draws the targeted flag in TARGET green. (Nothing untargeted is not pinned
// here: ProximityState already gates it, so no guard in V's code could go red.)
func TestLaunchViewDrawsTargetedFlag(t *testing.T) {
	w := moonSiteDescentWorld(t, false)
	v := NewLaunchView(launchThemeForTest(), NewOrbitView(launchThemeForTest()))
	v.Render(w, DesignWidth, DesignHeight)
	if n := v.canvas.CountOverlayColor(render.ColorTarget); n < 1 {
		t.Errorf("targeted flag not drawn in V (TARGET-green overlay cells: %d)", n)
	}
}

// lzRingWorld is the Moon site fixture with lzs LZs, the vessel 150 m short of
// LZ-1 and 40 m up, so the inset reads in metres (final approach).
func lzRingWorld(t *testing.T, lzs int) *sim.World {
	t.Helper()
	w := moonSiteDescentWorld(t, false)
	w.Sites[0].LZCount = lzs
	placeCraftNear(w, w.LZPlaces(w.Sites[0])[0])
	return w
}

// placeCraftNear puts the active vessel 150 m short of an LZ and 40 m up.
func placeCraftNear(w *sim.World, pl sim.LZPlace) {
	c := w.ActiveCraft()
	vd := c.State.V.Unit()
	tan := vd.Sub(pl.Up.Scale(vd.Dot(pl.Up))).Unit()
	c.State.R = pl.Pos.Sub(w.BodyPosition(c.Primary)).Sub(tan.Scale(150)).Add(pl.Up.Scale(40))
	c.State.V = tan.Scale(20)
}

// overheadDisc draws the inset for w and returns the view, the bars and the
// disc's rows as text (the cell grid the digits and braille sit on).
func overheadDisc(t *testing.T, w *sim.World) (v *OrbitView, along, cross float64, rows [][]rune) {
	t.Helper()
	if entered, ref := w.ToggleProximityView(); !entered {
		t.Fatalf("o refused: %q", ref)
	}
	v = newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, ok := w.ProximityState()
	if !ok {
		t.Fatal("no proximity state")
	}
	along, cross = v.drawOverhead(w, st)
	for _, l := range strings.Split(ansi.Strip(v.ov.String()), "\n") {
		rows = append(rows, []rune(l))
	}
	return
}

// lzCell is where LZ n's centre (or a ring point) lands in the disc's cells.
func lzCell(t *testing.T, w *sim.World, v *OrbitView, along, cross float64, wp orbital.Vec3) (col, row int, ok bool) {
	t.Helper()
	st, _ := w.ProximityState()
	a, l := overheadRel(st, wp)
	stretch := (4.0 * overheadBarRows / cross) / (2.0 * overheadBarCells / along)
	px, py, on := v.ov.Project(overheadPoint(a, l, stretch))
	return px / 2, py / 4, on
}

// ADR 0068 decision 8, T8: the inset draws every LZ as a circle about its own
// centre, the digit at each centre and the flag at the one you fly to, and the
// cross bar is the measured 200 m (ring held) or the 50 m floor (lone flag).
func TestOverheadLZCirclesDigitsAndFlag(t *testing.T) {
	for _, claim := range []int{0, 2} { // LZ-1 by default; a claim on LZ-2 moves the flag
		w := lzRingWorld(t, 7)
		if claim > 0 {
			w.ActiveCraft().LZClaim = spacecraft.LZClaim{SiteID: w.Sites[0].ID, N: claim}
		}
		flown := max(claim, 1)
		v, along, cross, rows := overheadDisc(t, w)
		if cross != 200 {
			t.Errorf("claim %d: a full ring reads the %g m cross bar, want 200 (ring held)", claim, cross)
		}
		at := func(col, row int) rune {
			if row < 0 || row >= len(rows) || col < 0 || col >= len(rows[row]) {
				return 0
			}
			return rows[row][col]
		}
		digits := 0
		for _, p := range w.LZPlaces(w.Sites[0]) {
			col, row, on := lzCell(t, w, v, along, cross, p.Pos)
			if !on {
				continue
			}
			want := rune('0' + p.N)
			if p.N == flown {
				want = flagGlyph
			}
			if got := at(col, row); got != want {
				t.Errorf("claim %d: LZ-%d centre cell (%d,%d) holds %q, want %q", claim, p.N, col, row, got, want)
			}
			if p.N != flown {
				digits++
			}
			// the circle's four compass points are inked: the outline is
			// about this LZ's centre, not the site point
			for _, th := range []float64{0, math.Pi / 2, math.Pi, 3 * math.Pi / 2} {
				st, _ := w.ProximityState()
				a, l := overheadRel(st, p.Pos)
				stretch := (4.0 * overheadBarRows / cross) / (2.0 * overheadBarCells / along)
				px, py, on := v.ov.Project(overheadPoint(a+sim.SiteLZRadiusM*math.Cos(th), l+sim.SiteLZRadiusM*math.Sin(th), stretch))
				if !on || px < 2 || py < 2 || px >= 2*overheadDiscCols-2 || py >= 4*overheadDiscRows-2 {
					continue // the canvas edge row/column does not take a clipped dot
				}
				if c := at(px/2, py/4); c == '\u2800' || c == ' ' {
					t.Errorf("claim %d: LZ-%d circle not inked at %.0f deg (cell %d,%d)", claim, p.N, th*180/math.Pi, px/2, py/4)
				}
			}
		}
		if digits < 3 {
			t.Errorf("claim %d: only %d neighbour digits in the picture, want the ring in view", claim, digits)
		}
	}
	// Lone flag: the 50 m floor, nothing to widen for.
	if _, _, cross, _ := overheadDisc(t, lzRingWorld(t, 1)); cross != overheadCrossFloorM {
		t.Errorf("a one-LZ flag reads the %g m cross bar, want the %g m floor", cross, overheadCrossFloorM)
	}
}

// The widening takes only LZs within one spacing across: exactly 100 m is in
// (143 m with its circle), 1 cm further is clipped, so LC-13's LZ-2 at 310 m
// never pushes the bar to 500 m.
func TestOverheadLZExtentGate(t *testing.T) {
	if got := overheadLZExtent([]overheadLZ{{left: -sim.SiteLZSpacingM}}); got != sim.SiteLZSpacingM+sim.SiteLZRadiusM {
		t.Errorf("an LZ exactly one spacing across holds %g m, want 143", got)
	}
	if got := overheadLZExtent([]overheadLZ{{left: sim.SiteLZSpacingM + 0.01}, {left: 310}}); got != 0 {
		t.Errorf("a far LZ widened the axis to %g m", got)
	}
}

// A vessel of yours standing on an LZ is drawn there; the colour is the
// site's glyph's (TARGET green for yours, Dim for a partner's).
func TestOverheadStandingVesselAndPartnerColour(t *testing.T) {
	w := lzRingWorld(t, 7)
	s := w.Sites[0]
	o, err := w.SpawnCraft(sim.SpawnSpec{Alongside: true})
	if err != nil {
		t.Fatal(err)
	}
	o.Primary, o.Color = w.ActiveCraft().Primary, "#FF00FF"
	o.Landed, o.LandedLatDeg, o.LandedLonDeg = true, s.LatDeg, s.LonDeg // on LZ-1
	for i, c := range w.Crafts {
		if c == o {
			continue
		}
		w.ActiveCraftIdx = i
	}
	w.SetTargetSite(s.ID)
	v, _, _, rows := overheadDisc(t, w)
	if !strings.ContainsRune(rowsText(rows), overheadStandGlyph) {
		t.Errorf("no %c for the vessel standing on LZ-1:\n%s", overheadStandGlyph, rowsText(rows))
	}
	if n := v.ov.CountOverlayColor(lipgloss.Color("#FF00FF")); n != 1 {
		t.Errorf("standing vessel in its own colour: %d cells, want 1", n)
	}
	own := v.ov.CountOverlayColor(render.ColorTarget)

	// The same site as a partner's: its digits go Dim.
	w2 := lzRingWorld(t, 7)
	ps := w2.Sites[0]
	ps.Owner = "SHA256:pal"
	w2.Sites, w2.PartnerSites = nil, []sim.PartnerSite{{Handle: "pal", Site: ps}}
	w2.SetTargetPartnerSite(ps.Owner, ps.ID)
	v2, _, _, _ := overheadDisc(t, w2)
	if n := v2.ov.CountOverlayColor(render.ColorTarget); n >= own {
		t.Errorf("partner's digits drawn in TARGET green: %d cells, yours %d", n, own)
	}
}

func rowsText(rows [][]rune) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(string(r) + "\n")
	}
	return b.String()
}
