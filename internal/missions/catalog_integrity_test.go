package missions

import "testing"

// TestEmbeddedCatalogIntegrity guards the hand-authored embedded ladder
// (v0.21 Slice 6) against the silent-failure modes the typed-kind schema is
// prone to: a typo'd kind or event action sits inert (never passes), and a
// requires edge to a missing mission — or a cycle — leaves a rung
// permanently locked. Pinning these in a test catches an authoring slip the
// compiler can't.
func TestEmbeddedCatalogIntegrity(t *testing.T) {
	cat, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}

	validKinds := map[Kind]bool{
		KindCircularize: true, KindOrbitInsertion: true, KindSOIFlyby: true,
		KindCircularizeFromPad: true, KindReachAltitude: true, KindLandAtBody: true,
		KindRendezvous: true, KindDock: true, KindReturnToBody: true, KindEvent: true,
		// Cycle 2 (ADR 0027) comms kinds — authored into the ladder by cycle 3.
		KindRelayCoverage: true, KindEstablishContact: true,
	}
	validActions := map[Action]bool{
		ActionThrottleFull: true, ActionThrottleCut: true, ActionThrottleUp: true,
		ActionThrottleDown: true, ActionOpenManeuver: true, ActionPlanTransfer: true,
		ActionPlanCircularize: true, ActionPlanIncl: true, ActionPlanRendezvous: true,
		ActionRefinePlan: true, ActionClearNodes: true, ActionToggleBurn: true,
		ActionStage: true, ActionCycleTarget: true, ActionClearTarget: true,
		ActionCycleView: true, ActionCycleNavMode: true, ActionAutoWarp: true,
		ActionSpawnCraft: true, ActionUndock: true, ActionTranspose: true,
		// Cycle 3 (ADR 0028) deploy verb — authored into the ladder by cycle 3.
		ActionDeploy: true,
	}

	ids := make(map[string]bool, len(cat.Missions))
	for _, m := range cat.Missions {
		if ids[m.ID] {
			t.Errorf("duplicate mission id %q", m.ID)
		}
		ids[m.ID] = true
	}

	for _, m := range cat.Missions {
		for _, req := range m.Requires {
			if !ids[req] {
				t.Errorf("mission %q requires unknown mission %q", m.ID, req)
			}
		}
		for j, o := range m.Objectives {
			if !validKinds[o.Kind] {
				t.Errorf("mission %q objective %d has unknown kind %q", m.ID, j, o.Kind)
			}
			// An event objective with an unknown (or empty) action can never
			// fire — the player would be stuck on a step that nothing matches.
			if o.Kind == KindEvent && !validActions[o.Params.Action] {
				t.Errorf("mission %q objective %d (event) has unknown action %q", m.ID, j, o.Params.Action)
			}
		}
	}

	// Every mission must be reachable: resolve requires by Kahn's algorithm
	// (repeatedly mark missions whose prerequisites are all marked). Anything
	// left unmarked is in a cycle or depends on a dangling prerequisite.
	resolved := make(map[string]bool, len(cat.Missions))
	for progress := true; progress; {
		progress = false
		for _, m := range cat.Missions {
			if resolved[m.ID] {
				continue
			}
			ok := true
			for _, req := range m.Requires {
				if !resolved[req] {
					ok = false
					break
				}
			}
			if ok {
				resolved[m.ID] = true
				progress = true
			}
		}
	}
	for _, m := range cat.Missions {
		if !resolved[m.ID] {
			t.Errorf("mission %q is unreachable (requires cycle or dangling prerequisite)", m.ID)
		}
	}
}
