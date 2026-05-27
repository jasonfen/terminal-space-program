package spacecraft

// Stage catalog (v0.10.1+). An ID-addressable library of reusable
// stage "modules" — the parts menu the spawn-form stack configurator
// builds a custom craft from, and the source the multi-tier
// Apollo-Stack loadout's payload tiers are sized against.
//
// Design note — additive only. The pre-v0.10.1 Loadouts literals
// (Saturn-V / SLS / Falcon-9 / single-stage) are deliberately NOT
// refactored to reference this catalog: every existing loadout and
// golden test must stay byte-identical. The numbers below mirror the
// inline loadout stages so a catalog-built module flies identically
// to the same stage inside its canonical launch vehicle, but the two
// definitions are independent on purpose (the catalog can gain parts
// or be retuned without touching shipped loadout regression tests).
//
// Catalog modules carry an empty Stage.LoadoutID — they did not come
// from a Loadout. Their Glyph / Color / Name are populated so the
// jettison-as-passive path (sim/staging.go buildJettisonedCraft) and
// the STAGES HUD still render them; save round-trips the per-stage
// fields directly (save schema v6, no migration needed).

// StageModule is one catalog part: a stage preset plus the metadata
// the configurator UI needs to list and describe it.
type StageModule struct {
	// ID is the stable catalog key (kebab-case). Referenced by the
	// configurator and BuildStage.
	ID string
	// Name / Glyph / Color are copied onto the built Stage so the
	// HUD + jettison rendering have a label and marker.
	Name  string
	Glyph string
	Color string
	// Tier is a one-word configurator hint: "booster", "sustainer",
	// "transfer", "payload", "tug". Purely descriptive.
	Tier string
	// dry / fuel / thrust / isp / bc are the stage's physical
	// numbers (kg, kg, N, s, m²/kg) — same units as stageWithBC.
	dry, fuel, thrust, isp, bc float64
	// launchSpriteRowsPx is the per-stage height (in braille
	// sub-pixels) of this stage's ViewLaunch silhouette. Rendered
	// as a `spriteWidthPx × launchSpriteRowsPx` filled rectangle
	// of braille dots via PlotColored, anchored at the stage's
	// bottom-centre. See CONTEXT.md "Launch Sprite" for the
	// convention; row count is stylised, not real metres.
	launchSpriteRowsPx int
	// canSoftLand (v0.11.4-followup) marks stages designed to
	// soft-land — populates the matching Stage.CanSoftLand flag
	// via catalogCanSoftLandByName so the surface-arrival
	// predicate gates correctly across staging. Today's true
	// entries: lander (LM-derived descent stage), f9-s1 (Falcon
	// 9 first stage with retro-burn recovery). Everything else
	// stays false — Saturn V stages crash on contact, CSM crashes
	// on contact, F9-S2 crashes on contact.
	canSoftLand bool
}

// Catalog stage IDs.
const (
	StageModuleSICID      = "s-ic"      // Saturn V first stage (F-1 cluster)
	StageModuleSIIID      = "s-ii"      // Saturn V second stage (J-2 cluster)
	StageModuleSIVBID     = "s-ivb"     // S-IVB / J-2 insertion + transfer
	StageModuleICPSID     = "icps"      // RL-10 low-TWR transfer stage
	StageModuleSRBID      = "srb"       // SLS twin 5-segment solids
	StageModuleCoreRS25ID = "core-rs25" // SLS core (4× RS-25)
	StageModuleF9S1ID     = "f9-s1"     // Falcon 9 first stage (9× Merlin 1D)
	StageModuleF9S2ID     = "f9-s2"     // Falcon 9 second stage (Merlin Vac)
	StageModuleLanderID   = "lander"    // LM-derived throttleable descent
	StageModuleCSMID      = "csm"       // Apollo Command/Service Module (SPS)
	StageModuleRCSTugID   = "rcs-tug"   // pure-monoprop proximity-ops module
)

// StageCatalog indexes the parts library by ID. The numbers mirror
// the inline loadout literals in loadouts.go (see file-level note).
// The CSM is net-new in v0.10.1 — Apollo Command/Service Module:
// CM + SM dry ≈ 11,900 kg, SPS storable propellant ≈ 18,400 kg,
// SPS thrust 91.2 kN @ Isp 314 s. It can do real orbital maneuvers,
// which is what makes it a rendezvous-relevant payload tier.
var StageCatalog = map[string]StageModule{
	StageModuleSICID: {
		ID: StageModuleSICID, Name: "S-IC", Glyph: "▲", Color: "#FF8C42",
		Tier: "booster", dry: 130000, fuel: 2160000, thrust: 35100000, isp: 263, bc: 8e-6,
		launchSpriteRowsPx: 24,
	},
	StageModuleSIIID: {
		ID: StageModuleSIIID, Name: "S-II", Glyph: "▲", Color: "#FFC042",
		Tier: "sustainer", dry: 40000, fuel: 440000, thrust: 5140000, isp: 421, bc: 2.5e-5,
		launchSpriteRowsPx: 20,
	},
	StageModuleSIVBID: {
		ID: StageModuleSIVBID, Name: "S-IVB", Glyph: "▲", Color: "#FFD93D",
		Tier: "transfer", dry: 11000, fuel: 109000, thrust: 1023000, isp: 421, bc: 6.25e-5,
		launchSpriteRowsPx: 12,
	},
	StageModuleICPSID: {
		ID: StageModuleICPSID, Name: "ICPS", Glyph: "◆", Color: "#5BB3FF",
		Tier: "transfer", dry: 3500, fuel: 25000, thrust: 110000, isp: 462, bc: 6.25e-5,
		launchSpriteRowsPx: 8,
	},
	StageModuleSRBID: {
		ID: StageModuleSRBID, Name: "SRBs", Glyph: "▲", Color: "#E0E0E0",
		Tier: "booster", dry: 198000, fuel: 1270000, thrust: 32000000, isp: 268, bc: 8e-6,
		launchSpriteRowsPx: 28,
	},
	StageModuleCoreRS25ID: {
		ID: StageModuleCoreRS25ID, Name: "Core", Glyph: "▲", Color: "#FF6B35",
		Tier: "sustainer", dry: 85275, fuel: 979452, thrust: 9290000, isp: 452, bc: 2.5e-5,
		launchSpriteRowsPx: 24,
	},
	StageModuleF9S1ID: {
		ID: StageModuleF9S1ID, Name: "F9-S1", Glyph: "▲", Color: "#E8E8E8",
		Tier: "booster", dry: 25600, fuel: 411000, thrust: 7607000, isp: 282, bc: 7.4e-6,
		launchSpriteRowsPx: 20,
		canSoftLand:        true,
	},
	StageModuleF9S2ID: {
		ID: StageModuleF9S2ID, Name: "F9-S2", Glyph: "▲", Color: "#B0D8FF",
		Tier: "transfer", dry: 3900, fuel: 107500, thrust: 934000, isp: 348, bc: 5e-5,
		launchSpriteRowsPx: 8,
	},
	StageModuleLanderID: {
		ID: StageModuleLanderID, Name: "Lander", Glyph: "▼", Color: "#5FFF87",
		Tier: "payload", dry: 4000, fuel: 8000, thrust: 45000, isp: 311, bc: 0,
		launchSpriteRowsPx: 6,
		canSoftLand:        true,
	},
	StageModuleCSMID: {
		ID: StageModuleCSMID, Name: "CSM", Glyph: "◉", Color: "#C0C0FF",
		Tier: "payload", dry: 11900, fuel: 18400, thrust: 91000, isp: 314, bc: 0,
		launchSpriteRowsPx: 10,
	},
	StageModuleRCSTugID: {
		ID: StageModuleRCSTugID, Name: "RCS Tug", Glyph: "●", Color: "#FF87D7",
		Tier: "tug", dry: 200, fuel: 0, thrust: 0, isp: 0, bc: 0,
		launchSpriteRowsPx: 4,
	},
}

// StageCatalogOrder is the configurator's canonical cycle order —
// roughly bottom-of-stack (heavy boosters) to top (payload), so a
// player building a stack from scratch naturally walks the list
// adding a booster first and a payload last.
var StageCatalogOrder = []string{
	StageModuleSICID,
	StageModuleSRBID,
	StageModuleF9S1ID,
	StageModuleCoreRS25ID,
	StageModuleSIIID,
	StageModuleSIVBID,
	StageModuleF9S2ID,
	StageModuleICPSID,
	StageModuleLanderID,
	StageModuleCSMID,
	StageModuleRCSTugID,
}

// BuildStage returns a fresh, full-tank Stage for the given catalog
// ID, with the catalog's default RCS pool (same DefaultRCSLoadout
// scaling stageWithBC uses). Unknown IDs return the zero Stage and
// ok=false so callers can reject rather than silently spawn junk.
func BuildStage(id string) (Stage, bool) {
	m, present := StageCatalog[id]
	if !present {
		return Stage{}, false
	}
	mp, monoCap, rcsThrust, rcsIsp := stageRCS(m.dry)
	return Stage{
		// LoadoutID intentionally empty — a catalog part is not from
		// a Loadout. Glyph/Color/Name below keep jettison + HUD
		// rendering working without a loadout lookup.
		LoadoutID:            "",
		Name:                 m.Name,
		Glyph:                m.Glyph,
		Color:                m.Color,
		DryMass:              m.dry,
		FuelMass:             m.fuel,
		FuelCapacity:         m.fuel,
		Thrust:               m.thrust,
		Isp:                  m.isp,
		MonopropMass:         mp,
		MonopropCap:          monoCap,
		RCSThrust:            rcsThrust,
		RCSIsp:               rcsIsp,
		BallisticCoefficient: m.bc,
		LaunchSpriteRowsPx:   m.launchSpriteRowsPx,
		CanSoftLand:          m.canSoftLand,
	}, true
}
