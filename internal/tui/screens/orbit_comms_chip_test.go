package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The comms chip (ADR 0027 / C2-7) surfaces the active probe's CommNet link
// state: DIRECT (one hop to a ground station), CONNECTED via N hops (through
// relays), or NO SIGNAL. commsChipLines is the pure content selector,
// exercised here without a live World.

func TestCommsChipLinesDirect(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	lines := v.commsChipLines(1, true)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "COMMS") {
		t.Errorf("chip missing COMMS header:\n%s", joined)
	}
	if !strings.Contains(joined, "DIRECT") {
		t.Errorf("a one-hop link should read DIRECT:\n%s", joined)
	}
	if strings.Contains(joined, "via") {
		t.Errorf("a direct link should not mention hops:\n%s", joined)
	}
}

func TestCommsChipLinesRelayed(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	joined := strings.Join(v.commsChipLines(3, true), "\n")
	if !strings.Contains(joined, "CONNECTED via 3 hops") {
		t.Errorf("a three-hop link should read CONNECTED via 3 hops:\n%s", joined)
	}
}

func TestCommsChipLinesNoSignal(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	joined := strings.Join(v.commsChipLines(0, false), "\n")
	if !strings.Contains(joined, "NO SIGNAL") {
		t.Errorf("a disconnected probe should read NO SIGNAL:\n%s", joined)
	}
}

// TestBuildCommsChipHiddenForCrewed: the default seed craft is crew-tended
// (C2-5), so it is never command-gated and the comms chip stays hidden.
func TestBuildCommsChipHiddenForCrewed(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if c := w.ActiveCraft(); c == nil || !c.Crewed {
		t.Fatalf("expected a crewed seed craft, got crewed=%v", c != nil && c.Crewed)
	}
	if chip := v.buildCommsChip(w); chip != nil {
		t.Errorf("comms chip should be hidden for a crewed craft, got:\n%s", strings.Join(chip, "\n"))
	}
}

// TestBuildCommsChipProbeNoSignal: an unmanned probe with no connection
// surfaces NO SIGNAL through the world-reading builder.
func TestBuildCommsChipProbeNoSignal(t *testing.T) {
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
	chip := v.buildCommsChip(w)
	if chip == nil {
		t.Fatal("comms chip should show for an unmanned probe")
	}
	if !strings.Contains(strings.Join(chip, "\n"), "NO SIGNAL") {
		t.Errorf("disconnected probe chip should read NO SIGNAL:\n%s", strings.Join(chip, "\n"))
	}
}
