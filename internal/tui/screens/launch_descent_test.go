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
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

// TestDescentCorridorLinesInstruments pins issue #377's row layout as
// exact rendered rows, so a formatting change has to be deliberate:
// altitude, descent rate, v_horiz, time to impact, `burn at`, and
// `stop margin` — `fpa` is no longer one of them (dropped in the same
// pass that added the two new rows; the pinned layout in issue #377 has
// no fpa row).
func TestDescentCorridorLinesInstruments(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	dc := sim.DescentCorridor{
		AltitudeM:         12_400,
		DescentRateMps:    182,
		HorizontalRateMps: 4,
		Impact: sim.ImpactPrediction{
			TimeToImpact: 64 * time.Second,
			SpeedMps:     240,
		},
		Stop:      sim.PoweredStopPrediction{Outcome: sim.StopStopped, MarginM: 3_400},
		StopOK:    true,
		BurnAt:    sim.BurnAtCue{AltitudeM: 8_000, InSec: 48},
		HasBurnAt: true,
		Margin:    sim.BurnMargin{State: sim.MarginOK},
	}
	want := []string{
		"DESCENT CORRIDOR",
		"  altitude:   12.40 km",
		"  descent:    182 m/s",
		"  v_horiz:    4 m/s (surface-rel)",
		"  impact in:  1m4s (240 m/s)",
		"  burn at:    8.00 km — in 48s",
		"  stop margin: 3.40 km up   (full thrust now)",
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

// TestDescentCorridorHorizontalRateAlarm: the folded v_horiz row keeps
// the DESCENT chip's own alarm — crossing the ground faster sideways than
// V_CRIT wrecks the vessel however gently the vertical rate has been
// nulled, and none of the corridor's other numbers say so.
func TestDescentCorridorHorizontalRateAlarm(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	quiet := v.descentCorridorLines(sim.DescentCorridor{HorizontalRateMps: sim.CrashVCritMps - 1})
	loud := v.descentCorridorLines(sim.DescentCorridor{HorizontalRateMps: sim.CrashVCritMps + 1})
	if strings.Contains(quiet[3], "CRASH") {
		t.Errorf("below V_CRIT raised the crash alarm: %q", quiet[3])
	}
	if !strings.Contains(loud[3], "CRASH on contact") {
		t.Errorf("above V_CRIT did not raise the crash alarm: %q", loud[3])
	}
}

// TestDescentCorridorNoBurnAtRowWhenHasBurnAtFalse: `burn at` is the one
// row in the pinned layout that's conditionally present (issue #377 §2:
// it hides once the burn is under way, or when there's no future safe
// start at all) — the row count itself has to shrink, not just show a
// placeholder.
func TestDescentCorridorNoBurnAtRowWhenHasBurnAtFalse(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	got := v.descentCorridorLines(sim.DescentCorridor{HasBurnAt: false})
	for _, row := range got {
		if strings.Contains(row, "burn at:") {
			t.Errorf("HasBurnAt=false still rendered a burn-at row: %q", row)
		}
	}
}

// TestStopMarginLabelAlarmLadder: `stop margin` is the alarm surface, so
// each state must change the LABEL, not only a shade a player can miss.
// Pins StopStopped (OK and TIGHT), StopCrashed, StopFuelLimited, and the
// StopOK=false (refused/undetermined) case.
func TestStopMarginLabelAlarmLadder(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	cases := []struct {
		name string
		dc   sim.DescentCorridor
		want string
	}{
		{
			"stopped, OK",
			sim.DescentCorridor{
				Stop: sim.PoweredStopPrediction{Outcome: sim.StopStopped, MarginM: 12_400}, StopOK: true,
				Margin: sim.BurnMargin{State: sim.MarginOK},
			},
			"12.40 km up   (full thrust now)",
		},
		{
			"stopped, TIGHT",
			sim.DescentCorridor{
				Stop: sim.PoweredStopPrediction{Outcome: sim.StopStopped, MarginM: 300}, StopOK: true,
				Margin: sim.BurnMargin{State: sim.MarginTight},
			},
			"300 m up   (full thrust now) TIGHT",
		},
		{
			"crashed",
			sim.DescentCorridor{
				Stop: sim.PoweredStopPrediction{Outcome: sim.StopCrashed, MarginM: -3_400, ImpactSpeedMps: 411}, StopOK: true,
				Margin: sim.BurnMargin{State: sim.MarginInsufficient, Limiter: sim.LimitThrust},
			},
			"short by 3.40 km (impact 411 m/s) CAN'T STOP (thrust)",
		},
		{
			"fuel-limited",
			sim.DescentCorridor{
				Stop: sim.PoweredStopPrediction{Outcome: sim.StopFuelLimited, MarginM: 5_000}, StopOK: true,
				Margin: sim.BurnMargin{State: sim.MarginInsufficient, Limiter: sim.LimitFuel},
			},
			"fuel-limited at 5.00 km CAN'T STOP (fuel)",
		},
		{"undetermined (refused)", sim.DescentCorridor{StopOK: false}, "—"},
	}
	for _, c := range cases {
		if got := v.stopMarginLabel(c.dc); got != c.want {
			t.Errorf("%s: stop margin label %q, want %q", c.name, got, c.want)
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
	for _, want := range []string{"DESCENT CORRIDOR", "altitude:", "descent:", "impact in:", "stop margin:"} {
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

// TestSurfaceViewShowsOneDescentBlock is the review regression for the
// chip duplication.
//
// The surface view assembles the shared chip set (which includes the
// airless-body DESCENT chip) and then appends its own DESCENT CORRIDOR.
// During a Moon descent both were live at once, in opposite corners,
// reporting the same altitude to two decimals and the same rate with
// OPPOSITE SIGNS — `v_vert: -40.0 m/s` on the left against `descent:
// 40 m/s` on the right. The corridor wins (it forecasts ground contact
// and says whether the stop is still flyable) and DESCENT stands down
// while it is up.
func TestSurfaceViewShowsOneDescentBlock(t *testing.T) {
	w := descendingMoonCraft(t, 8_000, 40)
	w.ViewMode = sim.ViewLaunch

	hud := NewOrbitView(ghostTestTheme())
	hud.Resize(200, 60)
	hud.Render(w, 0, 200, 60)

	v := NewLaunchView(launchThemeForTest(), hud)
	v.Resize(200, 60)
	out := stripANSI(v.Render(w, 200, 60))

	if !strings.Contains(out, "DESCENT CORRIDOR") {
		t.Fatal("precondition: the corridor block is not on screen for a Moon descent")
	}
	// Exactly one altitude row, and one rate row, across the whole frame.
	if n := strings.Count(out, "altitude:"); n != 1 {
		t.Errorf("frame carries %d `altitude:` rows, want 1 — the two descent blocks are duplicating", n)
	}
	if n := strings.Count(out, "v_vert:"); n != 0 {
		t.Errorf("frame still carries %d `v_vert:` rows — the DESCENT chip did not stand down", n)
	}
	// The rows worth keeping came along rather than being dropped. `fpa`
	// is deliberately NOT among them — issue #377's pinned row layout
	// replaces it with `stop margin` (and `burn at`, when a future safe
	// start exists).
	for _, row := range []string{"descent:", "v_horiz:", "impact in:", "stop margin:"} {
		if !strings.Contains(out, row) {
			t.Errorf("corridor block is missing the %q row", row)
		}
	}
}

// TestOrbitMapKeepsItsDescentChip: the substitution is the SURFACE view's
// alone. The orbit map has no ground line and no corridor block, so
// removing DESCENT there would delete the readout rather than replace it.
func TestOrbitMapKeepsItsDescentChip(t *testing.T) {
	w := descendingMoonCraft(t, 8_000, 40)
	w.ViewMode = sim.ViewTilted

	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)
	out := stripANSI(v.Render(w, 0, 200, 60))

	if !strings.Contains(out, "v_vert:") {
		t.Error("the orbit map lost its DESCENT chip — there is no corridor there to replace it")
	}
}

// TestBurnAtRowDisappearsOnceBurnStarts is issue #377's acceptance item
// verbatim: "burn at disappears once the burn is under way; stop margin
// does not." A comfortable descent shows a `burn at` cue pre-burn; the
// instant ActiveBurn is set, the very next render drops that row while
// `stop margin` keeps rendering.
func TestBurnAtRowDisappearsOnceBurnStarts(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, 120)

	before := stripANSI(v.Render(w, 200, 60))
	if !strings.Contains(before, "burn at:") {
		t.Fatal("precondition: expected a `burn at` row for a comfortably-stoppable descent")
	}
	if !strings.Contains(before, "stop margin:") {
		t.Fatal("precondition: expected a `stop margin` row")
	}

	w.ActiveCraft().ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100}

	after := stripANSI(v.Render(w, 200, 60))
	if strings.Contains(after, "burn at:") {
		t.Errorf("burn at row still rendered once ActiveBurn was set:\n%s", after)
	}
	if !strings.Contains(after, "stop margin:") {
		t.Errorf("stop margin row disappeared once the burn started — it must stay live:\n%s", after)
	}
}
