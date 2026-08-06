package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// diskRasterBenchRadius mirrors a realistic focused-body pixel radius —
// large enough to clear BodyTextureMinRadius (12) with room to spare, in
// the range the live idle-CPU measurement (#363 PR) was taken at.
const diskRasterBenchRadius = 24

// BenchmarkBodyDiskRaster compares the #363 raster cache's hit path
// against the pre-#363 behavior of calling the texture closure for
// every pixel every frame. Run with:
//
//	go test ./internal/tui/screens/ -run '^$' -bench BenchmarkBodyDiskRaster -benchmem
//
// "Uncached" reproduces the old per-tick cost (what every idle frame
// paid before this change); "CachedHit" is what every idle frame pays
// after it — a cache-key build, a map lookup, and a blit instead of the
// texture closure's trig + feature-polygon evaluation per pixel.
func BenchmarkBodyDiskRaster(b *testing.B) {
	body := diskCacheTestBody()
	const r = diskRasterBenchRadius
	subLat, subLon := 12.3, 45.6
	upX, upY := 0.0, 1.0
	light := &render.SolarLight{SubSolarLatDeg: 8.0, SubSolarLonDeg: 30.0, EclipseFactor: 1}
	pos := orbital.Vec3{}

	b.Run("Uncached", func(b *testing.B) {
		canvas := widgets.NewCanvas(200, 60)
		canvas.SetScale(1)
		tex := render.TextureFor(body, r, subLat, subLon, upX, upY, light)
		if tex == nil {
			b.Fatal("expected a non-nil texture for the bench body")
		}
		tag := widgets.CellTag{Color: lipgloss.Color(body.Color), BodyID: body.ID}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			canvas.FillTexturedDiskTagged(pos, r, func(dx, dy int) lipgloss.Color {
				return tex(dx, dy, r)
			}, tag)
		}
	})

	b.Run("CachedHit", func(b *testing.B) {
		canvas := widgets.NewCanvas(200, 60)
		canvas.SetScale(1)
		v := NewOrbitView(plainTheme())
		tex := render.TextureFor(body, r, subLat, subLon, upX, upY, light)
		if tex == nil {
			b.Fatal("expected a non-nil texture for the bench body")
		}
		tag := widgets.CellTag{Color: lipgloss.Color(body.Color), BodyID: body.ID}
		// Warm the cache once (the one real miss a frame-to-frame idle
		// sequence pays) before timing the steady-state hit path.
		warm := v.texturedDiskRaster(0, body, r, subLat, subLon, upX, upY, light, tex)
		canvas.FillTexturedDiskTagged(pos, r, func(dx, dy int) lipgloss.Color {
			return warm(dx, dy, r)
		}, tag)
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			cached := v.texturedDiskRaster(0, body, r, subLat, subLon, upX, upY, light, tex)
			canvas.FillTexturedDiskTagged(pos, r, func(dx, dy int) lipgloss.Color {
				return cached(dx, dy, r)
			}, tag)
		}
	})
}
