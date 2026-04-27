package spacecraft

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestNewInLEO(t *testing.T) {
	systems, _ := bodies.LoadAll()
	earth := systems[0].FindBody("Earth")
	sc := NewInLEO(*earth)

	// v0.5.13+ default: S-IVB-like 11 000 kg dry + 40 000 kg fuel.
	if sc.TotalMass() != 51000 {
		t.Errorf("total mass = %v, want 51000", sc.TotalMass())
	}
	// Altitude should be ~500 km (v0.6.1+ default — was 200 km).
	alt := sc.Altitude()
	if math.Abs(alt-500e3) > 1 {
		t.Errorf("altitude = %.1f m, want ~500000", alt)
	}
	// Circular orbital speed at 500 km altitude: √(μ/(R_earth + 500 km)) ≈ 7613 m/s.
	v := sc.OrbitalSpeed()
	if math.Abs(v-7613) > 50 {
		t.Errorf("orbital speed = %.1f m/s, want ~7613 m/s", v)
	}
}
