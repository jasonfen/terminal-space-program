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

// HohmannPreviewFor computes a preview to the indicated body index in
// the current system. Picks the (µ, r1, r2) frame based on whether the
// target shares the craft's primary (intra-primary, e.g. LEO craft +
// Luna both around Earth) or sits in the heliocentric frame (Mars from
// LEO). v0.7.4+: previously the helper always used the system primary
// (Sun) and the craft's heliocentric distance — for moon targets that
// computed a Hohmann from the craft's solar distance (~150M km) to the
// moon's parent-relative semimajor axis (~384k km for Luna), giving
// nonsense Δv. Same flavor of bug fixed for PlanTransfer in v0.5.7.
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

	var mu, r1, r2 float64
	switch {
	case target.ParentID == w.Craft.Primary.ID:
		// Intra-primary: craft and target both orbit the craft's
		// primary. Mirrors PlanIntraPrimaryHohmann's frame —
		// shared-primary GM, craft's parent-relative |R|, target's
		// parent-relative SMA. Phasing is ignored (display-only).
		mu = w.Craft.Primary.GravitationalParameter()
		r1 = w.Craft.State.R.Norm()
		r2 = target.SemimajorAxisMeters()
	case w.Craft.Primary.ParentID != "" && target.ID == w.Craft.Primary.ParentID:
		// Moon → parent (e.g. Luna craft + Earth target). The actual
		// transfer is a moon-escape ellipse, not a two-impulse
		// circular Hohmann; surfacing fake Δv would mislead. Direct
		// the player to the dedicated `[H]` auto-plant which uses
		// PlanMoonEscape.
		return HohmannPreview{
			TargetName: target.EnglishName,
			Note:       "moon → parent — use [H] for escape transfer",
		}
	default:
		// Heliocentric: craft and target both ultimately orbit the
		// system primary (LEO craft + Mars). Patched-conic textbook
		// Hohmann is a reasonable preview at this layer.
		primary := sys.Bodies[0]
		mu = primary.GravitationalParameter()
		r1 = w.CraftInertial().Norm()
		r2 = target.SemimajorAxisMeters()
	}

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
