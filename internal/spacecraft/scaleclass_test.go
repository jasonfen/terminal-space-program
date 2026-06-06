package spacecraft

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// The whole existing real fleet carries real, Earth-sized Δv budgets, so
// every such catalog loadout reports real — including those that never set
// the field (empty normalizes to real, ADR 0014). We do not rescale the
// real fleet. The lone exception is the scale-matched Kern Stack (Slice
// C/D), explicitly tagged stripped-back and asserted in TestKernStackShape.
func TestLoadoutScaleDefaultsToReal(t *testing.T) {
	for _, id := range LoadoutOrder {
		if id == LoadoutKernStackID {
			continue // the one stripped-back vehicle
		}
		l := Loadouts[id]
		if got := l.Scale(); got != bodies.ScaleReal {
			t.Errorf("loadout %q Scale() = %q, want %q", id, got, bodies.ScaleReal)
		}
	}
}

// An explicit stripped-back tag (how the Kern Stack, Slice D, declares
// itself scale-matched to Lumen) is reported verbatim.
func TestLoadoutScaleExplicit(t *testing.T) {
	l := Loadout{ID: "Kern-Stack", ScaleClass: bodies.ScaleStrippedBack}
	if got := l.Scale(); got != bodies.ScaleStrippedBack {
		t.Errorf("stripped-back loadout Scale() = %q, want %q", got, bodies.ScaleStrippedBack)
	}
}
