package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

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
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
	v := NewLaunchView(th)
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
