package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// inertUnderEngine is an engineless, tankless stage (an adapter) under a
// powered stage: the lit stage is index 1, the bottom stage index 0.
func inertUnderEngine() []spacecraft.Stage {
	return []spacecraft.Stage{
		{Name: "Adapter", DryMass: 400},
		{Name: "Engine", DryMass: 600, FuelMass: 900, FuelCapacity: 1000, Thrust: 100_000, Isp: 300},
	}
}

// ADR 0057 decision 3 guard 7: pip 1 is lit, the name stays stage 0's.
func TestStagePipsPaintLitStage(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	c := spacecraft.NewFromStages(inertUnderEngine())
	if got := v.stagePips(c); got != "◇◉" {
		t.Errorf("stagePips = %q, want %q", got, "◇◉")
	}
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.Crafts[w.ActiveCraftIdx].Stages = inertUnderEngine()
	w.ActiveCraft().SyncFields()
	line := strings.Join(v.buildStagesBox(w), "\n")
	if !strings.Contains(line, "◇◉") || !strings.Contains(line, "Adapter (1/2)") {
		t.Errorf("STAGES line = %q, want pips ◇◉ and the bottom stage's name Adapter (1/2)", line)
	}
}

// A vessel whose bottom stage is powered keeps ◉ on index 0.
func TestStagePipsBottomLitUnchanged(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	c := spacecraft.NewFromStages([]spacecraft.Stage{
		{FuelMass: 10, FuelCapacity: 10, Thrust: 5},
		{FuelMass: 10, FuelCapacity: 10, Thrust: 5},
	})
	if got := v.stagePips(c); got != "◉●" {
		t.Errorf("stagePips = %q, want %q", got, "◉●")
	}
}

func TestActiveStageFuelReadsLitTank(t *testing.T) {
	c := spacecraft.NewFromStages(inertUnderEngine())
	pct, mass, ok := activeStageFuel(c)
	if !ok || pct != 90 || mass != 900 {
		t.Errorf("activeStageFuel = %g%% %g kg ok=%v, want 90%% 900 kg ok", pct, mass, ok)
	}
}

// The spawn form's engine figures (grid row, summary line) and the
// lift-off gate read the lit stage.
func TestSpawnFormEngineReadsLitStage(t *testing.T) {
	stages := inertUnderEngine()
	if got := stagesSummary(stages); !strings.Contains(got, "100kN @ Isp 300s") || strings.Contains(got, "RCS-only") {
		t.Errorf("stagesSummary = %q, want the lit engine 100kN @ Isp 300s", got)
	}
	rows := layoutCatalog([]catalogRow{{name: "Stack", stages: stages}}, 0)
	if len(rows) != 1 || !strings.Contains(rows[0].head, "100kN @ Isp 300s") || strings.Contains(rows[0].head, "RCS-only") {
		t.Errorf("grid row = %+v, want the lit engine", rows)
	}
	s := NewSpawnCraft(Theme{})
	earth := earthLike()
	earth.ID = "earth"
	s.Reset([]bodies.CelestialBody{earth}, "earth", nil, "", "", nil)
	selectCustom(s)
	s.customStages = stages
	if !s.launchpadAllowed() {
		t.Error("launchpadAllowed false: a 100 kN engine under 1.9 t lifts off, the lit stage's thrust must be used")
	}
}
