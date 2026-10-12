package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/render"
)

// Navball frame follows the flight (#601, ADR 0051 decision 13d note).
//
// KSP's way: the navball speed readout follows the frame (Surface / Orbit /
// Target), `;` cycles it by hand, and the frame also moves itself on the way
// up (Surface to Orbit) and near a rendezvous target (Orbit to Target).
//
// The rule that keeps a manual choice safe: an automatic switch fires only
// on CROSSING a threshold (an edge, never a level), and only out of the one
// frame it is meant to leave (SURF on the way up, ORBIT on the way down,
// ORBIT on the way into rendezvous range, TGT on the way out). A player who
// presses `;` keeps that frame until the next crossing.
//
// Thresholds:
//
//   - SURF to ORBIT on ascent: 6 atmosphere scale heights (density about
//     0.25% of the surface value, where drag stops being the dominant force
//     and orbital speed becomes the number insertion uses), clamped to the
//     body's atmosphere cutoff. 6.4 scale heights is KSP's own Kerbin
//     figure (36 km of 5.6 km). Earth 51 km, Mars about 67 km. Airless
//     bodies: 10 km, but never more than a tenth of the body's radius
//     (Phobos 1.1 km).
//   - ORBIT to SURF on descent: 80% of the ascent altitude. The gap is the
//     hysteresis band, so a vessel hovering or skimming at the line does
//     not flicker.
//   - ORBIT to TGT: range to a vessel Target at or inside the co-warp couple
//     range (35 km), the same moment the Proximity View hint appears and
//     "you are now flying relative to them" becomes true. TGT back to ORBIT
//     at the co-warp decouple range (42 km), the same hysteresis pair.

const (
	navAutoScaleHeights   = 6.0
	navAutoAirlessMaxM    = 10_000.0
	navAutoAirlessRadFrac = 0.1
	navAutoDescentFrac    = 0.8
)

// navAutoState is the edge memory: which side of each line the active vessel
// was on last Tick. Transient (not saved): after a load or a vessel/primary/
// target change it re-seeds silently from the current state, so a switch is
// never replayed and never fired by merely being somewhere.
type navAutoState struct {
	craftID uint64
	primary string
	seeded  bool
	above   bool // above the ascent line (with hysteresis)

	target    Target
	tgtSeeded bool
	tgtInside bool
}

// NavAutoAscentAltitude is the SURF to ORBIT altitude for a body, from its
// radius and atmosphere (scale height and cutoff in metres; hasAtmo false
// for airless bodies).
func NavAutoAscentAltitude(radiusM, scaleHeightM, cutoffM float64, hasAtmo bool) float64 {
	if hasAtmo && scaleHeightM > 0 {
		h := navAutoScaleHeights * scaleHeightM
		if cutoffM > 0 && h > cutoffM {
			h = cutoffM
		}
		return h
	}
	return math.Min(navAutoAirlessMaxM, navAutoAirlessRadFrac*radiusM)
}

// NavAutoAscentAltitudeFor is NavAutoAscentAltitude for a catalog body.
func (w *World) NavAutoAscentAltitudeFor(b bodies.CelestialBody) float64 {
	if a := b.Atmosphere; a != nil {
		return NavAutoAscentAltitude(b.RadiusMeters(), a.ScaleHeight, a.CutoffAltitude, true)
	}
	return NavAutoAscentAltitude(b.RadiusMeters(), 0, 0, false)
}

// NavSpeed is the NAVIGATION box's speed: the active vessel's speed in the
// frame the navball is showing, and that frame (the effective one: a TGT
// with no resolvable target reads as ORBIT, as ResolveAttitudeIntent does).
func (w *World) NavSpeed() (float64, NavMode) {
	c := w.ActiveCraft()
	if c == nil {
		return 0, w.NavMode
	}
	nav := w.NavMode
	if nav == NavTarget {
		if _, vT, ok := w.TargetStateRelativeToActivePrimary(); ok {
			return c.State.V.Sub(vT).Norm(), NavTarget
		}
		nav = NavOrbit
	}
	if nav == NavSurface {
		om := render.BodySpinOmegaWorld(c.Primary)
		omega := orbital.Vec3{X: om.X, Y: om.Y, Z: om.Z}
		return c.State.V.Sub(omega.Cross(c.State.R)).Norm(), NavSurface
	}
	return c.OrbitalSpeed(), NavOrbit
}

// updateAutoNavMode steps the edge detectors once per Tick, after
// integration, on final positions.
func (w *World) updateAutoNavMode() {
	c := w.ActiveCraft()
	if c == nil {
		w.navAuto = navAutoState{}
		return
	}
	st := &w.navAuto
	if !st.seeded || st.craftID != c.ID || st.primary != c.Primary.ID {
		*st = navAutoState{craftID: c.ID, primary: c.Primary.ID, seeded: true}
		st.above = c.Altitude() >= w.NavAutoAscentAltitudeFor(c.Primary)
	} else {
		up := w.NavAutoAscentAltitudeFor(c.Primary)
		alt := c.Altitude()
		switch {
		case !st.above && alt >= up:
			st.above = true
			if w.NavMode == NavSurface {
				w.NavMode = NavOrbit
			}
		case st.above && alt < up*navAutoDescentFrac:
			st.above = false
			if w.NavMode == NavOrbit {
				w.NavMode = NavSurface
			}
		}
	}

	// Rendezvous range.
	ps, ok := w.ProximityState()
	if !ok {
		st.tgtSeeded = false
		return
	}
	if !st.tgtSeeded || st.target != w.Target {
		st.target, st.tgtSeeded = w.Target, true
		st.tgtInside = ps.RangeM <= coWarpCoupleRangeM
		return
	}
	switch {
	case !st.tgtInside && ps.RangeM <= coWarpCoupleRangeM:
		st.tgtInside = true
		if w.NavMode == NavOrbit {
			w.NavMode = NavTarget
		}
	case st.tgtInside && ps.RangeM >= coWarpDecoupleRangeM:
		st.tgtInside = false
		if w.NavMode == NavTarget {
			w.NavMode = NavOrbit
		}
	}
}
