package sim

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/planner"
)

// HohmannPreview summarises a reference heliocentric (system-primary-
// centered) Hohmann transfer from the craft's current distance to a
// target body's orbital radius. The numbers assume both legs are
// circular and coplanar — textbook Hohmann. Phasing is ignored; this
// is a "what would it cost?" display, not a physically-accurate
// multi-impulse planner.
type HohmannPreview struct {
	TargetName string
	DV1, DV2   float64 // m/s
	TTransfer  float64 // seconds
	Valid      bool    // false when inputs are degenerate
	Note       string  // human-readable reason when !Valid
}

// HohmannPreviewFor computes a preview to the indicated body index in the
// current system. Uses the system primary's GM, the craft's current
// inertial distance as r1, and the target's semimajor axis as r2.
func (w *World) HohmannPreviewFor(bodyIdx int) HohmannPreview {
	sys := w.System()
	if bodyIdx < 0 || bodyIdx >= len(sys.Bodies) {
		return HohmannPreview{Note: "no target"}
	}
	target := sys.Bodies[bodyIdx]
	if target.SemimajorAxis == 0 {
		return HohmannPreview{TargetName: target.EnglishName, Note: "system primary — no orbital radius"}
	}
	if w.Craft == nil {
		return HohmannPreview{TargetName: target.EnglishName, Note: "no spacecraft"}
	}
	if !w.CraftVisibleHere() {
		return HohmannPreview{TargetName: target.EnglishName, Note: "spacecraft not in this system"}
	}

	primary := sys.Bodies[0]
	mu := primary.GravitationalParameter()
	r1 := w.CraftInertial().Norm()
	r2 := target.SemimajorAxisMeters()

	dv1, dv2, t, err := planner.HohmannTransfer(r1, r2, mu)
	if err != nil {
		return HohmannPreview{TargetName: target.EnglishName, Note: err.Error()}
	}
	if math.IsNaN(dv1) || math.IsNaN(dv2) || math.IsNaN(t) {
		return HohmannPreview{TargetName: target.EnglishName, Note: "degenerate solution"}
	}
	return HohmannPreview{
		TargetName: target.EnglishName,
		DV1:        dv1,
		DV2:        dv2,
		TTransfer:  t,
		Valid:      true,
	}
}

// Format renders the preview as 3 short lines for the HUD.
func (p HohmannPreview) Format() []string {
	if !p.Valid {
		if p.TargetName == "" {
			return nil
		}
		return []string{fmt.Sprintf("  Hohmann: %s", p.Note)}
	}
	return []string{
		fmt.Sprintf("  Δv1: %.2f km/s", p.DV1/1000),
		fmt.Sprintf("  Δv2: %.2f km/s", p.DV2/1000),
		fmt.Sprintf("  t:   %.1f d", p.TTransfer/bodies.SecondsPerDay),
	}
}
