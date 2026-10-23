package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0055 A6: their port behind the plane through yours. Fixtures put their
// port dead on your axis (lateral 0) at 4.12 m ahead (x > 0) or past (x < 0),
// so the ring is 14 cells across at the 2 m step in both and only the sign of
// the axial offset differs.
func axialWorld(t *testing.T, x float64) *sim.World {
	t.Helper()
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: x}, 0)
	st, ok := w.ProximityState()
	if !ok || !st.HasPort {
		t.Fatal("fixture has no port scene")
	}
	return w
}

// titleLine is the scope panel's title row only (the left chip has hyphens).
func titleLine(frame string) string {
	return string([]rune(frameLine(frame, 1))[clPanelCol : clPanelCol+clPanelW])
}

// discRow is canvas row r restricted to the scope's disc columns, so the
// navball's own '◌' (the anti-target mark) is never counted.
func discRow(frame string, r int) string {
	return string([]rune(frameLine(frame, r))[clDiscCol0 : clDiscCol0+clDiscCols])
}

// Behind: the ring is the reverse glyph at the ring's centre and there is no
// standoff cross. Ahead: the ring and the cross. The ring-vs-glyph test reads
// the frame, so a test of PortRangeM (positive both ways) cannot pass it.
func TestCenterlineBehindDrawsReverseGlyphAndNoCross(t *testing.T) {
	cx, cy := cellAt(0, 0)
	behind := axialWorld(t, -4.12)
	if st, _ := behind.ProximityState(); st.PortAxialM >= 0 {
		t.Fatalf("fixture: PortAxialM = %g, want negative", st.PortAxialM)
	}
	_, out := renderSplit(t, behind)
	if got := frameRune(out, clDiscCol0+cx, clDiscRow0+cy); got != '◌' {
		t.Errorf("behind: crosshair cell = %q, want the reverse glyph '◌'", got)
	}
	if _, _, ok := crossCell(out); ok {
		t.Error("behind: standoff cross drawn")
	}

	ahead := axialWorld(t, 4.12)
	_, out = renderSplit(t, ahead)
	for r := clDiscRow0; r < clDiscRow0+clDiscRows; r++ {
		if strings.ContainsRune(discRow(out, r), '◌') {
			t.Errorf("ahead: '◌' on canvas row %d", r)
		}
	}
	if _, _, ok := crossCell(out); !ok {
		t.Error("ahead: no standoff cross")
	}
}

// The glyph replaces the ring at every scale, not only the small ones, and
// it stays on the rim beyond the last step.
func TestCenterlineBehindGlyphAtAnyScale(t *testing.T) {
	w := portedPairWorld(t, []string{"rcs-tug"}, []string{"nudge-tug"}, orbital.Vec3{X: -4.12, Y: -6}, 0) // 16 m step
	_, out := renderSplit(t, w)
	n := 0
	for r := clDiscRow0; r < clDiscRow0+clDiscRows; r++ {
		n += strings.Count(discRow(out, r), "◌")
	}
	if n != 1 {
		t.Errorf("'◌' count in the disc = %d, want 1", n)
	}
}

func TestCenterlineTitleSignsTheRangeWhileBehind(t *testing.T) {
	for _, c := range []struct {
		x    float64
		want string
	}{{-4.12, "port -4.12 m"}, {4.12, "port 4.12 m"}} {
		_, out := renderSplit(t, axialWorld(t, c.x))
		if got := titleLine(out); !strings.Contains(got, c.want) || (c.x > 0 && strings.Contains(got, "-")) {
			t.Errorf("x=%g: title row = %q, want %q", c.x, got, c.want)
		}
	}
}

// The chip's port: row keeps the unsigned distance either way.
func TestCenterlineChipPortRowUnsignedBothWays(t *testing.T) {
	for _, x := range []float64{-4.12, 4.12} {
		v := NewOrbitView(chipTestTheme())
		v.Resize(DesignWidth, DesignHeight)
		row := chipLine(v.buildProximityChip(axialWorld(t, x)), "port:")
		if !strings.Contains(row, "4.12 m") || strings.Contains(row, "-4.12") {
			t.Errorf("x=%g: port: row = %q, want 4.12 m unsigned", x, row)
		}
	}
}

// The glyph is TARGET-coloured like the ring it replaces. lipgloss is
// colourless under go test, so force a profile or the assert is vacuous.
func TestCenterlineBehindGlyphIsTargetColoured(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
	_, out := renderSplit(t, axialWorld(t, -4.12))
	cx, cy := cellAt(0, 0)
	line := strings.Split(out, "\n")[clDiscRow0+cy+clFrameLine]
	// The ring's colour is the TARGET colour; the cell is wrapped in its escape.
	want := lipgloss.NewStyle().Foreground(render.ColorTarget).Render("◌")
	if !strings.Contains(line, strings.TrimSuffix(want, "\x1b[0m")) {
		t.Errorf("disc cell (%d, %d) is not '◌' in TARGET colour; want prefix %q in %q", cx, cy, want, line)
	}
}
