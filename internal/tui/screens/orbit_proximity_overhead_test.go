package screens

// One-line reason for a new test file: the OVERHEAD inset's prod file is new
// (orbit_proximity_overhead.go), so its guards have no sibling to join.

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
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
