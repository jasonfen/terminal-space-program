package save_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0055 "Settled here" 1: port trims are session-only. A save mid-dock
// comes back with the hold and no trim; the envelope has no key for them.
// The surface pair still round-trips. Sabotage: write the port pair to the
// wire and this goes red.
func TestPortTrimsAreNotInTheSaveEnvelope(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := w.ActiveCraft()
	if c == nil {
		t.Fatal("no active craft")
	}
	c.PitchTrim = 0.2
	c.PortPitchTrim, c.PortYawTrim = 0.017, -0.035
	path := filepath.Join(t.TempDir(), "save.json")
	if err := save.Save(w, path); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if s := strings.ToLower(string(b)); strings.Contains(s, "port_pitch") || strings.Contains(s, "port_yaw") || strings.Contains(s, "portpitch") || strings.Contains(s, "portyaw") {
		t.Error("the save envelope carries a port trim key")
	}
	w2, err := save.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	c2 := w2.ActiveCraft()
	if c2.PortPitchTrim != 0 || c2.PortYawTrim != 0 {
		t.Errorf("loaded port pair = (%v, %v), want 0", c2.PortPitchTrim, c2.PortYawTrim)
	}
	if c2.PitchTrim != 0.2 {
		t.Errorf("surface PitchTrim = %v after load, want 0.2 (still persisted)", c2.PitchTrim)
	}
}
