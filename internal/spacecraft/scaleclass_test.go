package spacecraft

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// Scale Class is opt-in: a loadout that never sets the field normalizes to
// real (ADR 0014), and the stripped-back fleet (the Kern Stack + the ADR 0031
// Lumen counterparts) reads stripped-back ONLY because it sets the tag
// explicitly. This asserts both halves — untagged ⇒ real, and nothing reads
// stripped-back without the explicit tag — without hardcoding which loadouts
// are which (the fleet grows).
func TestLoadoutScaleDefaultsToReal(t *testing.T) {
	for _, id := range LoadoutOrder {
		l := Loadouts[id]
		if l.ScaleClass == "" && l.Scale() != bodies.ScaleReal {
			t.Errorf("untagged loadout %q Scale() = %q, want %q", id, l.Scale(), bodies.ScaleReal)
		}
		if l.Scale() == bodies.ScaleStrippedBack && l.ScaleClass != bodies.ScaleStrippedBack {
			t.Errorf("loadout %q reads stripped-back without an explicit scale_class tag", id)
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
