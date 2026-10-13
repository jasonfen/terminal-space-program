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

// L1 (review of #638): the same-colour shading is for parts-built stages
// (they share a derived chemistry colour). Authored catalog loadouts paint
// exactly as they did before the rule: the relay carriers author one green
// for every stage on purpose.
func TestLaunchStackLayout_CatalogLoadoutsKeepAuthoredColours(t *testing.T) {
	forceTrueColor(t)
	cmd, basis := upBasis()
	checked := 0
	for _, id := range spacecraft.LoadoutOrder {
		l := spacecraft.Loadouts[id]
		allowed := map[lipgloss.Color]bool{stageSeparatorColor(): true}
		for _, st := range l.Stages {
			allowed[stageSpriteColor(st)] = true
			allowed[lipgloss.Color(st.Color)] = true // landing legs
		}
		for _, r := range launchStackLayout(l.Stages) {
			if r.kind == rectSeparator {
				continue
			}
			if want := stageSpriteColor(l.Stages[r.stage]); r.color != want {
				t.Errorf("%s: stage %d painted %s, authored %s", id, r.stage, r.color, want)
			}
		}
		for _, p := range ComposeLaunchSprite(l.Stages, cmd, basis, 1) {
			if !allowed[p.Color] {
				t.Errorf("%s: pixel colour %s is not an authored stage colour", id, p.Color)
				break
			}
		}
		checked++
	}
	if checked < 30 {
		t.Fatalf("only %d catalog loadouts checked; instrument blind", checked)
	}
}

// The relay carriers are the loadouts the unconditional rule recoloured;
// pin that each really has two adjacent drawn stages authored in one colour,
// so the test above has teeth.
func TestLaunchStackLayout_RelayCarriersHaveSameColourNeighbours(t *testing.T) {
	for _, id := range []string{"Comsat-Carrier-3", "Node-Carrier-3", "Survey-Pack", "Scan-Pack"} {
		l, ok := spacecraft.Loadouts[id]
		if !ok {
			t.Fatalf("no loadout %s", id)
		}
		var prev lipgloss.Color
		same := false
		for _, st := range l.Stages {
			if st.LaunchSpriteRowsPx <= 0 {
				continue
			}
			if c := stageSpriteColor(st); prev != "" && c == prev {
				same = true
			}
			prev = stageSpriteColor(st)
		}
		if !same {
			t.Errorf("%s: no adjacent same-colour stages; premise of the L1 guard changed", id)
		}
	}
}

// Parts-built next to authored still shades (one derived stage is enough);
// two authored stages of one colour do not.
func TestLaunchStackLayout_ShadeNeedsADerivedStage(t *testing.T) {
	mk := func(derived bool) spacecraft.Stage {
		return spacecraft.Stage{LaunchSpriteRowsPx: 8, LaunchSpriteWidthPx: 3, LaunchSpriteColor: "#8CE0C0", LaunchSpriteDerived: derived}
	}
	body := func(stages ...spacecraft.Stage) []lipgloss.Color {
		var cs []lipgloss.Color
		for _, r := range launchStackLayout(stages) {
			if r.kind == rectBody {
				cs = append(cs, r.color)
			}
		}
		return cs
	}
	if cs := body(mk(false), mk(false)); cs[0] != cs[1] {
		t.Errorf("two authored same-colour stages were recoloured: %v", cs)
	}
	for _, pair := range [][2]bool{{false, true}, {true, false}, {true, true}} {
		if cs := body(mk(pair[0]), mk(pair[1])); cs[0] == cs[1] {
			t.Errorf("derived=%v: same-colour neighbours painted alike: %v", pair, cs)
		}
	}
}
