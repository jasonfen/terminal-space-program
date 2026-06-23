package screens

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// drawCommPath (ADR 0027 / C2-7) draws the active probe's relay sightline on
// the orbit canvas when connected, and nothing otherwise. The projection /
// dashing math is covered by the canvas dense-line tests; here we assert the
// wiring: a connected path mutates the canvas, a disconnected one leaves it
// untouched.
func TestDrawCommPath(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.canvas.Resize(60, 30) // pixel grid 120 × 120
	v.canvas.SetScale(1)
	v.canvas.Center(orbital.Vec3{})

	c := &spacecraft.Spacecraft{ID: 1, Controllable: true}
	w := &sim.World{Crafts: []*spacecraft.Spacecraft{c}, ActiveCraftIdx: 0}

	// Disconnected probe: no path → canvas unchanged.
	w.CommGraph = &sim.CommGraph{Connected: map[uint64]bool{}}
	v.canvas.Clear()
	before := v.canvas.String()
	v.drawCommPath(w)
	if v.canvas.String() != before {
		t.Error("a disconnected craft must not draw a relay path")
	}

	// Connected probe with a two-point path spanning the canvas → pixels drawn.
	w.CommGraph = &sim.CommGraph{
		Connected: map[uint64]bool{1: true},
		Paths:     map[uint64][]orbital.Vec3{1: {{X: -40}, {X: 40}}},
	}
	v.canvas.Clear()
	blank := v.canvas.String()
	v.drawCommPath(w)
	if v.canvas.String() == blank {
		t.Error("a connected probe should draw its relay path on the canvas")
	}
}

// TestDrawCommPathBridgesLongHop: a relay hop longer than the canvas (a
// Moon→Earth-scale leg) must still draw its visible run. The hop here straddles
// the whole canvas with both endpoints off-screen — the old guarded line
// primitive would draw nothing (both endpoint dots off-canvas), so a changed
// canvas proves drawCommPath force-bridges the link instead of dot-only.
func TestDrawCommPathBridgesLongHop(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.canvas.Resize(60, 30) // 120 × 120 px grid
	v.canvas.SetScale(1)
	v.canvas.Center(orbital.Vec3{})

	c := &spacecraft.Spacecraft{ID: 1, Controllable: true}
	w := &sim.World{Crafts: []*spacecraft.Spacecraft{c}, ActiveCraftIdx: 0}
	w.CommGraph = &sim.CommGraph{
		Connected: map[uint64]bool{1: true},
		// Endpoints project to x=-40 and x=160 — both off-canvas, straddling
		// the 120px-wide view: the guarded variant draws 0 pixels here.
		Paths: map[uint64][]orbital.Vec3{1: {{X: -100}, {X: 100}}},
	}
	v.canvas.Clear()
	blank := v.canvas.String()
	v.drawCommPath(w)
	if v.canvas.String() == blank {
		t.Error("drawCommPath must force-bridge a long straddling relay hop (guarded would draw nothing)")
	}
}
