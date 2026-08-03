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
//
// live is the serve layer's session-liveness input: owner fingerprint →
// has a live session right now (attended or reprieved-away — an Away
// session is still simulating and still counts; that is the point of the
// Reprieve). It gates the RENDEZVOUS ARM fields only, not the peer (#252
// review, finding 1): the store never scrubs reports, so a partner who
// disconnects for good leaves a frozen report with RendezvousTarget
// still set — an immortal arm that would hold the survivor's standing
// intent (and its 0×-hold / dead-orbit coast) forever. A rendezvous
// intent requires a live SESSION, not a live report; suppressing the arm
// here makes the sim's normal retract path fire, with its normal cancel
// chip. A live session's report gaps are unaffected — the arm rides
// through however stale the report is, so a silent reprieved partner is
// held for, never cancelled. A nil map means no owner is live (the safe
// default for callers with no liveness source).
//
// away is the serve layer's per-owner Away verdict (#253): reports carry
// what a peer's WORLD is doing, not whether anyone is at its controls, so
// the caller supplies Server.isAway's answer and it rides the peer as
// standing state. Orthogonal to live — an away session is still live
// (that is the Reprieve), so its arm survives while Away marks it
// unattended. nil means nobody is away (solo / tests).
func CoWarpPeersFrom(w *sim.World, reports []CraftReport, handles map[string]string, viewerFP string, live, away map[string]bool) []sim.CoWarpPeer {
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
		p := sim.CoWarpPeer{
			Owner:        rep.Owner,
			Handle:       handles[rep.Owner],
			SubspaceTime: rep.SubspaceTime,
			EffWarp:      rep.EffWarp,
			Crafts:       crafts,
			Paused:       rep.Paused,
			Away:         away[rep.Owner],
		}
		// Rendezvous Warp (v0.29 S1): this peer is armed toward the viewer
		// iff its report's intent names us AND its session is live (#252
		// review — see the liveness rationale above). The committed τ rides
		// along so the responder can adopt the initiator's value; a dead
		// session's τ/CA are suppressed with the arm so nothing downstream
		// adopts a dead waypoint.
		if live[rep.Owner] && rep.RendezvousTarget != "" && rep.RendezvousTarget == viewerFP {
			p.ArmedTowardViewer = true
			p.RendezvousTau = rep.RendezvousTau
			p.RendezvousCA = rep.RendezvousCA
			// Seat + published rate (ADR 0037 §2). Gated with the arm on
			// purpose: a dead session's frozen report must not keep clamping
			// the survivor's clock any more than it keeps their arm alive.
			p.RendezvousInitiator = rep.RendezvousInitiator
			p.RendezvousRate = rep.RendezvousRate
			p.RendezvousBurning = rep.RendezvousBurning
			// Name the vessel behind the invite (#295) — the arm always acts
			// through the reporter's active craft, so the report's marker
			// (#288) is exactly the right source.
			if active, aok := rep.ActiveCraft(); aok {
				p.ActiveCraftName = active.Name
			}
		}
		out = append(out, p)
	}
	return out
}
