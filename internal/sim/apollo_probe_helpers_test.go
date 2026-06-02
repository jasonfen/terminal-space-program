package sim

// apollo_probe_helpers_test.go holds the shared ascent simulation used by
// the Apollo diagnostic probes (apollo_ascent_probe_test.go and
// apollo_lunar_budget_test.go). It flies a gravity-turn ascent of the
// Saturn-V lower stack (S-IC / S-II / S-IVB) through the real force models
// (physics.Accel + physics.DragAccel, RK4) to a circular parking orbit and
// reports the S-IVB's remaining Δv at park — the number TLI (≈3133 m/s)
// has to beat.
//
// Parameterized by `payload` (dead mass above the S-IVB) and the three
// stage specs so the budget probe can sweep mass trims and watch the
// at-park margin move. Lifted verbatim from the original inline closure in
// apollo_ascent_probe_test.go; keep the two callers in sync via this one
// helper rather than copying the integrator.

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

const apolloG0 = 9.80665

// apolloStage is one stage's mass/engine spec (dry kg, fuel kg, thrust N,
// Isp s, ballistic coefficient).
type apolloStage struct{ dry, fuel, thrust, isp, bc float64 }

// apolloAscentResult reports a single ascent run.
type apolloAscentResult struct {
	vTarget       float64 // commanded speed-pacing target
	remain        float64 // S-IVB Δv remaining at park, pushing the payload
	apoKm, periKm float64 // parked orbit
	fuelLeftSIVB  float64 // kg of S-IVB propellant left at park
	maxApoKm      float64 // peak osculating apoapsis during ascent
	expended      float64 // total engine Δv imparted reaching orbit
	reached       bool    // true if a park was achieved before running dry
}

// loadEarth returns the Earth body from the embedded systems.
func loadEarth() (bodies.CelestialBody, bool) {
	systems, _, err := bodies.LoadAllWithWarnings()
	if err != nil {
		return bodies.CelestialBody{}, false
	}
	for _, sys := range systems {
		if cb, ok := bodies.LookupByID([]bodies.System{sys}, "earth"); ok {
			return cb, true
		}
	}
	return bodies.CelestialBody{}, false
}

// flyApolloAscent flies the gravity-turn ascent of `stages` (exactly three:
// S-IC, S-II, S-IVB) carrying `payload` dead mass, pacing the pitch program
// by surface speed against vTarget, to a `targetAltM` circular park. It
// returns the S-IVB's remaining Δv pushing the payload.
func flyApolloAscent(earth bodies.CelestialBody, stages []apolloStage, payload, vTarget, targetAltM float64) apolloAscentResult {
	mu := earth.GravitationalParameter()
	Re := earth.RadiusMeters()
	rTarget := Re + targetAltM
	sivbDryFloor := stages[2].dry + payload

	omega := physics.AtmosphereOmega(earth)
	spin := omega.Unit()
	seed := orbital.Vec3{X: 1}
	if math.Abs(spin.Dot(seed)) > 0.9 {
		seed = orbital.Vec3{Y: 1}
	}
	upHat0 := seed.Sub(spin.Scale(seed.Dot(spin))).Unit()
	r0 := upHat0.Scale(Re)
	v0 := omega.Cross(r0)

	const (
		dt     = 0.2
		hStart = 1000.0
	)
	fuel := []float64{stages[0].fuel, stages[1].fuel, stages[2].fuel}
	active := 0
	r, v := r0, v0
	maxApo := 0.0
	expendedDv := 0.0
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
		eastHat := spin.Cross(r).Unit()

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

		vUp := v.Dot(upHat)
		horizontalNow := !math.IsNaN(apo) && apo >= rTarget*0.98 && vUp < 250
		var thrustDir orbital.Vec3
		switch {
		case alt < hStart:
			thrustDir = upHat
		case horizontalNow:
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

		if !math.IsNaN(peri) && peri >= rTarget-5e3 && alt > 100e3 {
			break
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

		expendedDv += (th / m) * dt

		mdot := th / (isp * apolloG0)
		fuel[active] -= mdot * dt
		for active < len(stages)-1 && fuel[active] <= 0 {
			fuel[active] = 0
			active++
		}
		if active == len(stages)-1 && fuel[active] <= 0 {
			return apolloAscentResult{vTarget: vTarget, reached: false, maxApoKm: maxApo}
		}
	}

	// Coast to apoapsis.
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

	// Circularize at apoapsis from the S-IVB.
	rApo := r.Norm()
	sp := physics.StateVector{R: r, V: v, M: totalMass()}
	a := physics.SemimajorAxis(sp, mu)
	vApo := math.Sqrt(mu * (2/rApo - 1/a))
	vCirc := math.Sqrt(mu / rApo)
	dvCirc := vCirc - vApo
	if dvCirc < 0 {
		dvCirc = 0
	}
	m0 := totalMass()
	mAfter := m0 * math.Exp(-dvCirc/(stages[active].isp*apolloG0))
	fuel[active] -= (m0 - mAfter)
	if fuel[active] < 0 {
		return apolloAscentResult{vTarget: vTarget, reached: false, apoKm: (rApo - Re) / 1000, maxApoKm: maxApo}
	}

	mNow := stages[2].dry + fuel[2] + payload
	remain := stages[2].isp * apolloG0 * math.Log(mNow/sivbDryFloor)
	return apolloAscentResult{
		vTarget: vTarget, remain: remain, reached: true,
		apoKm: (rApo - Re) / 1000, periKm: (rApo - Re) / 1000,
		fuelLeftSIVB: fuel[2], maxApoKm: maxApo, expended: expendedDv + dvCirc,
	}
}

// bestApolloAscent sweeps vTarget and returns the run that leaves the S-IVB
// the most Δv at park (the best clean ascent for a given mass config).
func bestApolloAscent(earth bodies.CelestialBody, stages []apolloStage, payload, targetAltM float64) apolloAscentResult {
	best := apolloAscentResult{remain: -1}
	for vt := 1400.0; vt <= 3200.0; vt += 100.0 {
		res := flyApolloAscent(earth, stages, payload, vt, targetAltM)
		if res.reached && res.remain > best.remain {
			best = res
		}
	}
	return best
}

// rocketDv returns the rocket-equation Δv to drop from m0 to m1.
func rocketDv(isp, m0, m1 float64) float64 {
	if m1 <= 0 || m0 <= m1 {
		return 0
	}
	return isp * apolloG0 * math.Log(m0/m1)
}
