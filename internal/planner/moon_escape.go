package planner

import (
	"errors"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// PlanMoonEscape constructs a two-impulse Moon Return: a single targeted
// departure (a full-3D BurnVector) that injects the craft from a moon
// parking orbit onto a parent-frame transfer whose perigee reaches a
// chosen target, plus a zero-Δv arrival marker at the SOI-exit moment.
//
// ADR 0013 replaced the v0.6.3 "minimum kiss-the-SOI escape" objective
// (a prograde impulse sized so the transfer apolune merely touched the
// moon's SOI, leaving the craft ≈ at rest relative to the moon and
// inheriting roughly the moon's own parent orbit). The targeted return
// instead leaves the SOI with a deliberate excess velocity v∞ aimed
// *retrograde to the moon's orbital motion, in the moon's orbital
// plane*, sized so the inherited parent-frame orbit drops to a usable
// perigee — folding the perigee-lowering work into the deep-in-the-well
// departure (Oberth) rather than paying for it separately later.
//
// Construction (ADR 0013 Decisions C/D):
//   - v∞ direction is analytic: −v̂_moon (retrograde, in the moon's
//     orbital plane). Because the target plane is the moon's own plane —
//     true at any departure time — there is no around-parent phasing
//     wait, only a short intra-moon wait for the parking-orbit point
//     where a prograde burn aims v∞ correctly.
//   - |v∞| is solved (1-D) so the analytic parent-frame perigee equals
//     rTargetPeri. The arrival inclination is whatever the moon's plane
//     is; only the perigee and the prograde-around-parent sense are
//     controlled (the cheap, controllable goal).
//   - The departure is a BurnVector: the escape-hyperbola periapsis
//     velocity (vEsc = √(v∞² + 2µ/r_park)) aimed prograde in the moon's
//     orbital plane at the periapsis direction whose outgoing asymptote
//     is −v̂_moon. Any tilt between the parking orbit and the moon's
//     orbital plane is folded into that one impulse (Δv = v_post − v_park).
//
// The arrival node stays a zero-Δv frame marker at the SOI-exit moment;
// the player plants their own capture/aerobrake manually (the expensive
// parent-capture term is theirs to choose — ADR 0013 Decision A).
//
// Inputs:
//   - muMoon, muParent: GM of the moon and of its parent.
//   - craftR, craftV: craft state in the moon's (inertially-oriented)
//     frame at plant time. |craftR| is the parking radius; the velocity
//     fixes the parking-orbit plane and motion sense.
//   - moonR, moonV: the moon's state relative to its parent at departure
//     (from the ephemeris Calculator). Fixes the moon's orbital plane,
//     motion direction, and parent distance.
//   - rSOI: the moon's sphere-of-influence radius (arrival-marker timing).
//   - rTargetPeri: desired parent-frame perigee (e.g. parent radius + 200 km).
//   - minLeadSeconds: pad the departure offset by whole parking-orbit
//     periods until it is ≥ this, so a centered finite burn fits ahead of
//     "now". Pass 0 to skip padding.
//   - moonID, parentID: PrimaryIDs so the HUD renders the departure in the
//     moon's frame and the arrival marker in the parent's frame.
func PlanMoonEscape(
	muMoon, muParent float64,
	craftR, craftV orbital.Vec3,
	moonR, moonV orbital.Vec3,
	rSOI, rTargetPeri float64,
	minLeadSeconds float64,
	moonID, parentID string,
) (TransferPlan, error) {
	rPark := craftR.Norm()
	if muMoon <= 0 || muParent <= 0 || rPark <= 0 || rSOI <= 0 {
		return TransferPlan{}, errors.New("planmoonescape: muMoon, muParent, rPark, rSOI must be positive")
	}
	if rSOI <= rPark {
		return TransferPlan{}, errors.New("planmoonescape: SOI radius must exceed parking radius")
	}
	rMoonDist := moonR.Norm()
	vMoonSpeed := moonV.Norm()
	if rMoonDist <= 0 || vMoonSpeed <= 0 {
		return TransferPlan{}, errors.New("planmoonescape: degenerate moon state")
	}
	if rTargetPeri <= 0 || rTargetPeri >= rMoonDist {
		return TransferPlan{}, errors.New("planmoonescape: target perigee must be positive and below the moon's parent distance")
	}

	nMoonHat := moonR.Cross(moonV)
	if nMoonHat.Norm() == 0 {
		return TransferPlan{}, errors.New("planmoonescape: degenerate moon orbital plane")
	}
	nMoonHat = nMoonHat.Unit()
	dHat := moonV.Unit().Scale(-1) // v∞ direction: retrograde to moon's motion, in its plane

	// ── 1-D solve |v∞| so the parent-frame perigee hits rTargetPeri. ──
	// vExit = vMoon + v∞·d̂; perigee decreases monotonically as |v∞| grows
	// (more retrograde → lower tangential speed at the moon's distance).
	perigeeForVInf := func(vInf float64) float64 {
		return parentPerigee(moonR, moonV.Add(dHat.Scale(vInf)), muParent)
	}
	lo, hi := 0.0, vMoonSpeed*0.999
	if perigeeForVInf(hi) > rTargetPeri {
		// Even a near-stop at the moon's distance can't drop the perigee to
		// the target — unreachable with a coplanar retrograde injection.
		return TransferPlan{}, errors.New("planmoonescape: target perigee unreachable")
	}
	for i := 0; i < 80; i++ {
		mid := (lo + hi) / 2
		if perigeeForVInf(mid) > rTargetPeri {
			lo = mid
		} else {
			hi = mid
		}
	}
	vInf := (lo + hi) / 2

	// ── Escape-hyperbola geometry in the moon's frame. ──
	vCirc := math.Sqrt(muMoon / rPark)
	vEsc := math.Sqrt(vInf*vInf + 2*muMoon/rPark) // periapsis speed
	e := 1 + rPark*vInf*vInf/muMoon
	nuInf := math.Acos(-1 / e) // true anomaly of the asymptote

	// Match the escape's rotation sense to the parking orbit's so the burn
	// is prograde-ish (cheap) rather than a reversal.
	s := 1.0
	if craftR.Cross(craftV).Dot(nMoonHat) < 0 {
		s = -1.0
	}
	// Periapsis direction whose outgoing asymptote (at +s·νInf) is d̂.
	pHat := orbital.Rotate(dHat, nMoonHat, -s*nuInf)
	// Prograde velocity direction at periapsis, in the moon's orbital plane.
	vPeriHat := orbital.Rotate(pHat, nMoonHat, s*math.Pi/2).Unit()

	// Plant point: the craft-parking-orbit direction closest to pHat (which
	// may be out of the parking plane for an inclined parking orbit). The
	// impulse folds the residual plane change.
	nCraft := craftR.Cross(craftV)
	if nCraft.Norm() == 0 {
		return TransferPlan{}, errors.New("planmoonescape: degenerate parking orbit")
	}
	nCraftHat := nCraft.Unit()
	pInPlane := pHat.Sub(nCraftHat.Scale(pHat.Dot(nCraftHat)))
	if pInPlane.Norm() == 0 {
		// Parking plane is perpendicular to the periapsis direction — a
		// polar/degenerate geometry ADR 0013 excludes from v1.
		return TransferPlan{}, errors.New("planmoonescape: parking orbit too steep for a single-impulse return")
	}
	plantHat := pInPlane.Unit()
	craftProHat := nCraftHat.Cross(plantHat).Unit() // prograde velocity dir at the plant point

	// Departure Δv: from the (circular-parking) velocity at the plant point
	// to the in-plane escape velocity. Folds the plane change in.
	vPost := vPeriHat.Scale(vEsc)
	dvVec := vPost.Sub(craftProHat.Scale(vCirc))
	dvDep := dvVec.Norm()
	if dvDep == 0 || math.IsNaN(dvDep) {
		return TransferPlan{}, errors.New("planmoonescape: degenerate departure Δv")
	}

	// ── Phasing: wait for the craft to reach the plant direction. ──
	nMean := math.Sqrt(muMoon / (rPark * rPark * rPark))
	sweep := signedAngleInPlane(craftR.Unit(), plantHat, nCraftHat)
	if sweep < 0 {
		sweep += 2 * math.Pi
	}
	waitSeconds := sweep / nMean
	period := 2 * math.Pi / nMean
	for minLeadSeconds > 0 && waitSeconds < minLeadSeconds {
		waitSeconds += period
	}

	// ── Coast time to SOI exit (hyperbolic TOF from periapsis). ──
	tof := hyperbolicTimeToRadius(muMoon, vInf, e, rSOI)

	depOffset := time.Duration(waitSeconds * float64(time.Second))
	return TransferPlan{
		Departure: TransferNode{
			Leg:        LegDeparture,
			PrimaryID:  moonID,
			DV:         dvDep,
			OffsetTime: depOffset,
			BurnDir:    dvVec.Scale(1 / dvDep),
		},
		Arrival: TransferNode{
			Leg:        LegArrival,
			PrimaryID:  parentID,
			DV:         0,
			OffsetTime: depOffset + time.Duration(tof*float64(time.Second)),
		},
		TransferDt: time.Duration(tof * float64(time.Second)),
	}, nil
}

// parentPerigee returns the perigee radius of the orbit defined by state
// (r, v) about a primary with gravitational parameter mu. For a hyperbolic
// or parabolic state (energy ≥ 0) a is negative and the returned value is
// still a·(1−e), the (positive) periapsis radius.
func parentPerigee(r, v orbital.Vec3, mu float64) float64 {
	rN := r.Norm()
	energy := v.Dot(v)/2 - mu/rN
	a := -mu / (2 * energy)
	h := r.Cross(v).Norm()
	ecc := math.Sqrt(math.Max(0, 1+2*energy*h*h/(mu*mu)))
	return a * (1 - ecc)
}

// signedAngleInPlane returns the angle (rad, in (−π, π]) from `from` to `to`
// measured about `axis` (right-handed). Both inputs should be unit vectors
// lying in the plane normal to axis.
func signedAngleInPlane(from, to, axis orbital.Vec3) float64 {
	return math.Atan2(from.Cross(to).Dot(axis), from.Dot(to))
}

// hyperbolicTimeToRadius returns the coast time (s) from periapsis to
// radius rTarget on an escape hyperbola about a primary with the given mu,
// excess speed vInf, and eccentricity e. Used to time the zero-Δv SOI-exit
// arrival marker.
func hyperbolicTimeToRadius(mu, vInf, e, rTarget float64) float64 {
	if vInf <= 0 || e <= 1 {
		return 0
	}
	a := mu / (vInf * vInf) // |a| for the hyperbola
	p := a * (e*e - 1)
	cosNu := (p/rTarget - 1) / e
	cosNu = math.Max(-1, math.Min(1, cosNu))
	// Hyperbolic anomaly F at the target true anomaly, then Kepler's
	// hyperbolic equation M = e·sinh F − F.
	coshF := (e + cosNu) / (1 + e*cosNu)
	if coshF < 1 {
		coshF = 1
	}
	f := math.Acosh(coshF)
	m := e*math.Sinh(f) - f
	nHyp := math.Sqrt(mu / (a * a * a))
	if nHyp == 0 {
		return 0
	}
	return m / nHyp
}
