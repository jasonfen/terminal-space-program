package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

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
	composite.DryMass = a.DryMass + b.DryMass
	composite.Fuel = a.Fuel + b.Fuel
	composite.Monoprop = a.Monoprop + b.Monoprop
	composite.MonopropCapacity = a.MonopropCapacity + b.MonopropCapacity
	composite.RCSThrust = a.RCSThrust + b.RCSThrust

	// Engine pick: keep the active partner's main if it has one,
	// else inherit from the other. RCS Isp likewise.
	if composite.Thrust <= 0 && b.Thrust > 0 {
		composite.Thrust = b.Thrust
		composite.Isp = b.Isp
	}
	if composite.RCSIsp <= 0 && b.RCSIsp > 0 {
		composite.RCSIsp = b.RCSIsp
	}

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
		w.ActiveCraftIdx = newLead
	case w.ActiveCraftIdx > drop:
		w.ActiveCraftIdx--
	}
}
