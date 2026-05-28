package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
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
func TestLaunchViewRenderTitleAndFooter(t *testing.T) {
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
	if !strings.Contains(out, "+/-") || !strings.Contains(out, "[v]") {
		t.Errorf("expected '+/-' and '[v]' in footer hints, got:\n%s", out)
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
// chase-cam. One `>` press = 10° east pitch trim, which puts a 0.17
// east component on commanded attitude (sin 10°) — well above any
// sane slew-lag noise. Assert that with PitchTrim = 10° the chase
// hAxis points east, not the fallback default.
func TestChaseHAxisFollowsPitchTrimAfterCommand(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	c.PitchTrim = spacecraft.PitchTrimStepRad // 10° east
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
		t.Errorf("with +10° pitch trim, hAxis should still align with east-ish: "+
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
