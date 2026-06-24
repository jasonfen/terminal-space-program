package spacecraft

import "fmt"

// Component is the finest catalog noun (ADR 0029 §1, the VAB cycle / Axis B
// cycle 4) — one level below the atomic Part. A Part may declare an
// optional list of component IDs; when present the Part's flat scalar stats
// are DERIVED by aggregation (aggregateComponents) instead of authored
// inline. This cashes in the forward-compat note ADR 0026 left on Part
// ("a Part can later declare itself a composition of finer components") with
// ZERO migration: an atomic Part (no Components) is untouched.
//
// One Component contributes only to the Stage scalars its Kind owns
// (ADR 0029 §2):
//
//	engine        → ThrustN, IspS, FuelType, dry mass
//	tank          → FuelCapacityKg, FuelType, dry mass
//	command-core  → CommandSource, dry mass, optional soft-land / parachute
//	antenna       → AntennaKind, RangeM, dry mass
//	structure     → dry mass only (adapters, fairings, ballast)
//
// Components are loaded through the existing ADR 0026 catalog loader (one
// more embedded file + user overlay, skip-bad-with-warning) — the modding
// path one level deeper. Visual fields (Glyph / Color) are cosmetic and
// carry no save-hash weight (there is no parts-catalog hash; ADR 0026 §4).
type Component struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Glyph string `json:"glyph,omitempty"`
	Color string `json:"color,omitempty"`

	// Kind is one of the ComponentKind* constants; it selects which Stage
	// scalars this component contributes during aggregation.
	Kind string `json:"kind"`

	// DryMassKg is the component's empty mass in kg — additive across the
	// whole stage for every kind.
	DryMassKg float64 `json:"dry_mass_kg"`

	// Engine fields.
	ThrustN float64 `json:"thrust_n,omitempty"`
	IspS    float64 `json:"isp_s,omitempty"`

	// FuelType is shared by engine + tank: every fuelled component in a
	// stage must agree on chemistry (ADR 0029 §3, the single-fuel-pool
	// invariant). Empty contributes nothing to the chemistry check.
	FuelType string `json:"fuel_type,omitempty"`

	// Tank field.
	FuelCapacityKg float64 `json:"fuel_capacity_kg,omitempty"`

	// Command-core fields. CommandSource is CommandCrewed / CommandProbe
	// (ADR 0027); CanSoftLand / HasParachute are the optional recovery
	// capabilities a command core may carry.
	CommandSource string `json:"command_source,omitempty"`
	CanSoftLand   bool   `json:"can_soft_land,omitempty"`
	HasParachute  bool   `json:"has_parachute,omitempty"`

	// Antenna fields (ADR 0027): kind (AntennaDirect / AntennaRelay) and the
	// rated range in metres.
	AntennaKind string  `json:"antenna_kind,omitempty"`
	RangeM      float64 `json:"range_m,omitempty"`
}

// Component-kind constants (ADR 0029 §2). The five kinds that ship this
// cycle; cargo-hold is deferred to the inert-cargo cycle (ADR 0029 §6).
const (
	ComponentEngine      = "engine"       // thrust + Isp + fuel chemistry
	ComponentTank        = "tank"         // fuel capacity + fuel chemistry
	ComponentCommandCore = "command-core" // control point (crewed/probe) + recovery
	ComponentAntenna     = "antenna"      // comms hardware (direct/relay)
	ComponentStructure   = "structure"    // inert dry mass (adapter / fairing / ballast)
)

// Components indexes the embedded component catalog by ID — the palette the
// VAB composes from (ADR 0029 §5). Resolved at package-var init from the
// embedded data/components.json (mirroring Loadouts / StageCatalog), and
// refreshed with the user overlay by LoadCatalogOverlay at startup. Empty
// in S1's stub; starter content lands in S4.
var Components = buildComponents()

// buildComponents loads the embedded component catalog at init. Panics on a
// malformed embedded file (a build/programmer error, like buildLoadouts).
func buildComponents() map[string]Component {
	comps, _, _, err := loadEmbeddedCatalog()
	if err != nil {
		panic("spacecraft: embedded component catalog failed to load: " + err.Error())
	}
	return comps
}

// aggregateComponents resolves every composed Part (one with a non-empty
// Components list) into its flat scalar stats and returns the resolved
// parts map plus a CatalogWarning per Part that fails validation. Atomic
// Parts (no Components) pass through byte-identical — the zero-migration
// property (ADR 0029 §1). Never mutates the input map.
//
// Aggregation (ADR 0029 §2): dry mass + tank capacity are additive;
// engines combine to Thrust = ΣF_i with the thrust-weighted parallel
// formula Isp_eff = ΣF_i / Σ(F_i/Isp_i) (exact for the single fuel pool);
// command-source / antenna / recovery attributes ride up. Tanks fill full
// (FuelMass == FuelCapacity) so a loadout-level fuel-fill override still
// scales from a full base, exactly as atomic parts do.
//
// A Part that references an unknown component, or mixes fuel chemistries in
// one stage (ADR 0029 §3), yields a warning and is left UNAGGREGATED in the
// map (so loadout resolution does not panic on a missing ID); the VAB
// rejects such a stage before it can be saved (S3).
func aggregateComponents(parts map[string]Part, comps map[string]Component) (map[string]Part, []CatalogWarning) {
	out := make(map[string]Part, len(parts))
	var warnings []CatalogWarning
	for id, p := range parts {
		if len(p.Components) == 0 {
			out[id] = p // atomic — untouched
			continue
		}
		agg, err := composePart(p, comps)
		if err != nil {
			warnings = append(warnings, CatalogWarning{Path: "part:" + id, Err: err})
			out[id] = p // keep raw so a referencing loadout still resolves
			continue
		}
		out[id] = agg
	}
	return out, warnings
}

// composePart derives a composed Part's flat scalar stats from its
// components. Identity (ID / Name / Glyph / Color / Tier), sprite styling,
// and any non-component fields (RCS pool, ballistic coefficient) are
// preserved from the Part; every component-derived scalar is assigned
// explicitly (to zero when no component supplies it) so a composed part
// cannot smuggle inline engine numbers past aggregation.
func composePart(p Part, comps map[string]Component) (Part, error) {
	out := p // preserves identity, sprite, RCS, BC; Components list retained
	var dryMass, fuelCap, totalThrust, sumThrustOverIsp float64
	var fuelType, commandSource, antennaKind string
	var antennaRange float64
	var canSoftLand, hasParachute bool

	setFuel := func(ft string) error {
		if ft == "" {
			return nil
		}
		if fuelType == "" {
			fuelType = ft
			return nil
		}
		if fuelType != ft {
			return fmt.Errorf("mixes fuel chemistries %q and %q in one stage (single fuel type per stage)", fuelType, ft)
		}
		return nil
	}

	for _, cid := range p.Components {
		c, ok := comps[cid]
		if !ok {
			return Part{}, fmt.Errorf("references unknown component %q", cid)
		}
		dryMass += c.DryMassKg
		switch c.Kind {
		case ComponentEngine:
			if c.ThrustN > 0 && c.IspS > 0 {
				totalThrust += c.ThrustN
				sumThrustOverIsp += c.ThrustN / c.IspS
			}
			if err := setFuel(c.FuelType); err != nil {
				return Part{}, err
			}
		case ComponentTank:
			fuelCap += c.FuelCapacityKg
			if err := setFuel(c.FuelType); err != nil {
				return Part{}, err
			}
		case ComponentCommandCore:
			// Crewed wins over probe (a crewed pod is never comms-gated),
			// mirroring SyncFields' vessel-level rule.
			if c.CommandSource == CommandCrewed {
				commandSource = CommandCrewed
			} else if c.CommandSource == CommandProbe && commandSource != CommandCrewed {
				commandSource = CommandProbe
			}
			canSoftLand = canSoftLand || c.CanSoftLand
			hasParachute = hasParachute || c.HasParachute
		case ComponentAntenna:
			// Keep the longest-ranged antenna (SyncFields' vessel rule).
			if c.AntennaKind != AntennaNone && c.RangeM > antennaRange {
				antennaKind = c.AntennaKind
				antennaRange = c.RangeM
			}
		case ComponentStructure:
			// Dry mass only — already added above.
		default:
			return Part{}, fmt.Errorf("component %q has unknown kind %q", cid, c.Kind)
		}
	}

	out.DryMassKg = dryMass
	out.FuelCapacityKg = fuelCap
	out.FuelMassKg = fuelCap // full tanks by default
	out.ThrustN = totalThrust
	if sumThrustOverIsp > 0 {
		out.IspS = totalThrust / sumThrustOverIsp
	} else {
		out.IspS = 0
	}
	out.FuelType = fuelType
	out.CommandSource = commandSource
	out.CanSoftLand = canSoftLand
	out.HasParachute = hasParachute
	if antennaKind != AntennaNone {
		out.Antenna = &Antenna{Kind: antennaKind, RangeM: antennaRange}
	} else {
		out.Antenna = nil
	}
	return out, nil
}
