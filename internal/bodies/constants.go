package bodies

import "time"

// Physical constants (SI).
const (
	// G is the universal gravitational constant in m^3 kg^-1 s^-2.
	G = 6.67430e-11
	// AU is one astronomical unit in meters.
	AU = 1.495978707e11
	// SecondsPerDay is exactly 86400.
	SecondsPerDay = 86400.0
	// EarthMassKg is the Earth's mass in kilograms.
	EarthMassKg = 5.9722e24
	// SunMassKg is the Sun's mass in kilograms.
	SunMassKg = 1.98892e30
)

// J2000 is the J2000.0 epoch: January 1, 2000 12:00 TT (UTC-approximate).
var J2000 = time.Date(2000, 1, 1, 12, 0, 0, 0, time.UTC)
