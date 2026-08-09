package sim

import (
	"math"
	"sort"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// OrbitFloorMarginM is the fixed safety margin (ADR 0044 §3) added
// above a body's atmospheric cutoff altitude to get its Orbit Floor.
// Airless bodies have a zero cutoff, so this constant alone is their
// floor. One flat rule, no small-body exception.
const OrbitFloorMarginM = 25_000.0 // 25 km

// orbitCeilingFraction is how far toward the SOI boundary the Orbit
// Ceiling reaches (ADR 0044 §3): far enough to be a real high orbit,
// close enough that it stays this body's orbit rather than a
// boundary-straddling wobble.
const orbitCeilingFraction = 2.0 / 3.0

// orbitDedupeToleranceM is the minimum separation (in metres) between
// two Orbit Stops before the later one is treated as a duplicate of
// the former and dropped. Guards against near-collisions after
// ladder snapping / clamping to the band edges.
const orbitDedupeToleranceM = 500.0 // 0.5 km

// OrbitBand is the range of spawn altitudes a body can hold (ADR
// 0044 §3).
type OrbitBand struct {
	FloorM     float64 // lowest legal altitude above mean radius
	CeilingM   float64 // highest legal altitude; meaningless unless HasCeiling
	HasCeiling bool    // false for a system primary (a star orbits nothing)
	Empty      bool    // floor > ceiling: no legal orbit altitude exists here
}

// OrbitBandFor derives the Orbit Band for body b in system sys.
//
// Floor: a star floors at its authored/derived stand-off
// (CelestialBody.OrbitStandOffM); every other body floors at
// Atmosphere.CutoffAltitude + OrbitFloorMarginM (zero cutoff for an
// airless body, so it floors at exactly the margin).
//
// Ceiling: two-thirds of the way from b's surface to its sphere of
// influence, computed against b's actual gravitational parent
// (sys.ParentOf(b), falling back to sys.Bodies[0] exactly as
// physics.FindPrimary does) so that planets — whose parentId is
// empty in the catalog — resolve against the system's star. A system
// primary has no orbit of its own, so physics.SOIRadius returns 0
// for it; that zero is read directly as "no ceiling", not an error.
func OrbitBandFor(sys bodies.System, b bodies.CelestialBody) OrbitBand {
	band := OrbitBand{FloorM: floorFor(b)}

	parent := sys.ParentOf(b)
	if parent == nil && len(sys.Bodies) > 0 {
		p := sys.Bodies[0]
		parent = &p
	}
	if parent == nil {
		return band
	}

	soi := physics.SOIRadius(b, *parent)
	if soi == 0 {
		// System primary (or otherwise orbit-less body): no ceiling.
		return band
	}

	band.CeilingM = orbitCeilingFraction * (soi - b.RadiusMeters())
	band.HasCeiling = true
	if band.CeilingM < band.FloorM {
		band.Empty = true
	}
	return band
}

// floorFor returns the Orbit Floor for b, ignoring any ceiling.
func floorFor(b bodies.CelestialBody) float64 {
	if b.BodyType == "Star" {
		return b.OrbitStandOffM()
	}
	cutoff := 0.0
	if b.Atmosphere != nil {
		cutoff = b.Atmosphere.CutoffAltitude
	}
	return cutoff + OrbitFloorMarginM
}

// synchronousPeriodSeconds returns the period (seconds) an orbit
// must match to sit synchronous above b, or 0 when unknown.
//
// A tidally locked body's rotation is slaved to its orbit
// (CelestialBody.TidallyLocked doc comment: SideralRotation is
// ignored for these bodies), so its synchronous period is its
// orbital period, not its (unpopulated/meaningless) rotation period.
// Every other body uses the magnitude of its own rotation period —
// sign encodes prograde/retrograde (Venus is negative) and doesn't
// belong in a period.
func synchronousPeriodSeconds(b bodies.CelestialBody) float64 {
	if b.TidallyLocked {
		return b.SideralOrbitSeconds()
	}
	return math.Abs(b.SideralRotationSeconds())
}

// synchronousAltitudeM returns the altitude (metres above mean
// radius) at which an orbit's period matches synchronousPeriodSeconds,
// via a = (μT²/4π²)^⅓. Returns 0 when the period or GM is unknown —
// callers must treat 0 as "no synchronous stop", not a real answer.
func synchronousAltitudeM(b bodies.CelestialBody) float64 {
	t := synchronousPeriodSeconds(b)
	if t <= 0 {
		return 0
	}
	mu := b.GravitationalParameter()
	if mu <= 0 {
		return 0
	}
	a := math.Cbrt(mu * t * t / (4 * math.Pi * math.Pi))
	return a - b.RadiusMeters()
}

// ladderRungMultipliers are the significant digits of the 1/2/5 ×
// 10ⁿ ladder that Orbit Stops snap to (in kilometres).
var ladderRungMultipliers = [3]float64{1, 2, 5}

// snapToLadderKm returns the nearest 1/2/5 × 10ⁿ rung (kilometres)
// to targetKm, "nearest" measured in log space so e.g. 141 snaps to
// 100 (ln-distance ~0.34) rather than 200 (ln-distance ~0.35) by the
// same kind of margin throughout the ladder's range. Exponents run
// wide enough (10⁻³ .. 10¹² km) to cover anything from a metre-scale
// nudge to a star's stand-off.
func snapToLadderKm(targetKm float64) float64 {
	if targetKm <= 0 {
		return 0
	}
	logTarget := math.Log(targetKm)
	best := 0.0
	bestDist := math.Inf(1)
	for n := -3; n <= 12; n++ {
		pow := math.Pow(10, float64(n))
		for _, m := range ladderRungMultipliers {
			rung := m * pow
			dist := math.Abs(math.Log(rung) - logTarget)
			if dist < bestDist {
				bestDist = dist
				best = rung
			}
		}
	}
	return best
}

// OrbitStops returns the handful of interesting altitudes (metres,
// ascending) within b's Orbit Band that ←/→ walks in the spawn form:
// floor, up to two round interior altitudes, synchronous altitude
// (when it lands inside the band), and the ceiling (when the band
// has one). A body may offer fewer than five when a candidate stop
// collides with another after ladder-snapping, or falls outside the
// band (most commonly: synchronous altitude sits above the ceiling).
// An Empty band returns nil — there is no legal altitude to stop at.
func OrbitStops(sys bodies.System, b bodies.CelestialBody) []float64 {
	band := OrbitBandFor(sys, b)
	if band.Empty {
		return nil
	}

	lo := band.FloorM
	var hi float64
	if band.HasCeiling {
		hi = band.CeilingM
	} else {
		sync := synchronousAltitudeM(b)
		hi = math.Max(sync, 100*lo)
	}

	stops := []float64{lo}

	if hi > lo {
		ratio := hi / lo
		for _, frac := range [2]float64{1.0 / 3.0, 2.0 / 3.0} {
			targetM := lo * math.Pow(ratio, frac)
			snappedM := snapToLadderKm(targetM/1000) * 1000
			if snappedM > lo && snappedM < hi {
				stops = append(stops, snappedM)
			}
		}
	}

	if sync := synchronousAltitudeM(b); sync > lo && sync < hi {
		stops = append(stops, sync)
	}

	if band.HasCeiling {
		stops = append(stops, band.CeilingM)
	}

	return dedupeSortedStops(stops)
}

// dedupeSortedStops sorts stops ascending and collapses any run of
// values within orbitDedupeToleranceM of the previous kept value.
func dedupeSortedStops(stops []float64) []float64 {
	sort.Float64s(stops)
	out := make([]float64, 0, len(stops))
	for _, s := range stops {
		if len(out) > 0 && s-out[len(out)-1] < orbitDedupeToleranceM {
			continue
		}
		out = append(out, s)
	}
	return out
}
