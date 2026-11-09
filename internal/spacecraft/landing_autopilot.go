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
// "final"); Coast is the wait for the brake cue, so it is spelled "wait".
func (p LandingPhase) Word() string {
	switch p {
	case PhaseBrake:
		return "brake"
	case PhaseApproach:
		return "approach"
	case PhaseFinal:
		return "final"
	}
	return "wait"
}

// LandingAutopilot is the per-vessel Site+ state (ADR 0067 decisions 1, 13): it
// sits beside Nodes and is ticked for every vessel, active or not. Saved with
// the vessel (schema 22) so a booster mid-brake survives a reload.
type LandingAutopilot struct {
	Engaged bool
	// Nose (fix E, P7): q's guidance nose hold. The law points the nose for the
	// phase and nothing else: no light, no throttle, no warp cap, no ROD. False
	// is the full autopilot (N), which is also what an older save loads as.
	Nose  bool
	Phase LandingPhase

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
	// NavFixed: the error was drawn modern-grade (a modern vessel, or a link at
	// the draw) or the ground has updated it (P20), so no update will fire. The
	// additive nav_fixed key in schema 22 (omitempty: absent = false).
	NavFixed bool

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

// ClearAttempt forgets the computer's-site error and the aim offset at the
// end of a landing attempt, so the next attempt draws its own.
func (a *LandingAutopilot) ClearAttempt() {
	a.NavAlongM, a.NavAcrossM, a.NavSeeded, a.NavFixed = 0, 0, false, false
	a.AimAlongM, a.AimAcrossM = 0, 0
}

// Owns reports whether the autopilot has the engine: engaged and not out in
// Coast with nothing lit. RCS pulses are blocked while it does.
func (a *LandingAutopilot) Owns() bool { return a != nil && a.Engaged && !a.Nose }

// CatalogRun is the contiguous run of the vessel's catalog loadout stages its
// own stages match by name (the LM is stages 1-2 of Apollo-TLI), or nil for a
// custom or edited stack, debris or an unknown loadout. Catalog-derived stage
// attributes a save never stored (antenna, nav sigma) are read through it.
func CatalogRun(c *Spacecraft) []Stage {
	if c.Role == RoleJettisonedStage || c.LoadoutID == "" || len(c.Stages) == 0 {
		return nil
	}
	l, ok := Loadouts[c.LoadoutID] // not LookupLoadout: its fallback would match an unknown id against the default stack
	cat := l.Stages
	if !ok {
		return nil
	}
	for k := 0; k+len(c.Stages) <= len(cat); k++ {
		match := true
		for i := range c.Stages {
			match = match && cat[k+i].Name == c.Stages[i].Name
		}
		if match {
			return cat[k : k+len(c.Stages)]
		}
	}
	return nil
}

// NavSigma is the vessel's no-link landing-computer error (ADR 0067 decision
// 3): the 1-sigma pair of its command part, from its own stages (a freshly
// built stack) else from the catalog run (a loaded save). ok=false is modern.
func (c *Spacecraft) NavSigma() (along, across float64, ok bool) {
	pick := func(stages []Stage) (float64, float64, bool) {
		for _, st := range stages {
			if IsCommandSource(st.CommandSource) && st.NavSigmaAlongM > 0 && st.NavSigmaAcrossM > 0 {
				return st.NavSigmaAlongM, st.NavSigmaAcrossM, true
			}
		}
		return 0, 0, false
	}
	if a, b, ok := pick(c.Stages); ok {
		return a, b, true
	}
	return pick(CatalogRun(c))
}
