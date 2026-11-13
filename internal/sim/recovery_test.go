package sim

// One-line reason for a new file: recovery.go is a new production file (the pad's
// recover: choice, the drone ship, the flight home at the drop, the cutoff).

import (
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
	return w, c
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
	if id2 := w.stationShip(c, DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, 100); id2 == id {
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
	if _, ok := w.RecoveryLZNear("earth", DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast); !ok {
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
		if _, err := w.recoverySlack(c, c.Stages[0].FuelMass); err != nil {
			b.Fatal(err)
		}
	}
	_ = time.Second
}
