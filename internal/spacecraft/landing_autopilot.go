package spacecraft

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// LandingPhase is where the Site+ landing autopilot is in a landing (ADR 0067
// decision 5). The zero value is Coast: engaged and waiting for the brake cue.
type LandingPhase int

const (
	PhaseCoast    LandingPhase = iota // engaged, brake not yet lit
	PhaseBrake                        // retro brake at the reserve throttle, stop point steered onto the aim
	PhaseApproach                     // the path is steeper than 45 degrees: the nose pitches up as the stop nears
	PhaseFinal                        // null the drift, fly the descent-rate schedule
)

// Word is the phase as GUIDANCE's hold: row spells it ("brake", "approach",
// "final"); Coast reads as "brake" armed, so it is spelled "coast".
func (p LandingPhase) Word() string {
	switch p {
	case PhaseBrake:
		return "brake"
	case PhaseApproach:
		return "approach"
	case PhaseFinal:
		return "final"
	}
	return "coast"
}

// LandingAutopilot is the per-vessel Site+ state (ADR 0067 decisions 1, 13): it
// sits beside Nodes and is ticked for every vessel, active or not. Saved with
// the vessel (schema 22) so a booster mid-brake survives a reload.
type LandingAutopilot struct {
	Engaged bool
	Phase   LandingPhase

	// ROD (decision 5, P66): the throttle holds RODRateMps (signed vertical
	// rate, negative = descending) while the nose stays the autopilot's.
	ROD        bool
	RODRateMps float64
	// ThrottleHeld: z/x gave the raw throttle back; steering is kept.
	ThrottleHeld bool

	// AimAlongM / AimAcrossM: the aim point's offset from the computer's site
	// (decision 6). Nothing writes it in S2; S4's arrows do.
	AimAlongM, AimAcrossM float64

	// NavAlongM / NavAcrossM / NavSeeded: the computer's-site error of decision
	// 3. Reserved as zero in S2; S4 fills them (and bumps no schema: they are
	// already carried).
	NavAlongM, NavAcrossM float64
	NavSeeded             bool

	// Transient, never saved: the brake's pitch-up knob (radians the thrust
	// leans from retrograde toward up) and this tick's flags for the readouts.
	PitchUpRad float64
	// StopSpanM is the stopping distance at the brake's light: the length the
	// steering authority (OutOfReachAuthorityFrac) is a fraction of.
	StopSpanM float64
	// TrackN is the unit normal of the ground-relative orbit plane at the
	// light: the along/across axes of the aim stay fixed to it, so a slow
	// vessel's wandering velocity cannot spin them.
	TrackN orbital.Vec3
	// ForecastAlongM / ForecastCrossM: the stop the law is steering toward (brake,
	// approach) or the position error it is closing (final), as miss: and cross:
	// sign them (+ long, + right). Written each tick; ForecastOK false until then.
	ForecastAlongM, ForecastCrossM float64
	ForecastOK                     bool
	Reserve                        bool // the brake throttle is eating into the reserve
	OutOfReach                     bool // the law saturates with the stop still off the aim
}

// Owns reports whether the autopilot has the engine: engaged and not out in
// Coast with nothing lit. RCS pulses are blocked while it does.
func (a *LandingAutopilot) Owns() bool { return a != nil && a.Engaged }
