package widgets

import "testing"

// BenchmarkPixelTagGridSet measures the #369 dense-grid write path in
// isolation: N sets on a pre-sized grid, the same shape of work
// blitCurvePoints does on every cache-HIT replay (the #368 review's
// ~41 ns/point finding). No canvas, no drawille, no projection — just the
// write this file exists to make cheap.
func BenchmarkPixelTagGridSet(b *testing.B) {
	const w, h = 240, 160 // a 120x40-cell canvas's pixel grid
	const n = 475         // #368 review's measured curve point count
	tag := CellTag{Color: "#00FF00", Owner: "bench"}

	var g pixelTagGrid
	g.ensureSize(w, h)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for p := 0; p < n; p++ {
			px, py := (p*37)%w, (p*23)%h // scattered, not a tight run
			g.set(px, py, tag)
		}
		g.clear()
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/point")
}

// benchPixelTagsMap is a byte-for-byte reproduction of the pre-#369
// map[[2]int]CellTag write path (Canvas.pixelTags before this file's
// change), kept ONLY here as a same-binary comparison baseline so the
// grid-vs-map delta is measured in one benchmark run rather than across
// two separate `main`/branch sessions.
func benchPixelTagsMapSet(m map[[2]int]CellTag, px, py int, tag CellTag) map[[2]int]CellTag {
	if m == nil {
		m = make(map[[2]int]CellTag)
	}
	m[[2]int{px, py}] = tag
	return m
}

// BenchmarkPixelTagsMapSetBaseline is BenchmarkPixelTagGridSet's baseline:
// the same N-point write pattern through the OLD map[[2]int]CellTag
// implementation, so `go test -bench PixelTag` in one run reports both
// numbers directly comparable (same process, same machine load, no
// separate-session variance).
func BenchmarkPixelTagsMapSetBaseline(b *testing.B) {
	const w, h = 240, 160
	const n = 475
	tag := CellTag{Color: "#00FF00", Owner: "bench"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var m map[[2]int]CellTag
		for p := 0; p < n; p++ {
			px, py := (p*37)%w, (p*23)%h
			m = benchPixelTagsMapSet(m, px, py, tag)
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*n), "ns/point")
}
