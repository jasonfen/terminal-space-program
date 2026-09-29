package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #221: the NO SIGNAL alarm names the cause instead of a bare warning,
// now on the live COMMS box (commsBoxStatusLine). Wording discipline from
// the RendezvousWait work: name the cause, give the fix, never steer the
// player at the wrong remedy.

func TestCommsBoxNoSignalNamesTheCause(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	probe := &spacecraft.Spacecraft{Controllable: true}

	blocked := v.commsBoxStatusLine(probe, 0, false, sim.CommDisconnectBlocked)
	if !strings.Contains(blocked, "⚠ NO SIGNAL: needs a relay") {
		t.Errorf("blocked probe must advise a relay: %q", blocked)
	}
	if strings.Contains(blocked, "antenna") {
		t.Errorf("blocked probe must not steer at the antenna (bum-steer discipline): %q", blocked)
	}

	ranged := v.commsBoxStatusLine(probe, 0, false, sim.CommDisconnectOutOfRange)
	if !strings.Contains(ranged, "⚠ NO SIGNAL: needs a stronger antenna") {
		t.Errorf("out-of-range probe must advise the antenna: %q", ranged)
	}
	if strings.Contains(ranged, "relay") {
		t.Errorf("out-of-range probe must not advise a relay: %q", ranged)
	}

	// Classification can legitimately be absent (a stale pre-#221 graph
	// mid-tick): degrade to the bare form, never to wrong advice.
	bare := v.commsBoxStatusLine(probe, 0, false, sim.CommDisconnectNone)
	if !strings.Contains(bare, "NO SIGNAL") {
		t.Errorf("unclassified disconnect still reads NO SIGNAL: %q", bare)
	}
	if strings.Contains(bare, "relay") || strings.Contains(bare, "antenna") {
		t.Errorf("unclassified disconnect must not guess a remedy: %q", bare)
	}
}

func TestCommsBoxConnectedForms(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	probe := &spacecraft.Spacecraft{Controllable: true}
	if direct := v.commsBoxStatusLine(probe, 1, true, sim.CommDisconnectNone); !strings.Contains(direct, "DIRECT") || strings.Contains(direct, "via") {
		t.Errorf("single hop reads DIRECT with no hop count: %q", direct)
	}
	if hops := v.commsBoxStatusLine(probe, 3, true, sim.CommDisconnectNone); !strings.Contains(hops, "CONNECTED via 3 hops") {
		t.Errorf("multi-hop form regressed: %q", hops)
	}
}

// TestCommsBoxProbeNoSignalFromWorld: an unmanned probe with no
// connection surfaces the alarm through the world-reading builder, and
// the box keeps its header and one row.
func TestCommsBoxProbeNoSignalFromWorld(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	probe := spacecraft.NewFromLoadout("Relay-Tug")
	probe.Primary = w.Crafts[0].Primary
	probe.State = w.Crafts[0].State
	probe.SystemIdx = w.Crafts[0].SystemIdx
	w.Crafts[0] = probe
	w.EnsureCraftIDs()
	w.SetActiveCraftIdx(0)
	w.CommGraph = &sim.CommGraph{Connected: map[uint64]bool{}} // force disconnected
	box := v.buildCommsBox(w)
	joined := strings.Join(box, "\n")
	if len(box) != 2 || !strings.Contains(box[0], "COMMS") || !strings.Contains(joined, "NO SIGNAL") {
		t.Errorf("disconnected probe box should be COMMS + a NO SIGNAL row:\n%s", joined)
	}
}

func TestCommsBoxReasonLinesWidthConsistent(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	probe := &spacecraft.Spacecraft{Controllable: true}
	assertChipCellWidthConsistent(t, "comms blocked", []string{v.commsBoxStatusLine(probe, 0, false, sim.CommDisconnectBlocked)})
	assertChipCellWidthConsistent(t, "comms out of range", []string{v.commsBoxStatusLine(probe, 0, false, sim.CommDisconnectOutOfRange)})
}
