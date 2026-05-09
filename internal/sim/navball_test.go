package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
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

// TestNavballMarkersTargetModeAddsTargetMarkers: when a craft target
// is bound and NavMode is NavTarget, the marker set includes target +
// anti-target glyphs in addition to the six orbit cardinals.
func TestNavballMarkersTargetModeAddsTargetMarkers(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	// Spawn a second craft to target. Reuse the active craft's primary
	// + state and offset slightly so the relative-position vector is
	// non-zero.
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
	if len(got) != 8 {
		t.Fatalf("target-mode marker count = %d, want 8 (6 cardinals + target/anti-target)", len(got))
	}
	hasTarget := false
	hasAnti := false
	for _, m := range got {
		if m.Glyph == NavballGlyphTarget {
			hasTarget = true
		}
		if m.Glyph == NavballGlyphAntiTarget {
			hasAnti = true
		}
	}
	if !hasTarget {
		t.Errorf("missing target glyph %c", NavballGlyphTarget)
	}
	if !hasAnti {
		t.Errorf("missing anti-target glyph %c", NavballGlyphAntiTarget)
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
