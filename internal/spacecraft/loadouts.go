package spacecraft

// Loadout describes a named craft archetype — propulsion numbers,
// dry/wet mass sizing, default RCS pool, and visual differentiation
// (glyph + color). v0.8.2 ship set + v0.9.1 Saturn-V multi-stage:
//
//   - S-IVB-1:  J-2-powered third stage. The v0.5.13+ default.
//   - ICPS:     RL-10-powered low-TWR transfer stage. Returns from
//               v0.5.6 — long burns, less mass.
//   - RCS-tug:  Pure-monoprop proximity-ops vehicle. No main engine;
//               navigates entirely on RCS. For docking maneuvers.
//   - Lander:   Throttleable descent-stage profile (LM-derived). Lower
//               thrust, lower Isp, sized for surface maneuvering.
//   - Saturn-V: 3-stage launch vehicle (S-IC booster, S-II sustainer,
//               S-IVB insertion). v0.9.1+. TWR > 1 at sea level on
//               stage 1.
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

// stageRCS builds the per-stage RCS pool for a stage of the given
// dry mass via DefaultRCSLoadout — same scaling that single-stage
// craft used pre-v0.9.1, so existing loadouts inherit identical
// RCS budgets when wrapped into Stages: [{...}].
func stageRCS(dryMass float64) (mp, monoCap, rcsThrust, rcsIsp float64) {
	return DefaultRCSLoadout(dryMass)
}

// stage builds a single Stage with full tanks + the catalog's
// default RCS pool. Used by the Loadouts map literals to keep the
// per-stage structure terse.
func stage(loadoutID, name, glyph, color string, dry, fuel, thrust, isp float64) Stage {
	mp, monoCap, rcsThrust, rcsIsp := stageRCS(dry)
	return Stage{
		LoadoutID:    loadoutID,
		Name:         name,
		Glyph:        glyph,
		Color:        color,
		DryMass:      dry,
		FuelMass:     fuel,
		FuelCapacity: fuel,
		Thrust:       thrust,
		Isp:          isp,
		MonopropMass: mp,
		MonopropCap:  monoCap,
		RCSThrust:    rcsThrust,
		RCSIsp:       rcsIsp,
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
			// stage that fires in atmosphere).
			stage(LoadoutSaturnVID, "S-IC", "▲", "#FF8C42", 130000, 2160000, 35100000, 263),
			// S-II sustainer: J-2 cluster, vacuum Isp.
			stage(LoadoutSaturnVID, "S-II", "▲", "#FFC042", 40000, 440000, 5140000, 421),
			// S-IVB insertion: J-2 single, vacuum Isp. Same shape
			// as the standalone S-IVB-1 loadout but lives as the
			// top stage of the Saturn-V chain.
			stage(LoadoutSaturnVID, "S-IVB", "▲", "#FFD93D", 11000, 109000, 1023000, 421),
		},
	},
}

// LoadoutOrder lists loadouts in canonical UI cycle order — the
// v0.8.2+ spawn form's craft-type field cycles through this. v0.9.1
// appends Saturn-V at the end so existing playtests keep landing on
// S-IVB-1 by default.
var LoadoutOrder = []string{
	LoadoutSIVB1ID,
	LoadoutICPSID,
	LoadoutRCSTugID,
	LoadoutLanderID,
	LoadoutSaturnVID,
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
