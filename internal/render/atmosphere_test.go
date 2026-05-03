package render

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func bodyWithAtm(color string) bodies.CelestialBody {
	return bodies.CelestialBody{
		ID:         "earth",
		MeanRadius: 6371.0,
		Color:      "#5BB3FF",
		Atmosphere: &bodies.Atmosphere{
			ScaleHeight:    8500,
			SurfaceDensity: 1.225,
			CutoffAltitude: 150000,
			Color:          color,
		},
	}
}

func bodyAirless() bodies.CelestialBody {
	return bodies.CelestialBody{
		ID:         "moon",
		MeanRadius: 1737.4,
	}
}

func TestAtmosphereOuterMetersIncludesCutoffPlusScaleHeight(t *testing.T) {
	b := bodyWithAtm("")
	got := AtmosphereOuterMeters(b)
	want := 6371000.0 + 150000 + 8500
	if got != want {
		t.Errorf("outer = %g, want %g (R + cutoff + scale-height)", got, want)
	}
}

func TestAtmosphereOuterMetersAirlessReturnsZero(t *testing.T) {
	if got := AtmosphereOuterMeters(bodyAirless()); got != 0 {
		t.Errorf("airless body outer = %g, want 0", got)
	}
}

func TestAtmosphereVisibleAtmosphered(t *testing.T) {
	b := bodyWithAtm("")
	for _, r := range []int{1, 5, 50, 500} {
		if !AtmosphereVisible(b, r) {
			t.Errorf("haze should render at body pxRadius=%d (any zoom)", r)
		}
	}
}

func TestAtmosphereVisibleAirless(t *testing.T) {
	if AtmosphereVisible(bodyAirless(), 5) {
		t.Error("airless body shouldn't render haze")
	}
}

// TestAtmosphereOuterPxFloorsAboveBody: at small zoom the physical
// (cutoff + scale-height) projection is sub-pixel relative to the
// body's pixel radius, so the floor of bodyPxRadius +
// AtmosphereMinHaloPx kicks in to keep the halo visible.
func TestAtmosphereOuterPxFloorsAboveBody(t *testing.T) {
	b := bodyWithAtm("")
	// scale tiny — physical haze projects to fewer px than body.
	// 1e-7 px/m × 6.529e6 m ≈ 0.65 px (rounds to 0). Floor must
	// engage.
	bodyPx := 10
	got := AtmosphereOuterPx(b, 1e-7, bodyPx)
	if got != bodyPx+AtmosphereMinHaloPx {
		t.Errorf("low-zoom outer px = %d, want floor %d", got, bodyPx+AtmosphereMinHaloPx)
	}
}

// TestAtmosphereOuterPxUsesPhysicalAtCloseZoom: at close zoom the
// physical projection beats the floor, and the haze ring sits at
// the literal (R + cutoff + H) projection.
func TestAtmosphereOuterPxUsesPhysicalAtCloseZoom(t *testing.T) {
	b := bodyWithAtm("")
	// scale 1 px/m gives physical outer px = AtmosphereOuterMeters(b)
	// ≈ 6.53e6 — far past any body floor.
	got := AtmosphereOuterPx(b, 1, 100)
	if got <= 100+AtmosphereMinHaloPx {
		t.Errorf("close-zoom outer px = %d, want physical projection", got)
	}
}

func TestAtmosphereHazeColorPrefersAtmColor(t *testing.T) {
	b := bodyWithAtm("#9DC8FF")
	if got, want := string(AtmosphereHazeColor(b)), "#9DC8FF"; got != want {
		t.Errorf("haze color = %q, want %q", got, want)
	}
}

func TestAtmosphereHazeColorFallsBackToBodyColor(t *testing.T) {
	b := bodyWithAtm("")
	// b.Color is "#5BB3FF" — that's what ColorFor returns when no
	// override matches. Haze inherits it.
	got := string(AtmosphereHazeColor(b))
	if got != "#5BB3FF" {
		t.Errorf("haze fallback = %q, want body color %q", got, "#5BB3FF")
	}
}
