package sim

import (
	"math"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// NavballBasis is the orthonormal world-frame basis the navball is
// painted in. The ball's "lat = 0, lon = 0" point lies along EX;
// "lat = +90" along EZ; "lon = +90, lat = 0" along EY.
//
// v0.10.0+: this is the active craft's **body frame** — EX = nose,
// EZ = body up (dorsal), EY = body right — so the navball is the
// world as seen from the craft. The nose is always the disc centre
// and a non-zero roll banks the whole ball. NavballBasis() builds it
// from the body frame; see that method's doc. (Pre-v0.10 this was a
// velocity frame with EX = prograde, EZ = orbital normal.)
type NavballBasis struct {
	EX, EY, EZ orbital.Vec3
}

// NavballBasis returns the active craft's **body frame** as the
// navball basis (v0.10.0+): the navball is the world as seen from the
// craft, so it is the craft's own orientation, not a velocity frame.
//
//   - EX (lat 0, lon 0): nose / thrust axis (CurrentAttitudeDir)
//   - EZ (lat +90):      body up (dorsal) — the heads-up reference
//     (local vertical projected ⟂ nose) rotated about the nose by the
//     roll, so banking rolls the whole ball
//   - EY:                body right (= nose × up)
//
// The disc centre is therefore always the nose (NavballSubObserver
// returns (0,0)); prograde/retrograde/normal/radial/target/node and
// the surface compass ticks are world directions projected into this
// frame, so they sit exactly where they are *relative to the craft* —
// and with roll 0 (heads-up) the local vertical is screen-up and East
// is screen-right, the compass sense the player expects, with no pole
// singularity (body up/right are explicit, always orthonormal).
//
// ok=false only when the nose is uninitialised (zero) and there is no
// commanded direction to fall back to — the caller degrades to a
// static navball. NavMode no longer changes the basis; it still
// selects which markers/labels are shown (see NavballMarkers).
func (w *World) NavballBasis() (NavballBasis, bool) {
	active := w.ActiveCraft()
	if active == nil {
		return NavballBasis{}, false
	}
	nose, roll := w.navballNoseRoll(active)
	bNose, bUp, bRight, ok := active.BodyFrameFor(nose, roll)
	if !ok {
		return NavballBasis{}, false
	}
	return NavballBasis{EX: bNose, EY: bRight, EZ: bUp}, true
}

// navballNoseRoll picks the nose direction and roll the navball body
// frame is built from: the physical CurrentAttitudeDir / CurrentRollDeg
// under slew, or the commanded direction / CommandedRollDeg under
// InstantSAS or before the first slew tick (CurrentAttitudeDir still
// zero). Mirrors the stepThrust / consumer gating. v0.10.0+.
func (w *World) navballNoseRoll(active *spacecraft.Spacecraft) (nose orbital.Vec3, rollDeg float64) {
	if !w.InstantSAS && active.CurrentAttitudeDir.Norm() != 0 {
		return active.CurrentAttitudeDir, active.CurrentRollDeg
	}
	return w.commandedDirFor(active), active.CommandedRollDeg
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

// NavballSubObserver returns the disc-centre (lat, lon) for the
// navball. The basis IS the craft body frame (EX = nose), so the nose
// is by construction at (0, 0) — the disc centre is always "where the
// craft points". ok=false only when the body frame is undefined (no
// nose and no commanded direction). v0.10.0+.
func (w *World) NavballSubObserver() (latDeg, lonDeg float64, ok bool) {
	if _, basisOK := w.NavballBasis(); !basisOK {
		return 0, 0, false
	}
	return 0, 0, true
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
//	NavOrbit   — orbit-frame prograde / retrograde / normal± / radial±
//	             (six cardinals using the radial-diamond glyphs ◇ ◆)
//	NavSurface — prograde / retrograde swap to surface-relative
//	             velocity; normal± / radial± stay orbit-frame
//	             (matches ResolveAttitudeIntent's NavSurface fallthrough)
//	NavTarget  — prograde / retrograde swap to target-relative velocity;
//	             radial+ / radial- swap to BurnTarget / BurnAntiTarget
//	             (toward / away from target) and use the target glyphs
//	             ◉ ◌ in target color so the swap is visible at a glance
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
