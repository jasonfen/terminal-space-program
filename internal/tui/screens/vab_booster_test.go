package screens

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0058 decision 9, guards [10] and the liquid-booster build. Hand-worked
// numbers are in work/2026-10-22-sls-parallel-staging-grill/slice-S6.md.

func openSLS(t *testing.T) *VAB {
	t.Helper()
	v := subVAB(t)
	v.HandleKey("o")
	moveLoadTo(t, v, "SLS-Block1")
	v.HandleKey("enter")
	if len(v.stages) != 3 {
		t.Fatalf("SLS opened with %d stages, want 3", len(v.stages))
	}
	return v
}

// pressB puts the cursor on stage i and presses b.
func pressB(v *VAB, i int) {
	v.focusStageInStack(i)
	v.flash = ""
	v.HandleKey("b")
}

func header(v *VAB, i int) string {
	return v.stageHeaderLine(i, v.Stats(), false, false, 120)
}

// An opened SLS carries the catalog mark: tag, Σ and TWR follow it.
func TestVABOpenedSLSShowsTheMark(t *testing.T) {
	v := openSLS(t)
	if !v.stages[0].litWithAbove || v.stages[1].litWithAbove {
		t.Fatalf("marks = %v %v, want S1 only", v.stages[0].litWithAbove, v.stages[1].litWithAbove)
	}
	if h := header(v, 0); !strings.Contains(h, " · lights with S2") {
		t.Errorf("S1 header lacks the tag: %q", h)
	}
	if h := header(v, 1); strings.Contains(h, "lights with") {
		t.Errorf("S2 header carries a tag: %q", h)
	}
	s := v.Stats()
	// Hand-worked (slice-S6.md): 2518.2 / 9040.6 / 9501.5, Σ 21060.2, TWR 1.64.
	for i, want := range []float64{2518, 9041, 9502} {
		if math.Abs(s.StageDV[i]-want) > 1.5 {
			t.Errorf("S%d Δv = %.1f, want %.0f", i+1, s.StageDV[i], want)
		}
	}
	if math.Abs(s.TotalDV-21060.2) > 1 {
		t.Errorf("Σ Δv = %.1f, want 21060.2 ±1 (ADR 21,061)", s.TotalDV)
	}
	if math.Abs(s.LiftoffTWR-1.64) > 0.01 {
		t.Errorf("TWR = %.3f, want 1.64 ±0.01", s.LiftoffTWR)
	}
}

// b toggles: off puts the serial numbers back (positive control for the
// stats following the mark), on restores them.
func TestVABBTogglesTheMarkAndTheStatsFollow(t *testing.T) {
	v := openSLS(t)
	pressB(v, 0)
	if v.stages[0].litWithAbove {
		t.Fatal("b on a marked stage did not clear it")
	}
	s := v.Stats()
	if math.Abs(s.TotalDV-21331) > 1.5 || math.Abs(s.LiftoffTWR-1.27) > 0.01 {
		t.Errorf("serial SLS: Σ %.1f TWR %.3f, want 21331 / 1.27", s.TotalDV, s.LiftoffTWR)
	}
	if strings.Contains(header(v, 0), "lights with") {
		t.Error("tag still shown after clearing")
	}
	pressB(v, 0)
	if !v.stages[0].litWithAbove || math.Abs(v.Stats().TotalDV-21060.2) > 1 {
		t.Errorf("re-marking did not restore the pair: Σ %.1f", v.Stats().TotalDV)
	}
}

func TestVABBRefusals(t *testing.T) {
	// top stage: nothing above.
	v := openSLS(t)
	pressB(v, 2)
	if v.stages[2].litWithAbove || v.flash != "nothing above" {
		t.Errorf("top stage: marked=%v flash=%q", v.stages[2].litWithAbove, v.flash)
	}
	// lower neighbour already marked: one booster group.
	pressB(v, 1)
	if v.stages[1].litWithAbove || v.flash != "one booster group" {
		t.Errorf("double mark: marked=%v flash=%q", v.stages[1].litWithAbove, v.flash)
	}
	// c-fused stage: boosters drop alone.
	v = openSLS(t)
	pressB(v, 0) // clear
	v.focusStageInStack(1)
	v.HandleKey("c") // S2 drops with S1
	pressB(v, 0)
	if v.stages[0].litWithAbove || v.flash != "boosters drop alone" {
		t.Errorf("b on a c-grouped stage: marked=%v flash=%q", v.stages[0].litWithAbove, v.flash)
	}
	pressB(v, 1)
	if v.stages[1].litWithAbove || v.flash != "boosters drop alone" {
		t.Errorf("b on a c-fused stage: marked=%v flash=%q", v.stages[1].litWithAbove, v.flash)
	}
	// and the other way round: c on a marked stage (S2 marked under the ICPS).
	v = openSLS(t)
	pressB(v, 0) // clear S1
	pressB(v, 1) // mark S2
	v.focusStageInStack(1)
	v.flash = ""
	v.HandleKey("c")
	if v.stages[1].decoupleFused || v.flash != "boosters drop alone" {
		t.Errorf("c on a marked stage: fused=%v flash=%q", v.stages[1].decoupleFused, v.flash)
	}
	v = openSLS(t)
	v.focusStageInStack(1)
	v.flash = ""
	v.HandleKey("c") // fuses S2 down onto the marked S1
	if v.stages[1].decoupleFused || v.flash != "boosters drop alone" {
		t.Errorf("c above a marked stage: fused=%v flash=%q", v.stages[1].decoupleFused, v.flash)
	}
}

func TestVABKeysLineAndFooterFit(t *testing.T) {
	v := openSLS(t)
	out := v.Render(140, 40)
	if !strings.Contains(out, "[b] lights with stage above") {
		t.Errorf("KEYS lacks the b hint:\n%s", out)
	}
	if !strings.Contains(out, " · lights with S2") {
		t.Errorf("vehicle column lacks the tag:\n%s", out)
	}
}

// The mark rides the design file (save then open) and a subassembly.
func TestVABMarkRoundTripsThroughDesignAndSubassembly(t *testing.T) {
	v := openSLS(t)
	v.HandleKey("s")
	typeText(v, "Mark Test")
	v.HandleKey("enter")
	ds, _ := spacecraft.ListDesigns()
	var saved *spacecraft.Design
	for i := range ds {
		if ds[i].Name() == "Mark Test" {
			saved = &ds[i]
		}
	}
	if saved == nil {
		t.Fatal("design not saved")
	}
	if p := saved.Loadout.Parts; !p[0].LitWithAbove || p[1].LitWithAbove || p[2].LitWithAbove {
		t.Fatalf("saved refs lit_with_above = %v %v %v, want true false false", p[0].LitWithAbove, p[1].LitWithAbove, p[2].LitWithAbove)
	}
	w := NewVAB(Theme{})
	w.Reset(testVABComps())
	w.loadDesign(*saved)
	if !w.stages[0].litWithAbove || w.stages[1].litWithAbove {
		t.Errorf("reopened marks = %v %v", w.stages[0].litWithAbove, w.stages[1].litWithAbove)
	}
	// and it resolves to a flyable pair: the spawned vessel has the mark.
	l, _ := saved.Resolve()
	if !l.Stages[0].LitWithAbove {
		t.Error("resolved design stage 0 not marked")
	}
	// Subassembly: S from the bottom carries the mark; adding it lands marked.
	v.focusStageInStack(0)
	v.HandleKey("S")
	typeText(v, "Boost Pair")
	v.HandleKey("enter")
	sub := spacecraft.Subassembly{}
	subs, _ := spacecraft.ListSubassemblies()
	for _, s := range subs {
		if s.Name() == "Boost Pair" {
			sub = s
		}
	}
	if sub.Name() != "Boost Pair" {
		t.Fatalf("subassembly not saved (mode %v flash %q)", v.mode, v.flash)
	}
	blk := subassemblyStages(sub)
	if !blk[0].litWithAbove {
		t.Errorf("subassembly block lost the mark: %+v", blk[0])
	}
}

// Duplicating a marked stage must not make a double mark; cracking keeps it.
func TestVABDuplicateKeepsOneMark(t *testing.T) {
	v := openSLS(t)
	v.focusStageInStack(0)
	v.HandleKey("y")
	if !v.stages[0].litWithAbove || v.stages[1].litWithAbove {
		t.Errorf("after copy marks = %v %v, want the original only", v.stages[0].litWithAbove, v.stages[1].litWithAbove)
	}
}

// The first liquid boosters in the game: two liquid stages built in the VAB,
// the lower marked, saved, spawned, lit, and the throttle target walked.
func TestVABBuildsALiquidBoosterVesselAndItFlies(t *testing.T) {
	v := NewVAB(Theme{})
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	v.Reset(spacecraft.Components)
	build := func(parts ...string) {
		v.newStage()
		for _, p := range parts {
			v.addComponentToCurrent(p)
		}
	}
	v.stages = nil
	build("rd180-booster", "kero-tank-100k") // S1 boosters
	build("rd180-booster", "kero-tank-100k") // S2 core
	build("crew-capsule")                    // S3 payload
	pressB(v, 0)
	if !v.stages[0].litWithAbove || !strings.Contains(header(v, 0), " · lights with S2") {
		t.Fatalf("mark not set / tagged: %q flash %q", header(v, 0), v.flash)
	}
	v.HandleKey("s")
	typeText(v, "Twin Stack")
	v.HandleKey("enter")

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{DesignID: "twin-stack", ParentBodyID: w.ActiveCraft().Primary.ID, AltitudeM: 200_000})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if set := spacecraft.LitStages(c.Stages); len(set) != 2 {
		t.Fatalf("lit set = %v, want two stages", set)
	}
	if c.Stages[0].FuelType == spacecraft.FuelTypeSolid {
		t.Fatal("booster is solid: not a liquid-booster vessel")
	}
	w.ActiveCraftIdx = len(w.Crafts) - 1
	c.Throttle = 1
	c.EngineMode = spacecraft.EngineMain
	w.StartManualBurn()
	if c.ManualBurn == nil {
		t.Fatal("burn did not light")
	}
	// c: all -> core -> boosters -> all; each group's throttle moves alone.
	next := func(want spacecraft.ThrottleTarget) {
		t.Helper()
		if got, ok := w.CycleThrottleTarget(); !ok || got != want {
			t.Fatalf("c -> %v ok=%v, want %v", got, ok, want)
		}
	}
	next(spacecraft.ThrottleCore)
	w.SetThrottle(0.6)
	if c.Throttle != 0.6 || c.BoosterThrottleEff() != 1 {
		t.Errorf("core 60%%: core=%v boosters=%v, want 0.6 / 1", c.Throttle, c.BoosterThrottleEff())
	}
	next(spacecraft.ThrottleBoosters)
	w.SetThrottle(0.3)
	if c.Throttle != 0.6 || math.Abs(c.BoosterThrottleEff()-0.3) > 1e-9 {
		t.Errorf("boosters 30%%: core=%v boosters=%v, want 0.6 / 0.3", c.Throttle, c.BoosterThrottleEff())
	}
	// Per-tank debit follows each group's own throttle.
	f0, f1 := c.Stages[0].FuelMass, c.Stages[1].FuelMass
	t0 := w.Clock.SimTime
	for i := 0; i < 20; i++ {
		w.Tick()
	}
	dt := w.Clock.SimTime.Sub(t0).Seconds()
	d0, d1 := f0-c.Stages[0].FuelMass, f1-c.Stages[1].FuelMass
	r0 := 3.83e6 / (311 * 9.80665) // one engine at 100%
	if dt <= 0 || math.Abs(d0/(0.3*r0*dt)-1) > 0.02 || math.Abs(d1/(0.6*r0*dt)-1) > 0.02 {
		t.Errorf("%.2f s debit: boosters %.0f (want %.0f) core %.0f (want %.0f)", dt, d0, 0.3*r0*dt, d1, 0.6*r0*dt)
	}
	next(spacecraft.ThrottleAll)
}

// ADR 0058 decision 14 / guard [13]: the VAB's b is on F1's VAB page and in
// docs/controls.md, and the c row says the two marks exclude each other.
func TestVABBHelpAndControlsRows(t *testing.T) {
	var rows [][2]string
	for _, s := range helpSections {
		if s.header == "VEHICLE ASSEMBLY (VAB)" {
			rows = s.rows
		}
	}
	found := false
	for _, r := range rows {
		if r[0] == "b" && strings.Contains(r[1], "lights with stage above") {
			found = true
		}
	}
	if !found {
		t.Errorf("F1's VAB page has no b row naming %q", "lights with stage above")
	}
	doc, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `b` | **Lights with stage above**", "boosters drop alone"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md VAB section lacks %q", want)
		}
	}
}

func marks(v *VAB) []bool {
	out := make([]bool, len(v.stages))
	for i, s := range v.stages {
		out[i] = s.litWithAbove
	}
	return out
}

func sameBools(a, b []bool) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Review fix: a mark with nothing above it, or directly above another mark,
// is undefined flight; reorder, remove and open drop it (silently).
func TestVABReorderThatPutsTheMarkOnTopDropsIt(t *testing.T) {
	v := openSLS(t)
	v.focusStageInStack(0)
	v.HandleKey("]")
	if !sameBools(marks(v), []bool{false, true, false}) {
		t.Fatalf("after one move up marks = %v, want the mark to ride the stage", marks(v))
	}
	v.HandleKey("]")
	if !sameBools(marks(v), []bool{false, false, false}) {
		t.Errorf("marked stage on top kept its mark: %v", marks(v))
	}
}

func TestVABRemoveThatMakesTwoMarksAdjacentDropsTheUpperOne(t *testing.T) {
	v := subVAB(t)
	stackOf(v, 0, "srb", "core-rs25", "srb", "icps-sls", "icps-sls")
	v.stages[0].litWithAbove, v.stages[2].litWithAbove = true, true
	v.focusStageInStack(1)
	v.HandleKey("x")
	if len(v.stages) != 4 || !sameBools(marks(v), []bool{true, false, false, false}) {
		t.Errorf("after removing S2: %d stages, marks %v, want 4 and the lower mark only", len(v.stages), marks(v))
	}
	// removing the stage above the only mark leaves it on top: dropped.
	v = openSLS(t)
	v.focusStageInStack(1)
	v.HandleKey("x")
	v.focusStageInStack(1)
	v.HandleKey("x")
	if len(v.stages) != 1 || v.stages[0].litWithAbove {
		t.Errorf("lone stage kept its mark: %v", marks(v))
	}
}

func TestVABOpeningADesignWithStrayMarksDropsThem(t *testing.T) {
	d := spacecraft.Design{Loadout: spacecraft.LoadoutDef{ID: "stray", Name: "stray", Parts: []spacecraft.PartRef{
		{PartID: "srb", LitWithAbove: true},
		{PartID: "core-rs25", LitWithAbove: true},
		{PartID: "icps-sls", LitWithAbove: true},
	}}}
	v := subVAB(t)
	v.loadDesign(d)
	if !sameBools(marks(v), []bool{true, false, false}) {
		t.Errorf("opened marks = %v, want the lowest only", marks(v))
	}
	if b := designStages(d, "sub"); !(b[0].litWithAbove && !b[1].litWithAbove && !b[2].litWithAbove) {
		t.Errorf("subassembly block marks = %v %v %v", b[0].litWithAbove, b[1].litWithAbove, b[2].litWithAbove)
	}
}
