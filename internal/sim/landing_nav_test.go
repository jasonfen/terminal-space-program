package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// One-line reason for a new file: landing_nav.go is a new prod file (the
// computer's-site error and the aim offset, ADR 0067 decisions 3, 4, 6).

// apolloLander is the deorbit fixture with its error switched on, an Apollo-era
// crewed command part (2 km / 1 km with no link) and no antenna: a crewed
// vessel with no COMMS link, which the old CanCommandCraft bypass called linked.
func apolloLander(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, c := deorbitWorld(t, 150, 0)
	w.navErrorOff = false
	c.LoadoutID = "" // a custom stack: the era comes from its own stages, not a catalog run
	for i := range c.Stages {
		c.Stages[i].AntennaKind, c.Stages[i].AntennaRangeM = spacecraft.AntennaNone, 0
	}
	top := &c.Stages[len(c.Stages)-1]
	top.CommandSource = spacecraft.CommandCrewed
	top.NavSigmaAlongM, top.NavSigmaAcrossM = NavSigmaApolloNoLinkAlongM, NavSigmaApolloNoLinkAcrossM
	c.SyncFields()
	w.RecomputeCommGraph()
	return w, c
}

func linkUp(w *World, c *spacecraft.Spacecraft, up bool) {
	w.CommGraph = &CommGraph{Connected: map[uint64]bool{c.ID: up}}
}

// rmsDraw is the rms (along, across) of n forced draws at consecutive ticks.
func rmsDraw(w *World, c *spacecraft.Spacecraft, n int) (along, across float64) {
	for i := 0; i < n; i++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(time.Second)
		w.drawNavError(c, true)
		along += c.Landing.NavAlongM * c.Landing.NavAlongM
		across += c.Landing.NavAcrossM * c.Landing.NavAcrossM
	}
	return math.Sqrt(along / float64(n)), math.Sqrt(across / float64(n))
}

// Guard 6 and P21: the draw's spread is the era's with no link and modern with
// the vessel's own link (a crewed vessel with no link is NOT linked), and a
// vessel with no nav_sigma_m is modern either way.
func TestNavErrorSpreadByEraAndLink(t *testing.T) {
	if a, x, ok := spacecraft.NewFromLoadout("Lander-Crewed").NavSigma(); !ok || a != NavSigmaApolloNoLinkAlongM || x != NavSigmaApolloNoLinkAcrossM {
		t.Errorf("catalog nav_sigma_m = %v / %v (ok=%v), want the tuning table's %v / %v", a, x, ok, NavSigmaApolloNoLinkAlongM, NavSigmaApolloNoLinkAcrossM)
	}
	w, c := apolloLander(t)
	linkUp(w, c, false)
	a, x := rmsDraw(w, c, 300)
	if math.Abs(a/NavSigmaApolloNoLinkAlongM-1) > 0.15 || math.Abs(x/NavSigmaApolloNoLinkAcrossM-1) > 0.15 {
		t.Errorf("no link (crewed): rms %.0f / %.0f m, want ~%v / %v", a, x, NavSigmaApolloNoLinkAlongM, NavSigmaApolloNoLinkAcrossM)
	}
	if c.Landing.NavFixed {
		t.Error("a no-link draw is marked fixed: the ground update could never fire")
	}
	linkUp(w, c, true)
	if a, x = rmsDraw(w, c, 300); math.Abs(a/NavSigmaModernAlongM-1) > 0.15 || math.Abs(x/NavSigmaModernAcrossM-1) > 0.15 {
		t.Errorf("linked: rms %.1f / %.1f m, want ~%v (modern-grade)", a, x, NavSigmaModernAlongM)
	}
	linkUp(w, c, false)
	top := &c.Stages[len(c.Stages)-1]
	top.NavSigmaAlongM, top.NavSigmaAcrossM = 0, 0
	if a, x = rmsDraw(w, c, 300); math.Abs(a/NavSigmaModernAlongM-1) > 0.15 || math.Abs(x/NavSigmaModernAcrossM-1) > 0.15 {
		t.Errorf("modern vessel, no link: rms %.1f / %.1f m, want ~%v", a, x, NavSigmaModernAlongM)
	}
}

// Guard 6: the same (vessel, site, burn tick) draws the same offset in two
// Worlds; a different burn tick draws a different one.
func TestNavErrorIsDeterministicInTheSeed(t *testing.T) {
	w1, c1 := apolloLander(t)
	w2, c2 := apolloLander(t)
	linkUp(w1, c1, false)
	linkUp(w2, c2, false)
	w1.drawNavError(c1, true)
	w2.drawNavError(c2, true)
	if c1.Landing.NavAlongM != c2.Landing.NavAlongM || c1.Landing.NavAcrossM != c2.Landing.NavAcrossM {
		t.Errorf("two Worlds, same seed: %v/%v vs %v/%v", c1.Landing.NavAlongM, c1.Landing.NavAcrossM, c2.Landing.NavAlongM, c2.Landing.NavAcrossM)
	}
	w2.Clock.SimTime = w2.Clock.SimTime.Add(time.Second)
	w2.drawNavError(c2, true)
	if c1.Landing.NavAlongM == c2.Landing.NavAlongM {
		t.Error("a different burn tick drew the same offset")
	}
	// kept, not re-rolled, until N re-solves
	keep := c2.Landing.NavAlongM
	w2.Clock.SimTime = w2.Clock.SimTime.Add(time.Hour)
	w2.drawNavError(c2, false)
	if c2.Landing.NavAlongM != keep {
		t.Error("an unforced draw re-rolled a seeded offset")
	}
}

// P20: drawn with no link, the ground's update fires once on the first link
// before the brake, lands the site modern-grade, and never fires again; drawn
// with a link, it never fires.
func TestGroundUpdateFiresOnceOnTheFirstLink(t *testing.T) {
	w, c := apolloLander(t)
	linkUp(w, c, false)
	w.drawNavError(c, true)
	c.Landing.Engaged = true
	before := math.Hypot(c.Landing.NavAlongM, c.Landing.NavAcrossM)
	w.updateNavOnLink(c)
	if len(w.PendingLandingEvents) != 0 {
		t.Fatal("an update fired with no link")
	}
	linkUp(w, c, true)
	w.updateNavOnLink(c)
	if n := len(w.PendingLandingEvents); n != 1 || !w.PendingLandingEvents[0].Update {
		t.Fatalf("events %+v on the first link, want one update", w.PendingLandingEvents)
	}
	if d := math.Hypot(c.Landing.NavAlongM, c.Landing.NavAcrossM); d > 30 || before < 100 {
		t.Errorf("site error %.0f m before, %.1f m after the update, want kilometres to modern-grade", before, d)
	}
	linkUp(w, c, false)
	w.updateNavOnLink(c)
	linkUp(w, c, true)
	w.updateNavOnLink(c)
	if len(w.PendingLandingEvents) != 1 {
		t.Errorf("%d events after the link came back, want the update to fire once", len(w.PendingLandingEvents))
	}
	// linked at the draw: nothing to update; and once the brake is lit no update either
	w.PendingLandingEvents = nil
	linkUp(w, c, true)
	w.drawNavError(c, true)
	w.updateNavOnLink(c)
	linkUp(w, c, false)
	w.drawNavError(c, true)
	c.Landing.Phase = spacecraft.PhaseBrake
	linkUp(w, c, true)
	w.updateNavOnLink(c)
	if len(w.PendingLandingEvents) != 0 {
		t.Errorf("events %+v, want none (linked at the draw; link first seen after the brake)", w.PendingLandingEvents)
	}
}

// Guards 7 and the P33 plumbing: miss: and cross: read the computer's site, lpd:
// still reads the real flag.
func TestMissReadsTheComputersSiteLpdReadsTheFlag(t *testing.T) {
	w, c := afterDeorbitBurn(t)
	base, ok := w.LandBesideReading()
	if !ok || !base.MissOK || !base.LPDOK {
		t.Fatalf("setup: reading %+v", base)
	}
	c.Landing = &spacecraft.LandingAutopilot{NavSeeded: true, NavAlongM: 2000, NavAcrossM: -1000}
	shifted, _ := w.LandBesideReading()
	// the site is 2 km further along, so the stop is 2 km short of it, and 1 km to its right;
	// the tolerance is the two definitions of along (the geodesic from the vessel here, the
	// orbit plane's tangent at the site in the autopilot's forecast)
	if d := shifted.MissM - base.MissM; math.Abs(d+2000) > 200 {
		t.Errorf("miss moved %.0f m, want -2000 +- 200 for a computer's site 2 km long", d)
	}
	if d := shifted.CrossM - base.CrossM; math.Abs(d-1000) > 200 {
		t.Errorf("cross moved %.0f m, want +1000 +- 200 for a computer's site 1 km left", d)
	}
	if shifted.LPDDeg != base.LPDDeg {
		t.Errorf("lpd moved from %.3f to %.3f with the offset, want the real flag's", base.LPDDeg, shifted.LPDDeg)
	}
}

// Guard 9: the aim rungs change at 50 km and 5 km exactly (the boundary takes
// the nearer rung); an arrow moves the aim by the rung, alt 10 m (P42) or a
// tenth of the rung when smaller.
func TestSiteShiftRungsAndNudge(t *testing.T) {
	for _, tc := range []struct {
		g, want float64
	}{
		{SiteShiftFarRangeM + 1, SiteShiftRungFarM}, {SiteShiftFarRangeM, SiteShiftRungMidM},
		{SiteShiftMidRangeM + 1, SiteShiftRungMidM}, {SiteShiftMidRangeM, SiteShiftRungNearM}, {0, SiteShiftRungNearM},
	} {
		if got := siteShiftRung(tc.g); got != tc.want {
			t.Errorf("rung at %.0f m = %v, want %v", tc.g, got, tc.want)
		}
	}
	for _, tc := range []struct{ g, want float64 }{
		{SiteShiftFarRangeM + 1, 10}, {SiteShiftMidRangeM + 1, 10}, {SiteShiftMidRangeM, 2.5},
	} {
		if got := siteShiftStep(tc.g, true); got != tc.want {
			t.Errorf("alt press at %.0f m = %v m, want %v m (P42)", tc.g, got, tc.want)
		}
	}
	w, c := afterDeorbitBurn(t)
	c.Landing = &spacecraft.LandingAutopilot{Engaged: true, Nose: true}
	if w.NudgeLandingAim(1, 0, false) || c.Landing.AimAlongM != 0 {
		t.Error("the q nose hold took an aim arrow: the aim arrows are N-only (P33)")
	}
	c.Landing.Nose = false
	if !w.NudgeLandingAim(-1, 1, false) {
		t.Fatal("Site+ auto refused an aim arrow")
	}
	rung := math.Abs(c.Landing.AimAlongM)
	if c.Landing.AimAlongM >= 0 || c.Landing.AimAcrossM != rung || (rung != SiteShiftRungFarM && rung != SiteShiftRungMidM && rung != SiteShiftRungNearM) {
		t.Errorf("aim %v / %v, want one rung short and one right", c.Landing.AimAlongM, c.Landing.AimAcrossM)
	}
	w.NudgeLandingAim(1, -1, true)
	if got, want := c.Landing.AimAlongM, -rung+math.Min(SiteShiftFineM, rung/10); math.Abs(got-want) > 1e-9 {
		t.Errorf("alt arrow: aim along %v, want %v", got, want)
	}
	w.ClearLandingAim()
	if c.Landing.AimAlongM != 0 || c.Landing.AimAcrossM != 0 {
		t.Error("ClearLandingAim left an aim")
	}
}

// F2 (review 2): a hand takeover keeps the aim while the vessel flies, but a
// hand landing ends the attempt, so the next N replans against the real flag.
func TestHandLandingClearsTheAimAttempt(t *testing.T) {
	w, c := deorbitWorld(t, 150, 0)
	if _, err := w.PlanDeorbitAndEngage(); err != nil {
		t.Fatal(err)
	}
	w.NudgeLandingAim(+1, 0, false)
	if c.Landing.AimAlongM == 0 || !c.Landing.NavSeeded {
		t.Fatalf("setup: aim %v seeded %v", c.Landing.AimAlongM, c.Landing.NavSeeded)
	}
	w.SetAttitudeMode(spacecraft.BurnSurfaceRetrograde) // the hold key takes the nose
	if c.Landing.Engaged {
		t.Fatal("setup: the hold key left the autopilot engaged")
	}
	ticks(w, 3) // still flying: the aim is the attempt's, it stays
	if c.Landing.AimAlongM == 0 {
		t.Error("the aim was dropped while the vessel still flew")
	}
	c.Landed = true
	ticks(w, 3)
	if c.Landing.AimAlongM != 0 || c.Landing.AimAcrossM != 0 || c.Landing.NavSeeded {
		t.Errorf("hand landing left aim %v/%v seeded %v", c.Landing.AimAlongM, c.Landing.AimAcrossM, c.Landing.NavSeeded)
	}
}

// walkAim presses the arrows, one axis at a time and alt once the rest
// is under 600 m, until the computer's site is within 25 m of the real flag;
// it reports the presses.
func walkAim(w *World, ap *spacecraft.LandingAutopilot) (presses int) {
	for i := 0; i < 1000; i++ {
		al, ac := ap.NavAlongM+ap.AimAlongM, ap.NavAcrossM+ap.AimAcrossM
		switch {
		case math.Abs(al) >= 25:
			w.NudgeLandingAim(-int(math.Copysign(1, al)), 0, math.Abs(al) < 600)
		case math.Abs(ac) >= 25:
			w.NudgeLandingAim(0, -int(math.Copysign(1, ac)), math.Abs(ac) < 600)
		default:
			return
		}
		presses++
	}
	return
}

// ADR 0067 S4 flown guards: an Apollo-era landing with no link and no presses
// lands off the flag by the computer's error; the same vessel after walking the
// aim on lands near the flag; linked at the burn it lands modern-grade.
func TestFlownLandingsByEraLinkAndPresses(t *testing.T) {
	t.Run("apollo no link no presses", func(t *testing.T) {
		w, c := apolloLander(t)
		linkUp(w, c, false)
		var off float64
		dist, _, landed := flyLandingNoRelink(t, w, c, func() {
			if c.Landing != nil && c.Landing.NavSeeded {
				off = math.Hypot(c.Landing.NavAlongM, c.Landing.NavAcrossM)
			}
		})
		t.Logf("no link, no presses: landed %.0f m from the flag, computer's error %.0f m", dist, off)
		if !landed || off < 200 || math.Abs(dist-off) > 150 {
			t.Errorf("landed=%v dist=%.0f error=%.0f, want a soft landing off the flag by the computer's error (+-150 m)", landed, dist, off)
		}
	})
	t.Run("apollo no link, aim walked on", func(t *testing.T) {
		w, c := apolloLander(t)
		linkUp(w, c, false)
		var did bool
		presses := 0
		dist, _, landed := flyLandingNoRelink(t, w, c, func() {
			if !did && c.Landing.Phase == spacecraft.PhaseBrake {
				did = true
				presses = walkAim(w, c.Landing)
			}
		})
		t.Logf("no link, %d presses: landed %.0f m from the flag", presses, dist)
		if !landed || dist > 50 || presses == 0 {
			t.Errorf("landed=%v dist=%.0f presses=%d, want a landing within 50 m after the presses", landed, dist, presses)
		}
	})
	t.Run("apollo linked at the burn", func(t *testing.T) {
		w, c := apolloLander(t)
		linkUp(w, c, true)
		dist, _, landed := flyLandingNoRelink(t, w, c, nil)
		t.Logf("linked: landed %.1f m from the flag", dist)
		if !landed || dist > 25 {
			t.Errorf("landed=%v dist=%.1f, want modern-grade within 25 m", landed, dist)
		}
	})
	t.Run("modern", func(t *testing.T) {
		w, c := apolloLander(t)
		linkUp(w, c, false)
		top := &c.Stages[len(c.Stages)-1]
		top.NavSigmaAlongM, top.NavSigmaAcrossM = 0, 0
		dist, _, landed := flyLandingNoRelink(t, w, c, nil)
		t.Logf("modern: landed %.1f m from the flag", dist)
		if !landed || dist > 25 {
			t.Errorf("landed=%v dist=%.1f, want within 25 m", landed, dist)
		}
	})
}

// flyLandingNoRelink is flyLanding that keeps the fixture's COMMS link as the
// test set it (the world recomputes the graph each tick; the antenna-less
// fixture stays unlinked, and a linked case only needs the link at the draw).
func flyLandingNoRelink(t *testing.T, w *World, c *spacecraft.Spacecraft, each func()) (distM, lastSpeed float64, landed bool) {
	return flyLanding(t, w, c, 2*3600*4, func(int) {
		if each != nil {
			each()
		}
	})
}
