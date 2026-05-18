package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const navballEps = 1e-6

// vec3Eq returns true when two vectors agree to within navballEps in
// each component.
func vec3Eq(a, b orbital.Vec3) bool {
	return math.Abs(a.X-b.X) < navballEps &&
		math.Abs(a.Y-b.Y) < navballEps &&
		math.Abs(a.Z-b.Z) < navballEps
}

// TestNavballBasisOrthonormal checks the spawn-state LEO body-frame
// basis is orthonormal: unit axes, zero off-diagonals. The navball
// basis is the craft body frame (EX=nose, EZ=up, EY=right=nose×up),
// so EX×EY = −EZ (i.e. EZ = EY×EX).
func TestNavballBasisOrthonormal(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	basis, ok := w.NavballBasis()
	if !ok {
		t.Fatal("NavballBasis: ok=false on a fresh LEO craft")
	}
	for name, pair := range map[string][2]orbital.Vec3{
		"EX·EX": {basis.EX, basis.EX},
		"EY·EY": {basis.EY, basis.EY},
		"EZ·EZ": {basis.EZ, basis.EZ},
	} {
		if d := pair[0].Dot(pair[1]); math.Abs(d-1) > navballEps {
			t.Errorf("%s = %g, want 1", name, d)
		}
	}
	for name, pair := range map[string][2]orbital.Vec3{
		"EX·EY": {basis.EX, basis.EY},
		"EX·EZ": {basis.EX, basis.EZ},
		"EY·EZ": {basis.EY, basis.EZ},
	} {
		if d := pair[0].Dot(pair[1]); math.Abs(d) > navballEps {
			t.Errorf("%s = %g, want 0", name, d)
		}
	}
	// Body frame: right = up×nose ⇒ right-handed, EX×EY = EZ.
	if cross := basis.EX.Cross(basis.EY); !vec3Eq(cross, basis.EZ) {
		t.Errorf("EX × EY = %v, want EZ = %v", cross, basis.EZ)
	}
}

// TestNavballBasisBodyFrame: on a fresh LEO craft (default
// AttitudeMode = prograde, roll 0, pre-first-tick so the commanded
// nose is used) the body-frame basis is EX = nose = +v̂ and EZ = body
// up = the heads-up reference = radial-out r̂ (⟂ the prograde nose in
// a circular orbit). NavMode does not change the basis.
func TestNavballBasisBodyFrame(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	basis, ok := w.NavballBasis()
	if !ok {
		t.Fatal("NavballBasis: ok=false")
	}
	progradeUnit := c.State.V.Scale(1 / c.State.V.Norm())
	if !vec3Eq(basis.EX, progradeUnit) {
		t.Errorf("EX (nose) = %v, want prograde %v", basis.EX, progradeUnit)
	}
	radialOut := c.State.R.Scale(1 / c.State.R.Norm())
	if !vec3Eq(basis.EZ, radialOut) {
		t.Errorf("EZ (body up) = %v, want radial-out %v", basis.EZ, radialOut)
	}
}

// TestNavballSubObserverProjectsCardinals checks the cardinal-axis
// projection in the body-frame basis. Default LEO craft: nose = +v̂
// (prograde), body up = r̂ (radial-out), right = v̂×r̂ = −normal. So:
//   - prograde   → (0,   0)     (the nose = disc centre)
//   - retrograde → (0, ±180)
//   - radial-out → (+90, _)     (body up = the +lat pole)
//   - radial-in  → (−90, _)
//   - normal±    → (0, ∓90)     (on the equator, ±90 lon)
func TestNavballSubObserverProjectsCardinals(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	basis, ok := w.NavballBasis()
	if !ok {
		t.Fatal("NavballBasis: ok=false")
	}
	progradeDir := spacecraft.DirectionUnit(spacecraft.BurnPrograde, c.State.R, c.State.V)
	retrogradeDir := spacecraft.DirectionUnit(spacecraft.BurnRetrograde, c.State.R, c.State.V)
	normalPlus := spacecraft.DirectionUnit(spacecraft.BurnNormalPlus, c.State.R, c.State.V)
	normalMinus := spacecraft.DirectionUnit(spacecraft.BurnNormalMinus, c.State.R, c.State.V)
	radialOut := spacecraft.DirectionUnit(spacecraft.BurnRadialOut, c.State.R, c.State.V)
	radialIn := spacecraft.DirectionUnit(spacecraft.BurnRadialIn, c.State.R, c.State.V)

	if lat, lon := basis.SubObserver(progradeDir); math.Abs(lat) > 1e-4 || math.Abs(lon) > 1e-4 {
		t.Errorf("prograde (nose) → (%g,%g), want (0,0)", lat, lon)
	}
	if lat, lon := basis.SubObserver(retrogradeDir); math.Abs(lat) > 1e-4 || math.Abs(math.Abs(lon)-180) > 1e-4 {
		t.Errorf("retrograde → (%g,%g), want (0,±180)", lat, lon)
	}
	if lat, _ := basis.SubObserver(radialOut); math.Abs(lat-90) > 1e-4 {
		t.Errorf("radial-out lat = %g, want +90 (body-up pole)", lat)
	}
	if lat, _ := basis.SubObserver(radialIn); math.Abs(lat+90) > 1e-4 {
		t.Errorf("radial-in lat = %g, want −90", lat)
	}
	latNP, lonNP := basis.SubObserver(normalPlus)
	latNM, lonNM := basis.SubObserver(normalMinus)
	if math.Abs(latNP) > 1e-4 || math.Abs(math.Abs(lonNP)-90) > 1e-4 {
		t.Errorf("normal+ → (%g,%g), want (0,±90)", latNP, lonNP)
	}
	if math.Abs(latNM) > 1e-4 || math.Abs(math.Abs(lonNM)-90) > 1e-4 {
		t.Errorf("normal- → (%g,%g), want (0,±90)", latNM, lonNM)
	}
	if math.Abs(lonNP+lonNM) > 1e-4 { // antipodal: +90 / −90
		t.Errorf("normal± should be antipodal in lon; got %g, %g", lonNP, lonNM)
	}
}

// TestNavballSubObserverNoseDirection: when the craft's AttitudeMode
// is BurnPrograde in NavOrbit, NavballSubObserver returns (0, 0) — the
// nose marker sits at the disk centre.
func TestNavballSubObserverNoseDirection(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavOrbit
	w.ActiveCraft().AttitudeMode = spacecraft.BurnPrograde
	lat, lon, ok := w.NavballSubObserver()
	if !ok {
		t.Fatal("NavballSubObserver: ok=false")
	}
	if math.Abs(lat) > 1e-4 || math.Abs(lon) > 1e-4 {
		t.Errorf("nose pointing prograde should map to (0, 0); got (%g, %g)", lat, lon)
	}
}

// TestNavballSubObserverIsAlwaysCentre: the basis is the body frame
// (EX = nose), so the disc centre is always the nose — (0, 0) —
// regardless of which way the craft holds attitude.
func TestNavballSubObserverIsAlwaysCentre(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	for _, mode := range []spacecraft.BurnMode{spacecraft.BurnPrograde, spacecraft.BurnRetrograde, spacecraft.BurnNormalPlus} {
		w.ActiveCraft().AttitudeMode = mode
		lat, lon, ok := w.NavballSubObserver()
		if !ok || math.Abs(lat) > 1e-9 || math.Abs(lon) > 1e-9 {
			t.Errorf("mode %v: sub-observer = (%g,%g,%v), want (0,0,true)", mode, lat, lon, ok)
		}
	}
}

// TestNavballBasisDegenerate: zero-velocity craft (e.g. fresh
// launchpad spawn before liftoff) returns ok=false.
func TestNavballBasisDegenerateZeroV(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ActiveCraft().State.V = orbital.Vec3{}
	if _, ok := w.NavballBasis(); ok {
		t.Error("NavballBasis: want ok=false for zero-velocity craft")
	}
}

// TestNavballMarkersOrbitMode: a fresh LEO craft in NavOrbit produces
// six markers (prograde, retrograde, normal±, radial±) at the
// expected (lat, lon) cardinals.
func TestNavballMarkersOrbitMode(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavOrbit
	got := w.NavballMarkers()
	if len(got) != 6 {
		t.Fatalf("orbit-mode marker count = %d, want 6", len(got))
	}
	byGlyph := map[rune]int{}
	for i, m := range got {
		byGlyph[m.Glyph] = i
	}
	// Body frame (nose=prograde, up=radial-out): prograde at centre,
	// retrograde antipodal, radial-out/in at the ±lat poles, normal±
	// on the equator at ±90 lon.
	if m := got[byGlyph[NavballGlyphPrograde]]; math.Abs(m.LatDeg) > 1e-4 || math.Abs(m.LonDeg) > 1e-4 {
		t.Errorf("prograde → (%g,%g), want (0,0)", m.LatDeg, m.LonDeg)
	}
	if m := got[byGlyph[NavballGlyphRetrograde]]; math.Abs(m.LatDeg) > 1e-4 || math.Abs(math.Abs(m.LonDeg)-180) > 1e-4 {
		t.Errorf("retrograde → (%g,%g), want (0,±180)", m.LatDeg, m.LonDeg)
	}
	if m := got[byGlyph[NavballGlyphRadialOut]]; math.Abs(m.LatDeg-90) > 1e-4 {
		t.Errorf("radial-out lat = %g, want +90", m.LatDeg)
	}
	if m := got[byGlyph[NavballGlyphRadialIn]]; math.Abs(m.LatDeg+90) > 1e-4 {
		t.Errorf("radial-in lat = %g, want −90", m.LatDeg)
	}
	np := got[byGlyph[NavballGlyphNormalPlus]]
	nm := got[byGlyph[NavballGlyphNormalMinus]]
	if math.Abs(np.LatDeg) > 1e-4 || math.Abs(math.Abs(np.LonDeg)-90) > 1e-4 {
		t.Errorf("normal+ → (%g,%g), want (0,±90)", np.LatDeg, np.LonDeg)
	}
	if math.Abs(nm.LatDeg) > 1e-4 || math.Abs(np.LonDeg+nm.LonDeg) > 1e-4 {
		t.Errorf("normal- → (%g,%g), want antipodal to normal+", nm.LatDeg, nm.LonDeg)
	}
}

// TestNavballMarkersTargetModeRadialSwapsToTarget: when a craft
// target is bound and NavMode is NavTarget, the marker set is still
// six cardinals — but the radial+ / radial- pair swaps to target /
// anti-target glyphs (◉ ◌) and points along the line-to-target
// instead of along the orbit-frame radial. This matches
// ResolveAttitudeIntent's NavTarget remap (radial keys → BurnTarget /
// BurnAntiTarget) so each navball glyph sits at the direction its
// axis key would aim.
func TestNavballMarkersTargetModeRadialSwapsToTarget(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	target := *active
	target.State.R = active.State.R.Add(orbital.Vec3{X: 1000, Y: 0, Z: 0})
	target.State.V = active.State.V.Add(orbital.Vec3{X: 0, Y: 1, Z: 0})
	target.Name = "target-test"
	w.Crafts = append(w.Crafts, &target)
	w.Target.Kind = TargetCraft
	w.Target.CraftIdx = len(w.Crafts) - 1
	w.NavMode = NavTarget

	got := w.NavballMarkers()
	if len(got) != 6 {
		t.Fatalf("target-mode marker count = %d, want 6", len(got))
	}
	byGlyph := map[rune]render.NavballMarker{}
	for _, m := range got {
		byGlyph[m.Glyph] = m
	}
	if _, ok := byGlyph[NavballGlyphTarget]; !ok {
		t.Errorf("missing target glyph %c (radial+ swap)", NavballGlyphTarget)
	}
	if _, ok := byGlyph[NavballGlyphAntiTarget]; !ok {
		t.Errorf("missing anti-target glyph %c (radial- swap)", NavballGlyphAntiTarget)
	}
	// The radial-diamond glyphs should NOT be in target-mode output.
	if _, ok := byGlyph[NavballGlyphRadialOut]; ok {
		t.Errorf("orbit-frame radial+ glyph %c should not appear in target mode", NavballGlyphRadialOut)
	}
	if _, ok := byGlyph[NavballGlyphRadialIn]; ok {
		t.Errorf("orbit-frame radial- glyph %c should not appear in target mode", NavballGlyphRadialIn)
	}
}

// TestNavballMarkersTargetModeProgradeMatchesTargetVelocity: in
// NavTarget mode, EX is the target-relative-velocity direction, and
// the prograde marker direction is the same — so the marker projects
// to (lat≈0, lon≈0), the disk centre.
func TestNavballMarkersTargetModeProgradeIsTargetRelative(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	active := w.ActiveCraft()
	target := *active
	target.State.R = active.State.R.Add(orbital.Vec3{X: 1000, Y: 0, Z: 0})
	target.State.V = active.State.V.Add(orbital.Vec3{X: 0, Y: 1, Z: 0})
	target.Name = "target-test"
	w.Crafts = append(w.Crafts, &target)
	w.Target.Kind = TargetCraft
	w.Target.CraftIdx = len(w.Crafts) - 1
	w.NavMode = NavTarget

	for _, m := range w.NavballMarkers() {
		if m.Glyph != NavballGlyphPrograde {
			continue
		}
		if math.Abs(m.LatDeg) > 1e-4 || math.Abs(m.LonDeg) > 1e-4 {
			t.Errorf("target-mode prograde lands at (%g, %g), want (0, 0)", m.LatDeg, m.LonDeg)
		}
		return
	}
	t.Errorf("missing prograde marker in target mode")
}

// TestNavballMarkersSurfaceModeLocalHorizon: NavSurface is a local-
// horizon sphere (sky pole = local up). Radial-out must read at the
// sky pole (lat +90), radial-in at the nadir (lat −90), and surface-
// prograde on the horizon band (lat ≈ 0) — not re-centred on prograde
// the way the pre-v0.10 orbital-normal-pole basis did. This is the
// "launching shows the sky, not the horizon" fix.
func TestNavballMarkersSurfaceModeLocalHorizon(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavSurface
	got := w.NavballMarkers()

	find := func(glyph rune) (render.NavballMarker, bool) {
		for _, m := range got {
			if m.Glyph == glyph {
				return m, true
			}
		}
		return render.NavballMarker{}, false
	}

	if m, ok := find(NavballGlyphRadialOut); !ok {
		t.Error("missing radial-out marker in surface mode")
	} else if math.Abs(m.LatDeg-90) > 1e-3 {
		t.Errorf("radial-out should sit at the sky pole (lat +90); got lat=%g", m.LatDeg)
	}

	if m, ok := find(NavballGlyphRadialIn); !ok {
		t.Error("missing radial-in marker in surface mode")
	} else if math.Abs(m.LatDeg+90) > 1e-3 {
		t.Errorf("radial-in should sit at the nadir (lat −90); got lat=%g", m.LatDeg)
	}

	if m, ok := find(NavballGlyphPrograde); !ok {
		t.Error("missing prograde marker in surface mode")
	} else if math.Abs(m.LatDeg) > 1.0 {
		t.Errorf("surface-prograde should ride the horizon (lat ≈ 0); got lat=%g", m.LatDeg)
	}
}

// TestNavballMarkersIncludeNode: a planted BurnPrograde node adds
// one extra marker (◎) to the cardinal set, projected to (0, 0)
// since the burn direction matches the orbit-frame prograde basis EX.
func TestNavballMarkersIncludeNode(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavOrbit
	c := w.ActiveCraft()
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode: spacecraft.BurnPrograde,
		DV:   100,
	})
	got := w.NavballMarkers()
	var node *render.NavballMarker
	for i := range got {
		if got[i].Glyph == NavballGlyphNode {
			node = &got[i]
			break
		}
	}
	if node == nil {
		t.Fatalf("expected node marker %c in output", NavballGlyphNode)
	}
	if math.Abs(node.LatDeg) > 1e-4 || math.Abs(node.LonDeg) > 1e-4 {
		t.Errorf("BurnPrograde node should project to (0, 0); got (%g, %g)", node.LatDeg, node.LonDeg)
	}
}

// TestNavballSurfaceEastIsScreenRight: on the launchpad the nose
// points radial-out and roll is 0 (heads-up). The body-frame navball
// is fed (0,0), so screen-right ∝ d·EY and screen-up ∝ d·EZ. With
// heads-up roll the world East must read screen-right, West left,
// North up, and the nose (radial-out) sits at the disc centre — the
// behaviour the player asked for, now singularity-free.
func TestNavballSurfaceEastIsScreenRight(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavSurface
	c, err := w.SpawnCraft(SpawnSpec{
		LoadoutID: spacecraft.LoadoutSaturnVID, ParentBodyID: "earth",
		Launchpad: true, Latitude: 28.6083, LongitudeOffset: -80.604,
	})
	if err != nil {
		t.Fatalf("SpawnCraft: %v", err)
	}
	basis, ok := w.NavballBasis()
	if !ok {
		t.Fatal("NavballBasis: ok=false on the pad")
	}
	if sl, so, sok := w.NavballSubObserver(); !sok || math.Abs(sl) > 1e-9 || math.Abs(so) > 1e-9 {
		t.Fatalf("body-frame sub-observer = (%g,%g,%v), want (0,0,true)", sl, so, sok)
	}

	// Body-frame navball fed (0,0): screen-right = d·EY, up = d·EZ.
	screen := func(dir orbital.Vec3) (right, up float64) {
		return dir.Dot(basis.EY), dir.Dot(basis.EZ)
	}

	upv := c.State.R.Scale(1 / c.State.R.Norm()) // radial-out = nose
	spinR := render.BodyRotationAxisWorld(c.Primary)
	spinAxis := orbital.Vec3{X: spinR.X, Y: spinR.Y, Z: spinR.Z}
	east := spinAxis.Cross(upv)
	east = east.Scale(1 / east.Norm())

	if r, u := screen(east); r <= 0 || math.Abs(u) > 1e-6 {
		t.Errorf("East should be screen-right; got right=%.3f up=%.3f", r, u)
	}
	if r, _ := screen(east.Scale(-1)); r >= 0 {
		t.Errorf("West should be screen-left; got right=%.3f", r)
	}
	if r, u := screen(basis.EX); math.Abs(r) > 1e-6 || math.Abs(u) > 1e-6 {
		t.Errorf("nose should be the disc centre; got right=%.3f up=%.3f", r, u)
	}

	// Roll 90° must rotate the ball: East moves off screen-right.
	c.CommandedRollDeg = 90
	c.CurrentRollDeg = 90
	rb, _ := w.NavballBasis()
	if r := east.Dot(rb.EY); r > 0.2 {
		t.Errorf("after 90° roll East should leave screen-right; got right=%.3f", r)
	}
}

// TestNavballMarkersSurfaceCompassTicks: in NavSurface mode the
// marker set includes four compass ticks (N / E / S / W) painted in
// the grid color, in addition to the cardinal SAS markers. NavOrbit
// mode produces no compass ticks.
func TestNavballMarkersSurfaceCompassTicks(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavSurface
	got := w.NavballMarkers()
	have := map[rune]bool{}
	for _, m := range got {
		have[m.Glyph] = true
	}
	for _, c := range []rune{'N', 'E', 'S', 'W'} {
		if !have[c] {
			t.Errorf("surface mode missing compass tick %c", c)
		}
	}

	// NavOrbit must not paint compass ticks.
	w.NavMode = NavOrbit
	for _, m := range w.NavballMarkers() {
		switch m.Glyph {
		case 'N', 'E', 'S', 'W':
			t.Errorf("orbit mode should not paint compass tick %c", m.Glyph)
		}
	}
}

// TestNavballMarkersStaleTargetNodeSkips: a target-relative node
// whose target binding is empty produces no direction (BurnDirection
// returns zero) and is silently skipped from the marker set.
func TestNavballMarkersStaleTargetNodeSkips(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavOrbit
	c := w.ActiveCraft()
	// Target node without a target bound at the world level — the
	// node's BurnDirectionWithTarget call will see zero rT, vT and
	// return zero. Marker is skipped.
	c.Nodes = append(c.Nodes, spacecraft.ManeuverNode{
		Mode: spacecraft.BurnTarget,
		DV:   50,
	})
	for _, m := range w.NavballMarkers() {
		if m.Glyph == NavballGlyphNode {
			t.Errorf("stale target-mode node should not produce marker; got %v", m)
		}
	}
}

// TestNavballMarkersDegenerateReturnsNil: zero-V craft → no basis →
// nil markers (callers degrade to a static / blank navball).
func TestNavballMarkersDegenerateReturnsNil(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.ActiveCraft().State.V = orbital.Vec3{}
	if got := w.NavballMarkers(); got != nil {
		t.Errorf("degenerate basis should produce nil markers, got %d", len(got))
	}
}

// TestNavballBasisTargetMissingFallsBackToOrbit: NavTarget without a
// craft target should silently fall back to the NavOrbit basis (per
// the same self-healing contract as ResolveAttitudeIntent + the SAS
// hold path).
func TestNavballBasisTargetMissingFallsBackToOrbit(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavTarget
	// No craft target set → reconcileNavMode hasn't been called yet,
	// but NavballBasis still has to handle the inconsistent state
	// without panicking and produce the orbit basis.
	w.Target.Kind = TargetNone
	basis, ok := w.NavballBasis()
	if !ok {
		t.Fatal("NavballBasis: ok=false despite NavOrbit fallback")
	}
	c := w.ActiveCraft()
	progradeUnit := c.State.V.Scale(1 / c.State.V.Norm())
	if !vec3Eq(basis.EX, progradeUnit) {
		t.Errorf("EX = %v, want orbit-mode prograde %v", basis.EX, progradeUnit)
	}
}
