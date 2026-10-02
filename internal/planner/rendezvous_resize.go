package planner

import (
	"errors"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// ErrRendezvousShapeMismatch: the two orbits differ in shape, which the
// Rendezvous Planner does not bridge (G4 Q5: matching their orbit first is
// MechJeb-level work, out of scope). The remedy that works for a vessel
// target is to make your own orbit round with circularize [C]; [H] only
// plans transfers to bodies.
var ErrRendezvousShapeMismatch = errors.New("orbits differ in shape: circularize [C] first")

// rendezvousRoundE is the eccentricity below which an orbit counts as round
// for the different-size solve. The holder's altitude is only a single
// number for a round orbit; anything stretched is a shape mismatch.
const rendezvousRoundE = 0.01

// rendezvousResizeGrid is the number of Δv samples the root search scans.
const rendezvousResizeGrid = 700

// rendezvousResizePoint is one sample of the post-burn orbit for a tangential
// burn of the given Δv: when it first reaches the holder's altitude and how
// long the holder needs to reach that same point.
type rendezvousResizePoint struct {
	valid  bool
	dv     float64 // signed Δv along the velocity direction
	vNew   float64
	period float64 // post-burn period
	tCross float64 // coast from the burn to the first holder-altitude crossing
	t0     float64 // holder's time to reach the crossing point
}

// rendezvousLadderResize is rendezvousLadderCore's different-size case (G4
// Q4, #407): the mover's radius lies outside the holder's band, so there is
// no point on the holder's unchanged orbit to return to at r0. One
// TANGENTIAL burn at the burn point instead puts the mover on a transfer
// orbit that reaches the holder's altitude (raise: burn point is the
// periapsis side and the far side climbs to their altitude; lower: the
// reverse). The row times the rendezvous at that altitude crossing, N laps
// later: arrival T = (N-1)·P' + tCross(Δv), where P' is the post-burn period,
// and the holder (circular, never burning) must reach the crossing point at
// that same instant: T = t0(Δv) + m·P_holder for a whole m.
//
// For each lap count the residual of that equation, wrapped to one holder
// period, is scanned over Δv (from the smallest burn that reaches their
// altitude upward, in the burn's own sense) for sign changes; each bracket is
// bisected to the root and the smallest-|Δv| root wins. Numbers come from this
// solve, not from ADR 0045 §2's table (grill G4 Contradiction 7). Every row is
// then re-propagated (Kepler, both vessels) to its own arrival instant and
// gated on that miss like every other row.
//
// Round orbits only: a stretched holder has no single altitude to meet at, and
// matching a stretched mover's shape is MechJeb-level work (G4 Q5), so either
// refuses with ErrRendezvousShapeMismatch (circularize [C] first).
func rendezvousLadderResize(moverState, holderState orbital.Vec3State, hEl orbital.Elements, primary bodies.CelestialBody, mu, moverRemainingDV float64) ([]RendezvousBurnOption, error) {
	if hEl.E >= rendezvousRoundE || orbital.ElementsFromState(moverState.R, moverState.V, mu).E >= rendezvousRoundE {
		return nil, ErrRendezvousShapeMismatch
	}
	r0, v0 := moverState.R, moverState.V
	r0mag, v0mag := r0.Norm(), v0.Norm()
	if r0mag <= 0 || v0mag <= 0 {
		return nil, errRendezvousInvalidInput
	}
	v0hat := v0.Scale(1 / v0mag)
	rH := holderState.R.Norm()
	pHolder := orbitalPeriod(physics.StateVector{R: holderState.R, V: holderState.V}, mu)
	if rH <= 0 || math.IsInf(pHolder, 0) || math.IsNaN(pHolder) || pHolder <= 0 {
		return nil, errRendezvousInvalidInput
	}
	nH := holderState.R.Cross(holderState.V)
	if nH.Norm() == 0 {
		return nil, errRendezvousInvalidInput
	}
	nHhat := nH.Unit()
	hEl2 := hEl

	sign := 1.0 // raise: prograde
	if rH < r0mag {
		sign = -1 // lower: retrograde
	}

	// sample evaluates the post-burn orbit for |Δv| = s along sign.
	sample := func(s float64) rendezvousResizePoint {
		vNew := v0mag + sign*s
		pt := rendezvousResizePoint{dv: sign * s, vNew: vNew}
		if vNew <= 0 {
			return pt
		}
		post := physics.StateVector{R: r0, V: v0hat.Scale(vNew)}
		el := orbital.ElementsFromState(post.R, post.V, mu)
		if el.A <= 0 || el.E >= 1 || el.E < 1e-9 {
			return pt
		}
		period := 2 * math.Pi * math.Sqrt(el.A*el.A*el.A/mu)
		p := el.A * (1 - el.E*el.E)
		c := (p/rH - 1) / el.E
		if c > 1 && c < 1+1e-9 {
			c = 1
		} else if c < -1 && c > -1-1e-9 {
			c = -1
		}
		if c > 1 || c < -1 {
			return pt
		}
		nuNow := orbital.TrueAnomalyFromState(post.R, post.V, mu, el)
		nuX := math.Acos(c)
		tCross := math.Inf(1)
		for _, nu := range [...]float64{nuX, 2*math.Pi - nuX} {
			dt := orbital.TimeToTrueAnomaly(nuNow, math.Mod(nu, 2*math.Pi), el.A, el.E, mu)
			if dt > 1e-6 && dt < tCross {
				tCross = dt
			}
		}
		if math.IsInf(tCross, 1) {
			return pt
		}
		cross, ok := physics.KeplerStep(post, mu, tCross)
		if !ok {
			return pt
		}
		rHm := holderState.R.Norm()
		cosPhi := math.Max(-1, math.Min(1, holderState.R.Dot(cross.R)/(rHm*cross.R.Norm())))
		sinPhi := holderState.R.Cross(cross.R).Dot(nHhat) / (rHm * cross.R.Norm())
		dPhi := math.Atan2(sinPhi, cosPhi)
		if dPhi < 0 {
			dPhi += 2 * math.Pi
		}
		pt.valid = true
		pt.period = period
		pt.tCross = tCross
		pt.t0 = holderTimeToPhase(holderState, hEl2, mu, dPhi, pHolder)
		return pt
	}

	// Search range in |Δv|. Upper bound: a post-burn semi-major axis of about
	// 3× the larger radius when raising; when lowering, down to a = r0/1.8
	// (periapsis nearly at the surface; the safety gate rejects well before).
	vMax := math.Sqrt(mu * (2/r0mag - 1/(3*math.Max(r0mag, rH))))
	sMax := vMax - v0mag
	if sign < 0 {
		vMin := math.Sqrt(mu * (2/r0mag - 1/(r0mag/1.8)))
		sMax = v0mag - vMin
	}
	if sMax <= 0 {
		return nil, errRendezvousInvalidInput
	}
	// Smallest burn that reaches their altitude (validity is monotonic in s).
	if !sample(sMax).valid {
		return nil, ErrRendezvousNoCrossing
	}
	lo, hi := 0.0, sMax
	for i := 0; i < 80; i++ {
		mid := (lo + hi) / 2
		if sample(mid).valid {
			hi = mid
		} else {
			lo = mid
		}
	}
	sMin := hi
	grid := make([]rendezvousResizePoint, rendezvousResizeGrid+1)
	ss := make([]float64, rendezvousResizeGrid+1)
	for i := range grid {
		ss[i] = sMin + (sMax-sMin)*float64(i)/float64(rendezvousResizeGrid)
		grid[i] = sample(ss[i])
	}

	wrap := func(x float64) float64 {
		x = math.Mod(x+pHolder/2, pHolder)
		if x < 0 {
			x += pHolder
		}
		return x - pHolder/2
	}
	resid := func(pt rendezvousResizePoint, n int) float64 {
		return wrap(float64(n-1)*pt.period + pt.tCross - pt.t0)
	}

	rows := make([]RendezvousBurnOption, 0, len(rendezvousCandidateLaps))
	for _, n := range rendezvousCandidateLaps {
		var cands []RendezvousBurnOption
		var safeFlags, achFlags []bool
		for i := 1; i < len(grid); i++ {
			a, b := grid[i-1], grid[i]
			if !a.valid || !b.valid {
				continue
			}
			ra, rb := resid(a, n), resid(b, n)
			if ra*rb > 0 || math.Abs(ra-rb) > pHolder/2 {
				continue
			}
			l, h := ss[i-1], ss[i]
			for k := 0; k < 60; k++ {
				mid := (l + h) / 2
				pm := sample(mid)
				if !pm.valid {
					break
				}
				if resid(pm, n)*ra > 0 {
					l = mid
				} else {
					h = mid
				}
			}
			pt := sample((l + h) / 2)
			if !pt.valid {
				continue
			}
			tArr := float64(n-1)*pt.period + pt.tCross
			newV := v0hat.Scale(pt.vNew)
			moverSV, mok := physics.KeplerStep(physics.StateVector{R: r0, V: newV}, mu, tArr)
			holderSV, hok := physics.KeplerStep(physics.StateVector{R: holderState.R, V: holderState.V}, mu, tArr)
			if !mok || !hok {
				continue
			}
			cand := RendezvousBurnOption{
				Laps:         n,
				DV:           math.Abs(pt.dv),
				BurnDir:      v0hat.Scale(sign),
				TArrival:     tArr,
				AchievableCA: moverSV.R.Sub(holderSV.R).Norm(),
				ArrivalSpeed: moverSV.V.Sub(holderSV.V).Norm(),
			}
			cands = append(cands, cand)
			safeFlags = append(safeFlags, rendezvousResizeSafe(r0, v0, newV, holderState, primary, mu))
			achFlags = append(achFlags, cand.AchievableCA <= rendezvousAchievableCATolFrac*r0mag)
			if safeFlags[len(safeFlags)-1] && achFlags[len(achFlags)-1] {
				break // ascending |Δv|: the first good root is the smallest
			}
		}
		if len(cands) == 0 {
			rows = append(rows, RendezvousBurnOption{Laps: n, Reason: "no rendezvous solution"})
			continue
		}
		pick := -1
		for i := range cands {
			if safeFlags[i] && achFlags[i] {
				pick = i
				break
			}
		}
		if pick < 0 {
			pick = 0
			for i := range cands {
				if cands[i].DV < cands[pick].DV {
					pick = i
				}
			}
		}
		chosen := cands[pick]
		switch {
		case !safeFlags[pick]:
			chosen.Reason = "burn drops periapsis unsafely"
		case !achFlags[pick]:
			chosen.Reason = "no rendezvous solution"
		case moverRemainingDV >= 0 && chosen.DV > moverRemainingDV:
			chosen.Reason = "unaffordable"
		default:
			chosen.Ok = true
		}
		rows = append(rows, chosen)
	}
	return rows, nil
}

// rendezvousResizeSafe is the periapsis gate for a different-size burn: the
// post-burn periapsis must clear the primary's surface by 50 km, and may not
// fall more than 100 km below the lower of the mover's own periapsis and the
// holder's altitude (lowering to meet a lower partner is the point of the
// burn, so the pre-burn periapsis alone would refuse every lowering).
func rendezvousResizeSafe(r0, v0, newV orbital.Vec3, holderState orbital.Vec3State, primary bodies.CelestialBody, mu float64) bool {
	prePeri := orbital.ElementsFromState(r0, v0, mu).Periapsis()
	postPeri := orbital.ElementsFromState(r0, newV, mu).Periapsis()
	if primary.RadiusMeters() > 0 && postPeri < primary.RadiusMeters()+50_000.0 {
		return false
	}
	floor := math.Min(prePeri, holderState.R.Norm()) - 100_000.0
	return postPeri >= floor
}
