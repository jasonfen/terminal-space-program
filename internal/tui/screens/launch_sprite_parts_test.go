package screens

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

// #596: a stage built from parts used to have no sprite rows and was
// skipped, so the upper stages sat straight on the first.

func partsStack(t *testing.T) []spacecraft.Stage {
	t.Helper()
	first, ok := spacecraft.BuildStage("s-ic")
	if !ok {
		t.Fatal("no s-ic")
	}
	mid, warn := spacecraft.ComposeStage([]string{"j2-upper", "hydro-tank-25k", "hydro-tank-25k", "hydro-tank-25k", "hydro-tank-25k"}, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	top, _ := spacecraft.BuildStage("csm")
	return []spacecraft.Stage{first, mid, top}
}

func upBasis() (orbital.Vec3, widgets.Basis) {
	return orbital.Vec3{Z: 1}, widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Z: 1}}
}

func TestComposeLaunchSprite_PartsBuiltStageIsDrawn(t *testing.T) {
	stages := partsStack(t)
	cmd, basis := upBasis()
	px := ComposeLaunchSprite(stages, cmd, basis, 1.0)

	mid := stages[1]
	var midPx int
	for _, p := range px {
		if string(p.Color) == mid.LaunchSpriteColor {
			midPx++
		}
	}
	if midPx == 0 {
		t.Fatalf("parts-built stage (colour %s) has no pixels in the drawing", mid.LaunchSpriteColor)
	}
	// It takes its own place in the stack: the three stages' rows plus two
	// separators (no taper below 6 rows is irrelevant here; count it).
	want := 0
	for _, r := range launchStackLayout(stages) {
		want += r.rows
	}
	if got := composedStackRows(stages); got != want {
		t.Errorf("composedStackRows=%d layout rows=%d", got, want)
	}
	if got := composedStackRows([]spacecraft.Stage{stages[0], stages[2]}); got >= composedStackRows(stages) {
		t.Errorf("dropping the parts-built stage did not shorten the drawing (%d vs %d)", got, composedStackRows(stages))
	}
}

func TestComposeLaunchSprite_PartsBuiltColoursDiffer(t *testing.T) {
	kero, _ := spacecraft.ComposeStage([]string{"merlin-sustainer", "kero-tank-9k"}, spacecraft.Components)
	hydro, _ := spacecraft.ComposeStage([]string{"rl10-vac", "hydro-tank-8k"}, spacecraft.Components)
	if kero.LaunchSpriteColor == "" || kero.LaunchSpriteColor == hydro.LaunchSpriteColor {
		t.Fatalf("kerolox %q and hydrolox %q stages should differ", kero.LaunchSpriteColor, hydro.LaunchSpriteColor)
	}
	cmd, basis := upBasis()
	seen := map[lipgloss.Color]bool{}
	for _, p := range ComposeLaunchSprite([]spacecraft.Stage{kero, hydro}, cmd, basis, 1) {
		seen[p.Color] = true
	}
	if !seen[lipgloss.Color(kero.LaunchSpriteColor)] || !seen[lipgloss.Color(hydro.LaunchSpriteColor)] {
		t.Errorf("both stage colours should be painted, saw %v", seen)
	}
}

// Two parts-built stages of one chemistry share a derived colour; the upper
// one is painted darker so they still read as two blocks.
func TestLaunchStackLayout_SameColourNeighboursDiffer(t *testing.T) {
	a, _ := spacecraft.ComposeStage([]string{"rl10-vac", "hydro-tank-8k"}, spacecraft.Components)
	b, _ := spacecraft.ComposeStage([]string{"rl10-vac", "hydro-tank-2k"}, spacecraft.Components)
	if a.LaunchSpriteColor != b.LaunchSpriteColor {
		t.Fatalf("test premise: same chemistry should share a colour (%s vs %s)", a.LaunchSpriteColor, b.LaunchSpriteColor)
	}
	var bodies []stackRect
	for _, r := range launchStackLayout([]spacecraft.Stage{a, b}) {
		if r.kind == rectBody {
			bodies = append(bodies, r)
		}
	}
	if len(bodies) != 2 || bodies[0].color == bodies[1].color {
		t.Fatalf("adjacent same-chemistry stages painted alike: %+v", bodies)
	}
}

func TestShadeHex(t *testing.T) {
	if got := shadeHex("#FF8040", 0.5); got != "#804020" {
		t.Errorf("shadeHex = %s, want #804020", got)
	}
	if got := shadeHex("red", 0.5); got != "red" {
		t.Errorf("non-hex changed: %s", got)
	}
}
