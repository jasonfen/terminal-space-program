package widgets

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// BenchmarkDrawEllipseClassCachedTaggedHit is the #369 targeted hit-path
// microbenchmark the issue asks for: the #368 pre-merge review measured a
// cache HIT still costing ~19.4µs for a 475-point curve (~41 ns/point)
// with zero geometry left to compute — entirely blitCurvePoints's
// pixelTags writes. This warms the cache once, then times only steady-
// state HITs (b.N repeats of the identical draw), isolating exactly that
// replay cost from the miss/flatten path BenchmarkOrbitViewRenderIdle
// exercises as a whole-frame average.
func BenchmarkDrawEllipseClassCachedTaggedHit(b *testing.B) {
	c := NewCanvas(120, 40) // 240x160 px, matches BenchmarkOrbitViewRenderIdle
	c.SetScale(1)
	c.Center(orbital.Vec3{})
	// Sized to land close to the #368 review's 475-point reference curve —
	// A=70 at scale=1 fills most of a 240x160px canvas without clipping.
	el := orbital.Elements{A: 70, E: 0.5, I: 0.4, Omega: 0.7, Arg: 1.1}
	tag := CellTag{Color: "#00FF00", Owner: "bench"}

	c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)
	_, computes := c.CurveCacheStats()
	if computes != 1 {
		b.Fatalf("warm-up should be exactly one compute, got %d — benchmark scenario is broken", computes)
	}
	if n := len(c.curveCache["curve-1"].points); n < 100 {
		b.Fatalf("warm-up curve has only %d points, want >=100 for a representative hit-path measurement", n)
	}
	points := len(c.curveCache["curve-1"].points)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Clear()
		c.DrawEllipseClassCachedTagged("curve-1", el, orbital.Vec3{}, 360, ClassReal, orbital.Vec3{}, 0, tag)
	}
	b.StopTimer()

	_, computesAfter := c.CurveCacheStats()
	if computesAfter != 1 {
		b.Fatalf("benchmark loop recomputed (computes %d) — it measured misses, not hits", computesAfter)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*points), "ns/point")
}
