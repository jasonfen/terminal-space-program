package screens

import (
	"math"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/settings"
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

// linkedWorld: the coverage rung current and read at Earth, no ground
// stations (so no home blanket), and a hand-made live network of the given
// body-fixed-agnostic world directions at 3 radii from Earth's centre.
func linkedWorld(t *testing.T, dirs ...orbital.Vec3) *sim.World {
	t.Helper()
	w, _, _ := spreadNoticeWorld(t)
	rung := coverageRung(t, w)
	rung.Objectives[0].Status = missions.Passed
	rung.Objectives[1].Params.PrimaryID = "earth"
	w.GroundStations = nil
	setLinkedNet(w, dirs...)
	return w
}

func setLinkedNet(w *sim.World, dirs ...orbital.Vec3) {
	sys := w.System()
	earth := *sys.FindBody("earth")
	c := w.BodyPosition(earth)
	g := &sim.CommGraph{Connected: map[uint64]bool{}}
	for _, d := range dirs {
		g.Net = append(g.Net, sim.CommNetNode{Pos: c.Add(d.Unit().Scale(3 * earth.RadiusMeters())), RangeM: 1e12})
	}
	w.CommGraph = g
}

// tintedCells is the set of cells the overlay painted on v's canvas.
func tintedCells(v *OrbitView) map[[2]int]bool {
	tint := render.Shade(render.ColorTarget, linkedTintFactor)
	out := map[[2]int]bool{}
	for col := 0; col < 300; col++ {
		for row := 0; row < 100; row++ {
			if v.canvas.CellBackground(col, row) == tint {
				out[[2]int{col, row}] = true
			}
		}
	}
	return out
}

// ADR 0066 guard 12: the overlay tints dots that see a connected source, not
// ones behind the limb; the row off, F2 and a body that is not the notice's
// leave it off; a cache hit equals the cache-dropped truth, also after the
// disk moves on screen and after a source moves by more than the quantum.
func TestLinkedNowOverlay(t *testing.T) {
	axes := []orbital.Vec3{{X: 1}, {X: -1}, {Y: 1}, {Y: -1}, {Z: 1}, {Z: -1}}
	frame := func(v *OrbitView, w *sim.World) { v.Render(w, 0, 140, 40) }

	// Blanket (a station on the body): every cell of the disk.
	wb := linkedWorld(t, axes[0])
	wb.GroundStations = []sim.GroundStationPreset{{BodyID: "earth"}}
	vb := NewOrbitView(plainTheme())
	vb.Resize(140, 40)
	frame(vb, wb)
	all := len(tintedCells(vb))
	if all < 20 {
		t.Fatalf("home blanket tinted %d cells, want the whole disk (20+)", all)
	}

	// One source alone: some visible dots see it, never all, and opposite
	// sources see disjoint dots (3 radii out sees a 70 degree cap).
	seen := make([]map[[2]int]bool, len(axes))
	any := false
	for i, d := range axes {
		w := linkedWorld(t, d)
		v := NewOrbitView(plainTheme())
		v.Resize(140, 40)
		frame(v, w)
		seen[i] = tintedCells(v)
		if len(seen[i]) >= all {
			t.Errorf("axis %v tinted %d of %d cells: the limb culled nothing", d, len(seen[i]), all)
		}
		any = any || len(seen[i]) > 0
	}
	if !any {
		t.Fatal("no source tinted any cell")
	}
	for i := 0; i < len(axes); i += 2 {
		for c := range seen[i] {
			if seen[i+1][c] {
				t.Errorf("cell %v sees both %v and %v", c, axes[i], axes[i+1])
			}
		}
	}

	// A source out of range tints nothing (the equal case sits on a float edge, so a clear margin).
	w := linkedWorld(t, axes[0])
	w.CommGraph.Net[0].RangeM = 1
	v := NewOrbitView(plainTheme())
	v.Resize(140, 40)
	frame(v, w)
	if n := len(tintedCells(v)); n != 0 {
		t.Errorf("out-of-range source tinted %d cells", n)
	}

	// Row off and F2 clear it.
	w = linkedWorld(t, axes[4])
	v = NewOrbitView(plainTheme())
	v.Resize(140, 40)
	frame(v, w)
	on := len(tintedCells(v))
	if on == 0 {
		t.Fatal("no tint with the row on")
	}
	s := v.settings
	s.SetChip(settings.ChipCoverageNow, false)
	v.SetSettings(s)
	frame(v, w)
	if n := len(tintedCells(v)); n != 0 {
		t.Errorf("row off left %d tinted cells", n)
	}
	s.SetChip(settings.ChipCoverageNow, true)
	v.SetSettings(s)
	v.SetDeclutter(true)
	frame(v, w)
	if n := len(tintedCells(v)); n != 0 {
		t.Errorf("declutter left %d tinted cells", n)
	}
	v.SetDeclutter(false)

	// Cache: an identical frame hits; a source moving 0.01 deg stays a hit, 0.1 deg recomputes,
	// and each result equals a fresh view's.
	truth := func() map[[2]int]bool {
		fv := NewOrbitView(plainTheme())
		fv.Resize(140, 40)
		frame(fv, w)
		return tintedCells(fv)
	}
	same := func(a, b map[[2]int]bool) bool {
		if len(a) != len(b) {
			return false
		}
		for c := range a {
			if !b[c] {
				return false
			}
		}
		return true
	}
	frame(v, w)
	h0, c0 := v.linkedCacheHits, v.linkedCacheComputes
	frame(v, w)
	if v.linkedCacheHits != h0+1 || v.linkedCacheComputes != c0 {
		t.Errorf("identical frame: hits %d->%d computes %d->%d, want one more hit", h0, v.linkedCacheHits, c0, v.linkedCacheComputes)
	}
	tilt := func(deg float64) orbital.Vec3 {
		r := deg * math.Pi / 180
		return orbital.Vec3{X: math.Sin(r), Z: math.Cos(r)}
	}
	setLinkedNet(w, tilt(0.1))
	frame(v, w)
	if v.linkedCacheComputes != c0+1 {
		t.Errorf("source moved 0.1 deg: computes %d, want %d", v.linkedCacheComputes, c0+1)
	}
	if got := tintedCells(v); !same(got, truth()) {
		t.Error("after the source moved, the cached tint differs from the cache-dropped truth")
	}
	// the disk moves on screen with every relay and camera input equal (radius too): a new
	// screen position must recompute, and equal the cache-dropped truth.
	setLinkedNet(w, axes...) // six sources: any hemisphere sees some
	earth := w.System().Bodies[0]
	for _, b := range w.System().Bodies {
		if b.ID == "earth" {
			earth = b
		}
	}
	pos := w.BodyPosition(earth)
	draw := func(v *OrbitView, shift float64) map[[2]int]bool {
		v.canvas.Clear()
		v.canvas.Center(pos.Add(orbital.Vec3{X: shift}))
		v.drawLinkedNow(w, earth, pos, 20, 10, 20, 0, 1)
		return tintedCells(v)
	}
	pv := NewOrbitView(plainTheme())
	pv.Resize(140, 40)
	pv.canvas.SetScale(1e-6)
	draw(pv, 0)
	c1 := pv.linkedCacheComputes
	moved := draw(pv, 3e6) // 3 px: an odd offset, so the cells' alignment changes
	if pv.linkedCacheComputes == c1 {
		t.Error("the disk moved on screen and the tint cache did not recompute")
	}
	fv := NewOrbitView(plainTheme())
	fv.Resize(140, 40)
	fv.canvas.SetScale(1e-6)
	if want := draw(fv, 3e6); !same(moved, want) || len(want) == 0 {
		t.Errorf("after the disk moved on screen, the cached tint (%d cells) differs from the cache-dropped truth (%d)", len(moved), len(want))
	}
}
