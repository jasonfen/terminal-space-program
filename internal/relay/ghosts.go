package relay

import (
	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// GhostsFor evaluates every stored report against the viewer's world
// (v0.27 S5, ADR 0034): each remote craft's last-reported primary-
// relative state is propagated analytically (KeplerStep, forward OR
// backward) across the subspace gap to the viewer's sim-time, then
// rebased onto the primary's position at that time. Gating per ADR
// 0015: only craft in the viewer's active system appear. Landed craft
// carry no orbit and are skipped (roster surfaces still count them).
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
		for _, cs := range rep.Crafts {
			if cs.Landed || cs.System != sysName {
				continue
			}
			primary, ok := bodyByID(w.System(), cs.Primary)
			if !ok {
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
				Handle:    handles[rep.Owner],
				Name:      cs.Name,
				Glyph:     cs.Glyph,
				PrimaryID: cs.Primary,
				Pos:       w.BodyPosition(primary).Add(st.R),
				// v0.28 S2: retain the primary-relative state (st.R paired
				// with st.V) so the orbit screen can rebuild the ghost's
				// Keplerian ellipse via the own-craft ElementsFromState
				// pipeline. Pos stays the world-frame marker position.
				RelPos: st.R,
				Vel:    st.V,
			})
		}
	}
	return out
}

func bodyByID(sys bodies.System, id string) (bodies.CelestialBody, bool) {
	for _, b := range sys.Bodies {
		if b.ID == id {
			return b, true
		}
	}
	return bodies.CelestialBody{}, false
}
