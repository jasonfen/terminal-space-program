package screens

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// Real CSI sequences, the way the canvas's colored String() emits
// them (per-cell set + reset). Hand-built so the test is independent
// of lipgloss's environment-dependent color profile.
const (
	tRed   = "\x1b[38;2;255;0;0m"
	tReset = "\x1b[0m"
)

// splitStyledCells must yield exactly one element per visible cell,
// with styled cells carrying their own self-contained escapes, so a
// spliced line keeps the same display width as the original.
func TestSplitStyledCells(t *testing.T) {
	line := "a" + tRed + "b" + tReset + "c"
	cells := splitStyledCells(line)
	if len(cells) != 3 {
		t.Fatalf("got %d cells, want 3: %q", len(cells), line)
	}
	if got := stripANSI(strings.Join(cells, "")); got != "abc" {
		t.Errorf("stripped join = %q, want \"abc\"", got)
	}
	if cells[0] != "a" || cells[2] != "c" {
		t.Errorf("plain cells should be bare runes: %q %q", cells[0], cells[2])
	}
	if cells[1] != tRed+"b"+tReset {
		t.Errorf("middle cell = %q, want self-contained styled rune", cells[1])
	}
	if lipgloss.Width(strings.Join(cells, "")) != 3 {
		t.Errorf("re-joined width != 3")
	}
}

// Regression: a single styled run spanning many runes (how lipgloss
// emits a border edge or "NAVBALL") must split to one cell per rune,
// not one cell + a stray zero-width reset. The stray cell used to
// inflate the count and shove the panel's right border off the
// splice, dropping the top-right corner / right `│`.
func TestSplitStyledCellsMultiRuneRun(t *testing.T) {
	run := tRed + "ABCDE" + tReset // one SET, 5 runes, one RESET
	cells := splitStyledCells("x" + run + "y")
	if len(cells) != 7 {
		t.Fatalf("got %d cells, want 7 (x + 5 + y): %v", len(cells), cells)
	}
	if cells[0] != "x" || cells[6] != "y" {
		t.Errorf("plain edges wrong: %q %q", cells[0], cells[6])
	}
	for i, want := range []string{"A", "B", "C", "D", "E"} {
		c := cells[1+i]
		if !strings.HasPrefix(c, tRed) || !strings.Contains(c, want) ||
			!strings.HasSuffix(c, "\x1b[0m") {
			t.Errorf("cell %d = %q, want self-contained styled %q", 1+i, c, want)
		}
	}
	if lipgloss.Width(strings.Join(cells, "")) != 7 {
		t.Errorf("re-joined width != 7")
	}
}

// overlayStyledBlock must splice a block in without changing the
// base lines' visible width and without touching out-of-range rows.
func TestOverlayStyledBlock(t *testing.T) {
	base := []string{
		strings.Repeat(" ", 10),
		strings.Repeat(" ", 10),
		strings.Repeat(" ", 10),
	}
	block := "XY\nZW"
	out := overlayStyledBlock(base, block, 1, 2, 10)

	if stripANSI(out[0]) != strings.Repeat(" ", 10) {
		t.Errorf("row 0 should be untouched, got %q", stripANSI(out[0]))
	}
	if got := stripANSI(out[1]); got != "  XY      " {
		t.Errorf("row 1 = %q, want %q", got, "  XY      ")
	}
	if got := stripANSI(out[2]); got != "  ZW      " {
		t.Errorf("row 2 = %q, want %q", got, "  ZW      ")
	}
	for i, l := range out {
		if w := lipgloss.Width(l); w != 10 {
			t.Errorf("row %d width = %d, want 10", i, w)
		}
	}
}

// overlayStyledBlock must preserve styling that already exists in the
// base line outside the spliced span (the canvas emits colored cells).
func TestOverlayPreservesBaseStyling(t *testing.T) {
	base := []string{tRed + "R" + tReset + strings.Repeat(" ", 9)}
	out := overlayStyledBlock(base, "Z", 0, 5, 10)
	if got := stripANSI(out[0]); got != "R    Z    " {
		t.Errorf("stripped = %q, want %q", got, "R    Z    ")
	}
	if !strings.Contains(out[0], tRed) {
		t.Errorf("base red styling was lost: %q", out[0])
	}
	if lipgloss.Width(out[0]) != 10 {
		t.Errorf("width = %d, want 10", lipgloss.Width(out[0]))
	}
}

// buildNavballPanel produces a bordered block of the declared size:
// no "NAVBALL" label, a [MODE]+RCS toggle row, and the eight SAS
// glyphs down the left. Every row must be exactly navballPanelW
// cells (the splice-alignment invariant) and a hit box recorded for
// each control (Mode + RCS + 8 glyphs).
func TestBuildNavballPanel(t *testing.T) {
	v := NewOrbitView(Theme{
		Primary: lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
	})
	disk := render.NavballString(navballDiskCols, navballDiskRows, 0, 0, nil)
	panel, boxes := v.buildNavballPanel(disk, sim.NavOrbit, false)

	plain := stripANSI(panel)
	if strings.Contains(plain, "NAVBALL") {
		t.Errorf("NAVBALL label should be gone:\n%s", plain)
	}
	for _, want := range []string{"[ORBIT]", "RCS", "PRO", "RET", "T+", "T-"} {
		if !strings.Contains(plain, want) {
			t.Errorf("panel missing %q:\n%s", want, plain)
		}
	}
	for _, b := range navballAxisRow {
		if !strings.ContainsRune(plain, b.glyph) {
			t.Errorf("panel missing glyph %q:\n%s", string(b.glyph), plain)
		}
		if w := lipgloss.Width(string(b.glyph)); w != 1 {
			t.Errorf("glyph %q width = %d, want 1 (splice invariant)", string(b.glyph), w)
		}
	}
	rows := strings.Split(panel, "\n")
	if len(rows) != navballPanelH {
		t.Errorf("panel height = %d, want %d", len(rows), navballPanelH)
	}
	for i, r := range rows {
		if w := lipgloss.Width(r); w != navballPanelW {
			t.Errorf("panel row %d width = %d, want %d", i, w, navballPanelW)
		}
		// Definitive splice guard: split must yield exactly one cell
		// per display column (the right-border-drop regression).
		if c := len(splitStyledCells(r)); c != navballPanelW {
			t.Errorf("panel row %d splits to %d cells, want %d", i, c, navballPanelW)
		}
	}
	// Mode + RCS + navballBtnRows hit-rows per axis button (each
	// button is a multi-row click target).
	wantBoxes := 2 + len(navballAxisRow)*navballBtnRows
	if len(boxes) != wantBoxes {
		t.Fatalf("got %d control boxes, want %d", len(boxes), wantBoxes)
	}
	sawMode, sawRCS := false, false
	for _, b := range boxes {
		switch b.id {
		case NavballControlMode:
			sawMode = true
		case NavballControlRCS:
			sawRCS = true
		}
		if b.colEnd <= b.colStart {
			t.Errorf("box %d has empty col range [%d,%d)", b.id, b.colStart, b.colEnd)
		}
	}
	if !sawMode {
		t.Errorf("no NavballControlMode box recorded")
	}
	if !sawRCS {
		t.Errorf("no NavballControlRCS box recorded")
	}
}

// End-to-end: at a generous terminal size the panel renders into the
// frame and HitNavballControl resolves a click at each control's
// centre back to that control's id.
func TestHitNavballControl(t *testing.T) {
	th := Theme{
		Primary: lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle().Border(lipgloss.RoundedBorder()),
		Title:   lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
	}
	v := NewOrbitView(th)
	v.Resize(220, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	_ = v.Render(w, 0, 220, 60)
	if len(v.navballControls) == 0 {
		t.Skip("panel not shown at this size on this build; layout-dependent")
	}
	for _, b := range v.navballControls {
		midCol := (b.colStart + b.colEnd) / 2
		got, ok := v.HitNavballControl(midCol, b.row)
		if !ok || got != b.id {
			t.Errorf("center of box id=%d (col %d,row %d): got id=%d ok=%v",
				b.id, midCol, b.row, got, ok)
		}
	}
	if _, ok := v.HitNavballControl(-5, -5); ok {
		t.Errorf("off-panel click should miss")
	}
}
