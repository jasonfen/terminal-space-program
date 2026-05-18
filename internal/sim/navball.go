package sim

import (
	"math"

	"github.com/charmbracelet/lipgloss"

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
// NavOrbit / NavTarget are velocity-framed (the sphere's pole is the
// orbital normal):
//
//   - EX (lat 0, lon 0): +v̂ (orbit) or +(v_target − v_active)̂ (target)
//   - EZ (lat +90):      orbital normal (r × v)̂, re-orthogonalised
//     against EX (target-prograde isn't generally ⟂ the orbit plane)
//   - EY = EZ × EX
//
// NavSurface is a **local-horizon** sphere (KSP surface navball): the
// pole is the local vertical so the sky/ground hemispheres read true.
// This is velocity-independent, so it is well-defined on the launchpad
// (a craft sitting on the pad pointing radial-out reads at the sky
// pole, not the horizon band):
//
//   - EZ (lat +90, sky pole): local up = r̂
//   - EX (lon 0):             local north
//   - EY = EZ × EX            (= −local east)
//
// The surface navball is a **zenith-centred, North-up compass rose**:
// NavballSubObserver pins the disc centre to the zenith (lat +90) with
// a fixed 180° roll, not the nose. Under that fixed view this basis
// projects North→screen-up, East→screen-right, South→down, West→left
// (see TestNavballSurfaceEastIsScreenRight), and the nose rides as a
// marker that slides toward East (right) when the player trims `>`.
// Velocity-framed orbit/target basis below is unaffected.
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

	nav := w.NavMode
	if nav == NavTarget && w.Target.Kind != TargetCraft {
		nav = NavOrbit
	}

	// NavSurface: local-horizon sphere — pole = local up (r̂),
	// lon-0 = local north. Velocity-independent so it is valid on
	// the launchpad: the craft pointing radial-out reads at the sky
	// pole (lat +90), not the horizon. North is undefined at a
	// geographic pole or on a non-rotating primary — fall back to
	// any horizontal axis (world +Z projected off up, else +X) so
	// the basis stays defined rather than blanking the navball.
	if nav == NavSurface {
		rN := r.Norm()
		up := r.Scale(1 / rN)
		spinR := render.BodyRotationAxisWorld(active.Primary)
		spinAxis := orbital.Vec3{X: spinR.X, Y: spinR.Y, Z: spinR.Z}
		var north orbital.Vec3
		east := spinAxis.Cross(up)
		if spinAxis.Norm() > 0 && east.Norm() > 1e-12 {
			east = east.Scale(1 / east.Norm())
			north = up.Cross(east)
		} else {
			ref := orbital.Vec3{Z: 1}
			horiz := ref.Sub(up.Scale(up.Dot(ref)))
			if horiz.Norm() < 1e-9 {
				ref = orbital.Vec3{X: 1}
				horiz = ref.Sub(up.Scale(up.Dot(ref)))
			}
			north = horiz.Scale(1 / horiz.Norm())
		}
		// EY = EZ × EX (= −east). Right-handed; the desired
		// East-is-screen-right comes from the fixed zenith-centred
		// 180°-roll view that NavballSubObserver feeds for surface
		// mode, not from the basis handedness (see the type doc).
		eX := north
		eZ := up
		eY := eZ.Cross(eX)
		return NavballBasis{EX: eX, EY: eY, EZ: eZ}, true
	}

	h := r.Cross(v)
	hN := h.Norm()
	if hN == 0 {
		return NavballBasis{}, false
	}
	eZ := h.Scale(1 / hN)

	var eX orbital.Vec3
	switch nav {
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
	// v0.10.0: NavSurface is a zenith-centred, North-up compass rose.
	// The disc centre is pinned to the local zenith (basis EZ = up →
	// lat +90), NOT the nose; the 180° roll (subLon = 180 at the pole,
	// where subLon *is* the screen roll) orients the fixed basis so
	// North→up, East→right, South→down, West→left. The nose is shown
	// as a marker (NavballMarkers) that slides toward East (right) as
	// the player trims `>`. This is stable on the launchpad (no
	// gimbal: the centre doesn't depend on where the nose points).
	if w.NavMode == NavSurface {
		return 90, 180, true
	}
	// v0.10.0: in slew mode the disk centre is the craft's PHYSICAL
	// nose (CurrentAttitudeDir) so it animates as the craft slews;
	// the cardinal/node markers (NavballMarkers) stay on the
	// commanded directions, so nose vs targets visibly diverge during
	// a slew. Pre-first-tick (CurrentAttitudeDir still zero) or under
	// InstantSAS, fall back to the commanded direction.
	var dir orbital.Vec3
	if !w.InstantSAS && active.CurrentAttitudeDir.Norm() != 0 {
		dir = active.CurrentAttitudeDir
	} else {
		rT, vT, _ := w.TargetStateRelativeToActivePrimary()
		dir = active.BurnDirectionWithTarget(active.AttitudeMode, rT, vT)
	}
	if dir.Norm() == 0 {
		return 0, 0, false
	}
	lat, lon := basis.SubObserver(dir)
	return lat, lon, true
}

// Navball glyphs. Mirroring KSP's symbol vocabulary so muscle memory
// transfers. Single-cell unicode chars from the Geometric Shapes
// block, picked to read distinctly at small disk sizes.
const (
	NavballGlyphPrograde    = '⊕'
	NavballGlyphRetrograde  = '⊖'
	NavballGlyphNormalPlus  = '△'
	NavballGlyphNormalMinus = '▽'
	NavballGlyphRadialOut   = '◇'
	NavballGlyphRadialIn    = '◆'
	NavballGlyphTarget      = '◉'
	NavballGlyphAntiTarget  = '◌'
	NavballGlyphNode        = '◎' // planted maneuver node burn direction
	NavballGlyphNose        = '⌖' // craft nose (surface compass-rose mode)
)

// NavballMarkers returns the marker set the painter should overlay
// for the active craft + current NavMode. Each marker is already
// projected to (lat, lon) in the active basis — the painter only
// needs to forward-project to (dx, dy) and skip back-hemisphere
// hits.
//
// Each marker direction is the unit vector that pressing that
// axis key would steer toward, resolved via ResolveAttitudeIntent +
// BurnDirectionWithTarget. So:
//
//   NavOrbit   — orbit-frame prograde / retrograde / normal± / radial±
//                (six cardinals using the radial-diamond glyphs ◇ ◆)
//   NavSurface — prograde / retrograde swap to surface-relative
//                velocity; normal± / radial± stay orbit-frame
//                (matches ResolveAttitudeIntent's NavSurface fallthrough)
//   NavTarget  — prograde / retrograde swap to target-relative velocity;
//                radial+ / radial- swap to BurnTarget / BurnAntiTarget
//                (toward / away from target) and use the target glyphs
//                ◉ ◌ in target color so the swap is visible at a glance
//
// This makes the marker set match the SAS hold semantics exactly:
// each glyph sits at the direction the corresponding axis key would
// aim. The disk center is always "where the craft is currently
// pointing" — when the player presses the prograde key and the SAS
// finishes settling, the prograde glyph and the disk center coincide.
//
// Returns nil when the basis is unavailable. Individual markers are
// dropped when their direction is degenerate (zero surface velocity
// in NavSurface, coincident target velocity in NavTarget, etc.).
//
// v0.9.5+.
func (w *World) NavballMarkers() []render.NavballMarker {
	active := w.ActiveCraft()
	if active == nil {
		return nil
	}
	basis, ok := w.NavballBasis()
	if !ok {
		return nil
	}
	rT, vT, _ := w.TargetStateRelativeToActivePrimary()

	// Per-mode glyph + color per intent. Only the radial pair
	// re-skins per mode — prograde / retrograde / normal± keep their
	// glyphs since the direction-vector resolution already handles the
	// frame swap.
	type entry struct {
		intent AttitudeIntent
		glyph  rune
		color  lipgloss.Color
	}
	radialOutGlyph := NavballGlyphRadialOut
	radialInGlyph := NavballGlyphRadialIn
	radialColor := render.ColorNavballMarkerRadial
	if w.NavMode == NavTarget && w.Target.Kind == TargetCraft {
		radialOutGlyph = NavballGlyphTarget
		radialInGlyph = NavballGlyphAntiTarget
		radialColor = render.ColorNavballMarkerTarget
	}
	entries := []entry{
		{IntentPrograde, NavballGlyphPrograde, render.ColorNavballMarkerPrograde},
		{IntentRetrograde, NavballGlyphRetrograde, render.ColorNavballMarkerPrograde},
		{IntentNormalPlus, NavballGlyphNormalPlus, render.ColorNavballMarkerNormal},
		{IntentNormalMinus, NavballGlyphNormalMinus, render.ColorNavballMarkerNormal},
		{IntentRadialOut, radialOutGlyph, radialColor},
		{IntentRadialIn, radialInGlyph, radialColor},
	}

	out := make([]render.NavballMarker, 0, len(entries)+len(active.Nodes))
	for _, e := range entries {
		mode := w.ResolveAttitudeIntent(e.intent)
		dir := active.BurnDirectionWithTarget(mode, rT, vT)
		if dir.Norm() == 0 {
			continue
		}
		lat, lon := basis.SubObserver(dir)
		out = append(out, render.NavballMarker{
			LatDeg: lat,
			LonDeg: lon,
			Glyph:  e.glyph,
			Color:  e.color,
		})
	}

	// Surface compass ticks (N / E / S / W) — only in NavSurface, where
	// the player is thinking in surface-frame heading terms during
	// ascent. The four ticks are fixed-direction unit vectors in the
	// craft's local-horizon frame:
	//
	//   up    = +r̂                      (local vertical)
	//   east  = unit(spinAxis × up)      (local east on the rotating body)
	//   north = up × east                (right-handed completion)
	//
	// Painted with the grid color so they read as structural reference
	// labels, distinct from the bright SAS-axis cardinals. Skipped at
	// the poles (east undefined) or on a non-rotating primary.
	if w.NavMode == NavSurface {
		spinR := render.BodyRotationAxisWorld(active.Primary)
		spinAxis := orbital.Vec3{X: spinR.X, Y: spinR.Y, Z: spinR.Z}
		rN := active.State.R.Norm()
		if spinAxis.Norm() > 0 && rN > 0 {
			up := active.State.R.Scale(1 / rN)
			east := spinAxis.Cross(up)
			if eN := east.Norm(); eN > 0 {
				east = east.Scale(1 / eN)
				north := up.Cross(east)
				pushCompass := func(dir orbital.Vec3, glyph rune) {
					lat, lon := basis.SubObserver(dir)
					out = append(out, render.NavballMarker{
						LatDeg: lat,
						LonDeg: lon,
						Glyph:  glyph,
						Color:  render.ColorNavballGrid,
					})
				}
				pushCompass(north, 'N')
				pushCompass(east, 'E')
				pushCompass(north.Scale(-1), 'S')
				pushCompass(east.Scale(-1), 'W')
			}
		}
		// Nose marker. The surface navball is zenith-centred (the
		// disc centre is local up, not the nose), so the craft's
		// actual pointing is shown as a glyph that sits at centre on
		// the pad and slides toward East (screen right) as the player
		// trims `>`. Physical nose in slew mode (tracks the slew +
		// pitch-trim); commanded otherwise / pre-first-tick.
		noseDir := active.CurrentAttitudeDir
		if w.InstantSAS || noseDir.Norm() == 0 {
			noseDir = w.commandedDirFor(active)
		}
		if noseDir.Norm() != 0 {
			lat, lon := basis.SubObserver(noseDir)
			out = append(out, render.NavballMarker{
				LatDeg: lat,
				LonDeg: lon,
				Glyph:  NavballGlyphNose,
				Color:  render.ColorNavballMarkerNoseFront,
			})
		}
	}

	// Maneuver-node markers — one per planted node, using the per-leg
	// trajectory palette (render.ManeuverSegmentColor) so the navball
	// glyph matches the predicted-orbit leg color the orbit screen
	// already paints. The burn direction is computed in the craft's
	// CURRENT state (BurnDirectionWithTarget) so the marker tracks
	// where the SAS hold would aim if the player switched to the
	// node's burn mode now. For non-impulsive / future nodes this
	// drifts as the orbit advances — same drift KSP shows.
	//
	// Target-relative nodes resolve their target via the captured
	// n.TargetCraftIdx (same path as executeDueNodesFor) — a target
	// switch between plant and fire doesn't retarget the marker.
	// Stale bindings (idx out of range, target on a different
	// primary) produce zero direction and silently skip.
	for i, n := range active.Nodes {
		nrT, nvT := rT, vT
		if n.IsTargetRelative() {
			nrT, nvT = orbital.Vec3{}, orbital.Vec3{}
			resolved := false
			if tIdx, ok := n.TargetCraftIdxValue(); ok && tIdx >= 0 && tIdx < len(w.Crafts) {
				if tc := w.Crafts[tIdx]; tc != nil && tc.Primary.ID == active.Primary.ID {
					nrT, nvT = tc.State.R, tc.State.V
					resolved = true
				}
			}
			// Stale / unbound target-relative node: skip the marker
			// rather than letting DirectionUnitTarget's degenerate
			// fall-through math (e.g. unit(rT - rA) with rT=0 →
			// inward-radial direction) paint a misleading glyph.
			if !resolved {
				continue
			}
		}
		dir := active.BurnDirectionWithTarget(n.Mode, nrT, nvT)
		if dir.Norm() == 0 {
			continue
		}
		lat, lon := basis.SubObserver(dir)
		out = append(out, render.NavballMarker{
			LatDeg: lat,
			LonDeg: lon,
			Glyph:  NavballGlyphNode,
			Color:  render.ManeuverSegmentColor(i),
		})
	}
	return out
}
