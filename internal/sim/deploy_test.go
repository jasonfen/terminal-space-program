package sim

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// deployCarrier spawns a 3-craft composite — an S-IC carrier core with two
// stacked nose payloads (S-IVB below, CSM on top) via a [1,1] NosePayloadPlan
// — and returns the world plus the carrier (the active craft).
func deployCarrier(t *testing.T) (*World, *spacecraft.Spacecraft) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	sic, _ := spacecraft.BuildStage(spacecraft.StageModuleSICID)
	sivb, _ := spacecraft.BuildStage(spacecraft.StageModuleSIVBID)
	csm, _ := spacecraft.BuildStage(spacecraft.StageModuleCSMID)
	carrier, err := w.SpawnCraft(SpawnSpec{
		CustomStages:    []spacecraft.Stage{sic, sivb, csm},
		NosePayloadPlan: []int{1, 1},
		ParentBodyID:    "earth",
		AltitudeM:       400e3,
	})
	if err != nil {
		t.Fatalf("SpawnCraft(carrier): %v", err)
	}
	return w, carrier
}

// TestDeployPopsTopPayloadKeepsCarrier — v0.23 / ADR 0028 C3-2. Deploy releases
// the topmost payload as its own craft, keeps the carrier active (drop-and-
// continue — the constellation/tug loop), and records the `deploy` action.
// Contrast with Undock, which explodes the whole composite and switches the
// active craft to a released component.
func TestDeployPopsTopPayloadKeepsCarrier(t *testing.T) {
	w, carrier := deployCarrier(t)
	idx := w.ActiveCraftIdx
	before := len(w.Crafts)

	if !w.Deploy(idx) {
		t.Fatal("Deploy of a loaded carrier returned false")
	}
	if len(w.Crafts) != before+1 {
		t.Fatalf("slate %d→%d, want +1 (released payload appended)", before, len(w.Crafts))
	}
	// Carrier stays the active craft — same object, mutated in place.
	if w.ActiveCraft() != carrier {
		t.Errorf("active craft changed after Deploy; want the carrier to keep flying")
	}
	if !strings.HasPrefix(carrier.Name, "S-IC") {
		t.Errorf("carrier name = %q, want S-IC* (still the firing core)", carrier.Name)
	}
	// Top payload (CSM) popped; carrier is still a composite carrying S-IVB.
	if got := stageNames(carrier.Stages); len(got) != 2 || got[0] != "S-IC" || got[1] != "S-IVB" {
		t.Errorf("carrier stages = %v, want [S-IC S-IVB]", got)
	}
	if len(carrier.DockedComponents) != 2 {
		t.Errorf("carrier DockedComponents = %d, want 2 (core + remaining payload)", len(carrier.DockedComponents))
	}

	// The released craft is the standalone CSM, controllable (EnsureCommandSource
	// ran on the released component — the crewed CM is its command source).
	released := w.Crafts[len(w.Crafts)-1]
	if got := stageNames(released.Stages); len(got) != 1 || got[0] != "CSM" {
		t.Fatalf("released craft stages = %v, want [CSM]", got)
	}
	if !released.Controllable {
		t.Errorf("released CSM not Controllable — command source not restored on deploy")
	}

	// The deploy action reaches the mission event sink.
	if acts := w.missionEvalContext().RecentActions; !containsAction(acts, missions.ActionDeploy) {
		t.Errorf("RecentActions = %v, want it to contain %q", acts, missions.ActionDeploy)
	}
}

// TestDeployDownToBareCarrier — successive deploys pop payloads top-down until
// only the carrier core remains, at which point it reverts to a plain craft
// (no DockedComponents) and a further Deploy is a no-op.
func TestDeployDownToBareCarrier(t *testing.T) {
	w, carrier := deployCarrier(t)
	idx := w.ActiveCraftIdx
	before := len(w.Crafts)

	if !w.Deploy(idx) { // pops CSM
		t.Fatal("first Deploy returned false")
	}
	if !w.Deploy(idx) { // pops S-IVB → only the S-IC core remains
		t.Fatal("second Deploy returned false")
	}
	if got := stageNames(carrier.Stages); len(got) != 1 || got[0] != "S-IC" {
		t.Errorf("carrier stages = %v, want [S-IC]", got)
	}
	if len(carrier.DockedComponents) != 0 {
		t.Errorf("bare carrier DockedComponents = %d, want 0 (reverted to plain craft)", len(carrier.DockedComponents))
	}
	if w.Deploy(idx) {
		t.Errorf("Deploy on a payload-less carrier returned true, want false (no-op)")
	}
	// Two payloads released → slate grew by 2.
	if len(w.Crafts) != before+2 {
		t.Errorf("slate = %d, want %d (+2 released payloads)", len(w.Crafts), before+2)
	}
}

// TestDeployedPayloadDoesNotReFuse — v0.23 / ADR 0028 C3-2 regression. The
// released payload must clear the auto-dock proximity gate (DockingDistM /
// DockingVMS), or checkDocking re-fuses it onto the carrier on the next tick
// and the deploy silently undoes itself. The carrier holds its state, so the
// gap is the one-sided separation push — it must exceed the gate, unlike
// Undock's symmetric spread.
func TestDeployedPayloadDoesNotReFuse(t *testing.T) {
	w, carrier := deployCarrier(t)
	if !w.Deploy(w.ActiveCraftIdx) {
		t.Fatal("Deploy returned false")
	}
	n := len(w.Crafts)
	comps := len(carrier.DockedComponents)

	// checkDocking runs every physics tick; it must NOT re-dock the freshly
	// deployed payload back onto the carrier.
	w.checkDocking()

	if len(w.Crafts) != n {
		t.Errorf("slate %d → %d after checkDocking: the deployed payload re-fused onto the carrier", n, len(w.Crafts))
	}
	if len(carrier.DockedComponents) != comps {
		t.Errorf("carrier components %d → %d: a craft re-docked onto it", comps, len(carrier.DockedComponents))
	}
}

func containsAction(acts []missions.Action, want missions.Action) bool {
	for _, a := range acts {
		if a == want {
			return true
		}
	}
	return false
}
