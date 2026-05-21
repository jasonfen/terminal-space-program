package sim

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// CycleEngineMode flips between EngineMain and EngineRCS, the v0.8.0
// `r` keystroke target. Stops any in-flight manual main burn before
// switching to RCS so the engine doesn't keep firing through the mode
// change — the player would expect `r` to mean "switch tools," not
// "keep burning while I switch tools."
func (w *World) CycleEngineMode() {
	if w.ActiveCraft() == nil {
		return
	}
	if w.ActiveCraft().EngineMode == spacecraft.EngineMain {
		w.StopManualBurn()
		w.ActiveCraft().EngineMode = spacecraft.EngineRCS
		return
	}
	w.ActiveCraft().EngineMode = spacecraft.EngineMain
}

// RCSActive reports whether the active craft is currently in RCS
// engine mode. Nil-safe (no craft → false), mirroring the
// CycleEngineMode guard. Used by the navball panel to colour the
// RCS toggle and by its click dispatch to toast the new state.
// v0.9.6-polish.
func (w *World) RCSActive() bool {
	c := w.ActiveCraft()
	return c != nil && c.EngineMode == spacecraft.EngineRCS
}

// FireRCSPulse delivers one RCSDvQuantum pulse in the given mode (the
// six attitude directions). No-op if the engine isn't in RCS mode,
// the craft is missing, monoprop is empty, or a planted finite burn
// is in flight (the planted burn owns the engine — RCS pulses while
// a main burn fires would muddy the integrator's force model).
//
// Does NOT touch AttitudeMode: RCS is a 6-axis translation tool, so
// pulses apply Δv along the requested orbital-frame direction without
// re-pointing the nose. SAS hold is controlled separately via the
// main-engine attitude keys / SetAttitudeMode.
//
// v0.8.0+.
func (w *World) FireRCSPulse(mode spacecraft.BurnMode) bool {
	if w.ActiveCraft() == nil || w.ActiveCraft().EngineMode != spacecraft.EngineRCS {
		return false
	}
	if w.ActiveCraft().ActiveBurn != nil {
		return false
	}
	// v0.9.2+: BurnDirection handles surface modes + pitch trim.
	// v0.9.3+: resolve target snapshot for the four target-relative
	// modes; non-target modes ignore the snapshot.
	rT, vT, _ := w.TargetStateRelativeToActivePrimary()
	dir := w.ActiveCraft().BurnDirectionWithTarget(mode, rT, vT)
	if dir.Norm() == 0 {
		return false
	}
	if !w.ActiveCraft().ApplyRCSPulseWithTarget(mode, rT, vT) {
		return false
	}
	w.recordRCSPuff(dir)
	return true
}

// recordRCSPuff appends a puff entry to the ring buffer. dir is
// the unit Δv direction; the renderer flips it to draw exhaust on
// the opposite side of the craft glyph. The puff stores the craft
// pointer so the visual stays attached to the craft as it moves
// (v0.8.3+).
func (w *World) recordRCSPuff(dir orbital.Vec3) {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	w.rcsPuffs[w.rcsPuffIdx] = rcsPuff{
		craft: c,
		dir:   dir,
		at:    w.Clock.SimTime,
	}
	w.rcsPuffIdx = (w.rcsPuffIdx + 1) % rcsPuffCap
	if w.rcsPuffLen < rcsPuffCap {
		w.rcsPuffLen++
	}
}

// RCSPuffSample is a single recent pulse surfaced to the canvas
// renderer — inertial position (translated through the recorded
// primary's current sim-time position) plus the direction the exhaust
// points (anti-Δv, i.e. -dir) plus an age-fraction in [0, 1] where 0 =
// just fired, 1 = about to expire. Caller draws ageFrac into a fade.
// v0.8.0+.
type RCSPuffSample struct {
	Inertial orbital.Vec3
	Exhaust  orbital.Vec3 // unit vector — points away from craft along thrust-anti
	AgeFrac  float64
}

// RCSPuffs returns the recent pulses still within rcsPuffTTL of
// SimTime, in oldest-to-newest order. Each sample's Inertial
// position tracks the firing craft's CURRENT inertial position
// (v0.8.3+) rather than the position at fire time, so the visual
// stays anchored to the craft as it moves rather than drifting
// behind. Puffs whose firing craft has been removed from the
// slate (e.g. via Undock or DockCrafts) are dropped.
func (w *World) RCSPuffs() []RCSPuffSample {
	if w.rcsPuffLen == 0 {
		return nil
	}
	now := w.Clock.SimTime
	out := make([]RCSPuffSample, 0, w.rcsPuffLen)
	start := w.rcsPuffIdx - w.rcsPuffLen
	if start < 0 {
		start += rcsPuffCap
	}
	for i := 0; i < w.rcsPuffLen; i++ {
		p := w.rcsPuffs[(start+i)%rcsPuffCap]
		age := now.Sub(p.at).Seconds()
		if age < 0 || age > rcsPuffTTL {
			continue
		}
		if p.craft == nil || !w.craftStillLive(p.craft) {
			continue
		}
		inertial := w.BodyPosition(p.craft.Primary).Add(p.craft.State.R)
		out = append(out, RCSPuffSample{
			Inertial: inertial,
			Exhaust:  p.dir.Scale(-1),
			AgeFrac:  age / rcsPuffTTL,
		})
	}
	return out
}

// craftStillLive reports whether the given craft pointer is still
// in the slate. Used by the puff renderer to drop stale references
// after a dock / undock / future delete operation.
func (w *World) craftStillLive(target *spacecraft.Spacecraft) bool {
	for _, c := range w.Crafts {
		if c == target {
			return true
		}
	}
	return false
}

// pruneRCSPuffs is invoked from Tick — drops puffs whose sim-time has
// fallen out of the TTL window. Keeps the visible-buffer accessor
// cheap by trimming aggressively rather than scanning every render.
// v0.8.0+.
func (w *World) pruneRCSPuffs() {
	if w.rcsPuffLen == 0 {
		return
	}
	cutoff := w.Clock.SimTime.Add(-time.Duration(rcsPuffTTL * float64(time.Second)))
	for w.rcsPuffLen > 0 {
		start := w.rcsPuffIdx - w.rcsPuffLen
		if start < 0 {
			start += rcsPuffCap
		}
		oldest := w.rcsPuffs[start]
		if oldest.at.After(cutoff) {
			return
		}
		w.rcsPuffLen--
	}
}
