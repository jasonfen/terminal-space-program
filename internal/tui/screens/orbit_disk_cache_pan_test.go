package screens

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// offsetColoredTexture returns a BodyTexture whose color is a pure
// function of (dx, dy) — distinct per offset — so a stale or blank
// cache read at any single offset is individually detectable, unlike
// a flat single-color texture where a bug could hide behind "every
// pixel happens to look the same anyway."
func offsetColoredTexture() render.BodyTexture {
	return func(dx, dy, rr int) lipgloss.Color {
		return lipgloss.Color(fmt.Sprintf("#%02X%02X01", (dx+128)&0xFF, (dy+128)&0xFF))
	}
}

// TestTexturedDiskRasterSelfHealsAfterPan is the review-mandated
// regression test for the viewport-clip cache bug: FillTexturedDiskTagged
// clips off-canvas offsets BEFORE calling the texture closure, so a
// grid built while only PART of a disk was on-canvas is incomplete for
// the offsets that were clipped away that frame. A pan that brings a
// previously-clipped region into view, with an otherwise IDENTICAL
// raster key (same body/radius/lighting — a pure pan doesn't change
// any of those), must still be served correct (non-blank) colors on
// the cache-HIT path, not the zero-value lipgloss.Color("").
func TestTexturedDiskRasterSelfHealsAfterPan(t *testing.T) {
	v := NewOrbitView(plainTheme())
	body := diskCacheTestBody()
	const r = 20
	subLat, subLon := 10.0, 20.0
	upX, upY := 0.0, 1.0
	light := &render.SolarLight{SubSolarLatDeg: 5, SubSolarLonDeg: 15, EclipseFactor: 1}
	tex := offsetColoredTexture()

	// Frame 1: only the LEFT half of the disk's offsets get touched —
	// standing in for a viewport that clips away the right half.
	cached1 := v.texturedDiskRaster(0, body, r, subLat, subLon, upX, upY, light, tex)
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= 0; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			cached1(dx, dy, r)
		}
	}

	// Frame 2: an IDENTICAL key (a pure pan changes screen position,
	// not the raster inputs) must be a cache HIT...
	computesBefore := v.diskRasterCacheComputes
	cached2 := v.texturedDiskRaster(0, body, r, subLat, subLon, upX, upY, light, tex)
	if v.diskRasterCacheComputes != computesBefore {
		t.Fatalf("expected a cache HIT on the panned call, got a recompute (computes %d -> %d)", computesBefore, v.diskRasterCacheComputes)
	}
	// ...yet still return CORRECT colors for the right-half offsets
	// frame 1 never drew — the pan-onto-never-drawn-region case.
	for dy := -r; dy <= r; dy++ {
		for dx := 1; dx <= r; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			got := cached2(dx, dy, r)
			want := tex(dx, dy, r)
			if got != want {
				t.Errorf("pan onto never-drawn offset (%d,%d): got %q, want %q (stale/blank cache read)", dx, dy, got, want)
			}
			if got == "" {
				t.Errorf("pan onto never-drawn offset (%d,%d): got the blank zero-value color", dx, dy)
			}
		}
	}
}

// TestOrbitRenderDiskCacheHitMatchesUncachedAfterPan is the
// review-specified end-to-end regression test: a cache-HIT render must
// be byte-identical to the same frame rendered with the cache dropped
// (a fresh OrbitView), with lipgloss forced to emit real ANSI color
// (Go tests have no TTY, so lipgloss strips color by default — the
// very reason the original bug's earlier tests, which compared
// cache-hit COUNTERS rather than rendered pixels/bytes, never caught
// this). Uses a disk radius larger than the canvas (zoomed past the
// viewport) so every frame is necessarily clipped, and pans the canvas
// center between frames so the two frames clip DIFFERENT halves of the
// disk while sharing an identical raster key.
//
// Forces color via lipgloss.SetColorProfile rather than
// t.Setenv("CLICOLOR_FORCE", "1"): lipgloss's global renderer detects
// (and permanently caches, via sync.Once) its color profile on the
// FIRST Style.Render() call in the process — in the full `screens`
// package test binary, many earlier tests already render through
// Canvas.String() without CLICOLOR_FORCE set, so by the time this test
// runs the profile is already cached as colorless and a plain env-var
// change here arrives too late to matter (verified: this test flaked
// exactly that way under `go test ./internal/tui/screens/...`, passing
// alone but failing as part of the full package run).
// SetColorProfile bypasses that lazy detection outright, so it's
// immune to execution order. It has no dedicated "restore" API, but
// leaving TrueColor forced for the rest of the process is NOT safe —
// verified: doing so broke TestHyperbolicGhostGlyphOnly /
// TestOrbitViewRendersGhosts / TestLaunchHUDRendersOrbitReadyOnApAboveFloor,
// which depend on the default colorless profile for their assertions.
// So this reads (and lazily triggers, if nothing has yet) the
// process's natural ambient profile via lipgloss.ColorProfile() FIRST,
// then restores exactly that value in t.Cleanup — same effective
// color output for every test that runs after this one, whether the
// renderer got there by lazy env detection or an explicit restore.
func TestOrbitRenderDiskCacheHitMatchesUncachedAfterPan(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	body := diskCacheTestBody()
	const r = 30 // bigger than the 20x10-cell (40x40px) canvas below
	subLat, subLon := 10.0, 20.0
	upX, upY := 0.0, 1.0
	light := &render.SolarLight{SubSolarLatDeg: 5, SubSolarLonDeg: 15, EclipseFactor: 1}

	renderPanned := func(v *OrbitView, canvas *widgets.Canvas, centerX float64) string {
		canvas.Clear()
		canvas.Center(orbital.Vec3{X: centerX})
		tex := offsetColoredTexture()
		cached := v.texturedDiskRaster(0, body, r, subLat, subLon, upX, upY, light, tex)
		canvas.FillTexturedDiskTagged(orbital.Vec3{}, r, func(dx, dy int) lipgloss.Color {
			return cached(dx, dy, r)
		}, widgets.CellTag{})
		return canvas.String()
	}

	// One shared OrbitView + Canvas, so frame 2 below is a genuine
	// cache HIT (identical key, only the canvas center — i.e. pan —
	// differs from frame 1).
	v := NewOrbitView(plainTheme())
	canvas := widgets.NewCanvas(20, 10)
	canvas.SetScale(1)

	// Frame 1: disk centered near the canvas's left edge — clips away
	// most of the right half.
	_ = renderPanned(v, canvas, -25)

	// Frame 2 (PAN): disk now centered near the right edge — clips
	// away most of the LEFT half instead, bringing much of the
	// previously-clipped region into view under an identical key.
	computesBefore := v.diskRasterCacheComputes
	got := renderPanned(v, canvas, 25)
	if v.diskRasterCacheComputes != computesBefore {
		t.Fatalf("expected frame 2 (the pan) to be a raster cache HIT, got a recompute (computes %d -> %d)", computesBefore, v.diskRasterCacheComputes)
	}

	// Reference: render the identical frame 2 from scratch with the
	// cache dropped (a fresh OrbitView never hits).
	freshV := NewOrbitView(plainTheme())
	freshCanvas := widgets.NewCanvas(20, 10)
	freshCanvas.SetScale(1)
	want := renderPanned(freshV, freshCanvas, 25)

	if got != want {
		t.Errorf("cache-HIT render diverges from the cache-dropped reference after a pan\ngot:  %q\nwant: %q", got, want)
	}
	// Vacuity guard: checks want (the cache-DROPPED ground truth), not got
	// (the cache-hit render under test) — a review finding on the
	// original version of this test (#367). Asserting on got makes the
	// guard vacuous exactly when it matters: if the stale-frame bug this
	// test exists to catch were present, got would plausibly be blank/
	// colorless (the bug's own symptom), and a got != want failure above
	// would never reach this line to notice the setup itself was broken.
	// want comes from a path this test doesn't otherwise exercise
	// (renderPanned on a fresh, never-hit OrbitView), so it fails loudly
	// on its own if color forcing ever stops working, independent of
	// whatever the cache under test does.
	if !containsANSI(want) {
		t.Fatal("test setup broken: expected lipgloss.SetColorProfile(termenv.TrueColor) to produce ANSI-colored output")
	}
}

func containsANSI(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			return true
		}
	}
	return false
}
