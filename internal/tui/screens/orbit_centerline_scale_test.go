package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// ADR 0055 A4: the disc ranges itself through 2, 4, 8, 16, 32, 64 m. On entry
// the step is the smallest S with e <= 0.95 R (R = S / 2, e the ring's far
// edge); after that it steps out above 0.95 R and in below 0.45 R, and holds
// still between (so it never flickers).

func TestCenterlineEntryStep(t *testing.T) {
	// lateral metres -> entry step; e = lateral + 0.5.
	// 1.44 m is the ADR's "-> 4 m", but e = 1.94 > 0.95 x 2 = 1.9, so the
	// rule gives 8 m; 1.35 m is the 4 m scene (see slice-S1.md, Corrections).
	for _, c := range []struct{ lateral, want float64 }{
		{0, 2}, {0.3, 2}, {0.45, 2}, {0.46, 4}, {1.35, 4}, {1.44, 8},
		{4, 16}, {7, 16}, {7.2, 32}, {14, 32}, {15.1, 64}, {31, 64}, {500, 64},
	} {
		if got := centerlineStep(0, c.lateral+0.5); got != c.want {
			t.Errorf("entry at lateral %.2f m: step %v m, want %v m", c.lateral, got, c.want)
		}
	}
}

func TestCenterlineStepHysteresis(t *testing.T) {
	// From the 16 m step (R = 8): out above 7.6, in below 3.6, hold between.
	for _, c := range []struct{ prev, e, want float64 }{
		{16, 7.59, 16}, {16, 7.61, 32}, // out at e > 0.95 R
		{16, 3.61, 16}, {16, 3.59, 8}, // in at e < 0.45 R
		{16, 5, 16},
		{8, 3.81, 16}, // 8 m: out above 3.8
		{8, 3.79, 8}, {8, 1.81, 8}, {8, 1.79, 4},
		{2, 0.1, 2},    // nothing below 2 m
		{64, 1000, 64}, // nothing above 64 m
		{64, 0.5, 2},   // a long way in steps all the way down
		{2, 29, 64},    // and a long way out steps all the way up
	} {
		if got := centerlineStep(c.prev, c.e); got != c.want {
			t.Errorf("from %v m at e=%.2f: step %v m, want %v m", c.prev, c.e, got, c.want)
		}
	}
}

// Through the renderer: once on the 16 m step, a lateral that entry would
// call 8 m (e = 3.7, between 3.6 and 3.8) holds the 16 m step, and the title
// row says so. Fresh on the same scene it reads 8 m.
func TestCenterlineStepHoldsAcrossFramesThroughTheRenderer(t *testing.T) {
	w := ringWorld(t, 3.2, 0, 0)
	v, out := renderSplit(t, w)
	if line := frameLine(out, 1); !strings.Contains(line, "CENTERLINE  8 m") {
		t.Fatalf("fresh entry at 3.2 m lateral: title %q, want 8 m", line)
	}
	v.clStepM = 16
	out = v.Render(w, 0, DesignWidth, DesignHeight)
	if line := frameLine(out, 1); !strings.Contains(line, "CENTERLINE  16 m") {
		t.Errorf("held step: title %q, want it to stay 16 m", line)
	}
	if !strings.Contains(ansi.Strip(out), "16 m") {
		t.Error("no 16 m in the frame")
	}
}
