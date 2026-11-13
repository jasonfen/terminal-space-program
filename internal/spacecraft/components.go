package spacecraft

import (
	"fmt"
	"math"
)

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

	// Description is an optional one-line blurb surfaced by the VAB part
	// inspector (ADR 0030 §7). Purely cosmetic catalog data — like Glyph /
	// Color it carries no save-hash weight (there is no parts-catalog hash;
	// ADR 0026 §4) and is omitempty so existing catalogs round-trip
	// byte-identical.
	Description string `json:"description,omitempty"`

	// Kind is one of the ComponentKind* constants; it selects which Stage
	// scalars this component contributes during aggregation.
	Kind string `json:"kind"`

	// DryMassKg is the component's empty mass in kg — additive across the
	// whole stage for every kind.
	DryMassKg float64 `json:"dry_mass_kg"`

	// Engine fields.
	ThrustN float64 `json:"thrust_n,omitempty"`
	IspS    float64 `json:"isp_s,omitempty"`
	// IspSLS / IspVacS / ThrustAt: see Part (ADR 0070).
	IspSLS   float64 `json:"isp_sl_s,omitempty"`
	IspVacS  float64 `json:"isp_vac_s,omitempty"`
	ThrustAt string  `json:"thrust_at,omitempty"`

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

	// Ports (ADR 0056 decision 7) lists the docking ports this component
	// carries, in the Part.Ports shape (`"ports": [{"end": "top"}]`).
	// composePart unions them onto the composed Part, so a stage that holds
	// a Docking Port component docks.
	Ports []PartPort `json:"ports,omitempty"`

	// CargoHold (ADR 0057 5.1) lets the stage that holds this component
	// receive cargo. composePart ORs it onto the composed Part.
	CargoHold bool `json:"cargo_hold,omitempty"`

	// RCSThrustN / MonopropMassKg (ADR 0067 decision 9) are the component's
	// published RCS figures (the LM's 890 N and its share of the 287 kg load).
	// composePart carries the largest thrust and the summed monoprop onto the
	// composed Part; zero adds nothing, and neither adds to dry mass (published
	// masses are inclusive).
	RCSThrustN     float64 `json:"rcs_thrust_n,omitempty"`
	MonopropMassKg float64 `json:"monoprop_mass_kg,omitempty"`
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

// ComposeStage aggregates a list of component IDs into a runtime Stage,
// deriving the RCS pool from dry mass exactly as the loadout-resolve path
// does (DefaultRCSLoadout) so a VAB preview matches the eventually-spawned
// craft. Returns a non-empty warning string when the components don't form
// a valid stage (an unknown component, or mixed fuel chemistry — ADR 0029
// §3); the returned Stage is then the zero value. Used by the VAB screen
// (internal/tui/screens/vab.go), which can't reach the unexported
// aggregation internals.
func ComposeStage(componentIDs []string, comps map[string]Component) (Stage, string) {
	agg, err := composePart(Part{Components: componentIDs}, comps)
	if err != nil {
		return Stage{}, err.Error()
	}
	st := agg.ToStage()
	mp, monoCap, rcsThrust, rcsIsp := stageRCSFor(st.DryMass, agg.RCSThrustN, agg.MonopropMassKg)
	st.MonopropMass, st.MonopropCap, st.RCSThrust, st.RCSIsp = mp, monoCap, rcsThrust, rcsIsp
	return st, ""
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
	var sumTVac, sumFlow, sumFlowSL float64 // ADR 0070: vacuum thrust, kg/s·g0 flow, SL-Isp-weighted flow
	var fuelType, commandSource, antennaKind string
	var antennaRange float64
	var canSoftLand, hasParachute bool
	var compPorts []PartPort
	cargoHold := p.CargoHold
	var rcsThrust, monoprop float64

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
		compPorts = append(compPorts, c.Ports...)
		cargoHold = cargoHold || c.CargoHold
		rcsThrust = math.Max(rcsThrust, c.RCSThrustN)
		monoprop += c.MonopropMassKg
		switch c.Kind {
		case ComponentEngine:
			if tv, sl, vac, basis := engineFigures(c.ThrustN, c.IspS, c.IspSLS, c.IspVacS, c.ThrustAt); tv > 0 && basis > 0 {
				totalThrust += c.ThrustN
				sumThrustOverIsp += c.ThrustN / basis
				flow := tv / vac // mass flow x g0
				sumTVac += tv
				sumFlow += flow
				sumFlowSL += flow * sl
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
	out.ThrustVacN, out.IspSLS, out.IspVacS, out.ThrustAt = 0, 0, 0, ""
	if sumFlow > 0 {
		out.ThrustVacN, out.IspVacS, out.IspSLS = sumTVac, sumTVac/sumFlow, sumFlowSL/sumFlow
	}
	out.FuelType = fuelType
	out.CommandSource = commandSource
	out.CanSoftLand = canSoftLand
	out.HasParachute = hasParachute
	// ADR 0056 decision 7: the Part's own ports (a composed catalog part
	// such as probe-sat-stage keeps them via out := p) plus every
	// component's.
	out.Ports = append(append([]PartPort(nil), p.Ports...), compPorts...)
	out.CargoHold = cargoHold
	// A Part's own RCS figures win; otherwise the components' (zero keeps the
	// dry-mass scaling).
	if p.RCSThrustN == 0 {
		out.RCSThrustN = rcsThrust
	}
	if p.MonopropMassKg == 0 {
		out.MonopropMassKg = monoprop
	}
	if antennaKind != AntennaNone {
		out.Antenna = &Antenna{Kind: antennaKind, RangeM: antennaRange}
	} else {
		out.Antenna = nil
	}
	applyDerivedSprite(&out)
	return out, nil
}

// engineFigures reads one engine's published figures (ADR 0070): thrustN at
// the condition thrustAt names ("sl" or "vac"; absent means vacuum), isp as the
// flat fallback, and the sea-level / vacuum Isp pair. It returns the thrust in
// vacuum, both Isps (an unstated side takes the other, or the flat isp) and the
// Isp at the thrust's own condition. Mass flow is thrustN/(g0*basis) either way.
func engineFigures(thrustN, isp, sl, vac float64, thrustAt string) (thrustVac, ispSL, ispVac, basis float64) {
	if sl <= 0 {
		sl = vac
	}
	if vac <= 0 {
		vac = sl
	}
	if sl <= 0 {
		sl, vac = isp, isp
	}
	basis = vac
	if thrustAt == "sl" {
		basis = sl
	}
	if basis <= 0 {
		return thrustN, sl, vac, 0
	}
	return thrustN * vac / basis, sl, vac, basis
}

// engine is the part's engine figures: a composed part carries them from
// composePart (ThrustVacN set), an atomic one states them in the file. isp is
// the rated Isp, at the condition the thrust is rated at.
func (p Part) engine() (thrustVac, ispSL, ispVac, isp float64) {
	if p.ThrustVacN > 0 {
		return p.ThrustVacN, p.IspSLS, p.IspVacS, p.IspS
	}
	if p.ThrustN <= 0 && p.IspS <= 0 && p.IspSLS <= 0 && p.IspVacS <= 0 {
		return 0, 0, 0, 0
	}
	thrustVac, ispSL, ispVac, isp = engineFigures(p.ThrustN, p.IspS, p.IspSLS, p.IspVacS, p.ThrustAt)
	if p.IspS > 0 {
		isp = p.IspS
	}
	return
}

// RatedIsp is the Isp at the condition the part's thrust is rated at.
func (p Part) RatedIsp() float64 { _, _, _, i := p.engine(); return i }

// RatedIsp is the component engine's Isp at the condition its thrust is rated at.
func (c Component) RatedIsp() float64 {
	_, _, _, i := engineFigures(c.ThrustN, c.IspS, c.IspSLS, c.IspVacS, c.ThrustAt)
	if c.IspS > 0 {
		return c.IspS
	}
	return i
}
