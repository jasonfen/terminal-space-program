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
// carries a Parcel carries too. Ghost refs on nodes, the active burn, and
// the craft's Target all round-trip (#294 review finding 5 retired the
// old drop-on-save behavior below the Nodes/ActiveBurn loops — see the
// comment there and on Target below): the owner fingerprint IS meaningful
// within the session it was set in, and a reconnect that re-latches
// Craft.Target back onto a ghost should re-latch a planted node's or a
// running burn's ref onto the same ghost too, not leave them silently
// zeroed while the standing lock recovers around them.
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
		// #294 review finding 5: a node planted against a ghost (remote
		// player's craft) used to drop its ghost ref unconditionally on
		// save (DropGhostRef, v0.28 S4) — reasoned at the time that the
		// owner fingerprint was session-local and the TargetCraftID a
		// REMOTE id that could collide with a local id on load. That
		// reasoning is exactly what #294 already overturned for
		// Craft.Target (see below): the fingerprint round-trips fine
		// within the session it was bound in, and TargetCraftID was
		// never at risk of a local collision — ghost refs are only ever
		// resolved by (owner, craftID) pair (World.ghostByRef), never by
		// craftID alone, so a same-numbered LOCAL craft ID is never
		// confused with one. Dropping the ref here left a stale-but-
		// silent gap: Craft.Target re-latches onto the peer after a
		// reconnect, but a node planted at that same target fires with
		// TargetGhostOwner=="" and a now-locally-meaningless
		// TargetCraftID — nodeTargetRelState's owner!="" branch never
		// even runs, so it falls to the local craftByID branch and
		// (almost always) fails to find a matching local craft, ok=false.
		// See executeDueNodesFor below for the refuse-to-fire guard that
		// makes that failure safe rather than a burn against a zero state.
		wc.Nodes = append(wc.Nodes, Node{
			ID:               n.ID,
			TriggerTimeNano:  trigNano,
			Mode:             int(n.Mode),
			DV:               n.DV,
			DurationNano:     int64(n.Duration),
			PrimaryID:        n.PrimaryID,
			Event:            int(n.Event),
			Throttle:         n.Throttle,
			TargetCraftID:    n.TargetCraftID,
			PlaneChangeRad:   n.PlaneChangeRad,
			BurnDirUnit:      vec3From(n.BurnDirUnit),
			AdvisoryKey:      n.AdvisoryKey,
			TargetGhostOwner: n.TargetGhostOwner,
		})
	}
	if c.ActiveBurn != nil {
		// #294 review finding 5: preserve the running burn's ghost ref too,
		// for the same reason as nodes above.
		ab := *c.ActiveBurn
		wc.ActiveBurn = &ActiveBurn{
			Mode:             int(ab.Mode),
			DVRemaining:      ab.DVRemaining,
			EndTimeNano:      ab.EndTime.UnixNano(),
			PrimaryID:        ab.PrimaryID,
			Throttle:         ab.Throttle,
			TargetCraftID:    ab.TargetCraftID,
			PlaneChangeRad:   ab.PlaneChangeRad,
			BurnDirUnit:      vec3From(ab.BurnDirUnit),
			TargetGhostOwner: ab.TargetGhostOwner,
		}
	}
	// v0.9.3 polish: per-craft Target. Skip serialising when the craft has
	// no target so untargeted craft still write out the same minimal JSON
	// they did pre-polish.
	//
	// Ghost targets (v0.27) used to be normalised to no-target here on the
	// reasoning that they're session-local and the owner fingerprint was
	// never persisted, so a saved ref could never resolve again. That
	// silently dropped a guest's cross-player rendezvous lock across every
	// reconnect that round-trips through the per-player session payload —
	// most visibly the [u] restart-to-adopt flow (#294) — while a body
	// target survived the same reconnect untouched. The fingerprint IS
	// meaningful within the session it was set in: persist it, and let the
	// ordinary "ghost not resolvable yet" tolerance every ghost-target
	// consumer already has (ResolveTargetGhost / TargetState / HUD) do the
	// re-latching once the owner's craft reports resume. A standalone save
	// loaded outside that session (or in single-player, which never sets a
	// ghost target) just carries a ref that never resolves — inert, same
	// as any other stale target.
	if c.Target.Kind != spacecraft.TargetNone {
		wc.Target = &Target{
			Kind:       int(c.Target.Kind),
			BodyIdx:    c.Target.BodyIdx,
			CraftID:    c.Target.CraftID,
			GhostOwner: c.Target.GhostOwner,
		}
	}
	return wc
}

// CraftToWireForTransfer projects one live craft onto its serialisable
// form exactly like CraftToWire, then strips every target-relative ref
// (Target, planted nodes, the active burn) before returning it — both
// ghost refs AND local craft refs. #294 review finding 3: a ghost ref is
// a (owner fingerprint, remote craft ID) pair meaningful only within the
// player's OWN world — relay.GhostsFor never emits a player's own craft
// as a ghost to itself, so the pair can never resolve anywhere else.
// CraftToWire round-tripping it is correct for a session save/
// reconnect, which stays within that same world. It is wrong for the
// dock ledger's parcel/return/transfer payloads (ADR 0040, internal/
// relay/dock_persist.go): those are delivered into a DIFFERENT player's
// world, where the ref can never resolve and — worse — can alias the
// RECIPIENT's own fingerprint (a guest craft carrying a node targeted at
// the HOST's ghost, transferred into the host's own world, now holds
// the host's own fingerprint as a "remote" ref).
//
// #294 review round 3 (finding G): a LOCAL ref (TargetGhostOwner=="")
// is just as unsafe here, for a different reason — w.AdoptCraft remaps
// only the transferred craft's OWN id, not the TargetCraftID a node or
// the active burn on it points at. A node targeting the sender's SISTER
// craft (a different local craft in the sender's own world) transfers
// with that sender-local id intact, and in the recipient's world that
// same numeric id belongs to whatever unrelated vessel happens to hold
// it — the node then resolves against, and fires at, a craft the player
// never chose. Both kinds of ref are equally meaningless once the craft
// leaves the world it was planned in, so both are stripped.
//
// The stripping itself is spacecraft.StripCrossOwnerTargetRefs — shared with
// the live (no-restart) dock-ledger delivery path in internal/relay, so the
// persisted and live paths can't drift apart on which refs count as
// target-relative.
func CraftToWireForTransfer(c *spacecraft.Spacecraft) Craft {
	return CraftToWire(spacecraft.StripCrossOwnerTargetRefs(c))
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
			ID:               n.ID,
			TriggerTime:      trig,
			Mode:             spacecraft.BurnMode(n.Mode),
			DV:               n.DV,
			Duration:         time.Duration(n.DurationNano),
			PrimaryID:        n.PrimaryID,
			Event:            sim.TriggerEvent(n.Event),
			Throttle:         n.Throttle,
			TargetCraftID:    n.TargetCraftID,
			PlaneChangeRad:   n.PlaneChangeRad,
			BurnDirUnit:      vec3To(n.BurnDirUnit),
			AdvisoryKey:      n.AdvisoryKey,
			TargetGhostOwner: n.TargetGhostOwner, // #294 review finding 5
		})
	}
	if wc.ActiveBurn != nil {
		c.ActiveBurn = &sim.ActiveBurn{
			Mode:             spacecraft.BurnMode(wc.ActiveBurn.Mode),
			DVRemaining:      wc.ActiveBurn.DVRemaining,
			EndTime:          time.Unix(0, wc.ActiveBurn.EndTimeNano).UTC(),
			PrimaryID:        wc.ActiveBurn.PrimaryID,
			Throttle:         wc.ActiveBurn.Throttle,
			TargetCraftID:    wc.ActiveBurn.TargetCraftID,
			PlaneChangeRad:   wc.ActiveBurn.PlaneChangeRad,
			BurnDirUnit:      vec3To(wc.ActiveBurn.BurnDirUnit),
			TargetGhostOwner: wc.ActiveBurn.TargetGhostOwner, // #294 review finding 5
		}
		// #294 review round 3 (finding D): defensive load-time teardown for
		// an ActiveBurn that is target-relative in Mode but carries no
		// target at all (TargetCraftID==0, ownerless). This is exactly the
		// shape a pre-round-3 give-up used to leave behind (it stripped the
		// ref but kept the burn "alive") — and, independently, exactly the
		// shape a v9 save (schema < 10, before CraftToWire started
		// preserving ghost refs) carries for a craft that was saved
		// mid-ghost-burn: the old wire form dropped the ref unconditionally
		// while keeping the burn. migrateV9PayloadToV10 is an identity
		// transform, so that shape reaches here unchanged on load. Either
		// way, nodeTargetRelState refuses unconditionally for craftID==0,
		// so a target-relative burn with no ref can never resolve, never
		// thrust, and never tear itself down (burnExhausted's fuel-present-
		// but-EndTime-never-reached hold is permanent) — a zombie burn that
		// wedges canKeplerStep's per-craft gate and clamps warp ≤10× for
		// the rest of the session. Tear it down here instead of letting it
		// load in that state.
		if spacecraft.IsTargetRelativeMode(c.ActiveBurn.Mode) && c.ActiveBurn.TargetCraftID == 0 {
			c.ActiveBurn = nil
		}
	}
	// v0.9.3 polish: per-craft Target. Pre-polish saves omit the field; nil
	// pointer leaves the craft's Target at zero (TargetNone) which is the
	// fresh-craft default. GhostOwner (#294) round-trips a TargetGhost's
	// remote-player fingerprint; absent on every save predating the fix,
	// which decodes to "" — harmless since those saves never wrote
	// Kind==TargetGhost in the first place (it was normalised away).
	if wc.Target != nil {
		c.Target = spacecraft.Target{
			Kind:       spacecraft.TargetKind(wc.Target.Kind),
			BodyIdx:    wc.Target.BodyIdx,
			CraftID:    wc.Target.CraftID,
			GhostOwner: wc.Target.GhostOwner,
		}
	}
	return c, nil
}
