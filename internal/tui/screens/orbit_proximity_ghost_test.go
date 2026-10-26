package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// ADR 0053 slice E in the Proximity View: a friend's hull is the rectangle
// they reported, their port is marked, and the same rows and the same port line
// read as against a vessel of your own.

const proxGhostOwner = "SHA256:friend"

// ghostOf is vessel v as a viewer's ghost slate carries it after a report.
func ghostOf(w *sim.World, v *spacecraft.Spacecraft) sim.Ghost {
	g := sim.VesselPortGeometry(v)
	gh := sim.Ghost{
		Owner: proxGhostOwner, CraftID: 9, Handle: "pal", Name: v.Name, PrimaryID: v.Primary.ID,
		RelPos: v.State.R, Vel: v.State.V,
		Pos:      w.BodyPosition(v.Primary).Add(v.State.R),
		Attitude: g.Axis, NoseM: g.NoseM, WidthM: g.WidthM,
	}
	for _, p := range g.Ports {
		gh.Ports = append(gh.Ports, sim.GhostPort{OffsetM: p.OffsetM, Name: p.Name})
	}
	return gh
}

// ghostPairWorld is portedPairWorld with the target turned into a ghost.
func ghostPairWorld(t *testing.T, own, tgt []string, off orbital.Vec3, tiltDeg float64) *sim.World {
	t.Helper()
	w := portedPairWorld(t, own, tgt, off, tiltDeg)
	b := w.Crafts[1]
	g := ghostOf(w, b)
	w.Crafts = w.Crafts[:1]
	w.ActiveCraftIdx = 0
	w.Ghosts = []sim.Ghost{g}
	w.SetTargetGhost(proxGhostOwner, 9)
	w.ViewMode = sim.ViewProximity
	return w
}

// The ghost's rows are the local vessel's rows: same numbers from the
// reported hull.
func TestProximityGhostRowsMatchALocalVessel(t *testing.T) {
	off := orbital.Vec3{X: 1.1, Y: 0.6, Z: 0.2}
	local := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, off, 3)
	ghost := ghostPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, off, 3)
	vl, vg := NewOrbitView(chipTestTheme()), NewOrbitView(chipTestTheme())
	vl.Resize(DesignWidth, DesignHeight)
	vg.Resize(DesignWidth, DesignHeight)
	cl, cg := vl.buildProximityChip(local), vg.buildProximityChip(ghost)
	if h := ansi.Strip(cg[0]); !strings.HasSuffix(h, "port: Nudge Tug") {
		t.Errorf("ghost header = %q, want port: Nudge Tug", h)
	}
	for _, label := range []string{"range:", "port:", "misalign:", "lateral:", "rel speed:"} {
		if a, b := chipLine(cl, label), chipLine(cg, label); a == "" || a != b {
			t.Errorf("%s ghost %q, local %q", label, b, a)
		}
	}
}

// The ghost hull is one rectangle of nose_m x width_m along its reported
// attitude. Measured on the canvas: lit inside every corner, dark a few
// pixels past the nose and past each flank.
func TestProximityGhostHullIsTheReportedRectangle(t *testing.T) {
	w := ghostPairWorld(t, []string{"rcs-tug"}, []string{"ntr-tug"}, orbital.Vec3{X: 8}, 0)
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, _ := w.ProximityState()
	v.userZoom = 3
	v.panOffset = st.Frame.AlongTrack.Scale(8)
	v.Render(w, 0, DesignWidth, DesignHeight)
	if !v.proxTargetSpriteShown {
		t.Fatal("setup: the ghost hull must be shown as a rectangle")
	}
	g := w.Ghosts[0]
	basis := widgets.Basis{X: st.Frame.AlongTrack, Y: st.Frame.RadialOut}
	sx, sy := stackDirScreen(g.Attitude, basis)
	along := basis.X.Scale(sx).Add(basis.Y.Scale(sy))
	across := basis.X.Scale(-sy).Add(basis.Y.Scale(sx))
	tail, head := -sim.DockPortTipInsetM(), g.NoseM-sim.DockPortTipInsetM()
	scale := v.canvas.Scale()
	lit := func(a, u float64) bool {
		p := st.TargetWorld.Add(along.Scale(a)).Add(across.Scale(u))
		px, py, ok := v.canvas.Project(p)
		return ok && v.canvas.SubPixelSet(px, py)
	}
	inset := 2 / scale // two pixels inside
	for _, a := range []float64{tail + inset, head - inset} {
		for _, u := range []float64{-g.WidthM/2 + inset, g.WidthM/2 - inset} {
			if !lit(a, u) {
				t.Errorf("rectangle corner (%.2f, %.2f) not lit", a, u)
			}
		}
	}
	// Probe off the +V axis line (u = 0), which the canvas draws through the
	// middle of everything.
	if !lit((tail+head)/2, g.WidthM/4) || !lit((tail+head)/2, 0) {
		t.Error("rectangle interior not lit (a gap row through the fill)")
	}
	// Solid: every pixel strictly inside the projected rectangle is lit (the
	// fixture's hull lies along the screen's X axis).
	x0, y0, _ := v.canvas.Project(st.TargetWorld.Add(along.Scale(tail + inset)).Add(across.Scale(-g.WidthM/2 + inset)))
	x1, y1, _ := v.canvas.Project(st.TargetWorld.Add(along.Scale(head - inset)).Add(across.Scale(g.WidthM/2 - inset)))
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if y0 > y1 {
		y0, y1 = y1, y0
	}
	holes := 0
	for py := y0; py <= y1; py++ {
		for px := x0; px <= x1; px++ {
			if !v.canvas.SubPixelSet(px, py) {
				holes++
			}
		}
	}
	if holes != 0 {
		t.Errorf("%d unlit pixels inside the rectangle (%d x %d px)", holes, x1-x0+1, y1-y0+1)
	}
	out := 4 / scale
	if lit(head+out, g.WidthM/4) {
		t.Error("lit past the reported nose: the rectangle is longer than nose_m")
	}
	if lit((tail+head)/2, g.WidthM/2+out) || lit((tail+head)/2, -g.WidthM/2-out) {
		t.Error("lit past the reported width")
	}
	// Their port is marked, and so is yours: two marks, and the port line's
	// label reads the same distance the chip row does.
	text := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
	if got := strings.Count(text, "○"); got != 2 {
		t.Errorf("port marks on screen = %d, want 2", got)
	}
	// Three: the chip row, the port line's label, and the Centerline ring's
	// label (ADR 0055 slice 2), which carries the same port range.
	if got := strings.Count(text, "8.00 m"); got != 3 {
		t.Errorf("\"8.00 m\" appears %d times, want 3 (chip row + the port line's label + the Centerline ring's)", got)
	}
}

// A friend's vessel out of your plane is the same dotted box as a local one,
// in the dim ghost colour, with both port marks (ADR 0055 B1). 8 m along
// track and 8 m deep: the sibling of TestProximityGhostHullIsTheReportedRectangle,
// whose fixture is 8 m along track and stays a filled block.
func TestProximityGhostHullOutOfPlaneIsADottedBox(t *testing.T) {
	w := ghostPairWorld(t, []string{"rcs-tug"}, []string{"ntr-tug"}, orbital.Vec3{X: 8, Z: 8}, 0)
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, _ := w.ProximityState()
	v.userZoom = 3
	v.panOffset = st.Frame.AlongTrack.Scale(8)
	out := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
	if !v.proxTargetSpriteShown {
		t.Fatal("setup: the ghost box must be shown, not the diamond")
	}
	g := w.Ghosts[0]
	lit := hullProbe(v, st, st.TargetWorld, g.Attitude)
	tail, head := -sim.DockPortTipInsetM(), g.NoseM-sim.DockPortTipInsetM()
	for _, a := range []float64{tail, head} {
		for _, u := range []float64{-g.WidthM / 2, g.WidthM / 2} {
			if !lit(a, u, 1) {
				t.Errorf("box corner (%.2f, %.2f) not lit", a, u)
			}
		}
	}
	if lit((tail+head)/2, g.WidthM/4, 0) {
		t.Error("ghost interior lit: a filled block, not a box")
	}
	// The Centerline scope (top right, past column 110) has its own ring mark
	// once the pair is out of plane; count the side picture only.
	marks := 0
	for _, l := range strings.Split(out, "\n") {
		if r := []rune(l); len(r) > 110 {
			l = string(r[:110])
		}
		marks += strings.Count(l, "○")
	}
	if marks != 2 {
		t.Errorf("port marks in the side picture = %d, want 2", marks)
	}
}

// A report with no shape stays the diamond, and draws no port mark.
func TestProximityGhostWithoutShapeStaysADiamond(t *testing.T) {
	w := ghostPairWorld(t, []string{"rcs-tug"}, []string{"ntr-tug"}, orbital.Vec3{X: 8}, 0)
	g := w.Ghosts[0]
	g.Attitude, g.NoseM, g.WidthM, g.Ports = orbital.Vec3{}, 0, 0, nil
	w.Ghosts[0] = g
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	st, _ := w.ProximityState()
	v.userZoom = 3
	v.panOffset = st.Frame.AlongTrack.Scale(8)
	text := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
	if v.proxTargetSpriteShown {
		t.Error("a ghost with no shape was drawn as a rectangle")
	}
	if !strings.Contains(text, "◆") {
		t.Error("the diamond is missing")
	}
	if !strings.Contains(text, "port: unknown") {
		t.Error("header does not say port: unknown")
	}
	if w.ProximityDockGateReady(st) {
		t.Error("ring ready on an unknown ghost")
	}
}

// The port line goes solid against a ghost the tick the gate holds, the same
// predicate as against a vessel; and the chip reads capturing while the
// approacher's pull runs.
func TestProximityGhostGateLineAndCapturing(t *testing.T) {
	out := ghostPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 3}, 0)
	st, _ := out.ProximityState()
	if proximityPortLineClass(out, st) != widgets.ClassPlanned {
		t.Error("line solid at 3 m")
	}
	w := ghostPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: 0.3}, 0)
	st, _ = w.ProximityState()
	if proximityPortLineClass(w, st) != widgets.ClassReal {
		t.Fatal("line not solid with the gate held against a ghost")
	}
	v := NewOrbitView(chipTestTheme())
	v.Resize(DesignWidth, DesignHeight)
	if got := chipLine(v.buildProximityChip(w), "port:"); strings.Contains(got, "capturing") {
		t.Fatalf("capturing before any pull: %q", got)
	}
	w.Clock.SimTime = w.Clock.SimTime.Add(50 * time.Millisecond)
	w.StepGhostCapture(nil)
	if !w.GhostCapturing(proxGhostOwner, 9) {
		t.Fatal("setup: the pull did not start")
	}
	if got := chipLine(v.buildProximityChip(w), "port:"); !strings.Contains(got, "capturing") {
		t.Errorf("port row during the pull = %q, want capturing", got)
	}
}
