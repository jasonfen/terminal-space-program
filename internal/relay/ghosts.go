package relay

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// GhostsFor evaluates every stored report against the viewer's world
// (v0.27 S5, ADR 0034): each remote craft's last-reported primary-
// relative state is propagated analytically (KeplerStep, forward OR
// backward) across the subspace gap to the viewer's sim-time, then
// rebased onto the primary's position at that time. Gating per ADR
// 0015: only craft in the viewer's active system appear. A landed craft is
// placed by body rotation at the viewer's clock (ADR 0066 decision 4), not
// coasted: a report without its lat/lon (an older build) is skipped.
// handles joins owner fingerprints to display names.
//
// Honest staleness: KeplerStep neither detects SOI exits nor knows
// about burns after the report — the ghost is "where they'd be if
// they kept coasting", exactly the ADR contract.
func GhostsFor(w *sim.World, reports []CraftReport, handles map[string]string) []sim.Ghost {
	sysName := w.System().Name
	viewerT := w.Clock.SimTime
	var out []sim.Ghost
	for _, rep := range reports {
		dt := viewerT.Sub(rep.SubspaceTime).Seconds()
		// The roster names a ghost; a report's own last known handle is the
		// fallback for an owner whose row is gone (ADR 0054 decision 5).
		handle, ok := handles[rep.Owner]
		if !ok || handle == "" {
			handle = rep.Handle
		}
		for _, cs := range rep.Crafts {
			if cs.System != sysName || (cs.Landed && cs.LatDeg == 0 && cs.LonDeg == 0) {
				continue
			}
			primary, ok := bodyByID(w.System(), cs.Primary)
			if !ok {
				continue
			}
			if cs.Landed {
				out = append(out, landedGhost(w, primary, rep.Owner, handle, cs))
				continue
			}
			st, ok := physics.KeplerStep(
				physics.StateVector{R: cs.R, V: cs.V, M: 1},
				primary.GravitationalParameter(), dt)
			if !ok {
				continue // degenerate state — better no ghost than a wrong one
			}
			out = append(out, sim.Ghost{
				Owner:     rep.Owner,
				CraftID:   cs.ID,
				Handle:    handle,
				Name:      sim.PrintableOnly(cs.Name),
				Glyph:     cs.Glyph,
				PrimaryID: cs.Primary,
				Pos:       w.BodyPosition(primary).Add(st.R),
				// v0.28 S2: retain the primary-relative state (st.R paired
				// with st.V) so the orbit screen can rebuild the ghost's
				// Keplerian ellipse via the own-craft ElementsFromState
				// pipeline. Pos stays the world-frame marker position.
				RelPos: st.R,
				Vel:    st.V,

				Attitude: cs.Attitude,
				NoseM:    cs.NoseM,
				WidthM:   cs.WidthM,
				Ports:    ghostPortsOf(cs.Ports),
			})
		}
	}
	return out
}

// landedGhost places a parked craft exactly as integrateLanded pins a local
// one: R = radius x the body-fixed direction at the viewer's clock, V = omega
// x R. Subspace skew costs nothing here (a surface point is a rotation
// question, a reported R would be hundreds of km off after a day).
func landedGhost(w *sim.World, primary bodies.CelestialBody, owner, handle string, cs CraftState) sim.Ghost {
	d := render.BodyFixedToWorld(primary, cs.LatDeg, cs.LonDeg, w.Clock.SimTime)
	r := primary.RadiusMeters()
	rel := orbital.Vec3{X: r * d.X, Y: r * d.Y, Z: r * d.Z}
	om := render.BodySpinOmegaWorld(primary)
	vel := orbital.Vec3{X: om.X, Y: om.Y, Z: om.Z}.Cross(rel)
	return sim.Ghost{
		Owner: owner, CraftID: cs.ID, Handle: handle,
		Name: sim.PrintableOnly(cs.Name), Glyph: cs.Glyph, PrimaryID: cs.Primary,
		Pos: w.BodyPosition(primary).Add(rel), RelPos: rel, Vel: vel,
		Landed: true, LatDeg: cs.LatDeg, LonDeg: cs.LonDeg,
		AntennaKind: cs.AntennaKind, AntennaRangeM: cs.AntennaRangeM, Controllable: cs.Controllable,
	}
}

func bodyByID(sys bodies.System, id string) (bodies.CelestialBody, bool) {
	for _, b := range sys.Bodies {
		if b.ID == id {
			return b, true
		}
	}
	return bodies.CelestialBody{}, false
}

func ghostPortsOf(ps []PortState) []sim.GhostPort {
	if len(ps) == 0 {
		return nil
	}
	out := make([]sim.GhostPort, len(ps))
	for i, p := range ps {
		out[i] = sim.GhostPort{OffsetM: p.OffsetM, Name: sim.PrintableOnly(p.Name)}
	}
	return out
}
