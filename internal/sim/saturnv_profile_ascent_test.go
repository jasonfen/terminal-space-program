package sim

// saturnv_profile_ascent_test.go flies a CLOSED-LOOP ascent of the catalog
// "Apollo Stack" mission-stack to a ~185 km circular parking orbit through
// the game's real force models (physics.Accel + physics.DragAccel, RK4),
// using the catalog masses / thrust / Isp / ballistic coefficients
// (loadouts.go, byte-identical to the values inlined here).
//
// The Apollo Stack's lower three stages are the canonical Saturn V
// (S-IC / S-II / S-IVB); above them ride the Lunar Module (Descent +
// Ascent) and the Service + Command Modules as dead payload during ascent
// — only the Saturn stages fire to reach the parking orbit. That payload
// is what TLI later has to push, so the parking orbit is flown with the
// full ~39 t mission stack on top.
//
// The documented Saturn V Ascent Profile Reference Table is an open-loop
// hand-flown schedule; flown verbatim it leaves a wildly eccentric orbit.
// So the guidance here is the proven feedback-driven gravity turn (shared
// in shape with flyApolloAscent in apollo_probe_helpers_test.go):
//
//   1. Vertical rise off the pad until tower-clear altitude.
//   2. Speed-paced gravity turn — pitch tips from vertical toward the
//      horizon as surface speed climbs toward a pacing target vTarget.
//   3. Apoapsis hold, gated on vUp < 250 m/s so it engages LATE — only
//      once the vehicle is already near-horizontal near the target apoapsis
//      — and only briefly. This is the key to efficiency: the longer the
//      vehicle climbs near-prograde before the (lossy, partly-vertical)
//      hold takes over, the lower the gravity/steering loss.
//
// MECO once periapsis has been raised to the target (orbit near-circular);
// a small prograde trim at apoapsis cleans up any residual eccentricity.
// vTarget is swept and the most fuel-efficient ascent (most S-IVB
// propellant at park) that still parks near 185 km circular is chosen.
// Staging is fuel-driven (S-IC → S-II → S-IVB drop as each runs dry).
//
// Run: go test ./internal/sim -run TestApolloStackClosedLoopAscent -v

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// closedLoopResult reports one closed-loop ascent run.
type closedLoopResult struct {
	vTarget                float64 // speed-pacing target that shaped the gravity turn
	apoKm, periKm, ecc     float64
	fuelLeftSIVB, expended float64
	reached                bool
}

// flyApolloStackClosedLoop flies the Apollo Stack (three Saturn stages +
// `payload` dead mass) to a `targetAltM` circular park with a speed-paced
// gravity turn + late apoapsis hold, finishing with a real circularization
// burn. When verbose, it logs phase milestones to t.
func flyApolloStackClosedLoop(t *testing.T, earth bodies.CelestialBody, stages []apolloStage, payload, vTarget, targetAltM float64, verbose bool) closedLoopResult {
	mu := earth.GravitationalParameter()
	Re := earth.RadiusMeters()
	rTarget := Re + targetAltM

	omega := physics.AtmosphereOmega(earth)
	spin := omega.Unit()
	seed := orbital.Vec3{X: 1}
	if math.Abs(spin.Dot(seed)) > 0.9 {
		seed = orbital.Vec3{Y: 1}
	}
	upHat0 := seed.Sub(spin.Scale(seed.Dot(spin))).Unit()
	r := upHat0.Scale(Re)
	v := omega.Cross(r)

	const (
		dt     = 0.2
		hStart = 1000.0
	)
	fuel := []float64{stages[0].fuel, stages[1].fuel, stages[2].fuel}
	active := 0
	expended := 0.0

	totalMass := func() float64 {
		m := payload
		for i := active; i < len(stages); i++ {
			m += stages[i].dry + fuel[i]
		}
		return m
	}
	osc := func() (apoAlt, periAlt, ecc, vUp float64, bound bool) {
		// Osculating shape is mass-independent (two-body) — reuse the canonical
		// element solver instead of re-deriving e / apo / peri by hand.
		el := orbital.ElementsFromState(r, v, mu)
		ecc = el.E
		vUp = v.Dot(r.Unit())
		if el.A > 0 {
			return el.Apoapsis() - Re, el.Periapsis() - Re, ecc, vUp, true
		}
		return math.NaN(), math.NaN(), ecc, vUp, false
	}
	step := func(tt float64, dir *orbital.Vec3) {
		m := totalMass()
		bc := stages[active].bc
		var aThrust orbital.Vec3
		firing := dir != nil && fuel[active] > 0
		if firing {
			th := stages[active].thrust
			aThrust = dir.Scale(th / m)
			expended += (th / m) * dt
		}
		accelFn := func(rr, vv orbital.Vec3, _ float64) orbital.Vec3 {
			return physics.Accel(rr, mu).
				Add(physics.DragAccel(rr, vv, earth, bc)).
				Add(aThrust)
		}
		ns := physics.StepRK4(physics.StateVector{R: r, V: v, M: m}, dt, accelFn, tt)
		r, v = ns.R, ns.V
		if cl, clamped := physics.ClampToSurface(physics.StateVector{R: r, V: v, M: m}, earth); clamped {
			r, v = cl.R, cl.V
		}
		if firing {
			mdot := stages[active].thrust / (stages[active].isp * apolloG0)
			fuel[active] -= mdot * dt
			for active < len(stages)-1 && fuel[active] <= 0 {
				fuel[active] = 0
				active++
			}
		}
	}
	logRow := func(phase string, tt float64) {
		if !verbose {
			return
		}
		apoAlt, _, _, _, _ := osc()
		surf := physics.AirRelativeVelocity(r, v, earth).Norm()
		t.Logf("%-26s %6.0f  %8.1f  %6.0f  %8.1f",
			phase, tt, (r.Norm()-Re)/1000, surf, apoAlt/1000)
	}

	if verbose {
		t.Logf("%-26s %6s  %8s  %6s  %8s", "phase", "t(s)", "alt(km)", "v(m/s)", "apo(km)")
		logRow("liftoff", 0)
	}

	// ── Powered ascent: vertical rise → speed-paced gravity turn → late hold ──
	prevActive := 0
	tt := 0.0
	for ; tt < 1400; tt += dt {
		alt := r.Norm() - Re
		upHat := r.Unit()
		eastHat := spin.Cross(r).Unit()
		apoAlt, periAlt, _, vUp, bound := osc()

		// MECO once periapsis is raised to the target — orbit near-circular.
		if bound && periAlt >= targetAltM-5e3 && alt > 100e3 {
			break
		}

		var dir orbital.Vec3
		switch {
		case alt < hStart:
			dir = upHat // vertical rise
		case bound && apoAlt+Re >= rTarget*0.98 && vUp < 250:
			// Apoapsis hold, GATED on vUp < 250 m/s so it engages late —
			// only once the gravity turn has tipped the vehicle near-
			// horizontal and apoapsis is near the target. The longer the
			// vehicle climbs near-prograde before this (partly-vertical,
			// lossy) hold takes over, the lower the gravity/steering loss;
			// the gate is the whole efficiency lever vs. an early hold that
			// fights down a large vertical velocity. The balance law's
			// vertical thrust offsets gravity-minus-centrifugal and damps
			// residual vUp, holding apoapsis while horizontal speed builds.
			rMag := r.Norm()
			vHorVec := v.Sub(upHat.Scale(vUp))
			horHat := eastHat
			if vHorVec.Norm() > 1 {
				horHat = vHorVec.Unit()
			}
			aGrav := mu / (rMag * rMag)
			aCentrifugal := vHorVec.Norm() * vHorVec.Norm() / rMag
			aT := stages[active].thrust / totalMass()
			aV := (aGrav - aCentrifugal) - vUp*0.1
			sinPhi := math.Max(-1, math.Min(1, aV/aT))
			phi := math.Asin(sinPhi)
			dir = horHat.Scale(math.Cos(phi)).Add(upHat.Scale(math.Sin(phi)))
		default:
			// Speed-paced gravity turn: pitchDeg is the angle from vertical,
			// growing with surface speed toward vTarget. Thrust stays along
			// (near) the velocity vector, so steering loss is small.
			vrel := physics.AirRelativeVelocity(r, v, earth).Norm()
			pitchDeg := 90.0 * vrel / vTarget
			if pitchDeg > 89.0 {
				pitchDeg = 89.0
			}
			p := pitchDeg * math.Pi / 180
			dir = upHat.Scale(math.Cos(p)).Add(eastHat.Scale(math.Sin(p)))
		}

		step(tt, &dir)

		if active != prevActive {
			logRow([]string{"S-IC burnout → S-II", "S-II burnout → S-IVB"}[prevActive], tt)
			prevActive = active
		}
		if active == len(stages)-1 && fuel[active] <= 0 {
			return closedLoopResult{vTarget: vTarget, reached: false}
		}
	}
	logRow("MECO (near-circular)", tt)

	// Coast + trim only if MECO left residual eccentricity.
	if _, _, ecc, _, _ := osc(); ecc >= 5e-3 {
		coastEnd := tt + 6000
		for ; tt < coastEnd; tt += dt {
			if _, _, _, vUp, _ := osc(); vUp <= 0 {
				break
			}
			step(tt, nil)
		}
		logRow("apoapsis", tt)

		circStart := tt
		for ; tt < circStart+600; tt += dt {
			_, periAlt, ecc, _, bound := osc()
			if bound && (periAlt >= targetAltM-300 || ecc < 1e-3) {
				break
			}
			upHat := r.Unit()
			vUp := v.Dot(upHat)
			horVec := v.Sub(upHat.Scale(vUp))
			if horVec.Norm() < 1 {
				break
			}
			dir := horVec.Unit()
			step(tt, &dir)
			if active == len(stages)-1 && fuel[active] <= 0 {
				return closedLoopResult{vTarget: vTarget, reached: false}
			}
		}
		logRow("circularized", tt)
	}

	apoAlt, periAlt, ecc, _, bound := osc()
	if !bound {
		return closedLoopResult{vTarget: vTarget, reached: false}
	}
	return closedLoopResult{
		vTarget:      vTarget,
		apoKm:        apoAlt / 1000,
		periKm:       periAlt / 1000,
		ecc:          ecc,
		fuelLeftSIVB: fuel[2],
		expended:     expended,
		reached:      true,
	}
}

func TestApolloStackClosedLoopAscent(t *testing.T) {
	earth, ok := loadEarth()
	if !ok {
		t.Fatal("earth not found")
	}
	const targetAlt = 185e3 // documented Apollo parking orbit

	// Apollo Stack, byte-identical to loadouts.go. Only the three Saturn
	// stages fire during ascent.
	stages := []apolloStage{
		{130000, 2160000, 35100000, 263, 8e-6}, // S-IC  (F-1 cluster, SL Isp)
		{40000, 440000, 5140000, 421, 2.5e-5},  // S-II  (J-2 cluster, vac Isp)
		{11000, 109000, 1023000, 421, 6.25e-5}, // S-IVB (J-2 single, vac Isp)
	}
	// Dead payload above the S-IVB during ascent: LM (Descent + Ascent) +
	// SM + CM, wet. From the Apollo Stack catalog entry:
	//   Descent 2500+6310, Ascent 1200+1269, SM 6000+16000, CM 5900+0.
	const payload = (2500 + 6310) + (1200 + 1269) + (6000 + 16000) + (5900 + 0.0) // 39179 kg

	// Sweep the pacing target; keep the most fuel-efficient ascent (most
	// S-IVB propellant at park) that reaches a near-circular low parking
	// orbit. The efficient frontier lofts apoapsis a little above 185 km —
	// the minimum-Δv injection carries some vertical velocity up rather
	// than burning it off to pin apoapsis exactly — so accept a tidy
	// near-circular LEO pinned to the 185 km target: both apsides within
	// ~10 km of nominal. (The old 170–210 band was wide enough that a
	// systematic guidance undershoot — which burns less fuel and so scores
	// as "more efficient" — would be selected as best; pinning near 185
	// stops a low orbit from masquerading as the winner. GH #86 #2.)
	parksClean := func(r closedLoopResult) bool {
		return r.reached && r.ecc <= 0.01 &&
			r.periKm >= 175 && r.apoKm <= 195
	}
	best := closedLoopResult{}
	cleanCount := 0
	for vt := 1800.0; vt <= 3400.0; vt += 25.0 {
		res := flyApolloStackClosedLoop(t, earth, stages, payload, vt, targetAlt, false)
		if parksClean(res) {
			cleanCount++
			if res.fuelLeftSIVB > best.fuelLeftSIVB {
				best = res
			}
		}
	}
	if !best.reached {
		t.Fatalf("no swept ascent reached a near-circular parking orbit near %.0f km", targetAlt/1000)
	}
	// The winning vTarget must not sit alone on the feasibility edge: if only
	// one sweep step parks clean, the single-step winner is at the mercy of
	// FMA/GOARCH/dt drift (the next step up or down flips feasibility), and the
	// downstream loss / TLI numbers ride a razor edge. Require a few clean runs
	// so the bands below describe a stable plateau, not a knife-edge. GH #86 #4.
	if cleanCount < 3 {
		t.Fatalf("only %d swept ascent(s) parked clean near %.0f km — winner is on the feasibility edge; widen the search or revisit guidance",
			cleanCount, targetAlt/1000)
	}

	t.Logf("Closed-loop Apollo Stack ascent to %.0f km circular (payload above S-IVB = %.0f kg):",
		targetAlt/1000, payload)
	// Re-fly the winning vTarget with phase logging.
	final := flyApolloStackClosedLoop(t, earth, stages, payload, best.vTarget, targetAlt, true)

	t.Logf("")
	t.Logf("Best vTarget = %.0f → parking orbit: apo %.2f km × peri %.2f km (e=%.4f)",
		final.vTarget, final.apoKm, final.periKm, final.ecc)
	t.Logf("  S-IVB propellant left at park: %.0f kg of %.0f (%.0f%%)",
		final.fuelLeftSIVB, stages[2].fuel, 100*final.fuelLeftSIVB/stages[2].fuel)
	t.Logf("  total engine Δv expended to orbit: %.0f m/s", final.expended)
	sivbTLI := rocketDv(stages[2].isp, stages[2].dry+final.fuelLeftSIVB+payload, stages[2].dry+payload)
	t.Logf("  S-IVB Δv remaining pushing the payload: %.0f m/s (TLI needs ≈3133)", sivbTLI)

	// Δv-loss accounting. The S-IC/S-II always dump their full tanks, so
	// every meter of ascent inefficiency is paid out of the S-IVB (it burns
	// last) and comes straight off the TLI margin. The late-hold gravity
	// turn keeps the loss low enough that the S-IVB reaches park with a
	// comfortable margin — TLI plus overhead for the plane change and part
	// of lunar capture.
	mu := earth.GravitationalParameter()
	Re := earth.RadiusMeters()
	vOrb := math.Sqrt(mu / (Re + targetAlt))
	rotBonus := physics.AtmosphereOmega(earth).Norm() * Re
	useful := vOrb - rotBonus
	loss := final.expended - useful
	const tliNeed = 3133.0
	t.Logf("  ── Δv budget ──")
	t.Logf("  useful Δv to %.0f km = v_orbit %.0f − rotation %.0f = %.0f m/s",
		targetAlt/1000, vOrb, rotBonus, useful)
	t.Logf("  gravity+drag+steering loss ≈ %.0f m/s", loss)
	t.Logf("  S-IVB at park %.0f m/s vs TLI %.0f → margin %+.0f m/s (plane change + partial capture)",
		sivbTLI, tliNeed, sivbTLI-tliNeed)

	// Efficiency guard — TWO-SIDED (GH #86 #1). The late-hold gravity turn must
	// beat the lossy open-loop schedule (~2950 m/s loss) by a clear margin
	// (upper bound), but the loss must also stay physically plausible (lower
	// bound). An upper-bound-only guard passes when a force-model regression
	// UNDER-counts ascent cost — weakened DragAccel, inflated thrust, or
	// deflated mass all drop `expended`, sending `loss` below 2500 (green) while
	// the very regression this test exists to catch goes unnoticed. The real
	// flown loss is ~2271 m/s; the [1900, 2500] band brackets it with room for
	// arch/dt drift while still flagging a force model that has gone soft.
	if loss < 1900 || loss > 2500 {
		t.Errorf("ascent loss %.0f m/s outside the plausible [1900, 2500] band — guidance is not flying an efficient gravity turn, or a force model is under-counting cost", loss)
	}
	// S-IVB TLI margin — also TWO-SIDED. The lower bound is the headline (an
	// efficient ascent leaves the S-IVB enough Δv for TLI + plane change +
	// partial capture). The upper bound is the same under-counting tripwire from
	// the other side: under-counted ascent cost leaves implausibly much S-IVB
	// propellant, inflating the TLI figure. Real is ~3428 m/s; cap at 3650.
	if sivbTLI < tliNeed || sivbTLI > 3650 {
		t.Errorf("S-IVB Δv at park %.0f m/s outside the [%.0f, 3650] band — ascent too lossy (short of TLI) or a force model is under-counting cost (implausibly high)", sivbTLI, tliNeed)
	}

	// Parking-orbit assertion: a real near-circular LEO PINNED to 185 km, not
	// merely inside a wide band (GH #86 #2). parksClean already bounds the
	// apsides to ±10 km; pin the mean altitude near nominal so a guidance
	// regression that systematically inserts low can't slip through.
	if !parksClean(final) {
		t.Errorf("not a tidy near-circular parking orbit: apo %.1f peri %.1f ecc %.4f",
			final.apoKm, final.periKm, final.ecc)
	}
	if meanAlt := (final.apoKm + final.periKm) / 2; math.Abs(meanAlt-185) > 10 {
		t.Errorf("parking orbit mean altitude %.1f km not pinned near 185 km (apo %.1f peri %.1f)",
			meanAlt, final.apoKm, final.periKm)
	}
}
