package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Spawn-form band sampling (#221 part 2, ADR 0027 v0.32 amendment §2).
// The degraded band between the home-telemetry blanket edge and reliable
// DSN line-of-sight is real (~40-minute blackouts) and DELIBERATE — it is
// the pressure that makes relay constellations worth building. The spawn
// form warns about it, and the warning must be computed from the live
// model per parent body: a label derived from Earth would be exactly
// inverted at Kern (its radius is ~10× smaller, so its blanket edge is
// 300 km, not 3,186 km — the amendment's measured table). So this
// samples the PRODUCTION connectivity solve — same commNode rules, same
// commLinked, same blanket gate — never a second analytic formula that
// would drift the moment stations, occlusion, or a user overlay changed.

const (
	// The two phase axes are sampled independently — time-marching couples
	// them and aliases at synchronous altitudes (a geostationary probe
	// never moves relative to the stations, so a time-march reads 0% or
	// 100% by longitude luck instead of the true equatorial answer).
	commBandRotationSamples = 16
	commBandOrbitSamples    = 24

	// CommBandDegradedThreshold: sampled coverage below this flags the
	// preset. Fractionally under 1.0 to absorb grid-edge noise, so the
	// blanket / geosync cases read clean.
	CommBandDegradedThreshold = 0.995
)

// CommBandCoverage samples connectivity for a hypothetical probe with
// the given antenna rated range (≤0 → the direct-basic backfill every
// non-debris vessel carries) in a circular EQUATORIAL orbit of bodyID
// at altM, over orbit phase × body rotation phase, and returns the
// connected fraction. The antenna matters (v0.32 review finding): a
// Relay-Tug at the Moon links to Earth's ring while a direct-basic
// probe there is genuinely out of reach — warning the relay spawn "out
// of network reach" would steer the player off the exact constellation
// the band exists to motivate. ok=false when the body is not in the
// viewed system. Equatorial in the body-equatorial frame (frame.go is
// the boundary) — the amendment's scratch harness sampled ecliptic-
// frame orbits and its inclination figures were confounded with axial
// tilt; the form spawns exactly one plane (posOrbit has no inclination
// field), and this samples that plane. Live craft are deliberately
// excluded: the label describes the preset band of the station model,
// deterministic per (body, altitude, antenna), not the player's current
// relay constellation.
func (w *World) CommBandCoverage(bodyID string, altM, antennaRangeM float64) (float64, bool) {
	sys := w.System()
	body := sys.FindBody(bodyID)
	if body == nil || altM <= 0 {
		return 0, false
	}
	if antennaRangeM <= 0 {
		antennaRangeM = spacecraft.DefaultProbeAntennaRangeM
	}

	// Occluders: every body in the system, frozen at SimTime — orbital
	// motion over one rotation period is noise at band scale; the parent
	// itself doesn't move relative to its own stations at all.
	occ := make([]physics.OccluderBody, 0, len(sys.Bodies))
	for i := range sys.Bodies {
		b := sys.Bodies[i]
		occ = append(occ, physics.OccluderBody{Center: w.BodyPosition(b), Radius: b.RadiusMeters()})
	}

	// Stations, scoped to this system exactly like RecomputeCommGraph.
	type stationSpec struct {
		st GroundStationPreset
		b  bodies.CelestialBody
	}
	stationBodies := map[string]bool{}
	var sts []stationSpec
	for _, st := range w.GroundStations {
		b := sys.FindBody(st.BodyID)
		if b == nil {
			continue
		}
		stationBodies[st.BodyID] = true
		sts = append(sts, stationSpec{st: st, b: *b})
	}

	r := body.RadiusMeters() + altM
	frame := orbital.ReferenceFrameForPrimary(*body)
	nearHome := stationBodies[bodyID] && r <= nearHomeRadiiFactor*body.RadiusMeters()
	bodyPos := w.BodyPosition(*body)

	rotSamples := commBandRotationSamples
	// Sweep by |period|: SideralRotation is signed (retrograde Venus /
	// Uranus are negative), and a retrograde host sweeps its stations
	// the same as a prograde one — collapsing to a single sample would
	// reintroduce the aliasing this grid exists to prevent (v0.32
	// review finding). Only a genuinely non-rotating body (period 0)
	// has one station geometry.
	rotPeriod := math.Abs(body.SideralRotationSeconds())
	if rotPeriod == 0 {
		rotSamples = 1
	}

	connected, total := 0, 0
	const probeID = uint64(1)
	for ri := 0; ri < rotSamples; ri++ {
		t := w.Clock.SimTime
		if rotSamples > 1 {
			t = t.Add(time.Duration(float64(ri) / float64(rotSamples) * rotPeriod * float64(time.Second)))
		}
		nodes := make([]commNode, 0, len(sts)+1)
		for _, ss := range sts {
			// Only the parent's own stations sweep rotation phase; a ring
			// on another body is range-dominated at that distance and its
			// phase is noise.
			ts := w.Clock.SimTime
			if ss.st.BodyID == bodyID {
				ts = t
			}
			nodes = append(nodes, commNode{
				pos:      w.groundStationWorldPosAt(ss.st, ss.b, ts),
				rangeM:   ss.st.AntennaRangeM,
				forwards: true,
				station:  true,
				bodyID:   ss.st.BodyID,
			})
		}
		for oi := 0; oi < commBandOrbitSamples; oi++ {
			phi := 2 * math.Pi * float64(oi) / float64(commBandOrbitSamples)
			rBody := orbital.Vec3{X: r * math.Cos(phi), Y: r * math.Sin(phi)}
			probe := commNode{
				pos:      bodyPos.Add(frame.ToWorld(rBody)),
				rangeM:   antennaRangeM,
				probe:    true,
				craftID:  probeID,
				bodyID:   bodyID,
				nearHome: nearHome,
			}
			if connectivity(append(nodes, probe), occ)[probeID] {
				connected++
			}
			total++
		}
	}
	return float64(connected) / float64(total), true
}
