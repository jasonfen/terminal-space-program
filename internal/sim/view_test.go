package sim

import "testing"

// TestCycleViewModeWraps: starts at the zero-value (ViewTop),
// cycles forward through Top → Right → Bottom → Left → OrbitFlat →
// Top in canonical order. v0.6.4+.
func TestCycleViewModeWraps(t *testing.T) {
	w := mustWorld(t)
	if w.ViewMode != ViewTop {
		t.Fatalf("default ViewMode = %v, want ViewTop", w.ViewMode)
	}
	want := []ViewMode{ViewRight, ViewBottom, ViewLeft, ViewOrbitFlat, ViewTop}
	for i, expect := range want {
		w.CycleViewMode()
		if w.ViewMode != expect {
			t.Errorf("after %d cycles: ViewMode = %v, want %v", i+1, w.ViewMode, expect)
		}
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
