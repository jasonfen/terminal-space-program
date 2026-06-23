// v0.9.1: schema v5 → v6 migration. v5 saves carry flat propulsion
// fields (DryMass / Fuel / Isp / Thrust / Monoprop / MonopropCapacity
// / RCSThrust / RCSIsp) on the Craft entry. v6 wraps those into a
// single-element Stages slice so the spacecraft.Spacecraft.Stages
// source-of-truth lights up regardless of save vintage.

package save

import "github.com/jasonfen/terminal-space-program/internal/spacecraft"

// wireStagesToSim copies the wire Stage form (this package's Stage
// type) into the sim's spacecraft.Stage form. v0.9.1+. Returns an
// empty slice when the wire entry is empty — caller falls back to
// migrateV5CraftToStages for pre-v6 saves.
func wireStagesToSim(wire []Stage) []spacecraft.Stage {
	if len(wire) == 0 {
		return nil
	}
	out := make([]spacecraft.Stage, len(wire))
	for i, s := range wire {
		out[i] = spacecraft.Stage{
			LoadoutID:            s.LoadoutID,
			Name:                 s.Name,
			Glyph:                s.Glyph,
			Color:                s.Color,
			DryMass:              s.DryMass,
			FuelMass:             s.FuelMass,
			FuelCapacity:         s.FuelCapacity,
			Thrust:               s.Thrust,
			Isp:                  s.Isp,
			MonopropMass:         s.MonopropMass,
			MonopropCap:          s.MonopropCap,
			RCSThrust:            s.RCSThrust,
			RCSIsp:               s.RCSIsp,
			BallisticCoefficient: s.BallisticCoefficient,
			CanSoftLand:          s.CanSoftLand,
			HasParachute:         s.HasParachute,
			CommandSource:        s.CommandSource,
			AntennaKind:          s.AntennaKind,
			AntennaRangeM:        s.AntennaRangeM,
		}
		// Save-compat (ADR 0027 §2 amendment): a pre-amendment save stored the
		// antenna's legacy power in antenna_power_w (now an ignored key), so
		// AntennaRangeM loads as 0 even though the antenna kind survives. A real
		// antenna always has a positive range, so kind!=none with range 0
		// uniquely marks an old save — re-range it from its kind's tier.
		if out[i].AntennaKind != spacecraft.AntennaNone && out[i].AntennaRangeM == 0 {
			out[i].AntennaRangeM = spacecraft.DefaultAntennaRangeForKind(out[i].AntennaKind)
		}
	}
	return out
}

// simStagesToWire copies the sim's spacecraft.Stage form into this
// package's wire Stage form — the inverse of wireStagesToSim. v0.12 /
// ADR 0009: used for both Craft.Stages and DockedComponent.Stages so
// the multi-stage docked-component breakdown round-trips. Returns nil
// for an empty input (keeps the omitempty wire field absent).
func simStagesToWire(stages []spacecraft.Stage) []Stage {
	if len(stages) == 0 {
		return nil
	}
	out := make([]Stage, len(stages))
	for i, s := range stages {
		out[i] = Stage{
			LoadoutID:            s.LoadoutID,
			Name:                 s.Name,
			Glyph:                s.Glyph,
			Color:                s.Color,
			DryMass:              s.DryMass,
			FuelMass:             s.FuelMass,
			FuelCapacity:         s.FuelCapacity,
			Thrust:               s.Thrust,
			Isp:                  s.Isp,
			MonopropMass:         s.MonopropMass,
			MonopropCap:          s.MonopropCap,
			RCSThrust:            s.RCSThrust,
			RCSIsp:               s.RCSIsp,
			BallisticCoefficient: s.BallisticCoefficient,
			CanSoftLand:          s.CanSoftLand,
			HasParachute:         s.HasParachute,
			CommandSource:        s.CommandSource,
			AntennaKind:          s.AntennaKind,
			AntennaRangeM:        s.AntennaRangeM,
		}
	}
	return out
}

// migrateV5CraftToStages wraps the v5 flat fields of wc into a
// single-element Stages slice. The RCS pool numbers are passed in
// because v5 had its own backfill for pre-v0.8.0 saves (loader
// resolves DefaultRCSLoadout when v5 fields are zero); we forward
// those resolved numbers here so the migrated stage carries the
// effective values, not the raw zero ones.
//
// FuelCapacity (a v0.9.1 field with no v5 analogue) is set equal to
// FuelMass — pre-v6 saves treated the live fuel as the de-facto
// capacity for proportional undock-share calculations, and there's
// no record of pristine-tank capacity in v5. Post-migration, capacity
// drifts upward only when a craft is fueled to a higher mark; v0.9.1
// playtests can surface UX gaps if any.
//
// Glyph / Color / Name on the synthesised Stage default to the wire
// craft's identity so a jettisoned migrated single-stage looks the
// same as it did pre-v0.9.1.
func migrateV5CraftToStages(wc Craft, monoprop, monoCap, rcsThrust, rcsIsp float64) []spacecraft.Stage {
	return []spacecraft.Stage{{
		LoadoutID:    wc.LoadoutID,
		Name:         wc.Name,
		Glyph:        wc.Glyph,
		Color:        wc.Color,
		DryMass:      wc.DryMass,
		FuelMass:     wc.Fuel,
		FuelCapacity: wc.Fuel, // v5 had no pristine-capacity record; live = effective.
		Thrust:       wc.Thrust,
		Isp:          wc.Isp,
		MonopropMass: monoprop,
		MonopropCap:  monoCap,
		RCSThrust:    rcsThrust,
		RCSIsp:       rcsIsp,
	}}
}
