package sim

import (
	"errors"
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const lzTestRadius = 1737400.0 // the Moon; the layout is radius-independent to the metre

// ADR 0068 guard 5: the honeycomb's nine centres are numbered as decision 3
// says and no two are closer than 100 m (nearest neighbours exactly 100).
func TestHoneycombCentresNumberedAndNoCloserThan100m(t *testing.T) {
	for _, lat := range []float64{0, 40, 89.5, 90, -90} { // the poles take the 0 degree meridian
		s := Site{LatDeg: lat, LonDeg: 25, LZCount: 9}
		var la, lo [10]float64
		for n := 1; n <= 9; n++ {
			la[n], lo[n] = s.lzLatLon(n, lzTestRadius)
		}
		minD, near := math.Inf(1), 0
		for a := 1; a <= 9; a++ {
			for b := a + 1; b <= 9; b++ {
				d := HaversineM(la[a], lo[a], la[b], lo[b], lzTestRadius)
				minD = math.Min(minD, d)
				if math.Abs(d-SiteLZSpacingM) < 1e-4 {
					near++
				}
			}
		}
		if minD < SiteLZSpacingM-1e-4 || near != 16 { // 6 ring spokes + 6 ring sides + 4 of the second ring
			t.Errorf("lat %v: closest pair %.9f m, %d pairs at 100 m (want >= 100 and 16)", lat, minD, near)
		}
		for n, want := range map[int]float64{2: 100, 3: 100, 7: 100, 8: 200, 9: math.Sqrt(3) * 100} {
			if d := HaversineM(la[1], lo[1], la[n], lo[n], lzTestRadius); math.Abs(d-want) > 1e-4 {
				t.Errorf("lat %v: LZ-%d is %.6f m from LZ-1, want %.6f", lat, n, d, want)
			}
		}
		if lat == 40 { // away from the poles the bearings are true
			for n, want := range map[int]float64{2: 0, 3: 60, 4: 120, 5: 180, 6: 240, 7: 300, 8: 0, 9: 30} {
				got := ForwardAzimuthDeg(la[1], lo[1], la[n], lo[n])
				if math.Abs(math.Remainder(got-want, 360)) > 0.01 {
					t.Errorf("LZ-%d bearing %.3f, want %.0f", n, got, want)
				}
			}
			if far := HaversineM(la[1], lo[1], la[8], lo[8], lzTestRadius) + SiteLZRadiusM; far > 243.1 {
				t.Errorf("a full site reaches %.1f m, ADR says inside 243", far)
			}
		}
	}
}

// ADR 0068 decision 9 / 9b and guard 7's half: the four shipped zones sit on
// real bodies of their systems, LC-13 keeps the real pair's 310 m northwest, the
// others the 100 m honeycomb, Kern's is 1 km from the default pad.
func TestShippedZonesPlacesAndSizes(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	radius := func(s Site) float64 {
		if r := w.siteRadiusM(s); r > 0 {
			return r
		}
		t.Fatalf("%s: no body %s in system %s", s.Name, s.Body, s.System)
		return 0
	}
	for _, s := range shippedZones {
		r := radius(s)
		la, lo := s.lzLatLon(2, r)
		d := HaversineM(s.LatDeg, s.LonDeg, la, lo, r)
		if s.Name == "LC-13" {
			if math.Abs(d-310) > 0.01 || math.Abs(ForwardAzimuthDeg(s.LatDeg, s.LonDeg, la, lo)-315) > 0.01 {
				t.Errorf("LC-13 LZ-2 is %.3f m at %.3f deg, want 310 m at 315", d, ForwardAzimuthDeg(s.LatDeg, s.LonDeg, la, lo))
			}
			pad := HaversineM(s.LatDeg, s.LonDeg, DefaultLaunchpadLatitude, DefaultLaunchpadLongitudeEast, r)
			t.Logf("LC-13 LZ-1 is %.0f m from the default pad", pad) // the ADR says 14.8 km
			if pad < 14000 || pad > 15500 {
				t.Errorf("LC-13 is %.0f m from the default pad", pad)
			}
		} else if math.Abs(d-SiteLZSpacingM) > 1e-4 {
			t.Errorf("%s LZ-2 is %.6f m from LZ-1, want 100", s.Name, d)
		}
		if s.Name == "Kern LZ" {
			if pad := HaversineM(s.LatDeg, s.LonDeg, 0, 0, r); math.Abs(pad-1000) > 1 {
				t.Errorf("Kern LZ is %.2f m from the pad at 0N 0E, want 1000", pad)
			}
		}
		if s.LZs() != 2 || s.Kind != SiteZone {
			t.Errorf("%s: %d LZs, kind %v", s.Name, s.LZs(), s.Kind)
		}
	}
	if len(w.Sites) != 0 {
		t.Errorf("a new world holds %d sites; the zones are never in w.Sites (so never saved or published)", len(w.Sites))
	}
	if w.RenameSite(shippedZones[0].ID, "x") || w.RemoveSite(shippedZones[0].ID) {
		t.Error("a shipped zone was renamed or deleted")
	}
}

// moonLZWorld is the deorbit fixture with its flag a two-LZ honeycomb site and
// a second vessel to stand on it.
func moonLZWorld(t *testing.T) (w *World, c, other *spacecraft.Spacecraft) {
	t.Helper()
	w, c = deorbitWorld(t, 150, 0)
	w.Sites[0].LZCount = 2
	o, err := w.SpawnCraft(SpawnSpec{Alongside: true})
	if err != nil {
		t.Fatal(err)
	}
	o.Primary, o.Name = c.Primary, "Other"
	w.ActiveCraftIdx = 0
	for i, v := range w.Crafts {
		if v == c {
			w.ActiveCraftIdx = i
		}
	}
	w.SetTargetSite(w.Sites[0].ID)
	return w, c, o
}

func standOn(o *spacecraft.Spacecraft, lat, lon float64) {
	o.Landed, o.LandedLatDeg, o.LandedLonDeg = true, lat, lon
}

// Guard 1: two N presses in one tick get two different LZs, and a third is
// told there is none free. The claim is the press, not the touchdown.
func TestTwoPressesInOneTickGetDifferentLZs(t *testing.T) {
	w, c, o := moonLZWorld(t)
	if fresh, err := w.claimLZ(o); err != nil || fresh { // o aims at nothing: no claim, no refusal
		t.Fatalf("a vessel with no site aim: fresh=%v err=%v", fresh, err)
	}
	o.Target = c.Target
	if _, err := w.claimLZ(c); err != nil {
		t.Fatal(err)
	}
	if _, err := w.claimLZ(o); err != nil {
		t.Fatal(err)
	}
	if c.LZClaim.N != 1 || o.LZClaim.N != 2 {
		t.Fatalf("claims = %d and %d, want 1 and 2", c.LZClaim.N, o.LZClaim.N)
	}
	third := *c
	third.ID, third.LZClaim = 999, spacecraft.LZClaim{}
	w.Crafts = append(w.Crafts, &third)
	third.Target = c.Target
	_, err := w.claimLZ(&third)
	var nf *NoFreeLZError
	if !errors.As(err, &nf) || err.Error() != w.Sites[0].Name+": no free LZ" {
		t.Errorf("third press: %v, want %q", err, w.Sites[0].Name+": no free LZ")
	}
	if third.LZClaim.N != 0 {
		t.Error("a refused press left a claim")
	}
}

// N is refused with nothing planted when the only LZ is taken, and plants
// nothing; B the same. The claim rides the real key paths too.
func TestNAndBClaimOrRefuseAtTheSite(t *testing.T) {
	w, c, o := moonLZWorld(t)
	w.Sites[0].LZCount = 0 // a plain flag: one LZ
	standOn(o, w.Sites[0].LatDeg, w.Sites[0].LonDeg)
	nodes := len(c.Nodes)
	if _, err := w.PlanDeorbitAndEngage(); err == nil || err.Error() != w.Sites[0].Name+": no free LZ" {
		t.Errorf("N on a taken flag: %v", err)
	}
	if _, err := w.PlanDeorbitOnly(); err == nil || err.Error() != w.Sites[0].Name+": no free LZ" {
		t.Errorf("B on a taken flag: %v", err)
	}
	if len(c.Nodes) != nodes || c.Landing.Owns() {
		t.Error("a refused press planted a node or engaged")
	}
	o.Landed = false // lift off: the LZ is free
	if _, err := w.PlanDeorbitAndEngage(); err != nil || c.LZClaim.N != 1 {
		t.Errorf("N after liftoff: err %v claim %d", err, c.LZClaim.N)
	}
	w.DisengageLandingAutopilot(c)
	if c.LZClaim.N != 0 {
		t.Error("Site+ off kept the claim")
	}
	if _, err := w.PlanDeorbitOnly(); err != nil || c.LZClaim.N != 1 {
		t.Errorf("B: err %v claim %d", err, c.LZClaim.N)
	}
	w.SetTargetSite(0) // unknown id clears the aim
	if c.LZClaim.N != 0 {
		t.Error("a retarget kept the claim")
	}
}

// Guard 8: every reader of a site target resolves the same LZ. Move LZ-2 (flip
// the layout) and the autopilot's aim, the deorbit planner's site, miss:'s
// ground point, the target state and the name all move together.
func TestEveryReaderResolvesTheClaimedLZ(t *testing.T) {
	w, c, o := moonLZWorld(t)
	s := w.Sites[0]
	standOn(o, s.LatDeg, s.LonDeg) // LZ-1 is taken: N gets LZ-2
	if _, err := w.PlanDeorbitAndEngage(); err != nil {
		t.Fatal(err)
	}
	if c.LZClaim.N != 2 {
		t.Fatalf("claim = %d, want 2", c.LZClaim.N)
	}
	check := func(label string) (lat, lon float64) {
		wantLat, wantLon := w.Sites[0].lzLatLon(2, c.Primary.RadiusMeters())
		apLat, apLon, _, _ := w.apTarget(c)
		gLat, gLon, _ := w.surfaceTargetLatLon(c)
		dLat, dLon, _, _ := w.deorbitSite(c)
		pts := map[string][2]float64{"autopilot": {apLat, apLon}, "miss/ground": {gLat, gLon}, "deorbit": {dLat, dLon}}
		ts, _ := w.targetSite()
		pts["target"] = [2]float64{ts.LatDeg, ts.LonDeg}
		for who, p := range pts {
			if HaversineM(p[0], p[1], wantLat, wantLon, c.Primary.RadiusMeters()) > 1e-3 {
				t.Errorf("%s: %s reads %v, LZ-2 is at %v,%v", label, who, p, wantLat, wantLon)
			}
		}
		if want := w.Sites[0].Name + " LZ-2"; w.TargetName() != want {
			t.Errorf("%s: TARGET reads %q, want %q", label, w.TargetName(), want)
		}
		return wantLat, wantLon
	}
	aLat, aLon := check("honeycomb")
	w.Sites[0].Layout = LayoutLC13 // LZ-2 moves 210 m
	bLat, bLon := check("moved")
	if HaversineM(aLat, aLon, bLat, bLon, c.Primary.RadiusMeters()) < 100 {
		t.Error("setup: LZ-2 did not move")
	}
}

// Guard 2: a touchdown at exactly 43 m plants no flag, at 43 m plus 1 cm plants
// one. The boundary is found on the float line, not assumed.
func TestTouchdownAtTheCircleEdgePlantsNoFlagOneCmBeyondDoes(t *testing.T) {
	arrive := func(lat float64) (planted int) {
		w, _, _ := siteWorld(t) // a flag at 12.5, -33 with a lander on it
		v, err := w.SpawnCraft(SpawnSpec{Alongside: true})
		if err != nil {
			t.Fatal(err)
		}
		v.Primary = w.Crafts[0].Primary
		before := len(w.Sites)
		w.applySurfaceArrival(v, physics.StateVector{R: v.State.R, V: v.State.V, M: v.State.M}, outcomeLanded, lat, -33)
		return len(w.Sites) - before
	}
	// The ADR's 43 m, written out so a changed constant is seen here.
	const edge = 43.0
	r := lzTestRadius
	// A distance that lands exactly on 43.0 is not reachable through lat/lon
	// floats (the grid is ~5e-11 m), so the equal case is pinned on the test itself.
	if !insideLZ(edge) || insideLZ(math.Nextafter(edge, 44)) || insideLZ(edge+0.01) {
		t.Error("insideLZ: exactly 43 m must be inside, the next float and 43 m + 1 cm outside")
	}
	if n := arrive(12.5 + (edge-0.001)/r/rad); n != 0 {
		t.Errorf("touchdown 1 mm inside planted %d flags, want 0", n)
	}
	if n := arrive(12.5 + (edge+0.01)/r/rad); n != 1 {
		t.Errorf("43 m + 1 cm planted %d flags, want 1", n)
	}
}

// The flash names the LZ a hand landing stood down in, and a touchdown spends
// the claim.
func TestHandLandingInsideAnLZFlashesItAndSpendsTheClaim(t *testing.T) {
	w, c, o := moonLZWorld(t)
	s := w.Sites[0]
	standOn(o, s.LatDeg, s.LonDeg)
	if _, err := w.claimLZ(c); err != nil || c.LZClaim.N != 2 {
		t.Fatalf("claim: %v %d", err, c.LZClaim.N)
	}
	lat, lon := s.lzLatLon(2, c.Primary.RadiusMeters())
	sites := len(w.Sites)
	w.applySurfaceArrival(c, physics.StateVector{R: c.State.R, V: c.State.V, M: c.State.M}, outcomeLanded, lat, lon)
	if len(w.Sites) != sites || c.LZClaim.N != 0 {
		t.Errorf("sites %d->%d, claim %d: want no flag and the claim spent", sites, len(w.Sites), c.LZClaim.N)
	}
	ev := w.PendingLandingEvents
	if len(ev) != 1 || ev[0].SiteName != s.Name+" LZ-2" || ev[0].DistanceM > 1 {
		t.Errorf("flash events = %+v", ev)
	}
}

// The autopilot's flash names the LZ it was claimed, even when a lower LZ was
// freed while it flew (here the LZ-1 claimant was taken over by hand): the
// claim must outlive touchdown until landingEnded reads it.
func TestAutopilotFlashNamesTheClaimedLZAfterALowerOneFrees(t *testing.T) {
	w, c, o := moonLZWorld(t)
	s := w.Sites[0]
	o.Target = c.Target
	if _, err := w.claimLZ(o); err != nil || o.LZClaim.N != 1 {
		t.Fatalf("o claim: %v %d", err, o.LZClaim.N)
	}
	if _, err := w.claimLZ(c); err != nil || c.LZClaim.N != 2 {
		t.Fatalf("c claim: %v %d", err, c.LZClaim.N)
	}
	o.LZClaim = spacecraft.LZClaim{} // o's pilot took it by hand
	if c.Landing == nil {
		c.Landing = &spacecraft.LandingAutopilot{}
	}
	c.Landing.Engaged, c.Landing.Nose = true, false
	lat, lon := s.lzLatLon(2, c.Primary.RadiusMeters())
	w.applySurfaceArrival(c, physics.StateVector{R: c.State.R, V: c.State.V, M: c.State.M}, outcomeLanded, lat, lon)
	w.tickLandingAutopilot(c)
	ev := w.PendingLandingEvents
	if len(ev) != 1 || ev[0].SiteName != s.Name+" LZ-2" || ev[0].DistanceM > 1 {
		t.Errorf("flash events = %+v, want one naming LZ-2 at under 1 m", ev)
	}
	if c.LZClaim.N != 0 {
		t.Errorf("claim %d after the flash, want spent", c.LZClaim.N)
	}
}

// Decision 5's occupancy: a crashed vessel holds its LZ until cleared; a partner
// of the same site is not asked (S5).
func TestACrashedVesselHoldsItsLZ(t *testing.T) {
	w, c, o := moonLZWorld(t)
	s := w.Sites[0]
	o.Crashed, o.LandedLatDeg, o.LandedLonDeg = true, s.LatDeg, s.LonDeg
	if n := w.lowestFreeLZ(s, c); n != 2 {
		t.Errorf("lowest free with a wreck on LZ-1 = %d, want 2", n)
	}
	if n := w.lowestFreeLZ(s, o); n != 1 {
		t.Errorf("the wreck itself reads %d, want its own LZ-1 free", n)
	}
}

// ADR 0068 decision 8, T8b: the map draws the circles when 86 m spans four
// cells or more, 21.5 m a cell or finer. The equal case (exactly four cells)
// draws; the next float down does not.
func TestLZCirclesOnMapThreshold(t *testing.T) {
	s := LZMapMinCells / SiteLZRadiusM
	for !LZCirclesOnMap(s) {
		s = math.Nextafter(s, math.Inf(1))
	}
	for LZCirclesOnMap(math.Nextafter(s, 0)) {
		s = math.Nextafter(s, 0)
	}
	if !LZCirclesOnMap(s) || LZCirclesOnMap(math.Nextafter(s, 0)) {
		t.Fatalf("threshold edge not found at %v", s)
	}
	if got := 2 * SiteLZRadiusM * s / 2; got != LZMapMinCells {
		t.Errorf("the edge is at %v cells, not exactly %v: the equal case is not under test", got, LZMapMinCells)
	}
	if mPerCell := 2 / s; math.Abs(mPerCell-21.5) > 1e-9 {
		t.Errorf("threshold is %.9f m a cell, want 21.5", mPerCell)
	}
}

// Decision 8's picture needs the LZ centres in the world and your standing
// vessels on them: LZ-1 and LZ-2 100 m apart on the ground, up unit, a landed
// vessel inside LZ-2 listed on 2, the active vessel, a flyer and a landed
// vessel just outside the circle (the equal case, 1 cm over) left out.
func TestLZPlacesAndStandingOn(t *testing.T) {
	w, c, o := moonLZWorld(t)
	s := w.Sites[0]
	ps := w.LZPlaces(s)
	if len(ps) != 2 {
		t.Fatalf("%d places for a two-LZ site", len(ps))
	}
	if d := ps[0].Pos.Sub(ps[1].Pos).Norm(); math.Abs(d-SiteLZSpacingM) > 0.01 {
		t.Errorf("LZ-1 to LZ-2 %.4f m in the world frame, want 100", d)
	}
	if math.Abs(ps[1].Up.Norm()-1) > 1e-12 {
		t.Errorf("Up is not a unit vector: %v", ps[1].Up)
	}
	r := w.siteRadiusM(s)
	la, lo := s.lzLatLon(2, r)
	standOn(o, la, lo)
	got := w.StandingOn(s)
	if len(got) != 1 || got[0].LZ != 2 || got[0].Pos.Sub(ps[1].Pos).Norm() > 0.01 {
		t.Fatalf("standing on LZ-2: %+v", got)
	}
	standOn(c, s.LatDeg, s.LonDeg) // the active vessel is never listed
	o.Landed = false
	if got := w.StandingOn(s); len(got) != 0 {
		t.Errorf("a flyer or the active vessel listed: %+v", got)
	}
	// 1 cm outside LZ-2's circle along the meridian is not standing on it.
	standOn(o, la+(SiteLZRadiusM+0.01)/r/rad, lo)
	if got := w.StandingOn(s); len(got) != 0 {
		t.Errorf("a vessel 43.01 m from LZ-2 listed: %+v", got)
	}
	// A partner's parked lander on LZ-2 is listed with no colour (drawn dim);
	// one still flying is not.
	g := Ghost{Owner: "ansi", CraftID: 9, PrimaryID: c.Primary.ID, Landed: true, LatDeg: la, LonDeg: lo}
	w.Ghosts = []Ghost{{Owner: "ansi", CraftID: 8, PrimaryID: c.Primary.ID, LatDeg: la, LonDeg: lo}, g}
	got = w.StandingOn(s)
	if len(got) != 1 || got[0].LZ != 2 || got[0].Color != "" || got[0].Pos.Sub(ps[1].Pos).Norm() > 0.01 {
		t.Errorf("a partner's parked lander on LZ-2: %+v", got)
	}
}

// ADR 0068 decision 6 (S4): a chosen LZ moves every reader together, N claims
// it taken or not.
func TestChosenLZMovesEveryReaderAndNClaimsItTaken(t *testing.T) {
	w, c, o := moonLZWorld(t)
	s := w.Sites[0]
	w.SetTargetSiteLZ(s.ID, 2) // nobody on LZ-1: auto would pick it
	rad := c.Primary.RadiusMeters()
	wantLat, wantLon := s.lzLatLon(2, rad)
	apLat, apLon, _, _ := w.apTarget(c)
	gLat, gLon, _ := w.surfaceTargetLatLon(c)
	dLat, dLon, _, _ := w.deorbitSite(c)
	ts, _ := w.targetSite()
	_, n, _ := w.ActiveLandingLZ()
	for who, p := range map[string][2]float64{"autopilot": {apLat, apLon}, "miss/ground": {gLat, gLon}, "deorbit": {dLat, dLon}, "target": {ts.LatDeg, ts.LonDeg}} {
		if HaversineM(p[0], p[1], wantLat, wantLon, rad) > 1e-3 {
			t.Errorf("%s reads %v, chosen LZ-2 is at %v,%v", who, p, wantLat, wantLon)
		}
	}
	if w.TargetName() != s.Name+" LZ-2" || n != 2 {
		t.Errorf("TARGET %q, flag LZ %d, want LZ-2", w.TargetName(), n)
	}
	// N with LZ-2 chosen while a vessel stands on it: claims LZ-2, no refusal.
	lat2, lon2 := s.lzLatLon(2, rad)
	standOn(o, lat2, lon2)
	if _, err := w.PlanDeorbitAndEngage(); err != nil || c.LZClaim.N != 2 {
		t.Errorf("N on a taken chosen LZ: err %v claim %d, want claim 2", err, c.LZClaim.N)
	}
	// Choosing another LZ drops the claim; the new choice rules.
	w.SetTargetSiteLZ(s.ID, 1)
	if c.LZClaim.N != 0 {
		t.Error("re-choosing kept the old claim")
	}
	if _, n, _ := w.ActiveLandingLZ(); n != 1 {
		t.Errorf("LZ-1 chosen, flying to %d", n)
	}
	// Auto again: today's rule (lowest free).
	w.SetTargetSiteLZ(s.ID, 0)
	if _, n, _ := w.ActiveLandingLZ(); n != 1 {
		t.Errorf("auto flies to LZ-%d, want lowest free 1", n)
	}
}

// ADR 0068 decision 11, guard 9: a partner's landed ghost occupies an LZ, a
// flying one does not; the 43 m edge (equal case pinned on insideLZ above); a ghost at a same-id partner
// site elsewhere does not cross over; 0N 0E is a position when Landed.
func TestPartnerGhostsOccupyOnlyWhenLanded(t *testing.T) {
	w, c, _ := moonLZWorld(t)
	s := w.Sites[0]
	r := w.siteRadiusM(s)
	prim := c.Primary.ID
	ghost := func(landed bool, lat, lon float64) Ghost {
		return Ghost{Owner: "ansi", CraftID: 9, PrimaryID: prim, Landed: landed, LatDeg: lat, LonDeg: lon}
	}
	free := func(want int, why string) {
		t.Helper()
		if n := w.lowestFreeLZ(s, c); n != want {
			t.Errorf("%s: lowest free = %d, want %d", why, n, want)
		}
	}
	free(1, "no ghosts")
	w.Ghosts = []Ghost{ghost(false, s.LatDeg, s.LonDeg)} // flying, same coordinates
	free(1, "a flying ghost")
	w.Ghosts = []Ghost{ghost(true, s.LatDeg, s.LonDeg)}
	free(2, "a landed ghost on LZ-1")
	// exactly 43 m north is inside (the equal case), 43 m + 1 cm is not.
	edge := s.LatDeg + SiteLZRadiusM/r/rad
	for HaversineM(edge, s.LonDeg, s.LatDeg, s.LonDeg, r) > SiteLZRadiusM {
		edge = math.Nextafter(edge, s.LatDeg) // rounding put it a hair out
	}
	if d := HaversineM(edge, s.LonDeg, s.LatDeg, s.LonDeg, r); SiteLZRadiusM-d > 1e-6 {
		t.Fatalf("edge fixture %.9f m from LZ-1, want 43", d)
	}
	w.Ghosts = []Ghost{ghost(true, edge, s.LonDeg)}
	free(2, "a landed ghost at the 43 m edge")
	w.Ghosts = []Ghost{ghost(true, s.LatDeg+(SiteLZRadiusM+0.01)/r/rad, s.LonDeg)}
	free(1, "a landed ghost 43.01 m out")
	// the same coordinates on another body of the system is not this body.
	for _, b := range w.System().Bodies {
		if b.ID != prim {
			g := ghost(true, s.LatDeg, s.LonDeg)
			g.PrimaryID = b.ID
			w.Ghosts = []Ghost{g}
			break
		}
	}
	free(1, "a landed ghost on another body")
	w.Ghosts = []Ghost{ghost(true, s.LatDeg, s.LonDeg)}
	if n := w.lowestFreeLZ(Site{ID: 78, Body: s.Body, System: "Elsewhere", LatDeg: s.LatDeg, LonDeg: s.LonDeg}, c); n != 1 {
		t.Errorf("a same-named body in another system: lowest free %d, want 1", n)
	}
	// a same-id partner site on the same body, elsewhere: its parked lander is not on ours.
	w.PartnerSites = []PartnerSite{{Site: Site{ID: s.ID, Owner: "ansi", Body: s.Body, System: s.System, LatDeg: s.LatDeg + 1, LonDeg: s.LonDeg}}}
	w.Ghosts = []Ghost{ghost(true, s.LatDeg+1, s.LonDeg)}
	free(1, "a ghost at a same-id partner site")
	// 0N 0E: a landed ghost there occupies a site at 0N 0E; a flying one does not.
	z := Site{ID: 77, Body: s.Body, System: s.System, LatDeg: 0, LonDeg: 0, LZCount: 2}
	w.Ghosts = []Ghost{ghost(false, 0, 0)}
	if n := w.lowestFreeLZ(z, c); n != 1 {
		t.Errorf("flying ghost at 0N 0E: lowest free %d, want 1", n)
	}
	w.Ghosts = []Ghost{ghost(true, 0, 0)}
	if n := w.lowestFreeLZ(z, c); n != 2 {
		t.Errorf("landed ghost at 0N 0E: lowest free %d, want 2", n)
	}
}

// ADR 0069 decision 16 (T15): a booster nobody flew by N that lands outside, or
// crashes, while the pilot is on another vessel raises the same Event Flash as an
// unattended lander, with the true distance from the site it was aimed at. The
// active vessel (the pilot watched it) raises none; the equal case is the
// vessel on screen.
func TestUnflownBoosterLandingOrCrashFlashesWithTheDistance(t *testing.T) {
	for _, tc := range []struct {
		name   string
		fuelKg float64
		active bool
		crash  bool
		want   int
	}{
		{"crash in the background", 500, false, true, 1},
		{"crash on screen", 500, true, true, 0},
	} {
		w, c, _ := fallWorld(t, spacecraft.StageModuleF9S1ID, boosterFall{altM: 10e3, downMps: -300, offE: 5e3, driftE: 100, fuelKg: tc.fuelKg})
		if !tc.active {
			core, err := w.SpawnCraft(SpawnSpec{LoadoutID: "Falcon-9", ParentBodyID: "earth", Launchpad: true,
				Latitude: DefaultLaunchpadLatitude, LongitudeOffset: DefaultLaunchpadLongitudeEast})
			if err != nil {
				t.Fatal(err)
			}
			w.SetActiveCraftIdx(w.craftIndex(core))
		}
		w.PendingLandingEvents = nil
		for i := 0; i < 4*3600*20 && !c.Landed && !c.Crashed; i++ {
			w.Tick()
		}
		if c.Crashed != tc.crash || len(w.PendingLandingEvents) != tc.want {
			t.Fatalf("%s: crashed=%v events %+v, want %d", tc.name, c.Crashed, w.PendingLandingEvents, tc.want)
		}
		if tc.want == 1 {
			e := w.PendingLandingEvents[0]
			if e.CraftName != c.Name || !e.Crashed || e.SiteName != "LC-13 LZ-1" || e.DistanceM < 1000 {
				t.Errorf("%s: event %+v, want %s crashed km from LC-13 LZ-1", tc.name, e, c.Name)
			}
		}
	}
}
