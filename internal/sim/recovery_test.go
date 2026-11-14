package sim

// One-line reason for a new file: recovery.go is a new production file (the pad's
// recover: choice, the drone ship, the flight home at the drop, the cutoff).

import (
	"fmt"
	"math"
	"testing"
	"time"

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
	if brg := bearingDeg(DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, s.LatDeg, s.LonDeg); math.Abs(brg-90) > 0.5 {
		t.Errorf("ship bears %.2f deg from the pad, want 090 (the launch heading)", brg)
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

// bearingDeg is the initial great-circle bearing from point 1 to point 2.
func bearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	p1, p2, dl := lat1*rad, lat2*rad, (lon2-lon1)*rad
	y := math.Sin(dl) * math.Cos(p2)
	x := math.Cos(p1)*math.Sin(p2) - math.Sin(p1)*math.Cos(p2)*math.Cos(dl)
	return math.Mod(math.Atan2(y, x)/rad+360, 360)
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
// none near (Baikonur) it is dropped to none; a ship pick with no distance is none.
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
	if baikonur.Stages[0].RecoverMode != spacecraft.RecoverNone || noKm.Stages[0].RecoverMode != spacecraft.RecoverNone {
		t.Errorf("Baikonur %v, ship with no km %v, want none", baikonur.Stages[0].RecoverMode, noKm.Stages[0].RecoverMode)
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
		{"ship300", ship(300), 0, one}, {"ship350", ship(350), 0, max}, {"ship350+16s", ship(350), 16, one},
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
func climbTimed(t testing.TB, loadout string, picks []RecoverPick, warpIdx int) (st climbStats, w *World, c *spacecraft.Spacecraft) {
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
// (50,658 and 43,369 kg) and 10x within 5 t (1.2 % of the 411 t tank) either way,
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
		{"lz 1x", lz, 0, 1300, 50658, true}, {"ship 1x", ship, 0, 3400, 43369, true},
		{"lz 10x", lz, 1, 700, 50658, false}, {"ship 10x", ship, 1, 3400, 43369, false},
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
// ships 90 and 150 are the cases that lost the booster or landed on 0.2 to 1 t.
func TestRecoveryTenXShipCutoffLandsWithMargin(t *testing.T) {
	for _, tc := range []struct {
		load string
		km   float64
	}{{"Falcon-9-Dragon", 75}, {"Falcon-9", 90}, {"Falcon-9", 150}} {
		for _, warp := range []int{0, 1} {
			st, w, c := climbTimed(t, tc.load, []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: tc.km}}, warp)
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
			b := w.Crafts[n]
			w.Clock.WarpIdx = 0
			flyDown(w, b)
			if d := distToLZ(t, w, b, id, 1); !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() < BoostbackFuelMarginKg {
				t.Errorf("%s: landed=%v %.1f m out with %.0f kg, want inside %v m with at least %.0f kg", name, b.Landed, d, b.ActiveStageFuel(), SiteLZRadiusM, BoostbackFuelMarginKg)
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

// A flat climb (the pitch kick a little early, MECO at 36 to 52 km instead of 52+)
// that cuts off must cut at a reserve the drop accepts. Falcon 9 + Dragon, ship 250 km,
// 1x, Jason's flown profile (kick 1 degree, surface prograde 2 s later, trim 0, then
// -1..-10 degrees over T+111..124). Before: the solve that closed at T+127 was not the
// one at T+131 (closing is not smooth in the stage's state), the "closed before, none
// now" fallback cut on the failed solve and the drop refused the same state (8.25, 8.5,
// 8.75). Kicks 8.0 and 8.5 are accepted at the drop but fly 1.7 to 13 km short with the
// tank dry: the landing burn takes more than the forecast says, a gap this cutoff does
// not own, so only the 8.25, 8.75 and 9.0 kicks are held to the landing.
func TestRecoveryFlatClimbCutoffNeverLeavesARefusedDrop(t *testing.T) {
	deg := math.Pi / 180
	for _, tc := range []struct {
		kick float64
		land bool
	}{{8.0, false}, {8.25, true}, {8.5, false}, {8.75, true}, {9.0, true}} {
		w := mustWorld(t)
		w.Crafts, w.ActiveCraftIdx = nil, -1
		c, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9-Dragon", ParentBodyID: "earth", Launchpad: true,
			Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast,
			Recover: []RecoverPick{{Mode: spacecraft.RecoverShip, ShipKm: RecoveryShipDefaultKm}}})
		if err != nil {
			t.Fatal(err)
		}
		w.RecomputeCommGraph()
		c.Throttle, c.EngineMode, c.AttitudeMode = 1, spacecraft.EngineMain, spacecraft.BurnRadialOut
		w.StartManualBurn()
		t0 := w.Clock.SimTime
		for i := 0; i < 60000 && c.Stages[0].HeldKg == 0 && c.ManualBurn != nil; i++ {
			switch el := w.Clock.SimTime.Sub(t0).Seconds(); {
			case el >= tc.kick && el < tc.kick+2:
				c.PitchTrim = deg
			case el >= tc.kick+2 && c.AttitudeMode == spacecraft.BurnRadialOut:
				c.AttitudeMode = spacecraft.BurnSurfacePrograde
			case el >= tc.kick+5 && el < 111:
				c.PitchTrim = 0
			case el >= 111 && el < 124:
				c.PitchTrim = -math.Ceil((el-111)/1.3+0.01) * deg
			case el >= 124:
				c.PitchTrim = -10 * deg
			}
			w.Tick()
		}
		w.Tick()
		name := fmt.Sprintf("kick %.2f", tc.kick)
		held, id := c.Stages[0].HeldKg, c.Stages[0].RecoverSiteID
		if held <= 0 {
			t.Errorf("%s: no cutoff", name)
			continue
		}
		n := len(w.Crafts)
		if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
			t.Fatal(err)
		}
		if len(w.PendingRecoveryEvents) != 0 {
			t.Errorf("%s: cut with %.0f kg held and the drop refused: %v", name, held, w.PendingRecoveryEvents)
			continue
		}
		if !tc.land {
			continue
		}
		b := w.Crafts[n]
		flyDown(w, b)
		if d := distToLZ(t, w, b, id, 1); !b.Landed || d > SiteLZRadiusM || b.ActiveStageFuel() < BoostbackFuelMarginKg {
			t.Errorf("%s: landed=%v %.1f m out with %.0f kg, want inside %v m with at least %.0f kg", name, b.Landed, d, b.ActiveStageFuel(), SiteLZRadiusM, BoostbackFuelMarginKg)
		}
	}
}
