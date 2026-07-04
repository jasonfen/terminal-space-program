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
	// (carried on the Part since v0.23/ADR 0026; previously copied
	// onto loadout stages by Name) so the surface-arrival predicate
	// gates correctly across staging. Today's true entries: lander
	// (LM-derived descent stage), f9-s1 (Falcon 9 first stage with
	// retro-burn recovery). Everything else stays false — Saturn V
	// stages crash on contact, CSM crashes on contact, F9-S2 crashes
	// on contact.
	canSoftLand bool
	// hasParachute (v0.12 Slice 3, ADR 0008) marks stages that carry a
	// recovery parachute — populates Stage.HasParachute (carried on the
	// Part since v0.23/ADR 0026, for both configurator parts and the
	// loadouts that reference them) so the surface-arrival predicate's
	// chute route and the Stage-action arm path gate correctly across
	// staging. Today's true entries: csm (Apollo Command/Service Module)
	// and capsule (the standalone re-entry test vehicle). Disjoint from
	// canSoftLand — a capsule has no engine landing route, only the chute.
	hasParachute bool
	// commandSource / antenna (v0.23 / ADR 0027): comms part attributes,
	// carried onto the built Stage by BuildStage so a configurator-picked
	// part contributes its connectivity role to a custom stack, exactly as
	// a loadout-referenced part does via Part.ToStage.
	commandSource string
	antennaKind   string
	antennaRangeM float64
	// VabSeed (v0.25 / ADR 0032 §6) is the part's optional crack-open seed
	// — the component-ID list the VAB expands this atomic block into when the
	// player presses enter on its stage header. Carried through from
	// Part.VabSeed; nil for parts with no authored decomposition. Never
	// touches the built Stage (BuildStage ignores it) — it is a VAB-editing
	// convenience only.
	VabSeed []string
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
	// Stack's LM tier. v0.13: BuildModule expands the configurator's single
	// "lander" pick into this [Descent, Ascent] pair, so a custom stack adds
	// the LM as one vessel rather than as two separate parts.
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
	// StageModuleApolloCSMLMID (v0.14 / ADR 0011) is a COMPOSITE module
	// pick: BuildModule expands it to [SM, CM, Descent, Ascent] and
	// ModuleNosePayloadTop reports that the top 2 (the LM) form a docked
	// nose payload. Picking it in the configurator and spawning lands the
	// post-transposition Apollo composite — SM firing core, LM an
	// Undock-able nose payload — already assembled, no flip to fly.
	StageModuleApolloCSMLMID = "csm-lm"
	// v0.12 Slice 3 / ADR 0008: standalone re-entry capsule — single
	// command-module-class stage with a parachute, no engine landing.
	StageModuleCapsuleID = "capsule"
	StageModuleRCSTugID  = "rcs-tug" // pure-monoprop proximity-ops module
)

// ADR-0009 trimmed Apollo LM propellant (kg). The csm-lm composite is
// the post-transposition Apollo stack, so its LM carries these — the
// same values the Apollo-Stack loadout uses (loadouts.go) — rather than
// the untrimmed standalone Lander modules. Kept in sync with the
// loadout by TestApolloCSMLMCompositeMatchesLoadoutLM (GH #89).
const (
	apolloLMDescentFuel = 6310.0 // descent 9500 → 6310 (real abort reserve, ~2500 m/s cap)
	apolloLMAscentFuel  = 1269.0 // ascent  1800 → 1269 (~2200 m/s cap)
)

// StageCatalog indexes the parts library by ID. v0.23 / ADR 0026 (C1-2):
// the data now lives in the embedded data/parts.json (loaded at package
// init via buildStageCatalog) rather than a hardcoded Go literal — the
// first cut toward the normalized, modder-overridable parts catalog. The
// in-memory StageModule shape and every reader (BuildStage / BuildModule /
// the catalog*ByName loadout helpers) are unchanged: the migration moved
// the *source* of the data, not its representation or behaviour, so the
// catalog flies byte-identical (golden-tested). The numbers mirror the
// inline loadout literals in loadouts.go (see file-level note).
//
// Embedded-catalog load is deliberately fatal: a malformed shipped
// parts.json is a build/programmer error, not a recoverable runtime
// condition (mirrors regexp.MustCompile). The *user overlay* path —
// skip-bad-with-warning — is wired in at a higher layer (C1-4), not here,
// so BuildStage stays I/O-free and the golden tests stay deterministic.
var StageCatalog = buildStageCatalog()

// buildStageCatalog loads the embedded parts catalog and indexes it by ID
// as StageModules. Runs once at package-var init. Panics if the embedded
// data fails to load (it must always load — see StageCatalog).
func buildStageCatalog() map[string]StageModule {
	comps, parts, _, err := loadEmbeddedCatalog()
	if err != nil {
		panic("spacecraft: embedded parts catalog failed to load: " + err.Error())
	}
	// Aggregate composed parts (ADR 0029) so a composed catalog part would
	// project its derived stats. The embedded catalog is all-atomic this
	// cycle (pass-through); a bad embedded composition is a build error.
	parts, aggWarnings := aggregateComponents(parts, comps)
	if len(aggWarnings) > 0 {
		panic("spacecraft: embedded composed part failed aggregation: " + aggWarnings[0].Error())
	}
	out := make(map[string]StageModule, len(parts))
	for id, p := range parts {
		out[id] = p.toStageModule()
	}
	return out
}

// toStageModule projects a data-driven Part onto the in-memory StageModule
// the stage catalog and BuildStage operate on. The RCS pool is NOT carried
// here — it is derived from dry mass at BuildStage time (the catalog
// convention, via DefaultRCSLoadout), exactly as before the migration.
func (p Part) toStageModule() StageModule {
	return StageModule{
		ID:                  p.ID,
		Name:                p.Name,
		Glyph:               p.Glyph,
		Color:               p.Color,
		Tier:                p.Tier,
		dry:                 p.DryMassKg,
		fuel:                p.FuelMassKg,
		thrust:              p.ThrustN,
		isp:                 p.IspS,
		bc:                  p.BallisticCoefficient,
		launchSpriteRowsPx:  p.LaunchSpriteRowsPx,
		launchSpriteWidthPx: p.LaunchSpriteWidthPx,
		launchSpriteColor:   p.LaunchSpriteColor,
		fuelType:            p.FuelType,
		launchSpriteHasLegs: p.LaunchSpriteHasLegs,
		canSoftLand:         p.CanSoftLand,
		hasParachute:        p.HasParachute,
		commandSource:       p.CommandSource,
		antennaKind:         antennaKindOf(p),
		antennaRangeM:       antennaRangeOf(p),
		VabSeed:             p.VabSeed,
	}
}

// antennaKindOf / antennaRangeOf read a Part's optional antenna safely.
func antennaKindOf(p Part) string {
	if p.Antenna == nil {
		return AntennaNone
	}
	return p.Antenna.Kind
}

func antennaRangeOf(p Part) float64 {
	if p.Antenna == nil {
		return 0
	}
	return p.Antenna.RangeM
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
	// The "lander" pick is the 2-stage Apollo-LM as one vessel: BuildModule
	// expands it to [Descent, Ascent] so the player adds a separable lander
	// in a single pick (one vessel, two internal stages — land on the
	// descent, drop it, fly the ascent), exactly like the standalone
	// "Lander" loadout. The descent/ascent are NOT separate picker entries.
	StageModuleLanderID,
	StageModuleCSMID,
	// The "csm-lm" pick is a composite: BuildModule expands it to
	// [SM, CM, Descent, Ascent] and ModuleNosePayloadTop marks the top 2
	// (the LM) as a docked nose payload, so spawning it lands the
	// post-transposition Apollo composite already assembled (ADR 0011).
	StageModuleApolloCSMLMID,
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
		CommandSource:        m.commandSource,
		AntennaKind:          m.antennaKind,
		AntennaRangeM:        m.antennaRangeM,
	}, true
}

// BuildModule returns the stage(s) a single configurator pick contributes
// to a custom stack, bottom-first. Most catalog ids map to exactly one
// stage; the "lander" id expands to the 2-stage Apollo-LM — Descent
// (bottom: legs + soft-land + powered-descent engine) + Ascent (top:
// returns to orbit) — so the configurator adds the lander as one vessel,
// the way the standalone Lander loadout ships it, rather than as two parts
// the player must stack in the right order. Unknown ids return ok=false
// (mirrors BuildStage).
func BuildModule(id string) ([]Stage, bool) {
	if id == StageModuleLanderID {
		d, okD := BuildStage(StageModuleLanderDescentID)
		a, okA := BuildStage(StageModuleLanderAscentID)
		if !okD || !okA {
			return nil, false
		}
		return []Stage{d, a}, true
	}
	if id == StageModuleApolloCSMLMID {
		// v0.14 / ADR 0011: the post-transposition Apollo composite as one
		// pick — the [SM, CM] core (SM firing the SPS) with the [Descent,
		// Ascent] LM stacked on top. ModuleNosePayloadTop reports the top 2
		// so the spawn path docks the LM as a nose payload rather than
		// stacking it linearly.
		sm, okSM := BuildStage(StageModuleServiceModuleID)
		cm, okCM := BuildStage(StageModuleCommandModuleID)
		d, okD := BuildStage(StageModuleLanderDescentID)
		a, okA := BuildStage(StageModuleLanderAscentID)
		if !okSM || !okCM || !okD || !okA {
			return nil, false
		}
		// GH #89: this composite IS the post-transposition Apollo stack,
		// so its LM must carry the ADR-0009 *trimmed* propellant — the
		// same as the Apollo-Stack loadout — not the untrimmed shared
		// Lander modules. Post-transposition the LM no longer doubles as
		// the LOI engine (the SM does), so it sheds the surplus the
		// standalone Lander keeps. Reusing the untrimmed descent/ascent
		// gave this composite ~0.9 km/s more descent Δv than the loadout
		// for the same mission. The trim is fuel-only (sprite, legs,
		// soft-land, engine all shared), so override here rather than
		// duplicating the catalog entries.
		d.FuelMass, d.FuelCapacity = apolloLMDescentFuel, apolloLMDescentFuel
		a.FuelMass, a.FuelCapacity = apolloLMAscentFuel, apolloLMAscentFuel
		return []Stage{sm, cm, d, a}, true
	}
	st, ok := BuildStage(id)
	if !ok {
		return nil, false
	}
	return []Stage{st}, true
}

// ModuleNosePayloadTop reports how many of the TOP stages that
// BuildModule(id) produces form a docked nose payload — released by
// Undock, not Staging (the top-release counterpart to a Loadout's
// bottom-up DecouplePlan; ADR 0011). Non-composite modules return 0, so
// the configurator stacks them linearly. The "csm-lm" composite returns
// 2 (the LM = Descent + Ascent rides on the [SM, CM] core's nose).
// v0.14.
func ModuleNosePayloadTop(id string) int {
	if id == StageModuleApolloCSMLMID {
		return 2
	}
	return 0
}
