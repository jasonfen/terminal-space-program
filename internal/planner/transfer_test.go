package planner

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// Reference values for textbook Earth → Mars Hohmann transfer
// (Curtis Example 8.3, slightly reformulated for SI):
//
//	r_earth      = 1 AU                = 1.496e11 m
//	r_mars       = 1.524 AU            = 2.279e11 m
//	r_park (LEO) = 6378 + 200 km       = 6.578e6 m
//	r_capture    = Mars 200 km altitude = 3.39e6 + 200e3 = 3.59e6 m
//
//	Δv_departure ≈ 3.61 km/s (heliocentric v∞ ≈ 2.945 km/s, escape from LEO)
//	Δv_arrival   ≈ 2.09 km/s (heliocentric v∞ ≈ 2.65 km/s, capture at low Mars)
//	total        ≈ 5.7 km/s
//	t_transfer   ≈ 258.8 days
//
// muSun is shared with hohmann_test.go (var declared there).
const (
	muEarth  = 3.986004418e14
	muMars   = 4.282837e13
	rEarth   = 1.495978707e11
	rMars    = 1.524 * 1.495978707e11
	rPark    = 6.578e6
	rMarsCap = 3.39e6 + 200e3
)

// TestPlanHohmannTransferEarthToMars: top-level Δv and transfer time
// match Curtis Example 8.3 (textbook Earth→Mars Hohmann) within 5%.
// The scope-excluded "phasing not enforced" caveat means absolute
// timing accuracy isn't tested — only the canonical Δv budget.
func TestPlanHohmannTransferEarthToMars(t *testing.T) {
	plan, err := PlanHohmannTransfer(
		muSun, rEarth, rMars,
		muEarth, rPark, "earth",
		muMars, rMarsCap, "mars",
	)
	if err != nil {
		t.Fatalf("PlanHohmannTransfer: %v", err)
	}

	// Δv_departure ~ 3.61 km/s.
	wantDvDep := 3610.0
	if d := math.Abs(plan.Departure.DV-wantDvDep) / wantDvDep; d > 0.05 {
		t.Errorf("departure Δv: got %.0f, want ≈%.0f m/s (rel %.2e)",
			plan.Departure.DV, wantDvDep, d)
	}

	// Δv_arrival ~ 2.09 km/s — that's into a LOW Mars orbit. The
	// 5% tolerance covers small variations in textbook reference
	// (some sources use ~2.6 km/s for higher capture orbits).
	wantDvArr := 2090.0
	if d := math.Abs(plan.Arrival.DV-wantDvArr) / wantDvArr; d > 0.05 {
		t.Errorf("arrival Δv: got %.0f, want ≈%.0f m/s (rel %.2e)",
			plan.Arrival.DV, wantDvArr, d)
	}

	// Frame tagging: departure in Earth frame, arrival in Mars frame.
	if plan.Departure.PrimaryID != "earth" {
		t.Errorf("departure PrimaryID: got %q, want earth", plan.Departure.PrimaryID)
	}
	if plan.Arrival.PrimaryID != "mars" {
		t.Errorf("arrival PrimaryID: got %q, want mars", plan.Arrival.PrimaryID)
	}

	// Transfer time ≈ 258.8 days ≈ 22.36e6 s.
	wantTransfer := 22.36e6
	gotTransfer := plan.TransferDt.Seconds()
	if d := math.Abs(gotTransfer-wantTransfer) / wantTransfer; d > 0.02 {
		t.Errorf("transfer time: got %.3e s, want ≈%.3e s (rel %.2e)",
			gotTransfer, wantTransfer, d)
	}

	// Outbound transfer: departure prograde, arrival retrograde
	// (slow down at apoapsis to drop into Mars orbit).
	if plan.Departure.IsRetrograde {
		t.Errorf("outbound departure should be prograde, got retrograde")
	}
	if !plan.Arrival.IsRetrograde {
		t.Errorf("outbound arrival should be retrograde, got prograde")
	}
}

// TestPlanHohmannTransferInbound: a Mars→Earth transfer flips the
// retrograde flags (slow down at departure to drop perihelion;
// speed up at Earth to circularize at the new lower radius).
func TestPlanHohmannTransferInbound(t *testing.T) {
	plan, err := PlanHohmannTransfer(
		muSun, rMars, rEarth,
		muMars, rMarsCap, "mars",
		muEarth, rPark, "earth",
	)
	if err != nil {
		t.Fatalf("PlanHohmannTransfer: %v", err)
	}
	if !plan.Departure.IsRetrograde {
		t.Errorf("inbound departure should be retrograde, got prograde")
	}
	if plan.Arrival.IsRetrograde {
		t.Errorf("inbound arrival should be prograde, got retrograde")
	}
}

// TestPlanHohmannTransferRejectsDegenerate: equal radii, zero/negative
// inputs all surface as errors rather than panicking or NaN-ing
// silently.
func TestPlanHohmannTransferRejectsDegenerate(t *testing.T) {
	cases := []struct {
		name              string
		muSun, rDep, rArr float64
	}{
		{"equal radii", muSun, rEarth, rEarth},
		{"zero rDep", muSun, 0, rMars},
		{"negative rArr", muSun, rEarth, -1},
		{"zero muSun", 0, rEarth, rMars},
		{"zero rArr", muSun, rEarth, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := PlanHohmannTransfer(
				c.muSun, c.rDep, c.rArr,
				muEarth, rPark, "earth",
				muMars, rMarsCap, "mars",
			)
			if err == nil {
				t.Errorf("expected error for %s", c.name)
			}
		})
	}
}

// TestPlanHohmannTransferPropagatesBurnErrors: a bad parking or capture
// radius surfaces the burn helper's error rather than a plan with a NaN Δv.
func TestPlanHohmannTransferPropagatesBurnErrors(t *testing.T) {
	for name, c := range map[string]struct{ rPark, rCapture float64 }{
		"escape (rPark=0)":     {0, rMarsCap},
		"capture (rCapture=0)": {rPark, 0},
	} {
		if _, err := PlanHohmannTransfer(muSun, rEarth, rMars, muEarth, c.rPark, "earth", muMars, c.rCapture, "mars"); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// angleAt advances an angle by mean motion n for t seconds.
func angleAt(theta, n, t float64) float64 { return theta + n*t }

// TestIntraPrimaryPhasingArrivesAtRequiredPhase: an independent check of the
// launch window. After waiting waitTime the target must lead the craft by
// π − n_target·T_transfer (mod 2π), the wait must be the FIRST such window
// (less than one synodic period) unless padded by minLead, and padding adds
// whole synodic periods. Outbound (craft faster than target) and inbound
// (craft slower) take opposite wrap directions.
func TestIntraPrimaryPhasingArrivesAtRequiredPhase(t *testing.T) {
	cases := []struct {
		name                          string
		rDep, rArr                    float64
		craftAngle, targetAngle, lead float64
	}{
		{"LEO to Luna", rEarthLEO, aLuna, 0, 1.0, 0},
		{"LEO to Luna, negative angles", rEarthLEO, aLuna, 3, -2, 0},
		{"LEO to Luna, target right behind", rEarthLEO, aLuna, 0.5, 0.25, 0},
		{"LEO to Luna, padded by minLead", rEarthLEO, aLuna, 0, 1.0, 20000},
		{"Luna distance to LEO (inbound)", aLuna, rEarthLEO, 0.5, 2.0, 0},
		{"inbound, target right behind", aLuna, rEarthLEO, 2.0, 0.5, 0},
	}
	for _, c := range cases {
		wait, transfer := intraPrimaryPhasing(muEarth, c.rDep, c.rArr, c.craftAngle, c.targetAngle, c.lead)
		aT := (c.rDep + c.rArr) / 2
		if want := math.Pi * math.Sqrt(aT*aT*aT/muEarth); math.Abs(transfer-want) > 1e-6*want {
			t.Errorf("%s: transfer %.1f s, want half the ellipse period %.1f s", c.name, transfer, want)
		}
		nC := math.Sqrt(muEarth / (c.rDep * c.rDep * c.rDep))
		nT := math.Sqrt(muEarth / (c.rArr * c.rArr * c.rArr))
		if wait < 0 || wait < c.lead {
			t.Errorf("%s: wait %.1f s is negative or short of minLead %.0f s", c.name, wait, c.lead)
		}
		got := wrapTau(angleAt(c.targetAngle, nT, wait) - angleAt(c.craftAngle, nC, wait))
		want := wrapTau(math.Pi - nT*transfer)
		if d := math.Abs(got - want); d > 1e-6 && math.Abs(d-2*math.Pi) > 1e-6 {
			t.Errorf("%s: phase at burn %.6f rad, want %.6f rad", c.name, got, want)
		}
		synodic := 2 * math.Pi / math.Abs(nT-nC)
		if first := math.Max(wait-c.lead, 0); first >= synodic+1 && c.lead == 0 {
			t.Errorf("%s: wait %.0f s skipped a window (synodic %.0f s)", c.name, wait, synodic)
		}
		if c.lead > 0 && wait-c.lead >= synodic+1 {
			t.Errorf("%s: wait %.0f s over-padded past minLead %.0f s (synodic %.0f s)", c.name, wait, c.lead, synodic)
		}
	}
	// Same radius: no relative motion, so no window to wait for.
	if wait, _ := intraPrimaryPhasing(muEarth, rEarthLEO, rEarthLEO, 0, 1, 1000); wait != 0 {
		t.Errorf("equal radii: wait %.1f, want 0", wait)
	}
}

// TestWrapTau: normalises to [0, 2π); the edges 0 and 2π map to 0.
func TestWrapTau(t *testing.T) {
	for _, c := range []struct{ in, want float64 }{
		{0, 0}, {1, 1}, {2 * math.Pi, 0}, {-math.Pi / 2, 3 * math.Pi / 2},
		{5 * math.Pi, math.Pi}, {-2 * math.Pi, 0}, {-0.5, 2*math.Pi - 0.5},
	} {
		if got := wrapTau(c.in); math.Abs(got-c.want) > 1e-12 || got < 0 || got >= 2*math.Pi {
			t.Errorf("wrapTau(%v) = %v, want %v in [0, 2π)", c.in, got, c.want)
		}
	}
}

// TestPlanIntraPrimaryHohmann: LEO to Luna's distance and back. Δv magnitudes
// sit in the textbook band (TLI from 200 km is ~3.1 km/s; braking into a
// 100 km lunar orbit ~0.8 km/s), the arrival is offset by exactly the coast,
// outbound burns prograde then retrograde, and inbound flips both.
func TestPlanIntraPrimaryHohmann(t *testing.T) {
	out, err := PlanIntraPrimaryHohmann(muEarth, rEarthLEO, aLuna, 0, 1, 0, "earth", muLuna, rLowLunarOrbit, "luna")
	if err != nil {
		t.Fatal(err)
	}
	if out.Departure.DV < 3000 || out.Departure.DV > 3250 {
		t.Errorf("TLI Δv %.0f m/s outside 3.0-3.25 km/s", out.Departure.DV)
	}
	if out.Arrival.DV < 700 || out.Arrival.DV > 950 {
		t.Errorf("lunar capture Δv %.0f m/s outside 0.7-0.95 km/s", out.Arrival.DV)
	}
	if days := out.TransferDt.Hours() / 24; days < 4.5 || days > 5.5 {
		t.Errorf("transfer %.2f days, want ~5", days)
	}
	if out.Arrival.OffsetTime != out.Departure.OffsetTime+out.TransferDt {
		t.Errorf("arrival offset %v != departure %v + coast %v", out.Arrival.OffsetTime, out.Departure.OffsetTime, out.TransferDt)
	}
	if out.Departure.IsRetrograde || !out.Arrival.IsRetrograde {
		t.Errorf("outbound: want prograde departure / retrograde arrival, got %v / %v", out.Departure.IsRetrograde, out.Arrival.IsRetrograde)
	}
	if out.Departure.PrimaryID != "earth" || out.Arrival.PrimaryID != "luna" {
		t.Errorf("frame ids %q / %q", out.Departure.PrimaryID, out.Arrival.PrimaryID)
	}
	if out.Departure.OffsetTime < 0 {
		t.Errorf("departure in the past: %v", out.Departure.OffsetTime)
	}

	in, err := PlanIntraPrimaryHohmann(muEarth, aLuna, rEarthLEO, 0, 1, 0, "luna", muEarth, rEarthLEO, "earth")
	if err != nil {
		t.Fatal(err)
	}
	if !in.Departure.IsRetrograde || in.Arrival.IsRetrograde {
		t.Errorf("inbound: want retrograde departure / prograde arrival, got %v / %v", in.Departure.IsRetrograde, in.Arrival.IsRetrograde)
	}

	// minLead pads the departure to at least that far out.
	padded, err := PlanIntraPrimaryHohmann(muEarth, rEarthLEO, aLuna, 0, 1, 50000, "earth", muLuna, rLowLunarOrbit, "luna")
	if err != nil {
		t.Fatal(err)
	}
	if padded.Departure.OffsetTime < 50000*time.Second {
		t.Errorf("departure %v is inside the 50000 s minLead", padded.Departure.OffsetTime)
	}
}

// TestPlanIntraPrimaryHohmannRejects: non-positive inputs (zero is the edge),
// equal radii, and a capture radius the burn helper refuses.
func TestPlanIntraPrimaryHohmannRejects(t *testing.T) {
	cases := []struct {
		name                        string
		mu, rDep, rArr, muTgt, rCap float64
	}{
		{"mu=0", 0, rEarthLEO, aLuna, muLuna, rLowLunarOrbit},
		{"rDep=0", muEarth, 0, aLuna, muLuna, rLowLunarOrbit},
		{"rArr=0", muEarth, rEarthLEO, 0, muLuna, rLowLunarOrbit},
		{"equal radii", muEarth, rEarthLEO, rEarthLEO, muLuna, rLowLunarOrbit},
		{"capture rCapture=0", muEarth, rEarthLEO, aLuna, muLuna, 0},
	}
	for _, c := range cases {
		if _, err := PlanIntraPrimaryHohmann(c.mu, c.rDep, c.rArr, 0, 1, 0, "earth", c.muTgt, c.rCap, "luna"); err == nil {
			t.Errorf("%s: want error", c.name)
		}
	}
}

// TestPlanLambertTransfer: an Earth-to-Mars Lambert plan at the Hohmann
// time of flight, 10 degrees off the 180 degree geometry. The Δv budget stays
// near the Hohmann ~5.7 km/s, the arrival is offset by depOffset + tof,
// outbound burns prograde then retrograde and inbound flips, and bad inputs
// (zero is the edge) or an unsolvable Lambert come back as errors.
func TestPlanLambertTransfer(t *testing.T) {
	const tof = 22.36e6
	vE := math.Sqrt(muSun / rEarth)
	vM := math.Sqrt(muSun / rMars)
	rDep := orbital.Vec3{X: rEarth}
	vDep := orbital.Vec3{Y: vE}
	ang := 170 * math.Pi / 180
	rArr := orbital.Vec3{X: rMars * math.Cos(ang), Y: rMars * math.Sin(ang)}
	vArr := orbital.Vec3{X: -vM * math.Sin(ang), Y: vM * math.Cos(ang)}
	const lead = 3 * 24 * time.Hour

	plan := func(muS, tf, rP, rC float64, rd, vd, ra, va orbital.Vec3) (TransferPlan, error) {
		return PlanLambertTransfer(muS, rd, vd, ra, va, tf, muEarth, rP, "earth", muMars, rC, "mars", lead, false, 0, false)
	}
	out, err := plan(muSun, tof, rPark, rMarsCap, rDep, vDep, rArr, vArr)
	if err != nil {
		t.Fatal(err)
	}
	if total := out.Departure.DV + out.Arrival.DV; total < 5000 || total > 7000 {
		t.Errorf("total Δv %.0f m/s, want near the Hohmann 5.7 km/s", total)
	}
	if out.Departure.OffsetTime != lead || out.TransferDt != time.Duration(tof*float64(time.Second)) ||
		out.Arrival.OffsetTime != lead+out.TransferDt {
		t.Errorf("offsets: dep %v coast %v arr %v", out.Departure.OffsetTime, out.TransferDt, out.Arrival.OffsetTime)
	}
	if out.Departure.IsRetrograde || !out.Arrival.IsRetrograde {
		t.Errorf("outbound flags: dep retro=%v arr retro=%v", out.Departure.IsRetrograde, out.Arrival.IsRetrograde)
	}
	if out.Departure.PrimaryID != "earth" || out.Arrival.PrimaryID != "mars" {
		t.Errorf("frame ids %q / %q", out.Departure.PrimaryID, out.Arrival.PrimaryID)
	}
	// Inbound: arrival radius smaller than departure radius.
	in, err := plan(muSun, tof, rMarsCap, rPark, rArr, vArr, rDep, vDep)
	if err != nil {
		t.Fatal(err)
	}
	if !in.Departure.IsRetrograde || in.Arrival.IsRetrograde {
		t.Errorf("inbound flags: dep retro=%v arr retro=%v", in.Departure.IsRetrograde, in.Arrival.IsRetrograde)
	}
	for name, c := range map[string]struct {
		muS, tf, rP, rC float64
	}{
		"muSun=0":          {0, tof, rPark, rMarsCap},
		"tof=0":            {muSun, 0, rPark, rMarsCap},
		"unsolvable (1 s)": {muSun, 1, rPark, rMarsCap},
		"escape rPark=0":   {muSun, tof, 0, rMarsCap},
		"capture rCap=0":   {muSun, tof, rPark, 0},
	} {
		if _, err := plan(c.muS, c.tf, c.rP, c.rC, rDep, vDep, rArr, vArr); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// TestIntraPrimaryPhasingWindowOpenNowFiresNow: when the current phase is
// already exactly the required one, the burn fires now (wait 0) rather than
// a whole synodic period later, in both rotation senses; and a minLead equal
// to the computed wait needs no padding. The angles are built from the same
// expressions the planner uses so the equality is exact, not near.
func TestIntraPrimaryPhasingWindowOpenNowFiresNow(t *testing.T) {
	for _, c := range []struct {
		name       string
		rDep, rArr float64
	}{
		{"outbound (craft faster)", rEarthLEO, aLuna},
		{"inbound (craft slower)", aLuna, rEarthLEO},
	} {
		aT := (c.rDep + c.rArr) / 2
		transfer := math.Pi * math.Sqrt(aT*aT*aT/muEarth)
		nT := math.Sqrt(muEarth / (c.rArr * c.rArr * c.rArr))
		openNow := wrapTau(math.Pi - nT*transfer) // target lead needed; craft at 0
		if wait, _ := intraPrimaryPhasing(muEarth, c.rDep, c.rArr, 0, openNow, 0); wait != 0 {
			t.Errorf("%s: window open now but wait = %.1f s", c.name, wait)
		}
		// A real wait, then ask for a lead equal to it: no extra synodic period.
		wait, _ := intraPrimaryPhasing(muEarth, c.rDep, c.rArr, 0, openNow+1, 0)
		if wait <= 0 {
			t.Fatalf("%s: setup wait %.1f", c.name, wait)
		}
		if again, _ := intraPrimaryPhasing(muEarth, c.rDep, c.rArr, 0, openNow+1, wait); again != wait {
			t.Errorf("%s: minLead == wait padded %.1f s -> %.1f s", c.name, wait, again)
		}
	}
}
