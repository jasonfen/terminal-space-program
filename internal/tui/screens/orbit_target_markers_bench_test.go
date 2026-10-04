package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// BenchmarkOrbitViewRenderBodyTargetNodes is the Flight School default
// frame (#548 review line 84): vessel in Earth orbit, the Moon targeted,
// so the body-target plane-node markers are drawn. The world ticks between
// frames (timer stopped) the way the game does, so a cache keyed on the
// clock would never hit.
func BenchmarkOrbitViewRenderBodyTargetNodes(b *testing.B) {
	v := NewOrbitView(plainTheme())
	w, err := sim.NewWorld()
	if err != nil {
		b.Fatalf("NewWorld: %v", err)
	}
	moon := -1
	for i, body := range w.System().Bodies {
		if body.ID == "moon" {
			moon = i
		}
	}
	if moon < 0 {
		b.Fatal("no moon")
	}
	w.SetTargetBody(moon)
	v.Resize(140, 40)
	v.Render(w, 0, 140, 40)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		w.Tick()
		b.StartTimer()
		v.Render(w, 0, 140, 40)
	}
}
