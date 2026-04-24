package sim

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

func TestHohmannPreviewForMarsApproximatesTextbookNumbers(t *testing.T) {
	w := mustWorld(t)
	// Find Mars.
	sys := w.System()
	marsIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Mars" {
			marsIdx = i
			break
		}
	}
	if marsIdx < 0 {
		t.Fatalf("Mars not found in Sol system")
	}

	p := w.HohmannPreviewFor(marsIdx)
	if !p.Valid {
		t.Fatalf("preview invalid: %s", p.Note)
	}

	// The textbook Earth→Mars Hohmann numbers (Curtis §6.2 Ex 6.1) are
	// Δv1 ≈ 2.94 km/s, Δv2 ≈ 2.65 km/s, t ≈ 258.8 d. Our preview uses
	// the craft's *inertial* distance from the Sun as r1, which is
	// approximately 1 AU (Earth orbital radius + LEO altitude ~ 1 AU
	// for the spawned LEO-1). Allow 10% tolerance to absorb LEO offset.
	expectedDV1 := 2943.0
	expectedDV2 := 2649.0
	expectedT := 258.8 * bodies.SecondsPerDay

	if !within(p.DV1, expectedDV1, 0.1) {
		t.Errorf("dv1: got %.1f m/s, want ~%.1f m/s (±10%%)", p.DV1, expectedDV1)
	}
	if !within(p.DV2, expectedDV2, 0.1) {
		t.Errorf("dv2: got %.1f m/s, want ~%.1f m/s (±10%%)", p.DV2, expectedDV2)
	}
	if !within(p.TTransfer, expectedT, 0.1) {
		t.Errorf("t: got %.1f d, want ~%.1f d (±10%%)",
			p.TTransfer/bodies.SecondsPerDay, expectedT/bodies.SecondsPerDay)
	}
}

func TestHohmannPreviewForSystemPrimaryIsInvalid(t *testing.T) {
	w := mustWorld(t)
	// Index 0 in every system is the primary.
	p := w.HohmannPreviewFor(0)
	if p.Valid {
		t.Errorf("system primary should produce Invalid preview")
	}
	if p.Note == "" {
		t.Errorf("invalid preview must carry a Note")
	}
}

func TestHohmannPreviewFormatInvalidReturnsNoteLine(t *testing.T) {
	p := HohmannPreview{TargetName: "X", Note: "oops"}
	lines := p.Format()
	if len(lines) != 1 {
		t.Fatalf("invalid preview Format: got %d lines, want 1", len(lines))
	}
}

func TestHohmannPreviewFormatValidReturnsThreeLines(t *testing.T) {
	p := HohmannPreview{Valid: true, DV1: 1000, DV2: 2000, TTransfer: 86400}
	lines := p.Format()
	if len(lines) != 3 {
		t.Fatalf("valid preview Format: got %d lines, want 3 (got %v)", len(lines), lines)
	}
}

func within(got, want, tol float64) bool {
	if want == 0 {
		return math.Abs(got) <= tol
	}
	return math.Abs(got-want)/math.Abs(want) <= tol
}
