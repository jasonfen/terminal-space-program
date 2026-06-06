package bodies

import (
	"encoding/json"
	"strings"
	"testing"
)

// An unset scaleClass means real (Sol-scale) — the integrator derives all
// dynamics from a Body's mass and radius, so every existing system stays
// real without touching its JSON (ADR 0014).
func TestSystemScaleDefaultsToReal(t *testing.T) {
	var s System
	if got := s.Scale(); got != ScaleReal {
		t.Errorf("empty System.Scale() = %q, want %q", got, ScaleReal)
	}
}

// A System unmarshals the optional scaleClass field and Scale() reports it
// verbatim — this is how Lumen (Slice D) declares itself stripped-back.
func TestSystemScaleFromJSON(t *testing.T) {
	var s System
	if err := json.Unmarshal([]byte(`{"systemName":"Lumen","scaleClass":"stripped-back"}`), &s); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := s.Scale(); got != ScaleStrippedBack {
		t.Errorf("Lumen Scale() = %q, want %q", got, ScaleStrippedBack)
	}
}

// scaleClass is omitted when unset, so adding the field does not perturb
// CatalogHash for the existing real-scale systems (ADR 0014).
func TestSystemScaleOmittedWhenUnset(t *testing.T) {
	data, err := json.Marshal(System{Name: "Sol"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := string(data); strings.Contains(got, "scaleClass") {
		t.Errorf("marshaled real system carries scaleClass: %s", got)
	}
}
