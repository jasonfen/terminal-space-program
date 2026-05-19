package sim

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// Hohmann coplanar/circular guard (v0.10.1+).
//
// The Hohmann auto-plant (`H` → PlanTransfer → PlanIntraPrimaryHohmann)
// is a textbook *circular-to-circular coplanar* two-impulse solver: it
// feeds the craft's instantaneous |R| in as a circular parking radius
// (sim/maneuver.go) and has no plane-change term anywhere. When the
// departure orbit is eccentric the assumed circular speed √(µ/r) is
// wrong; when it is inclined relative to the target's orbital plane a
// prograde burn never reaches the target. Either way the planted
// transfer is silently off.
//
// This guard does NOT change the planner — the real fix (eccentric-
// aware departure at periapsis + a plane change folded into the burns,
// a constrained-Lambert variant) is the deferred L-tier "Combined
// plane-shift + Hohmann" backlog item. It only surfaces a clear,
// non-blocking warning so the player understands why the result looks
// off and what to do instead (match the plane with `I`, circularize,
// then `H`). The transfer is still planted — this is advisory, not a
// refusal.

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
	nCraft := rC.Cross(vC)
	if nCraft.Norm() == 0 || nTarget.Norm() == 0 || mu <= 0 {
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
	return "Hohmann assumes a ~circular, coplanar orbit (" +
		strings.Join(problems, ", ") +
		") — match plane [I] + circularize, then [H]"
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
