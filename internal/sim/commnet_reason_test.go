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

func TestDisconnectReasonIgnoresDisconnectedRelay(t *testing.T) {
	// Review finding (v0.32 batch): a probe travelling WITH a relay,
	// both beyond every station's reach — the standard send-a-probe-
	// plus-relay-outward flow. The dead relay next door is not "the
	// network in range": another relay changes nothing, so the advice
	// must be antenna for BOTH craft, and two craft in the identical
	// predicament must never read contradictory advice.
	nodes := []commNode{
		station(0, 1e9, 100000), // far beyond everyone
		probeNode(1, 0, 3000, false),
		probeNode(2, 500, 100000, true), // relay antenna, also disconnected
	}
	res := connectivityFull(nodes, nil)
	reasons := classifyDisconnects(nodes, res)
	if got := reasons[1]; got != CommDisconnectOutOfRange {
		t.Errorf("probe beside a DISCONNECTED relay: reason = %v, want CommDisconnectOutOfRange (a dead relay is not the network)", got)
	}
	if got := reasons[2]; got != CommDisconnectOutOfRange {
		t.Errorf("the relay itself: reason = %v, want CommDisconnectOutOfRange", got)
	}
}

func TestDisconnectReasonCountsConnectedRelay(t *testing.T) {
	// The complement: a CONNECTED relay in range (occluded from the
	// probe) IS the network — the geometry is the problem, advise a
	// relay. Station←→relay link is clear; probe is in range of the
	// relay only, with a body on that segment.
	occ := []physics.OccluderBody{{Center: orbital.Vec3{X: 95000}, Radius: 50}}
	nodes := []commNode{
		station(0, 0, 100000),
		probeNode(1, 100000, 3000, false), // out of station range (√(3e3·1e5)≈17.3 km)
		relayNode(90000, 100000),          // linked to the station, networked
	}
	res := connectivityFull(nodes, occ)
	if res.connected[1] {
		t.Fatalf("probe should be disconnected (occluded from the relay)")
	}
	reasons := classifyDisconnects(nodes, res)
	if got := reasons[1]; got != CommDisconnectBlocked {
		t.Errorf("probe occluded from a CONNECTED relay in range: reason = %v, want CommDisconnectBlocked", got)
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
