package sim

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// localReArm is the same-World mirror of the cross-player re-arm-by-leaving
// latch (ADR 0038 §5, internal/relay/dock.go's DockCooldown) for a pair of
// craft that just came apart in THIS World's own slate. #343: the
// cross-player path already refuses a fresh Claim between the same pair
// until they clear ReArmDistM (or ReArmCeiling of sim-time passes with the
// range unreadable) — the local path had no such memory at all, so once the
// initial separation push closed again (secular drift, or two vessels that
// simply drift together for unrelated reasons) checkDocking re-fused them
// with nothing standing in the way.
//
// Held only in memory — never persisted. Undock's along-track push (#343)
// means the pair's positions diverge secularly rather than closing on a
// fixed schedule, so a latch that resets across a save/load costs nothing
// worth a save-schema bump for.
type localReArm struct {
	idA, idB   uint64
	releasedAt time.Time
}

// Undock splits the craft at idx back into its DockedComponents,
// removing the composite from the slate and inserting one craft
// per component. Each restored craft inherits a share of the
// composite's current Fuel + Monoprop pools, prorated by its
// pre-dock capacity. The lead stays exactly where it is; each component
// above it appears where its hull was drawn (nose to the seam, pointing
// back along -a_hat) and drifts off along the seam at UndockSpringMS (the
// "spring release"), no teleport (ADR 0053 "Settled here" item 4). A
// re-arm latch keeps the pair from re-capturing at once. v0.8.3+.
//
// No-op when the craft has no DockedComponents (i.e. wasn't a
// composite). Active idx tracks to the first restored component
// so the player keeps flying the most-recently-active vessel's
// identity. The composite's Nodes / ActiveBurn / ManualBurn /
// AttitudeMode / EngineMode are dropped — they were tied to the
// composite, which no longer exists.
func (w *World) Undock(idx int) bool { return w.undockSplit(idx, false) }

// undockSplit is the split Undock and ReleaseBatch share. batch swaps the
// spring for the rotation fan (batch_fan.go), names and appends the pieces
// above the lead, stamps the batch id and the clamp exemption, and records one
// deploy per piece (ADR 0059 decision 3).
func (w *World) undockSplit(idx int, batch bool) bool {
	// A stack that lost bottom stages without its breakdown following (a
	// state a pre-#707 build could produce) is repaired first, so it tiles.
	if idx >= 0 && idx < len(w.Crafts) {
		w.Crafts[idx].ReconcileDockedComponents()
	}
	if w.UndockRefusal(idx) != "" {
		return false
	}
	c := w.Crafts[idx]

	// v0.12 / ADR 0009: decide whether the composite carries a full
	// per-component stage breakdown. When every component records its
	// Stages and the counts tile the composite's live Stages exactly
	// (Σ len(comp.Stages) == len(c.Stages)), we partition the LIVE
	// stages back to each component — preserving multi-stage structure
	// (the Apollo LM = Descent + Ascent) and, crucially, CURRENT fuel.
	// The firing Stages[0] is drained while docked (LOI burns the SM),
	// so the dock-time snapshot fuel is stale; the live slice is not.
	// DockCrafts/Transpose both build c.Stages as the in-order
	// concatenation of the components' stages, so sequential slicing is
	// exact. Falls back to the legacy single-stage prorate rebuild when
	// any component predates the breakdown (old saves) or the counts
	// don't tile — preserving back-compat.
	useBreakdown := true
	var stageSum int
	for _, comp := range c.DockedComponents {
		if len(comp.Stages) == 0 {
			useBreakdown = false
			break
		}
		stageSum += len(comp.Stages)
	}
	if stageSum != len(c.Stages) {
		useBreakdown = false
	}

	// Compute total capacity sums for the legacy share calculation.
	var totalCapFuel, totalCapMono float64
	for _, comp := range c.DockedComponents {
		totalCapFuel += comp.FuelCapacity
		totalCapMono += comp.MonopropCapacity
	}

	// ADR 0053 "Settled here" item 4: the pair parts along the seam, from the
	// seam. The lead (component 0) keeps its state; every component above it
	// is placed where its hull was drawn (the composite sprite draws it nose
	// to the seam, flipped: ComposeLaunchSprite's seams) and pushed
	// UndockSpringMS along +a_hat relative to the one below, nose pointing
	// back along -a_hat. No teleport: the hulls start in contact and open
	// into a gap the pilot can watch. The re-arm latch below, not distance,
	// keeps them from re-fusing; with a 0.5 m / 4 degree gate an accidental
	// re-dock needs a flown approach anyway (#343's secular-drift worry).
	axis := stackAxis(c)

	restored := make([]*spacecraft.Spacecraft, 0, len(c.DockedComponents))
	stageOffset := 0 // running index into c.Stages for the breakdown path
	for i, comp := range c.DockedComponents {
		var stages []spacecraft.Stage
		if useBreakdown {
			// Multi-stage path: peel the next len(comp.Stages) LIVE
			// stages off the composite (own backing array so the
			// restored craft doesn't alias the soon-discarded composite).
			cnt := len(comp.Stages)
			stages = append([]spacecraft.Stage(nil), c.Stages[stageOffset:stageOffset+cnt]...)
			stageOffset += cnt
		} else {
			// Legacy single-stage path: rebuild a one-element Stages
			// slice from the flat DockedComponent record, prorating the
			// composite's pooled fuel/monoprop by pre-dock capacity.
			var fuelShare, monoShare float64
			if totalCapFuel > 0 {
				fuelShare = c.Fuel * (comp.FuelCapacity / totalCapFuel)
			}
			if totalCapMono > 0 {
				monoShare = c.Monoprop * (comp.MonopropCapacity / totalCapMono)
			}
			// The rebuild must not strip the component's hardware: carry the
			// command source, antenna and ports of whatever stages it recorded
			// (#707), else the catalog ports of its loadout for a pre-breakdown
			// flat record (#683; the Stage.Ports() name lookup is gone).
			cmd, antKind, antRange, ports := spacecraft.PartsOfStages(comp.Stages)
			if len(comp.Stages) == 0 {
				ports = spacecraft.LookupCatalogPorts(comp.LoadoutID, comp.Name)
			}
			stages = []spacecraft.Stage{{
				CommandSource: cmd,
				AntennaKind:   antKind,
				AntennaRangeM: antRange,
				PartPorts:     ports,
				LoadoutID:     comp.LoadoutID,
				Name:          comp.Name,
				Glyph:         comp.Glyph,
				Color:         comp.Color,
				DryMass:       comp.DryMass,
				FuelMass:      fuelShare,
				FuelCapacity:  comp.FuelCapacity,
				Thrust:        comp.Thrust,
				Isp:           comp.Isp,
				MonopropMass:  monoShare,
				MonopropCap:   comp.MonopropCapacity,
				RCSThrust:     comp.RCSThrust,
				RCSIsp:        comp.RCSIsp,
				// v0.12 Slice 3 (ADR 0008): restore the surface-arrival
				// capabilities so an undocked lander / chute capsule keeps
				// its soft-land / parachute qualification. SyncFields below
				// re-derives the flat mirrors from this Stages[0].
				CanSoftLand:  comp.CanSoftLand,
				HasParachute: comp.HasParachute,
			}}
		}
		r, v, att := c.State.R, c.State.V, c.CurrentAttitudeDir
		if i > 0 {
			// Where this component's bottom row was drawn: the composite
			// sprite reflects it, so its row 0 sits at the TOP of its block.
			if useBreakdown {
				if top := StackRows(c.Stages[:stageOffset]); top > 0 {
					r = r.Add(axis.Scale(float64(top-1) * DockStrideM))
				}
			}
			if !batch {
				v = v.Add(axis.Scale(float64(i) * UndockSpringMS))
			}
			att = axis.Scale(-1)
		}
		s := w.restoreComponentCraft(c, comp, stages, r, v)
		s.CurrentAttitudeDir = att
		if batch {
			// A held stack stays a stack: the pieces keep the composite's
			// pointing mode, and a one-row piece (its hull is the same both
			// ways round) keeps the stack's direction, so a radial hold does
			// not swing 24 hulls round through the carrier's nose.
			s.AttitudeMode = c.AttitudeMode
			if StackRows(stages) == 1 {
				s.CurrentAttitudeDir = axis
			}
		}
		restored = append(restored, s)
	}

	if batch {
		w.applyBatchFan(c, restored, axis)
	}

	// Each restored component is a fresh vessel — stamp new stable IDs
	// (ADR 0012). The composite's ID is retired with it; a target that
	// pointed at the composite stops resolving rather than aliasing a
	// component. Targets aimed at the unaffected tail crafts keep
	// resolving by ID through the insert shift — no remap needed.
	for _, s := range restored {
		w.stampCraftID(s)
	}

	// #343: arm a same-World re-arm latch (localReArm) for every pair among
	// the just-restored components, mirroring the cross-player DockCooldown
	// latch's shape — checkDocking refuses to re-fuse any of these pairs
	// until their ports have separated past ReArmDistM or ReArmCeiling of sim-time
	// has passed, regardless of what the proximity gates alone say. Covers
	// both the along-track push's own secular drift and two of these
	// components simply drifting back together for unrelated reasons.
	var releasedAt time.Time
	if w.Clock != nil {
		releasedAt = w.Clock.SimTime
	}
	for i := 0; i < len(restored); i++ {
		for j := i + 1; j < len(restored); j++ {
			w.localReArms = append(w.localReArms, localReArm{
				idA: restored[i].ID, idB: restored[j].ID, releasedAt: releasedAt,
			})
		}
	}

	if batch {
		// The lead replaces the composite in place so digit slots keep their
		// vessels; the pieces follow at the end of the slate (ADR 0059 Q9 C).
		for _, s := range restored[1:] {
			s.Name = w.nextCraftName(s.Name)
		}
		w.stampBatch(c, restored)
		w.Crafts[idx] = restored[0]
		w.Crafts = append(w.Crafts, restored[1:]...)
		for range restored[1:] {
			w.RecordAction(missions.ActionDeploy)
		}
	} else {
		// Replace composite slot with restored components in place.
		tail := append([]*spacecraft.Spacecraft{}, w.Crafts[idx+1:]...)
		w.Crafts = append(w.Crafts[:idx], restored...)
		w.Crafts = append(w.Crafts, tail...)
	}

	// Active craft becomes the first restored component (matches
	// the "you keep flying the lead vessel" convention from
	// DockCrafts).
	w.SetActiveCraftIdx(idx)
	w.StopManualBurn()
	w.recordSplit(restored, c.Name, batch)
	return true
}

// UndockRefusal says, in the player's words, why Undock(idx) will refuse — or
// "" when it will split the composite. It is exhaustive over Undock's false
// paths BY CONSTRUCTION: Undock consults it instead of re-deriving the
// conditions, so the two cannot drift and a refused `U` can never be silent
// (#308 — live, a legitimate refusal read as "undock is broken" and cost an
// evening of diagnosis from the wrong premise).
//
// Each string names the situation and, where there is one, the way out. The
// caller renders it verbatim.
func (w *World) UndockRefusal(idx int) string {
	if idx < 0 || idx >= len(w.Crafts) || w.Crafts[idx] == nil {
		return "undock: no vessel selected"
	}
	c := w.Crafts[idx]
	if len(c.DockedComponents) < 2 {
		return "undock: nothing is docked to this vessel"
	}
	// v0.28 S5: a cross-player stack must not be split locally — that would
	// clone the guest's craft into this (the owner's) World while the guest
	// still believes it's docked. The guest undocks their own component
	// through the dock ledger (UndockGuest); this local path handles only
	// same-player composites (every component owned by this World).
	if StackHasGuest(c) {
		return "undock: cross-player stack — the guest releases it from their seat"
	}
	return ""
}

// radialOutUnit returns the unit vector pointing radially outward from the
// craft's primary (its position direction), or +X when the craft sits exactly
// at the origin. Shared by Deploy for its separation push, and by
// progradeUnit below as the fallback when velocity can't define a direction.
func radialOutUnit(c *spacecraft.Spacecraft) orbital.Vec3 {
	r := c.State.R
	if r.Norm() > 0 {
		return r.Scale(1 / r.Norm())
	}
	return orbital.Vec3{X: 1}
}

// progradeUnit returns the unit vector along the craft's current velocity
// (prograde) direction, falling back to radialOutUnit when the craft has no
// velocity to define one (e.g. landed at a pole, V≈0). Shared by Undock's
// local split and SeparationPush for the along-track separation push (#343):
// a purely radial push and position offset leaves a pair on slightly
// different orbital energy, whose secular along-track drift can still carry
// them back inside the docking gates after the fact; pushing along the
// velocity vector instead deliberately changes semi-major axis, so the
// separation it creates grows rather than reversing.
func progradeUnit(c *spacecraft.Spacecraft) orbital.Vec3 {
	v := c.State.V
	if v.Norm() > 0 {
		return v.Scale(1 / v.Norm())
	}
	return radialOutUnit(c)
}

// restoreComponentCraft synthesizes a standalone *Spacecraft from a docked
// component and the LIVE stages peeled off the composite, placed at (r, v).
// Shared by Undock (releases every component) and Deploy (pops the top
// payload). v0.23 / ADR 0028.
//
// ADR 0027: every other vessel-construction path (NewFromLoadout /
// NewFromStages / save-load) backfills a command source on a command-less
// stack so it stays controllable. A released component must too — otherwise a
// payload whose own stages carry no command source (an uncrewed custom
// payload, a legacy single-stage rebuild) derives Controllable=false and
// becomes unflyable debris. EnsureCommandSource is a no-op when the component
// already carries one (a crewed cabin), so it never overrides authored roles.
func (w *World) restoreComponentCraft(c *spacecraft.Spacecraft, comp spacecraft.DockedComponent, stages []spacecraft.Stage, r, v orbital.Vec3) *spacecraft.Spacecraft {
	s := &spacecraft.Spacecraft{
		Name:      comp.Name,
		LoadoutID: comp.LoadoutID,
		Role:      comp.Role,
		Glyph:     comp.Glyph,
		Color:     comp.Color,
		Throttle:  1.0,
		Stages:    stages,
		Primary:   c.Primary,
		// v0.16 / ADR 0015: a released component stays in the composite's System.
		SystemIdx: c.SystemIdx,
		State:     physics.StateVector{R: r, V: v},
	}
	spacecraft.EnsureCommandSource(s)
	s.SyncFields()
	s.State.M = s.TotalMass()
	return s
}

// deployablePayload is Deploy's precondition, factored out so the UI can
// ask "would Deploy refuse?" before it asks the player to confirm (ADR 0052
// decision 7: refusal first, then the ask). Returns the carrier, the top
// payload component, and whether a Deploy would succeed.
func (w *World) deployablePayload(idx int) (*spacecraft.Spacecraft, spacecraft.DockedComponent, bool) {
	if idx < 0 || idx >= len(w.Crafts) {
		return nil, spacecraft.DockedComponent{}, false
	}
	c := w.Crafts[idx]
	if c == nil || len(c.DockedComponents) < 2 {
		return nil, spacecraft.DockedComponent{}, false
	}
	// The top payload is the last component; its live stages are the top of
	// the composite's Stages (newCustomCraft / DockCrafts / Transpose all build
	// Stages as the in-order concatenation of the components, bottom-to-top).
	top := c.DockedComponents[len(c.DockedComponents)-1]
	cnt := len(top.Stages)
	if cnt <= 0 || cnt >= len(c.Stages) {
		// Malformed component breakdown (no per-stage record, or it claims the
		// whole stack) — refuse rather than strand the carrier. The legacy
		// single-stage prorate path Undock falls back to isn't reachable for
		// deployable composites, which always carry full Stages breakdowns.
		return nil, spacecraft.DockedComponent{}, false
	}
	return c, top, true
}

// DeployPayloadName names the payload Deploy would release from the craft
// at idx, and reports false when Deploy would refuse (nothing is changed
// either way).
func (w *World) DeployPayloadName(idx int) (string, bool) {
	_, top, ok := w.deployablePayload(idx)
	if !ok {
		return "", false
	}
	return top.Name, true
}

// Deploy releases the **topmost** nose payload of the composite at idx as its
// own craft while keeping the carrier active — the drop-and-continue verb for
// constellations and tugs (ADR 0028 decisions 3/4/7). Each press pops one
// payload: the last DockedComponent (the top of the stack) is peeled off,
// leaving the carrier flying with one fewer payload; once only the carrier
// core remains it reverts to a plain craft. Emits the `deploy` semantic
// action (ADR 0025 vocab) for the mission model.
//
// Contrast with Undock, which explodes the whole composite at once and
// switches the active craft to a released component. Deploy reuses Undock's
// per-component synthesis (separation push, command-source backfill, stable
// ID) via restoreComponentCraft but releases just the top and never changes
// the active craft. No-op (returns false) when the craft is not a loaded
// composite (fewer than two components). v0.23 / ADR 0028 C3-2.
func (w *World) Deploy(idx int) bool {
	c, top, ok := w.deployablePayload(idx)
	if !ok {
		return false
	}
	cnt := len(top.Stages)
	keep := len(c.Stages) - cnt
	payloadStages := append([]spacecraft.Stage(nil), c.Stages[keep:]...)

	// Push the payload clear of the auto-dock proximity gate, or checkDocking
	// re-fuses it onto the carrier next tick and the deploy silently undoes
	// itself. The carrier holds its state, so the whole gap is this one-sided
	// push — it must exceed DockingDistM, unlike Undock's symmetric ±spread
	// (two components, ~2× the per-side offset). Exceed DockingVMS too so the
	// velocity gate independently keeps them apart as they drift.
	const (
		separationM = DockingDistM * 1.5 // 75 m > the 50 m proximity gate
		pushVMS     = DockingVMS * 1.5   // 0.15 m/s > the 0.1 m/s velocity gate
	)
	radialOut := radialOutUnit(c)
	released := w.restoreComponentCraft(c, top, payloadStages,
		c.State.R.Add(radialOut.Scale(separationM)),
		c.State.V.Add(radialOut.Scale(pushVMS)))
	// Unique slate name (Relay Comsat-1, -2, …) so a constellation of identical
	// payloads stays distinguishable in target-cycle / HUD — every other spawn
	// path (SpawnCraft, buildJettisonedCraft) names new craft this way.
	released.Name = w.nextCraftName(released.Name)
	w.stampCraftID(released) // fresh stable identity (ADR 0012)

	// Shrink the carrier: drop the top component + its stages. Own backing
	// arrays so the carrier never aliases the released payload's stages.
	c.Stages = append([]spacecraft.Stage(nil), c.Stages[:keep]...)
	c.DockedComponents = c.DockedComponents[:len(c.DockedComponents)-1]
	if len(c.DockedComponents) < 2 {
		// Only the carrier core is left — it's no longer a composite.
		c.DockedComponents = nil
	}
	c.SyncFields()
	c.State.M = c.TotalMass()

	w.Crafts = append(w.Crafts, released)
	// The carrier keeps flying — no SetActiveCraftIdx, no StopManualBurn:
	// Deploy is drop-and-continue, so an in-progress burn / planted node on
	// the carrier survives (Undock drops them because the composite ceases to
	// exist; the carrier here does not).
	w.RecordAction(missions.ActionDeploy)
	return true
}

// Docking distances and speeds. Since ADR 0053 these no longer fuse anything
// by themselves: docking is the port gate (dockGateHolds, docking_magnet.go).
// DockingDistM (50 m) is "clear of the pair": the Proximity ring, the fit
// floor, the Deploy/staging/handback push distances, the re-arm distance and
// the warp-cap zone. DockingVMS (0.1 m/s) is the gate's relative-speed term
// and the push speeds' yardstick.
const (
	DockingDistM = 50.0 // metres, the ring radius and the "clear of the pair" distance.
	DockingVMS   = 0.1  // m/s, the gate's relative-speed term.
)

// DockSeams are the stage indices where each docked component above the lead
// begins, ascending: [] for a lone vessel. They come from the composite's
// DockedComponents stage counts, the same tiling Undock relies on, and are
// nil when that record does not tile the live stack (an old save): the sprite
// then draws the plain linear stack. ComposeLaunchSprite flips every block
// above a seam nose to the seam (ADR 0053 "Settled here" item 1).
func DockSeams(c *spacecraft.Spacecraft) []int {
	if c == nil || len(c.DockedComponents) < 2 {
		return nil
	}
	total := 0
	for _, comp := range c.DockedComponents {
		if len(comp.Stages) == 0 {
			return nil
		}
		total += len(comp.Stages)
	}
	if total != len(c.Stages) {
		return nil
	}
	seams := make([]int, 0, len(c.DockedComponents)-1)
	at := len(c.DockedComponents[0].Stages)
	for _, comp := range c.DockedComponents[1:] {
		seams = append(seams, at)
		at += len(comp.Stages)
	}
	return seams
}

// UndockSpringMS is the Spring Release: how fast each component above the
// seam is pushed along +a_hat, relative to the one below it, on Undock (ADR
// 0053 "Settled here" item 4).
const UndockSpringMS = 0.15

// ReArmDistM is the re-arm-by-leaving distance (ADR 0038 §5, amended
// 2026-10-25 for #794): once a pair undocks, a fresh dock between the SAME
// pair is refused until their PORTS have been more than this far apart, "back
// away first, then you can dock again", a rule with no invisible timer for a
// drift-back to silently outlast. Measured port to port (PortGapM), not
// anchor to anchor, and sized for the 0.5 m port gate: the old 100 m anchor
// distance was sized for the 50 m click, kept a released piece latched while
// the ring showed every term met, and was already 43 m at port contact on a
// CSM against an S-IVB+LM stack.
const ReArmDistM = 5.0

// ReArmCeiling bounds how long a re-arm latch may hold when the distance it
// waits on can never be read (#326). The latch clears on a POSITIVE range
// reading, and a partner who is landed, in another system, in another SOI, or
// simply not reporting yields no reading at all — so the ordinary reason to
// undock (one of you leaves) is exactly the case that holds the latch open
// forever, and a restart re-seeds it from disk with an empty report store.
//
// Measured in SIM-time against the release stamp the record already carries,
// not wall time: the latch is a statement about the pair's flight ("back away
// first"), and sim-time is the clock that flight happens on — under warp a
// pair who have not separated in ten minutes of flying are not about to. It
// costs nothing durable, since the stamp is a field the record already
// persists. Far beyond ADR 0038 §5's "silent instant re-fuse" worry, so it
// cannot expire a latch that is still doing its job; short enough that a
// vessel is never un-dockable for a session.
const ReArmCeiling = 10 * time.Minute

// pruneLocalReArms drops any local re-arm latch (#343) whose pair has
// separated past ReArmDistM, has outlived ReArmCeiling of sim-time since
// release, or names a craft no longer on the slate (staged / ended flight —
// nothing left to guard). Same shape as the cross-player reconcileCooldown,
// run once per checkDocking pass rather than per candidate pair.
func (w *World) pruneLocalReArms() {
	if len(w.localReArms) == 0 {
		return
	}
	kept := w.localReArms[:0]
	for _, r := range w.localReArms {
		if w.Clock != nil && !r.releasedAt.IsZero() && w.Clock.SimTime.Sub(r.releasedAt) > ReArmCeiling {
			continue
		}
		aC, _, aOK := w.craftByID(r.idA)
		bC, _, bOK := w.craftByID(r.idB)
		if !aOK || !bOK {
			continue
		}
		// An unreadable port gap (no port, or a nose not yet initialised) is
		// no evidence of separation: the latch holds, as the cross-player one
		// does, until the ceiling.
		ga, gb := VesselPortGeometry(aC), VesselPortGeometry(bC)
		if gap, ok := PortGapM(ga, aC.State.R, gb, bC.State.R); ok && gap > ReArmDistM {
			continue // cleared — the pair backed apart, latch has done its job
		}
		// A pair that provably cannot dock (a known shape with no port, like
		// every Starlink in a released batch) has nothing to guard.
		if (ga.Known && !ga.HasPort()) || (gb.Known && !gb.HasPort()) {
			continue
		}
		kept = append(kept, r)
	}
	w.localReArms = kept
}

// isLocalReArmed reports whether idA/idB currently hold a same-World re-arm
// latch (#343) — true holds the pair apart from checkDocking's auto-fuse
// regardless of what the proximity gates say. Assumes pruneLocalReArms has
// already run this tick.
func (w *World) isLocalReArmed(idA, idB uint64) bool {
	for _, r := range w.localReArms {
		if (r.idA == idA && r.idB == idB) || (r.idA == idB && r.idB == idA) {
			return true
		}
	}
	return false
}

// ReArmDocking clears the same-World re-arm latch(es) naming the active
// vessel — the flight-view `c` key's whole behaviour (#372). It is the
// explicit "yes, I meant it" a player presses instead of flying a 200 m
// round trip or waiting out ReArmCeiling. Nothing else changes: local
// docking stays automatic, so a pair already inside both proximity gates
// fuses on the very next checkDocking pass once the latch is gone, exactly
// as an un-latched approach would.
//
// When the Target slot holds a vessel, only the latch naming that EXACT
// pair clears — a targeted "yes, re-arm with them," which also refuses
// (ok=false) when that pair holds no latch even though some OTHER latch
// exists. Otherwise every latch naming the active vessel clears, which
// after an ordinary two-component undock is normally exactly one; a latch
// between two other craft (neither the active vessel) is never touched.
//
// partners names every latch actually cleared (the partner craft's Name,
// in w.localReArms order) for the caller's chip. ok is false when nothing
// was cleared, so a press with no latch held can say so rather than no-op
// silently (#372 acceptance) — a dead keypress reads as a broken key.
//
// Once cleared, the latch does not come back: pruneLocalReArms never
// re-arms one, and only a fresh Undock appends a new entry.
func (w *World) ReArmDocking() (partners []string, ok bool) {
	c := w.ActiveCraft()
	if c == nil || len(w.localReArms) == 0 {
		return nil, false
	}
	var targetID uint64
	haveTarget := w.Target.Kind == TargetCraft
	if haveTarget {
		targetID = w.Target.CraftID
	}
	kept := w.localReArms[:0]
	for _, r := range w.localReArms {
		var partnerID uint64
		switch c.ID {
		case r.idA:
			partnerID = r.idB
		case r.idB:
			partnerID = r.idA
		default:
			kept = append(kept, r) // doesn't name the active vessel — untouched
			continue
		}
		if haveTarget && partnerID != targetID {
			kept = append(kept, r) // names the active vessel but not the aimed partner
			continue
		}
		name := "partner"
		if pc, _, ok := w.craftByID(partnerID); ok {
			name = pc.Name
		}
		partners = append(partners, name)
		// Not appended to kept — this is the clear.
	}
	w.localReArms = kept
	return partners, len(partners) > 0
}

// checkDocking is the dock scan, run once per tick after integration. It
// owns the capture (ADR 0053 decision 5, docking_magnet.go): a running magnet
// pull is stepped (let go, keep pulling, or dock on completion); otherwise
// the first eligible pair that holds the port gate starts one. It no longer
// docks anything by itself, and the 50 m / 0.1 m/s click is gone. Returns
// the slate pair only on the tick a pull completes in a dock. v0.8.3+.
func (w *World) checkDocking() (int, int, bool) {
	if len(w.Crafts) < 2 {
		w.dockMagnet = nil
		return 0, 0, false
	}
	w.pruneLocalReArms()
	// A pull already running owns the tick: it either lets go, keeps
	// pulling, or completes (the dock).
	if w.dockMagnet != nil {
		return w.stepDockMagnet()
	}
	for i := 0; i < len(w.Crafts); i++ {
		a := w.Crafts[i]
		if a == nil {
			continue
		}
		for j := i + 1; j < len(w.Crafts); j++ {
			b := w.Crafts[j]
			if b == nil || !dockPairEligible(a, b) {
				continue
			}
			// ADR 0053 decision 5: the port gate, not the old 50 m / 0.1 m/s
			// click. Holding it starts the magnet; the dock is the magnet
			// completing.
			if !dockPairGateHolds(a, b) {
				continue
			}
			// #343: a just-undocked (or otherwise latched) pair stays apart
			// regardless of the gate, the local mirror of the cross-player
			// re-arm-by-leaving latch. The refusal is not silent: the port
			// row carries a standing "latched" line (#794).
			if w.isLocalReArmed(a.ID, b.ID) {
				continue
			}
			w.startDockMagnet(i, j)
			return 0, 0, false
		}
	}
	return 0, 0, false
}

// DockCrafts fuses two craft (by slate index) into a single
// composite. The lead's position (its hull does not move; ADR 0053
// "Settled here" item 1); momentum-
// conserving combination for velocity; summed pools for fuel,
// monoprop, and capacities; concatenated roles. The active
// partner's name, glyph, color, nodes, and engine state survive
// — its loadout becomes the composite's identity. The other
// partner is removed from the slate. v0.8.3+.
//
// If neither partner has a main engine, the composite ends up
// RCS-only (Thrust=0). If only one has a main, the composite
// inherits that engine (the lit stage, spacecraft.LitStageIdx: an
// engineless leader's thrust passes up to the first engine above it).
//
// ActiveCraftIdx is adjusted so the player keeps flying the
// composite (the active partner's slate position).
//
// #421: also stashes World.LastDockEvent (partner name + resulting
// component count included) for the HUD's auto-fuse announcement.
// Captured here, before fuseComposite drops the partner's slot, because
// by the time a caller further up (World.Tick) could look for it the
// slate has already changed shape and the dropped identity is gone.
func (w *World) DockCrafts(idxA, idxB int) {
	if idxA == idxB {
		return
	}
	if idxA < 0 || idxB < 0 || idxA >= len(w.Crafts) || idxB >= len(w.Crafts) {
		return
	}
	// Decide which slot owns the composite. "Lead" is the active
	// partner when either matches ActiveCraftIdx, else the first
	// argument. The composite occupies the lead's slot; the drop
	// slot is removed from the slate. v0.8.3+: pre-fix had two
	// swaps (active-canonical + idxA<idxB) that could undo each
	// other when the active partner was the higher-indexed craft.
	lead, drop := idxA, idxB
	if idxB == w.ActiveCraftIdx {
		lead, drop = idxB, idxA
	}
	partnerName := ""
	if c := w.Crafts[drop]; c != nil {
		partnerName = c.Name
	}
	composite, newLead, ok := w.fuseComposite(lead, drop)
	if !ok || composite == nil {
		return
	}
	var when time.Time
	if w.Clock != nil {
		when = w.Clock.SimTime
	}
	w.LastDockEvent = &DockEvent{
		When:           when,
		CraftIdx:       newLead,
		PartnerIdx:     drop,
		CompositeName:  composite.Name,
		PartnerName:    partnerName,
		ComponentCount: len(composite.DockedComponents),
	}
	w.recordRowFor(composite, "dock: with "+partnerName)
}

// fuseComposite fuses the craft at dropIdx into the craft at leadIdx,
// producing one composite that keeps the LEAD's identity, and returns the
// composite plus its slate index after the drop slot is removed. Unlike
// DockCrafts (which picks lead/drop from ActiveCraftIdx), the caller fixes
// which slot leads — DockGuestCraft forces the docker to lead so the fused
// stack takes on the docker's identity and this World's ownership regardless
// of which slot is active. The mass/velocity/stage/component/reindex math is
// identical to the classic single-World dock. ok is false for invalid or nil
// slots or a zero-mass pair. v0.28 S5.
func (w *World) fuseComposite(lead, drop int) (*spacecraft.Spacecraft, int, bool) {
	return w.fuseCompositeAt(lead, drop, false)
}

// fuseCompositeAt is fuseComposite with the composite's position chosen:
// atCentroid=false keeps the LEAD's State.R (ADR 0053 "Settled here" item 1:
// the hulls stay where they were, no jump to a mass centroid a stack length
// away), the rule both the local dock and the cross-player guest dock
// (DockGuestCraft) follow; true is the legacy mass-weighted centroid, kept
// only for callers that want it.
func (w *World) fuseCompositeAt(lead, drop int, atCentroid bool) (*spacecraft.Spacecraft, int, bool) {
	if lead == drop || lead < 0 || drop < 0 || lead >= len(w.Crafts) || drop >= len(w.Crafts) {
		return nil, -1, false
	}
	a := w.Crafts[lead]
	b := w.Crafts[drop]
	if a == nil || b == nil {
		return nil, -1, false
	}

	mA := a.TotalMass()
	mB := b.TotalMass()
	mTotal := mA + mB
	if mTotal <= 0 {
		return nil, -1, false
	}

	composite := *a // shallow copy preserves the lead partner's identity.
	// ADR 0063 §1: a composite exposes no port, so a Port* hold becomes
	// Inertial here and the stack keeps its nose (no slew toward prograde).
	if spacecraft.IsPortMode(composite.AttitudeMode) {
		composite.HoldInertial()
	}

	// v0.9.1+: composite Stages = lead's stages with the partner's
	// stages appended on top. Resolves scoping #4 (composite-craft
	// mass distribution): the appended-on-top order means undocking
	// can split correctly along stage boundaries — the partner's
	// stages were higher up in the stack post-dock, so they peel off
	// as a unit when Undock fires.
	//
	// The bottom stage of the composite is unchanged (it's still
	// `a`'s bottom — the active partner's currently-firing engine).
	// Composite thrust / Isp accessors degenerate cleanly because
	// SyncFields reads the lit stage (the lowest stage with an engine,
	// Stages[0] when the lead is powered) for engine numbers; the
	// player keeps firing the same engine they were before docking.
	//
	// For the **composite-as-a-whole** thrust + Isp readout (which
	// the v0.9 plan §"Code surface (composite-craft post-docking)"
	// committed to "sum thrust, mass-weighted Isp"), see
	// CompositeEngineSummary below — it walks the full Stages slice
	// for any consumer that wants the pooled-engine view rather than
	// the bottom-stage view. Pre-v0.9.1 callers that read c.Thrust /
	// c.Isp keep getting the bottom-stage values (back-compat) until
	// they migrate to the summary helper.
	composite.Stages = make([]spacecraft.Stage, 0, len(a.Stages)+len(b.Stages))
	composite.Stages = append(composite.Stages, a.Stages...)
	composite.Stages = append(composite.Stages, b.Stages...)
	composite.SyncFields()

	compR := a.State.R
	if atCentroid {
		compR = orbital.Vec3{
			X: (mA*a.State.R.X + mB*b.State.R.X) / mTotal,
			Y: (mA*a.State.R.Y + mB*b.State.R.Y) / mTotal,
			Z: (mA*a.State.R.Z + mB*b.State.R.Z) / mTotal,
		}
	}
	composite.State = physics.StateVector{
		R: compR,
		V: orbital.Vec3{
			X: (mA*a.State.V.X + mB*b.State.V.X) / mTotal,
			Y: (mA*a.State.V.Y + mB*b.State.V.Y) / mTotal,
			Z: (mA*a.State.V.Z + mB*b.State.V.Z) / mTotal,
		},
	}
	composite.State.M = composite.TotalMass()

	// Concatenate roles when distinct, e.g. "transfer-stage+lander".
	if b.Role != "" && a.Role != b.Role {
		if a.Role == "" {
			composite.Role = b.Role
		} else {
			composite.Role = a.Role + "+" + b.Role
		}
	}

	// Record component identities so Undock can restore them.
	// Flatten chained docks: if either partner was already a
	// composite, splice its components into the new list rather
	// than nesting.
	composite.DockedComponents = nil
	if len(a.DockedComponents) > 0 {
		composite.DockedComponents = append(composite.DockedComponents, a.DockedComponents...)
	} else {
		composite.DockedComponents = append(composite.DockedComponents, a.AsDockedComponent())
	}
	if len(b.DockedComponents) > 0 {
		composite.DockedComponents = append(composite.DockedComponents, b.DockedComponents...)
	} else {
		composite.DockedComponents = append(composite.DockedComponents, b.AsDockedComponent())
	}

	w.Crafts[lead] = &composite
	w.Crafts = append(w.Crafts[:drop], w.Crafts[drop+1:]...)

	// Re-resolve the lead index after the drop removal: if drop
	// was before lead in the slate, lead shifts down by one.
	newLead := lead
	if drop < lead {
		newLead = lead - 1
	}
	switch {
	case w.ActiveCraftIdx == lead || w.ActiveCraftIdx == drop:
		// Player ends up on the composite regardless of which slot
		// they were flying.
		w.SetActiveCraftIdx(newLead)
	case w.ActiveCraftIdx > drop:
		// Slate shifts left around the dropped slot — the active
		// craft pointer follows. Same craft, no target rebind.
		w.ActiveCraftIdx--
	}
	return &composite, newLead, true
}
