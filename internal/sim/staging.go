// Package sim — staging chain (v0.9.1+).
//
// World.StageActive jettisons the active craft's bottom stage,
// spawning it as a passive Spacecraft at the same inertial position
// + velocity. The active craft retains the upper-stage chain (its
// new bottom = the old Stages[1], now firing). Resolves open
// scoping question #3 (KSP-style player-managed sequential
// decouples) per docs/v0.9-plan.md §"Resolved scoping questions".

package sim

import (
	"errors"
	"fmt"

	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Errors returned by StageActive.
var (
	// ErrStageNoCraft — caller passed a craftIdx outside the slate.
	ErrStageNoCraft = errors.New("stage: no craft at idx")
	// ErrStageEmpty — craft has no Stages slice (legacy literal-
	// constructed test fixture). The staging path requires at least
	// one stage entry. Real spawn paths always populate Stages via
	// NewFromLoadout, so this is a defensive check.
	ErrStageEmpty = errors.New("stage: craft has no stages")
	// ErrStageOnlyOne — craft has only one stage left. Dropping
	// the only stage would leave the player with nothing to
	// control, which is the wrong default. Status-flash + no-op.
	ErrStageOnlyOne = errors.New("stage: cannot drop the only remaining stage")
)

// StageActive jettisons the bottom stage (Stages[0]) of the craft
// at craftIdx, spawning it as a passive Spacecraft at the active
// craft's exact inertial position + velocity. The jettisoned stage
// carries any residual fuel + monoprop. The active craft is
// rebuilt from Stages[1:] — the new bottom (Stages[0] post-shift)
// becomes the firing engine.
//
// Returns (newActiveIdx, jettisonedIdx, nil) on success. Both
// indices are slate-relative after the spawn; jettisonedIdx is the
// position of the dropped-stage craft. newActiveIdx is unchanged
// from the input craftIdx since the active craft stays in place
// (the jettisoned stage is appended to the slate at the end).
//
// No-op (returns ErrStageOnlyOne) when the craft has exactly one
// stage — preserves the player's "core" craft. This matches KSP's
// "you can't decouple your last command pod" intuition. The HUD
// surfaces this via a status flash on the `space` keypress.
//
// v0.9.1+.
func (w *World) StageActive(craftIdx int) (newActiveIdx, jettisonedIdx int, err error) {
	if craftIdx < 0 || craftIdx >= len(w.Crafts) {
		return 0, 0, fmt.Errorf("%w: %d (slate has %d)", ErrStageNoCraft, craftIdx, len(w.Crafts))
	}
	c := w.Crafts[craftIdx]
	if c == nil {
		return 0, 0, fmt.Errorf("%w: %d (nil)", ErrStageNoCraft, craftIdx)
	}
	if len(c.Stages) == 0 {
		return 0, 0, ErrStageEmpty
	}
	if len(c.Stages) == 1 {
		return 0, 0, ErrStageOnlyOne
	}

	// Pop bottom stage (Stages[0]) and build a passive Spacecraft
	// from it. The dropped stage carries its residual fuel +
	// monoprop, inherits the active craft's primary frame +
	// position/velocity, and gets a derived name via nextCraftName
	// against the stage's loadout-specific name.
	dropped := c.Stages[0]
	c.Stages = c.Stages[1:]
	c.SyncFields() // re-derives DryMass / Fuel / Thrust / Isp / etc.
	// Mass field on the integrator state needs the new total too.
	c.State.M = c.TotalMass()

	jettisoned := buildJettisonedCraft(dropped, c)
	jettisoned.Name = w.nextCraftName(jettisoned.Name)

	w.Crafts = append(w.Crafts, jettisoned)
	jettisonedIdx = len(w.Crafts) - 1

	// Active craft stays in place (the player keeps flying the
	// upper chain). Slate ordering: active idx is unchanged; the
	// jettisoned stage sits at the end.
	newActiveIdx = craftIdx
	return newActiveIdx, jettisonedIdx, nil
}

// buildJettisonedCraft synthesises a passive Spacecraft from a
// jettisoned Stage at the position/velocity/primary of the active
// craft it came from. The new craft is single-stage (the dropped
// stage becomes its only stage), Throttle=0 (passive — no live
// engine), Glyph + Color inherited from the stage spec (which is
// itself populated from the parent loadout's per-stage entry).
func buildJettisonedCraft(s spacecraft.Stage, parent *spacecraft.Spacecraft) *spacecraft.Spacecraft {
	name := s.Name
	if name == "" {
		name = s.LoadoutID
	}
	if name == "" {
		name = "stage"
	}
	glyph := s.Glyph
	if glyph == "" {
		// Fall back to the parent loadout's catalog glyph so the
		// dropped stage still renders something distinguishable.
		l := spacecraft.LookupLoadout(s.LoadoutID)
		glyph = l.Glyph
	}
	color := s.Color
	if color == "" {
		l := spacecraft.LookupLoadout(s.LoadoutID)
		color = l.Color
	}
	c := &spacecraft.Spacecraft{
		Name:                 name,
		LoadoutID:            s.LoadoutID,
		Role:                 "jettisoned-stage",
		Glyph:                glyph,
		Color:                color,
		Throttle:             0, // passive — player isn't flying it.
		BallisticCoefficient: spacecraft.DefaultBallisticCoefficient,
		Stages:               []spacecraft.Stage{s},
		Primary:              parent.Primary,
		State: physics.StateVector{
			R: parent.State.R,
			V: parent.State.V,
			// Mass set via SyncFields + TotalMass below.
		},
	}
	c.SyncFields()
	c.State.M = c.TotalMass()
	return c
}
