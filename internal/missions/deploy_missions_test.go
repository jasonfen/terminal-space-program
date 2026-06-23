package missions

import "testing"

// findMission returns the embedded mission with the given id (a copy), failing
// the test if it is absent.
func findMission(t *testing.T, id string) Mission {
	t.Helper()
	cat, err := DefaultCatalog()
	if err != nil {
		t.Fatalf("DefaultCatalog: %v", err)
	}
	for _, m := range cat.Missions {
		if m.ID == id {
			return m
		}
	}
	t.Fatalf("embedded catalog missing mission %q", id)
	return Mission{}
}

// TestDeployPayloadMissionLatches — v0.23 / ADR 0028 C3-5. The pure deploy-event
// mission passes the moment a deploy action fires, and ignores unrelated actions.
func TestDeployPayloadMissionLatches(t *testing.T) {
	m := findMission(t, "chal-deploy-payload")
	if got := m.Evaluate(EvalContext{RecentActions: []Action{ActionStage}}); got != InProgress {
		t.Errorf("unrelated action: mission status %v, want InProgress", got)
	}
	if got := m.Evaluate(EvalContext{RecentActions: []Action{ActionDeploy}}); got != Passed {
		t.Errorf("deploy fired: mission status %v, want Passed", got)
	}
}

// TestRelayConstellationMissionComposes — v0.23 / ADR 0028 C3-5. The
// constellation mission composes existing kinds with no new evalKind: an
// ordered [deploy event] → [relay_coverage min_relays 3]. The coverage step
// stays locked until the deploy step latches, then passes once three relays are
// connected.
func TestRelayConstellationMissionComposes(t *testing.T) {
	m := findMission(t, "chal-relay-constellation")

	// Coverage is reached but no deploy yet: the ordered deploy step is still
	// active, so the mission must not pass on coverage alone.
	if got := m.Evaluate(EvalContext{ConnectedRelayCount: 3}); got != InProgress {
		t.Fatalf("coverage before deploy: mission status %v, want InProgress (deploy step gates it)", got)
	}
	// Deploy fires → first objective passes; coverage not yet met this tick.
	if got := m.Evaluate(EvalContext{RecentActions: []Action{ActionDeploy}}); got != InProgress {
		t.Fatalf("after deploy, no coverage: mission status %v, want InProgress", got)
	}
	// Three relays connected → coverage step passes → mission complete.
	if got := m.Evaluate(EvalContext{ConnectedRelayCount: 3}); got != Passed {
		t.Errorf("deploy + 3 relays connected: mission status %v, want Passed", got)
	}
}
