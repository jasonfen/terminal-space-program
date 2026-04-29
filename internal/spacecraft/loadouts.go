package spacecraft

// Loadout describes a named craft archetype — propulsion numbers,
// dry/wet mass sizing, default RCS pool, and visual differentiation
// (glyph + color). v0.8.2+ ship set:
//
//   - S-IVB-1: J-2-powered third stage. The v0.5.13+ default.
//   - ICPS:    RL-10-powered low-TWR transfer stage. Returns from
//              v0.5.6 — long burns, less mass.
//   - RCS-tug: Pure-monoprop proximity-ops vehicle. No main engine;
//              navigates entirely on RCS. For docking maneuvers.
//   - Lander:  Throttleable descent-stage profile (LM-derived). Lower
//              thrust, lower Isp, sized for surface maneuvering.
//
// Future loadouts land alongside this catalog and are referenced
// from Spacecraft.LoadoutID — a string lookup keeps the on-disk
// format human-editable and lets future modding overlays add craft
// types without code changes.
type Loadout struct {
	ID                 string
	Name               string
	Role               string
	DryMass            float64
	Fuel               float64
	Isp                float64
	Thrust             float64
	Glyph              string
	Color              string
}

// LoadoutS_IVB1 is the v0.5.13+ Apollo S-IVB / J-2 default.
const LoadoutSIVB1ID = "S-IVB-1"

// LoadoutICPS is the v0.5.6 RL-10 low-TWR transfer stage.
const LoadoutICPSID = "ICPS"

// LoadoutRCSTug is the pure-monoprop proximity-ops vehicle.
const LoadoutRCSTugID = "RCS-tug"

// LoadoutLander is the throttleable descent-stage profile.
const LoadoutLanderID = "Lander"

// Loadouts indexes the v0.8.2 launch set by ID. Future patches may
// load user overlays (similar to bodies/themes) but the embedded
// catalog is canonical for now.
var Loadouts = map[string]Loadout{
	LoadoutSIVB1ID: {
		ID:      LoadoutSIVB1ID,
		Name:    "S-IVB",
		Role:    "transfer-stage",
		DryMass: 11000,
		Fuel:    40000,
		Isp:     421,
		Thrust:  1023000, // J-2 vacuum thrust
		Glyph:   "▲",
		Color:   "#FFD93D", // saturated yellow (matches v0.7.1+ ColorCraftMarker)
	},
	LoadoutICPSID: {
		ID:      LoadoutICPSID,
		Name:    "ICPS",
		Role:    "transfer-stage",
		DryMass: 3500,
		Fuel:    25000,
		Isp:     462,
		Thrust:  108000, // RL-10 vacuum thrust
		Glyph:   "◆",
		Color:   "#5BB3FF", // ocean blue
	},
	LoadoutRCSTugID: {
		ID:      LoadoutRCSTugID,
		Name:    "RCS Tug",
		Role:    "tug",
		DryMass: 200,
		Fuel:    0,
		Isp:     0, // no main engine
		Thrust:  0, // pure-monoprop — RCS budget is the only Δv
		Glyph:   "●",
		Color:   "#FF87D7", // pink
	},
	LoadoutLanderID: {
		ID:      LoadoutLanderID,
		Name:    "Lander",
		Role:    "lander",
		DryMass: 4000,
		Fuel:    8000,
		Isp:     311,    // LM descent-stage Isp
		Thrust:  45000,  // throttleable, peak ~10 klbf
		Glyph:   "▼",
		Color:   "#5FFF87", // mint
	},
}

// LoadoutOrder lists loadouts in canonical UI cycle order — the
// v0.8.2+ spawn form's craft-type field cycles through this. v0.8.2
// keeps the order matching the design-doc launch set; future
// loadouts append.
var LoadoutOrder = []string{
	LoadoutSIVB1ID,
	LoadoutICPSID,
	LoadoutRCSTugID,
	LoadoutLanderID,
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
func NewFromLoadout(loadoutID string) *Spacecraft {
	l := LookupLoadout(loadoutID)
	mp, monoCap, rcsThrust, rcsIsp := DefaultRCSLoadout(l.DryMass)
	return &Spacecraft{
		Name:             l.Name,
		LoadoutID:        l.ID,
		Role:             l.Role,
		Glyph:            l.Glyph,
		Color:            l.Color,
		DryMass:          l.DryMass,
		Fuel:             l.Fuel,
		Isp:              l.Isp,
		Thrust:           l.Thrust,
		Throttle:         1.0,
		Monoprop:         mp,
		MonopropCapacity: monoCap,
		RCSThrust:        rcsThrust,
		RCSIsp:           rcsIsp,
	}
}
