package spacecraft

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
	}
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
		Stages: []Stage{
			stage(LoadoutLanderID, "Lander", "▼", "#5FFF87", 4000, 8000, 45000, 311),
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
	c := &Spacecraft{
		Name:                 l.Name,
		LoadoutID:            l.ID,
		Role:                 l.Role,
		Glyph:                l.Glyph,
		Color:                l.Color,
		Throttle:             1.0,
		BallisticCoefficient: DefaultBallisticCoefficient,
		Stages:               stages,
	}
	c.SyncFields()
	return c
}
