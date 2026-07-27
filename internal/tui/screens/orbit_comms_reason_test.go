package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #221: the NO SIGNAL chip names the cause instead of a bare warning —
// the model was sound, the silence was the defect. Wording discipline
// from the RendezvousWait work: name the cause, give the fix, never
// steer the player at the wrong remedy.

func TestCommsChipNoSignalNamesTheCause(t *testing.T) {
	v := NewOrbitView(chipTestTheme())

	blocked := strings.Join(v.commsChipLines(0, false, sim.CommDisconnectBlocked), "\n")
	if !strings.Contains(blocked, "NO SIGNAL") || !strings.Contains(blocked, "no station in view — relay needed") {
		t.Errorf("blocked probe chip must advise a relay:\n%s", blocked)
	}
	if strings.Contains(blocked, "antenna") {
		t.Errorf("blocked probe chip must not steer at the antenna (bum-steer discipline):\n%s", blocked)
	}

	ranged := strings.Join(v.commsChipLines(0, false, sim.CommDisconnectOutOfRange), "\n")
	if !strings.Contains(ranged, "NO SIGNAL") || !strings.Contains(ranged, "out of range — stronger antenna needed") {
		t.Errorf("out-of-range probe chip must advise the antenna:\n%s", ranged)
	}
	if strings.Contains(ranged, "relay") {
		t.Errorf("out-of-range probe chip must not advise a relay:\n%s", ranged)
	}

	// Classification can legitimately be absent (a stale pre-#221 graph
	// mid-tick): degrade to the bare form, never to wrong advice.
	bare := strings.Join(v.commsChipLines(0, false, sim.CommDisconnectNone), "\n")
	if !strings.Contains(bare, "NO SIGNAL") {
		t.Errorf("unclassified disconnect still reads NO SIGNAL:\n%s", bare)
	}
	if strings.Contains(bare, "relay") || strings.Contains(bare, "antenna") {
		t.Errorf("unclassified disconnect must not guess a remedy:\n%s", bare)
	}
}

func TestCommsChipConnectedFormsUnchanged(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	direct := strings.Join(v.commsChipLines(1, true, sim.CommDisconnectNone), "\n")
	if !strings.Contains(direct, "DIRECT") {
		t.Errorf("single hop reads DIRECT:\n%s", direct)
	}
	hops := strings.Join(v.commsChipLines(3, true, sim.CommDisconnectNone), "\n")
	if !strings.Contains(hops, "CONNECTED via 3 hops") {
		t.Errorf("multi-hop form regressed:\n%s", hops)
	}
}

func TestCommsChipReasonLinesWidthConsistent(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	assertChipCellWidthConsistent(t, "comms blocked", v.commsChipLines(0, false, sim.CommDisconnectBlocked))
	assertChipCellWidthConsistent(t, "comms out of range", v.commsChipLines(0, false, sim.CommDisconnectOutOfRange))
}
