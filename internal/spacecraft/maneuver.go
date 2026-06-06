package spacecraft

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

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
	// TriggerNextClosestApproach (v0.9.3+) resolves to the next
	// time-to-encounter between the active craft and a target craft
	// captured at plant time via ManeuverNode.TargetCraftIdx. Lazy-
	// frozen on the first tick after plant the same way AN / DN are.
	// Only valid for the four target-relative burn modes; pickable
	// in the m-form only when World.Target.Kind == TargetCraft at
	// plant time.
	TriggerNextClosestApproach
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
	case TriggerNextClosestApproach:
		return "next closest approach"
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
	TriggerNextClosestApproach,
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
	// TargetCraftID (v0.14.x / ADR 0012; was the one-based slate index
	// TargetCraftIdx) is the stable Spacecraft.ID of the target craft
	// this node was planted against, captured at plant time. Populated
	// only for the four target-relative modes (BurnTargetPrograde /
	// Retrograde / BurnTarget / AntiTarget) and for the
	// TriggerNextClosestApproach event. Zero-value-omitempty: non-target
	// nodes save without the field, no schema bump for that.
	//
	// Bound by identity at plant time so neither a later target switch
	// nor a slate mutation (end-flight / dock / undock / stage) silently
	// retargets the planted burn — the node stays aimed at the exact
	// craft the player chose, or degrades to no-op if that craft is gone
	// (resolve via World.craftByID at fire time). Zero means "no target".
	TargetCraftID uint64 `json:",omitempty"`
	// PlaneChangeRad (v0.10.4+) is the signed orbital-plane rotation
	// angle (radians) for a BurnPlaneChange node — the angle the
	// horizontal velocity is rotated through about the radial axis.
	// Populated only for BurnPlaneChange (the `I` inclination auto-
	// plant); zero for every other mode. Zero-value-omitempty so
	// non-plane-change nodes save without the field — no schema bump,
	// same convention as TargetCraftIdx.
	PlaneChangeRad float64 `json:",omitempty"`
	// BurnDirUnit (v0.12.x+) is the fixed inertial (primary-relative)
	// unit thrust direction for a BurnVector node — the fused-Lambert
	// departure Δv direction, carrying eccentricity + raise + plane
	// change together. Populated only for BurnVector (the fused [H]
	// auto-plant); the zero vector for every other mode. Captured at
	// plant time and held for the burn (the craft slews to it). Save
	// round-trips it additively, following the CurrentAttitudeDir
	// schema-v6 precedent — no migration.
	BurnDirUnit orbital.Vec3 `json:",omitempty"`
}

// TargetCraftIDValue returns the bound target craft's stable ID and
// ok=false when no target was bound at plant time. omitempty drops
// "no target" nodes from the wire. Resolve the ID to a live craft via
// World.craftByID at use time. v0.14.x / ADR 0012 (was
// TargetCraftIdxValue, which returned a slate index).
func (n ManeuverNode) TargetCraftIDValue() (uint64, bool) {
	if n.TargetCraftID == 0 {
		return 0, false
	}
	return n.TargetCraftID, true
}

// SetTargetCraftID binds the node to a craft by its stable ID. v0.14.x.
func (n *ManeuverNode) SetTargetCraftID(id uint64) { n.TargetCraftID = id }

// ClearTargetCraftID unbinds the node's target. v0.14.x.
func (n *ManeuverNode) ClearTargetCraftID() { n.TargetCraftID = 0 }

// IsTargetRelative reports whether this node's burn mode requires a
// target craft state to resolve direction. v0.9.3+.
func (n ManeuverNode) IsTargetRelative() bool {
	return IsTargetRelativeMode(n.Mode)
}

// IsTargetRelativeMode reports whether the given burn mode requires
// a target craft state. Used by m-form cycle gating and resolver
// dispatch. v0.9.3+.
func IsTargetRelativeMode(m BurnMode) bool {
	switch m {
	case BurnTargetPrograde, BurnTargetRetrograde, BurnTarget, BurnAntiTarget:
		return true
	}
	return false
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
	// TargetCraftID (v0.14.x / ADR 0012; was TargetCraftIdx) is the
	// stable Spacecraft.ID of the burn's target — zero means no target
	// bound. Populated when a target-relative finite-burn node fires;
	// the world's stepThrust resolves the target snapshot from this each
	// tick (via craftByID) so the burn keeps tracking even if the player
	// swaps World.Target or the slate shifts mid-burn.
	TargetCraftID uint64 `json:",omitempty"`
	// PlaneChangeRad (v0.10.4+) carries the BurnPlaneChange rotation
	// angle from the firing node onto the running burn, so the
	// attitude/thrust path can resolve the tilted plane-change
	// direction each tick. Zero for non-plane-change burns.
	PlaneChangeRad float64 `json:",omitempty"`
	// BurnDirUnit (v0.12.x+) mirrors ManeuverNode.BurnDirUnit onto the
	// in-flight burn so the attitude/thrust path resolves the fixed
	// BurnVector direction each tick. Zero for non-BurnVector burns.
	BurnDirUnit orbital.Vec3 `json:",omitempty"`
}

// TargetCraftIDValue mirrors ManeuverNode.TargetCraftIDValue — returns
// the stable ID the burn is bound to, or ok=false when no target was
// captured at fire time. Resolve via World.craftByID. v0.14.x / ADR 0012.
func (b ActiveBurn) TargetCraftIDValue() (uint64, bool) {
	if b.TargetCraftID == 0 {
		return 0, false
	}
	return b.TargetCraftID, true
}

// BurnStalled reports whether a planted burn is paused waiting for the
// player to stage: Δv is still owed but the firing (bottom) stage has run
// dry, so no thrust is being produced. The burn stays alive and resumes
// automatically once a fuelled stage is decoupled into place (v0.12.x
// pause-and-resume across staging). Drives the HUD "stage to resume"
// prompt and the throttle-cut abort path.
func (s *Spacecraft) BurnStalled() bool {
	return s.ActiveBurn != nil && s.ActiveBurn.DVRemaining > 0 && s.ActiveStageFuel() <= 0
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
