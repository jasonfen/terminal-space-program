// Package spacecraft — v0.11.4+ CanSoftLand catalog-flag tests
// (ADR 0004 + per-Stage follow-up). Pins which Stages qualify as
// soft-land candidates so a future catalog change doesn't silently
// flip an unrelated vessel's lifecycle behaviour.

package spacecraft

import "testing"

// TestLanderAndFalcon9CanSoftLand — the two v0.11.4 launchable
// consumers of the surface-arrival predicate carry CanSoftLand=true
// on their bottom stage (Falcon-9 → F9-S1; Lander → its only
// stage). NewFromLoadout calls SyncFields which mirrors the bottom
// stage's flag onto the freshly spawned Spacecraft.
func TestLanderAndFalcon9CanSoftLand(t *testing.T) {
	for _, id := range []string{LoadoutLanderID, LoadoutFalcon9ID} {
		l := LookupLoadout(id)
		if len(l.Stages) == 0 || !l.Stages[0].CanSoftLand {
			t.Errorf("Loadouts[%q].Stages[0].CanSoftLand = false, want true", id)
		}
		c := NewFromLoadout(id)
		if !c.CanSoftLand {
			t.Errorf("NewFromLoadout(%q).CanSoftLand = false, want true (SyncFields propagation)", id)
		}
	}
}

// TestSaturnVAndS_IVB1DoNotSoftLand — non-lander loadouts' bottom
// stage stays CanSoftLand=false so the surface-arrival predicate
// routes any contact through the Crashed branch.
func TestSaturnVAndS_IVB1DoNotSoftLand(t *testing.T) {
	for _, id := range []string{LoadoutSaturnVID, LoadoutSIVB1ID} {
		l := LookupLoadout(id)
		if len(l.Stages) > 0 && l.Stages[0].CanSoftLand {
			t.Errorf("Loadouts[%q].Stages[0].CanSoftLand = true, want false", id)
		}
		c := NewFromLoadout(id)
		if c.CanSoftLand {
			t.Errorf("NewFromLoadout(%q).CanSoftLand = true, want false", id)
		}
	}
}

// TestApolloStackLanderStageCarriesCanSoftLand — the Apollo Stack
// loadout has the Lander stage in the middle of its [S-IC, S-II,
// S-IVB, LM, CSM] chain. The active Spacecraft at spawn flies as
// the composite, with S-IC as the bottom stage — CanSoftLand=false
// then (S-IC can't land). After decoupling through S-IC / S-II /
// S-IVB the next-to-fire is LM, whose entry must carry
// CanSoftLand=true so the eventual Lander-decouple jettisoned craft
// inherits the soft-land capability.
func TestApolloStackLanderStageCarriesCanSoftLand(t *testing.T) {
	l := LookupLoadout(LoadoutApolloStackID)
	if len(l.Stages) < 4 {
		t.Fatalf("Apollo Stack should have ≥ 4 stages, got %d", len(l.Stages))
	}
	// Walk the chain to find the Lander stage (LM-named). The
	// Apollo-Stack convention is bottom-first: [S-IC, S-II, S-IVB,
	// LM, CSM] → LM at Stages[3].
	var lmStage *Stage
	for i := range l.Stages {
		if l.Stages[i].Name == "LM" {
			lmStage = &l.Stages[i]
			break
		}
	}
	if lmStage == nil {
		t.Fatal("could not find LM stage in Apollo Stack")
	}
	if !lmStage.CanSoftLand {
		t.Errorf("Apollo Stack LM stage CanSoftLand = false, want true (catalog lookup by name)")
	}
}
