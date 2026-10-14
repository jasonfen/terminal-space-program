package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0053 decision 7: the six PORT modes are frame-locked, so hold: tags
// their own frame (PORT) whatever frame the navball is showing right now.
// Sabotage that restores the old rule ("tag the live NavMode") reads
// "Port+ (ORBIT)" / "(SURF)" here.
func TestHoldNamesPortModesInTheirOwnFrame(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	want := map[spacecraft.BurnMode]string{
		spacecraft.BurnPortIn:    "Port+ (PORT)",
		spacecraft.BurnPortOut:   "Port− (PORT)",
		spacecraft.BurnPortUp:    "Port up (PORT)",
		spacecraft.BurnPortDown:  "Port down (PORT)",
		spacecraft.BurnPortLeft:  "Port left (PORT)",
		spacecraft.BurnPortRight: "Port right (PORT)",
	}
	for _, nav := range []sim.NavMode{sim.NavOrbit, sim.NavSurface, sim.NavTarget, sim.NavPort} {
		w.NavMode = nav
		for mode, label := range want {
			if got := attitudeHoldLabel(w, mode); got != label {
				t.Errorf("NavMode %v, hold %v: got %q, want %q", nav, mode, got, label)
			}
		}
	}
	// The ordinary modes still tag their own frame, unchanged.
	w.NavMode = sim.NavPort
	if got := attitudeHoldLabel(w, spacecraft.BurnPrograde); got != "Prograde (ORBIT)" {
		t.Errorf("Prograde under PORT: %q, want Prograde (ORBIT)", got)
	}
}

func TestNavballButtonReadsPort(t *testing.T) {
	if got := navModeLabel(sim.NavPort); got != "PORT" {
		t.Errorf("navModeLabel(NavPort) = %q, want PORT", got)
	}
	// The cell is the button's bracketed form.
	if got := "[" + navModeLabel(sim.NavPort) + "]"; got != "[PORT]" {
		t.Errorf("button = %q, want [PORT]", got)
	}
}

func TestHelpManualFlightRowsNamePort(t *testing.T) {
	_, semi := helpRow(t, ";")
	for _, want := range []string{"Target", "Port"} {
		if !strings.Contains(semi, want) {
			t.Errorf("`;` row omits %q: %q", want, semi)
		}
	}
	for key, want := range map[string]string{"w / s": "up / down", "a / d": "left / right", "q / e": "in / out"} {
		_, d := helpRow(t, key)
		if !strings.Contains(d, "PORT") || !strings.Contains(d, want) {
			t.Errorf("%q row lacks its PORT meaning (%q): %q", key, want, d)
		}
	}
}
