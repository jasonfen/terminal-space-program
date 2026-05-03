// MIT License
//
// Copyright (c) 2025 Francis (furan917/go-solar-system)
// Copyright (c) 2026 jasonfen (terminal-space-program)
//
// Struct shape lifted from furan917/go-solar-system@e632e6e
// internal/models/body.go. Display hooks removed; Keplerian fields
// retained; OrbitalElement added. See NOTICE.md for attribution.

package bodies

import (
	"math"
	"time"
)

// CelestialBody describes a star, planet, or moon by its physical
// and orbital properties. Units: km for lengths, days for periods,
// degrees for angles, kg (via Mass.Value * 10^Exponent) for mass.
type CelestialBody struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	EnglishName     string  `json:"englishName"`
	BodyType        string  `json:"bodyType"`
	IsPlanet        bool    `json:"isPlanet"`
	Moons           []Moon  `json:"moons,omitempty"`
	SemimajorAxis   float64 `json:"semimajorAxis"`
	Perihelion      float64 `json:"perihelion,omitempty"`
	Aphelion        float64 `json:"aphelion,omitempty"`
	Eccentricity    float64 `json:"eccentricity"`
	Inclination     float64 `json:"inclination"`
	Mass            Mass    `json:"mass"`
	Density         float64 `json:"density,omitempty"`
	Gravity         float64 `json:"gravity,omitempty"`
	Escape          float64 `json:"escape,omitempty"`
	MeanRadius      float64 `json:"meanRadius"`
	SideralOrbit    float64 `json:"sideralOrbit,omitempty"`
	SideralRotation float64 `json:"sideralRotation,omitempty"`
	AroundPlanet    *Planet `json:"aroundPlanet,omitempty"`
	DiscoveredBy    string  `json:"discoveredBy,omitempty"`
	DiscoveryDate   string  `json:"discoveryDate,omitempty"`

	// Stellar-only properties
	Temperature  float64 `json:"temperature,omitempty"`
	StellarClass string  `json:"stellarClass,omitempty"`
	Age          float64 `json:"age,omitempty"`

	// Optional precise Keplerian elements (overrides semimajor/eccentricity/etc.)
	OrbitalElements *OrbitalElement `json:"orbitalElements,omitempty"`

	// Longitude of ascending node (Ω) and argument of periapsis (ω),
	// in degrees. Zero when unknown — flat-plane approximation.
	LongitudeOfAscendingNode float64 `json:"longitudeOfAscendingNode,omitempty"`
	ArgumentOfPeriapsis      float64 `json:"argumentOfPeriapsis,omitempty"`

	// ParentID identifies this body's gravitational parent. Empty
	// means "system primary" (e.g. the Sun for Sol bodies). Set on
	// moons (e.g. Luna.ParentID = "earth"). Drives hierarchical
	// BodyPosition recursion and FindPrimary's nested-SOI walk.
	// v0.5.0+.
	ParentID string `json:"parentId,omitempty"`

	// Color is the rendered display color as a hex string (e.g.
	// "#5BB3FF"). When set, render.ColorFor prefers this over the
	// hardcoded bodyPalette table; when empty, the table fallback +
	// stellar-tint / bodyType-default chain still applies. v0.7.1+.
	Color string `json:"color,omitempty"`

	// Atmosphere, when non-nil, declares an exponential-density
	// atmosphere for this body — drives drag (v0.8.4) and haze
	// rendering. Bodies without atmospheres leave this nil.
	Atmosphere *Atmosphere `json:"atmosphere,omitempty"`

	// TidallyLocked, when true, ties this body's rotation to its
	// orbital period — the same face always points at the parent.
	// SideralRotation is ignored for these bodies; the renderer
	// derives sub-observer longitude from orbital phase. v0.8.5+.
	TidallyLocked bool `json:"tidallyLocked,omitempty"`

	// AxialTilt is the body's obliquity (rotation-axis angle from
	// the orbital-plane normal), in degrees. Drives view-aware
	// texture projection (v0.8.5.7+) — ViewTop on a tilted body
	// reveals polar regions; Uranus's 97° tilt makes it roll
	// pole-on along its orbit.
	AxialTilt float64 `json:"axialTilt,omitempty"`

	// AxialAzimuth is the body's spin-axis azimuth in the world
	// inertial frame, in degrees. The axis projects onto the world
	// X-Y plane at this angle measured counterclockwise from world
	// +X (so 0° tips toward +X, 90° toward +Y, 180° toward -X).
	// Combined with AxialTilt the unit spin axis is
	//
	//	n = (sin(tilt)·cos(azimuth), sin(tilt)·sin(azimuth), cos(tilt))
	//
	// Defaults to 0 — same as the v0.8.5.7 launch behaviour where
	// every body's axis lay in the X-Z plane. Real bodies have
	// varied pole directions; populating this field lets each one
	// tip the right way once we have data.
	AxialAzimuth float64 `json:"axialAzimuth,omitempty"`
}

// Atmosphere is an exponential-density atmospheric model:
// ρ(h) = SurfaceDensity · exp(-h/ScaleHeight) for altitudes below
// CutoffAltitude, zero above. Color is the haze tint used by the
// renderer; defaults to body Color when empty.
type Atmosphere struct {
	ScaleHeight    float64 `json:"scaleHeight"`    // m
	SurfaceDensity float64 `json:"surfaceDensity"` // kg/m³ at altitude 0
	CutoffAltitude float64 `json:"cutoffAltitude"` // m above surface — drag = 0 above
	Color          string  `json:"color,omitempty"`
}

type Planet struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	EnglishName string `json:"englishName"`
}

type Moon struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	EnglishName string `json:"englishName"`
}

type Mass struct {
	Value    float64 `json:"massValue"`
	Exponent int     `json:"massExponent"`
}

type OrbitalElement struct {
	SemimajorAxis            float64   `json:"semimajorAxis"`
	Eccentricity             float64   `json:"eccentricity"`
	Inclination              float64   `json:"inclination"`
	ArgumentOfPeriapsis      float64   `json:"argumentOfPeriapsis"`
	LongitudeOfAscendingNode float64   `json:"longitudeOfAscendingNode"`
	MeanAnomaly              float64   `json:"meanAnomaly"`
	Epoch                    time.Time `json:"epoch"`
}

// MassKg returns the body's mass in kilograms.
func (cb *CelestialBody) MassKg() float64 {
	if cb.Mass.Value == 0 {
		return 0
	}
	return cb.Mass.Value * math.Pow10(cb.Mass.Exponent)
}

// GravitationalParameter returns GM in m^3/s^2.
func (cb *CelestialBody) GravitationalParameter() float64 {
	return G * cb.MassKg()
}

// SemimajorAxisMeters converts the stored semimajor axis (km) to meters.
func (cb *CelestialBody) SemimajorAxisMeters() float64 {
	return cb.SemimajorAxis * 1000.0
}

// RadiusMeters converts the stored mean radius (km) to meters.
func (cb *CelestialBody) RadiusMeters() float64 {
	return cb.MeanRadius * 1000.0
}

// SideralRotationSeconds converts the stored sidereal rotation
// period (hours, signed for prograde / retrograde) to seconds.
// Returns 0 when no rotation period is known.
func (cb *CelestialBody) SideralRotationSeconds() float64 {
	return cb.SideralRotation * 3600.0
}

// SideralOrbitSeconds converts the stored sidereal orbital period
// (days) to seconds. Returns 0 when no orbital period is known.
func (cb *CelestialBody) SideralOrbitSeconds() float64 {
	return cb.SideralOrbit * 86400.0
}
