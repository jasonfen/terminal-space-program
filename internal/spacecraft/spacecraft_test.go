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

	if sc.TotalMass() != 1000 {
		t.Errorf("total mass = %v, want 1000", sc.TotalMass())
	}
	// Altitude should be ~200 km.
	alt := sc.Altitude()
	if math.Abs(alt-200e3) > 1 {
		t.Errorf("altitude = %.1f m, want ~200000", alt)
	}
	// Orbital speed should be ~7.78 km/s at LEO.
	v := sc.OrbitalSpeed()
	if math.Abs(v-7784) > 50 {
		t.Errorf("orbital speed = %.1f m/s, want ~7784 m/s", v)
	}
}
