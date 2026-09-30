package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

var bracketKey = regexp.MustCompile(`\[([^\]]+)\]`)

// keyToken turns a rung's bracketed key text into the key string the
// keymap matches on ("space" is " ").
func keyToken(tok string) string {
	if tok == "space" {
		return " "
	}
	return tok
}

func bindingHasKey(b key.Binding, tok string) bool {
	for _, k := range b.Keys() {
		if k == keyToken(tok) {
			return true
		}
	}
	return false
}

func tutorialRung(t *testing.T, id string) missions.Mission {
	t.Helper()
	cat, err := missions.LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	for _, m := range cat.Missions {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("mission %q not in catalog", id)
	return missions.Mission{}
}

// Every Flight School event rung that names a key in [brackets] must name a
// key bound to the action that rung waits on (#519: text and binding drift).
func TestFlightSchoolRungKeyTextMatchesActionBinding(t *testing.T) {
	km := DefaultKeymap()
	byAction := map[missions.Action][]key.Binding{
		missions.ActionCycleView:       {km.CycleView},
		missions.ActionCycleTarget:     {km.CycleTarget},
		missions.ActionOpenManeuver:    {km.Maneuver},
		missions.ActionPlanTransfer:    {km.PlanTransfer},
		missions.ActionSpawnCraft:      {km.SpawnCraft},
		missions.ActionThrottleFull:    {km.ThrottleFull},
		missions.ActionIgnite:          {km.ToggleBurn},
		missions.ActionStage:           {km.Stage},
		missions.ActionPlanCircularize: {km.PlanCircularize},
		missions.ActionPlanRendezvous:  {km.PlanRendezvous},
	}
	cat, err := missions.LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range cat.Missions {
		if m.Program != "tutorial" {
			continue
		}
		for _, o := range m.Objectives {
			if o.Kind != missions.KindEvent {
				continue
			}
			toks := bracketKey.FindAllStringSubmatch(o.Description, -1)
			if len(toks) == 0 {
				continue
			}
			bs, ok := byAction[o.Params.Action]
			if !ok {
				t.Errorf("%s / %q: action %q has no binding in the guard table", m.ID, o.Name, o.Params.Action)
				continue
			}
			found := false
			for _, tk := range toks {
				for _, b := range bs {
					found = found || bindingHasKey(b, tk[1])
				}
			}
			if !found {
				t.Errorf("%s / %q: text %q names no key bound to action %q", m.ID, o.Name, o.Description, o.Params.Action)
			}
		}
	}
}

// #519: following the lift-off rung literally (throttle up, then the key its
// text names) must light the engine and leave the first stage attached.
func TestFlightSchoolLiftOffRungKeyIgnitesEngine(t *testing.T) {
	m := tutorialRung(t, "tut-launch")
	var rung missions.Objective
	for i, o := range m.Objectives {
		if o.Params.Action == missions.ActionThrottleFull && i+1 < len(m.Objectives) {
			rung = m.Objectives[i+1]
		}
	}
	toks := bracketKey.FindStringSubmatch(rung.Description)
	if toks == nil {
		t.Fatalf("rung after throttle-up has no [key]: %+v", rung)
	}
	k := keyToken(toks[1])

	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	saturn.Primary = a.world.Crafts[0].Primary
	saturn.State = a.world.Crafts[0].State
	saturn.Stages[len(saturn.Stages)-1].CommandSource = spacecraft.CommandCrewed
	saturn.SyncFields()
	saturn.OnPad, saturn.Landed = true, true
	a.world.Crafts[0] = saturn
	a.world.ActiveCraftIdx = 0
	a.active = screenOrbit
	stagesBefore := len(a.world.ActiveCraft().Stages)

	pressKey(a, 'z')
	pressKey(a, []rune(k)[0])

	c := a.world.ActiveCraft()
	if len(c.Stages) != stagesBefore {
		t.Errorf("rung text %q: pressing [%s] on the pad dropped a stage (%d -> %d)", rung.Description, toks[1], stagesBefore, len(c.Stages))
	}
	if c.ManualBurn == nil {
		t.Errorf("rung text %q: pressing [%s] did not light the engine", rung.Description, toks[1])
	}
	if strings.Contains(rung.Description, "clamps") {
		t.Errorf("rung mentions clamps, there are none: %q", rung.Description)
	}
}

// ignite is recorded only when the engine actually lights: b with zero
// throttle is refused.
func TestIgniteRecordedOnlyWhenEngineLights(t *testing.T) {
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	saturn := spacecraft.NewFromLoadout(spacecraft.LoadoutSaturnVID)
	saturn.Primary = a.world.Crafts[0].Primary
	saturn.State = a.world.Crafts[0].State
	saturn.Stages[len(saturn.Stages)-1].CommandSource = spacecraft.CommandCrewed
	saturn.SyncFields()
	saturn.OnPad, saturn.Landed = true, true
	a.world.Crafts[0] = saturn
	a.world.ActiveCraftIdx = 0
	a.active = screenOrbit
	a.world.Missions = []missions.Mission{{
		ID:         "ignite",
		Objectives: []missions.Objective{{Kind: missions.KindEvent, Params: missions.Params{Action: missions.ActionIgnite}}},
	}}

	pressKey(a, 'x') // throttle cut
	pressKey(a, 'b') // refused: zero throttle
	a.world.Tick()
	if a.world.Missions[0].Status == missions.Passed {
		t.Fatal("ignite credited although the engine did not light")
	}
	pressKey(a, 'z')
	pressKey(a, 'b')
	a.world.Tick()
	if a.world.Missions[0].Status != missions.Passed {
		t.Fatalf("ignite not credited when b lit the engine (status %v)", a.world.Missions[0].Status)
	}
}
