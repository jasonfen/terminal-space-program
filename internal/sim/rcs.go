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
	if w.Craft == nil {
		return
	}
	if w.EngineMode == spacecraft.EngineMain {
		w.StopManualBurn()
		w.EngineMode = spacecraft.EngineRCS
		return
	}
	w.EngineMode = spacecraft.EngineMain
}

// FireRCSPulse delivers one RCSDvQuantum pulse in the given mode (the
// six attitude directions). No-op if the engine isn't in RCS mode,
// the craft is missing, monoprop is empty, or a planted finite burn
// is in flight (the planted burn owns the engine — RCS pulses while
// a main burn fires would muddy the integrator's force model).
//
// Updates AttitudeMode so the HUD reflects the last fired direction
// — the player's mental model is "this key just nudged me prograde,"
// so showing prograde as the held attitude is the least surprising.
//
// v0.8.0+.
func (w *World) FireRCSPulse(mode spacecraft.BurnMode) bool {
	if w.Craft == nil || w.EngineMode != spacecraft.EngineRCS {
		return false
	}
	if w.ActiveBurn != nil {
		return false
	}
	dir := spacecraft.DirectionUnit(mode, w.Craft.State.R, w.Craft.State.V)
	if dir.Norm() == 0 {
		return false
	}
	if !w.Craft.ApplyRCSPulse(mode) {
		return false
	}
	w.AttitudeMode = mode
	w.recordRCSPuff(dir)
	return true
}

// recordRCSPuff appends a puff entry to the ring buffer. dir is the
// unit Δv direction; the renderer flips it to draw exhaust on the
// opposite side of the craft chevron. v0.8.0+.
func (w *World) recordRCSPuff(dir orbital.Vec3) {
	if w.Craft == nil {
		return
	}
	w.rcsPuffs[w.rcsPuffIdx] = rcsPuff{
		primaryID: w.Craft.Primary.ID,
		relR:      w.Craft.State.R,
		dir:       dir,
		at:        w.Clock.SimTime,
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
// SimTime, in oldest-to-newest order. Empty when nothing has fired
// recently or the buffer hasn't filled yet. v0.8.0+.
func (w *World) RCSPuffs() []RCSPuffSample {
	if w.rcsPuffLen == 0 {
		return nil
	}
	sys := w.System()
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
		var primaryPos orbital.Vec3
		if b := sys.FindBody(p.primaryID); b != nil {
			primaryPos = w.BodyPosition(*b)
		}
		out = append(out, RCSPuffSample{
			Inertial: primaryPos.Add(p.relR),
			Exhaust:  p.dir.Scale(-1),
			AgeFrac:  age / rcsPuffTTL,
		})
	}
	return out
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
