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

// TestLunarCoverageObjective (ADR 0065 guard 10): passes on the callback's
// fraction reaching pass_frac, including the equal case; nil callback and
// ok=false stay InProgress; a missing primary_id is inert.
func TestLunarCoverageObjective(t *testing.T) {
	cb := func(f float64, ok bool) func(string, float64, float64, float64, float64) (float64, bool) {
		return func(string, float64, float64, float64, float64) (float64, bool) { return f, ok }
	}
	mk := func(pf float64) Objective {
		return Objective{Kind: KindLunarCoverage, Status: InProgress, Params: Params{PrimaryID: "moon", SiteLatDeg: -90, PassFrac: pf}}
	}
	cases := []struct {
		name string
		o    Objective
		cb   func(string, float64, float64, float64, float64) (float64, bool)
		want Status
	}{
		{"1.0 vs 1.0", mk(1.0), cb(1.0, true), Passed},
		{"0.99 vs 1.0", mk(1.0), cb(0.99, true), InProgress},
		{"0.99 vs 0.99 equal", mk(0.99), cb(0.99, true), Passed},
		{"default pass_frac", mk(0), cb(0.99, true), InProgress},
		{"nil callback", mk(1.0), nil, InProgress},
		{"ok false", mk(1.0), cb(1.0, false), InProgress},
		{"no primary_id", Objective{Kind: KindLunarCoverage, Status: InProgress}, cb(1.0, true), InProgress},
	}
	for _, c := range cases {
		if got := c.o.Evaluate(EvalContext{SiteCoverage: c.cb}); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
