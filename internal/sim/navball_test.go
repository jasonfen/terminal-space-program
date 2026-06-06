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

// TestNavballBasisOrthonormal checks the spawn-state LEO basis is
// orthonormal and right-handed: EX·EX = EY·EY = EZ·EZ = 1, off-
// diagonals zero, EZ = EX × EY.
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
	cross := basis.EX.Cross(basis.EY)
	if !vec3Eq(cross, basis.EZ) {
		t.Errorf("EX × EY = %v, want EZ = %v", cross, basis.EZ)
	}
}

// TestNavballBasisOrbitMode checks NavOrbit produces EX = +v̂ and
// EZ = (r × v)̂ on the spawn-state LEO craft.
func TestNavballBasisOrbitMode(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavOrbit
	c := w.ActiveCraft()
	basis, ok := w.NavballBasis()
	if !ok {
		t.Fatal("NavballBasis: ok=false")
	}
	progradeUnit := c.State.V.Scale(1 / c.State.V.Norm())
	if !vec3Eq(basis.EX, progradeUnit) {
		t.Errorf("EX = %v, want prograde %v", basis.EX, progradeUnit)
	}
	hUnit := c.State.R.Cross(c.State.V)
	hUnit = hUnit.Scale(1 / hUnit.Norm())
	if !vec3Eq(basis.EZ, hUnit) {
		t.Errorf("EZ = %v, want orbital normal %v", basis.EZ, hUnit)
	}
}

// TestNavballSubObserverProjectsCardinals checks the cardinal-axis
// projection table for an orbit-mode basis. With EX = prograde,
// EZ = normal+, EY = EZ × EX:
//   - prograde         → (0,    0)
//   - retrograde       → (0,  ±180)
//   - normal+          → (90,   _)
//   - normal-          → (-90,  _)
//   - radial-related   → (0, ±90)
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

	checks := []struct {
		name     string
		dir      orbital.Vec3
		wantLat  float64
		wantLon  float64
		ignoreLon bool // true for poles where lon is undefined
	}{
		{"prograde", progradeDir, 0, 0, false},
		{"retrograde", retrogradeDir, 0, 180, true /* ±180 both valid */},
		{"normal+", normalPlus, 90, 0, true},
		{"normal-", normalMinus, -90, 0, true},
		{"radial-related ±90 lon", radialOut, 0, 0, false},
		{"radial-related ∓90 lon", radialIn, 0, 0, false},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			lat, lon := basis.SubObserver(tc.dir)
			if math.Abs(lat-tc.wantLat) > 1e-4 {
				t.Errorf("lat = %g, want %g", lat, tc.wantLat)
			}
			switch tc.name {
			case "retrograde":
				absLon := math.Abs(lon)
				if math.Abs(absLon-180) > 1e-4 {
					t.Errorf("|lon| = %g, want 180", absLon)
				}
			case "radial-related ±90 lon", "radial-related ∓90 lon":
				absLon := math.Abs(lon)
				if math.Abs(absLon-90) > 1e-4 {
					t.Errorf("|lon| = %g, want 90", absLon)
				}
			default:
				if !tc.ignoreLon && math.Abs(lon-tc.wantLon) > 1e-4 {
					t.Errorf("lon = %g, want %g", lon, tc.wantLon)
				}
			}
		})
	}
	// radialOut and radialIn must land on opposite longitudes.
	latOut, lonOut := basis.SubObserver(radialOut)
	latIn, lonIn := basis.SubObserver(radialIn)
	if math.Abs(latOut) > 1e-4 || math.Abs(latIn) > 1e-4 {
		t.Errorf("radial markers should sit on equator (lat≈0); got %g, %g", latOut, latIn)
	}
	wrap := lonOut + lonIn
	for wrap > 180 {
		wrap -= 360
	}
	for wrap < -180 {
		wrap += 360
	}
	if math.Abs(wrap) > 1e-4 && math.Abs(math.Abs(wrap)-360) > 1e-4 {
		t.Errorf("radial+ and radial- should be antipodal in lon; got %g + %g = %g", lonOut, lonIn, wrap)
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

// TestNavballSubObserverNoseRetrograde: when the craft holds
// retrograde, the nose maps to (0, ±180).
func TestNavballSubObserverNoseRetrograde(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.NavMode = NavOrbit
	w.ActiveCraft().AttitudeMode = spacecraft.BurnRetrograde
	lat, lon, ok := w.NavballSubObserver()
	if !ok {
		t.Fatal("NavballSubObserver: ok=false")
	}
	if math.Abs(lat) > 1e-4 {
		t.Errorf("retrograde lat = %g, want 0", lat)
	}
	if math.Abs(math.Abs(lon)-180) > 1e-4 {
		t.Errorf("retrograde |lon| = %g, want 180", math.Abs(lon))
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
	checks := []struct {
		glyph    rune
		wantLat  float64
		wantLon  float64
		ignoreLon bool
	}{
		{NavballGlyphPrograde, 0, 0, false},
		{NavballGlyphRetrograde, 0, 180, true},
		{NavballGlyphNormalPlus, 90, 0, true},
		{NavballGlyphNormalMinus, -90, 0, true},
		{NavballGlyphRadialOut, 0, 0, true},  // ±90 lon
		{NavballGlyphRadialIn, 0, 0, true},
	}
	for _, c := range checks {
		idx, ok := byGlyph[c.glyph]
		if !ok {
			t.Errorf("missing marker glyph %c", c.glyph)
			continue
		}
		m := got[idx]
		if math.Abs(m.LatDeg-c.wantLat) > 1e-4 {
			t.Errorf("%c lat = %g, want %g", c.glyph, m.LatDeg, c.wantLat)
		}
		if !c.ignoreLon && math.Abs(m.LonDeg-c.wantLon) > 1e-4 {
			t.Errorf("%c lon = %g, want %g", c.glyph, m.LonDeg, c.wantLon)
		}
	}
	// retrograde lon must be ±180 exactly.
	if m := got[byGlyph[NavballGlyphRetrograde]]; math.Abs(math.Abs(m.LonDeg)-180) > 1e-4 {
		t.Errorf("retrograde |lon| = %g, want 180", math.Abs(m.LonDeg))
	}
	// radial markers sit on the equator with antipodal lons.
	rOut := got[byGlyph[NavballGlyphRadialOut]]
	rIn := got[byGlyph[NavballGlyphRadialIn]]
	if math.Abs(rOut.LatDeg) > 1e-4 || math.Abs(rIn.LatDeg) > 1e-4 {
		t.Errorf("radial markers should be on equator, got %g and %g", rOut.LatDeg, rIn.LatDeg)
	}
	if math.Abs(math.Abs(rOut.LonDeg)-90) > 1e-4 {
		t.Errorf("radialOut |lon| = %g, want 90", math.Abs(rOut.LonDeg))
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
	target.ID = 0 // shallow copy inherited active's ID; mint a fresh one
	w.Crafts = append(w.Crafts, &target)
	w.stampCraftID(&target)
	w.Target.Kind = TargetCraft
	w.Target.CraftID = target.ID
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
	target.ID = 0 // shallow copy inherited active's ID; mint a fresh one
	w.Crafts = append(w.Crafts, &target)
	w.stampCraftID(&target)
	w.Target.Kind = TargetCraft
	w.Target.CraftID = target.ID
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
