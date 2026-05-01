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

func TestAtmosphereVisibleSuppressesAtCloseZoom(t *testing.T) {
	b := bodyWithAtm("")
	if !AtmosphereVisible(b, AtmosphereVisibilityCap-1) {
		t.Error("haze should be visible below the cap")
	}
	if AtmosphereVisible(b, AtmosphereVisibilityCap) {
		t.Error("haze should be suppressed at the cap")
	}
	if AtmosphereVisible(b, AtmosphereVisibilityCap*2) {
		t.Error("haze should be suppressed well past the cap")
	}
}

func TestAtmosphereVisibleAirless(t *testing.T) {
	if AtmosphereVisible(bodyAirless(), 5) {
		t.Error("airless body shouldn't render haze")
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
