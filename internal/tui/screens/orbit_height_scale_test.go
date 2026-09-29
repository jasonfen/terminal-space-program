package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// bodyPxRadiusAt focuses the named body on a fresh view sized cols x rows
// and returns its base (pre-userZoom) pixel radius and the canvas row count.
func bodyPxRadiusAt(t *testing.T, englishName string, cols, rows int) (float64, int) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	idx := -1
	for i, b := range w.System().Bodies {
		if b.EnglishName == englishName {
			idx = i
		}
	}
	if idx < 0 {
		t.Skipf("%s not in loaded system", englishName)
	}
	v := newSOIPassTestView()
	v.Resize(cols, rows)
	w.ViewMode = sim.ViewTilted
	w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: idx}
	v.Render(w, 0, cols, rows)
	return w.System().Bodies[idx].RadiusMeters() * v.baseScale, v.canvas.Rows()
}

func TestHeightScaledCellsContract(t *testing.T) {
	cases := []struct{ base, rows, want int }{
		{12, 37, 12}, // Design Size: unchanged
		{24, 37, 24},
		{12, 46, 15}, // 181x49
		{24, 46, 30},
		{12, 30, 12}, // below the floor never shrinks
	}
	for _, c := range cases {
		if got := heightScaledCells(c.base, c.rows); got != c.want {
			t.Errorf("heightScaledCells(%d, %d) = %d, want %d", c.base, c.rows, got, c.want)
		}
	}
	// Whole-cell and monotone: a one-row resize never moves it by more than 1.
	prev := heightScaledCells(12, 37)
	for r := 38; r <= 80; r++ {
		got := heightScaledCells(12, r)
		if got < prev || got-prev > 1 {
			t.Errorf("rows %d: %d after %d, want a 0 or +1 step", r, got, prev)
		}
		prev = got
	}
}

func TestPlanetWithMoonsScalesWithHeight(t *testing.T) {
	px40, rows40 := bodyPxRadiusAt(t, "Earth", 140, 40)
	px49, rows49 := bodyPxRadiusAt(t, "Earth", 181, 49)
	if rows40 != 37 || rows49 != 46 {
		t.Fatalf("canvas rows %d, %d; want 37, 46", rows40, rows49)
	}
	if math.Abs(px40-float64(render.BodyTextureMinRadius)) > 1e-6 {
		t.Errorf("Earth at 140x40: %.3f px, want exactly %d (unchanged from main)", px40, render.BodyTextureMinRadius)
	}
	if math.Abs(px49-15) > 1e-6 {
		t.Errorf("Earth at 181x49: %.3f px, want 15 (1.25x)", px49)
	}
}

func TestTerminalBodyUnaffectedByHeightScale(t *testing.T) {
	px40, _ := bodyPxRadiusAt(t, "Moon", 140, 40)
	px49, _ := bodyPxRadiusAt(t, "Moon", 181, 49)
	// The Moon has no moons: its floor stays the fixed 12 px at both sizes.
	if px40 != px49 || math.Abs(px40-float64(render.BodyTextureMinRadius)) > 1e-6 {
		t.Errorf("Moon base radius %.3f px at 140x40, %.3f at 181x49; want both %d", px40, px49, render.BodyTextureMinRadius)
	}
}

func TestZoomStillMultipliesScaledBase(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	v := newSOIPassTestView()
	v.Resize(181, 49)
	w.ViewMode = sim.ViewTilted
	for i, b := range w.System().Bodies {
		if b.EnglishName == "Earth" {
			w.Focus = sim.Focus{Kind: sim.FocusBody, BodyIdx: i}
		}
	}
	v.Render(w, 0, 181, 49)
	base := v.baseScale
	v.ZoomIn()
	v.Render(w, 0, 181, 49)
	if v.baseScale != base || math.Abs(v.canvas.Scale()-base*1.25) > base*1e-9 {
		t.Errorf("after +: base %.6e (was %.6e), scale %.6e, want base*1.25", v.baseScale, base, v.canvas.Scale())
	}
}
