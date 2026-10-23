package save

import "github.com/jasonfen/terminal-space-program/internal/missions"

// renamedObjectives maps an objective name a past catalog used to the name
// that replaced it when it is the same milestone, so a saved Passed carries
// over. Add a row when a catalog edit renames a rung rather than retiring it.
var renamedObjectives = map[string]string{
	"Stage to lift off": "Light the engine", // #525, tut-launch
}

// refreshMissions rebuilds every saved mission whose ID is in the catalog
// from the catalog, so text, params, gates and fail_on edits reach old
// saves. Missions the catalog does not know load verbatim. Progress rule:
//   - mission status carries over unchanged (a completed mission stays
//     completed, a failed one stays failed);
//   - each objective keeps its saved status by name (through
//     renamedObjectives); a saved objective the catalog dropped is dropped
//     with its status;
//   - a catalog objective the save never had is Passed only when the
//     mission is Passed or a later objective is Passed (objectives pass in
//     order, so the player is already beyond it), otherwise it takes the
//     catalog's default and is earned normally.
func refreshMissions(saved, catalog []missions.Mission) []missions.Mission {
	byID := make(map[string]missions.Mission, len(catalog))
	for _, c := range catalog {
		byID[c.ID] = c
	}
	out := missions.Clone(saved)
	for i, old := range saved {
		c, ok := byID[old.ID]
		if !ok {
			continue
		}
		prior := map[string]missions.Status{}
		for _, o := range old.Objectives {
			name := o.Name
			if n, ok := renamedObjectives[name]; ok {
				name = n
			}
			prior[name] = o.Status
		}
		m := missions.Clone([]missions.Mission{c})[0]
		m.Status = old.Status
		for j := range m.Objectives {
			if s, ok := prior[m.Objectives[j].Name]; ok {
				m.Objectives[j].Status = s
			}
		}
		// Backwards so a run of new rungs sees the ones after it settled.
		for j := len(m.Objectives) - 1; j >= 0; j-- {
			if _, ok := prior[m.Objectives[j].Name]; ok {
				continue
			}
			beyond := old.Status == missions.Passed
			for k := j + 1; k < len(m.Objectives) && !beyond; k++ {
				beyond = m.Objectives[k].Status == missions.Passed
			}
			if beyond {
				m.Objectives[j].Status = missions.Passed
			}
		}
		out[i] = m
	}
	return out
}
