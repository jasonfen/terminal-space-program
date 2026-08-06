package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// BenchmarkOrbitViewRenderIdle exercises the whole per-tick OrbitView.Render
// path in the idle steady state (#363/#364 idle-CPU diagnosis): the world
// clock is never advanced between calls, so this is exactly the picture
// painted twice in a row between two 20 Hz ticks with warp paused / at rest.
//
// Useful with `-cpuprofile` for a repeatable, noise-free breakdown of where
// idle-frame CPU goes, independent of the live-CPU `ps -o %cpu` measurement
// (which also carries tea.Program / terminal I/O overhead this benchmark
// doesn't exercise):
//
//	go test ./internal/tui/screens/ -run '^$' -bench BenchmarkOrbitViewRenderIdle \
//	  -benchtime=3s -cpuprofile=/tmp/cpu.prof
//	go tool pprof -top /tmp/cpu.prof
func BenchmarkOrbitViewRenderIdle(b *testing.B) {
	v := NewOrbitView(plainTheme())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		b.Fatalf("NewWorld: %v", err)
	}
	v.Render(w, 0, 120, 40) // warm the Framing Event / zoom fit
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v.Render(w, 0, 120, 40)
	}
}
