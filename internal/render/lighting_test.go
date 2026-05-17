package render

import (
	"math"
	"math/rand"
	"strconv"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// channelSum parses "#RRGGBB" and returns R+G+B (0..765). Test-only
// brightness proxy for "is this color darker".
func channelSum(t *testing.T, c lipgloss.Color) int {
	t.Helper()
	s := string(c)
	if len(s) != 7 || s[0] != '#' {
		t.Fatalf("not a hex color: %q", s)
	}
	r, _ := strconv.ParseUint(s[1:3], 16, 8)
	g, _ := strconv.ParseUint(s[3:5], 16, 8)
	b, _ := strconv.ParseUint(s[5:7], 16, 8)
	return int(r) + int(g) + int(b)
}

func TestShade(t *testing.T) {
	cases := []struct {
		in   lipgloss.Color
		f    float64
		want lipgloss.Color
	}{
		{"#808080", 0.5, "#404040"},
		{"#FFFFFF", 1, "#FFFFFF"},
		{"#ABCDEF", 0, "#000000"},
		{"#80FF40", 2, "#80FF40"},   // f clamped to 1 → identity
		{"#102030", -1, "#000000"},  // f clamped to 0
		{"red", 0.5, "red"},         // non-hex → unchanged
		{"#GGGGGG", 0.5, "#GGGGGG"}, // malformed hex → unchanged
		{"#FFF", 0.5, "#FFF"},       // wrong length → unchanged
	}
	for _, tc := range cases {
		if got := Shade(tc.in, tc.f); got != tc.want {
			t.Errorf("Shade(%q, %v) = %q, want %q", tc.in, tc.f, got, tc.want)
		}
	}
}

func TestSolarLightSubSolarFull(t *testing.T) {
	// Camera and Sun both looking at (lat0, lon0); the disk-center
	// pixel is the sub-solar point → full illumination (factor 1).
	l := &SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 0}
	got := l.FactorAt(0, 0, 32, 0, 0)
	if math.Abs(got-1) > 1e-6 {
		t.Errorf("sub-solar pixel factor = %v, want 1", got)
	}
}

func TestSolarLightAntiSolarFloor(t *testing.T) {
	// Sun on the far side (sub-solar lon 180°); the camera-center
	// pixel is the anti-solar point → night floor.
	l := &SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 180}
	got := l.FactorAt(0, 0, 32, 0, 0)
	if math.Abs(got-nightFloor) > 1e-6 {
		t.Errorf("anti-solar pixel factor = %v, want nightFloor %v", got, nightFloor)
	}
}

func TestSolarLightTerminatorMonotonic(t *testing.T) {
	// Sub-solar at the +x limb (lon 90°). Sweeping dx from −r..+r at
	// dy=0 walks lon −90°..+90°, i.e. night → day. Factor must be
	// monotonically non-decreasing — catches sign flips / mirrored
	// terminators.
	const r = 32
	l := &SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 90}
	prev := -1.0
	for dx := -r; dx <= r; dx++ {
		f := l.FactorAt(dx, 0, r, 0, 0)
		if f < prev-1e-9 {
			t.Fatalf("factor not monotonic at dx=%d: %v < prev %v", dx, f, prev)
		}
		prev = f
	}
}

func TestSolarLightFactorRange(t *testing.T) {
	const r = 32
	l := &SolarLight{SubSolarLatDeg: 12, SubSolarLonDeg: -47, EclipseFactor: 1}
	rng := rand.New(rand.NewSource(1))
	for i := 0; i < 500; i++ {
		dx := rng.Intn(2*r+1) - r
		dy := rng.Intn(2*r+1) - r
		if dx*dx+dy*dy > r*r {
			continue
		}
		f := l.FactorAt(dx, dy, r, 0, 0)
		if f < umbraFloor-1e-9 || f > 1+1e-9 {
			t.Fatalf("factor out of [%v,1] at (%d,%d): %v", umbraFloor, dx, dy, f)
		}
	}
}

func TestSolarLightEclipseMultiplies(t *testing.T) {
	// A non-trivial eclipse factor must pull every pixel at or below
	// its no-eclipse value (clamped to umbraFloor).
	const r = 32
	lit := &SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 0, EclipseFactor: 1}
	ecl := &SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 0, EclipseFactor: 0.1}
	for _, dx := range []int{-20, -8, 0, 8, 20} {
		a := lit.FactorAt(dx, 0, r, 0, 0)
		b := ecl.FactorAt(dx, 0, r, 0, 0)
		if b > a+1e-9 {
			t.Errorf("eclipsed factor %v > lit %v at dx=%d", b, a, dx)
		}
		if b < umbraFloor-1e-9 {
			t.Errorf("eclipsed factor %v below umbraFloor at dx=%d", b, dx)
		}
	}
}

func TestTextureForShadesNightSide(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", BodyType: "Planet"}
	const r = 32
	// Sub-solar on the far side → the camera-center pixel is night.
	light := &SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 180, EclipseFactor: 1}
	shaded := TextureFor(earth, r, 0, 0, light)
	if shaded == nil {
		t.Fatal("earth texture nil")
	}
	bare := EarthPixelColor(0, 0, r, 0, 0)
	got := shaded(0, 0, r)
	if channelSum(t, got) >= channelSum(t, bare) {
		t.Errorf("night-side pixel %q not darker than bare %q", got, bare)
	}
}

func TestTextureForSunExempt(t *testing.T) {
	sun := bodies.CelestialBody{ID: "sun", BodyType: "Star"}
	const r = 32
	light := &SolarLight{SubSolarLatDeg: 0, SubSolarLonDeg: 180, EclipseFactor: 0.1}
	tex := TextureFor(sun, r, 0, 0, light)
	if tex == nil {
		t.Fatal("sun texture nil")
	}
	for _, p := range [][2]int{{0, 0}, {8, -4}, {-10, 6}} {
		if got, want := tex(p[0], p[1], r), SunPixelColor(p[0], p[1], r, 0, 0); got != want {
			t.Errorf("sun pixel (%d,%d) shaded: got %q want %q", p[0], p[1], got, want)
		}
	}
}

func TestTextureForNilLightUnshaded(t *testing.T) {
	earth := bodies.CelestialBody{ID: "earth", BodyType: "Planet"}
	const r = 32
	tex := TextureFor(earth, r, 0, 0, nil)
	if tex == nil {
		t.Fatal("earth texture nil")
	}
	for _, p := range [][2]int{{5, 5}, {-12, 3}, {0, 0}} {
		if got, want := tex(p[0], p[1], r), EarthPixelColor(p[0], p[1], r, 0, 0); got != want {
			t.Errorf("nil-light pixel (%d,%d): got %q want %q", p[0], p[1], got, want)
		}
	}
}
