package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
)

// #221 (ADR 0027 amendment, v0.32): CommGraph records WHY a probe is
// disconnected — two reasons, the minimum that is never actively wrong
// advice. Telling a deep-space probe to build a relay is a bum steer;
// telling a 5,000 km satellite to fit a bigger antenna is equally wrong.

func TestDisconnectReasonBlocked(t *testing.T) {
	// Station in range but a body sits on the segment: the geometry is
	// the problem, so the advice is a relay.
	occ := []physics.OccluderBody{{Center: orbital.Vec3{}, Radius: 100}}
	nodes := []commNode{
		station(0, -1000, 100000),
		probeNode(1, 1000, 3000, false),
	}
	res := connectivityFull(nodes, occ)
	reasons := classifyDisconnects(nodes, res)
	if got := reasons[1]; got != CommDisconnectBlocked {
		t.Errorf("occluded-but-in-range probe: reason = %v, want CommDisconnectBlocked", got)
	}
}

func TestDisconnectReasonOutOfRange(t *testing.T) {
	rng := commLinkRangeM(100000, 3000)
	nodes := []commNode{
		station(0, 0, 100000),
		probeNode(1, rng*1.5, 3000, false),
	}
	res := connectivityFull(nodes, nil)
	reasons := classifyDisconnects(nodes, res)
	if got := reasons[1]; got != CommDisconnectOutOfRange {
		t.Errorf("nothing-in-range probe: reason = %v, want CommDisconnectOutOfRange", got)
	}
}

func TestDisconnectReasonIgnoresDeadEndNeighbours(t *testing.T) {
	// The probe's only in-range neighbour is a direct-only craft — a
	// dead end that cannot forward. Being "in range" of it says nothing
	// about reaching the network, so the advice must stay antenna, not
	// relay.
	rng := commLinkRangeM(100000, 3000)
	nodes := []commNode{
		station(0, rng*3, 100000), // far out of the probe's reach
		probeNode(1, 0, 3000, false),
		probeNode(2, 500, 3000, false), // direct-only craft beside the probe
	}
	res := connectivityFull(nodes, nil)
	reasons := classifyDisconnects(nodes, res)
	if got := reasons[1]; got != CommDisconnectOutOfRange {
		t.Errorf("probe whose only in-range neighbour is a dead end: reason = %v, want CommDisconnectOutOfRange", got)
	}
}

func TestDisconnectReasonConnectedProbeHasNone(t *testing.T) {
	nodes := []commNode{
		station(0, 0, 100000),
		probeNode(1, 1000, 3000, false),
	}
	res := connectivityFull(nodes, nil)
	reasons := classifyDisconnects(nodes, res)
	if _, present := reasons[1]; present {
		t.Errorf("a connected probe must carry no disconnect reason; got %v", reasons[1])
	}
}

func TestCommGraphReasonAccessorNilSafe(t *testing.T) {
	var g *CommGraph
	if got := g.Reason(42); got != CommDisconnectNone {
		t.Errorf("nil graph: Reason = %v, want CommDisconnectNone", got)
	}
	g = &CommGraph{Connected: map[uint64]bool{7: true}}
	if got := g.Reason(7); got != CommDisconnectNone {
		t.Errorf("connected probe: Reason = %v, want CommDisconnectNone", got)
	}
}

func TestRecomputeCommGraphRecordsReasons(t *testing.T) {
	// Production integration: the world-level recompute must carry the
	// classification through — a fresh default world has ground stations
	// on Earth, so a probe parked mid-band (occlusion-bound) or in deep
	// space classifies through RecomputeCommGraph without any test-side
	// shortcut formula (the amendment's per-primary discipline).
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	w.RecomputeCommGraph()
	g := w.CommGraph
	if g == nil {
		t.Fatalf("RecomputeCommGraph left no graph")
	}
	for id, connected := range g.Connected {
		if connected {
			if got := g.Reason(id); got != CommDisconnectNone {
				t.Errorf("connected craft %d carries reason %v", id, got)
			}
		}
	}
}
