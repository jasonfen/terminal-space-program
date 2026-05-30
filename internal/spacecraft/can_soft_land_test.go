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
// loadout has the 2-stage LM (Descent + Ascent) in the middle of its
// [S-IC, S-II, S-IVB, Descent, Ascent, CSM] chain (v0.12 Slice 2 /
// ADR 0007). The active Spacecraft at spawn flies as the composite,
// with S-IC as the bottom stage — CanSoftLand=false then. After
// decoupling through S-IC / S-II / S-IVB the next-to-fire is Descent,
// whose entry must carry CanSoftLand=true so the LM-decouple
// jettisoned craft inherits the soft-land capability. Ascent also
// carries CanSoftLand=true (ADR 0007 decision 5).
func TestApolloStackLanderStageCarriesCanSoftLand(t *testing.T) {
	l := LookupLoadout(LoadoutApolloStackID)
	if len(l.Stages) < 4 {
		t.Fatalf("Apollo Stack should have ≥ 4 stages, got %d", len(l.Stages))
	}
	for _, name := range []string{"Descent", "Ascent"} {
		var st *Stage
		for i := range l.Stages {
			if l.Stages[i].Name == name {
				st = &l.Stages[i]
				break
			}
		}
		if st == nil {
			t.Fatalf("could not find %q stage in Apollo Stack", name)
		}
		if !st.CanSoftLand {
			t.Errorf("Apollo Stack %s stage CanSoftLand = false, want true (catalog lookup by name)", name)
		}
	}
}

// TestLanderStageCarriesHasLegs (v0.11.5 sub-scope 6; updated v0.12
// Slice 2) — the legs ride on the LM *Descent* stage. Both the
// stand-alone Lander loadout (Stages[0] = Descent) and the
// Apollo-Stack's Descent stage must carry LaunchSpriteHasLegs=true so
// the renderer paints the splayed-leg silhouette. The Ascent stage
// (legs stayed on the descent stage) must NOT carry legs.
func TestLanderStageCarriesHasLegs(t *testing.T) {
	if l := LookupLoadout(LoadoutLanderID); len(l.Stages) == 0 || !l.Stages[0].LaunchSpriteHasLegs {
		t.Errorf("Loadouts[Lander].Stages[0] (Descent).LaunchSpriteHasLegs = false, want true")
	}
	apollo := LookupLoadout(LoadoutApolloStackID)
	var descent, ascent *Stage
	for i := range apollo.Stages {
		switch apollo.Stages[i].Name {
		case "Descent":
			descent = &apollo.Stages[i]
		case "Ascent":
			ascent = &apollo.Stages[i]
		}
	}
	if descent == nil || ascent == nil {
		t.Fatal("Apollo Stack missing Descent/Ascent stages")
	}
	if !descent.LaunchSpriteHasLegs {
		t.Errorf("Apollo Stack Descent stage LaunchSpriteHasLegs = false, want true")
	}
	if ascent.LaunchSpriteHasLegs {
		t.Errorf("Apollo Stack Ascent stage LaunchSpriteHasLegs = true, want false (legs stay on descent)")
	}
	// Other launch-vehicle bottom stages must NOT carry legs (would
	// produce a phantom landing-leg silhouette on the S-IC etc.).
	for _, id := range []string{LoadoutSaturnVID, LoadoutSLSBlock1ID, LoadoutFalcon9ID, LoadoutSIVB1ID, LoadoutICPSID} {
		s := LookupLoadout(id).Stages[0]
		if s.LaunchSpriteHasLegs {
			t.Errorf("Loadouts[%s].Stages[0].LaunchSpriteHasLegs = true, want false", id)
		}
	}
}
