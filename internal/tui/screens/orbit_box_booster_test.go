package screens

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0058 decisions 6-8, guards [8] and [9]. Every expected string below
// is hand-worked in work/2026-10-22-sls-parallel-staging-grill/slice-S5.md
// before being pinned; TWR uses the game's own Earth surface g
// (G*M/R^2 = 9.8203), not g0, so SLS at T+42 reads 2.14 where the ADR's
// g0 arithmetic said 2.15 (logged as a code/ADR disagreement).

const testG0 = 9.80665

// boosterWorld puts c on the default world's active slot, mid manual burn
// for elapsed seconds, with the RCS pool zeroed so TotalMass is the ADR's
// stage sum.
func boosterWorld(t *testing.T, c *spacecraft.Spacecraft, elapsed float64) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c.Primary = w.ActiveCraft().Primary
	for i := range c.Stages {
		c.Stages[i].MonopropMass, c.Stages[i].MonopropCap = 0, 0
	}
	c.SyncFields()
	c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime.Add(-time.Duration(elapsed * float64(time.Second)))}
	w.Crafts[w.ActiveCraftIdx] = c
	return w
}

// slsAtT42 is the ADR's decision 6 state: tanks 758,608 / 891,420 kg,
// mass 1,961,803 kg, core at 100%.
func slsAtT42(t *testing.T) *sim.World {
	t.Helper()
	c := spacecraft.NewFromLoadout("SLS-Block1")
	c.Stages[0].FuelMass, c.Stages[1].FuelMass = 758608, 891420
	w := boosterWorld(t, c, 42)
	if m := c.TotalMass(); m < 1961802 || m > 1961804 {
		t.Fatalf("fixture mass = %.0f, want 1,961,803", m)
	}
	return w
}

// fhAtT84 is the ADR's hypothetical Falcon Heavy (no FH part exists):
// a liquid booster pair (two f9-s1 as one marked stage), a core f9-s1,
// an f9-s2, and a Dragon with trunk. T+84 with the core at 60%, the
// boosters pinned at 100%.
func fhAtT84(t *testing.T, target spacecraft.ThrottleTarget) *sim.World {
	t.Helper()
	flow := 7.607e6 / (282 * testG0) // one f9-s1 at 100%, 2750.7 kg/s
	one := 1.0
	c := spacecraft.NewFromStages([]spacecraft.Stage{
		{Name: "F9-S1 x2", DryMass: 51200, FuelMass: 822000 - 2*flow*84, FuelCapacity: 822000, Thrust: 15.214e6, Isp: 282, FuelType: spacecraft.FuelTypeKerolox, LitWithAbove: true},
		{Name: "F9-S1", DryMass: 25600, FuelMass: 411000 - 0.6*flow*84, FuelCapacity: 411000, Thrust: 7.607e6, Isp: 282, FuelType: spacecraft.FuelTypeKerolox},
		{Name: "F9-S2", DryMass: 3900, FuelMass: 107500, FuelCapacity: 107500, Thrust: 934e3, Isp: 348, FuelType: spacecraft.FuelTypeKerolox},
		{Name: "Dragon", DryMass: 12862},
	})
	c.Throttle, c.BoosterThrottle, c.ThrottleTarget = 0.6, &one, target
	w := boosterWorld(t, c, 84)
	if m := c.TotalMass(); m < 833300 || m > 833320 {
		t.Fatalf("fixture mass = %.0f, want 833,309", m)
	}
	return w
}

func assertFits(t *testing.T, name string, lines []string) {
	t.Helper()
	for _, l := range lines {
		if got := lipgloss.Width(l); got > tierTopLeftWidth-2 {
			t.Errorf("%s row is %d cells, pin is %d: %q", name, got, tierTopLeftWidth-2, l)
		}
	}
}

func TestEngineBoxSLSAtT42(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := slsAtT42(t)
	lines := v.buildEngineBox(w)
	if want := "ENGINE  ● BURN · boosters T-1m02s"; lines[0] != want {
		t.Errorf("title = %q, want %q", lines[0], want)
	}
	if !strings.HasPrefix(strings.TrimLeft(lines[1], " "), "throttle: 100% ● FIRING T+42s") {
		t.Errorf("throttle row = %q", lines[1])
	}
	if want := "  TWR:      2.14"; !strings.HasPrefix(lines[2], "  TWR:") || strings.TrimRight(lines[2], " ") != want {
		t.Errorf("TWR row = %q, want %q", lines[2], want)
	}
	assertFits(t, "SLS T+42", lines)

	// x with the target on all: the core is cut, the solids keep burning.
	w.ActiveCraft().Throttle = 0
	lines = v.buildEngineBox(w)
	if want := "ENGINE  ● BURN · boosters T-1m02s"; lines[0] != want {
		t.Errorf("core cut: title = %q, want %q", lines[0], want)
	}
	if !strings.Contains(lines[1], "0% ● FIRING T+42s") {
		t.Errorf("core cut: throttle row = %q", lines[1])
	}
	if want := "  TWR:      1.66 (max 2.14)"; strings.TrimRight(lines[2], " ") != want {
		t.Errorf("core cut: TWR row = %q, want %q", lines[2], want)
	}
	assertFits(t, "SLS T+42 core cut", lines)
}

func TestEngineBoxSLSBoostersDry(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := slsAtT42(t)
	w.ActiveCraft().Stages[0].FuelMass = 0
	w.ActiveCraft().SyncFields()
	if got, want := v.buildEngineBox(w)[0], "ENGINE  ● BURN · boosters DRY"; got != want {
		t.Errorf("title = %q, want %q", got, want)
	}
}

func TestEngineBoxNoBadgeBeforeIgnitionOrAfterTheDrop(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := slsAtT42(t)
	w.ActiveCraft().ManualBurn = nil
	if got := v.buildEngineBox(w)[0]; strings.Contains(got, "boosters") {
		t.Errorf("unlit title = %q, want no booster badge", got)
	}
	w = slsAtT42(t)
	w.ActiveCraft().Stages = w.ActiveCraft().Stages[1:] // the drop
	w.ActiveCraft().SyncFields()
	if got := v.buildEngineBox(w)[0]; strings.Contains(got, "boosters") {
		t.Errorf("post-drop title = %q, want no booster badge", got)
	}
}

func TestEngineBoxFalconHeavyAtT84(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())

	w := fhAtT84(t, spacecraft.ThrottleCore)
	lines := v.buildEngineBox(w)
	if want := "ENGINE  ● BURN · boosters 100% T-1m05s"; lines[0] != want {
		t.Errorf("title = %q, want %q", lines[0], want)
	}
	if !strings.HasPrefix(lines[1], "  core:     60% ● FIRING T+1m24s") {
		t.Errorf("throttle row = %q, want label core: and 60%% ● FIRING T+1m24s", lines[1])
	}
	if want := "  TWR:      2.42 (max 2.79)"; strings.TrimRight(lines[2], " ") != want {
		t.Errorf("TWR row = %q, want %q", lines[2], want)
	}
	assertFits(t, "FH T+84 core", lines)

	// x on the core: 1.86.
	w.ActiveCraft().Throttle = 0
	lines = v.buildEngineBox(w)
	if !strings.HasPrefix(lines[1], "  core:     0% ● FIRING T+1m24s") {
		t.Errorf("core cut: throttle row = %q", lines[1])
	}
	if want := "  TWR:      1.86 (max 2.79)"; strings.TrimRight(lines[2], " ") != want {
		t.Errorf("core cut: TWR row = %q, want %q", lines[2], want)
	}
	assertFits(t, "FH T+84 core cut", lines)

	// target boosters: the badge shows the core, the row is the boosters.
	w = fhAtT84(t, spacecraft.ThrottleBoosters)
	lines = v.buildEngineBox(w)
	if want := "ENGINE  ● BURN · core 60%  T-2m45s"; lines[0] != want {
		t.Errorf("boosters target: title = %q, want %q", lines[0], want)
	}
	if !strings.HasPrefix(lines[1], "  boosters: 100% ● FIRING T+1m24s") {
		t.Errorf("boosters target: throttle row = %q", lines[1])
	}
	assertFits(t, "FH T+84 boosters", lines)

	// target all: the unchanged label.
	w = fhAtT84(t, spacecraft.ThrottleAll)
	lines = v.buildEngineBox(w)
	if !strings.HasPrefix(lines[1], "  throttle: 60% ● FIRING T+1m24s") {
		t.Errorf("all target: throttle row = %q", lines[1])
	}
	if want := "ENGINE  ● BURN · boosters 100% T-1m05s"; lines[0] != want {
		t.Errorf("all target: title = %q, want %q", lines[0], want)
	}
}

// The stale target on a vessel with no liquid booster (SLS: solids) must
// not rename the row.
func TestEngineBoxStaleTargetOnSolidBoostersKeepsThrottleLabel(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := slsAtT42(t)
	w.ActiveCraft().ThrottleTarget = spacecraft.ThrottleBoosters
	if row := v.buildEngineBox(w)[1]; !strings.HasPrefix(row, "  throttle:") {
		t.Errorf("row = %q, want throttle:", row)
	}
}

func TestPropellantFuelPairBottomFirst(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := slsAtT42(t)
	fuel := func() string { return v.buildPropellantBox(w)[1] }
	if row := fuel(); !strings.HasPrefix(row, "  fuel:     60% · 91%") {
		t.Errorf("T+42 fuel row = %q, want 60%% · 91%%", row)
	}
	w.ActiveCraft().Stages[0].FuelMass = 0
	w.ActiveCraft().Stages[1].FuelMass = 760000 // 77.6%
	w.ActiveCraft().SyncFields()
	if row := fuel(); !strings.HasPrefix(row, "  fuel:     0% · 78%") {
		t.Errorf("after burnout fuel row = %q, want 0%% · 78%%", row)
	}
	w.ActiveCraft().Stages = w.ActiveCraft().Stages[1:]
	w.ActiveCraft().SyncFields()
	if row := fuel(); !strings.HasPrefix(row, "  fuel:     78%") || strings.Contains(row, "·") {
		t.Errorf("after the drop fuel row = %q, want 78%% alone", row)
	}
	for _, r := range v.buildPropellantBox(w) {
		if got := lipgloss.Width(r); got > tierTopLeftWidth-2 {
			t.Errorf("PROPELLANT row %d cells: %q", got, r)
		}
	}
}

func TestPropellantFuelPairFitsItsCell(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := fhAtT84(t, spacecraft.ThrottleAll)
	w.ActiveCraft().Stages[0].FuelMass = 822000
	w.ActiveCraft().Stages[1].FuelMass = 411000
	w.ActiveCraft().SyncFields()
	row := v.buildPropellantBox(w)[1]
	if !strings.HasPrefix(row, "  fuel:     100% · 100%") {
		t.Errorf("row = %q", row)
	}
	// the widest pair is 11 cells; mass: must still sit in its column
	if !strings.Contains(row, "  mass:") {
		t.Errorf("mass: label lost: %q", row)
	}
}

func TestStagePipsPaintTheWholeLitSet(t *testing.T) {
	orig := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(orig) })

	v := NewOrbitView(launchThemeForTest())
	w := slsAtT42(t)
	got := v.stagePips(w.ActiveCraft())
	if plain := ansi.Strip(got); plain != "◉◉●" {
		t.Errorf("pips = %q, want ◉◉●", plain)
	}
	// Instrument check: the lit glyph is styled (an escape appears) and both
	// lit pips carry the same flame colour.
	if n := strings.Count(got, "\x1b["); n == 0 {
		t.Fatalf("setup: forced TrueColor but pips carry no escape: %q", got)
	}
	flame := v.stagePips(spacecraft.NewFromStages([]spacecraft.Stage{{FuelMass: 1, FuelCapacity: 1, Thrust: 1}}))
	if !strings.HasPrefix(got, flame+flame) {
		t.Errorf("first two pips %q, want the lit style twice (%q)", got, flame)
	}

	w.ActiveCraft().Stages[0].FuelMass = 0
	if plain := ansi.Strip(v.stagePips(w.ActiveCraft())); plain != "○◉●" {
		t.Errorf("dry boosters: pips = %q, want ○◉●", plain)
	}
	line := strings.Join(v.buildStagesBox(w), "")
	if !strings.Contains(ansi.Strip(line), "STAGES  ○◉●  ▸ SRBs (1/3) [space]") {
		t.Errorf("STAGES line = %q", ansi.Strip(line))
	}
	_ = fmt.Sprint
}

// ADR 0058 review finding 3: Proximity's throttle row names and reads the
// throttle target, like ENGINE, since the throttle keys act on it there too.
func TestProximityThrottleRowFollowsTheTarget(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	for _, tc := range []struct {
		target spacecraft.ThrottleTarget
		prefix string
	}{
		{spacecraft.ThrottleBoosters, "  boosters:  100% ● FIRING T+1m24s"},
		{spacecraft.ThrottleCore, "  core:      60% ● FIRING T+1m24s"},
		{spacecraft.ThrottleAll, "  throttle:  60% ● FIRING T+1m24s"},
	} {
		w := fhAtT84(t, tc.target)
		if got := v.proximityEngineRow(w); !strings.HasPrefix(got, tc.prefix) {
			t.Errorf("target %v: row = %q, want prefix %q", tc.target, got, tc.prefix)
		}
	}
}
