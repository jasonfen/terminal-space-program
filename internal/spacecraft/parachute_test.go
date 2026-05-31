package spacecraft

import "testing"

// TestEffectiveBCDeployedShortCircuits — a deployed parachute swamps
// the capsule's own drag: EffectiveBallisticCoefficient returns the
// fixed ChuteDeployedBC regardless of the bottom stage's BC or the
// legacy / default fallback chain (ADR 0008 §3).
func TestEffectiveBCDeployedShortCircuits(t *testing.T) {
	// Bottom stage carries a small BC; deployed chute should override it.
	c := &Spacecraft{
		Stages: []Stage{{BallisticCoefficient: 8e-6}},
	}
	if got := c.EffectiveBallisticCoefficient(); got != 8e-6 {
		t.Fatalf("stowed: got %g, want stage BC 8e-6", got)
	}
	c.ChuteState = ChuteArmed
	if got := c.EffectiveBallisticCoefficient(); got != 8e-6 {
		t.Errorf("armed: got %g, want stage BC 8e-6 (armed != deployed)", got)
	}
	c.ChuteState = ChuteDeployed
	if got := c.EffectiveBallisticCoefficient(); got != ChuteDeployedBC {
		t.Errorf("deployed: got %g, want ChuteDeployedBC %g", got, ChuteDeployedBC)
	}
}

// TestEffectiveBCDeployedOverridesDefaultFallback — deployed BC wins
// even when the craft has no stage BC at all (would otherwise hit the
// DefaultBallisticCoefficient fallback).
func TestEffectiveBCDeployedOverridesDefaultFallback(t *testing.T) {
	c := &Spacecraft{ChuteState: ChuteDeployed}
	if got := c.EffectiveBallisticCoefficient(); got != ChuteDeployedBC {
		t.Errorf("deployed w/ no stages: got %g, want ChuteDeployedBC %g", got, ChuteDeployedBC)
	}
}

// TestSyncFieldsDerivesHasParachute — the Spacecraft.HasParachute
// mirror is re-derived from Stages[0] on every SyncFields, exactly
// like CanSoftLand (ADR 0008 §1).
func TestSyncFieldsDerivesHasParachute(t *testing.T) {
	c := &Spacecraft{
		Stages: []Stage{
			{Name: "booster", HasParachute: false, Thrust: 1000, Isp: 300},
			{Name: "capsule", HasParachute: true},
		},
	}
	c.SyncFields()
	// Bottom stage (booster) has no chute, so the mirror is false even
	// though an upper stage carries one — the chute rides the surviving
	// top stage and only becomes "active" once it is the bottom.
	if c.HasParachute {
		t.Errorf("HasParachute mirror should be false while a non-chute booster is the bottom stage")
	}
	// Drop the booster so the capsule is the bottom stage.
	c.Stages = c.Stages[1:]
	c.SyncFields()
	if !c.HasParachute {
		t.Errorf("HasParachute mirror should be true once the chute-bearing capsule is the bottom stage")
	}
}

// TestCSMStageHasParachute — the Apollo CSM stage (catalog + the
// Apollo-Stack loadout literal that resolves by Name) carries the
// parachute capability so the marquee Apollo arc earns an Earth
// splashdown (ADR 0008 §6).
func TestCSMStageHasParachute(t *testing.T) {
	csm, ok := BuildStage(StageModuleCSMID)
	if !ok {
		t.Fatalf("BuildStage(csm) not found in catalog")
	}
	if !csm.HasParachute {
		t.Errorf("catalog csm stage should have HasParachute=true")
	}
	if csm.CanSoftLand {
		t.Errorf("csm has no engine landing capability — CanSoftLand should be false")
	}
	// The Apollo-Stack loadout's CSM literal resolves its flags by Name.
	if !catalogHasParachuteByName("CSM") {
		t.Errorf("catalogHasParachuteByName(CSM) should be true")
	}
}

// TestReentryCapsuleLoadout — the standalone re-entry capsule is a
// single command-module-class stage carrying HasParachute and *not*
// CanSoftLand: the clean, directly-spawnable test vehicle (ADR 0008
// §6). It must be in LoadoutOrder so the spawn form lists it.
func TestReentryCapsuleLoadout(t *testing.T) {
	c := NewFromLoadout(LoadoutCapsuleID)
	if len(c.Stages) != 1 {
		t.Fatalf("re-entry capsule should be single-stage, got %d", len(c.Stages))
	}
	if !c.HasParachute {
		t.Errorf("re-entry capsule should have HasParachute=true (mirror), got false")
	}
	if c.CanSoftLand {
		t.Errorf("re-entry capsule should NOT have CanSoftLand (chute is its only route down)")
	}
	if c.ChuteState != ChuteStowed {
		t.Errorf("fresh capsule chute should be Stowed, got %v", c.ChuteState)
	}
	var inOrder bool
	for _, id := range LoadoutOrder {
		if id == LoadoutCapsuleID {
			inOrder = true
		}
	}
	if !inOrder {
		t.Errorf("LoadoutCapsuleID missing from LoadoutOrder (spawn form won't list it)")
	}
}

// TestArmParachuteStateMachine — ArmParachute moves a stowed chute to
// armed exactly once and only when the craft has the capability; it is
// a no-op (returns false) without the capability, when already armed,
// and when already deployed (one-way, no re-stow — ADR 0008 §2).
func TestArmParachuteStateMachine(t *testing.T) {
	// No capability → cannot arm.
	noCap := &Spacecraft{HasParachute: false}
	if noCap.ArmParachute() {
		t.Errorf("ArmParachute on a craft without HasParachute should return false")
	}
	if noCap.ChuteState != ChuteStowed {
		t.Errorf("state should stay Stowed when arming fails, got %v", noCap.ChuteState)
	}

	c := &Spacecraft{HasParachute: true}
	if !c.ArmParachute() {
		t.Fatalf("first ArmParachute on a capable stowed craft should return true")
	}
	if c.ChuteState != ChuteArmed {
		t.Fatalf("state should be Armed after arming, got %v", c.ChuteState)
	}
	if c.ArmParachute() {
		t.Errorf("re-arming an already-armed chute should return false (no-op)")
	}
	// Deployed is terminal: arming must not re-stow / re-arm it.
	c.ChuteState = ChuteDeployed
	if c.ArmParachute() {
		t.Errorf("arming a deployed chute should return false (terminal)")
	}
	if c.ChuteState != ChuteDeployed {
		t.Errorf("deployed state must be preserved, got %v", c.ChuteState)
	}
}
