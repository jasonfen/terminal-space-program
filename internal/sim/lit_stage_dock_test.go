package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0057 decision 3 guard 6: a tug-led composite flies the S-IVB's engine,
// and an Undock gives the tug back engineless.
func TestTugLedCompositeReadsPartnerEngine(t *testing.T) {
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	earth := w.Systems[0].FindBody("Earth")
	tug := spacecraft.NewFromLoadout(spacecraft.LoadoutRCSTugID)
	tli := spacecraft.NewFromLoadout("Apollo-TLI")
	if tug.Thrust != 0 {
		t.Fatalf("fixture: RCS tug Thrust = %g, want 0", tug.Thrust)
	}
	wantThrust, wantIsp := tli.Stages[0].Thrust, tli.Stages[0].Isp
	if wantThrust <= 0 {
		t.Fatal("fixture: Apollo-TLI bottom stage has no engine")
	}
	tug.Primary, tli.Primary = *earth, *earth
	tug.State = tli.State
	w.Crafts = []*spacecraft.Spacecraft{tug, tli}
	w.ActiveCraftIdx = 0

	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("DockCrafts left %d vessels, want 1", len(w.Crafts))
	}
	c := w.Crafts[0]
	if c.Stages[0].Thrust != 0 {
		t.Fatalf("composite bottom stage should be the tug (engineless), Thrust = %g", c.Stages[0].Thrust)
	}
	if c.Thrust != wantThrust || c.Isp != wantIsp {
		t.Errorf("composite Thrust/Isp = %g/%g, want the S-IVB's %g/%g", c.Thrust, c.Isp, wantThrust, wantIsp)
	}
	if !w.Undock(0) {
		t.Fatalf("Undock refused: %q", w.UndockRefusal(0))
	}
	var back *spacecraft.Spacecraft
	for _, x := range w.Crafts {
		if len(x.Stages) == 1 && x.Stages[0].Thrust == 0 {
			back = x
		}
	}
	if back == nil {
		t.Fatalf("no engineless tug among %d vessels after Undock", len(w.Crafts))
	}
	if back.Thrust != 0 {
		t.Errorf("undocked tug Thrust = %g, want 0", back.Thrust)
	}
}
