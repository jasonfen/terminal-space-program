package screens

import (
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// setNose points the slewed nose (CurrentAttitudeDir) pitchDeg above the
// local horizon on bearing hdgDeg, using the same east = spin x up frame
// the production trims use. No integrator runs: the test sets the nose and
// renders, so it covers the render seam, not slew dynamics.
func setNose(w *sim.World, pitchDeg, hdgDeg float64) {
	c := w.ActiveCraft()
	ax := render.BodyRotationAxisWorld(c.Primary)
	spin := orbital.Vec3{X: ax.X, Y: ax.Y, Z: ax.Z}
	up := c.State.R.Scale(1 / c.State.R.Norm())
	east := spin.Cross(up)
	east = east.Scale(1 / east.Norm())
	north := up.Cross(east)
	p, h := pitchDeg*math.Pi/180, hdgDeg*math.Pi/180
	horiz := east.Scale(math.Sin(h)).Add(north.Scale(math.Cos(h)))
	c.CurrentAttitudeDir = horiz.Scale(math.Cos(p)).Add(up.Scale(math.Sin(p)))
}

// TestNavballReadoutBesideProFace (G8 Q3): the composed panel shows the
// measured nose, "pitch +62° hdg 088°" with two spaces between, on the
// "⊕ PRO" row, in SURF and ORBIT nav modes alike, at the Design Size and at
// 181x49. Seam: OrbitView.ComposeNavballOverlay, the call orbit and launch
// views make.
func TestNavballReadoutBesideProFace(t *testing.T) {
	const want = "pitch +62°  hdg 088°"
	for _, sz := range [][2]int{{138, 37}, {179, 46}} {
		for _, nm := range []sim.NavMode{sim.NavSurface, sim.NavOrbit} {
			w, _ := spawnSaturnVOnPad(t)
			w.NavMode = nm
			w.InstantSAS = false
			setNose(w, 62, 88)
			v := NewOrbitView(Theme{Primary: lipgloss.NewStyle(), Dim: lipgloss.NewStyle(), Warning: lipgloss.NewStyle()})
			out := v.ComposeNavballOverlay(w, blankCanvas(sz[0], sz[1]), sz[0], sz[1])
			var proRow string
			for _, l := range strings.Split(stripANSI(out), "\n") {
				if strings.Contains(l, "PRO") {
					proRow = l
				}
			}
			if !strings.Contains(proRow, want) {
				t.Errorf("%dx%d %v: PRO row %q lacks %q", sz[0], sz[1], nm, proRow, want)
			}
			if !strings.Contains(proRow, "⊕ PRO") {
				t.Errorf("%dx%d %v: PRO face lost: %q", sz[0], sz[1], nm, proRow)
			}
			for i, l := range strings.Split(out, "\n") {
				if lipgloss.Width(l) != sz[0] {
					t.Fatalf("%dx%d %v: composed row %d is %d wide, want %d", sz[0], sz[1], nm, i, lipgloss.Width(l), sz[0])
				}
			}
		}
	}
}

// TestNavballReadoutNotWaitingForDeadband: the ball's sub-observer holds a
// 2 degree dead-band, the readout does not. A 1 degree nose move changes
// the readout on the next render.
func TestNavballReadoutNotWaitingForDeadband(t *testing.T) {
	w, _ := spawnSaturnVOnPad(t)
	w.InstantSAS = false
	setNose(w, 40, 90)
	if got := navballReadoutLabel(w); got != "pitch +40°  hdg 090°" {
		t.Fatalf("readout = %q", got)
	}
	setNose(w, 41, 91)
	if got := navballReadoutLabel(w); got != "pitch +41°  hdg 091°" {
		t.Errorf("1 degree move not reflected: %q", got)
	}
}

// TestNavballReadoutFitsPanel: the longest reading ("pitch -90°  hdg 359°",
// 20 cells) fits the disk region at every panel size.
func TestNavballReadoutFitsPanel(t *testing.T) {
	const longest = "pitch -90°  hdg 359°"
	for _, rows := range []int{designCanvasRows, 46, 67} {
		g := navballGeometry(designCanvasCols, rows)
		if w := lipgloss.Width(longest); w > g.diskRegionW {
			t.Errorf("%d rows: readout %d cells wider than the disk region %d", rows, w, g.diskRegionW)
		}
	}
}

// TestNavballPadRoseFollowsHeadingTrim (G8 Q5): on the pad the nose sits on
// the pole, so a heading trim moves only the ball's longitude, never its
// great-circle position. The sticky 2 degree dead-band compares positions,
// so on its own it would freeze the rose when you press the heading keys.
// Seam: the same OrbitView composes two frames, as the live screen does.
func TestNavballPadRoseFollowsHeadingTrim(t *testing.T) {
	w, c := spawnSaturnVOnPad(t)
	w.NavMode = sim.NavSurface
	w.InstantSAS = false
	v := NewOrbitView(Theme{Primary: lipgloss.NewStyle(), Dim: lipgloss.NewStyle(), Warning: lipgloss.NewStyle()})
	const cols, rows = 138, 37
	// The compass E tick: the only 'E' inside the disk region (the panel's
	// button labels are digits-free caps in the left column, so scan from
	// the disk's first column rightwards).
	g := navballGeometry(cols, rows)
	diskLeft := cols - g.panelW + 1 + navballGlyphColW
	eCell := func() (int, int) {
		out := stripANSI(v.ComposeNavballOverlay(w, blankCanvas(cols, rows), cols, rows))
		for r, l := range strings.Split(out, "\n") {
			cells := []rune(l)
			for ci := diskLeft; ci < len(cells); ci++ {
				if cells[ci] == 'E' {
					return r, ci
				}
			}
		}
		return -1, -1
	}
	setNose(w, 90, 90)
	r0, c0 := eCell()
	if r0 < 0 {
		t.Fatal("no compass E on the pad ball")
	}
	c.HeadingTrim = -40 * math.Pi / 180 // commanded bearing 050
	setNose(w, 90, 50)
	r1, c1 := eCell()
	if r1 == r0 && c1 == c0 {
		t.Errorf("rose did not move after a 40 degree heading trim: E stayed at row %d col %d", r0, c0)
	}
}
