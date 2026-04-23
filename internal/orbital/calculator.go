// MIT License
//
// Copyright (c) 2025 Francis (furan917/go-solar-system)
// Copyright (c) 2026 jasonfen (terminal-space-program)
//
// Calculator interface and SolarSystem/Generic/Exact implementations
// lifted from furan917/go-solar-system@e632e6e/internal/orbital/calculator.go
// with these adaptations:
//   - models.CelestialBody → bodies.CelestialBody
//   - factory's body-detection removed (caller picks the calculator per-system)
//   - field renames: body.GetMassKg() → body.MassKg() (our helper name)

package orbital

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// SystemType identifies which Calculator implementation is appropriate.
type SystemType string

const (
	SystemTypeSolar   SystemType = "solar"
	SystemTypeGeneric SystemType = "generic"
	SystemTypeExact   SystemType = "exact"
)

// Calculator produces a mean anomaly (radians) for a body at a given sim-time.
// Callers turn that into eccentric/true anomaly via kepler.go + position via
// the Keplerian orbital elements stored on bodies.CelestialBody.
type Calculator interface {
	CalculateMeanAnomaly(body bodies.CelestialBody, t time.Time) float64
	GetSystemType() SystemType
}

// SolarSystemCalculator uses J2000.0 mean anomalies for Sol's 8+1 bodies.
type SolarSystemCalculator struct{ epochTime time.Time }

// GenericCalculator seeds a deterministic mean anomaly from the body's
// physical parameters — suitable for exoplanet systems without published
// J2000-equivalent ephemerides.
type GenericCalculator struct{ epochTime time.Time }

// ExactCalculator uses per-body OrbitalElements.Epoch/MeanAnomaly.
type ExactCalculator struct{}

func NewSolarSystemCalculator(epoch time.Time) *SolarSystemCalculator {
	return &SolarSystemCalculator{epochTime: epoch}
}
func NewGenericCalculator(epoch time.Time) *GenericCalculator {
	return &GenericCalculator{epochTime: epoch}
}
func NewExactCalculator() *ExactCalculator { return &ExactCalculator{} }

// j2000MeanAnomalies: mean anomaly M at J2000.0 epoch (radians) for each
// Solar System body by englishName. Values lifted from upstream.
var j2000MeanAnomalies = map[string]float64{
	"Mercury": 174.7948 * math.Pi / 180.0,
	"Venus":   50.4161 * math.Pi / 180.0,
	"Earth":   357.5291 * math.Pi / 180.0,
	"Mars":    19.3730 * math.Pi / 180.0,
	"Jupiter": 20.0202 * math.Pi / 180.0,
	"Saturn":  317.0207 * math.Pi / 180.0,
	"Uranus":  141.0498 * math.Pi / 180.0,
	"Neptune": 256.2250 * math.Pi / 180.0,
	"Pluto":   14.8820 * math.Pi / 180.0,
}

func (sc *SolarSystemCalculator) CalculateMeanAnomaly(body bodies.CelestialBody, t time.Time) float64 {
	m0, ok := j2000MeanAnomalies[body.EnglishName]
	if !ok {
		return NewGenericCalculator(sc.epochTime).CalculateMeanAnomaly(body, t)
	}
	days := t.Sub(bodies.J2000).Hours() / 24.0
	if body.SideralOrbit <= 0 {
		return m0
	}
	n := 2 * math.Pi / body.SideralOrbit
	return math.Mod(m0+n*days, 2*math.Pi)
}

func (sc *SolarSystemCalculator) GetSystemType() SystemType { return SystemTypeSolar }

func (gc *GenericCalculator) CalculateMeanAnomaly(body bodies.CelestialBody, t time.Time) float64 {
	seed := body.SemimajorAxis + body.SideralOrbit + body.MeanRadius
	m0 := math.Mod(seed*(math.Pi/180.0), 2*math.Pi)
	days := t.Sub(gc.epochTime).Hours() / 24.0
	if body.SideralOrbit <= 0 {
		return m0
	}
	n := 2 * math.Pi / body.SideralOrbit
	return math.Mod(m0+n*days, 2*math.Pi)
}

func (gc *GenericCalculator) GetSystemType() SystemType { return SystemTypeGeneric }

func (ec *ExactCalculator) CalculateMeanAnomaly(body bodies.CelestialBody, t time.Time) float64 {
	if body.OrbitalElements == nil {
		return 0
	}
	days := t.Sub(body.OrbitalElements.Epoch).Hours() / 24.0
	m0 := body.OrbitalElements.MeanAnomaly * math.Pi / 180.0
	if body.SideralOrbit <= 0 {
		return math.Mod(m0, 2*math.Pi)
	}
	n := 2 * math.Pi / body.SideralOrbit
	return math.Mod(m0+n*days, 2*math.Pi)
}

func (ec *ExactCalculator) GetSystemType() SystemType { return SystemTypeExact }

// ForSystem picks the right Calculator for a System name. Sol uses the
// J2000-keyed table; everything else falls back to Generic unless any body
// has explicit OrbitalElements, in which case Exact is used.
func ForSystem(system bodies.System, epoch time.Time) Calculator {
	if system.Name == "Sol" {
		return NewSolarSystemCalculator(epoch)
	}
	for _, b := range system.Bodies {
		if b.OrbitalElements != nil {
			return NewExactCalculator()
		}
	}
	return NewGenericCalculator(epoch)
}
