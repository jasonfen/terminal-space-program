package spacecraft

import "github.com/jasonfen/terminal-space-program/internal/bodies"

// Loadout describes a named craft archetype — propulsion numbers,
// dry/wet mass sizing, default RCS pool, and visual differentiation
// (glyph + color). v0.8.2 ship set + v0.9.1 Saturn-V multi-stage +
// v0.9.4 SLS / Falcon 9:
//
//   - S-IVB-1:    J-2-powered third stage. The v0.5.13+ default.
//   - ICPS:       RL-10-powered low-TWR transfer stage. Returns from
//                 v0.5.6 — long burns, less mass.
//   - RCS-tug:    Pure-monoprop proximity-ops vehicle. No main engine;
//                 navigates entirely on RCS. For docking maneuvers.
//   - Lander:     Throttleable descent-stage profile (LM-derived). Lower
//                 thrust, lower Isp, sized for surface maneuvering.
//   - Saturn-V:   3-stage Apollo launch vehicle (S-IC / S-II / S-IVB).
//                 v0.9.1+. TWR > 1 at sea level on stage 1.
//   - SLS-Block1: 3-stage NASA heavy-lift (SRBs / Core / ICPS). v0.9.4+.
//                 SRBs and core fire in parallel in real life; we
//                 approximate as sequential.
//   - Falcon-9:   2-stage SpaceX LV (Merlin 1D × 9 / Merlin Vacuum).
//                 v0.9.4+. Smaller stack, higher lift-off TWR.
//   - Apollo-Stack: Saturn-V launch chain + LM + CSM payload, 5
//                 stages. v0.10.1+. Mid-stage Lander decouples to a
//                 controllable craft (payload separation); CSM is
//                 the surviving core.
//
// Future loadouts land alongside this catalog and are referenced
// from Spacecraft.LoadoutID — a string lookup keeps the on-disk
// format human-editable and lets future modding overlays add craft
// types without code changes.
//
// v0.9.1+: Stages is the source of truth. Single-stage loadouts
// declare one entry; multi-stage (Saturn-V) declares the chain
// bottom-first (Stages[0] = S-IC booster, fires first; Stages[2] =
// S-IVB, fires last). The legacy flat fields (DryMass / Fuel / Isp
// / Thrust) are derived from Stages[0] for back-compat with pre-
// v0.9.1 readers — for single-stage loadouts they match the stage
// exactly; for multi-stage they reflect the bottom (firing) stage.
type Loadout struct {
	ID    string
	Name  string
	Role  string
	Glyph string
	Color string
	// Stages is the per-stage breakdown, bottom-first. Required
	// (must be non-empty); a one-element Stages declares a
	// single-stage craft.
	Stages []Stage
	// DecouplePlan (v0.12 Slice 2 / ADR 0007) is an optional
	// bottom-up list of staging group sizes — how many contiguous
	// bottom Stages each staging press releases as a single craft.
	// Nil ⇒ all-ones (one Stage per press, the historical default).
	// The Apollo Stack declares [1,1,1,2] so the descent + ascent LM
	// pair extracts together as a 2-stage craft (see ADR 0007). Sum
	// of the plan should be < len(Stages) (the top stage is the
	// surviving core). Copied onto the Spacecraft in NewFromLoadout.
	DecouplePlan []int
	// SlewRateDegPerSec (v0.10.0+) is the per-loadout attitude
	// angular-rate cap (deg/s, sim-time). Zero => the global
	// DefaultSlewRateDegPerSec. Loadout-level (not per-stage):
	// staging does not change the slew rate this cycle (attitude
	// dynamics are deferred). All catalog literals leave this unset
	// in v0.10.0; per-vehicle tuning is a follow-up dial.
	SlewRateDegPerSec float64

	// ScaleClass is the loadout's spawn-form scale hint (ADR 0014),
	// shared with bodies.System. Optional: an unset value normalizes
	// to bodies.ScaleReal via Scale(), so the existing real fleet needs
	// no per-literal change. The scale-matched Kern Stack sets
	// bodies.ScaleStrippedBack. Never used to filter craft by System —
	// any Loadout can be spawned in any System.
	ScaleClass bodies.ScaleClass
}

// DefaultSlewRateDegPerSec is the attitude slew-rate cap applied to
// any loadout that does not override it (Loadout.SlewRateDegPerSec
// == 0) and to legacy/test craft built without a loadout. 15°/s ≈ 12 s
// for a 180° flip — visible and deliberate, but snappy enough not to
// be tedious (raised from 5°/s after v0.10.0 playtest). Cosine loss
// is still a real consequence the player times burns around. v0.10.0+.
const DefaultSlewRateDegPerSec = 15.0

// Scale returns the loadout's normalized ScaleClass (empty =>
// bodies.ScaleReal). The spawn form compares this against the target
// System's Scale() to surface the Δv-to-orbit / "best for" hint; it is
// never used to filter the craft list (ADR 0014).
func (l Loadout) Scale() bodies.ScaleClass {
	return l.ScaleClass.Normalize()
}

// DryMass returns the bottom stage's dry mass (single-stage
// equivalent for pre-v0.9.1 readers; sum-across-stages is via
// SumDryMass(l.Stages) when the caller wants it).
func (l Loadout) DryMass() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].DryMass
}

// Fuel returns the bottom stage's fuel mass — same convention.
func (l Loadout) Fuel() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].FuelMass
}

// Isp returns the bottom stage's main-engine specific impulse.
func (l Loadout) Isp() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].Isp
}

// Thrust returns the bottom stage's main-engine thrust.
func (l Loadout) Thrust() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].Thrust
}

// LoadoutS_IVB1 is the v0.5.13+ Apollo S-IVB / J-2 default.
const LoadoutSIVB1ID = "S-IVB-1"

// LoadoutICPS is the v0.5.6 RL-10 low-TWR transfer stage.
const LoadoutICPSID = "ICPS"

// LoadoutRCSTug is the pure-monoprop proximity-ops vehicle.
const LoadoutRCSTugID = "RCS-tug"

// LoadoutLander is the throttleable descent-stage profile.
const LoadoutLanderID = "Lander"

// LoadoutSaturnV is the 3-stage Apollo launch vehicle. v0.9.1+.
const LoadoutSaturnVID = "Saturn-V"

// LoadoutSLSBlock1 is the 3-stage NASA Space Launch System Block 1.
// v0.9.4+. Twin 5-segment solid boosters (SRBs) → 4× RS-25 core stage
// → ICPS. Same low-TWR upper-stage shape as Saturn V (Core TWR ~0.87
// after SRB sep), so gameplay translates: continuous-burn ascent,
// no coast-and-circularize.
const LoadoutSLSBlock1ID = "SLS-Block1"

// LoadoutFalcon9 is the 2-stage SpaceX Falcon 9 Block 5. v0.9.4+.
// 9× Merlin 1D first stage → 1× Merlin Vacuum second stage. Higher
// TWR than the heavy-lift options (~1.4 at lift-off) and a smaller
// stack — handles like a sport rocket compared to the Saturn V /
// SLS heavies.
const LoadoutFalcon9ID = "Falcon-9"

// LoadoutApolloStack is the v0.10.1+ full Apollo mission stack:
// Saturn-V launch chain with a Lunar Module + Command/Service
// Module payload on top. Stages bottom-first =
// [S-IC, S-II, S-IVB, Lander, CSM]. The decouple sequence is the
// real mission arc on top of the v0.9.1 staging machinery:
// drop S-IC → S-II → S-IVB (after the TLI burn) → Lander; the
// Lander spawns as its own controllable slate craft (payload
// separation) and the CSM is left as the player's surviving core
// to fly the rendezvous / return. The first three stages reuse the
// canonical Saturn-V tuning so ascent flies identically.
const LoadoutApolloStackID = "Apollo-Stack"

// LoadoutCapsule is the v0.12 Slice 3 (ADR 0008) standalone re-entry
// capsule: a single command-module-class stage with a recovery
// parachute and no engine landing capability. The clean, directly-
// spawnable test vehicle for the chute subsystem — spawn it, de-orbit,
// `space` to arm, watch it auto-deploy and splash down under V_CRIT
// without crashing. The CSM is only reachable via the full Apollo
// Stack → orbit → four decouples, too slow to iterate on.
const LoadoutCapsuleID = "Capsule"

// LoadoutKernStack is the v0.15 / ADR 0014 scale-matched vehicle for
// the stripped-back Lumen system: a simplified 4-stage Apollo analog —
// Boost → Transfer → single Lander → parachute Pod, bottom-first —
// sized to a ~6 km/s Cursor-landing-and-return budget on Lumen's
// ~1/10-linear scale (~3.4 km/s to Kern orbit vs Sol's ~9.4). It is the
// only Loadout tagged bodies.ScaleStrippedBack, so the spawn form's
// Δv-to-orbit hint flags it as best-for-Lumen — but, per ADR 0014, the
// tag never filters: the Kern Stack can still be spawned in Sol (where
// it can't reach orbit) and the real fleet in Lumen. DecouplePlan
// [1,1,1] drops Boost, Transfer and Lander one at a time; the engineless
// parachute Pod is the surviving core that splashes down (ADR 0008
// recovery model). Engine Isp values are KSP stock numbers collapsed to
// the model's single-Isp-per-stage convention (no altitude-varying Isp):
// Mainsail (Boost, sea-level 285), Poodle (Transfer, vac 350), Terrier
// (Lander, vac 345).
const LoadoutKernStackID = "Kern-Stack"

// stageRCS builds the per-stage RCS pool for a stage of the given
// dry mass via DefaultRCSLoadout — same scaling that single-stage
// craft used pre-v0.9.1, so existing loadouts inherit identical
// RCS budgets when wrapped into Stages: [{...}].
func stageRCS(dryMass float64) (mp, monoCap, rcsThrust, rcsIsp float64) {
	return DefaultRCSLoadout(dryMass)
}

// stage builds a single Stage with full tanks + the catalog's
// default RCS pool. Used by the Loadouts map literals to keep the
// per-stage structure terse. BallisticCoefficient stays 0 →
// EffectiveBallisticCoefficient falls back to the spacecraft-level
// default (0.01 m²/kg, the v0.8.4 LEO baseline). For multi-stage
// loadouts that fly through atmosphere (Saturn-V S-IC at sea level)
// use stageWithBC instead so per-stage drag is realistic.
func stage(loadoutID, name, glyph, color string, dry, fuel, thrust, isp float64) Stage {
	return stageWithBC(loadoutID, name, glyph, color, dry, fuel, thrust, isp, 0)
}

// stageWithBC adds an explicit BallisticCoefficient (m²/kg) for
// stages that fly through atmosphere. v0.9.2.1+. BC = C_D · A / m;
// for a Saturn-V S-IC booster (~80 m² cross-section, C_D ~0.3,
// ~2.9 Mkg wet) ≈ 8e-6 m²/kg. Pre-v0.9.2.1 the default 0.01 was
// 1250× too high, making sea-level drag dominate the launch.
func stageWithBC(loadoutID, name, glyph, color string, dry, fuel, thrust, isp, bc float64) Stage {
	mp, monoCap, rcsThrust, rcsIsp := stageRCS(dry)
	return Stage{
		LoadoutID:            loadoutID,
		Name:                 name,
		Glyph:                glyph,
		Color:                color,
		DryMass:              dry,
		FuelMass:             fuel,
		FuelCapacity:         fuel,
		Thrust:               thrust,
		Isp:                  isp,
		MonopropMass:         mp,
		MonopropCap:          monoCap,
		RCSThrust:            rcsThrust,
		RCSIsp:               rcsIsp,
		BallisticCoefficient: bc,
		LaunchSpriteRowsPx:   catalogLaunchSpriteRowsPxByName(name),
		LaunchSpriteWidthPx:  catalogLaunchSpriteWidthPxByName(name),
		LaunchSpriteColor:    catalogLaunchSpriteColorByName(name),
		FuelType:             catalogFuelTypeByName(name),
		LaunchSpriteHasLegs:  catalogLaunchSpriteHasLegsByName(name),
		CanSoftLand:          catalogCanSoftLandByName(name),
		HasParachute:         catalogHasParachuteByName(name),
	}
}

// kernStage builds a Kern Stack stage. The Kern Stack's stage names
// (Boost / Transfer / Pod) are terminal-native and deliberately absent
// from the StageCatalog (the catalog holds the real fleet's parts), so
// the by-Name sprite/flag lookups in stage() return zero for them. This
// helper layers the launch-sprite silhouette, fuel type, and the
// soft-land / parachute capability flags on directly, keeping the Kern
// Stack self-contained in loadouts.go (ADR 0014 ships no new catalog
// parts). "Lander" does resolve in the catalog, but we set it here too so
// all four stages read from one place.
func kernStage(name, glyph, color string, dry, fuel, thrust, isp float64,
	spriteRows, spriteWidth int, spriteColor, fuelType string,
	hasLegs, canSoftLand, hasParachute bool) Stage {
	s := stage(LoadoutKernStackID, name, glyph, color, dry, fuel, thrust, isp)
	s.LaunchSpriteRowsPx = spriteRows
	s.LaunchSpriteWidthPx = spriteWidth
	s.LaunchSpriteColor = spriteColor
	s.FuelType = fuelType
	s.LaunchSpriteHasLegs = hasLegs
	s.CanSoftLand = canSoftLand
	s.HasParachute = hasParachute
	return s
}

// catalogHasParachuteByName looks up the StageCatalog's hasParachute
// flag for a loadout stage by its Name field. Mirrors
// catalogCanSoftLandByName (same LM→Lander alias for the Apollo-Stack
// literal). v0.12 Slice 3 (ADR 0008): the Apollo-Stack's "CSM" literal
// resolves against the catalog's "CSM" entry and so inherits its
// parachute capability.
func catalogHasParachuteByName(name string) bool {
	if name == "LM" {
		name = "Lander"
	}
	for _, m := range StageCatalog {
		if m.Name == name {
			return m.hasParachute
		}
	}
	return false
}

// catalogLaunchSpriteRowsPxByName returns the StageCatalog
// launchSpriteRowsPx that matches a loadout stage's Name. Used by
// stageWithBC to populate LaunchSpriteRowsPx on loadout literals so
// canonical Saturn-V / SLS / Falcon-9 / Apollo-Stack spawns render
// the composed stack via the same data as configurator-built stacks
// (v0.11.3 Slice 4; pivoted from ASCII to braille after the v0.11.3
// playtest).
//
// Special case: the Apollo-Stack's "LM" stage maps to catalog ID
// "lander" (Name = "Lander") — same physics, different loadout label.
// catalogCanSoftLandByName looks up the StageCatalog's
// canSoftLand flag for a stage by its Name field. Mirrors
// catalogLaunchSpriteRowsPxByName — same LM→Lander alias for the
// Apollo-Stack literal that uses "LM" as the Name but resolves
// against the catalog's "Lander" entry. v0.11.4-followup.
func catalogCanSoftLandByName(name string) bool {
	if name == "LM" {
		name = "Lander"
	}
	for _, m := range StageCatalog {
		if m.Name == name {
			return m.canSoftLand
		}
	}
	return false
}

func catalogLaunchSpriteRowsPxByName(name string) int {
	if name == "LM" {
		name = "Lander"
	}
	for _, m := range StageCatalog {
		if m.Name == name {
			return m.launchSpriteRowsPx
		}
	}
	return 0
}

// catalogLaunchSpriteWidthPxByName mirrors catalogLaunchSpriteRowsPxByName
// for the per-stage width — same LM→Lander alias. v0.11.5.
func catalogLaunchSpriteWidthPxByName(name string) int {
	if name == "LM" {
		name = "Lander"
	}
	for _, m := range StageCatalog {
		if m.Name == name {
			return m.launchSpriteWidthPx
		}
	}
	return 0
}

// catalogFuelTypeByName looks up the StageCatalog fuelType for a
// loadout stage by Name (LM→Lander alias). v0.11.5.
func catalogFuelTypeByName(name string) string {
	if name == "LM" {
		name = "Lander"
	}
	for _, m := range StageCatalog {
		if m.Name == name {
			return m.fuelType
		}
	}
	return ""
}

// catalogLaunchSpriteColorByName looks up the StageCatalog
// launchSpriteColor for a loadout stage by Name (LM→Lander alias).
// v0.11.5-followup.
func catalogLaunchSpriteColorByName(name string) string {
	if name == "LM" {
		name = "Lander"
	}
	for _, m := range StageCatalog {
		if m.Name == name {
			return m.launchSpriteColor
		}
	}
	return ""
}

// catalogLaunchSpriteHasLegsByName looks up the StageCatalog
// launchSpriteHasLegs flag for a loadout stage by Name
// (LM→Lander alias). v0.11.5.
func catalogLaunchSpriteHasLegsByName(name string) bool {
	if name == "LM" {
		name = "Lander"
	}
	for _, m := range StageCatalog {
		if m.Name == name {
			return m.launchSpriteHasLegs
		}
	}
	return false
}

// Loadouts indexes the v0.8.2 launch set + v0.9.1 Saturn-V by ID.
// Future patches may load user overlays (similar to bodies/themes)
// but the embedded catalog is canonical for now.
//
// v0.9.1+ shape: every loadout lists Stages bottom-first. Single-
// stage loadouts (S-IVB-1, ICPS, RCS-tug, Lander) list one entry —
// same physics as pre-v0.9.1. Saturn-V lists three entries: S-IC
// booster (fires first, jettisons first), S-II sustainer, S-IVB
// insertion (the core / payload).
var Loadouts = map[string]Loadout{
	LoadoutSIVB1ID: {
		ID:    LoadoutSIVB1ID,
		Name:  "S-IVB",
		Role:  "transfer-stage",
		Glyph: "▲",
		Color: "#FFD93D", // saturated yellow (matches v0.7.1+ ColorCraftMarker)
		Stages: []Stage{
			stage(LoadoutSIVB1ID, "S-IVB", "▲", "#FFD93D", 11000, 40000, 1023000, 421),
		},
	},
	LoadoutICPSID: {
		ID:    LoadoutICPSID,
		Name:  "ICPS",
		Role:  "transfer-stage",
		Glyph: "◆",
		Color: "#5BB3FF", // ocean blue
		Stages: []Stage{
			stage(LoadoutICPSID, "ICPS", "◆", "#5BB3FF", 3500, 25000, 108000, 462),
		},
	},
	LoadoutRCSTugID: {
		ID:    LoadoutRCSTugID,
		Name:  "RCS Tug",
		Role:  "tug",
		Glyph: "●",
		Color: "#FF87D7", // pink
		Stages: []Stage{
			stage(LoadoutRCSTugID, "RCS Tug", "●", "#FF87D7", 200, 0, 0, 0),
		},
	},
	LoadoutLanderID: {
		ID:    LoadoutLanderID,
		Name:  "Lander",
		Role:  "lander",
		Glyph: "▼",
		Color: "#5FFF87", // mint
		// v0.12 Slice 2 / ADR 0007: the Apollo-LM-style Lander is two
		// stages — Descent (bottom: legs + soft-land + the powered-
		// descent engine) and Ascent (top: returns to orbit). Surface-
		// staging the descent on the ground leaves it as a Landed
		// passive wreck and flies the ascent back up. Fuel-heavy
		// descent (dry 2500 / fuel 9500 → ~3.0 km/s descent-burn Δv
		// hauling the ascent) so a lunar landing doesn't run dry; the
		// ascent (dry 1200 / fuel 1800 → ~2.8 km/s) returns to orbit.
		// No DecouplePlan: the default single-pop drops Descent and
		// leaves Ascent as the core. CanSoftLand + landing-leg
		// silhouette ride per-Stage via the StageCatalog by-Name
		// lookups (Descent / Ascent entries).
		Stages: []Stage{
			stage(LoadoutLanderID, "Descent", "▼", "#5FFF87", 2500, 9500, 45000, 311),
			stage(LoadoutLanderID, "Ascent", "▲", "#7BFFA0", 1200, 1800, 16000, 311),
		},
	},
	// Saturn-V (v0.9.1+): the canonical Apollo launch vehicle.
	// Tuning per the v0.9 plan §v0.9.1 — TWR > 1 at sea level on
	// stage 1 (S-IC: 35,100 kN against ~2.9 Mkg total mass = ~1.24
	// at sea-level g). Stages bottom-first: S-IC fires + decouples
	// first, S-II next, S-IVB is the core / payload that survives
	// every decouple.
	LoadoutSaturnVID: {
		ID:    LoadoutSaturnVID,
		Name:  "Saturn V",
		Role:  "launch-vehicle",
		Glyph: "▲",
		Color: "#FFD93D",
		Stages: []Stage{
			// S-IC booster: F-1 cluster, sea-level Isp (the only
			// stage that fires in atmosphere). BC tuned for the
			// real S-IC's ~80 m² cross-section and ~2.9 Mkg wet
			// mass: BC = C_D · A / m ≈ 0.3 · 80 / 2.9e6 ≈ 8e-6.
			stageWithBC(LoadoutSaturnVID, "S-IC", "▲", "#FF8C42",
				130000, 2160000, 35100000, 263, 8e-6),
			// S-II sustainer: J-2 cluster, vacuum Isp. Smaller
			// cross-section ~40 m² but mostly vacuum flight, so
			// drag is negligible regardless. BC ≈ 0.3·40/480e3 ≈ 2.5e-5.
			stageWithBC(LoadoutSaturnVID, "S-II", "▲", "#FFC042",
				40000, 440000, 5140000, 421, 2.5e-5),
			// S-IVB insertion: J-2 single, vacuum Isp. Same shape
			// as the standalone S-IVB-1 loadout but lives as the
			// top stage of the Saturn-V chain.
			stageWithBC(LoadoutSaturnVID, "S-IVB", "▲", "#FFD93D",
				11000, 109000, 1023000, 421, 6.25e-5),
		},
	},
	// SLS Block 1 (v0.9.4+): NASA's heavy-lift LV, Artemis 1+. The
	// SRBs and core stage actually fire in parallel from lift-off in
	// real life; we model them as sequential stages here because the
	// engine here is single-fire-per-stage. Lift-off TWR with SRBs
	// alone ≈ 1.27 (vs real ~1.57 with both SRBs and core); the
	// Δv lost during the boost phase is mostly recovered in stage 2,
	// where our "fresh" core has all 979 t of LH2/LOX rather than
	// the ~722 t left after firing for 126 s.
	LoadoutSLSBlock1ID: {
		ID:    LoadoutSLSBlock1ID,
		Name:  "SLS Block 1",
		Role:  "launch-vehicle",
		Glyph: "▲",
		Color: "#FF6B35",
		Stages: []Stage{
			// Twin 5-segment solid rocket boosters. Casings dropped
			// at burnout. ~12.2 m² combined cross-section, ~1.47 Mkg
			// wet → BC ≈ 0.3 · 12 / 1.47e6 ≈ 2.4e-6, but two long
			// skinny solids alongside the core have higher effective
			// drag — round to 8e-6 to match Saturn V S-IC scale.
			stageWithBC(LoadoutSLSBlock1ID, "SRBs", "▲", "#E0E0E0",
				198000, 1270000, 32000000, 268, 8e-6),
			// Core stage: 4× RS-25 cluster, vacuum Isp (fires through
			// most of the ascent above the dense atmosphere by the
			// time SRBs separate at ~50 km). BC similar to S-II.
			stageWithBC(LoadoutSLSBlock1ID, "Core", "▲", "#FF6B35",
				85275, 979452, 9290000, 452, 2.5e-5),
			// ICPS (Interim Cryogenic Propulsion Stage): RL10B-2,
			// vacuum Isp. Same shape as the standalone ICPS loadout
			// but lives as the top of the SLS chain. TLI-class burn,
			// not orbital insertion — TWR is very low (~0.1).
			stageWithBC(LoadoutSLSBlock1ID, "ICPS", "◆", "#5BB3FF",
				3500, 25000, 110000, 462, 6.25e-5),
		},
	},
	// Falcon 9 Block 5 (v0.9.4+): SpaceX two-stage LV. ~549 t fully
	// fuelled — about a fifth of Saturn V / SLS by mass. Lift-off
	// TWR ~1.4 (9× Merlin 1D = 7607 kN SL vs ~5380 kN weight). Stage
	// 2's Merlin Vacuum gives TWR ~0.85 at ignition, similar to
	// Saturn V S-II — same continuous-burn upper-stage profile.
	LoadoutFalcon9ID: {
		ID:    LoadoutFalcon9ID,
		Name:  "Falcon 9",
		Role:  "launch-vehicle",
		Glyph: "▲",
		Color: "#E8E8E8",
		Stages: []Stage{
			// First stage: 9× Merlin 1D, sea-level Isp. 3.7 m
			// diameter, ~10.75 m² cross-section, ~437 t wet →
			// BC ≈ 0.3 · 10.75 / 437e3 ≈ 7.4e-6.
			stageWithBC(LoadoutFalcon9ID, "F9-S1", "▲", "#E8E8E8",
				25600, 411000, 7607000, 282, 7.4e-6),
			// Second stage: 1× Merlin Vacuum, vacuum Isp. Above the
			// atmosphere by ignition, BC matters less — pick a
			// vacuum-stage value similar to S-IVB.
			stageWithBC(LoadoutFalcon9ID, "F9-S2", "▲", "#B0D8FF",
				3900, 107500, 934000, 348, 5e-5),
		},
		// CanSoftLand is per-Stage: F9-S1 carries it (retro-burn
		// recovery), F9-S2 does not (orbital re-entry > V_CRIT
		// kinematically). After S1 decouples and becomes its own
		// slate craft, S1.CanSoftLand=true rides with it; the
		// surviving composite (just F9-S2) re-derives
		// CanSoftLand=false via SyncFields. See
		// stages_catalog.go for the catalog flag.
	},
	// Apollo-Stack (v0.10.1+): the full mission stack. The first
	// three stages are the canonical Saturn-V tuning (byte-identical
	// numbers — ascent flies the same). On top: a Lunar Module
	// (Lander tier) then the Command/Service Module as the surviving
	// core. Lift-off TWR with the LM+CSM payload: 35,100 kN against
	// ~2.93 Mkg total ≈ 1.22 at sea-level g — still > 1.
	LoadoutApolloStackID: {
		ID:    LoadoutApolloStackID,
		Name:  "Apollo Stack",
		Role:  "mission-stack",
		Glyph: "▲",
		Color: "#FFD93D",
		Stages: []Stage{
			stageWithBC(LoadoutApolloStackID, "S-IC", "▲", "#FF8C42",
				130000, 2160000, 35100000, 263, 8e-6),
			stageWithBC(LoadoutApolloStackID, "S-II", "▲", "#FFC042",
				40000, 440000, 5140000, 421, 2.5e-5),
			stageWithBC(LoadoutApolloStackID, "S-IVB", "▲", "#FFD93D",
				11000, 109000, 1023000, 421, 6.25e-5),
			// Lunar Module — split into Descent + Ascent (v0.12 Slice 2
			// / ADR 0007). Post-transposition (ADR 0009) the LM rides as
			// a docked nose payload above the SM/CM core and is released
			// via Undock for the lunar descent. Fuel trimmed to the ADR
			// 0009 locked table: descent 9500→6310 (real abort reserve,
			// ~2500 m/s cap), ascent 1800→1269 (~2200 m/s cap) — the LM
			// no longer double-duties as the LOI engine, so it can shed
			// the surplus. The LM is a negligible fraction of the
			// ~2.93 Mkg stack, so lift-off TWR stays ~1.22.
			stage(LoadoutApolloStackID, "Descent", "▼", "#5FFF87",
				2500, 6310, 45000, 311),
			stage(LoadoutApolloStackID, "Ascent", "▲", "#7BFFA0",
				1200, 1269, 16000, 311),
			// Command/Service Module — split into a propulsive Service
			// Module + a passive Command Module (v0.12 / ADR 0009). The
			// SM (SPS engine; SPS fuel trimmed 18400→16000) fires LOI and
			// TEI once transposition makes it Stages[0]; the CM is the
			// engineless parachute capsule that splashes down. SM dry
			// 6000 + CM dry 5900 = the pre-split CSM dry 11900 → the
			// split is mass-neutral and lift-off TWR is unchanged. SM
			// below CM so the firing SPS sits beneath the passive capsule.
			stage(LoadoutApolloStackID, "SM", "◉", "#C0C0FF",
				6000, 16000, 91000, 314),
			stage(LoadoutApolloStackID, "CM", "◓", "#B8C8E0",
				5900, 0, 0, 0),
		},
		// v0.12 / ADR 0009: drop S-IC, S-II, S-IVB individually, then the
		// trailing 2 releases the LM (Descent + Ascent) as a single
		// 2-stage craft. After the three Saturn pops the active craft is
		// [Descent, Ascent, SM, CM] — the pre-transposition state. From
		// there the player either presses D (one-shot transpose: reorder
		// so the SM fires, LM rides as a docked nose payload) OR stages
		// once more to drop the LM as a free craft for the canonical
		// manual flip (slew the [SM, CM] core 180°, RCS-dock to the LM).
		// The 2-group keeps the LM intact either way — staging it one
		// stage at a time would strand the Descent and split the lander.
		// Sum (5) < 7 stages.
		DecouplePlan: []int{1, 1, 1, 2},
	},
	// Re-entry capsule (v0.12 Slice 3, ADR 0008): single command-module
	// stage carrying a parachute and no engine landing. HasParachute /
	// !CanSoftLand ride per-Stage via the StageCatalog by-Name lookup
	// (the "Capsule" entry). No main engine — the player de-orbits with
	// RCS (or spawns sub-orbital) and recovers under chute.
	LoadoutCapsuleID: {
		ID:    LoadoutCapsuleID,
		Name:  "Capsule",
		Role:  "capsule",
		Glyph: "◓",
		Color: "#B8C8E0", // pale bare-metal command module
		Stages: []Stage{
			stage(LoadoutCapsuleID, "Capsule", "◓", "#B8C8E0", 5800, 0, 0, 0),
		},
	},
	// Kern Stack (v0.15 / ADR 0014): the scale-matched Lumen vehicle.
	// Four stages bottom-first [Boost, Transfer, Lander, Pod]. Masses are
	// tuned for a ~6 km/s total ideal Δv (Boost ~2.3 + Transfer ~2.2 +
	// Lander ~1.5 km/s) — enough for Lumen orbit (~3.4 km/s), a Cursor
	// transfer + capture + descent, and the ascent/return, on the
	// stripped-back scale. Lift-off TWR ≈ 1.70 against Kern's Earth-like
	// surface g (360 kN vs ~21.3 t × g0) — retuned up from the original
	// 250 kN / TWR 1.18 after a playtest found the pad climb glacial and
	// gravity-loss-heavy on a Kerbin-class world. Thrust doesn't enter the
	// ideal Δv budget, so the ~6 km/s sizing is unchanged. Isp values are
	// KSP stock engines collapsed to one Isp per stage. ScaleClass tags it
	// stripped-back for the spawn-form hint (never a filter, ADR 0014).
	LoadoutKernStackID: {
		ID:         LoadoutKernStackID,
		Name:       "Kern Stack",
		Role:       "mission-stack",
		Glyph:      "▲",
		Color:      "#7BD3FF", // Lumen-cyan — distinct from the real fleet's warm yellows
		ScaleClass: bodies.ScaleStrippedBack,
		Stages: []Stage{
			// Boost: Mainsail-class kerolox first stage (Isp 285 sea-level —
			// the only stage that fires in atmosphere). TWR ≈ 1.70 off the pad.
			kernStage("Boost", "▲", "#7BD3FF", 2000, 12000, 360000, 285,
				14, 4, "#D8E6F0", FuelTypeKerolox, false, false, false),
			// Transfer: Poodle-class vacuum stage (Isp 350) for the Kern
			// orbit insertion and the Cursor transfer/capture burns.
			kernStage("Transfer", "▲", "#9BE0FF", 1000, 3500, 60000, 350,
				10, 3, "#C8E0F0", FuelTypeHydrolox, false, false, false),
			// Lander: Terrier-class throttleable descent/ascent engine
			// (Isp 345) with legs — fires the powered descent to Cursor and
			// the return-to-orbit burn. Soft-land qualified.
			kernStage("Lander", "▼", "#5FFF87", 800, 1000, 16000, 345,
				5, 3, "#D4C088", FuelTypeHypergolic, true, true, false),
			// Pod: engineless crew capsule, the surviving core. Recovers
			// under parachute (ADR 0008) — the "return" half of the budget.
			kernStage("Pod", "◓", "#B8C8E0", 1000, 0, 0, 0,
				6, 3, "#C8C8D0", "", false, false, true),
		},
		// Drop Boost, Transfer, Lander one at a time; the Pod survives every
		// decouple and splashes down. Sum (3) < 4 stages.
		DecouplePlan: []int{1, 1, 1},
	},
}

// LoadoutOrder lists loadouts in canonical UI cycle order — the
// v0.8.2+ spawn form's craft-type field cycles through this. v0.9.1
// appends Saturn-V at the end so existing playtests keep landing on
// S-IVB-1 by default. v0.9.4+ appends SLS Block 1 and Falcon 9.
var LoadoutOrder = []string{
	LoadoutSIVB1ID,
	LoadoutICPSID,
	LoadoutRCSTugID,
	LoadoutLanderID,
	LoadoutSaturnVID,
	LoadoutSLSBlock1ID,
	LoadoutFalcon9ID,
	LoadoutApolloStackID,
	LoadoutCapsuleID,
	LoadoutKernStackID,
}

// LookupLoadout returns the catalog entry for the given ID, or the
// S-IVB-1 default when the ID is empty / unknown. v0.8.2+: the
// fallback path keeps pre-v0.8.2 saves loadable — those entries
// have no LoadoutID and resolve to the default loadout.
func LookupLoadout(id string) Loadout {
	if l, ok := Loadouts[id]; ok {
		return l
	}
	return Loadouts[LoadoutSIVB1ID]
}

// NewFromLoadout constructs a Spacecraft from a loadout entry. The
// caller still has to set Primary + State (orbit), name, and any
// per-instance overrides. Convenience for spawn paths so they don't
// duplicate the field-setting boilerplate.
//
// v0.9.1+: Stages is the source of truth — copied from the catalog
// entry — and SyncFields populates the legacy flat fields
// (DryMass / Fuel / Thrust / Isp / Monoprop / RCSThrust / RCSIsp)
// from Stages so pre-v0.9.1 readers keep working without per-site
// changes.
func NewFromLoadout(loadoutID string) *Spacecraft {
	l := LookupLoadout(loadoutID)
	stages := make([]Stage, len(l.Stages))
	copy(stages, l.Stages)
	// Copy the decouple plan (own backing array) so StageActive's
	// positional reslice (DecouplePlan[1:]) never mutates the shared
	// catalog literal. Empty/nil plan stays nil ⇒ single-pop default.
	var plan []int
	if len(l.DecouplePlan) > 0 {
		plan = make([]int, len(l.DecouplePlan))
		copy(plan, l.DecouplePlan)
	}
	c := &Spacecraft{
		Name:                 l.Name,
		LoadoutID:            l.ID,
		Role:                 l.Role,
		Glyph:                l.Glyph,
		Color:                l.Color,
		Throttle:             1.0,
		BallisticCoefficient: DefaultBallisticCoefficient,
		Stages:               stages,
		DecouplePlan:         plan,
		SlewRateDegPerSec:    l.SlewRateDegPerSec,
	}
	c.SyncFields()
	return c
}

// NewFromStages constructs a Spacecraft from a player-assembled
// stage list (bottom-first, same convention as Loadout.Stages) —
// the v0.10.1+ stack-configurator path. Sibling of NewFromLoadout
// with no catalog entry behind it: LoadoutID is left empty (a
// custom craft is not a catalog archetype), and identity/visuals
// come from the top (core) stage so the slate HUD has a sensible
// name + marker for the vessel the player keeps flying.
//
// The caller still sets Primary + State. Returns nil when stages
// is empty — an empty stack is not a spawnable craft (callers
// reject before reaching the spawn path).
//
// Custom craft persist through save/load via the existing v6
// per-stage wire format (save schema v6, v0.9.1) — no migration:
// the flat shadow fields are re-derived by SyncFields on load and
// the empty LoadoutID resolves to the default only for those
// derived mirrors, never overriding the round-tripped Stages.
func NewFromStages(stages []Stage) *Spacecraft {
	if len(stages) == 0 {
		return nil
	}
	cp := make([]Stage, len(stages))
	copy(cp, stages)
	core := cp[len(cp)-1] // top stage = the surviving "core"
	name := core.Name
	if name == "" {
		name = "Custom"
	}
	glyph := core.Glyph
	if glyph == "" {
		glyph = "▲"
	}
	color := core.Color
	if color == "" {
		color = "#FFD93D"
	}
	c := &Spacecraft{
		Name:                 name,
		LoadoutID:            "", // custom — no catalog archetype
		Role:                 "custom",
		Glyph:                glyph,
		Color:                color,
		Throttle:             1.0,
		BallisticCoefficient: DefaultBallisticCoefficient,
		Stages:               cp,
	}
	c.SyncFields()
	return c
}
