package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
)

// NavballBasis is the orthonormal world-frame basis the navball is
// painted in. The ball's "lat = 0, lon = 0" point lies along EX;
// "lat = +90" (north pole) along EZ; "lon = +90, lat = 0" along EY
// (right-handed completion).
//
// EX is always the active NavMode's prograde direction so that when
// the craft's nose points along prograde, the prograde marker sits
// at the disk centre — matching KSP's "ball rotates so prograde stays
// in front of you" behaviour.
//
// EZ is the orbital normal in all modes (KSP convention: even in
// surface mode the navball's "north" stays orbital, since the local
// "up" is already covered by the radial-out marker).
//
// v0.9.5+.
type NavballBasis struct {
	EX, EY, EZ orbital.Vec3
}

// NavballBasis returns the orthonormal basis for the active craft +
// current NavMode. ok=false when the basis is degenerate — zero
// velocity (NavOrbit), zero surface velocity (NavSurface, e.g.
// stationary on the launchpad before liftoff), missing or coincident
// target velocity (NavTarget), or a craft state with zero r / a
// rectilinear orbit (no defined orbital plane).
//
// EX (lat = 0, lon = 0):
//   - NavOrbit   — +v̂                          (orbit-frame velocity)
//   - NavSurface — +(v − ω×r)̂                  (surface-relative velocity)
//   - NavTarget  — +(v_target − v_active)̂      (relative velocity, target-prograde)
//
// EZ (lat = +90, north pole) — orbital normal (r × v)̂ in all modes;
// re-orthogonalised against EX to handle modes where prograde is
// not perpendicular to the orbit plane (NavSurface near the launchpad,
// NavTarget when relative motion has a radial component).
//
// EY = EZ × EX, completing the right-handed basis.
func (w *World) NavballBasis() (NavballBasis, bool) {
	active := w.ActiveCraft()
	if active == nil {
		return NavballBasis{}, false
	}
	r := active.State.R
	v := active.State.V
	if r.Norm() == 0 {
		return NavballBasis{}, false
	}
	h := r.Cross(v)
	hN := h.Norm()
	if hN == 0 {
		return NavballBasis{}, false
	}
	eZ := h.Scale(1 / hN)

	nav := w.NavMode
	if nav == NavTarget && w.Target.Kind != TargetCraft {
		nav = NavOrbit
	}

	var eX orbital.Vec3
	switch nav {
	case NavSurface:
		omegaR := render.BodySpinOmegaWorld(active.Primary)
		omega := orbital.Vec3{X: omegaR.X, Y: omegaR.Y, Z: omegaR.Z}
		vSurf := v.Sub(omega.Cross(r))
		n := vSurf.Norm()
		if n == 0 {
			return NavballBasis{}, false
		}
		eX = vSurf.Scale(1 / n)
	case NavTarget:
		_, vT, ok := w.TargetStateRelativeToActivePrimary()
		if !ok {
			return NavballBasis{}, false
		}
		dv := vT.Sub(v)
		n := dv.Norm()
		if n == 0 {
			return NavballBasis{}, false
		}
		eX = dv.Scale(1 / n)
	default:
		vN := v.Norm()
		if vN == 0 {
			return NavballBasis{}, false
		}
		eX = v.Scale(1 / vN)
	}

	// Orthogonalise eZ against eX. Required because surface-prograde
	// and target-prograde aren't generally perpendicular to the orbital
	// normal, so the raw (h, eX) pair isn't an orthogonal basis.
	d := eZ.Dot(eX)
	eZ = eZ.Sub(eX.Scale(d))
	zN := eZ.Norm()
	if zN < 1e-9 {
		// eX is parallel to orbital normal — physically odd (would
		// require the craft's surface or relative velocity to coincide
		// with the orbit normal). Fall back to undefined basis; caller
		// degrades gracefully.
		return NavballBasis{}, false
	}
	eZ = eZ.Scale(1 / zN)
	eY := eZ.Cross(eX)
	return NavballBasis{EX: eX, EY: eY, EZ: eZ}, true
}

// SubObserver projects a unit world-frame direction onto the basis
// and returns its (lat, lon) on the navball sphere in degrees. The
// painter's sub-observer point is the active craft's nose direction
// projected this way; markers compute their position via the same
// transform.
//
// The input is assumed unit-length; minor float drift is clamped.
func (b NavballBasis) SubObserver(dir orbital.Vec3) (latDeg, lonDeg float64) {
	x := dir.Dot(b.EX)
	y := dir.Dot(b.EY)
	z := dir.Dot(b.EZ)
	if z > 1 {
		z = 1
	} else if z < -1 {
		z = -1
	}
	latDeg = math.Asin(z) * 180.0 / math.Pi
	lonDeg = math.Atan2(y, x) * 180.0 / math.Pi
	return latDeg, lonDeg
}

// NavballSubObserver returns (lat, lon, ok) on the navball for the
// active craft's nose direction (s.AttitudeMode → world-frame unit
// vector via BurnDirectionWithTarget) projected into the active
// NavMode's basis.
//
// ok=false when the basis is degenerate or the craft has no defined
// nose direction (e.g. surface-prograde before liftoff). Caller
// degrades to a static / blank navball.
func (w *World) NavballSubObserver() (latDeg, lonDeg float64, ok bool) {
	active := w.ActiveCraft()
	if active == nil {
		return 0, 0, false
	}
	basis, basisOK := w.NavballBasis()
	if !basisOK {
		return 0, 0, false
	}
	rT, vT, _ := w.TargetStateRelativeToActivePrimary()
	dir := active.BurnDirectionWithTarget(active.AttitudeMode, rT, vT)
	if dir.Norm() == 0 {
		return 0, 0, false
	}
	lat, lon := basis.SubObserver(dir)
	return lat, lon, true
}
