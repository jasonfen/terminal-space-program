package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// launch_window.go, the pad Launch Window (#460, grill G6): when the
// rotating pad next sweeps through the target's orbital plane, and the
// heading that pass wants. The pad passes through a fixed plane when
// n_T . r(t) = 0 and r(t) rotates rigidly about the spin axis, so there
// are two roots per rotation when |lat| <= the plane's tilt to the
// equator, none otherwise. The root is bracketed on production position
// samples (render.BodyFixedToWorld, the pad the integrator pins) and
// bisected; the heading at the pass comes from the launch-azimuth
// identity sin(az) = cos(i_T) / cos(lat) and is then CHOSEN among the
// four candidate bearings by the production plane normal
// (spacecraft.HeadingOrbitNormal), never trusted on the identity alone.
//
// Time does not move with the commanded heading (G6 Q1): the row counts
// to a pass and names the heading that pass wants. With no pass (the
// pad's latitude is above the plane's tilt) the best Δincl due east
// (or west, for a retrograde target) and when it comes is reported
// instead (Q3).

// LaunchWindow is one solve of the pad window for the active Landed
// vessel and its bound target's plane.
type LaunchWindow struct {
	// Open: a pass exists. Always: the whole pad circle lies in the
	// target's plane (an equatorial pad under an equatorial target), the
	// window never closes and NextPass is zero.
	Open, Always bool
	// PassAt is the absolute sim time of the next pass (Open). HeadingDeg
	// is the compass bearing, [0,360), whose launch plane is the
	// target's at that instant and direction of travel.
	PassAt     time.Time
	HeadingDeg float64
	// When !Open: the least plane angle any rotation instant reaches for
	// the target-direction heading (due east for a prograde target), and
	// when.
	BestDeg float64
	BestAt  time.Time
	// padDir is the pad's unit position at PassAt (Open), kept so the
	// lead at the pass is one Kepler step, not a second solve.
	padDir orbital.Vec3
	nT     orbital.Vec3
}

// NextPass is the countdown to the pass (Open) or to the best moment
// (!Open), measured from now. Zero for an Always window.
func (lw LaunchWindow) NextPass(now time.Time) time.Duration {
	if lw.Always {
		return 0
	}
	at := lw.BestAt
	if lw.Open {
		at = lw.PassAt
	}
	return at.Sub(now)
}

type launchWindowKey struct {
	craftID  uint64
	primary  string
	lat, lon float64
	kind     TargetKind
	targetID uint64
	bodyIdx  int
	ghost    string
	nT       [3]int64
}

type launchWindowCache struct {
	valid  bool
	key    launchWindowKey
	val    LaunchWindow
	solves int
}

// LaunchWindowSolves is the number of times the solver has run on this
// World (the call-count the cache test asserts on).
func (w *World) LaunchWindowSolves() int { return w.lwCache.solves }

func quantNormal(n orbital.Vec3) [3]int64 {
	return [3]int64{int64(math.Round(n.X * 1e7)), int64(math.Round(n.Y * 1e7)), int64(math.Round(n.Z * 1e7))}
}

// launchWindowTargetNormal returns the unit normal of the bound
// target's plane, or false when there is nothing to launch into: no
// target, a landed vessel target (its "plane" is the co-rotation
// pseudo-orbit), or a degenerate / pole-guarded normal.
func (w *World) launchWindowTargetNormal() (orbital.Vec3, bool) {
	switch w.Target.Kind {
	case TargetBody:
		sys := w.System()
		if w.Target.BodyIdx <= 0 || w.Target.BodyIdx >= len(sys.Bodies) {
			return orbital.Vec3{}, false
		}
		n := orbital.OrbitNormalWorld(sys.Bodies[w.Target.BodyIdx])
		if n.Norm() == 0 {
			return orbital.Vec3{}, false
		}
		return n.Unit(), true
	case TargetCraft:
		if t, _, ok := w.craftByID(w.Target.CraftID); ok && (t.Landed || t.Crashed) {
			return orbital.Vec3{}, false
		}
		fallthrough
	case TargetGhost:
		n, ok := w.TargetPlaneNormal()
		if !ok {
			return orbital.Vec3{}, false
		}
		return n.Unit(), true
	}
	return orbital.Vec3{}, false
}

// LaunchWindow returns the active Landed vessel's pad window against
// its bound target's plane. ok is false when the vessel is not on the
// pad, there is no usable target plane, the primary does not spin, or
// the pad is at a pole (the plane normal is not trustworthy there,
// ADR 0050 decision 8). Cached: the solve reruns only when the vessel,
// pad, target or target plane changes, or the cached pass has gone by.
func (w *World) LaunchWindow() (LaunchWindow, bool) {
	c := w.ActiveCraft()
	if c == nil || !c.Landed || c.Crashed {
		return LaunchWindow{}, false
	}
	nT, ok := w.launchWindowTargetNormal()
	if !ok {
		return LaunchWindow{}, false
	}
	omegaR := render.BodySpinOmegaWorld(c.Primary)
	omega := orbital.Vec3{X: omegaR.X, Y: omegaR.Y, Z: omegaR.Z}
	if omega.Norm() == 0 {
		return LaunchWindow{}, false
	}
	if !orbital.PlaneNormalOK(c.State.R.Cross(c.State.V), omega.Norm(), c.Primary.RadiusMeters()) {
		return LaunchWindow{}, false
	}
	lat, lon := c.SurfaceLatLon()
	key := launchWindowKey{
		craftID: c.ID, primary: c.Primary.ID, lat: lat, lon: lon,
		kind: w.Target.Kind, targetID: w.Target.CraftID, bodyIdx: w.Target.BodyIdx,
		ghost: w.Target.GhostOwner, nT: quantNormal(nT),
	}
	now := w.Clock.SimTime
	cc := &w.lwCache
	if cc.valid && cc.key == key {
		at := cc.val.BestAt
		if cc.val.Open {
			at = cc.val.PassAt
		}
		if cc.val.Always || now.Before(at) {
			return cc.val, true
		}
	}
	cc.solves++
	lw := solveLaunchWindow(c.Primary, lat, lon, nT, now)
	cc.valid, cc.key, cc.val = true, key, lw
	return lw, true
}

// foldedPlaneAngleDeg is the plane angle between two normals folded to
// [0, 90] (prograde and retrograde in one plane both read 0).
func foldedPlaneAngleDeg(a, b orbital.Vec3) float64 {
	na, nb := a.Norm(), b.Norm()
	if na == 0 || nb == 0 {
		return 0
	}
	cos := math.Abs(a.Dot(b)) / (na * nb)
	if cos > 1 {
		cos = 1
	}
	return math.Acos(cos) * 180 / math.Pi
}

func solveLaunchWindow(primary bodies.CelestialBody, latDeg, lonDeg float64, nT orbital.Vec3, now time.Time) LaunchWindow {
	spinR := render.BodyRotationAxisWorld(primary)
	spin := orbital.Vec3{X: spinR.X, Y: spinR.Y, Z: spinR.Z}
	omegaR := render.BodySpinOmegaWorld(primary)
	omegaMag := math.Sqrt(omegaR.X*omegaR.X + omegaR.Y*omegaR.Y + omegaR.Z*omegaR.Z)
	period := 2 * math.Pi / omegaMag
	radius := primary.RadiusMeters()

	pos := func(t time.Time) orbital.Vec3 {
		d := render.BodyFixedToWorld(primary, latDeg, lonDeg, t)
		return orbital.Vec3{X: d.X, Y: d.Y, Z: d.Z}
	}
	f := func(t time.Time) float64 { return nT.Dot(pos(t)) }
	at := func(s float64) time.Time { return now.Add(time.Duration(s * float64(time.Second))) }

	const samples = 720
	step := period / samples
	lw := LaunchWindow{nT: nT}

	// Bracket the first sign change of f strictly after now.
	maxAbs := 0.0
	prevS, prevF := 0.0, f(now)
	maxAbs = math.Abs(prevF)
	rootS, found := 0.0, false
	for i := 1; i <= samples+1; i++ {
		s := float64(i) * step
		fv := f(at(s))
		maxAbs = math.Max(maxAbs, math.Abs(fv))
		if !found && (prevF < 0) != (fv < 0) && prevF != 0 {
			lo, hi, flo := prevS, s, prevF
			for k := 0; k < 50; k++ {
				mid := 0.5 * (lo + hi)
				fm := f(at(mid))
				if (flo < 0) == (fm < 0) {
					lo, flo = mid, fm
				} else {
					hi = mid
				}
			}
			rootS, found = 0.5*(lo+hi), true
		}
		prevS, prevF = s, fv
	}

	cosI := nT.Dot(spin)
	cosLat := math.Cos(latDeg * math.Pi / 180)
	switch {
	case maxAbs < 1e-6:
		lw.Open, lw.Always = true, true
		lw.PassAt = now
		lw.padDir = pos(now)
	case found:
		lw.Open = true
		lw.PassAt = at(rootS)
		lw.padDir = pos(lw.PassAt)
	}

	if lw.Open {
		// Candidate bearings from the launch-azimuth identity; the
		// production plane normal picks the one whose plane IS the
		// target's, travelling the target's way.
		s := math.Abs(cosI) / math.Max(cosLat, 1e-12)
		if s > 1 {
			s = 1
		}
		a := math.Asin(s) * 180 / math.Pi
		best, bestDot := 0.0, -2.0
		rv := lw.padDir.Scale(radius)
		for _, b := range []float64{a, 180 - a, 360 - a, 180 + a} {
			h, ok := spacecraft.HeadingOrbitNormal(rv, spin, (b-90)*math.Pi/180)
			if !ok {
				continue
			}
			if d := h.Unit().Dot(nT); d > bestDot {
				best, bestDot = b, d
			}
		}
		lw.HeadingDeg = math.Mod(best+360, 360)
		return lw
	}

	// No pass: least plane angle for the target-direction heading.
	bearing := 90.0
	if cosI < 0 {
		bearing = 270
	}
	off := (bearing - 90) * math.Pi / 180
	g := func(s float64) float64 {
		r := pos(at(s)).Scale(radius)
		h, ok := spacecraft.HeadingOrbitNormal(r, spin, off)
		if !ok {
			return 90
		}
		return foldedPlaneAngleDeg(h, nT)
	}
	// The best moment must be one still ahead of us. The sample grid
	// starts at "now", so a minimum just BEHIND now used to win as sample 0
	// (it beat tomorrow's off-grid samples) and the row sat on T-0s,
	// re-solving every frame, until tomorrow's sample dipped lower (#548).
	// Take the smallest local minimum among samples 1..N (so the grid's
	// first sample only counts if the curve is still falling into it), plus
	// the stretch before sample 1 when g is falling at "now".
	gs := make([]float64, samples+2)
	for i := range gs {
		gs[i] = g(float64(i) * step)
	}
	refine := func(lo, hi float64) (float64, float64) {
		for k := 0; k < 60; k++ {
			m1, m2 := lo+(hi-lo)/3, hi-(hi-lo)/3
			if g(m1) < g(m2) {
				hi = m2
			} else {
				lo = m1
			}
		}
		m := 0.5 * (lo + hi)
		return m, g(m)
	}
	bestS, bestV := -1.0, math.Inf(1)
	if g(1) < gs[0] { // falling at now: the minimum is ahead, before or at sample 1
		bestS, bestV = refine(0, step)
	}
	pick := -1
	for i := 1; i <= samples; i++ {
		if gs[i] <= gs[i-1] && gs[i] <= gs[i+1] && (pick < 0 || gs[i] < gs[pick]) {
			pick = i
		}
	}
	if pick > 0 {
		if m, v := refine(float64(pick-1)*step, float64(pick+1)*step); v < bestV {
			bestS, bestV = m, v
		}
	}
	if bestS < 0 { // monotone over the whole period: the smallest ahead sample
		bestS, bestV = step, gs[1]
		for i := 2; i <= samples; i++ {
			if gs[i] < bestV {
				bestS, bestV = float64(i)*step, gs[i]
			}
		}
	}
	lw.BestDeg, lw.BestAt = bestV, at(bestS)
	lw.HeadingDeg = bearing
	return lw
}

// LaunchWindowLeadDeg is the lead the bound vessel/ghost target will
// have at the cached pass: its position one Kepler step on, measured
// from the pad's position at the pass about the target's own plane
// normal (positive = target ahead of the pad, TargetLeadAngleDeg's
// convention). ok is false for a body target, an Always window (no
// single pass instant), a no-pass window, or a hyperbolic target.
func (w *World) LaunchWindowLeadDeg(lw LaunchWindow) (float64, bool) {
	if !lw.Open || lw.Always {
		return 0, false
	}
	if w.Target.Kind != TargetCraft && w.Target.Kind != TargetGhost {
		return 0, false
	}
	c := w.ActiveCraft()
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if c == nil || !ok {
		return 0, false
	}
	dt := lw.PassAt.Sub(w.Clock.SimTime).Seconds()
	st, ok := physics.KeplerStep(physics.StateVector{R: rT, V: vT}, c.Primary.GravitationalParameter(), dt)
	if !ok {
		return 0, false
	}
	a := lw.padDir.Scale(c.Primary.RadiusMeters())
	h := lw.nT
	// Orient the plane normal along the target's own travel so "ahead"
	// is ahead along its orbit.
	if h.Dot(rT.Cross(vT)) < 0 {
		h = h.Scale(-1)
	}
	theta := math.Atan2(h.Dot(a.Cross(st.R)), a.Dot(st.R))
	return theta * 180 / math.Pi, true
}
