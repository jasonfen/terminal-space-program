package screens

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// vabColourTheme is a theme whose styles render to DISTINCT escape sequences
// under a forced colour profile (the zero Theme renders plain text, which
// makes any style assertion vacuous).
func vabColourTheme(t *testing.T) Theme {
	t.Helper()
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })
	st := func(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }
	th := Theme{Primary: st("51"), Warning: st("214"), Alert: st("203"), Dim: st("59"), Footer: st("59"), Title: st("51")}
	if th.Warning.Render("x") == th.Dim.Render("x") {
		t.Fatal("test setup broken: forced colour profile did not colour the styles")
	}
	return th
}

// TestVABWarningsUseWarningStyle (#500): the build-blocking warnings are in
// the warning (amber) style, not the dim grey of a disabled control.
func TestVABWarningsUseWarningStyle(t *testing.T) {
	th := vabColourTheme(t)
	v := NewVAB(th)
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng") // engine, no command source: a warning
	warns := v.Warnings()
	if len(warns) == 0 {
		t.Fatal("setup: expected at least one warning")
	}
	w := 60
	got := strings.Join(v.renderVehicleColumn(w, 0), "\n")
	for _, warn := range warns {
		want := th.Warning.Render(truncWidth("⚠ "+warn, w))
		if !strings.Contains(got, want) {
			t.Errorf("warning %q not rendered in the warning style", warn)
		}
	}
}

// TestVABUnselectedPartsNotDisabledGrey (#500): palette rows, vehicle group
// rows and the inspector body off-cursor are not drawn in the Dim style.
func TestVABUnselectedPartsNotDisabledGrey(t *testing.T) {
	th := vabColourTheme(t)
	v := NewVAB(th)
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.focus = focusPalette
	v.paletteIdx = 0
	pal := strings.Join(v.renderPalette(40), "\n")
	for _, it := range v.palette[1:4] {
		_, label := v.paletteItemLabel(it)
		if strings.Contains(pal, th.Dim.Render(label)) {
			t.Errorf("unselected palette part %q drawn in Dim", label)
		}
	}
	insp := strings.Join(v.renderInspector(40), "\n")
	if strings.Contains(insp, th.Dim.Render("structure")) || strings.Contains(insp, th.Dim.Render(fmt.Sprintf("%.0f kN", 0.0))) {
		t.Error("inspector body drawn in Dim")
	}
	for _, r := range v.renderInspector(40)[1:] {
		// every body row is border + content; content must not be all-Dim
		if strings.Contains(r, th.Dim.Render("tank ·")) || strings.Contains(r, th.Dim.Render("engine ·")) {
			t.Errorf("inspector row drawn in Dim: %q", r)
		}
	}
	vc := strings.Join(v.renderVehicleColumn(60, 0), "\n")
	if strings.Contains(vc, th.Dim.Render("Big Tank")) {
		t.Error("vehicle group row off-cursor drawn in Dim")
	}
}

// TestVABInspectorFollowsFocusedColumn (#501): with the vehicle column
// focused, the inspector describes the vehicle row under the cursor, not the
// palette item.
func TestVABInspectorFollowsFocusedColumn(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	// Palette cursor on an engine; vehicle cursor on the tank group.
	for i, it := range v.palette {
		if it.id == "eng2" {
			v.paletteIdx = i
		}
	}
	v.focus = focusStack
	for i, r := range v.stackRows() {
		if r.isHeader() {
			continue
		}
		if g := v.rowGroups(r.stageIdx)[r.group]; g.compID == "tank" {
			v.stackCursor = i
		}
	}
	got := strings.Join(v.renderInspector(40), "\n")
	if !strings.Contains(got, "Big Tank") || strings.Contains(got, "Vac Engine") {
		t.Errorf("stack focus: inspector should describe Big Tank, got:\n%s", got)
	}
	v.focus = focusPalette
	got = strings.Join(v.renderInspector(40), "\n")
	if !strings.Contains(got, "Vac Engine") {
		t.Errorf("palette focus: inspector should describe Vac Engine, got:\n%s", got)
	}
}

// TestVABInspectorIsWholeBox (#501): the inspector has a closing bottom edge
// and a right edge on every row, at one consistent width.
func TestVABInspectorIsWholeBox(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	lines := v.renderInspector(40)
	if len(lines) < 3 {
		t.Fatalf("inspector too short: %v", lines)
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰") || !strings.HasSuffix(lines[len(lines)-1], "╯") {
		t.Errorf("no bottom edge: %q", lines[len(lines)-1])
	}
	for _, l := range lines {
		if lipgloss.Width(l) != 40 {
			t.Errorf("row width %d != 40: %q", lipgloss.Width(l), l)
		}
	}
	for _, l := range lines[1 : len(lines)-1] {
		if !strings.HasSuffix(l, "│") {
			t.Errorf("row lacks right edge: %q", l)
		}
	}
}

// TestVABNamingScreenShowsVehicle (#501): `s` shows the vehicle being named.
func TestVABNamingScreenShowsVehicle(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.addComponentToCurrent("tank")
	v.HandleKey("s")
	out := v.Render(140, 40)
	for _, want := range []string{"Big Engine", "Big Tank", "Δv"} {
		if !strings.Contains(out, want) {
			t.Errorf("naming screen missing %q:\n%s", want, out)
		}
	}
}

// TestVABVehicleColumnWindowsAroundCursor (#501): a 12-stage build keeps the
// cursor row visible and the column within the screen height.
func TestVABVehicleColumnWindowsAroundCursor(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	for i := 0; i < 12; i++ {
		v.newStage()
		v.addComponentToCurrent("eng")
		v.addComponentToCurrent("tank")
	}
	const h = 40
	for _, cursor := range []int{0, len(v.stackRows()) / 2, len(v.stackRows()) - 1} {
		v.focus = focusStack
		v.stackCursor = cursor
		out := v.Render(140, h)
		if n := strings.Count(out, "\n") + 1; n > h {
			t.Errorf("cursor %d: render is %d rows, want <= %d", cursor, n, h)
		}
		if !strings.Contains(out, "→ ") {
			t.Errorf("cursor %d: cursor marker scrolled out of view", cursor)
		}
		r := v.stackRows()[cursor]
		if r.isHeader() && !strings.Contains(out, fmt.Sprintf("S%d ", r.stageIdx+1)) {
			t.Errorf("cursor %d: its stage header S%d not visible", cursor, r.stageIdx+1)
		}
	}
}
