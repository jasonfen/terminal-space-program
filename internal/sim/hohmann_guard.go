package sim

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// Hohmann coplanar/circular preview note (v0.10.1+; re-pointed v0.12.x).
//
// The bodyinfo Hohmann PREVIEW (HohmannPreviewFor) is a textbook
// circular-to-circular coplanar two-impulse estimate — it ignores
// departure eccentricity and the craft↔target plane tilt, so its Δv is
// optimistic when the parking orbit is eccentric or inclined.
//
// v0.12.x (ADR 0005): the `[H]` AUTO-PLANT itself is no longer coplanar.
// PlanTransfer's intra-primary path now plants a plane-aware dual-strategy
// transfer (combined fused-Lambert vs split raise + apoapsis plane change,
// cheaper wins) — so the old "match plane [I], circularize, then [H]"
// advice is retired. This note now only flags that the *preview* numbers
// are coplanar-approximate; the planted transfer handles the geometry.

const (
	// hohmannEccTol — departure eccentricity above which the circular
	// assumption introduces a meaningful Δv / timing error.
	hohmannEccTol = 0.02
	// hohmannInclTolDeg — relative-plane tilt (deg) above which the
	// coplanar assumption breaks the rendezvous geometry. ~1° at
	// Luna's distance is already thousands of km of miss.
	hohmannInclTolDeg = 1.0
)

// hohmannGuardDetail returns a human-readable warning when the
// departure orbit (rC, vC about a primary with GM mu) is too
// eccentric or too far out of the target orbit's plane (normal
// nTarget, in the same frame as rC/vC) for the coplanar circular
// Hohmann solver to be accurate. Returns "" when the orbit is within
// tolerance — or when the inputs are too degenerate to assess (no
// false positives).
//
// Pure (no World) so the geometry is unit-testable in isolation; the
// World wrapper below only supplies nTarget.
func hohmannGuardDetail(rC, vC orbital.Vec3, mu float64, nTarget orbital.Vec3, eTol, inclTolDeg float64) string {
	// minNormalThreshold — orbit-normal magnitudes below this are treated
	// as degenerate (unassessable). An exact `== 0` check is insufficient:
	// when nTarget is built from two position samples a fixed interval
	// apart, a custom short-period (<2h) moon advances ~180° between
	// samples and the cross-product collapses to ~1e-9, not exactly 0 —
	// which would otherwise divide the cosine below by ~1e-9 and emit a
	// spurious tilt warning. Scaled up from the orbital frame.go `< 1e-12`
	// pattern for orbit-normal cross magnitudes. (#90)
	const minNormalThreshold = 1e-8
	nCraft := rC.Cross(vC)
	if nCraft.Norm() < minNormalThreshold || nTarget.Norm() < minNormalThreshold || mu <= 0 {
		return "" // can't assess — stay silent rather than cry wolf
	}
	var problems []string

	if el := orbital.ElementsFromState(rC, vC, mu); el.E > eTol {
		problems = append(problems, fmt.Sprintf("e=%.2f", el.E))
	}

	cos := nCraft.Dot(nTarget) / (nCraft.Norm() * nTarget.Norm())
	if cos > 1 {
		cos = 1
	} else if cos < -1 {
		cos = -1
	}
	ang := math.Acos(cos) * 180 / math.Pi
	relIncl := math.Min(ang, 180-ang) // plane tilt, direction-agnostic
	if relIncl > inclTolDeg {
		problems = append(problems, fmt.Sprintf("rel.incl %.1f°", relIncl))
	}

	if len(problems) == 0 {
		return ""
	}
	return "Hohmann preview is coplanar/circular (" +
		strings.Join(problems, ", ") +
		") — [H] plants a plane-aware transfer"
}

// HohmannDepartureWarning returns a non-empty advisory when the
// active craft's departure orbit is poorly conditioned for the
// coplanar circular Hohmann auto-plant to the body at targetIdx.
// Scoped to the intra-primary case (craft + target share a primary,
// e.g. LEO → Luna) — the path where the |R|-as-circular-radius and
// zero-inclination assumptions bite hardest. Returns "" for the
// heliocentric / moon-escape paths (out of scope for this guard) and
// whenever the geometry is within tolerance. v0.10.1+.
func (w *World) HohmannDepartureWarning(targetIdx int) string {
	sys := w.System()
	if targetIdx <= 0 || targetIdx >= len(sys.Bodies) {
		return ""
	}
	c := w.ActiveCraft()
	if c == nil {
		return ""
	}
	target := sys.Bodies[targetIdx]
	if target.ParentID != c.Primary.ID {
		return "" // not the intra-primary path
	}
	mu := c.Primary.GravitationalParameter()
	// Target orbit-plane normal: two parent-relative samples a short
	// time apart, crossed. Frame-agnostic and needs no body-velocity
	// API — works for any moon/parent pair.
	now := w.Clock.SimTime
	const dt = time.Hour
	p0 := w.BodyPositionAt(target, now).Sub(w.BodyPositionAt(c.Primary, now))
	p1 := w.BodyPositionAt(target, now.Add(dt)).Sub(w.BodyPositionAt(c.Primary, now.Add(dt)))
	nTarget := p0.Cross(p1)
	return hohmannGuardDetail(c.State.R, c.State.V, mu, nTarget, hohmannEccTol, hohmannInclTolDeg)
}
