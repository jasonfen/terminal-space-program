package sim

import "testing"

// TestCycleViewModeWraps: starts at the zero-value (ViewEquatorial),
// cycles forward through the AllViewModes set and lands back at
// ViewEquatorial. v0.6.4+.
func TestCycleViewModeWraps(t *testing.T) {
	w := mustWorld(t)
	if w.ViewMode != ViewEquatorial {
		t.Fatalf("default ViewMode = %v, want ViewEquatorial", w.ViewMode)
	}
	w.CycleViewMode()
	if w.ViewMode != ViewOrbitPerpendicular {
		t.Errorf("after one cycle: ViewMode = %v, want ViewOrbitPerpendicular", w.ViewMode)
	}
	w.CycleViewMode()
	if w.ViewMode != ViewEquatorial {
		t.Errorf("after two cycles: ViewMode = %v, want ViewEquatorial (wrap)", w.ViewMode)
	}
}

// TestViewModeStringDistinct: each enumerated mode renders a distinct
// human label. Catches accidental case duplications when new modes
// land.
func TestViewModeStringDistinct(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range AllViewModes {
		s := m.String()
		if s == "" || s == "?" {
			t.Errorf("ViewMode(%d) returned empty / unknown string %q", m, s)
		}
		if seen[s] {
			t.Errorf("duplicate label %q", s)
		}
		seen[s] = true
	}
}
