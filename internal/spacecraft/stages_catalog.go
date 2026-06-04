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
	// as a `launchSpriteWidthPx × launchSpriteRowsPx` filled
	// rectangle of braille dots via PlotColored, anchored at the
	// stage's bottom-centre. See CONTEXT.md "Launch Sprite" for the
	// convention; row count is stylised, not real metres.
	launchSpriteRowsPx int
	// launchSpriteWidthPx (v0.11.5) is the per-stage width in
	// braille sub-pixels. Zero falls back to the renderer's
	// defaultSpriteWidthPx (2 — pre-v0.11.5 universal constant).
	// Practical range [1, 5]; stylised character, not physics.
	launchSpriteWidthPx int
	// launchSpriteColor (v0.11.5-followup) overrides Color for the
	// silhouette body / engine bell / taper / legs. Empty falls back
	// to Color. Apollo-Stack family ships unified-palette overrides
	// here so the 5-band stack doesn't read as rainbow stripes.
	launchSpriteColor string
	// fuelType (v0.11.5) selects the engine's exhaust flame colour
	// per FuelType* constants in stage.go. Empty for stages with
	// no main engine (RCS-tug); ColorWarning fallback for unset
	// values keeps pre-v0.11.5 behaviour for un-catalogued stages.
	fuelType string
	// launchSpriteHasLegs (v0.11.5) opts a stage into the Lander
	// silhouette's splayed landing-leg render. Only meaningful at
	// Stages[0] — upper-stage flag is ignored.
	launchSpriteHasLegs bool
	// canSoftLand (v0.11.4-followup) marks stages designed to
	// soft-land — populates the matching Stage.CanSoftLand flag
	// via catalogCanSoftLandByName so the surface-arrival
	// predicate gates correctly across staging. Today's true
	// entries: lander (LM-derived descent stage), f9-s1 (Falcon
	// 9 first stage with retro-burn recovery). Everything else
	// stays false — Saturn V stages crash on contact, CSM crashes
	// on contact, F9-S2 crashes on contact.
	canSoftLand bool
	// hasParachute (v0.12 Slice 3, ADR 0008) marks stages that carry a
	// recovery parachute — populates Stage.HasParachute (directly in
	// BuildStage, by-Name via catalogHasParachuteByName for the loadout
	// literals) so the surface-arrival predicate's chute route and the
	// Stage-action arm path gate correctly across staging. Today's true
	// entries: csm (Apollo Command/Service Module) and capsule (the
	// standalone re-entry test vehicle). Disjoint from canSoftLand —
	// a capsule has no engine landing route, only the chute.
	hasParachute bool
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
	StageModuleLanderID   = "lander"    // LM-derived throttleable descent (single-stage)
	// v0.12 Slice 2 / ADR 0007: the 2-stage Lander split — descent (legs,
	// soft-land, surface-stage candidate) + ascent (no legs, returns to
	// orbit). Used by the split standalone Lander loadout and the Apollo
	// Stack's LM tier. v0.13: both are now in StageCatalogOrder too, so the
	// configurator offers the descent/ascent split alongside the
	// single-piece "lander" module (a custom stack can build an Apollo-LM).
	StageModuleLanderDescentID = "lander-descent"
	StageModuleLanderAscentID  = "lander-ascent"
	StageModuleCSMID           = "csm" // Apollo Command/Service Module (SPS)
	// v0.12 / ADR 0009: the fused CSM split into a propulsive Service
	// Module (SPS engine + all propellant; does LOI/TEI) and a passive
	// Command Module (engineless parachute capsule; the surviving core).
	// Like lander-descent/ascent above, both live in the catalog map so
	// the Apollo-Stack loadout's by-Name sprite/flag lookups resolve
	// them, but are intentionally left out of StageCatalogOrder — the
	// configurator still offers the single fused "csm" module. They do
	// NOT alias to "csm": the csm entry carries hasParachute (it survives
	// to re-entry as one piece), but the split SM must NOT — only the CM
	// carries the chute.
	StageModuleServiceModuleID = "service-module"
	StageModuleCommandModuleID = "command-module"
	// v0.12 Slice 3 / ADR 0008: standalone re-entry capsule — single
	// command-module-class stage with a parachute, no engine landing.
	StageModuleCapsuleID = "capsule"
	StageModuleRCSTugID  = "rcs-tug" // pure-monoprop proximity-ops module
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
		launchSpriteRowsPx:  24,
		launchSpriteWidthPx: 5,
		// Real S-IC was matte white with black roll patterns. Warm
		// cream keeps a hint of the catalog's S-IC-orange identity so
		// adjacent stages still read as distinct bands.
		launchSpriteColor: "#F5EFE0",
		fuelType:          FuelTypeKerolox,
	},
	StageModuleSIIID: {
		ID: StageModuleSIIID, Name: "S-II", Glyph: "▲", Color: "#FFC042",
		Tier: "sustainer", dry: 40000, fuel: 440000, thrust: 5140000, isp: 421, bc: 2.5e-5,
		launchSpriteRowsPx:  20,
		launchSpriteWidthPx: 4,
		launchSpriteColor:   "#E8E8E8", // neutral pale — matches real S-II white paint
		fuelType:            FuelTypeHydrolox,
	},
	StageModuleSIVBID: {
		ID: StageModuleSIVBID, Name: "S-IVB", Glyph: "▲", Color: "#FFD93D",
		Tier: "transfer", dry: 11000, fuel: 109000, thrust: 1023000, isp: 421, bc: 6.25e-5,
		launchSpriteRowsPx:  12,
		launchSpriteWidthPx: 3,
		launchSpriteColor:   "#D8D8D8", // slightly cooler off-white capping the Saturn V trio
		fuelType:            FuelTypeHydrolox,
	},
	StageModuleICPSID: {
		ID: StageModuleICPSID, Name: "ICPS", Glyph: "◆", Color: "#5BB3FF",
		Tier: "transfer", dry: 3500, fuel: 25000, thrust: 110000, isp: 462, bc: 6.25e-5,
		launchSpriteRowsPx:  8,
		launchSpriteWidthPx: 3,
		launchSpriteColor:   "#C0C8D0", // muted cool grey — tames the saturated slate-blue Color
		fuelType:            FuelTypeHydrolox,
	},
	StageModuleSRBID: {
		ID: StageModuleSRBID, Name: "SRBs", Glyph: "▲", Color: "#E0E0E0",
		Tier: "booster", dry: 198000, fuel: 1270000, thrust: 32000000, isp: 268, bc: 8e-6,
		launchSpriteRowsPx:  28,
		launchSpriteWidthPx: 5,
		// SRB Color is already neutral grey — no override needed.
		fuelType: FuelTypeSolid,
	},
	StageModuleCoreRS25ID: {
		ID: StageModuleCoreRS25ID, Name: "Core", Glyph: "▲", Color: "#FF6B35",
		Tier: "sustainer", dry: 85275, fuel: 979452, thrust: 9290000, isp: 452, bc: 2.5e-5,
		launchSpriteRowsPx:  24,
		launchSpriteWidthPx: 4,
		// Real SLS core foam insulation is brick orange. Mute it so
		// the booster-grey + core-orange stack doesn't shout.
		launchSpriteColor: "#C45A2B",
		fuelType:          FuelTypeHydrolox,
	},
	StageModuleF9S1ID: {
		ID: StageModuleF9S1ID, Name: "F9-S1", Glyph: "▲", Color: "#E8E8E8",
		Tier: "booster", dry: 25600, fuel: 411000, thrust: 7607000, isp: 282, bc: 7.4e-6,
		launchSpriteRowsPx:  20,
		launchSpriteWidthPx: 3,
		fuelType:            FuelTypeKerolox,
		canSoftLand:         true,
	},
	StageModuleF9S2ID: {
		ID: StageModuleF9S2ID, Name: "F9-S2", Glyph: "▲", Color: "#B0D8FF",
		Tier: "transfer", dry: 3900, fuel: 107500, thrust: 934000, isp: 348, bc: 5e-5,
		launchSpriteRowsPx:  8,
		launchSpriteWidthPx: 3,
		fuelType:            FuelTypeKerolox,
	},
	StageModuleLanderID: {
		ID: StageModuleLanderID, Name: "Lander", Glyph: "▼", Color: "#5FFF87",
		Tier: "payload", dry: 4000, fuel: 8000, thrust: 45000, isp: 311, bc: 0,
		launchSpriteRowsPx:  5,
		launchSpriteWidthPx: 3,
		// Real LM descent stage was wrapped in gold foil over an
		// aluminium frame. Muted gold reads as "metal hardware"
		// alongside the Saturn V whites instead of mint-green.
		launchSpriteColor:   "#D4C088",
		fuelType:            FuelTypeHypergolic,
		launchSpriteHasLegs: true,
		canSoftLand:         true,
	},
	// Lander descent stage (v0.12 Slice 2 / ADR 0007): the bottom half
	// of the 2-stage Lander — keeps the v0.11.5 Lander silhouette
	// (squat body, splayed legs, hypergolic flame) and the soft-land
	// qualification. Fuel-heavy like the real LM descent stage: it
	// fires the entire powered descent hauling the ascent stage as
	// dead-weight payload, so it needs the lion's share of propellant.
	// With dry 2500 / fuel 9500 the descent-burn Δv (full stack) is
	// ~3.0 km/s — comfortably more than a lunar descent (the original
	// 6000 kg gave only ~2.1 km/s and ran dry mid-landing). Thrust
	// stays 45 kN (the original single-Lander descent engine).
	StageModuleLanderDescentID: {
		ID: StageModuleLanderDescentID, Name: "Descent", Glyph: "▼", Color: "#5FFF87",
		Tier: "payload", dry: 2500, fuel: 9500, thrust: 45000, isp: 311, bc: 0,
		launchSpriteRowsPx:  5,
		launchSpriteWidthPx: 3,
		launchSpriteColor:   "#D4C088", // muted gold foil — matches single Lander
		fuelType:            FuelTypeHypergolic,
		launchSpriteHasLegs: true,
		canSoftLand:         true,
	},
	// Lander ascent stage (v0.12 Slice 2 / ADR 0007): the top half —
	// smaller, no legs (they stayed on the descent stage), its own
	// hypergolic engine sized for the lunar-ascent-to-orbit Δv (~2.8
	// km/s with dry 1200 / fuel 1800). Carries canSoftLand=true anyway
	// (a forgiving sandbox choice — a player who flies the bare ascent
	// stage back down soft-lands rather than crashes; see ADR 0007
	// decision 5).
	StageModuleLanderAscentID: {
		ID: StageModuleLanderAscentID, Name: "Ascent", Glyph: "▲", Color: "#7BFFA0",
		Tier: "payload", dry: 1200, fuel: 1800, thrust: 16000, isp: 311, bc: 0,
		launchSpriteRowsPx:  3,
		launchSpriteWidthPx: 2,
		launchSpriteColor:   "#C8C8B0", // pale metal — distinct band above the gold descent
		fuelType:            FuelTypeHypergolic,
		canSoftLand:         true,
	},
	StageModuleCSMID: {
		ID: StageModuleCSMID, Name: "CSM", Glyph: "◉", Color: "#C0C0FF",
		Tier: "payload", dry: 11900, fuel: 18400, thrust: 91000, isp: 314, bc: 0,
		launchSpriteRowsPx:  10,
		launchSpriteWidthPx: 2,
		// CSM Service Module was bare aluminium — silver-white.
		launchSpriteColor:   "#C8C8D0",
		fuelType:            FuelTypeHypergolic,
		// v0.12 Slice 3 (ADR 0008): the CSM survives the Apollo decouple
		// chain to re-entry and earns an Earth splashdown under chute.
		hasParachute: true,
	},
	// v0.12 / ADR 0009: Service Module — the propulsive half of the split
	// CSM. Carries the SPS engine + all the storable propellant and does
	// LOI / mid-course corrections / TEI. Dry ~6,000 kg; SPS fuel trimmed
	// 18,400→16,000 (ADR 0009 locked table). NO parachute — it is
	// jettisoned before re-entry. Sprite mirrors the CSM service-module
	// silhouette (silver, slim) so the post-transposition Stages[0]=SM
	// renders an engine bell.
	StageModuleServiceModuleID: {
		ID: StageModuleServiceModuleID, Name: "SM", Glyph: "◉", Color: "#C8C8D0",
		Tier: "payload", dry: 6000, fuel: 16000, thrust: 91000, isp: 314, bc: 0,
		launchSpriteRowsPx:  6,
		launchSpriteWidthPx: 2,
		launchSpriteColor:   "#C8C8D0", // bare aluminium service module
		fuelType:            FuelTypeHypergolic,
	},
	// v0.12 / ADR 0009: Command Module — the passive half of the split
	// CSM and the true surviving core. Engineless crew capsule with a
	// recovery parachute (ADR 0008 model); the only piece that splashes
	// down. Dry ~5,900 kg (CSM dry 11,900 − SM 6,000). No main engine.
	StageModuleCommandModuleID: {
		ID: StageModuleCommandModuleID, Name: "CM", Glyph: "◓", Color: "#B8C8E0",
		Tier: "payload", dry: 5900, fuel: 0, thrust: 0, isp: 0, bc: 0,
		launchSpriteRowsPx:  6,
		launchSpriteWidthPx: 3,
		launchSpriteColor:   "#D8D8E0", // pale command-module cone (distinct from HUD #B8C8E0)
		// fuelType intentionally unset — no main engine (RCS-only).
		hasParachute: true,
	},
	// Re-entry capsule (v0.12 Slice 3, ADR 0008): a minimal command-
	// module-class stage carrying a parachute and NO engine landing
	// capability — the clean, directly-spawnable test vehicle for the
	// chute subsystem (one spawn, a de-orbit, a `space` press). Sized
	// roughly like an Apollo Command Module alone (the CSM minus the
	// Service Module): ~5,800 kg dry, a small RCS-only attitude budget,
	// no main engine. bc 0 → the stowed/armed BC falls back to the
	// default; only the deployed chute's ChuteDeployedBC matters for its
	// descent.
	StageModuleCapsuleID: {
		ID: StageModuleCapsuleID, Name: "Capsule", Glyph: "◓", Color: "#B8C8E0",
		Tier: "payload", dry: 5800, fuel: 0, thrust: 0, isp: 0, bc: 0,
		launchSpriteRowsPx:  6,
		launchSpriteWidthPx: 3,
		launchSpriteColor:   "#C8C8D0", // bare-metal command module
		// fuelType intentionally unset — no main engine (RCS-only).
		hasParachute: true,
	},
	StageModuleRCSTugID: {
		ID: StageModuleRCSTugID, Name: "RCS Tug", Glyph: "●", Color: "#FF87D7",
		Tier: "tug", dry: 200, fuel: 0, thrust: 0, isp: 0, bc: 0,
		launchSpriteRowsPx:  4,
		launchSpriteWidthPx: 2,
		// fuelType intentionally unset — pure monoprop, no main engine.
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
	// v0.13: the 2-stage Lander split (descent + ascent) is now
	// configurator-pickable too, so a custom stack can build an
	// Apollo-style LM — Descent below Ascent. Flown as a [Descent,
	// Ascent] pair the single-pop chain lands on the descent, drops it
	// (legs + soft-land ride per-Stage), and flies the ascent back to
	// orbit, exactly like the standalone "Lander" loadout. They sit
	// next to the single-piece lander, before the CSM payload.
	StageModuleLanderDescentID,
	StageModuleLanderAscentID,
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
		LaunchSpriteWidthPx:  m.launchSpriteWidthPx,
		LaunchSpriteColor:    m.launchSpriteColor,
		FuelType:             m.fuelType,
		LaunchSpriteHasLegs:  m.launchSpriteHasLegs,
		CanSoftLand:          m.canSoftLand,
		HasParachute:         m.hasParachute,
	}, true
}
