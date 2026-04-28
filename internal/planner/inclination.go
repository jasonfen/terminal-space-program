package planner

import (
	"errors"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// InclinationPlan describes a single normal-burn that rotates the
// craft's orbital plane around the line of nodes. PlanInclinationChange
// returns one of these; the sim layer adapts it into a ManeuverNode by
// mapping NormalSign onto BurnNormalPlus / BurnNormalMinus. We don't
// reuse TransferPlan because that type encodes mode via the boolean
// IsRetrograde — a normal-direction burn doesn't fit cleanly through
// a prograde/retrograde flag.
//
// AtAN is set true when the planner picked the ascending node, false
// for descending. Diagnostic only — the integrator doesn't care.
type InclinationPlan struct {
	PrimaryID  string
	DV         float64       // m/s, magnitude
	OffsetTime time.Duration // wall delay from "now" until burn fires
	NormalSign int           // +1 → BurnNormalPlus, -1 → BurnNormalMinus
	AtAN       bool
}

var (
	ErrEquatorialOrbit  = errors.New("planinclination: source orbit is equatorial — no defined node line")
	ErrHyperbolicOrbit  = errors.New("planinclination: source orbit is hyperbolic / degenerate")
	ErrInclinationRange = errors.New("planinclination: target inclination must be in [0, π] radians")
	ErrInclinationNoOp  = errors.New("planinclination: source already at target inclination")
	ErrNoNodeReachable  = errors.New("planinclination: no future node crossing reachable from current state")
)

// PlanInclinationChange constructs a single-burn plane rotation that
// fires at the next ascending or descending node (whichever comes
// sooner) and rotates the orbit's inclination to targetIncl (radians,
// in [0, π]). The longitude of ascending node Ω is preserved — pure
// inclination change, no plane shift.
//
// Δv magnitude: 2 · v_horizontal · sin(|Δi|/2), where v_horizontal is
// the velocity component perpendicular to the position vector at the
// chosen node (= h/r). For circular orbits v_horizontal = v; for
// eccentric orbits at the node it's v · cos(γ) where γ is the
// flight-path angle. Using v_horizontal (rather than |v|) keeps the
// formula exact for eccentric orbits — only the in-plane perpendicular
// component contributes to plane rotation.
//
// Direction: the burn is along ±h (orbit normal). At the ascending
// node, +h increases inclination; at the descending node, +h
// decreases it (h_z gains/loses sign based on which side of the
// equator the velocity is currently pushing). NormalSign records
// the chosen side.
//
// Errors:
//   - ErrEquatorialOrbit when |i| or |π−i| < 1 mrad — line of nodes
//     undefined, no AN/DN to fire at.
//   - ErrHyperbolicOrbit when e ≥ 1 or a ≤ 0.
//   - ErrInclinationRange when targetIncl is outside [0, π].
//   - ErrInclinationNoOp when |Δi| < 1 µrad.
//   - ErrNoNodeReachable when both TimeToNodeCrossing calls return
//     -1 (defensive — should be unreachable when the elements check
//     passes).
func PlanInclinationChange(state orbital.Vec3State, mu, targetIncl float64, primaryID string) (InclinationPlan, error) {
	if mu <= 0 {
		return InclinationPlan{}, errors.New("planinclination: mu must be positive")
	}
	if targetIncl < 0 || targetIncl > math.Pi {
		return InclinationPlan{}, ErrInclinationRange
	}
	el := orbital.ElementsFromState(state.R, state.V, mu)
	if el.E >= 1 || el.A <= 0 {
		return InclinationPlan{}, ErrHyperbolicOrbit
	}
	const equatorialTol = 1e-3
	if el.I < equatorialTol || math.Abs(el.I-math.Pi) < equatorialTol {
		return InclinationPlan{}, ErrEquatorialOrbit
	}
	deltaI := targetIncl - el.I
	if math.Abs(deltaI) < 1e-6 {
		return InclinationPlan{}, ErrInclinationNoOp
	}

	tAN := orbital.TimeToNodeCrossing(state, mu, true)
	tDN := orbital.TimeToNodeCrossing(state, mu, false)
	var dt float64
	switch {
	case tAN < 0 && tDN < 0:
		return InclinationPlan{}, ErrNoNodeReachable
	case tAN < 0:
		dt = tDN
	case tDN < 0:
		dt = tAN
	default:
		dt = math.Min(tAN, tDN)
	}

	// Identify *physical* AN (rising through equator) vs DN
	// (descending) at the burn moment by the sign of the current
	// state's v.Z. The next equator crossing flips v.Z's sign — so
	// currently rising (v.Z > 0) means the next crossing is
	// descending = DN. This is robust to the ω-driven AN/DN labels
	// in events.go, which become noise-dependent for near-circular
	// orbits where ω is degenerate. v.Z = 0 (currently at apex of
	// the orbit's z-extent) falls back to z's sign: above the
	// equator → next crossing is descending.
	atAN := state.V.Z < 0
	if state.V.Z == 0 {
		atAN = state.R.Z < 0
	}

	// r and v_horizontal at the chosen node:
	//   p = a(1-e²)             — semi-latus rectum
	//   r(ν) = p / (1 + e·cos ν) — node radius
	//   ν_AN = -ω, ν_DN = π - ω — the geometry node-line crossings
	//   h = √(µ·p)               — specific angular momentum magnitude
	//   v_horizontal = h / r     — perpendicular component
	// For circular orbits (e ≈ 0) ω is degenerate, but rNode = a
	// regardless of ν so the cancellation is harmless.
	p := el.A * (1 - el.E*el.E)
	cosNu := math.Cos(el.Arg) // cos(-ω) = cos(ω)
	if !atAN {
		cosNu = -cosNu // cos(π - ω) = -cos(ω)
	}
	rNode := p / (1 + el.E*cosNu)
	h := math.Sqrt(mu * p)
	vHorizontal := h / rNode
	dv := 2 * vHorizontal * math.Sin(math.Abs(deltaI)/2)

	// Sign rule (verified for prograde orbits in [0, π/2)):
	//   AN, Δi > 0 → +Normal (h gets a kick in +z half-space → i↑)
	//   AN, Δi < 0 → -Normal
	//   DN, Δi > 0 → -Normal (h_z would shrink under +Normal at DN)
	//   DN, Δi < 0 → +Normal
	increase := deltaI > 0
	sign := -1
	if (atAN && increase) || (!atAN && !increase) {
		sign = +1
	}

	return InclinationPlan{
		PrimaryID:  primaryID,
		DV:         dv,
		OffsetTime: time.Duration(dt * float64(time.Second)),
		NormalSign: sign,
		AtAN:       atAN,
	}, nil
}
