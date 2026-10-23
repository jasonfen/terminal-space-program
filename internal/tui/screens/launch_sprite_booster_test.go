package screens

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/widgets"
)

var boosterTestBasis = widgets.Basis{X: orbital.Vec3{X: 1}, Y: orbital.Vec3{Z: 1}}

// micro rounds an offset to 1e-6 m. Not bit-exact on purpose: the Go compiler
// fuses multiply-adds per architecture (arm64 yes, amd64 no) and per
// surrounding code, so raw bits move by an ulp with no change to what is drawn.
func micro(x float64) int64 { return int64(math.Round(x * 1e6)) }

// spriteDigest hashes every pixel the launch view paints for a stack (body,
// bell, legs and the core flame at two frames and two attitudes), to a
// micrometre, so "serial sprites stay identical" is a measurement.
func spriteDigest(stages []spacecraft.Stage) string {
	h := sha256.New()
	for _, cmd := range []orbital.Vec3{{Z: 1}, {X: 1, Z: 3}} {
		var all []SpritePixel
		all = append(all, ComposeLaunchSprite(stages, cmd, boosterTestBasis, 1.5)...)
		all = append(all, ComposeEngineBell(stages, cmd, boosterTestBasis, 1.5)...)
		all = append(all, ComposeLegs(stages, cmd, boosterTestBasis, 1.5)...)
		for f := 0; f < 2; f++ {
			all = append(all, ComposeFlame(stages, cmd, boosterTestBasis, 1.5, 1, f, EngineBellWidth(stages))...)
		}
		for _, p := range all {
			fmt.Fprintf(h, "%d %d %d %s;", micro(p.OffsetWorld.X), micro(p.OffsetWorld.Y), micro(p.OffsetWorld.Z), p.Color)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func unmarkedLoadoutIDs() []string {
	var ids []string
	for id, l := range spacecraft.Loadouts {
		marked := false
		for _, s := range l.Stages {
			marked = marked || s.LitWithAbove
		}
		if !marked {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids
}

// Golden digests (micrometre-rounded) captured on main 57405da, before S7 touched the layout.
const (
	goldenSaturnV   = "e3388b974dc8bb1c"
	goldenAllSerial = "c7509d1027d4a6e8"
)

func TestSerialSpritesByteIdentical(t *testing.T) {
	if got := spriteDigest(spacecraft.Loadouts["Saturn-V"].Stages); got != goldenSaturnV {
		t.Errorf("Saturn V sprite digest = %s, want %s", got, goldenSaturnV)
	}
	h := sha256.New()
	for _, id := range unmarkedLoadoutIDs() {
		fmt.Fprint(h, id, spriteDigest(spacecraft.Loadouts[id].Stages))
	}
	if got := hex.EncodeToString(h.Sum(nil))[:16]; got != goldenAllSerial {
		t.Errorf("all %d serial loadouts digest = %s, want %s", len(unmarkedLoadoutIDs()), got, goldenAllSerial)
	}
}

func slsStages() []spacecraft.Stage { return spacecraft.Loadouts["SLS-Block1"].Stages }

func bodyRects(rs []stackRect, kind stackRectKind) (out []stackRect) {
	for _, r := range rs {
		if r.kind == kind {
			out = append(out, r)
		}
	}
	return
}

// Hand-worked, SLS Block 1 (parts.json): srb 28 rows x 5 wide (marked),
// core-rs25 24 x 4, icps-sls 8 x 3 (floored to 4 by minRenderedStageWidthPx).
//
//	column width  = max(1, 5/2)                  = 2
//	offset        = core 4/2 + column 2/2 + 1    = 4   (columns at 3.5 and 4.5)
//	srb           rowBase 0, 28 rows, both sides (base level with the core's)
//	core          rowBase 0, 24 rows, width 4
//	core->icps    taper at row 24 (24>=6 and 8>=6), separator at row 25
//	icps          rowBase 26, 8 rows
//	height        26 + 8 = 34 (the sum of rect rows would say 28+24+1+1+8 = 62)
func TestLaunchStackLayoutSLSHasTwoSideColumns(t *testing.T) {
	rs := launchStackLayout(slsStages())
	sides := bodyRects(rs, rectSide)
	if len(sides) != 2 {
		t.Fatalf("side rects = %d, want 2: %+v", len(sides), rs)
	}
	got := map[float64]bool{}
	for _, r := range sides {
		if r.stage != 0 || r.width != 2 || r.rowBase != 0 || r.rows != 28 {
			t.Errorf("side rect %+v, want stage 0, width 2, rowBase 0, rows 28", r)
		}
		got[r.xOffset] = true
	}
	if !got[-4] || !got[4] {
		t.Errorf("side offsets %v, want -4 and +4", got)
	}
	for _, r := range rs {
		if r.stage == 0 && r.kind != rectSide {
			t.Errorf("stage 0 still paints a %v rect %+v (no bottom block, no taper, no separator)", r.kind, r)
		}
	}
	core := bodyRects(rs, rectBody)
	if len(core) != 2 || core[0].stage != 1 || core[0].rowBase != 0 || core[0].rows != 24 || core[0].width != 4 {
		t.Errorf("core body = %+v, want stage 1 at rowBase 0, 24 rows, width 4 first", core)
	}
	if len(core) == 2 && core[1].rowBase != 26 {
		t.Errorf("icps rowBase = %d, want 26", core[1].rowBase)
	}
	if h := composedStackRows(slsStages()); h != 34 {
		t.Errorf("composedStackRows = %d, want 34", h)
	}
	if w := composedStackMaxWidth(slsStages()); w != 10 {
		t.Errorf("composedStackMaxWidth = %d, want 10 (2*4 offset + 2 wide)", w)
	}
}

func TestComposeLaunchSpriteSLSPixelsBesideCore(t *testing.T) {
	st := slsStages()
	boosterColor := stageSpriteColor(st[0])
	var n int
	xs := map[float64]bool{}
	minY := math.MaxFloat64
	for _, p := range ComposeLaunchSprite(st, orbital.Vec3{Z: 1}, boosterTestBasis, 1) {
		if p.Color != boosterColor {
			continue
		}
		n++
		xs[p.OffsetWorld.Dot(boosterTestBasis.X)] = true
		minY = math.Min(minY, p.OffsetWorld.Dot(boosterTestBasis.Y))
	}
	if n != 2*2*28 {
		t.Errorf("booster-coloured pixels = %d, want %d (2 columns x 2 wide x 28 rows)", n, 2*2*28)
	}
	if len(xs) != 4 || !xs[-4.5] || !xs[-3.5] || !xs[3.5] || !xs[4.5] {
		t.Errorf("booster pixel columns = %v, want -4.5 -3.5 3.5 4.5", xs)
	}
	if minY != 0 {
		t.Errorf("booster base at y=%v, want 0 (level with the core's base)", minY)
	}
}

// A lone marked stage (the core is gone) is just a stage: one column, full
// width, no side rects.
func TestLoneMarkedStageIsOneColumn(t *testing.T) {
	s := slsStages()[0]
	rs := launchStackLayout([]spacecraft.Stage{s})
	if len(rs) != 1 || rs[0].kind != rectBody || rs[0].width != 5 || rs[0].xOffset != 0 {
		t.Errorf("lone marked stage layout = %+v, want one centred body of width 5", rs)
	}
}

func TestBoosterFlamesFollowTheirGroup(t *testing.T) {
	st := slsStages()
	cmd := orbital.Vec3{Z: 1}
	b := boosterTestBasis
	full := ComposeBoosterFlames(st, cmd, b, 1, 1, 0)
	if len(full) == 0 {
		t.Fatal("lit solids paint no flame")
	}
	xs := map[float64]bool{}
	for _, p := range full {
		if p.Color != flameColorForFuelType(spacecraft.FuelTypeSolid) {
			t.Errorf("booster flame colour %q, want the solid colour", p.Color)
		}
		xs[math.Round(p.OffsetWorld.Dot(b.X)*2)/2] = true
		if p.OffsetWorld.Dot(b.Y) >= 0 {
			t.Errorf("flame pixel above the base: %v", p.OffsetWorld)
		}
	}
	left, right := 0, 0
	for x := range xs {
		if x < 0 {
			left++
		} else {
			right++
		}
	}
	if left == 0 || right == 0 {
		t.Errorf("flame columns %v: want one flame under each side column", xs)
	}
	if got := len(ComposeBoosterFlames(st, cmd, b, 1, 0, 0)); got != 0 {
		t.Errorf("throttle 0 painted %d flame pixels", got)
	}
	if lo, hi := len(ComposeBoosterFlames(st, cmd, b, 1, 0.2, 0)), len(full); lo >= hi {
		t.Errorf("low throttle flame (%d px) not shorter than full (%d px)", lo, hi)
	}
	if got := ComposeBoosterFlames(st[1:], cmd, b, 1, 1, 0); got != nil {
		t.Errorf("unmarked stack painted %d booster flame pixels", len(got))
	}
}

// The core's bell and flame sit under the core, not under the booster stage.
func TestCoreFlameCentredOnCoreWidth(t *testing.T) {
	st := slsStages()
	core := coreStages(st)
	if len(core) != 2 || core[0].Name != st[1].Name {
		t.Fatalf("coreStages(SLS) starts at %q (len %d), want the core", core[0].Name, len(core))
	}
	if got := coreStages(st[1:]); len(got) != 2 {
		t.Errorf("coreStages of an unmarked stack changed it: len %d", len(got))
	}
	if EngineBellWidth(core) != 6 {
		t.Errorf("core bell width = %d, want 4+2", EngineBellWidth(core))
	}
}

// slsOnPadBurning spawns the SLS on the pad, ignites it, and renders the real
// chase-cam, so the flames are read back off the canvas the player sees.
func slsOnPadBurning(t *testing.T) (*sim.World, *spacecraft.Spacecraft, *LaunchView) {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c, err := w.SpawnCraft(sim.SpawnSpec{LoadoutID: spacecraft.LoadoutSLSBlock1ID, ParentBodyID: "earth", Launchpad: true, Latitude: 28.6083, LongitudeOffset: -80.604})
	if err != nil {
		t.Fatal(err)
	}
	c.Stages[len(c.Stages)-1].CommandSource = spacecraft.CommandCrewed
	c.SyncFields()
	w.StartManualBurn()
	for i := 0; i < 5; i++ {
		w.Tick()
	}
	th := launchThemeForTest()
	return w, c, NewLaunchView(th, NewOrbitView(th))
}

func TestLaunchViewPaintsBoosterFlamesFromTheirOwnGroup(t *testing.T) {
	w, c, v := slsOnPadBurning(t)
	if c.ManualBurn == nil {
		t.Fatal("setup: SLS is not burning")
	}
	solid := flameColorForFuelType(spacecraft.FuelTypeSolid)
	v.Render(w, 140, 40)
	lit := v.canvas.CountColor(solid)
	if lit == 0 {
		t.Fatalf("lit SLS paints no solid-coloured flame cells")
	}
	// Core throttled to zero: the solids keep burning at 1.0, so their flames stay.
	c.Throttle = 0
	v.Render(w, 140, 40)
	if got := v.canvas.CountColor(solid); got == 0 {
		t.Errorf("core at 0%% erased the booster flames (%d -> 0): they must follow their own group", lit)
	}
	// Boosters burned out, core burning: booster flames gone.
	c.Throttle = 1
	c.Stages[0].FuelMass = 0
	c.SyncFields()
	v.Render(w, 140, 40)
	if got := v.canvas.CountColor(solid); got != 0 {
		t.Errorf("burned-out boosters still paint %d flame cells", got)
	}
}
