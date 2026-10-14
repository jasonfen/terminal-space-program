package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func benchLaunchRender(b *testing.B, w *sim.World) {
	th := launchThemeForTest()
	v := NewLaunchView(th, NewOrbitView(th))
	v.Render(w, DesignWidth, DesignHeight)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Render(w, DesignWidth, DesignHeight)
	}
}

// BenchmarkLaunchViewRenderPad is the launch view's idle steady-state frame
// on the pad at the Design Size (#639: the sky and ground backdrop covers
// the whole canvas, so this guards what that costs).
func BenchmarkLaunchViewRenderPad(b *testing.B) {
	w, _ := spawnSaturnVOnPad(b)
	benchLaunchRender(b, w)
}

// BenchmarkLaunchViewRenderClimb is the same frame 30 km up, mid-sky.
func BenchmarkLaunchViewRenderClimb(b *testing.B) {
	benchLaunchRender(b, ascendingCraftWorld(b, "earth", 30_000, 800, orbital.Vec3{X: 1}))
}
