package spacecraft

import "time"

// TriggerEvent selects how a node's TriggerTime is determined. v0.6.0+.
//
// Absolute (zero value) preserves the v0.1–v0.5 semantics: TriggerTime
// is set explicitly at plant time and never changes.
//
// The event-relative modes leave TriggerTime zero at plant time; the
// resolver in sim's executeDueNodes computes TriggerTime once at the
// first Tick where the live orbit yields a future crossing (lazy
// freeze). After resolution the node behaves like an Absolute node.
//
// v0.8.1+: lifted from internal/sim into internal/spacecraft so each
// Spacecraft can own its own []ManeuverNode without an import cycle.
type TriggerEvent int

const (
	TriggerAbsolute TriggerEvent = iota
	TriggerNextPeri
	TriggerNextApo
	TriggerNextAN
	TriggerNextDN
)

// String returns a human-readable label for the trigger event.
func (e TriggerEvent) String() string {
	switch e {
	case TriggerAbsolute:
		return "T+"
	case TriggerNextPeri:
		return "next peri"
	case TriggerNextApo:
		return "next apo"
	case TriggerNextAN:
		return "next AN"
	case TriggerNextDN:
		return "next DN"
	}
	return "?"
}

// AllTriggerEvents lists the trigger modes in canonical UI cycle order.
var AllTriggerEvents = [...]TriggerEvent{
	TriggerAbsolute,
	TriggerNextPeri,
	TriggerNextApo,
	TriggerNextAN,
	TriggerNextDN,
}

// ManeuverNode represents a planned burn. v0.5.14+: TriggerTime is the
// burn-CENTER moment (the planner's intended firing point), not the
// burn start. For impulsive burns (Duration=0) center == start ==
// TriggerTime. For finite burns the integrator actually starts the
// burn at TriggerTime - Duration/2 so the burn is centered on
// TriggerTime.
//
// Duration controls finite vs impulsive: zero = instant Δv (legacy
// v0.1 path); non-zero = sustained engine burn lasting up to Duration
// or until DV is delivered, whichever first. Finite-burn execution
// is driven by Spacecraft.ActiveBurn during subsequent ticks.
//
// PrimaryID is the body whose frame the burn was planned in (empty =
// the craft's home primary at plant time). Auto-plant transfers
// (v0.3.1) plant a geocentric departure plus a heliocentric / arrival-
// frame node; PrimaryID lets the planner UI render a frame-distinct
// glyph and lets the burn-execution layer warn if a node fires in an
// unexpected frame.
//
// Event (v0.6.0+) selects whether TriggerTime is absolute or resolved
// from a live-orbit event. Zero value = TriggerAbsolute.
//
// v0.8.1+: ManeuverNode lives on Spacecraft.Nodes (was World.Nodes).
type ManeuverNode struct {
	TriggerTime time.Time
	Mode        BurnMode
	DV          float64
	Duration    time.Duration
	PrimaryID   string
	Event       TriggerEvent
	// Throttle (v0.7.6+) is the engine throttle setting [0, 1] used
	// for this node's burn. Zero (the JSON omitempty default) is
	// remapped to 1.0 — full open — by EffectiveThrottle so v1–v3
	// saves and pre-v0.7.6 plant paths keep their prior behaviour
	// without explicit migrations. Per-node throttle decouples
	// planted burns from live `Craft.Throttle` so adjusting throttle
	// mid-coast doesn't slow an in-flight planted burn.
	Throttle float64
}

// EffectiveThrottle returns the throttle to use when firing this
// node's burn, mapping the JSON omitempty zero-default to 1.0 (full
// open). v0.7.6+.
func (n ManeuverNode) EffectiveThrottle() float64 {
	if n.Throttle <= 0 {
		return 1.0
	}
	if n.Throttle > 1 {
		return 1.0
	}
	return n.Throttle
}

// IsResolved reports whether the node's TriggerTime has been set —
// either because the node was planted with TriggerAbsolute or because
// the lazy-freeze resolver has fired for an event-relative node.
func (n ManeuverNode) IsResolved() bool {
	return n.Event == TriggerAbsolute || !n.TriggerTime.IsZero()
}

// BurnStart returns the sim-time at which the integrator should fire
// this node's burn. For impulsive nodes (Duration=0) BurnStart equals
// TriggerTime. For finite nodes BurnStart is `TriggerTime - Duration/2`
// so the burn is centered on TriggerTime. v0.5.14+.
func (n ManeuverNode) BurnStart() time.Time {
	if n.Duration <= 0 {
		return n.TriggerTime
	}
	return n.TriggerTime.Add(-n.Duration / 2)
}

// BurnEnd returns the sim-time at which the integrator should
// terminate this node's burn (regardless of Δv-remaining or fuel
// state). v0.5.14+.
func (n ManeuverNode) BurnEnd() time.Time {
	if n.Duration <= 0 {
		return n.TriggerTime
	}
	return n.TriggerTime.Add(n.Duration / 2)
}

// ActiveBurn is the runtime state of an in-progress finite burn. Set
// by the dispatcher when a node with Duration>0 fires; cleared when
// DVRemaining hits zero or SimTime passes EndTime. v0.8.1+: lives on
// Spacecraft.ActiveBurn so each craft can run its own burn
// concurrently.
type ActiveBurn struct {
	Mode        BurnMode
	DVRemaining float64
	EndTime     time.Time
	PrimaryID   string
	Throttle    float64
}

// ManualBurn is the runtime state of a v0.7.3+ player-held manual
// burn. Mirrors ActiveBurn's role in the integrator dispatch but
// carries no Δv budget, no end time, and no fixed mode — direction
// comes from Spacecraft.AttitudeMode (which the player can update on
// the fly via the attitude keys), and the burn ends when the player
// stops it or fuel runs out. StartTime is informational only.
//
// v0.8.1+: lives on Spacecraft.ManualBurn.
type ManualBurn struct {
	StartTime time.Time
}
