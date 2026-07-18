package relay

import (
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// CoWarpPeersFrom adapts the store's reports into the sim-level co-warp
// input (v0.28 S1, ADR 0034 §5) — the relay-side twin of GhostsFor. Each
// remote craft's last-reported primary-relative state is Kepler-stepped
// across the subspace gap to the viewer's sim-time (forward OR backward),
// so range + |v_rel| against the viewer's active craft are geometrically
// exact for a coasting peer within the same-subspace tolerance. Gating
// per ADR 0015: only craft in the viewer's active system contribute;
// landed craft carry no orbit and are skipped. The peer's Owner/Handle,
// SubspaceTime, and reported EffWarp travel through so ComputeCoWarp can
// apply the same-subspace gate and the min-over-Effective clamp.
//
// Honest staleness matches ghosts: KeplerStep neither sees a peer's burn
// after the report nor an SOI exit — but a burning peer reports every
// tick (elements change), so the propagation gap it feeds co-warp with is
// one tick, not one heartbeat.
func CoWarpPeersFrom(w *sim.World, reports []CraftReport, handles map[string]string, viewerFP string) []sim.CoWarpPeer {
	sysName := w.System().Name
	viewerT := w.Clock.SimTime
	var out []sim.CoWarpPeer
	for _, rep := range reports {
		dt := viewerT.Sub(rep.SubspaceTime).Seconds()
		var crafts []sim.CoWarpCraft
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
				continue // degenerate state — no peer beats a wrong one
			}
			crafts = append(crafts, sim.CoWarpCraft{Primary: cs.Primary, R: st.R, V: st.V})
		}
		if len(crafts) == 0 {
			continue
		}
		out = append(out, sim.CoWarpPeer{
			Owner:        rep.Owner,
			Handle:       handles[rep.Owner],
			SubspaceTime: rep.SubspaceTime,
			EffWarp:      rep.EffWarp,
			Crafts:       crafts,
			// Rendezvous Warp (v0.29 S1): this peer is armed toward the
			// viewer iff its report's intent names us. The committed τ rides
			// along so the responder can adopt the initiator's value.
			ArmedTowardViewer: rep.RendezvousTarget != "" && rep.RendezvousTarget == viewerFP,
			RendezvousTau:     rep.RendezvousTau,
			RendezvousCA:      rep.RendezvousCA,
		})
	}
	return out
}
