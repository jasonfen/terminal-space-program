package sim

// One-line reason for a new file: recovery.go is a new production file (the pad's
// recover: choice, the drone ship, the flight home at the drop, the cutoff).

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// padClimb spawns loadout on the Cape pad with the given recovery picks and flies
// the harness ascent (vertical 8 s, a 0.015 rad kick, then surface prograde) until
// the bottom stage's burn ends: by its own cutoff (a held reserve) or dry. coreThrottle
// below 1 throttles the core (Falcon Heavy's boosters need it to have a window at all).
func padClimb(t *testing.T, loadout string, picks []RecoverPick, coreThrottle float64, tweak ...func(*spacecraft.Spacecraft)) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	return w, padClimbIn(t, w, loadout, picks, coreThrottle, tweak...)
}

// standOnLZ parks a landed vessel in LZ n of site id (a booster already home).
func standOnLZ(t *testing.T, w *World, id uint64, n int) {
	t.Helper()
	o, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast})
	if err != nil {
		t.Fatal(err)
	}
	s, _ := w.siteByID(id)
	lat, lon := s.lzLatLon(n, o.Primary.RadiusMeters())
	o.Landed, o.LandedLatDeg, o.LandedLonDeg = true, lat, lon
}

// padClimbIn is padClimb in a world that may already hold vessels.
func padClimbIn(t *testing.T, w *World, loadout string, picks []RecoverPick, coreThrottle float64, tweak ...func(*spacecraft.Spacecraft)) *spacecraft.Spacecraft {
	t.Helper()
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: loadout, ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast, Recover: picks})
	if err != nil {
		t.Fatal(err)
	}
	w.RecomputeCommGraph()
	for _, f := range tweak {
		f(c)
	}
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	if coreThrottle < 1 {
		writeThrottle(c, ThrottleCore, coreThrottle)
	}
	t0 := w.Clock.SimTime
	for i := 0; i < 60000 && c.Stages[0].HeldKg == 0 && c.ManualBurn != nil; i++ {
		switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
		case el > 8 && el < 14:
			c.PitchTrim = 0.015
		case el >= 14 && c.AttitudeMode == spacecraft.BurnRadialOut:
			c.PitchTrim, c.AttitudeMode = 0, spacecraft.BurnSurfacePrograde
		}
		w.Tick()
	}
	if c.Stages[0].HeldKg > 0 {
		w.Tick() // the tick after the cutoff ends the burn
	}
	return c
}

// flyDown ticks until every vessel in bs is down, or an hour of flight.
func flyDown(w *World, bs ...*spacecraft.Spacecraft) {
	for i := 0; i < 20*3600; i++ {
		done := true
		for _, b := range bs {
			done = done && (b.Landed || b.Crashed)
		}
		if done {
			return
		}
		w.Tick()
	}
}

// distToLZ is how far a landed vessel stands from LZ n of site id.
func distToLZ(t *testing.T, w *World, b *spacecraft.Spacecraft, id uint64, n int) float64 {
	t.Helper()
	s, ok := w.siteByID(id)
	if !ok {
		t.Fatalf("no site %d", id)
	}
	la, lo := s.lzLatLon(n, b.Primary.RadiusMeters())
	return HaversineM(b.LandedLatDeg, b.LandedLonDeg, la, lo, b.Primary.RadiusMeters())
}

// ADR 0069 decisions 12 and 15 end to end: a Falcon 9 + Dragon spawned with
// recovery LZ cuts its first stage off by itself with the reserve held, `space`
// drops it and gives the reserve back, the stage takes LC-13 as its own target
// without touching the core's, flies home unattended and lands in the LZ circle.
func TestRecoveryCutsOffAtTheReserveAndTheStageFliesHome(t *testing.T) {
	w, c := padClimb(t, "Falcon-9-Dragon", []RecoverPick{{Mode: spacecraft.RecoverLZ}}, 1)
	st := c.Stages[0]
	kept := st.HeldKg / st.FuelCapacity
	if st.HeldKg <= 0 || st.FuelMass != 0 || c.ManualBurn != nil || !c.DryOrder {
		t.Fatalf("no cutoff: held %.0f kg, fuel %.0f, burn %v, dry order %v", st.HeldKg, st.FuelMass, c.ManualBurn != nil, c.DryOrder)
	}
	if kept < 0.09 || kept > 0.14 { // measured 11.8 %: the reserve of an LC-13 return, ADR 0069 decision 15
		t.Errorf("cut off with %.1f %% of the tank held, want the 9 to 14 %% an LZ return needs", kept*100)
	}
	if c.Target != (spacecraft.Target{}) {
		t.Errorf("the core's target = %+v, want none: recovery must not use the t slot", c.Target)
	}
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	b := w.Crafts[n]
	if got := b.Stages[0].FuelMass; math.Abs(got-st.HeldKg) > 1 || b.Stages[0].HeldKg != 0 {
		t.Errorf("dropped stage holds %.0f kg in its tank (held %.0f), want the reserve back: %.0f", got, b.Stages[0].HeldKg, st.HeldKg)
	}
	if len(w.PendingRecoveryEvents) != 0 || len(b.Nodes) != 2 || !b.Landing.Owns() || b.Target.SiteID != lc13ID(t) || b.LZClaim.N != 1 {
		t.Fatalf("flying home: events %v nodes %d owns %v target %+v claim %+v, want the boostback and entry burn planted and LC-13 LZ-1 held", w.PendingRecoveryEvents, len(b.Nodes), b.Landing.Owns(), b.Target, b.LZClaim)
	}
	if c.Target != (spacecraft.Target{}) || w.Target != (spacecraft.Target{}) || w.ActiveCraft() != c {
		t.Errorf("the drop moved the pilot's vessel or target: active %v target %+v", w.ActiveCraft() == c, w.Target)
	}
	flyDown(w, b)
	d := distToLZ(t, w, b, lc13ID(t), 1)
	if !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() <= 0 {
		t.Errorf("landed=%v crashed=%v %.1f m from LZ-1 with %.0f kg, want inside the %v m circle with propellant", b.Landed, b.Crashed, d, b.ActiveStageFuel(), SiteLZRadiusM)
	}
}

// Recovery none burns to dry, as before: nothing held, no flight home, and the
// badge's DRY word stays (tui test). The stage still knows the LZ it was given:
// the override is the mode, not a missing site.
func TestRecoveryNoneBurnsToDry(t *testing.T) {
	w, c := padClimb(t, "Falcon-9", []RecoverPick{{Mode: spacecraft.RecoverLZ}}, 1, func(c *spacecraft.Spacecraft) {
		c.Stages[0].RecoverMode = spacecraft.RecoverNone
	})
	if st := c.Stages[0]; st.HeldKg != 0 || st.FuelMass != 0 {
		t.Fatalf("held %.0f kg, fuel %.0f, want a stage burned to dry", st.HeldKg, st.FuelMass)
	}
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	if b := w.Crafts[n]; len(b.Nodes) != 0 || b.Landing.Owns() || len(w.PendingRecoveryEvents) != 0 {
		t.Errorf("an unrecovered stage got nodes %d owns %v events %v", len(b.Nodes), b.Landing.Owns(), w.PendingRecoveryEvents)
	}
}

// Decision 12 on Falcon Heavy: the side boosters cut off by themselves (BECO) while
// the core burns on, `space` drops both, each takes its own LZ (LZ-1, LZ-2 of LC-13),
// and both land inside their circles at 1x and at the top warp. The core runs at 50 %
// until the boosters go: at full thrust the drop is at 2.4 km/s and no boostback closes.
func TestRecoveryFalconHeavyBoostersLandOnLZ1AndLZ2(t *testing.T) {
	for _, warp := range []int{0, len(WarpFactors) - 1} {
		w, c := padClimb(t, "Falcon-Heavy", []RecoverPick{{Mode: spacecraft.RecoverLZ}}, 0.5)
		if c.Stages[0].HeldKg <= 0 || c.ManualBurn == nil || c.Stages[1].FuelMass <= 0 {
			t.Fatalf("BECO: held %.0f, core burning %v, core fuel %.0f, want the boosters cut while the core burns on", c.Stages[0].HeldKg, c.ManualBurn != nil, c.Stages[1].FuelMass)
		}
		n := len(w.Crafts)
		if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
			t.Fatal(err)
		}
		bs := w.Crafts[n:]
		if len(bs) != 2 || len(w.PendingRecoveryEvents) != 0 || bs[0].LZClaim.N == bs[1].LZClaim.N || bs[0].LZClaim.N == 0 || bs[1].LZClaim.N == 0 {
			t.Fatalf("drop: %d vessels, events %v, claims %d and %d, want two boosters flying home to LZ-1 and LZ-2", len(bs), w.PendingRecoveryEvents, bs[0].LZClaim.N, bs[1].LZClaim.N)
		}
		lz := []int{bs[0].LZClaim.N, bs[1].LZClaim.N} // the claim is let go at touchdown
		w.Clock.WarpIdx = warp
		flyDown(w, bs...)
		for i, b := range bs {
			if d := distToLZ(t, w, b, lc13ID(t), lz[i]); !b.Landed || d > SiteLZRadiusM {
				t.Errorf("warp %d: %s landed=%v %.1f m from LZ-%d, want inside the %v m circle", warp, b.Name, b.Landed, d, lz[i], SiteLZRadiusM)
			}
		}
	}
}

// Decision 13: `ship` stations a site downrange of the pad, due east, with one LZ,
// named, in the target menu; a second ship is numbered. Flown, a stage recovered
// to a ship 250 km out lands in its circle.
func TestRecoveryShipIsAnLZSiteDownrange(t *testing.T) {
	w, c := padClimb(t, "Falcon-9-Dragon", []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: 250}}, 1)
	id := c.Stages[0].RecoverSiteID
	s, ok := w.siteByID(id)
	if !ok || c.Stages[0].RecoverMode != spacecraft.RecoverShip || s.Kind != SitePoint || s.Name != "Drone Ship" || s.LZs() != 1 {
		t.Fatalf("ship site = %+v (%v), want a one-LZ point named Drone Ship", s, ok)
	}
	if d := HaversineM(DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, s.LatDeg, s.LonDeg, c.Primary.RadiusMeters()); math.Abs(d-250e3) > 500 {
		t.Errorf("ship is %.1f km from the pad, want 250", d/1e3)
	}
	if cross := crossRangeM(w, c, s); math.Abs(cross) > 5e3 { // #998: the ship follows the ground track (104 deg for this due-east climb), not a fixed 090
		t.Errorf("ship stands %.0f m off the vessel's ground track at the cutoff, want within 5 km", cross)
	}
	rows, _ := w.TargetMenu()
	found := false
	for _, r := range rows {
		found = found || (r.Group == GroupSites && r.SiteID == id && r.Name == "Drone Ship" && r.Own)
	}
	if !found {
		t.Error("the ship is not a row of the t menu's SITES")
	}
	if id2 := w.stationShip(c, DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, 100, 1); id2 == id {
		t.Error("a second ship reused the first's site")
	} else if s2, _ := w.siteByID(id2); s2.Name != "Drone Ship 2" {
		t.Errorf("second ship = %q, want Drone Ship 2", s2.Name)
	}
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	b := w.Crafts[n]
	if len(w.PendingRecoveryEvents) != 0 {
		t.Fatalf("the drop was refused: %v", w.PendingRecoveryEvents)
	}
	flyDown(w, b)
	if d := distToLZ(t, w, b, id, 1); !b.Landed || d > SiteLZRadiusM {
		t.Errorf("landed=%v %.1f m from the ship, want inside %v m", b.Landed, d, SiteLZRadiusM)
	}
}

// The pad offers a stage only when it can land (legs, a command source, an engine)
// and every stage under it can too, and never the top stage (decision 11).
func TestRecoverableStages(t *testing.T) {
	for _, tc := range []struct {
		loadout string
		want    []int
	}{
		{"Falcon-9", []int{0}}, {"Falcon-9-Dragon", []int{0}}, {"Falcon-Heavy", []int{0, 1}},
		{"Saturn-V", nil}, {"Apollo-Stack", nil}, // the Ascent stage sits above stages that cannot land
		{"Ground-Station-Lander", nil}, // a single stage is the top stage
	} {
		got := RecoverableStages(spacecraft.LookupLoadout(tc.loadout).Stages)
		if len(got) != len(tc.want) {
			t.Errorf("%s: offered %v, want %v", tc.loadout, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%s: offered %v, want %v", tc.loadout, got, tc.want)
			}
		}
	}
}

// An LZ pick resolves to the nearest LZ site within RecoveryLZMaxM of the pad; with
// none near (Baikonur) it is dropped to none; a ship pick with no distance is `ship auto` (ADR 0071 decision 3).
func TestRecoveryLZPickNeedsASiteNearThePad(t *testing.T) {
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	pick := []RecoverPick{{Mode: spacecraft.RecoverLZ}}
	cape, _ := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true, Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast, Recover: pick})
	baikonur, _ := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true, Latitude: 45.965, LongitudeOffset: 63.342, Recover: pick})
	noKm, _ := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true, Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
		Recover: []RecoverPick{{Mode: spacecraft.RecoverShip}}})
	if cape.Stages[0].RecoverMode != spacecraft.RecoverLZ || cape.Stages[0].RecoverSiteID != lc13ID(t) {
		t.Errorf("Cape: recover %v site %d, want LZ at LC-13", cape.Stages[0].RecoverMode, cape.Stages[0].RecoverSiteID)
	}
	if baikonur.Stages[0].RecoverMode != spacecraft.RecoverNone || noKm.Stages[0].RecoverMode != spacecraft.RecoverShipAuto {
		t.Errorf("Baikonur %v, ship with no km %v, want none and ship auto", baikonur.Stages[0].RecoverMode, noKm.Stages[0].RecoverMode)
	}
	if s, ok := w.siteByID(noKm.Stages[0].RecoverSiteID); !ok || math.Abs(HaversineM(DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, s.LatDeg, s.LonDeg, noKm.Primary.RadiusMeters())-RecoveryShipDefaultKm*1e3) > 500 {
		t.Errorf("ship auto starts at %+v, want %v km out", s, RecoveryShipDefaultKm)
	}
	if _, ok := w.RecoveryLZNear("earth", DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, 1); !ok {
		t.Error("no LZ near the Cape")
	}
}

// Dropping before the window (the stage still too heavy) says what N said and
// leaves the stage a vessel with its target set, so ], t, N is as today.
func TestRecoveryEarlyDropSaysWhyAndKeepsTheTarget(t *testing.T) {
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, _ := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9-Dragon", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast, Recover: []RecoverPick{{Mode: spacecraft.RecoverLZ}}})
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	for i := 0; i < 20*40; i++ { // 40 s up, nowhere near the window
		w.Tick()
	}
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	b := w.Crafts[n]
	if len(w.PendingRecoveryEvents) != 1 || w.PendingRecoveryEvents[0].CraftName != b.Name || w.PendingRecoveryEvents[0].Refusal == "" {
		t.Fatalf("events %v, want one refusal naming the stage", w.PendingRecoveryEvents)
	}
	if b.Target.SiteID != lc13ID(t) || len(b.Nodes) != 0 || b.Landing.Owns() {
		t.Errorf("refused stage: target %+v nodes %d owns %v, want the target kept and nothing planted", b.Target, len(b.Nodes), b.Landing.Owns())
	}
}

// The reserve solve runs at most once a second and the cheap weight test keeps it
// off the climb until the stage can be landed at all: a Falcon 9 climb costs a
// handful of solves, not one a second (decision 15, bench in the PR).
func TestRecoveryReserveSolveCount(t *testing.T) {
	w, c := padClimb(t, "Falcon-9-Dragon", []RecoverPick{{Mode: spacecraft.RecoverLZ}}, 1)
	secs := w.Clock.SimTime.Sub(c.LiftoffSimTime).Seconds()
	if c.Stages[0].HeldKg <= 0 {
		t.Fatal("no cutoff")
	}
	if w.recoverySolves < 4 || w.recoverySolves > 16 {
		t.Errorf("%d reserve solves over a %.0f s climb, want 4 to 16 (measured 11)", w.recoverySolves, secs)
	}
}

// The cutoff gives the pilot time: a stage dropped 8 s after its cutoff still plans
// (measured: 15 s too), because the reserve carries RecoveryPadKg over the plan's own margin.
func TestRecoveryDropAfterCutoffStillLands(t *testing.T) {
	w, c := padClimb(t, "Falcon-9-Dragon", []RecoverPick{{Mode: spacecraft.RecoverLZ}}, 1)
	for i := 0; i < 20*8; i++ {
		w.Tick()
	}
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	b := w.Crafts[n]
	if len(w.PendingRecoveryEvents) != 0 || len(b.Nodes) != 2 {
		t.Fatalf("a drop 8 s after the cutoff was refused: %v", w.PendingRecoveryEvents)
	}
	_ = c
}

// RecoveryCutIn counts the climb down to the cutoff once the window is found.
func TestRecoveryCutInCountsDown(t *testing.T) {
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, _ := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9-Dragon", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast, Recover: []RecoverPick{{Mode: spacecraft.RecoverLZ}}})
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	t0 := w.Clock.SimTime
	var first, later float64
	for i := 0; i < 60000 && c.Stages[0].HeldKg == 0; i++ {
		switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
		case el > 8 && el < 14:
			c.PitchTrim = 0.015
		case el >= 14 && c.AttitudeMode == spacecraft.BurnRadialOut:
			c.PitchTrim, c.AttitudeMode = 0, spacecraft.BurnSurfacePrograde
		}
		w.Tick()
		if s, ok := w.RecoveryCutIn(c); ok {
			if first == 0 {
				first = s
			}
			later = s
		}
	}
	if first < 5 || later >= first-3 {
		t.Errorf("cut-in read %.1f s when found and %.1f s at the end, want a countdown from over 5 s", first, later)
	}
	if _, ok := w.RecoveryCutIn(c); ok {
		t.Error("a cut stage still reads a cutoff clock")
	}
}

// BenchmarkRecoveryReserveSolve is one reserve solve inside the window (decision
// 15's bench): the cost the 1 Hz schedule pays per solve.
func BenchmarkRecoveryReserveSolve(b *testing.B) {
	t := &testing.T{}
	w, c := padClimb(t, "Falcon-9-Dragon", []RecoverPick{{Mode: spacecraft.RecoverLZ}}, 1)
	c.Stages[0].ReleaseReserve()
	c.SyncFields()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, _, err := w.recoverySlack(c, c.Stages[0].FuelMass, 0, false); err != nil {
			b.Fatal(err)
		}
	}
	_ = time.Second
}

// R3: a plan N accepts must land. Ship distances 150 to 350 km in 50 km steps and
// the LZ, dropped at the cutoff or 16 s and 30 s after it, flown at 1x or the top
// warp (alternating: the failure was the same at both): the drop is accepted and the
// stage stands inside the circle with propellant left. Ship 300 and 350 were
// accepted, lit the brake 24 s early at 30 km and crashed 3 km out dry. Cost about
// 5 s of the sim suite.
func TestRecoveryAcceptedPlanLands(t *testing.T) {
	type tc struct {
		name  string
		pick  RecoverPick
		after int   // seconds after the cutoff the pilot drops
		warps []int // flight-home warp indices
	}
	one, max := []int{0}, []int{len(WarpFactors) - 1} // one warp each, alternating: the failure was the same at both
	ship := func(km float64) RecoverPick { return RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: km} }
	cases := []tc{
		{"ship150", ship(150), 0, one}, {"ship200", ship(200), 0, one}, {"ship250", ship(250), 0, max},
		{"ship300", ship(300), 0, one}, {"ship350", ship(350), 0, max}, {"ship300+16s", ship(300), 16, one},
		{"lz+30s", RecoverPick{Mode: spacecraft.RecoverLZ}, 30, max},
	}
	for _, k := range cases {
		for _, warp := range k.warps {
			w, c := padClimb(t, "Falcon-9-Dragon", []RecoverPick{k.pick}, 1)
			for i := 0; i < 20*k.after; i++ {
				w.Tick()
			}
			id := c.Stages[0].RecoverSiteID
			n := len(w.Crafts)
			if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
				t.Fatal(err)
			}
			if len(w.PendingRecoveryEvents) != 0 {
				t.Errorf("%s: refused %v, want an accepted plan (every distance here closes)", k.name, w.PendingRecoveryEvents)
				continue
			}
			b := w.Crafts[n]
			w.Clock.WarpIdx = warp
			flyDown(w, b)
			if d := distToLZ(t, w, b, id, 1); !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() <= 0 {
				t.Errorf("%s warp %d: accepted, landed=%v crashed=%v %.1f m out with %.0f kg, want inside %v m with propellant", k.name, warp, b.Landed, b.Crashed, d, b.ActiveStageFuel(), SiteLZRadiusM)
			}
		}
	}
}

// R2: a full LZ site is not an LZ choice. Two boosters already stand on LC-13: the
// pad's LZ pick falls to none, the climb burns to dry (no cutoff held 50 t for a
// drop that is then refused `no free LZ`), and a site that fills during the climb
// stops the cutoff too. The equal case: one LZ free for a single stage is enough.
func TestRecoveryFullLZSiteIsNotChosenAndNotReserved(t *testing.T) {
	lz := []RecoverPick{{Mode: spacecraft.RecoverLZ}}
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	standOnLZ(t, w, lc13ID(t), 1)
	if _, ok := w.RecoveryLZNear("earth", DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, 1); !ok {
		t.Error("LZ-2 is free: one half should still be offered")
	}
	if _, ok := w.RecoveryLZNear("earth", DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, 2); ok {
		t.Error("one LZ free: a booster pair needs two")
	}
	standOnLZ(t, w, lc13ID(t), 2)
	if _, ok := w.RecoveryLZNear("earth", DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, 1); ok {
		t.Error("both LZs taken: the LZ is still offered")
	}
	c := padClimbIn(t, w, "Falcon-9-Dragon", lz, 1)
	if st := c.Stages[0]; st.RecoverMode != spacecraft.RecoverNone || st.HeldKg != 0 || st.FuelMass != 0 {
		t.Errorf("full site: mode %v held %.0f fuel %.0f, want none and burned to dry", st.RecoverMode, st.HeldKg, st.FuelMass)
	}
	// the site fills after the pad: the cutoff checks again
	w2 := mustWorld(t)
	w2.Crafts, w2.ActiveCraftIdx = nil, -1
	c2 := padClimbIn(t, w2, "Falcon-9-Dragon", lz, 1, func(c *spacecraft.Spacecraft) {
		if c.Stages[0].RecoverMode != spacecraft.RecoverLZ {
			t.Fatalf("free site: mode %v, want LZ", c.Stages[0].RecoverMode)
		}
		standOnLZ(t, w2, lc13ID(t), 1)
		standOnLZ(t, w2, lc13ID(t), 2)
		w2.ActiveCraftIdx = len(w2.Crafts) - 3 // the climbing vessel stays the Active Vessel
	})
	if st := c2.Stages[0]; st.HeldKg != 0 || st.FuelMass != 0 {
		t.Errorf("site filled on the climb: held %.0f fuel %.0f, want burned to dry with no reserve held", st.HeldKg, st.FuelMass)
	}
}

// R1: Falcon Heavy's boosters to a drone ship: the ship stationed for a pair has
// two LZs, so both halves are accepted and land inside their circles (one ship, one
// LZ refused the second half `no free LZ`). A single stage's ship keeps one.
func TestRecoveryFalconHeavyBoostersShareAShip(t *testing.T) {
	w, c := padClimb(t, "Falcon-Heavy", []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: 250}}, 0.5)
	id := c.Stages[0].RecoverSiteID
	if s, ok := w.siteByID(id); !ok || s.LZs() != 2 {
		t.Fatalf("pair ship site = %+v (%v), want two LZs", s, ok)
	}
	if c.Stages[0].HeldKg <= 0 {
		t.Fatal("no BECO")
	}
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	bs := w.Crafts[n:]
	if len(bs) != 2 || len(w.PendingRecoveryEvents) != 0 || bs[0].LZClaim.N == bs[1].LZClaim.N {
		t.Fatalf("drop: %d vessels, events %v, want both boosters accepted on different LZs", len(bs), w.PendingRecoveryEvents)
	}
	lz := []int{bs[0].LZClaim.N, bs[1].LZClaim.N}
	flyDown(w, bs...)
	for i, b := range bs {
		if d := distToLZ(t, w, b, id, lz[i]); !b.Landed || d > SiteLZRadiusM {
			t.Errorf("%s landed=%v %.1f m from ship LZ-%d, want inside %v m", b.Name, b.Landed, d, lz[i], SiteLZRadiusM)
		}
	}
}

// climbStats is what a timed climb cost (R5): the worst tick and the total of
// w.Tick(), the reserve solves and the aim flights they ran, the smallest gap
// between two solves in real seconds (a tick is BaseStep of real time), and the
// reserve held at the cutoff.
type climbStats struct {
	worst, total time.Duration
	solves       int
	evals        int
	minGapRealS  float64
	held         float64
	cappedMax    float64 // top effective warp while the ship cap applied (0: it never did)
}

// climbTimed flies padClimb's ascent at warp index warpIdx from 20 s up (a burn
// clamps it to 10x) and times every w.Tick().
func climbTimed(t testing.TB, loadout string, picks []RecoverPick, warpIdx int, opts ...func(*World)) (st climbStats, w *World, c *spacecraft.Spacecraft) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, err = w.SpawnCraft(SpawnSpec{LoadoutID: loadout, ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast, Recover: picks})
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range opts {
		o(w)
	}
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	t0 := w.Clock.SimTime
	var lastSolve time.Time
	st.minGapRealS = math.Inf(1)
	for i := 0; i < 60000 && c.Stages[0].HeldKg == 0 && c.ManualBurn != nil; i++ {
		switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
		case el > 8 && el < 14:
			c.PitchTrim = 0.015
		case el >= 14 && c.AttitudeMode == spacecraft.BurnRadialOut:
			c.PitchTrim, c.AttitudeMode = 0, spacecraft.BurnSurfacePrograde
		}
		if w.Clock.SimTime.Sub(t0).Seconds() > 20 {
			w.Clock.WarpIdx = warpIdx
		}
		if w.recoveryWarpCap() > 0 {
			st.cappedMax = math.Max(st.cappedMax, w.EffectiveWarp())
		}
		n0 := w.recoverySolves
		start := time.Now()
		w.Tick()
		d := time.Since(start)
		st.total += d
		st.worst = max(st.worst, d)
		if w.recoverySolves > n0 {
			if !lastSolve.IsZero() {
				st.minGapRealS = math.Min(st.minGapRealS, w.Clock.SimTime.Sub(lastSolve).Seconds()/(float64(w.lastSimDelta)/float64(w.Clock.BaseStep)))
			}
			lastSolve = w.Clock.SimTime
		}
	}
	st.solves, st.evals, st.held = w.recoverySolves, w.aimEvals, c.Stages[0].HeldKg
	return st, w, c
}

// BenchmarkRecoveryClimb: per-tick worst case and total of a recovered climb, LZ
// and ship, at 1x and 10x (R5). Run with -benchtime=1x.
func BenchmarkRecoveryClimb(b *testing.B) {
	for _, tc := range []struct {
		name string
		pick RecoverPick
		warp int
	}{
		{"lz_1x", RecoverPick{Mode: spacecraft.RecoverLZ}, 0}, {"lz_10x", RecoverPick{Mode: spacecraft.RecoverLZ}, 1},
		{"ship250_1x", RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: 250}, 0}, {"ship250_10x", RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: 250}, 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				st, _, _ := climbTimed(b, "Falcon-9-Dragon", []RecoverPick{tc.pick}, tc.warp)
				b.ReportMetric(float64(st.worst.Milliseconds()), "worst_tick_ms")
				b.ReportMetric(float64(st.total.Milliseconds()), "total_ms")
				b.ReportMetric(float64(st.solves), "solves")
				b.ReportMetric(st.held, "held_kg")
				b.ReportMetric(float64(st.evals), "evals")
			}
		})
	}
}

// shortDrop flies a Falcon 9 + Dragon LZ climb, keeps only keep of the tank's
// capacity as the held reserve and drops it: the stage is a refused booster.
func shortDrop(t *testing.T, keep float64) (*World, *spacecraft.Spacecraft, int, time.Duration) {
	t.Helper()
	w, c := padClimb(t, "Falcon-9-Dragon", []RecoverPick{{Mode: spacecraft.RecoverLZ}}, 1)
	c.Stages[0].HeldKg = keep * c.Stages[0].FuelCapacity
	n, e0 := len(w.Crafts), w.aimEvals
	start := time.Now()
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	return w, w.Crafts[n], w.aimEvals - e0, time.Since(start)
}

// R6: a refused N on a booster is cheap. The shortfall search ("needs 3452 m/s,
// stage has 1712 m/s") re-solved the plan a dozen times with a cold start each (0.6 s,
// 3,500 flights); the drop no longer runs it at all (the flash is the plain refusal)
// and a repeated N press on the same stage in the same second is answered from the
// cache. Pin: the drop under 200 flights, the first N press under 1,500 (measured
// 2,400 before), the second press zero, and the figure unchanged within 1 %.
func TestRecoveryRefusedNIsCheap(t *testing.T) {
	w, b, dropEvals, dropWall := shortDrop(t, 0.10)
	if len(w.PendingRecoveryEvents) != 1 {
		t.Fatalf("events %v, want the one refusal", w.PendingRecoveryEvents)
	}
	press := func() (string, int, time.Duration) {
		e0, start := w.aimEvals, time.Now()
		var msg string
		w.asVessel(b, func() {
			if _, err := w.PlanDeorbitAndEngage(); err != nil {
				msg = err.Error()
			}
		})
		return msg, w.aimEvals - e0, time.Since(start)
	}
	msg1, e1, d1 := press()
	msg2, e2, d2 := press()
	w.Tick() // the navigation error redraws every tick; the key holds the true aim, not the drawn one
	msg3, e3, _ := press()
	for i := 0; i < 4; i++ {
		w.Tick()
	}
	msg4, e4, _ := press()
	t.Logf("drop %d flights %v, press 1 %d flights %v, press 2 %d flights %v, after 1 tick %d, after 4 ticks %d: %s", dropEvals, dropWall, e1, d1, e2, d2, e3, e4, msg1)
	if msg1 == "" || msg1 != msg2 || msg1 != msg3 || msg1 != msg4 {
		t.Fatalf("N said %q, %q, %q, %q, want the same refusal every time", msg1, msg2, msg3, msg4)
	}
	if dropEvals > 200 || e1 > 1500 || e2 != 0 || e3 != 0 || e4 != 0 {
		t.Errorf("drop %d, first press %d, then %d, %d, %d flights, want under 200, under 1,500 and 0 each", dropEvals, e1, e2, e3, e4)
	}
}

// R5: the cutoff's reserve solves are cheap and spaced in real time. A ship climb
// ran 6,100 aim flights over 7 solves (0.4 s a tick, four ticks before MECO) and at
// 10x the same sim-time schedule solved ten times as often in real time (a solve
// every 0.1 to 0.3 real seconds). Pins, per config: flights under 2,000 at 10x and
// 3,400 for a ship at 1x (the LZ is unchanged at about 1,050), and at least one real
// second between solves. The cutoff must not move: 1x holds exactly the old reserve
// (55,884 and 46,807 kg; 50,658 and 43,369 until the forecast flew the landing law and
// held 5.2 and 3.4 t more) and 10x within 5 t (1.2 % of the 411 t tank) either way,
// and the stage dropped there still lands inside the circle with propellant.
func TestRecoveryReserveSolvesAreCheapAndSpaced(t *testing.T) {
	lz, ship := RecoverPick{Mode: spacecraft.RecoverLZ}, RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: 250}
	one := map[string]float64{}
	for _, tc := range []struct {
		name      string
		pick      RecoverPick
		warp      int
		maxEvals  int
		held1x    float64 // the reserve before this change, kg
		fixedHeld bool
	}{
		{"lz 1x", lz, 0, 1300, 55884, true}, {"ship 1x", ship, 0, 3400, 45707, true},
		{"lz 10x", lz, 1, 700, 55884, false}, {"ship 10x", ship, 1, 3400, 45707, false},
	} {
		st, w, c := climbTimed(t, "Falcon-9-Dragon", []RecoverPick{tc.pick}, tc.warp)
		one[tc.name] = st.held
		t.Logf("%s: %d solves, %d flights, held %.0f kg, worst tick %v, min gap %.2f real s", tc.name, st.solves, st.evals, st.held, st.worst, st.minGapRealS)
		if st.evals > tc.maxEvals {
			t.Errorf("%s: %d aim flights, want at most %d", tc.name, st.evals, tc.maxEvals)
		}
		if st.minGapRealS < 1-1e-6 {
			t.Errorf("%s: two solves %.2f real s apart, want at least 1", tc.name, st.minGapRealS)
		}
		if tol := 5000.0; math.Abs(st.held-tc.held1x) > tol {
			t.Errorf("%s: held %.0f kg, was %.0f: the cutoff moved by more than %.0f", tc.name, st.held, tc.held1x, tol)
		}
		if tc.fixedHeld && math.Abs(st.held-tc.held1x) > 1 {
			t.Errorf("%s: held %.0f kg, want exactly the old %.0f (1x is unchanged)", tc.name, st.held, tc.held1x)
		}
		if tc.warp == 0 {
			continue
		}
		id := c.Stages[0].RecoverSiteID
		n := len(w.Crafts)
		if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
			t.Fatal(err)
		}
		if len(w.PendingRecoveryEvents) != 0 {
			t.Errorf("%s: the drop was refused: %v", tc.name, w.PendingRecoveryEvents)
			continue
		}
		b := w.Crafts[n]
		w.Clock.WarpIdx = 0
		flyDown(w, b)
		if d := distToLZ(t, w, b, id, 1); !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() <= 0 {
			t.Errorf("%s: landed=%v %.1f m out with %.0f kg, want inside %v m with propellant", tc.name, b.Landed, d, b.ActiveStageFuel(), SiteLZRadiusM)
		}
	}
}

// N1: at 10x a ship pick must still get its cutoff on the predicted line, not on the
// fixed-grid fallback: the stage is cut, dropped, accepted, lands inside the circle
// and keeps at least the boostback margin. Falcon 9 + Dragon ship 75 and the Falcon 9
// ships 90 and 150 are the cases that lost the booster or landed on 0.2 to 1 t. The
// bare Falcon 9 ship 350 had no cutoff at all (#993), and Falcon Heavy's booster pair
// to ships 150 and 250 (core on the default ship) landed on 0.35 t at 10x (#994).
func TestRecoveryTenXShipCutoffLandsWithMargin(t *testing.T) {
	for _, tc := range []struct {
		load string
		km   float64
	}{{"Falcon-9-Dragon", 75}, {"Falcon-9", 90}, {"Falcon-9", 150}, {"Falcon-9", 350}, {"Falcon-Heavy", 150}, {"Falcon-Heavy", 250}} {
		picks := []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: tc.km}}
		if tc.load == "Falcon-Heavy" {
			picks = append(picks, RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: RecoveryShipDefaultKm})
		}
		for _, warp := range []int{0, 1} {
			st, w, c := climbTimed(t, tc.load, picks, warp)
			name := fmt.Sprintf("%s ship %.0f warp %d", tc.load, tc.km, warp)
			t.Logf("%s: %d solves, %d flights, held %.0f kg, worst tick %v, min gap %.2f real s", name, st.solves, st.evals, st.held, st.worst, st.minGapRealS)
			if want := recoveryShipWarpCap; warp == 1 && (st.cappedMax == 0 || st.cappedMax > want) { // equal case: exactly the cap is allowed
				t.Errorf("%s: warp while capped peaked at %.1f, want 0 < peak <= %.0f", name, st.cappedMax, want)
			}
			if st.held <= 0 {
				t.Errorf("%s: no cutoff, the stage burned to %v", name, c.Stages[0].FuelMass)
				continue
			}
			id := c.Stages[0].RecoverSiteID
			n := len(w.Crafts)
			if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
				t.Fatal(err)
			}
			if len(w.PendingRecoveryEvents) != 0 {
				t.Errorf("%s: held %.0f kg, the drop was refused: %v", name, st.held, w.PendingRecoveryEvents)
				continue
			}
			bs := w.Crafts[n:]
			lz := make([]int, len(bs)) // the claim clears on touchdown: read it before
			for i, b := range bs {
				lz[i] = max(b.LZClaim.N, 1)
			}
			w.Clock.WarpIdx = 0
			flyDown(w, bs...)
			for i, b := range bs {
				if d := distToLZ(t, w, b, id, lz[i]); !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() < BoostbackFuelMarginKg {
					t.Errorf("%s: %s landed=%v %.1f m from LZ-%d with %.0f kg, want inside %v m with at least %.0f kg", name, b.Name, b.Landed, d, lz[i], b.ActiveStageFuel(), SiteLZRadiusM, BoostbackFuelMarginKg)
				}
			}
		}
	}
}

// fhPad spawns a Falcon Heavy on the pad with the pad's defaults (boosters LZ, core
// ship 250 km) or with the boosters set to none, and lights it at the player's 100 %.
func fhPad(t *testing.T, boosters RecoverPick) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-Heavy", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
		Recover: []RecoverPick{boosters, {Mode: spacecraft.RecoverShip, ShipKm: RecoveryShipDefaultKm}}})
	if err != nil {
		t.Fatal(err)
	}
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	return w, c
}

// The Falcon Heavy auto hold: from the pad defaults, with no hand throttling, the
// core holds 60 % until BECO and both boosters land on LZ-1 and LZ-2 (before, the
// core at 100 % never gave a window: no BECO, three refusals). After BECO the
// player's own 100 % is back, and it was never rewritten.
func TestRecoveryFalconHeavyAutoCoreHoldLandsBoosters(t *testing.T) {
	for _, warp := range []int{0, len(WarpFactors) - 1} {
		w := mustWorld(t)
		w.Crafts, w.ActiveCraftIdx = nil, -1
		c := padClimbIn(t, w, "Falcon-Heavy", []RecoverPick{{Mode: spacecraft.RecoverLZ}, {Mode: spacecraft.RecoverShip, ShipKm: RecoveryShipDefaultKm}}, 1)
		if c.Stages[0].HeldKg <= 0 || c.ManualBurn == nil || c.Stages[1].FuelMass <= 0 {
			t.Fatalf("warp %d: held %.0f, core burning %v, core fuel %.0f, want BECO with the core burning on", warp, c.Stages[0].HeldKg, c.ManualBurn != nil, c.Stages[1].FuelMass)
		}
		if c.Throttle != 1 || c.EffectiveThrottle() != 1 || c.CoreAuto() {
			t.Errorf("after BECO: throttle %.2f effective %.2f auto %v, want the player's 1 back", c.Throttle, c.EffectiveThrottle(), c.CoreAuto())
		}
		n := len(w.Crafts)
		if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
			t.Fatal(err)
		}
		bs := w.Crafts[n:]
		if len(bs) != 2 || len(w.PendingRecoveryEvents) != 0 || bs[0].LZClaim.N == bs[1].LZClaim.N {
			t.Fatalf("drop: %d vessels, events %v, want two boosters accepted on LZ-1 and LZ-2", len(bs), w.PendingRecoveryEvents)
		}
		lz := []int{bs[0].LZClaim.N, bs[1].LZClaim.N}
		w.Clock.WarpIdx = warp
		flyDown(w, bs...)
		for i, b := range bs {
			if d := distToLZ(t, w, b, lc13ID(t), lz[i]); !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() <= 0 {
				t.Errorf("warp %d: %s landed=%v %.1f m from LZ-%d with %.0f kg, want inside %v m with propellant", warp, b.Name, b.Landed, d, lz[i], b.ActiveStageFuel(), SiteLZRadiusM)
			}
		}
	}
}

// Boosters set to none leave the core at the player's throttle all the way, the
// hold shows 60 % while it applies, and the player's input wins: moving the core
// ends the hold for the flight (the next press steps from the 60 % shown).
func TestRecoveryFalconHeavyAutoCoreHoldYieldsToThePlayerAndToNone(t *testing.T) {
	w, c := fhPad(t, RecoverPick{Mode: spacecraft.RecoverNone})
	for i := 0; i < 20*60; i++ {
		w.Tick()
		if c.Stages[0].FuelMass > 0 && (c.EffectiveThrottle() != 1 || c.CoreAuto()) {
			t.Fatalf("boosters none: core at %.2f (auto %v) at tick %d, want the player's 1 all the way", c.EffectiveThrottle(), c.CoreAuto(), i)
		}
	}
	w, c = fhPad(t, RecoverPick{Mode: spacecraft.RecoverLZ})
	for i := 0; i < 20*10; i++ {
		w.Tick()
	}
	if got := c.EffectiveThrottle(); got != spacecraft.CoreAutoThrottle || !c.CoreAuto() || c.BoosterThrottleEff() != 1 {
		t.Fatalf("auto hold: core %.2f boosters %.2f auto %v, want core %.2f and boosters at the player's 1", got, c.BoosterThrottleEff(), c.CoreAuto(), spacecraft.CoreAutoThrottle)
	}
	w.AdjustThrottle(-0.05) // the key steps from what the readout shows
	if got := c.EffectiveThrottle(); math.Abs(got-0.55) > 1e-9 || c.CoreAuto() || c.BoosterThrottleEff() != 1 {
		t.Errorf("after one step down: core %.2f boosters %.2f auto %v, want 0.55, the boosters still at 1 and the hold ended", got, c.BoosterThrottleEff(), c.CoreAuto())
	}
	w.SetThrottle(0.6) // hold over: target all behaves as before, the boosters follow the core again
	if c.CoreAuto() || c.EffectiveThrottle() != 0.6 || c.BoosterThrottleEff() != 0.6 {
		t.Errorf("after setting 0.6: core %.2f boosters %.2f auto %v, want the player's 0.6 on both, no hold", c.EffectiveThrottle(), c.BoosterThrottleEff(), c.CoreAuto())
	}
}

// N2: while the auto hold runs the ENGINE row says `core:`, so a write under the
// default target (all) is a core write: the boosters stay where they are. Every
// key is pinned (one trim step, a write equal to the held 60 %, full, cut); after
// the hold ends target all moves both again.
func TestRecoveryFalconHeavyHoldWriteUnderAllIsACoreWrite(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(w *World)
		core float64
	}{
		{"trim 1 %", func(w *World) { w.AdjustThrottle(-0.01) }, 0.59},
		{"equal to the held 60 %", func(w *World) { w.SetThrottle(spacecraft.CoreAutoThrottle) }, spacecraft.CoreAutoThrottle},
		{"full", func(w *World) { w.SetThrottle(1) }, 1},
		{"cut", func(w *World) { w.SetThrottle(0) }, 0},
	} {
		w, c := fhPad(t, RecoverPick{Mode: spacecraft.RecoverLZ})
		for i := 0; i < 20*10; i++ {
			w.Tick()
		}
		if !c.CoreAuto() || c.BoosterThrottleEff() != 1 {
			t.Fatalf("%s: no hold at the start (auto %v boosters %.2f)", tc.name, c.CoreAuto(), c.BoosterThrottleEff())
		}
		tc.do(w)
		if got := c.EffectiveThrottle(); math.Abs(got-tc.core) > 1e-9 || c.BoosterThrottleEff() != 1 || c.CoreAuto() {
			t.Errorf("%s: core %.2f boosters %.2f auto %v, want core %.2f, boosters 1, hold ended", tc.name, got, c.BoosterThrottleEff(), c.CoreAuto(), tc.core)
		}
	}
}

// flatKickClimb flies Jason's flat climb (kick 1 degree at T+kick, surface prograde
// 2 s later, trim 0, then -1..-10 degrees over T+111..124) of a Falcon 9 + Dragon
// recovering at pick, 1x, until the cutoff or the tank runs dry.
func flatKickClimb(t *testing.T, kick float64, pick RecoverPick) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	deg := math.Pi / 180
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9-Dragon", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
		Recover: []RecoverPick{pick}})
	if err != nil {
		t.Fatal(err)
	}
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	t0 := w.Clock.SimTime
	for i := 0; i < 60000 && c.Stages[0].HeldKg == 0 && c.ManualBurn != nil; i++ {
		switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
		case el >= kick && el < kick+2:
			c.PitchTrim = deg
		case el >= kick+2 && c.AttitudeMode == spacecraft.BurnRadialOut:
			c.AttitudeMode = spacecraft.BurnSurfacePrograde
		case el >= kick+5 && el < 111:
			c.PitchTrim = 0
		case el >= 111 && el < 124:
			c.PitchTrim = -math.Ceil((el-111)/1.3+0.01) * deg
		case el >= 124:
			c.PitchTrim = -10 * deg
		}
		w.Tick()
	}
	w.Tick()
	return w, c
}

// A flat climb (the pitch kick a little early, MECO at 36 to 52 km instead of 52+)
// that cuts off must cut at a reserve the drop accepts, or say plainly that it
// could not. Falcon 9 + Dragon, ship 250 km, 1x. Before: the solve that closed at
// T+127 was not the one at T+131 (closing is not smooth in the stage's state), the
// "closed before, none now" fallback cut on the failed solve and the drop refused
// the same state (8.25, 8.5, 8.75). Kicks 8.0 and 8.5 were then accepted at the
// drop but flew 1.7 to 13 km short with the tank dry: the forecast left 4 to 5 t
// at the ground and the flown landing law left 0 to 2 t. The forecast now flies
// that law (oneBurnFuelLeft), so the drop is accepted only when the flight keeps
// its propellant: 8.5, 8.75 and 9.0 land with 3.4 to 4.2 t, 8.25 reaches the reserve
// line with no plan that closes (the pilot is told), 8.0 cuts (burnAtCue finds the last safe light) and its 36 km drop, whose boostback is planned in vacuum and loses about 110 m/s to drag, arrives 15 km out: the correction burn behind the boostback (about 150 m/s) puts it on the circle (it flew 11 km out, dry, before: #999).
func TestRecoveryFlatClimbCutoffNeverLeavesARefusedDrop(t *testing.T) {
	const (
		land   = iota // cuts off, the drop is accepted, the stage lands with the margin
		told          // reaches the reserve line, the drop is refused and the pilot told
		noPlan        // no plan ever closes: no cutoff
	)
	for _, tc := range []struct {
		kick float64
		want int
	}{{8.0, land}, {8.25, land}, {8.5, land}, {8.75, land}, {9.0, land}} {
		w, c := flatKickClimb(t, tc.kick, RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: RecoveryShipDefaultKm})
		name := fmt.Sprintf("kick %.2f", tc.kick)
		held, id := c.Stages[0].HeldKg, c.Stages[0].RecoverSiteID
		if (held > 0) != (tc.want != noPlan) {
			t.Errorf("%s: held %.0f kg, want a cutoff: %v", name, held, tc.want != noPlan)
			continue
		}
		if tc.want == noPlan {
			continue
		}
		n := len(w.Crafts)
		if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
			t.Fatal(err)
		}
		if refused := len(w.PendingRecoveryEvents) != 0; refused != (tc.want == told) {
			t.Errorf("%s: drop refused=%v (%v), want %v", name, refused, w.PendingRecoveryEvents, tc.want == told)
			continue
		}
		if tc.want == told {
			continue
		}
		b := w.Crafts[n]
		flyDown(w, b)
		if d := distToLZ(t, w, b, id, 1); !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() < BoostbackFuelMarginKg {
			t.Errorf("%s: landed=%v %.1f m out with %.0f kg, want inside %v m with at least %.0f kg", name, b.Landed, d, b.ActiveStageFuel(), SiteLZRadiusM, BoostbackFuelMarginKg)
		}
	}
}

// ADR 0071 decision 1: a stage whose one-engine reserve solve cannot close lands on
// three engines of nine; a stage the one-engine solve closes stays on one.
// Guards 1, 2 and 4 of the ADR (#993, #994 at 10x, the equal case), flown from the
// pad: wantThree says which landing throttle the dropped stages must carry, wantCut
// whether the climb gets a MECO/BECO at all, and every dropped stage must land inside
// the LZ circle (SiteLZRadiusM) on the ship.
func TestRecoveryThreeEngineFallback(t *testing.T) {
	for _, tc := range []struct {
		name      string
		load      string
		km        float64
		warp      int
		wantThree bool
		east      bool // the ship stays due east, off the track (#994 was filed with it there; on the track one engine closes)
	}{
		{"#993 bare Falcon 9, ship 350 km", "Falcon-9", 350, 0, true, false},
		{"#994 Falcon Heavy boosters, ship 250 km at 10x", "Falcon-Heavy", 250, 1, true, true},
		// the equal case: today's one-engine solve closes these, they must not move
		{"Falcon 9, ship 150 km (one engine closes)", "Falcon-9", 150, 0, false, false},
		{"Falcon Heavy boosters, ship 250 km at 1x (one engine closes)", "Falcon-Heavy", 250, 0, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st, w, c := climbTimed(t, tc.load, []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: tc.km}}, tc.warp, func(w *World) { w.noShipFollow = tc.east })
			if st.held <= 0 {
				t.Fatalf("no cutoff: the stage burned dry")
			}
			w.Tick()
			id, n := c.Stages[0].RecoverSiteID, len(w.Crafts)
			if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
				t.Fatal(err)
			}
			if len(w.PendingRecoveryEvents) > 0 {
				t.Fatalf("drop refused: %v", w.PendingRecoveryEvents)
			}
			w.Clock.WarpIdx = 0
			bs := w.Crafts[n:]
			w.SetActiveCraftIdx(n)
			dir := recAttach(t, w) // #1023: the first stage's recording names its landing engines
			flyDown(w, bs...)
			wantRow := "landing burn: 1 engine"
			if tc.wantThree {
				wantRow = "landing burn: 3 engines"
			}
			var rows []string
			for _, e := range allEvents(t, dir) {
				if strings.HasPrefix(e, "landing burn") {
					rows = append(rows, e)
				}
			}
			if len(rows) != len(bs) { // one row per stage, each in its own file
				t.Errorf("landing burn rows = %q, want %d of %q", rows, len(bs), wantRow)
			}
			for _, r := range rows {
				if r != wantRow {
					t.Errorf("landing burn row %q, want %q", r, wantRow)
				}
			}
			for i, b := range bs {
				d := 1e18
				for k := 1; k <= 2; k++ {
					d = math.Min(d, distToLZ(t, w, b, id, k))
				}
				one := math.Abs(b.LandingThrottle()-1.0/9) < 1e-9
				t.Logf("stage %d: held %.0f kg, landed=%v %.1f m out, landing throttle %.4f", i+1, st.held, b.Landed, d, b.LandingThrottle())
				if !b.Landed || d > SiteLZRadiusM {
					t.Errorf("stage %d: landed=%v %.1f m out, want landed inside %v m", i+1, b.Landed, d, SiteLZRadiusM)
				}
				if tc.wantThree != (math.Abs(b.LandingThrottle()-ThreeEngineLandingThrottle) < 1e-9) || (!tc.wantThree && !one) {
					t.Errorf("stage %d: landing throttle %.4f, want three engines %v (three %.4f, one %.4f)", i+1, b.LandingThrottle(), tc.wantThree, ThreeEngineLandingThrottle, 1.0/9)
				}
			}
		})
	}
}

// Guard 4 of ADR 0071 on the core: Falcon Heavy's core, boosters dropped at their
// BECO, to a typed ship 250 km out on the standard climb gets a MECO and lands inside
// the circle (on today's one-engine solve it burns dry).
func TestRecoveryCoreThreeEngineFallback(t *testing.T) {
	w := mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c := padClimbIn(t, w, "Falcon-Heavy", []RecoverPick{{Mode: spacecraft.RecoverLZ}, {Mode: spacecraft.RecoverShip, ShipKm: 250}}, 1)
	w.StageActive(w.ActiveCraftIdx)
	st := &c.Stages[0]
	for i := 0; i < 60000 && st.HeldKg == 0 && c.ManualBurn != nil && st.FuelMass > 0; i++ {
		w.Tick()
	}
	if st.HeldKg == 0 {
		t.Fatal("no MECO: the core burned dry")
	}
	w.Tick()
	id, n := st.RecoverSiteID, len(w.Crafts)
	w.StageActive(w.ActiveCraftIdx)
	if len(w.PendingRecoveryEvents) > 0 {
		t.Fatalf("drop refused: %v", w.PendingRecoveryEvents)
	}
	core := w.Crafts[n]
	flyDown(w, core)
	d := distToLZ(t, w, core, id, 1)
	t.Logf("core: landed=%v %.1f m from the ship, landing throttle %.4f", core.Landed, d, core.LandingThrottle())
	if !core.Landed || d > SiteLZRadiusM {
		t.Errorf("core landed=%v %.1f m out, want inside %v m", core.Landed, d, SiteLZRadiusM)
	}
}

// flownF9 flies the pilot's recorded bare Falcon 9 to a ship 350 km out (ADR 0071
// playtest, 2026-11-15, flight 2) and stages delay after the cutoff (0 is the tick
// after it). The climb is the recording's: prograde hold at T+10.5 with a 1 degree
// trim to T+16.2, then +5 degrees at T+126.5 and +10 at T+127.5, held to the cutoff.
// The 10.5 and 16.2 are fitted, not read: the climb is knife-edge in them, and
// these give the recording's 35.3 km at T+100 and 81 km at the cutoff (recorded
// 81.3 km, 2,590 m/s, 30,715 kg held; this replay 30,853 kg).
func flownF9(t *testing.T, delay time.Duration) (w *World, b *spacecraft.Spacecraft, held float64, id uint64) {
	t.Helper()
	const deg = math.Pi / 180
	w = mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
		Recover: []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: 350}}})
	if err != nil {
		t.Fatal(err)
	}
	w.noShipFollow = true // the recording's ship stood 090 off the pad, 14 deg off the track
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	t0 := w.Clock.SimTime
	for i := 0; i < 60000 && c.Stages[0].HeldKg == 0 && c.ManualBurn != nil; i++ {
		switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
		case el >= 10.5 && el < 16.2:
			c.PitchTrim, c.AttitudeMode = 1*deg, spacecraft.BurnSurfacePrograde
		case el >= 16.2 && el < 126.5:
			c.PitchTrim = 0
		case el >= 126.5 && el < 127.5:
			c.PitchTrim = 5 * deg
		case el >= 127.5:
			c.PitchTrim = 10 * deg
		}
		w.Tick()
	}
	if c.Stages[0].HeldKg == 0 {
		t.Fatalf("no cutoff: the stage burned dry (T+%.0f)", w.Clock.SimTime.Sub(t0).Seconds())
	}
	held = c.Stages[0].HeldKg
	for end := w.Clock.SimTime.Add(delay); w.Clock.SimTime.Before(end); {
		w.Tick()
	}
	if delay == 0 {
		w.Tick() // the tick after the cutoff ends the burn
	}
	id, n := c.Stages[0].RecoverSiteID, len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	if len(w.PendingRecoveryEvents) > 0 {
		t.Fatalf("drop refused %v after %v: held %.0f kg", w.PendingRecoveryEvents, delay, held)
	}
	return w, w.Crafts[n], held, id
}

// ADR 0071 playtest flight 2: the cut the three-engine fallback made was refused at
// the drop 2 s later (`no boostback lands on the site`) and the stage fell 392 km
// from the ship. Staging is the pilot's press, so a cut must land whenever it comes:
// it was made on 2.2 t of slack, where this plan stops closing 1.5 s after the cut
// (and the one tick after it flew 21 km short: the plan closed, the autopilot lit
// 170 km up). It now cuts on 4 t where one engine cannot aim, and only if the plan
// still closes RecoveryStageGraceS later. Delay 0 is the boundary: the very next tick.
//
// The stage lands inside the 43 m circle at every delay. It used to land 125 to 205 m
// out: the three-engine stop swung the nose 35 degrees in two seconds, the 15 deg/s
// slew left it 25 degrees behind and flung the stage 10 m/s sideways at 1 m up
// (oneBurnLateralLimit).
func TestRecoveryThreeEngineCutLandsWhenTheyStage(t *testing.T) {
	for _, delay := range []time.Duration{0, time.Second, 2 * time.Second, 5 * time.Second, 10 * time.Second} {
		t.Run(delay.String(), func(t *testing.T) {
			w, b, held, id := flownF9(t, delay)
			if held < 30000 || held > 40000 {
				t.Errorf("cut on %.0f kg held, want the 30 to 40 t the recording's climb gives", held)
			}
			flyDown(w, b)
			d := distToLZ(t, w, b, id, 1)
			t.Logf("staged %v after the cut: held %.0f kg, landed=%v crashed=%v %.1f m from the ship", delay, held, b.Landed, b.Crashed, d)
			if !b.Landed || b.Crashed || !insideLZ(d) {
				t.Errorf("landed=%v crashed=%v %.1f m from the ship, want down, soft and inside the %v m circle", b.Landed, b.Crashed, d, SiteLZRadiusM)
			}
			if math.Abs(b.LandingThrottle()-ThreeEngineLandingThrottle) > 1e-9 {
				t.Errorf("landing throttle %.4f, want three engines %.4f", b.LandingThrottle(), ThreeEngineLandingThrottle)
			}
		})
	}
}

// Side boosters of a Falcon Heavy dropped low land on their circle. The boostback is
// planned in vacuum and flown through air, so a drop at 44 km arrives 2.3 km short
// of the plan, one at 42 km 2.8 km: the landing law steered out about 1.5 km and
// the rest hovered dry 0.7 to 1.3 km out (booster 1, 2026-11-15). The correction
// burn behind the boostback (16 and 22 m/s) closes it. The climb is Jason's flight
// fitted: an early 1 degree kick from T+8 to T+off, the core to a ship 250 km out.
func TestRecoveryLowBoosterDropLandsInsideTheCircle(t *testing.T) {
	const deg = math.Pi / 180
	for _, tc := range []struct {
		name    string
		offSec  float64
		corrDV  float64 // the correction burn the drop must fly, m/s
		corrTol float64
	}{{"drop at 44 km", 23, 16, 4}, {"drop at 42 km", 26, 22, 5}} {
		t.Run(tc.name, func(t *testing.T) {
			w := mustWorld(t)
			w.Crafts, w.ActiveCraftIdx = nil, -1
			c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-Heavy", ParentBodyID: "earth", Launchpad: true,
				Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
				Recover: []RecoverPick{{Mode: spacecraft.RecoverLZ}, {Mode: spacecraft.RecoverShip, ShipKm: 250}}})
			if err != nil {
				t.Fatal(err)
			}
			w.RecomputeCommGraph()
			c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
			w.StartManualBurn()
			t0 := w.Clock.SimTime
			for i := 0; i < 60000 && c.ManualBurn != nil && c.Stages[0].HeldKg == 0; i++ {
				el := w.Clock.SimTime.Sub(t0).Seconds()
				for _, s := range []struct{ at, deg float64 }{{8, 1}, {tc.offSec, 0}, {65, -5}, {74, 0}, {114, -1}, {128, 0}} {
					if el >= s.at-1.05 {
						c.PitchTrim = s.deg * deg
					}
				}
				if el >= 13-1.05 && c.AttitudeMode == spacecraft.BurnRadialOut {
					c.AttitudeMode = spacecraft.BurnSurfacePrograde
				}
				w.Tick()
			}
			if c.Stages[0].HeldKg == 0 {
				t.Fatal("no BECO")
			}
			for end := w.Clock.SimTime.Add(time.Second); w.Clock.SimTime.Before(end); {
				w.Tick()
			}
			id, n := c.Stages[0].RecoverSiteID, len(w.Crafts)
			w.PendingBurnFiredEvents = nil
			if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil || len(w.PendingRecoveryEvents) != 0 {
				t.Fatalf("drop: %v %v", err, w.PendingRecoveryEvents)
			}
			bs := w.Crafts[n:]
			if len(bs) != 2 {
				t.Fatalf("%d boosters dropped, want 2", len(bs))
			}
			flyDown(w, bs...)
			corr := 0
			for _, e := range w.PendingBurnFiredEvents {
				if e.CraftName == bs[0].Name && e.DV < 60 && e.DV > 1 {
					corr++
					if math.Abs(e.DV-tc.corrDV) > tc.corrTol {
						t.Errorf("correction burn %.1f m/s, want %.0f within %.0f", e.DV, tc.corrDV, tc.corrTol)
					}
				}
			}
			if corr != 1 {
				t.Errorf("%d short burns fired by booster 1, want one correction", corr)
			}
			for i, b := range bs {
				d := math.Min(distToLZ(t, w, b, id, 1), distToLZ(t, w, b, id, 2))
				t.Logf("booster %d: landed=%v %.1f m out, %.0f kg left", i+1, b.Landed, d, b.ActiveStageFuel())
				if !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() < BoostbackFuelMarginKg {
					t.Errorf("booster %d: landed=%v %.1f m out with %.0f kg, want inside %v m with at least %.0f kg", i+1, b.Landed, d, b.ActiveStageFuel(), SiteLZRadiusM, BoostbackFuelMarginKg)
				}
			}
		})
	}
}

// shipClimb flies the harness ascent of padClimbIn on a Falcon 9 + Dragon with a
// ship pick km out, launched on headingDeg (090 due east), to the cutoff, counting
// the ticks the ship's position changed (each is a site-ledger write).
func shipClimb(t *testing.T, headingDeg, km float64) (w *World, c *spacecraft.Spacecraft, moves int) {
	t.Helper()
	w = mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9-Dragon", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
		Recover: []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: km}}})
	if err != nil {
		t.Fatal(err)
	}
	w.RecomputeCommGraph()
	c.HeadingTrim = (headingDeg - 90) * math.Pi / 180
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	t0 := w.Clock.SimTime
	s0, _ := w.siteByID(c.Stages[0].RecoverSiteID)
	last := [2]float64{s0.LatDeg, s0.LonDeg}
	for i := 0; i < 60000 && c.Stages[0].HeldKg == 0 && c.ManualBurn != nil; i++ {
		switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
		case el > 8 && el < 14:
			c.PitchTrim = 0.015
		case el >= 14 && c.AttitudeMode == spacecraft.BurnRadialOut:
			c.PitchTrim, c.AttitudeMode = 0, spacecraft.BurnSurfacePrograde
		}
		w.Tick()
		if s, _ := w.siteByID(c.Stages[0].RecoverSiteID); [2]float64{s.LatDeg, s.LonDeg} != last {
			last = [2]float64{s.LatDeg, s.LonDeg}
			moves++
		}
	}
	if c.Stages[0].HeldKg == 0 {
		t.Fatal("no cutoff")
	}
	return w, c, moves
}

// crossRangeM is how far the ship stands off the great circle from the pad through
// the vessel's ground point, positive to the right.
func crossRangeM(w *World, c *spacecraft.Spacecraft, s Site) float64 {
	vl, vo := render.WorldToBodyFixed(c.Primary, unitV(c.State.R), w.Clock.SimTime)
	r := c.Primary.RadiusMeters()
	d := HaversineM(c.LaunchLatDeg, c.LaunchLonDeg, s.LatDeg, s.LonDeg, r)
	rel := (ForwardAzimuthDeg(c.LaunchLatDeg, c.LaunchLonDeg, s.LatDeg, s.LonDeg) - ForwardAzimuthDeg(c.LaunchLatDeg, c.LaunchLonDeg, vl, vo)) * rad
	return d * math.Sin(rel)
}

// ADR 0071 decision 4 (#998): a ship pick follows the vessel's ground track from the
// pad until the cutoff, so a launch off due east (a 51.6 degree target from the Cape
// flies about 045) still finds its ship on the track and lands on it; the ship moves
// a bounded number of times and never after the cutoff.
func TestRecoveryShipFollowsTheHeadingUntilTheCutoff(t *testing.T) {
	for _, tc := range []struct {
		name    string
		heading float64
	}{{"due east", 90}, {"051.6 target (045 launch)", 45}} {
		w, c, moves := shipClimb(t, tc.heading, 250)
		id := c.Stages[0].RecoverSiteID
		s, _ := w.siteByID(id)
		cross := crossRangeM(w, c, s)
		t.Logf("%s: cross-range %.0f m, %d moves, ship bears %.2f deg", tc.name, cross, moves, ForwardAzimuthDeg(c.LaunchLatDeg, c.LaunchLonDeg, s.LatDeg, s.LonDeg))
		if math.Abs(cross) > 5e3 {
			t.Errorf("%s: ship %.0f m off the ground track at the cutoff, want within 5 km", tc.name, cross)
		}
		if secs := w.Clock.SimTime.Sub(c.LiftoffSimTime).Seconds(); moves < 1 || float64(moves) > secs/(recoveryMinSec*recoveryShipSlow) {
			t.Errorf("%s: ship moved %d times in a %.0f s climb, want 1 to one per %.0f s", tc.name, moves, secs, recoveryMinSec*recoveryShipSlow)
		}
		n := len(w.Crafts)
		if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
			t.Fatal(err)
		}
		if len(w.PendingRecoveryEvents) != 0 {
			t.Fatalf("%s: the drop was refused: %v", tc.name, w.PendingRecoveryEvents)
		}
		b := w.Crafts[n]
		flyDown(w, b, c)
		if after, _ := w.siteByID(id); after.LatDeg != s.LatDeg || after.LonDeg != s.LonDeg || after.UpdatedAtNano != s.UpdatedAtNano {
			t.Errorf("%s: the ship moved after the cutoff: %+v -> %+v", tc.name, s, after)
		}
		if d := distToLZ(t, w, b, id, 1); !b.Landed || d > SiteLZRadiusM {
			t.Errorf("%s: landed=%v %.1f m from the ship, want inside %v m", tc.name, b.Landed, d, SiteLZRadiusM)
		}
	}
}

// fhAutoClimb flies a Falcon Heavy (boosters LZ, core pick) to the core's MECO and
// counts the ticks the core's ship moved (each is a site-ledger write). fitted is the
// review's climb fitted to the real game (1 degree kick T+8 to T+23, the recording's
// trims, core pitched down from T+134); else the standard padClimb ascent. It returns
// at the tick after the cutoff, the boosters already staged away.
func fhAutoClimb(t *testing.T, fitted bool, pick RecoverPick, worst ...*time.Duration) (w *World, c *spacecraft.Spacecraft, moves int) {
	t.Helper()
	const deg = math.Pi / 180
	w = mustWorld(t)
	w.Crafts, w.ActiveCraftIdx = nil, -1
	c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-Heavy", ParentBodyID: "earth", Launchpad: true,
		Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
		Recover: []RecoverPick{{Mode: spacecraft.RecoverLZ}, pick}})
	if err != nil {
		t.Fatal(err)
	}
	w.RecomputeCommGraph()
	c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
	w.StartManualBurn()
	t0 := w.Clock.SimTime
	steps := []struct{ at, deg float64 }{{8, 1}, {23, 0}, {65, -5}, {74, 0}, {114, -1}, {128, 0}, {134, -5}, {134.5, -10}, {138, -15}}
	trim := func() {
		el := w.Clock.SimTime.Sub(t0).Seconds()
		if fitted {
			for _, s := range steps {
				if el >= s.at-1.05 {
					c.PitchTrim = s.deg * deg
				}
			}
			if el >= 13-1.05 && c.AttitudeMode == spacecraft.BurnRadialOut {
				c.AttitudeMode = spacecraft.BurnSurfacePrograde
			}
			return
		}
		switch {
		case el > 8 && el < 14:
			c.PitchTrim = 0.015
		case el >= 14 && c.AttitudeMode == spacecraft.BurnRadialOut:
			c.PitchTrim, c.AttitudeMode = 0, spacecraft.BurnSurfacePrograde
		}
	}
	for i := 0; i < 60000 && c.Stages[0].HeldKg == 0 && c.ManualBurn != nil; i++ {
		trim()
		w.Tick()
	}
	if c.Stages[0].HeldKg == 0 {
		t.Fatal("no BECO")
	}
	w.Tick() // the tick after the cutoff ends the burn
	w.StageActive(w.ActiveCraftIdx)
	st := &c.Stages[0]
	s0, _ := w.siteByID(st.RecoverSiteID)
	last := s0.UpdatedAtNano
	for i := 0; i < 60000 && st.HeldKg == 0 && c.ManualBurn != nil && st.FuelMass > 0; i++ {
		trim()
		start := time.Now()
		w.Tick()
		if d := time.Since(start); len(worst) > 0 && d > *worst[0] {
			*worst[0] = d
		}
		if s, _ := w.siteByID(st.RecoverSiteID); s.UpdatedAtNano != last {
			last = s.UpdatedAtNano
			moves++
		}
	}
	if st.HeldKg == 0 {
		t.Fatalf("no MECO: the core burned dry (fuel %.0f)", st.FuelMass)
	}
	w.Tick()
	return w, c, moves
}

// ADR 0071 decisions 2 to 4 and Jason's entry-burn call, fix 3: `ship auto` on the Falcon
// Heavy core. The ship starts 250 km out and, from the first moment a landing works,
// takes the forecast stop of the fall; the core burns to the smallest reserve that
// lands, and lands inside the circle. The standard (steep) climb cannot land without
// a boostback, so it cuts on the ordinary solve and the ship stays 250 km out; the
// fitted flat climb lands on no boostback (a node of a metre or so at most), under
// the entry burn the speed rule asks for, far downrange. A typed distance stays typed.
func TestRecoveryShipAutoCore(t *testing.T) {
	for _, tc := range []struct {
		name         string
		fitted       bool
		pick         RecoverPick
		wantAuto     bool
		minKm, maxKm float64 // the ship's distance from the pad at the cutoff
		maxBoostDV   float64
	}{
		{"standard climb, ship auto", false, RecoverPick{Mode: spacecraft.RecoverShip}, true, 249, 251, 3500},
		{"fitted climb, ship auto", true, RecoverPick{Mode: spacecraft.RecoverShip}, true, 1500, 3000, 5},
		{"fitted climb, typed 250 km", true, RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: 250}, false, 249, 251, 3500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, c, moves := fhAutoClimb(t, tc.fitted, tc.pick)
			st := &c.Stages[0]
			id := st.RecoverSiteID
			if auto := st.RecoverMode == spacecraft.RecoverShipAuto; auto != tc.wantAuto {
				t.Fatalf("core recover mode %v, want auto %v", st.RecoverMode, tc.wantAuto)
			}
			s, _ := w.siteByID(id)
			r := c.Primary.RadiusMeters()
			if d := HaversineM(c.LaunchLatDeg, c.LaunchLonDeg, s.LatDeg, s.LonDeg, r) / 1e3; d < tc.minKm || d > tc.maxKm {
				t.Errorf("ship %.1f km from the pad at MECO, want %v to %v", d, tc.minKm, tc.maxKm)
			}
			t.Logf("MECO held %.0f kg, ship moved %d times", st.HeldKg, moves)
			if secs := w.Clock.SimTime.Sub(c.LiftoffSimTime).Seconds(); float64(moves) > secs/(recoveryMinSec*recoveryShipSlow) {
				t.Errorf("ship moved %d times in a %.0f s climb, want one per %.0f s at most", moves, secs, recoveryMinSec*recoveryShipSlow)
			}
			if tc.wantAuto && tc.fitted && moves < 1 {
				t.Errorf("ship never moved on the fitted climb")
			}
			// the ship stays where the cutoff left it through the drop and the landing
			n := len(w.Crafts)
			if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
				t.Fatal(err)
			}
			if len(w.PendingRecoveryEvents) != 0 {
				t.Fatalf("drop refused: %v", w.PendingRecoveryEvents)
			}
			core := w.Crafts[n]
			var boost, entry float64
			for _, nd := range core.Nodes {
				switch nd.AdvisoryKey {
				case AdvisoryKeyDeorbit:
					boost = nd.DV
				case AdvisoryKeyEntry:
					entry = nd.DV
				}
			}
			t.Logf("boostback %.1f m/s, entry burn %.0f m/s", boost, entry)
			if boost > tc.maxBoostDV {
				t.Errorf("boostback %.1f m/s, want at most %v", boost, tc.maxBoostDV)
			}
			if tc.wantAuto && tc.fitted && entry < EntryFallBurnMps-1 {
				t.Errorf("entry burn %.0f m/s, want the speed rule's %v on this fall", entry, EntryFallBurnMps)
			}
			flyDown(w, core)
			if after, _ := w.siteByID(id); after.LatDeg != s.LatDeg || after.LonDeg != s.LonDeg || after.UpdatedAtNano != s.UpdatedAtNano {
				t.Errorf("the ship moved after the cutoff: %+v -> %+v", s, after)
			}
			d := distToLZ(t, w, core, id, 1)
			t.Logf("core landed=%v %.1f m from the ship, %.0f kg left, throttle %.4f", core.Landed, d, core.ActiveStageFuel(), core.LandingThrottle())
			if !core.Landed || d > SiteLZRadiusM || core.ActiveStageFuel() < BoostbackFuelMarginKg {
				t.Errorf("core landed=%v %.1f m out with %.0f kg, want inside %v m with at least %.0f kg", core.Landed, d, core.ActiveStageFuel(), SiteLZRadiusM, BoostbackFuelMarginKg)
			}
		})
	}
}

// The speed rule (Jason, review Q5): a `ship auto` fall faster than EntryFallSpeedMps at
// the top entry height gets an entry burn of EntryFallBurnMps; not faster (the equal
// case included) gets none from the rule.
func TestEntryFallBurnRule(t *testing.T) {
	for _, tc := range []struct {
		speed float64
		want  float64
	}{{EntryFallSpeedMps - 1, 0}, {EntryFallSpeedMps, 0}, {EntryFallSpeedMps + 1, EntryFallBurnMps}, {3000, EntryFallBurnMps}} {
		if got := fallSpeedEntryFloor(tc.speed).dv; got != tc.want {
			t.Errorf("fall at %.0f m/s: rule burn %.0f, want %.0f", tc.speed, got, tc.want)
		}
	}
}

// The cost of `ship auto` on the core (the cost tests above price the Falcon 9): the
// whole fitted climb's solves and aim flights, and the slowest core tick. The flight count is capped; the tick time is logged only (CI measured 450 ms against 218 ms
// here, so a wall-clock cap flakes, as the other cost tests' ticks are only logged).
func TestRecoveryShipAutoSolvesAreCheap(t *testing.T) {
	var worst time.Duration
	w, c, moves := fhAutoClimb(t, true, RecoverPick{Mode: spacecraft.RecoverShip}, &worst)
	t.Logf("fitted auto core: %d solves, %d flights, %d ship moves, held %.0f kg, worst core tick %v", w.recoverySolves, w.aimEvals, moves, c.Stages[0].HeldKg, worst)
	if w.aimEvals > 7000 {
		t.Errorf("%d aim flights, want at most 7000 (measured 5,583)", w.aimEvals)
	}
}

// #1012: `ship auto` tries the no-boostback fall first (solveBoostback). With the ship
// standing where the fall stops, zero boostback is the answer; the ordinary solve
// aims the same stop with a small burn (0.9 m/s on the fitted climb) or none at all.
func TestShipAutoSolveBoostbackTriesZeroFirst(t *testing.T) {
	w, _, _ := fhAutoClimb(t, true, RecoverPick{Mode: spacecraft.RecoverShip})
	n := len(w.Crafts)
	if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
		t.Fatal(err)
	}
	core := w.Crafts[n]
	core.Nodes = nil
	st := core.Stages[0]
	if st.RecoverMode != spacecraft.RecoverShipAuto {
		t.Fatalf("core mode %v, want ship auto", st.RecoverMode)
	}
	lead := boostbackLead(core)
	st0, ok := physics.KeplerStep(core.State, core.Primary.GravitationalParameter(), lead)
	if !ok {
		t.Fatal("no kepler step")
	}
	st0.M = core.TotalMass()
	tS := w.Clock.SimTime.Add(time.Duration(lead * float64(time.Second)))
	site0, _ := w.siteByID(st.RecoverSiteID)
	lat, lon := site0.lzLatLon(1, core.Primary.RadiusMeters())
	_, off, err := w.solveNoBoost(core, st0, tS, [2]float64{lat, lon})
	if err != nil {
		t.Fatal(err)
	}
	// the ship stands on the fall's stop (the cutoff's own move), then 40 m off it: inside
	// the 50 m / 100 m tolerance either way, so zero still stands
	for _, offM := range []float64{0, 40} {
		dLat := offM / core.Primary.RadiusMeters() * 180 / math.Pi
		sol, err := w.solveBoostback(core, st0, tS, [2]float64{off.aimLat + dLat, off.aimLon})
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("stop %.0f m off the site: boostback %.3f m/s, entry %.0f m/s", offM, sol.dv, sol.entry.dv)
		if sol.dv != 0 {
			t.Errorf("stop %.0f m off: boostback %.3f m/s, want 0 (the no-boostback fall tried first)", offM, sol.dv)
		}
	}
}

// #1012: a `ship auto` cutoff whose last closing forecast needed three engines commits
// the stage to three engines (saved with the stage, copied to the dropped one); a
// forecast that closed on one engine leaves the landing throttle alone.
func TestCutAtReserveCommitsThreeEnginesOnlyWhenTheForecastNeededThem(t *testing.T) {
	for _, tc := range []struct {
		name      string
		autoThree bool
		want      float64
	}{{"forecast on three engines", true, ThreeEngineLandingThrottle}, {"forecast on one engine", false, 1.0 / 9}} {
		t.Run(tc.name, func(t *testing.T) {
			w := mustWorld(t)
			w.Crafts, w.ActiveCraftIdx = nil, -1
			c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true,
				Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
				Recover: []RecoverPick{{Mode: spacecraft.RecoverShip}}})
			if err != nil {
				t.Fatal(err)
			}
			w.recoveryRTFor(c).autoThree = tc.autoThree
			if got := c.Stages[0].LandingThrottle; math.Abs(got-1.0/9) > 1e-9 {
				t.Fatalf("fixture: landing throttle %.4f before the cut, want one engine of nine", got)
			}
			w.cutAtReserve(c)
			if got := c.Stages[0].LandingThrottle; math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("landing throttle %.4f after the cut, want %.4f", got, tc.want)
			}
			if c.Stages[0].HeldKg <= 0 {
				t.Errorf("held %.0f kg, want the reserve held back", c.Stages[0].HeldKg)
			}
		})
	}
}

// #1012: correctionFor caps the burn to the propellant the tank can pay (what the
// landing leaves above BoostbackFuelMarginKg). Booster 1 of the 44 km drop gets its
// correction burn planted; the same state asked again with the tank cut so the burn
// is affordable by only half gets a smaller one, and no more than the tank pays.
func TestCorrectionForCapsTheBurnToWhatTheTankPays(t *testing.T) {
	w, b := fhBoosterDrop(t)
	for i := 0; i < 20*3600 && !(b.Landed || b.Crashed); i++ {
		w.Tick()
		if hasCorrectionNode(b) {
			break
		}
	}
	if !hasCorrectionNode(b) {
		t.Fatal("setup: no correction node planted")
	}
	b.Nodes = nil
	s, _ := w.siteByID(b.Stages[0].RecoverSiteID)
	lat, lon := s.lzLatLon(1, b.Primary.RadiusMeters())
	lat, lon = w.computerSiteLatLon(b, lat, lon)
	cp, ok := w.correctionFor(b, [2]float64{lat, lon})
	if !ok || cp.dv <= 0 {
		t.Fatalf("uncapped plan ok=%v dv %.2f", ok, cp.dv)
	}
	ve := b.Isp * stdGravityMps2
	burned := func(dv float64) float64 { return b.TotalMass() * (1 - math.Exp(-dv/ve)) }
	t.Logf("uncapped: %.2f m/s, %.1f kg, fuel %.0f kg", cp.dv, burned(cp.dv), b.ActiveStageFuel())
	// take propellant out until the landing leaves margin plus only part of the burn
	full := b.Stages[0].FuelMass
	var capped correctionPlan
	for drain := 0.0; drain < full; drain += 20 {
		b.Stages[0].FuelMass = full - drain
		if c2, ok2 := w.correctionFor(b, [2]float64{lat, lon}); ok2 && c2.dv < 0.7*cp.dv {
			capped = c2
			t.Logf("tank drained %.0f kg", drain)
			break
		}
	}
	b.Stages[0].FuelMass = full
	if capped.dv == 0 {
		t.Fatal("no tank gave a capped plan")
	}
	t.Logf("capped: %.2f m/s (%.0f%% of the uncapped)", capped.dv, 100*capped.dv/cp.dv)
	if capped.dv >= cp.dv || capped.dv <= 0 {
		t.Errorf("capped burn %.2f m/s, want inside (0, %.2f)", capped.dv, cp.dv)
	}
}

// #1012: the three-engine cut re-solves RecoveryStageGraceS ahead before it commits
// (staging is the pilot's press, a plan shut a few seconds later was refused at the
// drop). The recording's bare Falcon 9 to a ship 350 km out (flownF9's climb) has a
// window at T+139.2 to 139.3 where the plan closes now but not after the 5 s coast.
// The three-engine line is forced to say "reached" at the first tick of each kind
// (recoveryCutoff called on that state, so the probe and the cut see the same fall):
// a plan that closes after the grace cuts, one that does not holds the engines.
func TestThreeEngineCutHoldsWhenThePlanShutsInsideTheGrace(t *testing.T) {
	const deg = math.Pi / 180
	for _, tc := range []struct {
		name    string
		shutsIn bool // the tick to cut on: closes now, not after the grace
		wantCut bool
	}{{"plan closes after the grace", false, true}, {"plan shuts inside the grace", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			w := mustWorld(t)
			w.Crafts, w.ActiveCraftIdx = nil, -1
			c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true,
				Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
				Recover: []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: 350}}})
			if err != nil {
				t.Fatal(err)
			}
			w.noShipFollow = true
			w.RecomputeCommGraph()
			c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
			w.StartManualBurn()
			t0 := w.Clock.SimTime
			st := &c.Stages[0]
			for i := 0; i < 60000 && c.ManualBurn != nil && st.HeldKg == 0; i++ {
				switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
				case el >= 10.5 && el < 16.2:
					c.PitchTrim, c.AttitudeMode = 1*deg, spacecraft.BurnSurfacePrograde
				case el >= 16.2 && el < 126.5:
					c.PitchTrim = 0
				case el >= 126.5 && el < 127.5:
					c.PitchTrim = 5 * deg
				case el >= 127.5:
					c.PitchTrim = 10 * deg
				}
				if rt := w.recRT[c.ID]; rt != nil && w.Clock.SimTime.Sub(t0).Seconds() > 133 {
					rt.three, rt.haveCut, rt.nextSolve = threeEngineLine{}, false, w.Clock.SimTime.Add(time.Hour) // the cutoff's own line would cut at T+136.6: keep the engines lit to the window
				}
				if w.Clock.SimTime.Sub(t0).Seconds() >= 138.9 { // from here the window is near: probe each tick
					_, _, _, e0 := w.recoverySlackAt(c, ThreeEngineLandingThrottle, st.FuelMass, 0, false, 0, 0)
					_, _, _, e5 := w.recoverySlackAt(c, ThreeEngineLandingThrottle, st.FuelMass, 0, false, RecoveryStageGraceS, 0)
					if e0 == nil && (e5 != nil) == tc.shutsIn {
						rt := w.recoveryRTFor(c)
						now := w.Clock.SimTime
						rt.nextSolve, rt.seenFeasible, rt.haveCut = now.Add(time.Hour), false, false
						rt.three = threeEngineLine{have: true, at: now, slack: 0, rate: 1}
						w.recoveryCutoff(c, w.Clock.BaseStep)
						t.Logf("T+%.2f, %.0f kg: held %.0f kg", now.Sub(t0).Seconds(), st.FuelMass, st.HeldKg)
						if cut := st.HeldKg > 0; cut != tc.wantCut {
							t.Errorf("cut=%v, want %v (plan closes now, after the grace: %v)", cut, tc.wantCut, e5 == nil)
						}
						return
					}
				}
				w.Tick()
			}
			t.Fatalf("no tick with the plan closing now and (after the grace shut=%v) before the stage cut or burned dry", tc.shutsIn)
		})
	}
}
