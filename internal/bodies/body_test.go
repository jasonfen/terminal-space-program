package bodies

import "testing"

// SurfaceColor is an additive catalog field (v0.11.0+) describing the
// horizon-fill colour used by ViewLaunch. The accessor defaults to the
// body's existing Color when SurfaceColor is unset, so most of the
// catalog doesn't need a hand-set value before Slice 4's sweep.
func TestSurfaceColorHexFallsBackToColor(t *testing.T) {
	b := CelestialBody{Color: "#5BB3FF"}
	if got := b.SurfaceColorHex(); got != "#5BB3FF" {
		t.Errorf("SurfaceColorHex() = %q, want %q (Color fallback)", got, "#5BB3FF")
	}
}

func TestSurfaceColorHexUsesExplicitValue(t *testing.T) {
	b := CelestialBody{Color: "#5BB3FF", SurfaceColor: "#3A6F40"}
	if got := b.SurfaceColorHex(); got != "#3A6F40" {
		t.Errorf("SurfaceColorHex() = %q, want %q (explicit SurfaceColor wins)", got, "#3A6F40")
	}
}
