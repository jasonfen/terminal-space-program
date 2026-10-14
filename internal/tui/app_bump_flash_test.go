package tui

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0053 decision 8: a hull bump reaches the bottom edge as an Event
// Flash naming the partner and the contact speed, once.
func TestBumpEventFlashesAndClears(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a.world.LastBumpEvent = &sim.BumpEvent{When: a.world.Clock.SimTime, PartnerName: "S-IVB-1", SpeedMS: 0.4}
	tickAt(a, time.Now())

	const want = "bumped S-IVB-1 at 0.40 m/s; re-approach under 0.10 m/s"
	if a.statusMsg != want {
		t.Errorf("statusMsg = %q, want %q", a.statusMsg, want)
	}
	if a.world.LastBumpEvent != nil {
		t.Error("LastBumpEvent was not cleared after flashing")
	}
}
