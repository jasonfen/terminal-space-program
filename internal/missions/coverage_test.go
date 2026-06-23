package missions

import "testing"

// TestRelayCoverageObjective (C2-6, ADR 0027): passes once at least
// MinRelays relay craft are connected; pass-and-stick; inert at MinRelays 0.
func TestRelayCoverageObjective(t *testing.T) {
	o := Objective{Kind: KindRelayCoverage, Params: Params{MinRelays: 3}, Status: InProgress}
	if got := o.Evaluate(EvalContext{ConnectedRelayCount: 2}); got != InProgress {
		t.Errorf("2 of 3 relays connected: got %v, want InProgress", got)
	}
	if got := o.Evaluate(EvalContext{ConnectedRelayCount: 3}); got != Passed {
		t.Errorf("3 of 3 relays connected: got %v, want Passed", got)
	}
	if got := o.Evaluate(EvalContext{ConnectedRelayCount: 5}); got != Passed {
		t.Errorf("more than enough relays: got %v, want Passed", got)
	}
	inert := Objective{Kind: KindRelayCoverage, Status: InProgress} // MinRelays 0
	if got := inert.Evaluate(EvalContext{ConnectedRelayCount: 5}); got != InProgress {
		t.Errorf("MinRelays 0 must be inert, got %v", got)
	}
}

// TestEstablishContactObjective (C2-6): passes when the active craft has a
// connection; pass-and-stick.
func TestEstablishContactObjective(t *testing.T) {
	o := Objective{Kind: KindEstablishContact, Status: InProgress}
	if got := o.Evaluate(EvalContext{ActiveConnected: false}); got != InProgress {
		t.Errorf("no connection: got %v, want InProgress", got)
	}
	if got := o.Evaluate(EvalContext{ActiveConnected: true}); got != Passed {
		t.Errorf("connected: got %v, want Passed", got)
	}
}

// TestDeployActionInVocabulary (C2-6): the deploy semantic action exists
// and an event objective matches it (cycle 3 emits it).
func TestDeployActionInVocabulary(t *testing.T) {
	if ActionDeploy != "deploy" {
		t.Errorf("ActionDeploy = %q, want deploy", ActionDeploy)
	}
	o := Objective{Kind: KindEvent, Params: Params{Action: ActionDeploy}, Status: InProgress}
	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionDeploy}}); got != Passed {
		t.Errorf("deploy event: got %v, want Passed", got)
	}
	if got := o.Evaluate(EvalContext{RecentActions: []Action{ActionStage}}); got != InProgress {
		t.Errorf("unrelated action: got %v, want InProgress", got)
	}
}
