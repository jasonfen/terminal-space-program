// Schema v12 -> v13: Flight School's "Off the Pad" ladder changed (#525):
// the lift-off rung "Stage to lift off" ([space], action stage) became
// "Light the engine" ([b], action ignite), and a new rung "Stage when the
// tank runs dry" sits after "Pitch east". Saves persist whole missions, so
// without this every existing save would keep the old [space] text (the
// #519 bug) and never see the new rung.
package save

import "github.com/jasonfen/terminal-space-program/internal/missions"

const tutLaunchID = "tut-launch"

// legacyLiftRungName is the v12 lift-off rung. Its replacement is the same
// milestone (the vessel has left the pad), so a Passed status carries over.
const (
	legacyLiftRungName = "Stage to lift off"
	liftRungName       = "Light the engine"
)

// migrateV12PayloadToV13 rebuilds tut-launch from the current catalog. A
// catalog that fails to load leaves the payload untouched (missions are
// additive, as in worldFromPayload).
func migrateV12PayloadToV13(p *Payload) {
	cat, err := missions.LoadAll()
	if err != nil {
		return
	}
	for _, c := range cat.Missions {
		if c.ID == tutLaunchID {
			rebuildTutLaunch(p, c)
			return
		}
	}
}

// rebuildTutLaunch replaces the payload's tut-launch with the catalog
// mission, carrying progress forward by this rule:
//   - mission status carries over unchanged;
//   - each objective keeps its status by name where the name still exists,
//     and the retired "Stage to lift off" hands its status to "Light the
//     engine";
//   - an objective new to the ladder (the staging rung) is Passed when the
//     mission is Passed or any later objective is Passed (the player is
//     already beyond it, never re-opened), otherwise it starts InProgress;
//   - so a rung the player completed is never un-passed, and a not-started
//     mission simply becomes the catalog's objectives.
//
// A payload without tut-launch (a user overlay dropped it, or Missions is
// nil and worldFromPayload will seed it) is left alone.
func rebuildTutLaunch(p *Payload, cat missions.Mission) {
	for i := range p.Missions {
		old := p.Missions[i]
		if old.ID != tutLaunchID {
			continue
		}
		prior := map[string]missions.Status{}
		for _, o := range old.Objectives {
			name := o.Name
			if name == legacyLiftRungName {
				name = liftRungName
			}
			prior[name] = o.Status
		}
		rebuilt := missions.Clone([]missions.Mission{cat})[0]
		rebuilt.Status = old.Status
		for j := range rebuilt.Objectives {
			if s, ok := prior[rebuilt.Objectives[j].Name]; ok {
				rebuilt.Objectives[j].Status = s
			}
		}
		for j := range rebuilt.Objectives {
			if _, ok := prior[rebuilt.Objectives[j].Name]; ok {
				continue
			}
			beyond := old.Status == missions.Passed
			for k := j + 1; k < len(rebuilt.Objectives) && !beyond; k++ {
				beyond = rebuilt.Objectives[k].Status == missions.Passed
			}
			if beyond {
				rebuilt.Objectives[j].Status = missions.Passed
			}
		}
		p.Missions[i] = rebuilt
		return
	}
}
