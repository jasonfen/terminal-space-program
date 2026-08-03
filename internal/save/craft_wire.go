package save

import (
	"fmt"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Per-craft wire conversion, lifted out of payloadFromWorld /
// worldFromPayload so ONE craft can round-trip on its own — the dock
// ledger's durable Parcels and in-flight handoffs (ADR 0040) are single
// craft parked outside any World, and the type comment on
// relay.DockRecord always said they "round-trip through the save
// package". The World-level paths call these, so a craft persisted on a
// dock record can never drift from a craft persisted in a save.

// CraftToWire projects one live craft onto its serialisable form. It is
// the exact per-craft body payloadFromWorld runs, so anything a save
// carries a Parcel carries too. Ghost refs on nodes / the active burn are
// dropped (their owner fingerprint is not persisted and their craft IDs
// are remote), and a ghost Target normalises to no-target — both on
// copies, so the live craft is untouched.
func CraftToWire(c *spacecraft.Spacecraft) Craft {
	if c == nil {
		return Craft{}
	}
	wc := Craft{
		ID:                 c.ID,
		SystemIdx:          c.SystemIdx, // v0.16 / schema v8 (ADR 0015): per-Vessel System binding.
		Name:               c.Name,
		DryMass:            c.DryMass,
		Fuel:               c.Fuel,
		Isp:                c.Isp,
		Thrust:             c.Thrust,
		PrimaryID:          c.Primary.ID,
		R:                  vec3From(c.State.R),
		V:                  vec3From(c.State.V),
		M:                  c.State.M,
		Monoprop:           c.Monoprop,
		MonopropCapacity:   c.MonopropCapacity,
		RCSThrust:          c.RCSThrust,
		RCSIsp:             c.RCSIsp,
		AttitudeMode:       int(c.AttitudeMode),
		EngineMode:         int(c.EngineMode),
		LoadoutID:          c.LoadoutID,
		Role:               c.Role,
		Glyph:              c.Glyph,
		Color:              c.Color,
		PitchTrim:          c.PitchTrim,
		CurrentAttitudeDir: vec3From(c.CurrentAttitudeDir),
		Landed:             c.Landed,
		LaunchLatDeg:       c.LaunchLatDeg,
		LaunchLonDeg:       c.LaunchLonDeg,
		Crashed:            c.Crashed,
		CanSoftLand:        c.CanSoftLand,
		OnPad:              c.OnPad,
		LandedLatDeg:       c.LandedLatDeg,
		LandedLonDeg:       c.LandedLonDeg,
		DecouplePlan:       c.DecouplePlan,
		ChuteState:         int(c.ChuteState),
	}
	// v0.9.1+: serialize Stages so v6 saves carry per-stage detail.
	// Single-stage craft still wire out a one-element Stages — round-trips
	// through the same migrate path that v5 craft fall through. Shares
	// simStagesToWire with DockedComponent.Stages so a new Stage field
	// can't be dropped from one path but not the other.
	wc.Stages = simStagesToWire(c.Stages)
	for _, dc := range c.DockedComponents {
		wc.DockedComponents = append(wc.DockedComponents, DockedComponent{
			Name:             dc.Name,
			LoadoutID:        dc.LoadoutID,
			Role:             dc.Role,
			Glyph:            dc.Glyph,
			Color:            dc.Color,
			DryMass:          dc.DryMass,
			FuelCapacity:     dc.FuelCapacity,
			MonopropCapacity: dc.MonopropCapacity,
			Isp:              dc.Isp,
			Thrust:           dc.Thrust,
			RCSThrust:        dc.RCSThrust,
			RCSIsp:           dc.RCSIsp,
			CanSoftLand:      dc.CanSoftLand,
			HasParachute:     dc.HasParachute,
			Stages:           simStagesToWire(dc.Stages),
			Owner:            dc.Owner,
			CraftID:          dc.CraftID,
		})
	}
	for _, n := range c.Nodes {
		var trigNano int64
		if !n.TriggerTime.IsZero() {
			trigNano = n.TriggerTime.UnixNano()
		}
		// v0.28 S4: a node planted against a ghost (remote player's craft)
		// drops its ghost ref on save — the owner fingerprint isn't
		// persisted, and its TargetCraftID is a REMOTE id that would collide
		// with a local id on load. DropGhostRef zeroes both while keeping the
		// burn geometry. No-op for local / untargeted nodes. Mirrors the
		// ghost-target normalisation below. n is a copy, so this doesn't
		// mutate the live world.
		n.DropGhostRef()
		wc.Nodes = append(wc.Nodes, Node{
			ID:              n.ID,
			TriggerTimeNano: trigNano,
			Mode:            int(n.Mode),
			DV:              n.DV,
			DurationNano:    int64(n.Duration),
			PrimaryID:       n.PrimaryID,
			Event:           int(n.Event),
			Throttle:        n.Throttle,
			TargetCraftID:   n.TargetCraftID,
			PlaneChangeRad:  n.PlaneChangeRad,
			BurnDirUnit:     vec3From(n.BurnDirUnit),
			AdvisoryKey:     n.AdvisoryKey,
		})
	}
	if c.ActiveBurn != nil {
		// v0.28 S4: drop a ghost ref on a running burn for the same reason as
		// nodes above — the copy keeps the live burn intact.
		ab := *c.ActiveBurn
		ab.DropGhostRef()
		wc.ActiveBurn = &ActiveBurn{
			Mode:           int(ab.Mode),
			DVRemaining:    ab.DVRemaining,
			EndTimeNano:    ab.EndTime.UnixNano(),
			PrimaryID:      ab.PrimaryID,
			Throttle:       ab.Throttle,
			TargetCraftID:  ab.TargetCraftID,
			PlaneChangeRad: ab.PlaneChangeRad,
			BurnDirUnit:    vec3From(ab.BurnDirUnit),
		}
	}
	// v0.9.3 polish: per-craft Target. Skip serialising when the craft has
	// no target so untargeted craft still write out the same minimal JSON
	// they did pre-polish.
	//
	// Ghost targets (v0.27) are session-local — the owner fingerprint isn't
	// persisted, so a saved ghost ref could never resolve again. Normalise
	// to no-target instead of writing a permanently-stuck Kind (also keeps
	// the persisted Kind vocabulary at its pre-v0.27 range — no schema bump).
	if c.Target.Kind != spacecraft.TargetNone && c.Target.Kind != spacecraft.TargetGhost {
		wc.Target = &Target{
			Kind:    int(c.Target.Kind),
			BodyIdx: c.Target.BodyIdx,
			CraftID: c.Target.CraftID,
		}
	}
	return wc
}

// CraftFromWire rehydrates one wire craft against the loaded systems. It
// is the exact per-craft body worldFromPayload runs, including every
// backfill older envelopes rely on (RCS loadout, single-stage migration,
// glyph/colour, command source). ErrCraftPrimary when the craft names a
// primary no loaded System holds.
func CraftFromWire(wc Craft, systems []bodies.System) (*spacecraft.Spacecraft, error) {
	// v0.16 / schema v8 (ADR 0015): rehydrate the Primary from the Vessel's
	// *own* System rather than scanning all systems, so a cross-System
	// body-ID collision (e.g. a user overlay) can't mis-rehydrate the
	// Primary. The v7→v8 migration has already set SystemIdx for pre-v8
	// saves; clamp a corrupt index to Sol.
	sysIdx := wc.SystemIdx
	if sysIdx < 0 || sysIdx >= len(systems) {
		sysIdx = 0
	}
	if len(systems) == 0 {
		return nil, fmt.Errorf("%w: %q (no systems loaded)", ErrCraftPrimary, wc.PrimaryID)
	}
	primaryPtr := systems[sysIdx].FindBody(wc.PrimaryID)
	if primaryPtr == nil {
		return nil, fmt.Errorf("%w: %q", ErrCraftPrimary, wc.PrimaryID)
	}
	primary := *primaryPtr
	// v0.8.0+: pre-RCS saves (v3 and earlier wire-out) carry zero RCS
	// fields. Populate from DefaultRCSLoadout(DryMass) so older saves
	// inherit a full RCS budget without a schema bump.
	monoprop := wc.Monoprop
	monoCap := wc.MonopropCapacity
	rcsThrust := wc.RCSThrust
	rcsIsp := wc.RCSIsp
	if monoCap == 0 && rcsThrust == 0 && rcsIsp == 0 {
		monoprop, monoCap, rcsThrust, rcsIsp = spacecraft.DefaultRCSLoadout(wc.DryMass)
	}
	// v0.9.1+: build Stages from the wire form, falling back to a
	// single-element migration of the v5 flat fields when the wire entry
	// doesn't carry Stages (pre-v6 saves OR v6 saves where the flat fields
	// predate the migration). Once Stages is populated, SyncFields below
	// re-derives the legacy flat fields from Stages so consumers stay
	// coherent.
	stages := wireStagesToSim(wc.Stages)
	if len(stages) == 0 {
		stages = migrateV5CraftToStages(wc, monoprop, monoCap, rcsThrust, rcsIsp)
	}
	c := &spacecraft.Spacecraft{
		ID:               wc.ID, // v7+ stable identity (ADR 0012); ensureCraftIDs stamps any zero.
		Name:             wc.Name,
		DryMass:          wc.DryMass,
		Fuel:             wc.Fuel,
		Isp:              wc.Isp,
		Thrust:           wc.Thrust,
		Throttle:         1.0, // v0.7.3+: transient.
		Monoprop:         monoprop,
		MonopropCapacity: monoCap,
		RCSThrust:        rcsThrust,
		RCSIsp:           rcsIsp,
		Stages:           stages,
		Primary:          primary,
		SystemIdx:        sysIdx, // v0.16 / schema v8 (ADR 0015): per-Vessel System binding.
		State: physics.StateVector{
			R: vec3To(wc.R),
			V: vec3To(wc.V),
			M: wc.M,
		},
		AttitudeMode:       spacecraft.BurnMode(wc.AttitudeMode),
		EngineMode:         spacecraft.EngineMode(wc.EngineMode),
		LoadoutID:          wc.LoadoutID,
		Role:               wc.Role,
		Glyph:              wc.Glyph,
		Color:              wc.Color,
		PitchTrim:          wc.PitchTrim,
		CurrentAttitudeDir: vec3To(wc.CurrentAttitudeDir),
		Landed:             wc.Landed,
		LaunchLatDeg:       wc.LaunchLatDeg,
		LaunchLonDeg:       wc.LaunchLonDeg,
		Crashed:            wc.Crashed,
		CanSoftLand:        wc.CanSoftLand,
		OnPad:              wc.OnPad,
		LandedLatDeg:       wc.LandedLatDeg,
		LandedLonDeg:       wc.LandedLonDeg,
		DecouplePlan:       wc.DecouplePlan,
		ChuteState:         spacecraft.ChuteState(wc.ChuteState),
	}
	c.SyncFields()
	// v0.8.2+: pre-v0.8.2 saves carry no Glyph/Color; backfill from the
	// loadout catalog so older saves get the visual differentiation without
	// manual edits. LoadoutID empty resolves to the S-IVB-1 default.
	if c.Glyph == "" || c.Color == "" {
		l := spacecraft.LookupLoadout(c.LoadoutID)
		if c.LoadoutID == "" {
			c.LoadoutID = l.ID
		}
		if c.Role == "" {
			c.Role = l.Role
		}
		if c.Glyph == "" {
			c.Glyph = l.Glyph
		}
		if c.Color == "" {
			c.Color = l.Color
		}
	}
	// v0.23 / ADR 0027: backfill a default command source on pre-comms craft
	// (no per-stage CommandSource) so old saves stay controllable; the Role
	// resolved above decides crewed vs probe. No-op for post-comms saves
	// whose stages already carry the attribute.
	spacecraft.EnsureCommandSource(c)
	c.SyncFields()
	for _, dc := range wc.DockedComponents {
		c.DockedComponents = append(c.DockedComponents, spacecraft.DockedComponent{
			Name:             dc.Name,
			LoadoutID:        dc.LoadoutID,
			Role:             dc.Role,
			Glyph:            dc.Glyph,
			Color:            dc.Color,
			DryMass:          dc.DryMass,
			FuelCapacity:     dc.FuelCapacity,
			MonopropCapacity: dc.MonopropCapacity,
			Isp:              dc.Isp,
			Thrust:           dc.Thrust,
			RCSThrust:        dc.RCSThrust,
			RCSIsp:           dc.RCSIsp,
			CanSoftLand:      dc.CanSoftLand,
			HasParachute:     dc.HasParachute,
			Stages:           wireStagesToSim(dc.Stages),
			Owner:            dc.Owner,
			CraftID:          dc.CraftID,
		})
	}
	// v0.8.1+: per-craft Nodes / ActiveBurn loaded directly from each Craft
	// entry.
	for _, n := range wc.Nodes {
		var trig time.Time
		if n.TriggerTimeNano != 0 {
			trig = time.Unix(0, n.TriggerTimeNano).UTC()
		}
		c.Nodes = append(c.Nodes, sim.ManeuverNode{
			ID:             n.ID,
			TriggerTime:    trig,
			Mode:           spacecraft.BurnMode(n.Mode),
			DV:             n.DV,
			Duration:       time.Duration(n.DurationNano),
			PrimaryID:      n.PrimaryID,
			Event:          sim.TriggerEvent(n.Event),
			Throttle:       n.Throttle,
			TargetCraftID:  n.TargetCraftID,
			PlaneChangeRad: n.PlaneChangeRad,
			BurnDirUnit:    vec3To(n.BurnDirUnit),
			AdvisoryKey:    n.AdvisoryKey,
		})
	}
	if wc.ActiveBurn != nil {
		c.ActiveBurn = &sim.ActiveBurn{
			Mode:           spacecraft.BurnMode(wc.ActiveBurn.Mode),
			DVRemaining:    wc.ActiveBurn.DVRemaining,
			EndTime:        time.Unix(0, wc.ActiveBurn.EndTimeNano).UTC(),
			PrimaryID:      wc.ActiveBurn.PrimaryID,
			Throttle:       wc.ActiveBurn.Throttle,
			TargetCraftID:  wc.ActiveBurn.TargetCraftID,
			PlaneChangeRad: wc.ActiveBurn.PlaneChangeRad,
			BurnDirUnit:    vec3To(wc.ActiveBurn.BurnDirUnit),
		}
	}
	// v0.9.3 polish: per-craft Target. Pre-polish saves omit the field; nil
	// pointer leaves the craft's Target at zero (TargetNone) which is the
	// fresh-craft default.
	if wc.Target != nil {
		c.Target = spacecraft.Target{
			Kind:    spacecraft.TargetKind(wc.Target.Kind),
			BodyIdx: wc.Target.BodyIdx,
			CraftID: wc.Target.CraftID,
		}
	}
	return c, nil
}
