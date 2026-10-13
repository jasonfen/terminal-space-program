package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #596: the VAB gains a picture of the rocket, drawn by the same function
// that draws the pad rocket.

// fgEscape is the foreground escape lipgloss writes for a #RRGGBB under the
// forced profile (taken from lipgloss itself, which rounds channels).
func fgEscape(hex string) string {
	out := lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("x")
	return strings.TrimSuffix(strings.SplitN(out, "x", 2)[0], "m")
}

func litRows(lines []string) int {
	n := 0
	for _, ln := range lines {
		for _, r := range ln {
			if r >= 0x2801 && r <= 0x28FF {
				n++
				break
			}
		}
	}
	return n
}

func partsVAB(t *testing.T) *VAB {
	t.Helper()
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	v.stages = []vabStage{
		{catalogPartID: "s-ic"},
		{components: []string{"j2-upper", "hydro-tank-25k", "hydro-tank-25k", "hydro-tank-25k", "hydro-tank-25k"}},
		{catalogPartID: "csm"},
	}
	return v
}

func TestVABPicture_Geometry(t *testing.T) {
	v := partsVAB(t)
	lines := v.renderPicture(40, 18)
	if len(lines) != 18 {
		t.Fatalf("picture is %d lines, want 18", len(lines))
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w != 40 {
			t.Errorf("line %d is %d cells wide, want 40", i, w)
		}
	}
	if litRows(lines) < 8 {
		t.Errorf("only %d rows lit; a three-stage rocket should stand taller", litRows(lines))
	}
}

// The stage built from parts shows up in the picture in its own colour, and
// taking it out makes the picture shorter (it used to be skipped).
func TestVABPicture_PartsBuiltStageIsInThePicture(t *testing.T) {
	forceTrueColor(t)
	v := partsVAB(t)
	mid := v.resolveStage(v.stages[1])
	if mid.LaunchSpriteColor == "" {
		t.Fatal("test premise: parts-built stage has no sprite colour")
	}
	full := v.renderPicture(80, 24)
	if !strings.Contains(strings.Join(full, "\n"), fgEscape(mid.LaunchSpriteColor)) {
		t.Errorf("picture lacks the parts-built stage colour %s", mid.LaunchSpriteColor)
	}
	v.stages = []vabStage{v.stages[0], v.stages[2]}
	if without := v.renderPicture(80, 24); litRows(without) >= litRows(full) {
		t.Errorf("removing the parts-built stage did not shorten the picture (%d vs %d rows)", litRows(without), litRows(full))
	}
}

// Same drawing as the pad: height in cells matches the launch composer's
// stack (plus the engine bell), nothing invented by the VAB.
func TestVABPicture_SameStackAsLaunchComposer(t *testing.T) {
	v := partsVAB(t)
	stages := v.resolvedStages()
	rows := composedStackRows(stages) + EngineBellRows(stages)
	wantCells := (rows + 3) / 4
	got := litRows(v.renderPicture(40, 30))
	if got < wantCells-1 || got > wantCells+1 {
		t.Errorf("picture spans %d cell rows, launch composer stack needs ~%d (%d sub-rows)", got, wantCells, rows)
	}
}

// A stack taller than the box is scaled to fit, not clipped.
func TestVABPicture_TallStackScalesToFit(t *testing.T) {
	forceTrueColor(t)
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	v.stages = nil
	for i := 0; i < 4; i++ {
		v.stages = append(v.stages, vabStage{catalogPartID: "s-ic"})
	}
	v.stages = append(v.stages, vabStage{catalogPartID: "csm"})
	lines := v.renderPicture(30, 10)
	top := v.resolveStage(v.stages[4])
	if !strings.Contains(strings.Join(lines, "\n"), fgEscape(string(stageSpriteColor(top)))) {
		t.Error("the top stage (CSM) was clipped off the picture instead of the stack scaling to fit")
	}
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10", len(lines))
	}
	if litRows(lines) < 9 {
		t.Errorf("scaled stack should fill the box, lit %d of 10 rows", litRows(lines))
	}
}

// The stage under the VAB cursor is marked on the drawing.
func TestVABPicture_MarksFocusedStage(t *testing.T) {
	v := partsVAB(t)
	v.stageIdx = 1
	joined := strings.Join(v.renderPicture(80, 24), "\n")
	if !strings.Contains(joined, "▸ S2") {
		t.Errorf("focused stage S2 not marked:\n%s", joined)
	}
	if !strings.Contains(joined, "S3") || strings.Contains(joined, "▸ S3") {
		t.Errorf("other stages should be labelled but not marked:\n%s", joined)
	}
	v.stageIdx = 2
	if !strings.Contains(strings.Join(v.renderPicture(80, 24), "\n"), "▸ S3") {
		t.Error("marker did not follow the cursor to S3")
	}
}

// At the 140x40 design size the picture sits under the stage list and the key
// hints stay on screen.
func TestVABRender_PictureBoxKeepsKeysAtDesignSize(t *testing.T) {
	v := partsVAB(t)
	out := v.Render(140, 38)
	for _, want := range []string{"PICTURE", "[s] save", "[o] open", "[enter] open part into pieces", "[esc] back"} {
		if !strings.Contains(out, want) {
			t.Errorf("140-wide VAB lacks %q", want)
		}
	}
	if n := len(strings.Split(out, "\n")); n != 38 {
		t.Errorf("VAB is %d lines, want exactly 38", n)
	}
}

// Short terminal: the picture gives way, the keys do not.
func TestVABRender_ShortTerminalDropsPictureNotKeys(t *testing.T) {
	v := partsVAB(t)
	out := v.Render(100, 30)
	if strings.Contains(out, "PICTURE") {
		t.Error("picture should not squeeze the stage list on a short terminal")
	}
	for _, want := range []string{"[s] save", "[o] open"} {
		if !strings.Contains(out, want) {
			t.Errorf("short VAB lacks %q", want)
		}
	}
}

// An empty stack says why the box is blank rather than leaving it silent.
func TestVABPicture_EmptyStackSaysSo(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(spacecraft.Components)
	lines := v.renderPicture(60, 12)
	if len(lines) != 12 || !strings.Contains(strings.Join(lines, "\n"), "nothing to draw yet") {
		t.Errorf("empty stack picture should say so, got %q", lines)
	}
}
