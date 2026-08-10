package screens

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// launchThemeForTest returns a minimal Theme with non-nil styles that
// the launch view's chrome paths read. Shared by the Slice 2 tower /
// SOI tests below.
func launchThemeForTest() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

// spawnSaturnVOnPad spawns a Saturn V at KSC LC-39A on Earth's
// launchpad and returns the world + the spawned craft (now active).
// Mirrors landed_test.go's pad-spawn pattern.
func spawnSaturnVOnPad(t *testing.T) (*sim.World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{
		LoadoutID:       spacecraft.LoadoutSaturnVID,
		ParentBodyID:    "earth",
		Launchpad:       true,
		Latitude:        28.6083,
		LongitudeOffset: -80.604,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	if !c.Landed {
		t.Fatal("launchpad spawn should set Landed=true")
	}
	// A crewed Saturn V: these launch-view tests exercise rendering and
	// flight physics, not the comms command gate (ADR 0027). Without a
	// command source the unmanned-default stack would refuse ignition / RCS.
	c.Stages[len(c.Stages)-1].CommandSource = spacecraft.CommandCrewed
	c.SyncFields()
	return w, c
}

// formatLaunchHUD renders the LaunchView readout strip overlaid on
// the bottom braille row of the chase-cam canvas. Format locked by
// v0.11 Slice 1: `T+ HH:MM:SS  v_z ±XXX m/s | downrange X.X km
// Q XX.X kPa (max YY.Y)`.
func TestFormatLaunchHUDTracerBullet(t *testing.T) {
	got := formatLaunchHUD(
		2*time.Minute+34*time.Second,
		120.0,
		15_400.0,
		18_345.0,
		24_500.0,
	)
	want := "T+ 00:02:34  v_z +120 m/s | downrange 15.4 km  Q 18.3 kPa (max 24.5)"
	if got != want {
		t.Errorf("\n got: %q\nwant: %q", got, want)
	}
}

// At T+0 with the rocket still on the pad: T+ zeros, v_z reads 0,
// downrange/Q all zero.
func TestFormatLaunchHUDPadIdle(t *testing.T) {
	got := formatLaunchHUD(0, 0, 0, 0, 0)
	want := "T+ 00:00:00  v_z +0 m/s | downrange 0.0 km  Q 0.0 kPa (max 0.0)"
	if got != want {
		t.Errorf("\n got: %q\nwant: %q", got, want)
	}
}

// Negative v_z (apex passed, falling back) renders signed; T+ above
// the hour boundary rolls cleanly past HH.
func TestFormatLaunchHUDDescentAcrossHourBoundary(t *testing.T) {
	got := formatLaunchHUD(time.Hour+9*time.Minute+5*time.Second, -42.0, 300_000, 0, 500)
	want := "T+ 01:09:05  v_z -42 m/s | downrange 300.0 km  Q 0.0 kPa (max 0.5)"
	if got != want {
		t.Errorf("\n got: %q\nwant: %q", got, want)
	}
}

// Pad marker depth-cull: the launch pad is body-fixed and the camera
// rotates with the rocket as it ascends. When the pad sits on the
// near hemisphere (positive dot product with the camera position
// vector from body centre) it must render; when it's on the far
// hemisphere it must cull, otherwise it draws on top of the body
// from behind. v0.11 Slice 1 grill resolution.
func TestPadMarkerNearHemisphereVisible(t *testing.T) {
	camFromBody := orbital.Vec3{X: 6.5e6, Y: 0, Z: 0}
	padFromBody := orbital.Vec3{X: 6.371e6, Y: 0, Z: 0} // same hemisphere as camera
	if !isNearHemisphere(padFromBody, camFromBody) {
		t.Errorf("pad on near hemisphere: got cull, want visible")
	}
}

func TestPadMarkerFarHemisphereCulled(t *testing.T) {
	camFromBody := orbital.Vec3{X: 6.5e6, Y: 0, Z: 0}
	padFromBody := orbital.Vec3{X: -6.371e6, Y: 0, Z: 0} // antipode
	if isNearHemisphere(padFromBody, camFromBody) {
		t.Errorf("pad on far hemisphere: got visible, want cull")
	}
}

// On the limb (exactly orthogonal to the camera direction) the cull
// is a tie. Pick "visible" so the horizon-edge marker is drawn
// rather than disappearing as the body rotates.
func TestPadMarkerLimbVisible(t *testing.T) {
	camFromBody := orbital.Vec3{X: 6.5e6, Y: 0, Z: 0}
	padFromBody := orbital.Vec3{X: 0, Y: 6.371e6, Z: 0} // exact limb
	if !isNearHemisphere(padFromBody, camFromBody) {
		t.Errorf("pad on limb: got cull, want visible (tie → visible)")
	}
}

// Auto-scale formula from plan: when the player hasn't pinned a zoom
// (LaunchZoom == 0), scale = max(1.0, altitude / (rows - rows/3))
// metres per cell — keeps the rocket centred while the horizon stays
// visible across the pad → 200 km altitude range.
func TestLaunchViewAutoScale(t *testing.T) {
	// Pad-low (altitude tiny → falls to 1.0 floor): rows=30 → denom=20.
	if got := launchAutoScale(0, 30); got != 1.0 {
		t.Errorf("pad: got %g, want 1.0 floor", got)
	}
	// Mid-ascent: altitude 20 km, rows 30, denom 20 → 1000 m/cell.
	if got := launchAutoScale(20_000, 30); got != 1000 {
		t.Errorf("20km: got %g, want 1000", got)
	}
	// Approaching the launch mission floor (200 km), rows 30, denom 20
	// → 10_000 m/cell (10 km/cell, body still visible).
	if got := launchAutoScale(200_000, 30); got != 10000 {
		t.Errorf("200km: got %g, want 10000", got)
	}
	// Tiny rows (degenerate canvas): denominator must clamp ≥ 1 so the
	// scale doesn't divide by zero.
	if got := launchAutoScale(50_000, 1); got <= 0 {
		t.Errorf("tiny canvas: got %g, want positive", got)
	}
}

// LaunchView.Render produces a non-empty frame whose title names the
// LAUNCH view and the active craft. Footer carries the ViewLaunch-
// specific key hints (+/- zoom, v cycle).
func TestLaunchViewRenderTitle(t *testing.T) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := v.Render(w, 120, 40)
	if len(out) == 0 {
		t.Fatal("empty render")
	}
	if !strings.Contains(out, "LAUNCH") {
		t.Errorf("expected 'LAUNCH' in title, got:\n%s", out)
	}
	if c := w.ActiveCraft(); c != nil && !strings.Contains(out, c.Name) {
		t.Errorf("expected craft name %q in title, got:\n%s", c.Name, out)
	}
	// v0.14+: the keybind cheat-sheet footer was dropped (the `?` overlay
	// is the keybinding reference) and its row given to the scene.
	if strings.Contains(out, "[?]help") {
		t.Errorf("expected no keybind footer after its removal, got:\n%s", out)
	}
}

// v0.11.3 Slice 4 regression: at pad spawn the loadout sets
// craft.Throttle to 1.0 (the loadout-default engine power setting),
// but the engine isn't actually firing — there's no ManualBurn and
// no ActiveBurn. The composed-rocket render must NOT draw flame in
// that state; otherwise amber flame cells paint into the body fill
// before the player has touched anything. Pinned by the v0.11.3
// playtest verify.
//
// Flame-unique characters used by the v0.11.3 flame palette: '░' and
// '▒'. Neither appears in stage sprites, the LUT, body braille
// textures, or any HUD glyph, so their absence is proof no flame
// rendered. (At throttle 1.0 the 3-row flame would include both,
// across frame A and frame B, so checking either is sufficient and
// the wall-clock frame index doesn't matter.)
func TestLaunchSpriteNoFlamePreIgnition(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	if c.ManualBurn != nil || c.ActiveBurn != nil {
		t.Fatalf("setup: pad spawn should not have an active burn; ManualBurn=%v ActiveBurn=%v",
			c.ManualBurn, c.ActiveBurn)
	}
	if c.Throttle == 0 {
		t.Fatalf("setup: pad spawn should carry loadout-default Throttle (the bug the test pins), got 0")
	}

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 120, 40)

	for _, flameRune := range []string{"░", "▒"} {
		if strings.Contains(out, flameRune) {
			t.Errorf("pre-ignition render contains flame rune %q — flame predicate must gate on ManualBurn/ActiveBurn, not Throttle; render:\n%s",
				flameRune, out)
		}
	}
}

// v0.11.1 Slice 2 tracer bullet: with a Saturn V on the launchpad, the
// LaunchView render contains the LUT crown glyph `╤`. The crown is
// unique to the launch-tower sprite (not used by horizon / pad marker /
// trail / vessel glyph), so its presence in the rendered string is
// proof the tower draws. Pre-impl this fails because no LUT exists.
func TestLaunchTowerRendersAtPad(t *testing.T) {
	w, _ := spawnSaturnVOnPad(t)
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 120, 40)
	if !strings.Contains(out, "╤") {
		t.Errorf("expected LUT crown glyph '╤' in render at pad, got:\n%s", out)
	}
}

// v0.11.1 Slice 2: a second craft sharing the active craft's SOI
// renders in the scene with its own glyph (so dropped stages /
// neighbouring vessels become visible during the launch session).
// Spawn the sister craft `Alongside` — that places it ~25 m east of
// the active craft in the same primary. The launch view's SOI walk
// must call drawSOICraft on it, which routes through the composed-
// sprite path (no panic, non-empty render).
//
// v0.11.3 Slice 4 post-pivot: with braille-pixel rendering there's
// no per-craft glyph sentinel we can pin into the render output, so
// this test asserts the smoke-level invariant (Render returns
// non-empty output with both crafts in the slate). Detailed
// visibility is covered by the ComposeLaunchSprite unit tests.
func TestSiblingCraftInSOIRenders(t *testing.T) {
	w, active := spawnSaturnVOnPad(t)
	_, err := w.SpawnCraft(sim.SpawnSpec{Alongside: true})
	if err != nil {
		t.Fatalf("SpawnCraft sister: %v", err)
	}
	// SpawnCraft set the sister active; switch back to the launchpad
	// craft so the camera frames the pad scene.
	for i, c := range w.Crafts {
		if c == active {
			w.SetActiveCraftIdx(i)
			break
		}
	}
	if w.ActiveCraft() != active {
		t.Fatalf("setup: active not restored to launchpad craft")
	}
	if len(w.Crafts) < 2 {
		t.Fatalf("setup: slate has %d craft, want ≥ 2", len(w.Crafts))
	}

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 120, 40)
	if len(out) == 0 {
		t.Fatal("empty render with sister in SOI")
	}
}

// Dropped stages: staging spawns a passive Spacecraft in `World.Crafts`
// at the active's offset (R, V); the launch view's SOI walk must
// route the dropped craft through drawSOICraft, which now uses the
// composed-sprite path (no panic, no regression in the render).
//
// v0.11.3 Slice 4 post-pivot: braille rendering removed per-glyph
// sentinels, so this test asserts the smoke-level invariant. The
// staging path's data correctness (slate population, fuel transfer,
// retrograde offset) is fully covered by staging_test.go; the
// dropped-stage cmd inheritance is pinned by
// TestStageActivePreservesAttitudeOnDroppedStage.
func TestDroppedStageVisibleAfterDecouple(t *testing.T) {
	w, active := spawnSaturnVOnPad(t)
	if len(active.Stages) < 2 {
		t.Skipf("Saturn V loadout has %d stages, need >= 2 to decouple", len(active.Stages))
	}
	active.Landed = false
	// v0.23 / ADR 0027: this test exercises the decouple/render path, not
	// comms — crew-tend the stack so staging isn't comms-gated (a probe on
	// the KSC pad has no DSN line-of-sight).
	if len(active.Stages) > 0 {
		active.Stages[len(active.Stages)-1].CommandSource = spacecraft.CommandCrewed
		active.SyncFields()
	}
	rNorm := active.State.R.Norm()
	active.State.R = active.State.R.Scale((rNorm + 1000) / rNorm)

	newActiveIdx, jettIdx, err := w.StageActive(w.ActiveCraftIdx)
	if err != nil {
		t.Fatalf("StageActive: %v", err)
	}
	if jettIdx < 0 || jettIdx >= len(w.Crafts) {
		t.Fatalf("jettisonedIdx %d out of range (slate=%d)", jettIdx, len(w.Crafts))
	}
	w.SetActiveCraftIdx(newActiveIdx)
	if w.Crafts[jettIdx] == nil {
		t.Fatalf("dropped stage at slate idx %d is nil", jettIdx)
	}

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 120, 40)
	if len(out) == 0 {
		t.Fatal("empty render after StageActive")
	}
}

// A craft bound to a different primary must not render in the active
// craft's launch scene — the SOI filter (`c.Primary == active.Primary`)
// keeps cross-SOI vessels out of frame. Spawn a launchpad craft on
// Earth, add a sister, then re-bind the sister to Luna; its sentinel
// glyph must NOT appear in the render.
func TestCraftInDifferentSOIDoesNotRender(t *testing.T) {
	w, active := spawnSaturnVOnPad(t)
	sister, err := w.SpawnCraft(sim.SpawnSpec{Alongside: true})
	if err != nil {
		t.Fatalf("SpawnCraft sister: %v", err)
	}
	sister.Glyph = "Ω"
	// Re-bind sister to a different body in the same system.
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("setup: Moon not found in default system")
	}
	sister.Primary = *moon
	// Restore active to the launchpad craft.
	for i, c := range w.Crafts {
		if c == active {
			w.SetActiveCraftIdx(i)
			break
		}
	}

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 120, 40)
	if strings.Contains(out, "Ω") {
		t.Errorf("expected sister-in-Luna-SOI to be culled, got:\n%s", out)
	}
}

// brakingDescentCraft builds a Luna descent scenario matching issue
// #378's measured repro: 20 km altitude, 400 m/s surface-relative
// horizontal speed, 40 m/s descent rate. R sits along body-frame X so
// "horizontal" here is the Y axis; V's X component is the (radially
// inward) descent rate and Y is the horizontal speed. attitudeRetro
// selects which way the nose points — surface-retrograde (braking,
// opposing the horizontal velocity) when true, surface-prograde
// (opposing the braking, i.e. pointing with travel) when false — so
// callers can flip attitude at a fixed velocity without touching
// anything else.
func brakingDescentCraft(t *testing.T, attitudeRetro bool) (*spacecraft.Spacecraft, bodies.CelestialBody) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("setup: Moon not found in default system")
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("setup: NewWorld should produce an active craft")
	}
	c.Primary = *moon
	c.Landed = false
	c.Crashed = false
	c.State.R = orbital.Vec3{X: moon.RadiusMeters() + 20_000}
	c.State.V = orbital.Vec3{X: -40, Y: 400}
	c.State.M = c.TotalMass()
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	if attitudeRetro {
		c.CurrentAttitudeDir = vRel.Scale(-1.0 / vRel.Norm()) // surface-retrograde: braking
	} else {
		c.CurrentAttitudeDir = vRel.Scale(1.0 / vRel.Norm()) // surface-prograde
	}
	return c, *moon
}

// horizVelUnit returns the unit vector of the surface-relative
// horizontal velocity component (the same quantity chaseHorizontalAxis
// is now supposed to track), computed independently of
// chaseHorizontalAxis so the test isn't just re-deriving the
// production formula and comparing it to itself.
func horizVelUnit(c *spacecraft.Spacecraft, localUp orbital.Vec3) orbital.Vec3 {
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	horiz := vRel.Sub(localUp.Scale(vRel.Dot(localUp)))
	return horiz.Scale(1.0 / horiz.Norm())
}

// TestChaseHAxisAlignsWithHorizVelocityDuringBraking pins issue #378's
// measurement, inverted: on a braking (surface-retrograde) descent,
// the old attitude-derived axis measured hAxis·v̂_horiz = -1.000 exactly
// (the scene mirrored, not skewed). The fixed axis must measure
// strictly positive — screen-right is the way the vessel is going,
// even while the nose points backwards to brake.
func TestChaseHAxisAlignsWithHorizVelocityDuringBraking(t *testing.T) {
	c, moon := brakingDescentCraft(t, true /* retrograde: braking */)
	camFromBody := c.State.R
	localUp := camFromBody.Scale(1.0 / camFromBody.Norm())

	hAxis := chaseHorizontalAxis(c, moon, camFromBody, localUp)
	vHat := horizVelUnit(c, localUp)

	dot := hAxis.Dot(vHat)
	if dot <= 0 {
		t.Fatalf("braking descent: hAxis·v̂_horiz = %.4f (want > 0; the issue #378 "+
			"measurement was exactly -1.000 — screen-right pointed against travel)", dot)
	}
	if dot < 0.999 {
		t.Errorf("braking descent: hAxis·v̂_horiz = %.4f (want ~1.0 — hAxis should be "+
			"the horizontal-velocity direction itself, not merely same-signed)", dot)
	}
}

// TestChaseHAxisSenseStableAcrossAttitudeFlip: acceptance criterion —
// switching the pilot's commanded attitude between surface-prograde and
// surface-retrograde at a fixed velocity must not change which way the
// camera's horizontal axis points. Attitude no longer has any say in
// this axis; only velocity does.
func TestChaseHAxisSenseStableAcrossAttitudeFlip(t *testing.T) {
	cRetro, moon := brakingDescentCraft(t, true)
	camFromBody := cRetro.State.R
	localUp := camFromBody.Scale(1.0 / camFromBody.Norm())
	hAxisRetro := chaseHorizontalAxis(cRetro, moon, camFromBody, localUp)

	cPro, _ := brakingDescentCraft(t, false)
	hAxisPro := chaseHorizontalAxis(cPro, moon, camFromBody, localUp)

	if dot := hAxisRetro.Dot(hAxisPro); dot < 0.999 {
		t.Errorf("chase axis changed when attitude flipped prograde<->retrograde at a "+
			"fixed velocity: hAxisRetro·hAxisPro = %.4f (want ~1.0 — pointing the nose "+
			"around must not mirror the world)", dot)
	}
}

// TestChaseHAxisFallsBackToEastBelowSpeedFloor: acceptance criterion —
// a pad-bound vessel (zero *surface-relative* velocity) must still get
// surface-frame east, unchanged from before this fix. Uses the same
// pad-spawn helper the vertical-climb regression test below relies on,
// but asserts the floor directly on a Landed, unignited craft rather
// than mid-ascent.
//
// Note: c.State.V (inertial) is NOT zero here — a landed craft
// co-rotates with the body, so it carries the body's rotational
// velocity at that latitude (~408 m/s at KSC's 28.6°N). It's the
// *air-relative* velocity (physics.AirRelativeVelocity, what
// chaseHorizontalAxis actually uses) that's ~0 for a vessel sitting
// still on the ground — that's the quantity this test's floor check
// is really about.
func TestChaseHAxisFallsBackToEastBelowSpeedFloor(t *testing.T) {
	_, c := spawnSaturnVOnPad(t)
	if got := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary).Norm(); got > chaseHorizSpeedFloorMps {
		t.Fatalf("setup: pad-bound craft should be at rest relative to the ground, "+
			"got |v_rel|=%.6f m/s (>= floor %.2f)", got, chaseHorizSpeedFloorMps)
	}

	camFromBody := c.State.R
	localUp := camFromBody.Scale(1.0 / camFromBody.Norm())
	hAxis := chaseHorizontalAxis(c, c.Primary, camFromBody, localUp)

	rR := render.Vec3{X: c.State.R.X, Y: c.State.R.Y, Z: c.State.R.Z}
	eastR := render.BodyFrameEast(c.Primary, rR)
	eastV := orbital.Vec3{X: eastR.X, Y: eastR.Y, Z: eastR.Z}
	if dot := hAxis.Dot(eastV); dot < 0.999 {
		t.Errorf("pad-bound vessel (V=0): hAxis·east = %.4f (want ~1.0 — a stationary "+
			"vessel has no direction of travel, so the axis must fall back to "+
			"surface-frame east, matching pre-fix behaviour)", dot)
	}
}

// TestDescentArcAheadTrailBehindOnScreen — acceptance criterion: the
// descent arc/impact point projects to screen-right (ahead, ok=true
// side of centre) and a trail breadcrumb behind the vessel projects to
// screen-left, using the real DescentCorridorFor forecast and the real
// LaunchView.Render pipeline (not a hand-rolled projection), on the
// same braking-descent scenario as the dot-product tests above.
func TestDescentArcAheadTrailBehindOnScreen(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("setup: Moon not found in default system")
	}
	c := w.ActiveCraft()
	c.Primary = *moon
	c.Landed = false
	c.Crashed = false
	c.State.R = orbital.Vec3{X: moon.RadiusMeters() + 20_000}
	c.State.V = orbital.Vec3{X: -40, Y: 400}
	c.State.M = c.TotalMass()
	vRel := physics.AirRelativeVelocity(c.State.R, c.State.V, c.Primary)
	c.CurrentAttitudeDir = vRel.Scale(-1.0 / vRel.Norm()) // braking: nose backwards

	// A trail breadcrumb ~60s behind the current position (roughly
	// along -V — good enough for a directional check over a short
	// baseline), stored body-fixed the way maybeSampleLaunchTrail does.
	pastWorld := c.State.R.Sub(c.State.V.Scale(60))
	pastUnit := pastWorld.Scale(1.0 / pastWorld.Norm())
	pastAlt := pastWorld.Norm() - moon.RadiusMeters()
	pastLat, pastLon := render.WorldToBodyFixed(*moon, render.Vec3{X: pastUnit.X, Y: pastUnit.Y, Z: pastUnit.Z}, w.Clock.SimTime)
	w.LaunchTrail = []sim.TrailPoint{{LatDeg: pastLat, LonDeg: pastLon, AltM: pastAlt, SampledAt: w.Clock.SimTime}}

	corridor, descending := sim.DescentCorridorFor(c, sim.DescentPredictHorizon)
	if !descending {
		t.Fatal("setup: expected the scenario to gate as descending")
	}
	if len(corridor.Impact.Path) < 2 {
		t.Fatal("setup: expected a forecast impact path")
	}

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	v.Render(w, 80, 24)

	pxCenter := v.canvas.Cols()                               // canvas pxW = cols*2, so pxW/2 == cols
	impactPx, _, _ := v.canvas.Project(corridor.Impact.Point) // body-relative; bodyCentre is the origin
	if impactPx <= pxCenter {
		t.Errorf("descent impact point projected at px=%d, canvas centre=%d — want strictly "+
			"right of centre (ahead, in the direction of travel)", impactPx, pxCenter)
	}

	trailWorld := render.BodyFixedToWorld(*moon, pastLat, pastLon, w.Clock.SimTime)
	trailPt := orbital.Vec3{X: trailWorld.X, Y: trailWorld.Y, Z: trailWorld.Z}.Scale(moon.RadiusMeters() + pastAlt)
	trailPx, _, _ := v.canvas.Project(trailPt)
	if trailPx >= pxCenter {
		t.Errorf("trail breadcrumb projected at px=%d, canvas centre=%d — want strictly "+
			"left of centre (behind, opposite the direction of travel)", trailPx, pxCenter)
	}
}

// setAirRelativeFlareVelocity sets c.State.V so that
// physics.AirRelativeVelocity(c.State.R, c.State.V, moon) — the
// quantity chaseHorizontalAxis/chaseHAxis actually consume — comes out
// to exactly {X: radialMps, Z: horizZMps}. The Moon is tidally locked,
// so a fixed body-frame position still co-rotates with it at a
// non-trivial rate (~4.6 m/s at low lunar altitude); setting c.State.V
// (inertial) directly, as an earlier version of these tests did, lets
// that co-rotation term leak into the "horizontal" component and
// silently invalidates a floor/sign-reversal test built around a
// specific air-relative magnitude. Adding back the co-rotation term
// (physics.AtmosphereOmega(moon).Cross(r)) cancels it out.
func setAirRelativeFlareVelocity(c *spacecraft.Spacecraft, moon bodies.CelestialBody, radialMps, horizZMps float64) {
	omegaCrossR := physics.AtmosphereOmega(moon).Cross(c.State.R)
	c.State.V = orbital.Vec3{X: radialMps, Z: horizZMps}.Add(omegaCrossR)
}

// touchdownFlareCraft builds a low-altitude Moon craft with a
// horizontal velocity component along body-frame Z — deliberately NOT
// aligned with body-frame east (which sits along Y for a position on
// the X axis; see render.BodyFrameEast) — so a latch test can tell
// "held the prior axis" apart from "snapped to the east fallback" by
// dot product instead of the two directions being coincidentally
// close. horizZMps is the horizontal (Z) component of AIR-RELATIVE
// velocity; the radial (X) component is a small fixed descent rate.
func touchdownFlareCraft(t *testing.T, horizZMps float64) (*spacecraft.Spacecraft, bodies.CelestialBody, orbital.Vec3, orbital.Vec3) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sys := w.System()
	moon := sys.FindBody("Moon")
	if moon == nil {
		t.Fatal("setup: Moon not found in default system")
	}
	c := w.ActiveCraft()
	c.Primary = *moon
	c.Landed = false
	c.Crashed = false
	c.State.R = orbital.Vec3{X: moon.RadiusMeters() + 500}
	setAirRelativeFlareVelocity(c, *moon, -2, horizZMps)
	c.State.M = c.TotalMass()
	camFromBody := c.State.R
	localUp := camFromBody.Scale(1.0 / camFromBody.Norm())
	return c, *moon, camFromBody, localUp
}

// TestChaseHAxisLatchesThroughSpeedFloorAtTouchdown — issue #380 review
// finding: chaseHorizontalAxis was stateless in horizontal velocity, so
// the final flare before touchdown (the pilot deliberately nulling
// horizontal speed) crosses chaseHorizSpeedFloorMps and would snap the
// whole scene to an unrelated surface-frame-east default at exactly the
// moment it costs the pilot most. LaunchView.chaseHAxis must instead
// hold the last velocity-derived axis through that dip.
func TestChaseHAxisLatchesThroughSpeedFloorAtTouchdown(t *testing.T) {
	// 5 m/s horizontal — comfortably above chaseHorizSpeedFloorMps
	// (0.1) — establishes a real latch.
	c, moon, camFromBody, localUp := touchdownFlareCraft(t, 5.0)
	v := NewLaunchView(launchThemeForTest(), NewOrbitView(launchThemeForTest()))
	axisBefore := v.chaseHAxis(c, moon, camFromBody, localUp)

	// Final flare: horizontal speed nulled to 0.01 m/s, well under the
	// floor. Same craft pointer, same LaunchView — this is what
	// consecutive frames during touchdown look like.
	setAirRelativeFlareVelocity(c, moon, -2, 0.01)
	axisAfter := v.chaseHAxis(c, moon, camFromBody, localUp)

	if dot := axisBefore.Dot(axisAfter); dot < 0.999 {
		t.Errorf("axis moved across the speed-floor dip at touchdown: "+
			"before·after = %.4f (want ~1.0 — should hold, not recompute)", dot)
	}

	// Prove it didn't merely coincide with holding — check it did NOT
	// snap to the (deliberately different) surface-frame-east fallback.
	eastV := chaseSurfaceEastAxis(moon, camFromBody)
	if dot := axisAfter.Dot(eastV); dot > 0.5 {
		t.Errorf("axis after the dip aligned with surface-frame east (dot=%.4f) — "+
			"looks like it snapped to the fallback instead of latching", dot)
	}
}

// TestChaseHAxisDoesNotFlipOnSignReversalOvershoot — issue #380 review
// finding: a braking overshoot that carries horizontal velocity through
// zero to the opposite sign, while staying inside the
// chaseHorizSpeedFloorMps dead zone the whole time, must not flip the
// chase-cam 180°. The latch should hold across the sign change as long
// as the magnitude never re-clears the floor.
func TestChaseHAxisDoesNotFlipOnSignReversalOvershoot(t *testing.T) {
	// +5 m/s horizontal (above floor) establishes the latch.
	c, moon, camFromBody, localUp := touchdownFlareCraft(t, 5.0)
	v := NewLaunchView(launchThemeForTest(), NewOrbitView(launchThemeForTest()))
	axisBefore := v.chaseHAxis(c, moon, camFromBody, localUp)

	// Overshoot: horizontal component reverses sign to -0.05 m/s —
	// magnitude 0.05 < chaseHorizSpeedFloorMps (0.1), so this is still
	// inside the dead zone despite crossing zero.
	setAirRelativeFlareVelocity(c, moon, -2, -0.05)
	axisAfter := v.chaseHAxis(c, moon, camFromBody, localUp)

	if dot := axisBefore.Dot(axisAfter); dot < 0.999 {
		t.Errorf("axis flipped across a sub-floor sign reversal: "+
			"before·after = %.4f (want ~1.0, not ~-1.0)", dot)
	}
}

// TestChaseHAxisResetsOnLiftoffAfterTouchdownLatch — issue #380 review,
// second round: the touchdown latch is right to hold WHILE a vessel
// sits parked (the scene is static, nothing visibly moves), but wrong
// once the SAME vessel relaunches. The early vertical climb's
// horizontal speed stays under chaseHorizSpeedFloorMps for seconds, not
// one tick (TestChaseHAxisStaysEastDuringVerticalClimb measures ~0.014
// m/s there), so without an explicit reset the latch would hold the
// heading from whenever the vessel touched down — an arbitrary
// direction from a previous flight, unrelated to the new ascent — for
// that whole window instead of the surface-frame-east a fresh pad spawn
// shows.
//
// End-to-end sequence pinned here: land travelling in a distinct
// non-east horizontal direction, confirm the latch holds through
// touchdown itself (Landed false->true must NOT reset it — that would
// reintroduce the touchdown mirroring this latch exists to prevent),
// then lift off (Landed true->false) and confirm the still-near-zero
// early-climb horizontal speed now renders surface-frame east rather
// than the stale touchdown heading.
func TestChaseHAxisResetsOnLiftoffAfterTouchdownLatch(t *testing.T) {
	// Distinct, non-east horizontal direction: air-relative velocity
	// along body-frame Z (east sits along Y here — see
	// touchdownFlareCraft's doc comment). 5 m/s, comfortably above the
	// floor, establishes a real latch while still descending.
	c, moon, camFromBody, localUp := touchdownFlareCraft(t, 5.0)
	v := NewLaunchView(launchThemeForTest(), NewOrbitView(launchThemeForTest()))

	axisDescending := v.chaseHAxis(c, moon, camFromBody, localUp)
	eastV := chaseSurfaceEastAxis(moon, camFromBody)
	if dot := axisDescending.Dot(eastV); dot > 0.5 {
		t.Fatalf("setup: descending axis should not already be east-ish (dot=%.4f) — "+
			"the test needs a heading clearly distinguishable from the fallback", dot)
	}

	// Touchdown: Landed flips false -> true (applySurfaceArrival,
	// internal/sim/lifecycle.go). Horizontal air-relative velocity
	// nulls to co-rotating (~0), as a real soft landing leaves it.
	c.Landed = true
	setAirRelativeFlareVelocity(c, moon, 0, 0)
	axisLanded := v.chaseHAxis(c, moon, camFromBody, localUp)
	if dot := axisDescending.Dot(axisLanded); dot < 0.999 {
		t.Errorf("axis moved across touchdown itself: descending·landed = %.4f "+
			"(want ~1.0 — the latch must hold through landing, not reset there)", dot)
	}

	// Liftoff: Landed flips true -> false (StartManualBurn /
	// planted-node ignition, internal/sim/maneuver.go). Horizontal
	// speed during the early vertical climb is integrator noise, well
	// under the floor — same order of magnitude as
	// TestChaseHAxisStaysEastDuringVerticalClimb's ~0.014 m/s and a
	// fresh pad spawn's early frames.
	c.Landed = false
	setAirRelativeFlareVelocity(c, moon, -2, 0.01)
	axisAscending := v.chaseHAxis(c, moon, camFromBody, localUp)

	if dot := axisAscending.Dot(eastV); dot < 0.999 {
		t.Errorf("post-liftoff axis is not surface-frame east: dot(ascending,east) = %.4f "+
			"(want ~1.0 — a relaunch should start from the same fallback a fresh pad "+
			"spawn gets)", dot)
	}
	if dot := axisAscending.Dot(axisLanded); dot > 0.5 {
		t.Errorf("post-liftoff axis still resembles the stale touchdown heading: "+
			"dot(ascending,landed) = %.4f (want small — liftoff should have cleared "+
			"the touchdown latch)", dot)
	}
}

// During vertical climb (Radial+, no pitch trim), the chase-cam's
// horizontal axis must remain body-frame east. v0.11.0 ships with
// epsilon = 1e-9 in chaseHorizontalAxis, which is below the per-tick
// slew lag between CurrentAttitudeDir (snapped to last tick's commanded
// direction) and localUp (= r̂_craft after this tick's integration).
// At Earth's rotation rate, one 50 ms tick separates the two by
// ω·Δt ≈ 3.6e-6 rad — six orders of magnitude above the epsilon — so
// the projection picks up that lag and flips hAxis to the slew-lag
// direction (≈ west during eastward body rotation). Visually, the
// chase-cam reverses east↔west during the first seconds of liftoff
// until the player applies pitch trim. Asserts the bug: hAxis after
// one engine-on tick should still align with body-frame east, not
// some lag-driven horizontal.
func TestChaseHAxisStaysEastDuringVerticalClimb(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	// Engine on. StartManualBurn clears Landed, the integrator takes
	// over, slew advances CurrentAttitudeDir each tick.
	w.StartManualBurn()
	if c.Landed {
		t.Fatal("setup: StartManualBurn did not clear Landed")
	}
	// Advance one physics tick (50 ms base step). The integrator
	// will move r̂_craft slightly east relative to the snapped
	// CurrentAttitudeDir.
	for i := 0; i < 20; i++ { // ~1 sim-second
		w.Tick()
	}

	camFromBody := c.State.R
	localUp := camFromBody.Scale(1.0 / camFromBody.Norm())
	hAxis := chaseHorizontalAxis(c, c.Primary, camFromBody, localUp)

	rR := render.Vec3{X: c.State.R.X, Y: c.State.R.Y, Z: c.State.R.Z}
	eastR := render.BodyFrameEast(c.Primary, rR)
	eastV := orbital.Vec3{X: eastR.X, Y: eastR.Y, Z: eastR.Z}

	dotEast := hAxis.Dot(eastV)
	if dotEast < 0.5 {
		t.Errorf("chase hAxis drifted off body-frame east during vertical climb: "+
			"hAxis·east = %.4f (want > 0.5; negative = the axis flipped west)", dotEast)
	}
}

// LUT sprite stride: the 9-row mobile-launcher must still be visible
// after a few seconds of vertical climb. Slice-2-as-shipped sized each
// sprite cell by `scaleMPerCell` in world (label says "metres per
// cell"), but `scaleMPerCell` is actually m-per-pixel — the canvas
// stores 4 pixels per terminal row × 2 pixels per terminal column.
// At altitude ≥ a few metres the 9-row sprite collapsed into ~2 cells
// of screen Y and disappeared entirely once the rocket climbed past
// it (verified live: Saturn V on KSC, altitude 4 m → zero LUT glyphs
// in the rendered canvas). Regression: at altitude 5 m the rendered
// scene must still contain ≥ 4 tower-spine `║` glyphs and the crown
// `╤` glyph.
func TestLaunchTowerStaysVisibleDuringEarlyAscent(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	// Simulate ~3 m altitude without going through engine ignition
	// (avoids slew + Q + thrust noise muddying the test). Bypass
	// Landed and lift the craft ~3 m radially.
	c.Landed = false
	rNorm := c.State.R.Norm()
	c.State.R = c.State.R.Scale((rNorm + 3.0) / rNorm)

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 200, 50)

	spineCount := strings.Count(out, "║")
	crownCount := strings.Count(out, "╤")
	if spineCount < 4 {
		t.Errorf("tower-spine glyph '║' count at altitude 3 m: got %d, want >= 4 "+
			"(sprite has 6 spine cells × 2 columns; ≥4 should still be on-canvas "+
			"after the auto-scale clamps to 1 m/px floor)", spineCount)
	}
	if crownCount < 1 {
		t.Errorf("crown glyph '╤' count at altitude 3 m: got %d, want >= 1", crownCount)
	}
}

// TestLaunchTowerRecedesAsRocketClimbs — v0.11.3 playtest fix: the
// original LUT row-stride was `scaleMPerPx · canvasCellPxH`, so the
// LUT's world height scaled with the chase-cam autozoom. As the
// rocket gained altitude, scaleMPerPx grew proportionally, the LUT
// grew with it, and the rocket could never clear the tower — the
// rocket-top-altitude < LUT-top-altitude inequality always held
// (LUT-top = 4/3 × rocket-altitude). The user reported "rocket
// doesn't exceed the LUT top until 1000 m or more."
//
// Fixed by pinning the LUT cell stride to a real-world metres
// constant (lutRowHeightM = 7.5 m → ~60 m total tower height,
// stylised from LC-39A's ~135 m crawler tower). Regression
// assertion: at altitude 200 m the rocket is clearly above the
// LUT — concretely, no LUT crown `╤` glyph appears above the
// canvas vertical centre.
func TestLaunchTowerRecedesAsRocketClimbs(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	// Lift the craft to 200 m altitude (well above the 60 m LUT
	// top). Bypass Landed so the chase-cam sees an in-flight craft.
	c.Landed = false
	rNorm := c.State.R.Norm()
	c.State.R = c.State.R.Scale((rNorm + 200) / rNorm)

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	width, height := 120, 40
	out := v.Render(w, width, height)

	// Split into lines; assert no '╤' appears in the upper half
	// (above the canvas vertical centre, where the rocket sits).
	// If the LUT were still scaling with zoom, the crown ╤ would
	// be in the upper portion (LUT top would be at altitude
	// ~4/3 × 200 = 267 m, above the rocket at 200 m, well above
	// canvas centre).
	lines := strings.Split(out, "\n")
	mid := len(lines) / 2
	for i := 0; i < mid; i++ {
		if strings.Contains(lines[i], "╤") {
			t.Errorf("LUT crown '╤' found above canvas centre at line %d (of %d): %q\n"+
				"full render:\n%s",
				i, len(lines), lines[i], out)
			break
		}
	}
}

// Counterpoint to the slew-lag fix: the threshold must remain low
// enough that a real player-applied pitch trim still steers the
// chase-cam. One `>` press = 5° east pitch trim, which puts a 0.087
// east component on commanded attitude (sin 5°) — well above any
// sane slew-lag noise. Assert that with PitchTrim = one step the chase
// hAxis points east, not the fallback default.
func TestChaseHAxisFollowsPitchTrimAfterCommand(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	c.PitchTrim = spacecraft.PitchTrimStepRad // 5° east
	w.StartManualBurn()
	// One tick is enough for slew to advance CurrentAttitudeDir
	// toward the trimmed direction (slew rate is degrees/sec; 1s of
	// ticks covers a full step).
	for i := 0; i < 20; i++ {
		w.Tick()
	}

	camFromBody := c.State.R
	localUp := camFromBody.Scale(1.0 / camFromBody.Norm())
	hAxis := chaseHorizontalAxis(c, c.Primary, camFromBody, localUp)

	rR := render.Vec3{X: c.State.R.X, Y: c.State.R.Y, Z: c.State.R.Z}
	eastR := render.BodyFrameEast(c.Primary, rR)
	eastV := orbital.Vec3{X: eastR.X, Y: eastR.Y, Z: eastR.Z}
	if dotEast := hAxis.Dot(eastV); dotEast < 0.5 {
		t.Errorf("with +5° pitch trim, hAxis should still align with east-ish: "+
			"hAxis·east = %.4f (want > 0.5)", dotEast)
	}
}

// Tower depth-cull: when the camera is on the far hemisphere from the
// launch site, every tower cell sits behind the body and must not
// render. Lift the craft off the pad (Landed=false) and place the
// camera at the antipode of the launch site so the near-hemisphere
// check evaluates negative for every LUT cell. The LUT-unique crown
// glyph `╤` must be absent from the rendered string.
func TestLaunchTowerCulledOnFarHemisphere(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	c.Landed = false // freeze in place — integrator won't re-snap R
	// Camera at the antipode of the launch site, body-relative.
	// Take whatever R the spawn set up (= padFromBody at simTime 0),
	// negate it so the camera points at the far hemisphere.
	c.State.R = c.State.R.Scale(-1)

	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	out := v.Render(w, 120, 40)
	if strings.Contains(out, "╤") {
		t.Errorf("expected no LUT crown glyph on far hemisphere, got:\n%s", out)
	}
}

// TestRocketSpriteShrinksAsAltitudeGrows (v0.11.5-followup): the
// original v0.11.3 cut passed the autozoom m/cell value through as the
// launch-sprite sub-pixel stride, so the rocket occupied the same
// canvas area regardless of altitude — playtester reported "the
// rocket stays super huge as I zoom out". Fix mirrors the LUT
// precedent (commit b73c54b): pin the sub-pixel stride to
// vesselSubPixelM real-world metres. Regression: at altitude 10 km
// (autozoom ≈ 370 m/cell) the rocket sprite must occupy strictly
// fewer canvas-cell rows than it did on the pad. We can't easily count
// rendered sprite pixels, so this test compares a low-zoom render to
// a high-zoom render and asserts the rendered string differs (a
// sprite that doesn't shrink would render identically in the same
// chase-cam frame regardless of altitude).
func TestRocketSpriteShrinksAsAltitudeGrows(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	c.Landed = false
	rNorm := c.State.R.Norm()
	// Low-zoom: 100 m altitude.
	c.State.R = c.State.R.Scale((rNorm + 100) / rNorm)
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	low := v.Render(w, 120, 40)

	// High-zoom: 10 km altitude.
	rNorm = c.State.R.Norm()
	c.State.R = c.State.R.Scale((rNorm + 9900) / rNorm)
	high := v.Render(w, 120, 40)

	if low == high {
		t.Errorf("rocket sprite must shrink (or otherwise differ) between low-zoom and high-zoom — pre-followup bug had identical render at any altitude")
	}
}

// TestLaunchViewRendersRCSPuffs (v0.11.5 sub-scope 5): firing an RCS
// pulse on the active craft must produce a visible change in the
// LaunchView render — i.e. the chase-cam scene picks up the puff
// pixels in the new white two-shade palette. Compares two renders
// (before vs after a pulse); they must differ.
func TestLaunchViewRendersRCSPuffs(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	before := v.Render(w, 120, 40)

	// Switch to RCS mode and fire a prograde pulse.
	c.EngineMode = spacecraft.EngineRCS
	if !w.FireRCSPulse(spacecraft.BurnPrograde) {
		t.Fatalf("FireRCSPulse(prograde) returned false; setup precondition broken")
	}
	if len(w.RCSPuffs()) == 0 {
		t.Fatalf("after FireRCSPulse, w.RCSPuffs() is empty")
	}
	after := v.Render(w, 120, 40)
	if before == after {
		t.Errorf("LaunchView render unchanged after RCS pulse; expected puff pixels to land in scene")
	}
}

// The launch / landing chase-cam must draw the active craft's
// current-orbit ellipse the same way the orbit-map screens do
// (drawOrbitPath → DrawEllipseClass, Real class). The view is built
// with a nil hudSource so the side chips (which echo velocity /
// apoapsis / inclination and would differ on V alone) are suppressed —
// the orbit ellipse on the canvas becomes the only V-dependent output
// (above the atmosphere the HUD strip's Q / vz / downrange are equal).
// With R fixed and only V flipped bound→hyperbolic, the bound render
// (ellipse drawn) must differ from the hyperbolic one (el.A < 0 fails
// the gate, no ellipse).
func TestLaunchViewRendersCurrentOrbit(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 400_000.0 // 400 km LEO, above atmosphere
	vCirc := math.Sqrt(mu / r)

	// Lift the craft off the pad into a clean circular orbit.
	c.Landed = false
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: vCirc}

	th := launchThemeForTest()
	v := NewLaunchView(th, nil) // nil hud → isolate the canvas ellipse
	bound := v.Render(w, 120, 40)

	// Same position, but escape-speed velocity → hyperbolic, no
	// bound ellipse to render.
	c.State.V = orbital.Vec3{Y: 2 * vCirc}
	hyperbolic := v.Render(w, 120, 40)

	if bound == hyperbolic {
		t.Error("LaunchView render identical for bound vs hyperbolic trajectory; current-orbit ellipse not drawn")
	}
}

// A landed craft must not draw its orbit, mirroring the orbit screen's
// activeCraftElements ok=false on Landed. A vessel co-rotating with the
// surface has a degenerate ellipse (apoapsis ≈ body radius) that clears
// the pixel gate at launch zoom, so without the Landed skip it would
// paint a phantom arc. Give a landed craft a genuine bound LEO orbit
// vector and confirm flipping that orbit hyperbolic changes nothing —
// the Landed gate suppresses the ellipse either way. nil hudSource
// suppresses the side chips so the canvas ellipse is the only thing
// that could differ.
func TestLaunchViewLandedOrbitNotDrawn(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	if !c.Landed {
		t.Fatal("setup: pad craft should be Landed")
	}
	mu := c.Primary.GravitationalParameter()
	// Place the (still-Landed) craft above the atmosphere so the HUD
	// strip's dynamic-pressure readout reads Q=0 for both velocities.
	r := c.Primary.RadiusMeters() + 400_000.0
	rHat := c.State.R.Scale(1 / c.State.R.Norm())
	c.State.R = rHat.Scale(r)
	vCirc := math.Sqrt(mu / r)
	// A horizontal velocity at this point → bound orbit if the gate
	// didn't skip Landed craft.
	tangent := orbital.Vec3{Z: 1}.Cross(rHat)
	c.State.V = tangent.Scale(vCirc / tangent.Norm())

	th := launchThemeForTest()
	v := NewLaunchView(th, nil) // nil hud → isolate the canvas ellipse
	bound := v.Render(w, 120, 40)

	c.State.V = tangent.Scale(2 * vCirc / tangent.Norm()) // hyperbolic
	hyper := v.Render(w, 120, 40)

	if bound != hyper {
		t.Error("landed-craft scene changed with orbit shape; orbit drawn despite Landed (phantom arc)")
	}
}

// (TestLaunchOrbitSamplesScalesWithProjectedSize retired with
// launchOrbitSamples — ADR 0042 §3 folded the chase-cam's private sample
// budget into the canvas's shared adaptive flattening. The behaviour it
// guarded is now covered by TestLaunchViewOrbitRendersAsLine below plus
// the widgets-level density tests in arc_test.go.)

// End-to-end: the magnified chase-cam must paint a current-orbit ellipse
// that reads as a dotted line, not just the apo/peri markers. Render a
// low circular orbit (nil hud to isolate the canvas) and confirm the
// orbit colour lands on many distinct cells — far more than the marker
// discs alone (a FillDisk-2 + FillDisk-3 cover only a handful of cells).
func TestLaunchViewOrbitRendersAsLine(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	mu := c.Primary.GravitationalParameter()
	r := c.Primary.RadiusMeters() + 200_000.0 // 200 km LEO
	c.Landed = false
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}

	th := launchThemeForTest()
	v := NewLaunchView(th, nil)
	v.Render(w, 120, 40)

	cells := v.canvas.CountColor(render.ColorCurrentOrbit)
	if cells < 40 {
		t.Errorf("orbit rendered on only %d cells; expected a dotted line (>=40), not just the apsis markers", cells)
	}
}
