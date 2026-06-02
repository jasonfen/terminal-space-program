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

	"github.com/jasonfen/terminal-space-program/internal/orbital"
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
	// ErrTransposeNotReady — Transpose called on a craft that is not in
	// the pre-transposition [Descent, Ascent, SM, CM] shape (ADR 0009).
	// The player must drop the three Saturn stages first.
	ErrTransposeNotReady = errors.New("transpose: stack not in [Descent, Ascent, SM, CM] shape — drop the launch vehicle first")
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

	// v0.12 Slice 2 / ADR 0007: a Decouple Plan lets a press release
	// more than one contiguous bottom stage as a single craft (the
	// Apollo LM's descent + ascent pair). Plan absent/empty ⇒ a group
	// of 1 (the historical single-pop). A group size must leave at
	// least one stage as the surviving core, so groupSize >=
	// len(Stages) refuses with ErrStageOnlyOne — which also covers the
	// single-stage "can't drop your last stage" case (groupSize 1,
	// len 1).
	groupSize := 1
	if len(c.DecouplePlan) > 0 {
		groupSize = c.DecouplePlan[0]
	}
	if groupSize < 1 {
		groupSize = 1
	}
	if groupSize >= len(c.Stages) {
		return 0, 0, ErrStageOnlyOne
	}

	// Pop the bottom `groupSize` stages (Stages[:groupSize]) and build
	// a passive Spacecraft from them. The dropped stages carry their
	// residual fuel + monoprop, inherit the active craft's primary
	// frame + position/velocity, and the new craft gets a derived name
	// via nextCraftName against the bottom stage's loadout-specific
	// name. Copy the dropped slice (own backing array) before
	// resharing c.Stages so the jettisoned craft and the active craft
	// don't alias the same underlying array.
	dropped := append([]spacecraft.Stage(nil), c.Stages[:groupSize]...)
	c.Stages = c.Stages[groupSize:]
	// Advance the plan positionally — this press consumed its head
	// entry. A released multi-stage craft inherits no plan (set in
	// buildJettisonedCraft), so its boundaries fall back to single-pop.
	if len(c.DecouplePlan) > 0 {
		c.DecouplePlan = c.DecouplePlan[1:]
	}
	c.SyncFields() // re-derives DryMass / Fuel / Thrust / Isp / etc.
	// Mass field on the integrator state needs the new total too.
	c.State.M = c.TotalMass()
	// Rename the active craft to reflect the new bottom stage. A
	// Saturn V whose S-IC has dropped is effectively an S-II/S-IVB
	// stack; after S-II drops it's just an S-IVB. The loadout-level
	// name ("Saturn V") was correct for the full chain but stops
	// matching reality once stages decouple. Skip when the new
	// bottom stage has no name set (defensive — real loadouts
	// always populate Stages[i].Name).
	if c.Stages[0].Name != "" {
		c.Name = c.Stages[0].Name
	}

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

// stagingSeparationM is how far behind the active craft the
// jettisoned stage spawns, in metres. Must exceed DockingDistM (50 m)
// so checkDocking's proximity gate doesn't immediately re-fuse the
// pair on the next tick. v0.9.1.1+ (bundled into v0.9.2 per the
// user's "fix in the .2 slice" framing — see PR description).
const stagingSeparationM = 60.0

// stagingPushVMS is the retrograde velocity nudge on the jettisoned
// stage, in m/s. Must exceed DockingVMS (0.1 m/s) so checkDocking's
// |v_rel| gate also rejects the pair. Models a KSP-style decoupler
// spring: the booster loses thrust + falls behind the still-firing
// upper stage. v0.9.1.1+.
const stagingPushVMS = 0.5

// buildJettisonedCraft synthesises a passive Spacecraft from one or
// more jettisoned Stages (bottom-first, the group popped by a single
// staging press). The new craft is Throttle=0 (passive — no live
// engine), Glyph + Color inherited from the bottom stage's spec
// (itself populated from the parent loadout's per-stage entry), and
// carries NO Decouple Plan — a released multi-stage craft's internal
// boundaries fall back to single-pop (ADR 0007), so the extracted
// 2-stage LM later surface-stages its descent stage alone with zero
// special-casing.
//
// Placement branches on whether the parent is Landed (ADR 0007):
//
//   - Orbital (parent.Landed == false): the jettisoned craft spawns
//     60 m behind the active craft (along -V) with a 0.5 m/s
//     retrograde push so it stays outside both docking gates
//     (DockingDistM=50 m / DockingVMS=0.1 m/s) — the v0.9.1.1 fix for
//     the immediate re-fuse. Mirrors Undock's "spring release".
//   - Surface (parent.Landed == true): the jettisoned craft is
//     pinned to the parent's landed lat/lon (Landed=true, coords
//     copied) — integrateLanded re-pins R/V from those coords every
//     tick, so the retrograde inertial offset wouldn't survive a tick
//     anyway. The co-located both-Landed pair is kept from re-fusing
//     by the both-Landed guard in checkDocking, not by separation.
func buildJettisonedCraft(stages []spacecraft.Stage, parent *spacecraft.Spacecraft) *spacecraft.Spacecraft {
	bottom := stages[0]
	name := bottom.Name
	if name == "" {
		name = bottom.LoadoutID
	}
	if name == "" {
		name = "stage"
	}
	glyph := bottom.Glyph
	if glyph == "" {
		// Fall back to the parent loadout's catalog glyph so the
		// dropped stage still renders something distinguishable.
		l := spacecraft.LookupLoadout(bottom.LoadoutID)
		glyph = l.Glyph
	}
	color := bottom.Color
	if color == "" {
		l := spacecraft.LookupLoadout(bottom.LoadoutID)
		color = l.Color
	}
	c := &spacecraft.Spacecraft{
		Name:                 name,
		LoadoutID:            bottom.LoadoutID,
		Role:                 "jettisoned-stage",
		Glyph:                glyph,
		Color:                color,
		Throttle:             0, // passive — player isn't flying it.
		BallisticCoefficient: spacecraft.DefaultBallisticCoefficient,
		Stages:               append([]spacecraft.Stage(nil), stages...),
		Primary:              parent.Primary,
		// Inherit parent's attitude at decouple so the dropped
		// stage's launch-view sprite renders at the angle it shed
		// at, not snapped-to-vertical or zeroed (v0.11.3 Slice 4).
		CurrentAttitudeDir: parent.CurrentAttitudeDir,
		// DecouplePlan intentionally nil — a released sub-craft
		// inherits no plan (single-pop boundaries thereafter).
	}
	if parent.Landed {
		// Surface staging: pin to the parent's landed surface point.
		// Copy all four coord fields so the jettisoned stage re-pins
		// to the exact same cell the parent was pinned to, whether the
		// parent soft-landed (LandedLat/Lon set) or sat on a launchpad
		// (LaunchLat/Lon). integrateLanded prefers LandedLat/Lon when
		// non-zero, falling back to LaunchLat/Lon — same precedence the
		// parent used. State is copied as a seed; the landed integrator
		// overwrites R/V from the coords on the next tick.
		c.Landed = true
		c.LandedLatDeg = parent.LandedLatDeg
		c.LandedLonDeg = parent.LandedLonDeg
		c.LaunchLatDeg = parent.LaunchLatDeg
		c.LaunchLonDeg = parent.LaunchLonDeg
		c.State = parent.State
	} else {
		// Retrograde unit vector — points opposite the active craft's
		// orbital velocity. Falls back to anti-radial when velocity is
		// degenerate (sub-orbital craft at apex or stationary), then to
		// -X if both R and V are zero (defensive; shouldn't happen for
		// a real spawn).
		retrograde := retrogradeUnit(parent.State.V, parent.State.R)
		c.State = physics.StateVector{
			R: parent.State.R.Add(retrograde.Scale(stagingSeparationM)),
			V: parent.State.V.Add(retrograde.Scale(stagingPushVMS)),
			// Mass set via SyncFields + TotalMass below.
		}
	}
	c.SyncFields()
	c.State.M = c.TotalMass()
	return c
}

// retrogradeUnit returns -v.Unit() when v is non-zero, falling back
// to -r.Unit() (anti-radial) when v is degenerate, then to -X. Used
// by the staging path to choose a separation direction that won't
// re-cross the active craft on the next tick. v0.9.1.1+.
func retrogradeUnit(v, r orbital.Vec3) orbital.Vec3 {
	if vMag := v.Norm(); vMag > 0 {
		return v.Scale(-1 / vMag)
	}
	if rMag := r.Norm(); rMag > 0 {
		return r.Scale(-1 / rMag)
	}
	return orbital.Vec3{X: -1}
}

// TransposeReady reports whether a craft is in the pre-transposition
// shape [Descent, Ascent, SM, CM] — the state left after the three
// Saturn stages decouple, where the transpose key (D) is actionable.
// The HUD uses it to surface the "TRANSPOSE READY — press D" hint, and
// Transpose uses it as its precondition.
func TransposeReady(c *spacecraft.Spacecraft) bool {
	return c != nil && len(c.Stages) == 4 &&
		c.Stages[0].Name == "Descent" && c.Stages[1].Name == "Ascent" &&
		c.Stages[2].Name == "SM" && c.Stages[3].Name == "CM"
}

// Transpose performs the Apollo transposition (ADR 0009) on the craft at
// craftIdx in one shot: it reproduces the end-state of the manual
// docking flip — the SM becomes the firing core (Stages[0]) with the LM
// as a releasable nose payload — without flying the rendezvous.
//
// Precondition: the craft is exactly [Descent, Ascent, SM, CM] — the
// state left after the three Saturn stages have decoupled (the first
// three entries of the loadout's [1,1,1,2] DecouplePlan; the trailing 2
// drops the LM for the manual flip instead). Otherwise returns
// ErrTransposeNotReady.
//
// It splits the stack into the LM (Descent + Ascent) and the CSM core
// (SM + CM), reorders the live craft to [SM, CM, Descent, Ascent] so the
// SM's SPS is the firing engine (SyncFields mirrors Stages[0]) for LOI
// and TEI, and registers BOTH halves as DockedComponents carrying their
// per-stage breakdown. The LM then peels off as a 2-stage craft via the
// existing Undock (docking.go) for the lunar descent; the SM/CM core
// survives. This is the docking machinery already in use, not new flight
// semantics — DockCrafts would build the identical composite from a
// hand-flown flip.
func (w *World) Transpose(craftIdx int) error {
	if craftIdx < 0 || craftIdx >= len(w.Crafts) {
		return fmt.Errorf("%w: %d (slate has %d)", ErrStageNoCraft, craftIdx, len(w.Crafts))
	}
	c := w.Crafts[craftIdx]
	if c == nil {
		return fmt.Errorf("%w: %d (nil)", ErrStageNoCraft, craftIdx)
	}
	if !TransposeReady(c) {
		return ErrTransposeNotReady
	}

	// Own backing arrays so the two halves don't alias the soon-rebuilt
	// c.Stages.
	lmStages := append([]spacecraft.Stage(nil), c.Stages[0:2]...)   // Descent, Ascent
	coreStages := append([]spacecraft.Stage(nil), c.Stages[2:4]...) // SM, CM

	// Register both halves as docked components (identity + per-stage
	// breakdown) so Undock can restore them. Order = the reordered
	// Stages concatenation: core first, LM appended on the nose.
	core := dockedComponentFromStages(coreStages, "CSM", c.Role)
	lm := dockedComponentFromStages(lmStages, "LM", "lander")

	// Reorder the live craft so the SM is the firing engine.
	c.Stages = append(append([]spacecraft.Stage(nil), coreStages...), lmStages...)
	c.DockedComponents = []spacecraft.DockedComponent{core, lm}
	c.Name = "CSM" // flying as the CSM core now
	// Clear any leftover DecouplePlan (the loadout's trailing LM-group
	// "2" if the player transposed instead of staging the LM off). Post-
	// transposition the LM leaves via Undock and the SM/CM core stages
	// one-at-a-time (SM jettison after TEI, then the CM re-enters); a
	// stale "2" here would pop the [SM, CM] core as a group on the next
	// space press.
	c.DecouplePlan = nil
	c.SyncFields()
	c.State.M = c.TotalMass()
	return nil
}

// dockedComponentFromStages builds a DockedComponent (identity + full
// per-stage breakdown) from a contiguous stage group, deriving the
// component's glyph/color/loadout from the group's bottom stage and its
// summed mass/engine mirrors via SyncFields. Used by Transpose to wrap
// the CSM core and the LM nose payload so the existing Undock restores
// each as a correct multi-stage craft. v0.12 / ADR 0009.
func dockedComponentFromStages(stages []spacecraft.Stage, name, role string) spacecraft.DockedComponent {
	bottom := stages[0]
	tmp := &spacecraft.Spacecraft{
		Name:      name,
		Role:      role,
		Glyph:     bottom.Glyph,
		Color:     bottom.Color,
		LoadoutID: bottom.LoadoutID,
		Stages:    append([]spacecraft.Stage(nil), stages...),
	}
	tmp.SyncFields()
	return tmp.AsDockedComponent()
}
