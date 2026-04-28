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

// TestHohmannPreviewForLunaIsIntraPrimary: from a LEO craft, a
// preview targeting Luna must use the intra-primary frame
// (Earth GM, parent-relative radii) — not the system primary's
// heliocentric frame. Pre-fix, the preview computed a Hohmann
// from the craft's solar distance (~150M km) to Luna's
// Earth-relative SMA (~384k km), which produced Δv values of
// ~28 km/s and ~242 km/s.
func TestHohmannPreviewForLunaIsIntraPrimary(t *testing.T) {
	w := mustWorld(t)
	sys := w.System()
	lunaIdx := -1
	for i, b := range sys.Bodies {
		if b.EnglishName == "Moon" || b.ID == "luna" {
			lunaIdx = i
			break
		}
	}
	if lunaIdx < 0 {
		t.Skip("Luna not found in Sol system")
	}

	p := w.HohmannPreviewFor(lunaIdx)
	if !p.Valid {
		t.Fatalf("preview invalid: %s", p.Note)
	}

	// Earth → Luna Hohmann from a 500-km LEO (the v0.6.1+ default
	// spawn altitude). Standard numbers are Δv1 ≈ 3.1 km/s
	// (TLI from circular LEO) and Δv2 ≈ 0.7 km/s (Luna-orbit
	// insertion at lunar SMA). Allow 30% tolerance — the preview
	// uses the craft's live |R| as r1 and Luna's bare SMA as r2,
	// which approximates a circular-to-circular insertion at
	// Luna's distance.
	if p.DV1 < 1500 || p.DV1 > 5000 {
		t.Errorf("Δv1: got %.0f m/s, want ~3100 m/s (TLI)", p.DV1)
	}
	if p.DV2 < 200 || p.DV2 > 1500 {
		t.Errorf("Δv2: got %.0f m/s, want ~700 m/s (Luna-orbit insertion)", p.DV2)
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
