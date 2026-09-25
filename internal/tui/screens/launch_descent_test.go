// Package screens — descent-half render tests for the surface view
// (issue #348 §3 / ADR 0043): the corridor instrument block and its
// alarm ladder, the dashed arc to ground, and the impact marker.

package screens

import (
	"math/bits"
	"strings"
	"testing"

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
// survive the smallest supported terminal: the impact marker lands on
// the ground line at the Design Size, not only at the roomy sizes a dev
// window happens to be. ADR 0051 slice 4 item 1 retires the LAUNCH-only
// DESCENT CORRIDOR block; decision 12 already folded its altitude:/vert:/
// impact:/stop: rows onto NAVIGATION, shared by both views, so those are
// what a descent now reads there instead.
func TestLaunchViewDescentInstrumentsAt80x24(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, 120)

	out := v.Render(w, DesignWidth, DesignHeight)
	stripped := stripANSI(out)
	if strings.Contains(stripped, "DESCENT CORRIDOR") {
		t.Errorf("render still shows the retired DESCENT CORRIDOR block:\n%s", out)
	}
	for _, want := range []string{"altitude:", "vert:", "impact:", "stop:"} {
		if !strings.Contains(stripped, want) {
			t.Errorf("render is missing %q (NAVIGATION's shared descent row):\n%s", want, out)
		}
	}
	if rows := len(strings.Split(out, "\n")); rows > DesignHeight {
		t.Errorf("render is %d rows tall, want <= %d", rows, DesignHeight)
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
// chip duplication, now fully resolved. The retired DESCENT chip's own
// altitude:/vert: rows were gone for good in an earlier slice; ADR 0051
// slice 4 item 1 retires the LAUNCH-only DESCENT CORRIDOR block too
// (decision 12 already folded its impact:/stop: numbers onto NAVIGATION,
// shared by both views), so a Moon descent now carries exactly one
// `altitude:` reading, not the temporary two this test used to pin.
func TestSurfaceViewShowsOneDescentBlock(t *testing.T) {
	w := descendingMoonCraft(t, 8_000, 40)
	w.ViewMode = sim.ViewLaunch

	hud := NewOrbitView(ghostTestTheme())
	hud.Resize(200, 60)
	hud.Render(w, 0, 200, 60)

	v := NewLaunchView(launchThemeForTest(), hud)
	v.Resize(200, 60)
	out := stripANSI(v.Render(w, 200, 60))

	if strings.Contains(out, "DESCENT CORRIDOR") {
		t.Fatal("the retired DESCENT CORRIDOR block is still on screen for a Moon descent")
	}
	if n := strings.Count(out, "altitude:"); n != 1 {
		t.Errorf("frame carries %d `altitude:` rows, want 1 (NAVIGATION only, now that the LAUNCH-only DESCENT CORRIDOR block is retired)", n)
	}
	// F9/F14 (gate review): the launch strip's own always-on bottom-row
	// clock line carries one "vert:" reading of its own; NAVIGATION
	// (ADR 0051) carries a second. Both read the same state, so they
	// never disagree; DESCENT (the chip this test originally guarded)
	// still never appears. Slice 4 item 2 retires the strip's own vert:,
	// which will bring this down to 1.
	if n := strings.Count(out, "vert:"); n != 2 {
		t.Errorf("frame carries %d `vert:` rows, want 2 (the launch strip's own + NAVIGATION's): the retired DESCENT chip must not have reappeared", n)
	}
	// The rows worth keeping came along rather than being dropped —
	// `fpa` included; it survived the #377 layout change (Jason's call).
	for _, row := range []string{"horiz:", "fpa:", "impact:", "stop:"} {
		if !strings.Contains(out, row) {
			t.Errorf("render is missing the %q row", row)
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

	if !strings.Contains(out, "vert:") {
		t.Error("the orbit map lost its DESCENT chip — there is no corridor there to replace it")
	}
}

// TestBurnAtRowDisappearsOnceBurnStarts is issue #377's acceptance item
// verbatim: "burn at disappears once the burn is under way; stop margin
// does not." The retired LAUNCH-only DESCENT CORRIDOR block used to
// carry both; ADR 0051 decision 12 moved the braking-start cue onto
// ENGINE's node row (`engineNodeLine`) and the margin onto NAVIGATION's
// `stop:` cell, shared by both views, so this now checks those instead.
func TestBurnAtRowDisappearsOnceBurnStarts(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w := descendingMoonCraft(t, 20_000, 120)

	before := stripANSI(v.Render(w, 200, 60))
	if !strings.Contains(before, "braking burn at") {
		t.Fatal("precondition: expected ENGINE's node row to show a braking-burn cue for a comfortably-stoppable descent")
	}
	if !strings.Contains(before, "stop:") {
		t.Fatal("precondition: expected NAVIGATION's stop: cell")
	}

	w.ActiveCraft().ActiveBurn = &spacecraft.ActiveBurn{DVRemaining: 100}

	after := stripANSI(v.Render(w, 200, 60))
	if strings.Contains(after, "braking burn at") {
		t.Errorf("ENGINE's braking-burn cue still rendered once ActiveBurn was set:\n%s", after)
	}
	if !strings.Contains(after, "stop:") {
		t.Errorf("NAVIGATION's stop: cell disappeared once the burn started, it must stay live:\n%s", after)
	}
}

// TestDescentCorridorRefusedForecastReadsAsAlarmNotSilence is PR #382
// review finding 1's end-to-end regression: a REACHABLE refusal
// (near-hover thrust — TWR barely above local g, an Isp-3000s engine so
// mass loss over the search window stays negligible — 20 km up at a
// mundane 50 m/s, confirmed via PredictPoweredStop directly to hit the
// step cap: Outcome=StopUndetermined, ok=false) must not present as
// healthy under a red arc. ADR 0051 decision 12 (re-grill Q2) moved the
// alarm WORDS off the stop cell onto NAVIGATION's title
// (`navigationDescentAlarm`, short form "⚠ NO STOP"); the cell itself
// keeps its own non-dash text ("unresolved (…)"). Both, plus the
// arc/impact-marker alarm promotion (drawDescentArc, keyed off
// dc.Margin.State), have to agree that this is CAN'T STOP.
func TestDescentCorridorRefusedForecastReadsAsAlarmNotSilence(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	for _, b := range w.System().Bodies {
		if b.ID == "moon" {
			c.Primary = b
		}
	}
	c.Landed, c.Crashed = false, false
	c.Stages = nil
	const altM = 20_000.0
	r := c.Primary.RadiusMeters() + altM
	gLocal := c.Primary.GravitationalParameter() / (r * r)
	const massKg0 = 15_000.0
	c.Thrust = (gLocal + 0.01) * massKg0 // TWR ~1% above local hover
	c.Isp = 3_000                        // negligible mass loss over the 1800s search window
	c.DryMass = massKg0 / 2
	c.Fuel = massKg0 / 2
	c.Monoprop = 0
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{X: -50}
	c.State.M = c.TotalMass()

	// Precondition: this really is the refusal case, not some other
	// outcome that happens to also alarm.
	if stop, ok := sim.PredictPoweredStop(c, sim.DescentPredictHorizon); ok {
		t.Fatalf("setup: expected PredictPoweredStop to refuse (step cap), got ok=true Outcome=%v", stop.Outcome)
	}

	out := stripANSI(v.Render(w, 200, 60))

	// NAVIGATION's title alarm badge carries the refusal in the alarm's
	// own short-form words (re-grill Q2).
	if !strings.Contains(out, "⚠ NO STOP") {
		t.Errorf("refused forecast did not raise NAVIGATION's title alarm:\n%s", out)
	}

	// The stop: cell itself: not a quiet em dash.
	var stopRow string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "impact:") && strings.Contains(line, "stop:") {
			stopRow = line
			break
		}
	}
	if stopRow == "" {
		t.Fatal("no impact:/stop: row found in the render")
	}
	if strings.Contains(stopRow, "stop:—") || strings.Contains(stopRow, "stop: —") {
		t.Errorf("refused forecast rendered a silent em dash: %q", stopRow)
	}
	if !strings.Contains(stopRow, "unresolved") {
		t.Errorf("refused forecast row does not read unresolved: %q", stopRow)
	}

	// The arc/impact-marker alarm: drawDescentArc paints alert-red
	// exactly when dc.Margin.State == MarginInsufficient, which is what
	// this refusal must map to (sim.DeriveMarginState).
	if n := v.canvas.CountColor(render.ColorAlert); n == 0 {
		t.Error("refused forecast did not promote the descent arc to alert-red")
	}
}
