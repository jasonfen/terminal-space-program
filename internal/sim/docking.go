package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Undock splits the craft at idx back into its DockedComponents,
// removing the composite from the slate and inserting one craft
// per component. Each restored craft inherits a share of the
// composite's current Fuel + Monoprop pools, prorated by its
// pre-dock capacity. Restored craft sit near the composite's
// current position, separated by a small offset so they don't
// immediately re-dock; their velocities pick up a tiny relative
// push (a "spring release") so they drift apart. v0.8.3+.
//
// No-op when the craft has no DockedComponents (i.e. wasn't a
// composite). Active idx tracks to the first restored component
// so the player keeps flying the most-recently-active vessel's
// identity. The composite's Nodes / ActiveBurn / ManualBurn /
// AttitudeMode / EngineMode are dropped — they were tied to the
// composite, which no longer exists.
func (w *World) Undock(idx int) bool {
	if idx < 0 || idx >= len(w.Crafts) {
		return false
	}
	c := w.Crafts[idx]
	if c == nil || len(c.DockedComponents) < 2 {
		return false
	}

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

	// Synthesize new craft, pushed apart by a small offset along a
	// 1-AU axis (radial-out from primary). Per-side offset 35 m
	// (so 2-component split is 70 m total — outside DockingDistM
	// of 50 m, no immediate re-fuse); +0.05 m/s relative gives
	// them clear separation drift.
	const (
		separationM = 35.0
		pushVMS     = 0.05
	)
	radialOut := c.State.R
	if radialOut.Norm() > 0 {
		radialOut = radialOut.Scale(1 / radialOut.Norm())
	} else {
		radialOut = orbital.Vec3{X: 1}
	}

	restored := make([]*spacecraft.Spacecraft, 0, len(c.DockedComponents))
	n := len(c.DockedComponents)
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
			stages = []spacecraft.Stage{{
				LoadoutID:    comp.LoadoutID,
				Name:         comp.Name,
				Glyph:        comp.Glyph,
				Color:        comp.Color,
				DryMass:      comp.DryMass,
				FuelMass:     fuelShare,
				FuelCapacity: comp.FuelCapacity,
				Thrust:       comp.Thrust,
				Isp:          comp.Isp,
				MonopropMass: monoShare,
				MonopropCap:  comp.MonopropCapacity,
				RCSThrust:    comp.RCSThrust,
				RCSIsp:       comp.RCSIsp,
				// v0.12 Slice 3 (ADR 0008): restore the surface-arrival
				// capabilities so an undocked lander / chute capsule keeps
				// its soft-land / parachute qualification. SyncFields below
				// re-derives the flat mirrors from this Stages[0].
				CanSoftLand:  comp.CanSoftLand,
				HasParachute: comp.HasParachute,
			}}
		}
		// Spread components symmetrically around the composite's
		// current position. For 2 components: -25 m and +25 m on
		// the radial axis. For N: even spacing in [-1, +1].
		offset := -1.0 + 2.0*float64(i)/float64(n-1)
		s := &spacecraft.Spacecraft{
			Name:      comp.Name,
			LoadoutID: comp.LoadoutID,
			Role:      comp.Role,
			Glyph:     comp.Glyph,
			Color:     comp.Color,
			Throttle:  1.0,
			Stages:    stages,
			Primary:   c.Primary,
			State: physics.StateVector{
				R: c.State.R.Add(radialOut.Scale(offset * separationM)),
				V: c.State.V.Add(radialOut.Scale(offset * pushVMS)),
			},
		}
		s.SyncFields()
		s.State.M = s.TotalMass()
		restored = append(restored, s)
	}

	// Each restored component is a fresh vessel — stamp new stable IDs
	// (ADR 0012). The composite's ID is retired with it; a target that
	// pointed at the composite stops resolving rather than aliasing a
	// component. Targets aimed at the unaffected tail crafts keep
	// resolving by ID through the insert shift — no remap needed.
	for _, s := range restored {
		w.stampCraftID(s)
	}

	// Replace composite slot with restored components in place.
	tail := append([]*spacecraft.Spacecraft{}, w.Crafts[idx+1:]...)
	w.Crafts = append(w.Crafts[:idx], restored...)
	w.Crafts = append(w.Crafts, tail...)

	// Active craft becomes the first restored component (matches
	// the "you keep flying the lead vessel" convention from
	// DockCrafts).
	w.SetActiveCraftIdx(idx)
	w.StopManualBurn()
	return true
}

// CompositeEngineSummary returns the pooled-engine view of a craft
// that may have multiple thrusting stages: sum-thrust across every
// stage with main thrust, mass-weighted-Isp by `Σ(Isp · thrust) /
// Σ thrust`. Resolves the v0.9 plan scoping decision #4 ("default:
// sum thrust, mass-weighted average Isp") for downstream consumers
// that want the composite-as-a-whole view rather than the bottom-
// stage view. v0.9.1+.
//
// Returns (totalThrust=0, weightedIsp=0) when no stage has thrust
// (RCS-tug class composites). For single-stage craft and composites
// where only one stage has thrust, this returns the same numbers as
// reading c.Thrust + c.Isp directly — the helper degenerates
// correctly for the common case.
func CompositeEngineSummary(c *spacecraft.Spacecraft) (totalThrust, weightedIsp float64) {
	if c == nil {
		return 0, 0
	}
	for _, s := range c.Stages {
		if s.Thrust <= 0 {
			continue
		}
		totalThrust += s.Thrust
		weightedIsp += s.Isp * s.Thrust
	}
	if totalThrust > 0 {
		weightedIsp /= totalThrust
	}
	return totalThrust, weightedIsp
}

// Docking proximity gates. Craft within DockingDistM and below
// |v_rel| = DockingVMS while sharing the same primary frame fuse
// into a single composite at the next tick. v0.8.3+.
const (
	DockingDistM = 50.0  // metres — KSP-ish "soft capture" distance.
	DockingVMS   = 0.1   // m/s — typical proximity-ops null-residual.
)

// checkDocking scans every craft pair in the same primary frame
// for a docking-eligible encounter (proximity + relative velocity
// below the gate). On a match, calls DockCrafts and returns —
// the slate has changed; subsequent pair checks deferred to next
// tick. v0.8.3+.
func (w *World) checkDocking() (int, int, bool) {
	if len(w.Crafts) < 2 {
		return 0, 0, false
	}
	for i := 0; i < len(w.Crafts); i++ {
		a := w.Crafts[i]
		if a == nil {
			continue
		}
		for j := i + 1; j < len(w.Crafts); j++ {
			b := w.Crafts[j]
			if b == nil {
				continue
			}
			// SOI mismatch — won't be near each other in the same
			// frame even if their inertial positions are close.
			if a.Primary.ID != b.Primary.ID {
				continue
			}
			// Either Landed — skip (v0.12 Slice 2 / ADR 0007, broadened
			// after the surface-staging playtest). A Landed craft must
			// not auto-fuse with anything by proximity:
			//
			//   1. A surface-staged descent + ascent pair sits co-
			//      located at the same lat/lon, both Landed, with
			//      identical V = ω×R — inside both gates. integrateLanded
			//      re-pins each from its stored coords every tick, so the
			//      orbital retrograde-nudge separation can't apply.
			//   2. When the player ignites the ascent to lift off, its
			//      Landed flag clears while it is STILL co-located with
			//      the parked descent stage (it hasn't climbed clear
			//      yet). A both-Landed-only guard would let that
			//      liftoff moment re-fuse the pair — exactly the
			//      "undocking rejoins the vessels" bug.
			//
			// Skipping when EITHER craft is Landed closes both windows.
			// Deliberate landed-docking (moon bases) is a future feature
			// that will revisit this guard (ADR 0007 forward hook);
			// orbital rendezvous docking is unaffected (both partners
			// are in flight, Landed=false).
			if a.Landed || b.Landed {
				continue
			}
			dr := a.State.R.Sub(b.State.R)
			if dr.Norm() > DockingDistM {
				continue
			}
			dv := a.State.V.Sub(b.State.V)
			if dv.Norm() > DockingVMS {
				continue
			}
			w.DockCrafts(i, j)
			return i, j, true
		}
	}
	return 0, 0, false
}

// DockCrafts fuses two craft (by slate index) into a single
// composite. Mass-weighted centroid for position; momentum-
// conserving combination for velocity; summed pools for fuel,
// monoprop, and capacities; concatenated roles. The active
// partner's name, glyph, color, nodes, and engine state survive
// — its loadout becomes the composite's identity. The other
// partner is removed from the slate. v0.8.3+.
//
// If neither partner has a main engine, the composite ends up
// RCS-only (Thrust=0). If only one has a main, the composite
// inherits that engine.
//
// ActiveCraftIdx is adjusted so the player keeps flying the
// composite (the active partner's slate position).
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
	a := w.Crafts[lead]
	b := w.Crafts[drop]
	if a == nil || b == nil {
		return
	}

	mA := a.TotalMass()
	mB := b.TotalMass()
	mTotal := mA + mB
	if mTotal <= 0 {
		return
	}

	composite := *a // shallow copy preserves the lead partner's identity.

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
	// SyncFields reads Stages[0] for engine numbers; the player
	// keeps firing the same engine they were before docking.
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

	composite.State = physics.StateVector{
		R: orbital.Vec3{
			X: (mA*a.State.R.X + mB*b.State.R.X) / mTotal,
			Y: (mA*a.State.R.Y + mB*b.State.R.Y) / mTotal,
			Z: (mA*a.State.R.Z + mB*b.State.R.Z) / mTotal,
		},
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
}
