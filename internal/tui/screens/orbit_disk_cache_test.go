package screens

import (
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// diskCacheTestBody is a minimal textured body (base + one continent),
// mirroring internal/render's litTestBody helper, sized for the #363
// raster-cache tests below.
func diskCacheTestBody() bodies.CelestialBody {
	return bodies.CelestialBody{
		ID:    "disktest",
		Color: "#4080C0",
		Texture: &bodies.Texture{
			Base:       "#4080C0",
			Continents: []bodies.TextureEllipse{{Lat: 0, Lon: 0, LatR: 40, LonR: 40, Color: "#40A040"}},
		},
	}
}

// TestTexturedDiskRasterHitsOnIdenticalKey confirms an identical call
// (same body, radius, sub-observer, up-vector, light) serves the cached
// grid instead of invoking tex again — the whole point of #363.
func TestTexturedDiskRasterHitsOnIdenticalKey(t *testing.T) {
	v := NewOrbitView(plainTheme())
	body := diskCacheTestBody()
	const r = 20
	subLat, subLon := 10.0, 20.0
	upX, upY := 0.0, 1.0
	light := &render.SolarLight{SubSolarLatDeg: 5, SubSolarLonDeg: 15, EclipseFactor: 1}

	calls := 0
	tex := render.BodyTexture(func(dx, dy, rr int) lipgloss.Color {
		calls++
		return lipgloss.Color("#123456")
	})

	c1 := v.texturedDiskRaster(0, body, r, subLat, subLon, upX, upY, light, tex)
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			c1(dx, dy, r)
		}
	}
	firstCalls := calls
	if firstCalls == 0 {
		t.Fatal("expected the uncached raster pass to invoke tex")
	}
	if v.diskRasterCacheComputes != 1 || v.diskRasterCacheHits != 0 {
		t.Fatalf("after first pass: computes=%d hits=%d, want computes=1 hits=0", v.diskRasterCacheComputes, v.diskRasterCacheHits)
	}

	// Second call with an IDENTICAL key must not touch tex again.
	c2 := v.texturedDiskRaster(0, body, r, subLat, subLon, upX, upY, light, tex)
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			if got, want := c2(dx, dy, r), lipgloss.Color("#123456"); got != want {
				t.Errorf("cached pixel (%d,%d) = %q, want %q", dx, dy, got, want)
			}
		}
	}
	if calls != firstCalls {
		t.Errorf("cache hit still invoked tex: calls went %d -> %d", firstCalls, calls)
	}
	if v.diskRasterCacheComputes != 1 || v.diskRasterCacheHits != 1 {
		t.Fatalf("after second (hit) pass: computes=%d hits=%d, want computes=1 hits=1", v.diskRasterCacheComputes, v.diskRasterCacheHits)
	}
}

// TestTexturedDiskRasterSubPixelDriftHits confirms the whole point of
// quantization: a sub-observer longitude nudge far smaller than one
// quantum (the ~0.0002°/tick idle drift at warp 1×, per #363) still
// hits the cache.
func TestTexturedDiskRasterSubPixelDriftHits(t *testing.T) {
	v := NewOrbitView(plainTheme())
	body := diskCacheTestBody()
	const r = 20
	tex := render.BodyTexture(func(dx, dy, rr int) lipgloss.Color { return lipgloss.Color("#123456") })

	v.texturedDiskRaster(0, body, r, 10.0, 20.0, 0, 1, nil, tex)
	if v.diskRasterCacheComputes != 1 {
		t.Fatalf("first call: computes=%d, want 1", v.diskRasterCacheComputes)
	}

	const idleDriftDeg = 0.0002 // one warp-1x tick, per #363
	v.texturedDiskRaster(0, body, r, 10.0, 20.0+idleDriftDeg, 0, 1, nil, tex)
	if v.diskRasterCacheComputes != 1 || v.diskRasterCacheHits != 1 {
		t.Errorf("idle-drift call: computes=%d hits=%d, want computes=1 hits=1 (sub-quantum drift must hit)", v.diskRasterCacheComputes, v.diskRasterCacheHits)
	}
}

// TestTexturedDiskRasterBustsOnChange enumerates every input the raster
// depends on and confirms a real change in each one busts the cache —
// the acceptance criterion that rotation, zoom, camera moves, focus
// switches, and lighting changes never produce a stale frame.
func TestTexturedDiskRasterBustsOnChange(t *testing.T) {
	body := diskCacheTestBody()
	tex := render.BodyTexture(func(dx, dy, rr int) lipgloss.Color { return lipgloss.Color("#123456") })
	baseLight := &render.SolarLight{SubSolarLatDeg: 5, SubSolarLonDeg: 15, EclipseFactor: 1}

	cases := []struct {
		name           string
		r              int
		subLat, subLon float64
		upX, upY       float64
		light          *render.SolarLight
	}{
		{"baseline", 20, 10, 20, 0, 1, baseLight},
		{"radius changed (zoom)", 24, 10, 20, 0, 1, baseLight},
		{"sub-observer lat rotated", 20, 10 + 5, 20, 0, 1, baseLight},
		{"sub-observer lon rotated (high warp)", 20, 10, 20 + 5, 0, 1, baseLight},
		{"screen-up rotated (camera move)", 20, 10, 20, 1, 0, baseLight},
		{"light direction changed", 20, 10, 20, 0, 1, &render.SolarLight{SubSolarLatDeg: 40, SubSolarLonDeg: 40, EclipseFactor: 1}},
		{"eclipse factor changed", 20, 10, 20, 0, 1, &render.SolarLight{SubSolarLatDeg: 5, SubSolarLonDeg: 15, EclipseFactor: 0.3}},
		{"light removed", 20, 10, 20, 0, 1, nil},
	}

	v := NewOrbitView(plainTheme())
	v.texturedDiskRaster(0, body, 20, 10, 20, 0, 1, baseLight, tex) // seed the baseline entry

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "baseline" {
				return
			}
			before := v.diskRasterCacheComputes
			v.texturedDiskRaster(0, body, tc.r, tc.subLat, tc.subLon, tc.upX, tc.upY, tc.light, tex)
			if v.diskRasterCacheComputes != before+1 {
				t.Errorf("%s: computes went %d -> %d, want a miss (bust)", tc.name, before, v.diskRasterCacheComputes)
			}
			// Restore the shared entry to baseline so the next case's
			// diff is isolated to its own single changed input.
			v.texturedDiskRaster(0, body, 20, 10, 20, 0, 1, baseLight, tex)
		})
	}
}

// TestTexturedDiskRasterPerBodyIndependent confirms two distinct bodies
// textured in the same frame each get their own cache slot rather than
// thrashing a single shared one.
func TestTexturedDiskRasterPerBodyIndependent(t *testing.T) {
	v := NewOrbitView(plainTheme())
	bodyA := diskCacheTestBody()
	bodyB := diskCacheTestBody()
	bodyB.ID = "disktest-b"
	tex := render.BodyTexture(func(dx, dy, rr int) lipgloss.Color { return lipgloss.Color("#123456") })

	v.texturedDiskRaster(0, bodyA, 20, 10, 20, 0, 1, nil, tex)
	v.texturedDiskRaster(0, bodyB, 20, 10, 20, 0, 1, nil, tex)
	if v.diskRasterCacheComputes != 2 {
		t.Fatalf("two distinct bodies: computes=%d, want 2", v.diskRasterCacheComputes)
	}
	v.texturedDiskRaster(0, bodyA, 20, 10, 20, 0, 1, nil, tex)
	v.texturedDiskRaster(0, bodyB, 20, 10, 20, 0, 1, nil, tex)
	if v.diskRasterCacheComputes != 2 || v.diskRasterCacheHits != 2 {
		t.Fatalf("second pass over both bodies: computes=%d hits=%d, want computes=2 hits=2", v.diskRasterCacheComputes, v.diskRasterCacheHits)
	}
}

// TestOrbitRenderDiskCacheHitsAcrossIdleFrames is an integration-level
// check: two Render() calls with the world clock unchanged (the idle
// case #363 targets) must not re-raster the focused body's disk.
func TestOrbitRenderDiskCacheHitsAcrossIdleFrames(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(120, 40)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	v.Render(w, 0, 120, 40)
	firstComputes := v.diskRasterCacheComputes
	if firstComputes == 0 {
		t.Skip("no textured body reached the raster path at default zoom (radius below BodyTextureMinRadius) — nothing to assert")
	}

	// Idle: render again with the clock untouched, exactly what happens
	// between two 20 Hz ticks with warp paused / at rest.
	v.Render(w, 0, 120, 40)
	if v.diskRasterCacheComputes != firstComputes {
		t.Errorf("idle re-render caused a raster recompute: computes went %d -> %d", firstComputes, v.diskRasterCacheComputes)
	}
	if v.diskRasterCacheHits == 0 {
		t.Error("idle re-render recorded no cache hits")
	}
}

// TestOrbitRenderDiskCacheBustsOnWarpRotation confirms that advancing
// the clock enough to rotate the body past one quantum (as high warp
// does) forces a real recompute — the disk must keep animating.
func TestOrbitRenderDiskCacheBustsOnWarpRotation(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(120, 40)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}

	v.Render(w, 0, 120, 40)
	firstComputes := v.diskRasterCacheComputes
	if firstComputes == 0 {
		t.Skip("no textured body reached the raster path at default zoom — nothing to assert")
	}

	// Jump the sim clock forward by a large margin — Earth's rotation
	// period is ~24h, so a full day moves the sub-observer longitude by
	// far more than one 0.02° quantum.
	w.Clock.SimTime = w.Clock.SimTime.Add(24 * time.Hour)
	v.Render(w, 0, 120, 40)
	if v.diskRasterCacheComputes == firstComputes {
		t.Error("a full-day clock jump did not bust the raster cache — disk would render stale")
	}
}

// TestOrbitRenderDiskCacheSkipsOffCanvasBodies is the review-mandated
// heap-bound regression test for the other retained-heap cause: at the
// default LEO-Earth start, most of Sol's bodies (Mercury, Venus, Mars,
// the outer planets, the Sun itself) are off-canvas — before the
// #363 review fix, every one of them still built and RETAINED a full
// raster grid that painted nothing (measured ~70x the idle retained
// heap). The raster cache must end up with far fewer entries than the
// system's total body count — only the bodies that actually intersect
// the canvas get one.
func TestOrbitRenderDiskCacheSkipsOffCanvasBodies(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(120, 40)

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	totalBodies := len(w.System().Bodies)
	if totalBodies < 2 {
		t.Skip("system has too few bodies to distinguish on-canvas from off-canvas")
	}

	v.Render(w, 0, 120, 40)

	if got := len(v.diskRasterCache); got >= totalBodies {
		t.Errorf("diskRasterCache holds %d entries for a %d-body system — expected most off-canvas bodies (at default LEO zoom) to be skipped entirely, not cached empty", got, totalBodies)
	}
}
