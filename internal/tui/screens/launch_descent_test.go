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

// TestDescentCorridorLinesInstruments pins the row layout as exact
// rendered rows, so a formatting change has to be deliberate: altitude,
// descent rate, v_horiz, fpa, time to impact, `burn at`, and
// `stop margin` — the 7-row block (Jason's call: `fpa` was folded out
// once because issue #377's pinned mock only sketched the two new rows,
// then restored — the mock wasn't an exhaustive spec of the whole
// block). All seven rows share one label column (15 cells before the
// value) — 14 (each label's own natural width) left `stop margin:`,
// itself exactly 14, with no room for a separating space at all, so its
// value would land jammed against the colon while every other row had
// daylight after its own: arithmetically aligned, but reading as a
// missing-space bug. 15 gives every row a space of breathing room,
// `stop margin:` included.
func TestDescentCorridorLinesInstruments(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)
	dc := sim.DescentCorridor{
		AltitudeM:          12_400,
		DescentRateMps:     182,
		HorizontalRateMps:  4,
		FlightPathAngleDeg: -88,
		HasFPA:             true,
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
		"  altitude:    12.40 km",
		"  descent:     182 m/s",
		"  v_horiz:     4 m/s (surface-rel)",
		"  fpa:         -88° (0 = horiz, −90 = straight down)",
		"  impact in:   1m4s (240 m/s)",
		"  burn at:     8.00 km — in 48s",
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

// TestDescentCorridorFPARow pins that `fpa` is present with its legend —
// it was folded out of the block once already (issue #377's pinned mock
// only sketched the two new rows), then restored on Jason's explicit
// call, so nothing was asserting it existed. Covers both HasFPA states:
// the legend when defined, and the em dash below the speed floor.
func TestDescentCorridorFPARow(t *testing.T) {
	v := NewLaunchView(launchThemeForTest(), nil)

	withFPA := v.descentCorridorLines(sim.DescentCorridor{FlightPathAngleDeg: -88, HasFPA: true})
	if withFPA[4] != "  fpa:         -88° (0 = horiz, −90 = straight down)" {
		t.Errorf("fpa row with HasFPA = %q, want the legend", withFPA[4])
	}

	withoutFPA := v.descentCorridorLines(sim.DescentCorridor{})
	if withoutFPA[4] != "  fpa:         —" {
		t.Errorf("fpa row without HasFPA = %q, want an em dash", withoutFPA[4])
	}
}

// TestStopMarginLabelAlarmLadder: `stop margin` is the alarm surface, so
// each state must change the LABEL, not only a shade a player can miss.
// Pins StopStopped (OK and TIGHT), StopCrashed, StopFuelLimited, and the
// StopOK=false (refused/undetermined) case.
//
// The refused case is pinned as an ALARM label, not an em dash (PR #382
// review finding 1): sim.DeriveMarginState maps !StopOK to
// MarginInsufficient specifically so drawDescentArc's alarm
// (dc.Margin.State == MarginInsufficient) paints the arc red for
// exactly this state, and a quiet "—" row under a red arc is a refused
// forecast reading as healthy in the corridor and alarming on the
// canvas at the same time — see
// TestDescentCorridorRefusedForecastReadsAsAlarmNotSilence for the
// reachable end-to-end case this was caught from.
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
		{
			"undetermined (refused)",
			sim.DescentCorridor{StopOK: false, Margin: sim.BurnMargin{State: sim.MarginInsufficient, Limiter: sim.LimitThrust}},
			"unresolved — CAN'T STOP (thrust)",
		},
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
	// The rows worth keeping came along rather than being dropped —
	// `fpa` included; it survived the #377 layout change (Jason's call).
	for _, row := range []string{"descent:", "v_horiz:", "fpa:", "impact in:", "stop margin:"} {
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

// TestDescentCorridorRefusedForecastReadsAsAlarmNotSilence is PR #382
// review finding 1's end-to-end regression: a REACHABLE refusal
// (near-hover thrust — TWR barely above local g, an Isp-3000s engine so
// mass loss over the search window stays negligible — 20 km up at a
// mundane 50 m/s, confirmed via PredictPoweredStop directly to hit the
// step cap: Outcome=StopUndetermined, ok=false) must not present as a
// healthy corridor under a red arc. Both signals — the arc/impact-marker
// alarm promotion (drawDescentArc, keyed off dc.Margin.State) and the
// `stop margin` row text — have to agree that this is CAN'T STOP.
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

	// The row: not a quiet em dash, and it reads CAN'T STOP in the
	// alarm's own words.
	var stopMarginRow string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "stop margin:") {
			stopMarginRow = line
			break
		}
	}
	if stopMarginRow == "" {
		t.Fatal("no `stop margin` row found in the render")
	}
	if strings.Contains(stopMarginRow, "stop margin:—") || strings.Contains(stopMarginRow, "stop margin: —") {
		t.Errorf("refused forecast rendered a silent em dash: %q", stopMarginRow)
	}
	if !strings.Contains(stopMarginRow, "CAN'T STOP") {
		t.Errorf("refused forecast row does not read CAN'T STOP: %q", stopMarginRow)
	}

	// The arc/impact-marker alarm: drawDescentArc paints alert-red
	// exactly when dc.Margin.State == MarginInsufficient, which is what
	// this refusal must map to (sim.DeriveMarginState).
	if n := v.canvas.CountColor(render.ColorAlert); n == 0 {
		t.Error("refused forecast did not promote the descent arc to alert-red")
	}
}
