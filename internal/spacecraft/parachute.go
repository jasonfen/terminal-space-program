package spacecraft

// Parachute subsystem (v0.12 Slice 3, ADR 0008). A Parachute is a
// per-Stage *capability* (HasParachute, catalog data, mirrored onto
// the Spacecraft from Stages[0] by SyncFields like CanSoftLand) plus a
// per-Vessel runtime *deploy state* (ChuteState). A deployed chute
// raises the vessel's effective Ballistic Coefficient enough that
// terminal velocity falls below V_CRIT, and qualifies the vessel for a
// Touchdown even without CanSoftLand — the second non-engine route
// into Landed that ADR 0004 banked.
//
// The whole subsystem is additive and omitempty: ChuteState's zero
// value is ChuteStowed and HasParachute defaults false, so a save
// written before this slice loads as a stowed, capability-less chute —
// no SchemaVersion bump (same posture as ADR 0004 / 0007).

// ChuteState is the runtime deploy state of a Parachute. One-way:
// STOWED → ARMED → DEPLOYED, with DEPLOYED terminal (no re-stow /
// cut-away). There is deliberately no torn / failure state — the
// over-speed tear model was considered and cut during the grill (ADR
// 0008 Alternatives → tear model).
type ChuteState int

const (
	// ChuteStowed is the zero value: the capability may be present but
	// the chute has not been staged/armed.
	ChuteStowed ChuteState = iota
	// ChuteArmed: staged, waiting for enough air to inflate. Arming is
	// folded into the Stage (`space`) action — see ArmParachute.
	ChuteArmed
	// ChuteDeployed: inflated; the ChuteDeployedBC bump is active. Terminal.
	ChuteDeployed
)

// String renders a ChuteState for the HUD readout (STOWED / ARMED /
// DEPLOYED).
func (s ChuteState) String() string {
	switch s {
	case ChuteArmed:
		return "ARMED"
	case ChuteDeployed:
		return "DEPLOYED"
	default:
		return "STOWED"
	}
}

// ChuteDeployedBC is the fixed effective Ballistic Coefficient (C_D ·
// A / m, m²/kg) a vessel reports while its parachute is deployed — an
// *absolute replace* of the normal BC chain, not a multiplier. The
// canopy area physically dominates the capsule's own drag, so the base
// BC is irrelevant; fixing BC makes terminal velocity predictable and
// mass-independent (the mass term lives inside BC = C_D·A/m). At Earth
// sea level (ρ₀ ≈ 1.2, g ≈ 9.81) the drag-model terminal-velocity
// relation v_term = √(2g / (ρ · BC)) gives ≈ 7.4 m/s — comfortably
// under CrashVCritMps (10 m/s). Retunable from playtest like V_CRIT.
const ChuteDeployedBC = 0.3

// ArmParachute moves a stowed parachute to the armed state. Returns
// true if it transitioned. It is a no-op (returns false) when the
// craft lacks the capability (HasParachute false), when the chute is
// already armed, or when it is already deployed (terminal). Arming is
// allowed in any conditions, including vacuum — "arm on the way down
// and forget it"; auto-deploy fires later when dynamic pressure
// reaches the floor (ADR 0008 §2). Callers gate the player-facing
// trigger on the chute being the bare top stage (the Stage action's
// single-stage no-op branch).
func (s *Spacecraft) ArmParachute() bool {
	if !s.HasParachute || s.ChuteState != ChuteStowed {
		return false
	}
	s.ChuteState = ChuteArmed
	return true
}
