// ADR 0051 slice 2a, re-grill Q9: COMMS' crewed-vs-gated distinction is
// the load-bearing rule, a crewed vessel's disconnect must never read
// as an alarm, since the link doesn't command-gate it (yet).

package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TestCommsBoxCrewedDisconnectHasNoAlarm: a crewed, disconnected vessel
// reads "no signal" with no ⚠ and no classified reason, evaluated as
// today's gated code would, an Apollo Stack on the Moon pad used to read
// "⚠ NO SIGNAL" in the Alert colour for a gate that doesn't apply to it
// (re-grill's own measured alternative, P-20/P-22).
func TestCommsBoxCrewedDisconnectHasNoAlarm(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	line := v.commsBoxStatusLine(&spacecraft.Spacecraft{Crewed: true, Controllable: true}, 0, false, sim.CommDisconnectBlocked)
	if strings.Contains(line, "⚠") {
		t.Errorf("crewed disconnect = %q, must not carry the alarm glyph", line)
	}
	if strings.Contains(line, "relay") || strings.Contains(line, "antenna") {
		t.Errorf("crewed disconnect = %q, must not carry the classified reason (re-grill Q9)", line)
	}
	if !strings.Contains(line, "no signal") {
		t.Errorf("crewed disconnect = %q, want the plain 'no signal' text", line)
	}
}

// TestCommsBoxUncrewedDisconnectIsAlarmed: an uncrewed, controllable
// probe (the link actually gates it) keeps the alarm and the reason.
func TestCommsBoxUncrewedDisconnectIsAlarmed(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	line := v.commsBoxStatusLine(&spacecraft.Spacecraft{Crewed: false, Controllable: true}, 0, false, sim.CommDisconnectBlocked)
	if !strings.Contains(line, "⚠") {
		t.Errorf("uncrewed disconnect = %q, want the alarm glyph", line)
	}
	if !strings.Contains(line, "relay") {
		t.Errorf("uncrewed disconnect = %q, want the classified reason folded onto this row", line)
	}
}

// TestCommsBoxCrewedConnectedReadsOrdinary: a connected crewed vessel
// reads DIRECT/CONNECTED exactly like any other vessel, no special
// wording (Forward constraint: "do not bake the crewed-never-gated rule
// into the COMMS box's wording").
func TestCommsBoxCrewedConnectedReadsOrdinary(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	line := v.commsBoxStatusLine(&spacecraft.Spacecraft{Crewed: true}, 1, true, sim.CommDisconnectReason(0))
	if !strings.Contains(line, "DIRECT") {
		t.Errorf("crewed connected (1 hop) = %q, want DIRECT", line)
	}
}
