package sim

// apollo_ascent_probe_test.go is a DIAGNOSTIC harness (not a CI assertion):
// it flies a gravity-turn ascent of the Apollo Stack through the real
// force models (physics.Accel + physics.DragAccel, RK4, real catalog
// masses / Isp / ballistic coefficients, serial staging) to a 200 km
// circular parking orbit and reports the S-IVB's remaining Δv at park —
// the number that decides whether TLI (≈3133 m/s) can complete.
//
// Run: go test ./internal/sim -run TestApolloAscentBudgetProbe -v
// It always passes; read the t.Log output.

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

func TestApolloAscentBudgetProbe(t *testing.T) {
	const g0 = 9.80665
	systems, _, err := bodies.LoadAllWithWarnings()
	if err != nil {
		t.Fatalf("load bodies: %v", err)
	}
	var earth bodies.CelestialBody
	found := false
	for _, sys := range systems {
		if cb, ok := bodies.LookupByID([]bodies.System{sys}, "earth"); ok {
			earth, found = cb, true
			break
		}
	}
	if !found {
		t.Fatal("earth not found")
	}
	mu := earth.GravitationalParameter()
	Re := earth.RadiusMeters()
	targetAlt := 200e3
	rTarget := Re + targetAlt

	// Apollo-Stack stages bottom→top (from loadouts.go, byte-identical).
	type stg struct{ dry, fuel, thrust, isp, bc float64 }
	stages := []stg{
		{130000, 2160000, 35100000, 263, 8e-6},  // S-IC
		{40000, 440000, 5140000, 421, 2.5e-5},    // S-II
		{11000, 109000, 1023000, 421, 6.25e-5},   // S-IVB
	}
	const payload = 45300.0 // LM(Descent+Ascent) + CSM wet, dead mass above S-IVB
	sivbDryFloor := stages[2].dry + payload

	// Launch basis: equatorial point so all motion stays in the equatorial
	// plane (spin axis is the plane normal) and the real tilted DragAccel
	// frame stays consistent. v0 = surface co-rotation (rotation boost).
	omega := physics.AtmosphereOmega(earth)
	spin := omega.Unit()
	seed := orbital.Vec3{X: 1}
	if math.Abs(spin.Dot(seed)) > 0.9 {
		seed = orbital.Vec3{Y: 1}
	}
	upHat0 := seed.Sub(spin.Scale(seed.Dot(spin))).Unit()
	r0 := upHat0.Scale(Re)
	v0 := omega.Cross(r0)

	type result struct {
		kickDeg, remain, apoKm, periKm, fuelLeftSIVB, maxApoKm, expended float64
		reached                                                          bool
	}

	// vTarget paces the commanded pitch program by SURFACE SPEED (an
	// efficient gravity turn): pitch from vertical ramps 0→90° as surface
	// speed climbs 0→vTarget, so the vehicle tips over as it accelerates
	// rather than as it climbs (altitude-paced lofts and wastes Δv).
	flyAscent := func(vTarget float64) result {
		const (
			dt     = 0.2
			hStart = 1000.0 // vertical rise until this altitude (m) to clear pad + build speed
		)
		fuel := []float64{stages[0].fuel, stages[1].fuel, stages[2].fuel}
		active := 0
		r, v := r0, v0
		maxApo := 0.0
		expendedDv := 0.0
		nextLog := 10.0
		// Set traceVTarget to a swept vTarget value to dump per-20s ascent
		// telemetry for that single run (altitude / vUp / vHor / apo / peri /
		// stage / expended Δv). -1 disables. Handy for re-debugging guidance.
		const traceVTarget = -1.0
		debug := traceVTarget > 0 && math.Abs(vTarget-traceVTarget) < 1
		totalMass := func() float64 {
			m := payload
			for i := active; i < len(stages); i++ {
				m += stages[i].dry + fuel[i]
			}
			return m
		}

		for tt := 0.0; tt < 1400; tt += dt {
			alt := r.Norm() - Re
			upHat := r.Unit()
			// instantaneous "east": prograde-horizontal in the equatorial plane.
			eastHat := spin.Cross(r).Unit()

			// Projected apoapsis / perigee of the current osculating orbit.
			sp := physics.StateVector{R: r, V: v, M: totalMass()}
			a := physics.SemimajorAxis(sp, mu)
			h := r.Cross(v).Norm()
			eps := physics.SpecificEnergy(sp, mu)
			ecc := math.Sqrt(math.Max(0, 1+2*eps*h*h/(mu*mu)))
			apo, peri := math.NaN(), math.NaN()
			if a > 0 {
				apo = a * (1 + ecc)
				peri = a * (1 - ecc)
				if (apo-Re)/1000 > maxApo {
					maxApo = (apo - Re) / 1000
				}
			}

			// Guidance:
			//  1. vertical rise to clear the pad,
			//  2. speed-paced pitch program (gravity turn) UNTIL the projected
			//     apoapsis reaches the target — this stops the loft early,
			//  3. then thrust pure horizontal-prograde to raise perigee to the
			//     target (efficient powered insertion, fuel → orbital speed).
			vUp := v.Dot(upHat)
			// Begin insertion once near the top of the arc: apoapsis at/above
			// target and the climb has nearly arrested (small vUp). Before
			// that, fly the gravity-turn pitch program.
			horizontalNow := !math.IsNaN(apo) && apo >= rTarget*0.98 && vUp < 250
			var thrustDir orbital.Vec3
			switch {
			case alt < hStart:
				thrustDir = upHat
			case horizontalNow:
				// Level-flight insertion: pitch above horizontal so the
				// vertical thrust cancels net vertical accel (gravity −
				// centrifugal) AND damps any residual vertical speed to zero,
				// holding altitude while the rest of thrust builds orbital
				// velocity. As v_h → orbital the needed pitch → 0.
				rMag := r.Norm()
				vHorVec := v.Sub(upHat.Scale(vUp))
				horHat := eastHat
				if vHorVec.Norm() > 1 {
					horHat = vHorVec.Unit()
				}
				aGrav := mu / (rMag * rMag)
				aCentrifugal := vHorVec.Norm() * vHorVec.Norm() / rMag
				aT := stages[active].thrust / totalMass()
				aV := (aGrav - aCentrifugal) - vUp*0.1 // null vertical speed
				sinPhi := math.Max(-1, math.Min(1, aV/aT))
				phi := math.Asin(sinPhi)
				thrustDir = horHat.Scale(math.Cos(phi)).Add(upHat.Scale(math.Sin(phi)))
			default:
				vrelSpeed := physics.AirRelativeVelocity(r, v, earth).Norm()
				pitchDeg := 90.0 * vrelSpeed / vTarget
				if pitchDeg > 89.0 {
					pitchDeg = 89.0
				}
				p := pitchDeg * math.Pi / 180
				thrustDir = upHat.Scale(math.Cos(p)).Add(eastHat.Scale(math.Sin(p)))
			}

			// MECO when the orbit is circular at/above target: perigee raised
			// to within 5 km of target radius.
			if !math.IsNaN(peri) && peri >= rTarget-5e3 && alt > 100e3 {
				break
			}

			if debug && tt >= nextLog {
				vUp := v.Dot(upHat)
				vHor := v.Sub(upHat.Scale(vUp)).Norm()
				t.Logf("    t=%4.0fs alt=%6.1fkm vUp=%5.0f vHor=%5.0f apo=%6.0f peri=%7.0f stage=%d expΔv=%5.0f horiz=%v",
					tt, alt/1000, vUp, vHor, (apo-Re)/1000, (peri-Re)/1000, active, expendedDv, horizontalNow)
				nextLog += 20.0
			}

			m := totalMass()
			th := stages[active].thrust
			isp := stages[active].isp
			bc := stages[active].bc
			aThrust := thrustDir.Scale(th / m)
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

			expendedDv += (th / m) * dt // engine Δv imparted this step

			// Burn fuel; stage when active tank empties.
			mdot := th / (isp * g0)
			fuel[active] -= mdot * dt
			for active < len(stages)-1 && fuel[active] <= 0 {
				fuel[active] = 0
				active++
			}
			if active == len(stages)-1 && fuel[active] <= 0 {
				return result{kickDeg: vTarget, reached: false, maxApoKm: maxApo,
					apoKm: (apo - Re) / 1000, periKm: (peri - Re) / 1000} // ran dry before orbit
			}
		}

		// Coast (gravity+drag, no thrust) to apoapsis: until radial velocity ≤ 0.
		for i := 0; i < 200000; i++ {
			vr := v.Dot(r.Unit())
			if vr <= 0 {
				break
			}
			m := totalMass()
			bc := stages[active].bc
			accelFn := func(rr, vv orbital.Vec3, _ float64) orbital.Vec3 {
				return physics.Accel(rr, mu).Add(physics.DragAccel(rr, vv, earth, bc))
			}
			ns := physics.StepRK4(physics.StateVector{R: r, V: v, M: m}, dt, accelFn, 0)
			r, v = ns.R, ns.V
		}

		rApo := r.Norm()
		// Circularize at apoapsis: prograde Δv = v_circ − v_apo.
		sp := physics.StateVector{R: r, V: v, M: totalMass()}
		a := physics.SemimajorAxis(sp, mu)
		vApo := math.Sqrt(mu * (2/rApo - 1/a))
		vCirc := math.Sqrt(mu / rApo)
		dvCirc := vCirc - vApo
		if dvCirc < 0 {
			dvCirc = 0
		}
		// Spend circularization fuel from the active (S-IVB) stage.
		m0 := totalMass()
		mAfter := m0 * math.Exp(-dvCirc/(stages[active].isp*g0))
		fuel[active] -= (m0 - mAfter)
		if fuel[active] < 0 {
			// circularization itself ran the stage dry
			return result{kickDeg: vTarget, reached: false, apoKm: (rApo - Re) / 1000, maxApoKm: maxApo}
		}

		// Remaining S-IVB Δv pushing the LM+CSM payload.
		mNow := stages[2].dry + fuel[2] + payload
		remain := stages[2].isp * g0 * math.Log(mNow/sivbDryFloor)

		// Report the parked orbit (apo/peri) after circularization.
		spc := physics.StateVector{R: r, V: v.Scale(vCirc / v.Norm()), M: mNow}
		ac := physics.SemimajorAxis(spc, mu)
		_ = ac
		return result{
			kickDeg: vTarget, remain: remain, reached: true,
			apoKm: (rApo - Re) / 1000, periKm: (rApo - Re) / 1000,
			fuelLeftSIVB: fuel[2], maxApoKm: maxApo, expended: expendedDv + dvCirc,
		}
	}

	best := result{remain: -1}
	t.Logf("Apollo Stack gravity-turn ascent to %.0f km circular (real force models):", targetAlt/1000)
	t.Logf("  full S-IVB Δv (pushing LM+CSM) = %.0f m/s; TLI from 200 km needs ≈ 3133 m/s", stages[2].isp*g0*math.Log((stages[2].dry+stages[2].fuel+payload)/sivbDryFloor))
	for vt := 1400.0; vt <= 3200.0; vt += 100.0 {
		res := flyAscent(vt)
		if res.reached {
			t.Logf("  vTarget %5.0f → park %6.1f km  S-IVB remaining Δv = %6.0f m/s  (TLI margin %+6.0f)", vt, res.apoKm, res.remain, res.remain-3133)
			if res.remain > best.remain {
				best = res
			}
		} else {
			t.Logf("  vTarget %5.0f → FAILED (ran dry; orbit apo %.0f / peri %.0f km, peak apo %.0f)", vt, res.apoKm, res.periKm, res.maxApoKm)
		}
	}
	if best.remain < 0 {
		t.Logf("RESULT: no swept ascent reached a 200 km park — stack is short before TLI even begins.")
		return
	}
	vOrb := math.Sqrt(mu / rTarget)
	rotBonus := v0.Norm()
	losses := best.expended - (vOrb - rotBonus)
	t.Logf("BEST ASCENT (vTarget=%.0f): park %.0f km, S-IVB has %.0f m/s remaining.", best.kickDeg, best.apoKm, best.remain)
	t.Logf("  ascent expended %.0f m/s to orbit; v_orbit=%.0f, rotation bonus=%.0f → gravity+drag+steering loss ≈ %.0f m/s",
		best.expended, vOrb, rotBonus, losses)
	t.Logf("  (textbook-optimal loss for a TWR≈1.2 vehicle is ~1600–1800 m/s; excess above that is this harness's open-loop guidance, not the stack)")
	t.Logf("  TLI needs ≈3133 m/s → margin %+.0f m/s (%s).", best.remain-3133,
		map[bool]string{true: "MAKES IT", false: "SHORT by this ascent"}[best.remain >= 3133])
}
