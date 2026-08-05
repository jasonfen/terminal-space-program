// Package screens — descent-half render tests for the surface view
// (issue #348 §3 / ADR 0043): the corridor instrument block and its
// alarm ladder, the dashed arc to ground, and the impact marker.

package screens

import (
	"math/bits"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// countBrailleDots sums the LIT SUB-PIXELS across a rendered canvas, by
// popcount of each braille glyph's dot bits (U+2800 + an 8-bit pattern).
// countBraille (orbit_local_arc_test.go) counts glyph CELLS, which can't
// tell a dashed line from a solid one — at a 3-on/2-off cadence and two
// pixels per cell, nearly every cell still carries ink. Dots can.
func countBrailleDots(s string) int {
	n := 0
	for _, r := range stripANSI(s) {
		if r > 0x2800 && r <= 0x28FF {
			n += bits.OnesCount(uint(r - 0x2800))
		}
	}
	return n
}

// descendingMoonCraft parks the world's active craft on a powered
// descent over the Moon: altM up, falling at vDownMps, airless primary.
func descendingMoonCraft(t *testing.T, altM, vDownMps float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	for _, b := range w.System().Bodies {
		if b.ID == "moon" {
			c.Primary = b
		}
	}
	c.Landed = false
	c.Crashed = false
	c.State.R = orbital.Vec3{X: c.Primary.RadiusMeters() + altM}
	c.State.V = orbital.Vec3{X: -vDownMps}
	c.State.M = c.TotalMass()
	return w
}

// TestDescentCorridorLinesInstruments pins the four instruments issue
// #348 §3 asks for — altitude, descent rate, time to impact, burn margin
// — as exact rendered rows, so a formatting change has to be deliberate.
func TestDescentCorridorLinesInstruments(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	dc := sim.DescentCorridor{
		AltitudeM:      12_400,
		DescentRateMps: 182,
		Impact: sim.ImpactPrediction{
			TimeToImpact: 64 * time.Second,
			SpeedMps:     240,
		},
		// a_net 1 vs a_req 0.5 → 2.00 on thrust; 30 m/s of Δv against a
		// 20 m/s stop burn → 1.50 on fuel. Fuel binds, and 1.50 is OK.
		Margin: sim.ComputeBurnMargin(2000, 1000, 1, 10, 100, 30),
	}
	want := []string{
		"DESCENT CORRIDOR",
		"  altitude:   12.40 km",
		"  descent:    182 m/s",
		"  impact in:  1m4s (240 m/s)",
		"  margin:     1.50 ×",
	}
	got := v.descentCorridorLines(dc)
	if len(got) != len(want) {
		t.Fatalf("got %d rows, want %d:\n%v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d:\n got: %q\nwant: %q", i, got[i], want[i])
		}
	}
}

// TestDescentCorridorMarginAlarmLadder: the margin row is the alarm
// surface, so each state must change the LABEL, not only a shade a
// player can miss. Pins all three rungs plus the undefined case.
func TestDescentCorridorMarginAlarmLadder(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	cases := []struct {
		name   string
		margin sim.BurnMargin
		want   string
	}{
		// a_net 1 vs a_req 2 → 0.50 on thrust: cannot stop.
		{"insufficient", sim.ComputeBurnMargin(2000, 1000, 1, 20, 100, 100), "0.50 × CAN'T STOP (thrust)"},
		// 25 m/s of Δv against a 20 m/s stop burn → 1.25 on fuel.
		{"tight", sim.ComputeBurnMargin(2000, 1000, 1, 10, 100, 25), "1.25 × TIGHT (fuel)"},
		{"ok", sim.ComputeBurnMargin(2000, 1000, 1, 10, 100, 100), "2.00 ×"},
		{"undefined", sim.BurnMargin{}, "—"},
	}
	for _, c := range cases {
		if got := v.marginLabel(c.margin); got != c.want {
			t.Errorf("%s: margin label %q, want %q", c.name, got, c.want)
		}
	}
}

// TestDescentArcIsPlannedDashed: the arc to ground is a PLAN, so it must
// ink at ClassPlanned's dash cadence (ADR 0041 §2 / PR #353), not a
// solid Real-class line. Measured in lit braille sub-pixels against the
// same polyline drawn through the primitive at each class — the live
// canvas must match Planned exactly and undershoot Real.
func TestDescentArcIsPlannedDashed(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	v.Resize(120, 40)
	v.canvas.Clear()
	v.canvas.SetScale(1) // 1 px per metre, so the run is 200 px long
	v.canvas.Center(orbital.Vec3{})

	pts := []orbital.Vec3{{X: -100}, {X: 100}}
	dc := sim.DescentCorridor{Impact: sim.ImpactPrediction{Path: pts, Point: pts[1]}}
	// Put the camera on the far side so the impact glyph is depth-culled
	// and the only ink on the canvas is the arc itself.
	v.drawDescentArc(orbital.Vec3{}, pts[1].Scale(-1), dc)
	live := countBrailleDots(v.canvas.String())
	if live == 0 {
		t.Fatal("descent arc inked nothing")
	}

	measure := func(class widgets.LineClass) int {
		c := widgets.NewCanvas(v.canvas.Cols(), v.canvas.Rows())
		c.SetScale(1)
		c.Center(orbital.Vec3{})
		c.Clear()
		c.PlotPolylineClass(pts, render.ColorPlannedNode, class)
		return countBrailleDots(c.String())
	}
	planned, solid := measure(widgets.ClassPlanned), measure(widgets.ClassReal)
	if live != planned {
		t.Errorf("arc inked %d dots, want ClassPlanned's %d", live, planned)
	}
	if planned >= solid {
		t.Errorf("ClassPlanned inked %d dots vs ClassReal's %d — the dash pattern is not thinning the line", planned, solid)
	}
}

// TestDescentArcAlarmRecolours: colour is the semantic axis (ADR 0041),
// so an unstoppable descent turns the whole arc alert-red rather than
// hiding the alarm in the chip's margin row alone. The impact marker
// promotes to MarkerAlarm with it.
func TestDescentArcAlarmRecolours(t *testing.T) {
	pts := []orbital.Vec3{{X: -100}, {X: 100}}
	draw := func(margin sim.BurnMargin) *widgets.Canvas {
		v := NewLaunchView(launchThemeForTest(), nil)
		v.Resize(120, 40)
		v.canvas.Clear()
		v.canvas.SetScale(1)
		v.canvas.Center(orbital.Vec3{})
		dc := sim.DescentCorridor{
			Impact: sim.ImpactPrediction{Path: pts, Point: pts[1]},
			Margin: margin,
		}
		v.drawDescentArc(orbital.Vec3{}, pts[1], dc) // camera on the near side
		return v.canvas
	}

	okCanvas := draw(sim.ComputeBurnMargin(2000, 1000, 1, 10, 100, 100))
	if n := okCanvas.CountColor(render.ColorPlannedNode); n == 0 {
		t.Error("nominal descent arc inked no planned-cyan cells")
	}
	if n := okCanvas.CountOverlayColor(render.ColorMarkerImpact); n != 1 {
		t.Errorf("nominal impact marker inked %d cells, want 1", n)
	}

	alarmCanvas := draw(sim.ComputeBurnMargin(2000, 1000, 1, 20, 100, 100))
	if n := alarmCanvas.CountColor(render.ColorAlert); n == 0 {
		t.Error("unstoppable descent arc inked no alert-red cells")
	}
	if n := alarmCanvas.CountColor(render.ColorPlannedNode); n != 0 {
		t.Errorf("unstoppable descent arc still inked %d planned-cyan cells — the alarm did not take the line", n)
	}
	if n := alarmCanvas.CountOverlayColor(render.ColorAlert); n != 1 {
		t.Errorf("alarm impact marker inked %d alert cells, want 1 (MarkerAlarm promotion)", n)
	}
}

// TestLaunchViewDescentInstrumentsAt80x24: the whole descent half has to
// survive the smallest supported terminal — the corridor chip composites
// onto the canvas and the impact marker lands on the ground line at
// 80×24, not only at the roomy sizes a dev window happens to be.
func TestLaunchViewDescentInstrumentsAt80x24(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, 120)

	out := v.Render(w, 80, 24)
	for _, want := range []string{"DESCENT CORRIDOR", "altitude:", "descent:", "impact in:", "margin:"} {
		if !strings.Contains(stripANSI(out), want) {
			t.Errorf("80×24 render is missing %q:\n%s", want, out)
		}
	}
	if rows := len(strings.Split(out, "\n")); rows > 24 {
		t.Errorf("render is %d rows tall, want ≤ 24", rows)
	}
	if n := v.canvas.CountOverlayColor(render.ColorMarkerImpact); n == 0 {
		t.Error("no impact marker on the ground line during a descent")
	}
}

// TestLaunchViewNoDescentInstrumentsOnAscent: the corridor is gated on a
// forecast ground contact while falling, so a climbing vehicle — the
// surface view's original job — gets none of it. Guards against the
// descent half turning into permanent ascent clutter.
func TestLaunchViewNoDescentInstrumentsOnAscent(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, -400) // climbing at 400 m/s

	out := stripANSI(v.Render(w, 120, 40))
	if strings.Contains(out, "DESCENT CORRIDOR") {
		t.Errorf("climbing vehicle rendered the descent corridor:\n%s", out)
	}
	if n := v.canvas.CountOverlayColor(render.ColorMarkerImpact); n != 0 {
		t.Errorf("climbing vehicle drew %d impact markers, want 0", n)
	}
}
