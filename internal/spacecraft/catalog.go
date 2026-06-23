package spacecraft

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Data-driven loadout / parts catalog (ADR 0026, cycle 1 of the Axis B
// vehicle-expansion program). This file is the schema + loader
// FOUNDATION only: it defines the normalized on-disk model (a single
// parts catalog + loadouts that reference parts by ID) and the
// bodies-pattern overlay loader. The embedded data files are empty stubs
// this slice (C1-1); the faithful, golden-tested migration of the ten
// inline Loadouts (loadouts.go) and the StageCatalog (stages_catalog.go)
// into them lands in C1-2 / C1-3. Nothing here changes any existing
// runtime path yet — the hardcoded catalogs remain the source of truth
// until the migration slices wire NewFromLoadout / BuildStage over this
// loader.
//
// Mirrors internal/bodies/systems.go: embedded JSON + a user overlay
// (skip-bad-with-warning), user wins on ID. Per ADR 0026 §4 there is
// deliberately NO save-hash gate — saves persist full Stages[] per craft
// and need the catalog only at spawn time, so the loadout catalog is not
// wired into BodyCatalogHash.

//go:embed data/parts.json data/loadouts.json
var catalogFS embed.FS

// Part is a normalized, data-authored atomic stage — engine + tank +
// structure fused, exactly as Stages are atomic today. It is the single
// parts-catalog representation that unifies today's inline Loadout.Stages
// and the separate StageCatalog (ADR 0026 §1). One Part materializes into
// one runtime Stage via ToStage at spawn time. Stages stay atomic this
// cycle; the schema is left forward-compatible so a Part can later declare
// itself a composition of finer components (engine / tank / decoupler /
// antenna) at the VAB cycle (4).
type Part struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Glyph string `json:"glyph,omitempty"`
	Color string `json:"color,omitempty"`
	// Tier is the configurator's one-word grouping hint ("booster",
	// "sustainer", "transfer", "payload", "tug") — purely descriptive,
	// carried over from StageModule.Tier. Not part of the runtime Stage.
	Tier string `json:"tier,omitempty"`

	// Physical numbers (units mirror Stage exactly).
	DryMassKg            float64 `json:"dry_mass_kg"`
	FuelMassKg           float64 `json:"fuel_mass_kg,omitempty"`
	FuelCapacityKg       float64 `json:"fuel_capacity_kg,omitempty"`
	ThrustN              float64 `json:"thrust_n,omitempty"`
	IspS                 float64 `json:"isp_s,omitempty"`
	MonopropMassKg       float64 `json:"monoprop_mass_kg,omitempty"`
	MonopropCapacityKg   float64 `json:"monoprop_capacity_kg,omitempty"`
	RCSThrustN           float64 `json:"rcs_thrust_n,omitempty"`
	RCSIspS              float64 `json:"rcs_isp_s,omitempty"`
	BallisticCoefficient float64 `json:"ballistic_coeff,omitempty"`

	// Launch-sprite + flame styling (ViewLaunch chase-cam silhouette).
	LaunchSpriteRowsPx  int    `json:"launch_sprite_rows_px,omitempty"`
	LaunchSpriteWidthPx int    `json:"launch_sprite_width_px,omitempty"`
	LaunchSpriteColor   string `json:"launch_sprite_color,omitempty"`
	LaunchSpriteHasLegs bool   `json:"launch_sprite_has_legs,omitempty"`
	FuelType            string `json:"fuel_type,omitempty"`

	// Capability flags (per-stage, mirror Stage).
	CanSoftLand  bool `json:"can_soft_land,omitempty"`
	HasParachute bool `json:"has_parachute,omitempty"`

	// Forward-compatible attributes consumed by cycle 2 (ADR 0027 — comms).
	// Declared in the schema now so it stays stable; C1 ignores them.
	// CommandSource is "crewed" | "probe" | "none" (empty == none);
	// Antenna declares the part's antenna {kind, power}.
	CommandSource string   `json:"command_source,omitempty"`
	Antenna       *Antenna `json:"antenna,omitempty"`
}

// Antenna is the forward-compatible (ADR 0027) per-part antenna attribute:
// kind is "none" | "direct" | "relay"; a direct antenna can use the
// network, a relay antenna can also forward for others. range_m is the
// antenna's rated range in metres — the distance at which it reaches an
// identical antenna (the CommNet combinability model). Declared in the
// cycle-1 schema; cycle 2 reads it.
type Antenna struct {
	Kind   string  `json:"kind"`
	RangeM float64 `json:"range_m,omitempty"`
}

// PartOverride carries the only per-instance knobs a loadout may apply to
// a referenced part (ADR 0026 §1): a fuel fill fraction, a display name,
// and a color. Deliberately NOT arbitrary field overrides — that
// ambiguity is the rejected "hybrid" option. FuelFillFraction is a
// pointer so an absent override is distinguishable from an explicit 0.0
// (empty tank).
type PartOverride struct {
	FuelFillFraction *float64 `json:"fuel_fill_fraction,omitempty"`
	Name             string   `json:"name,omitempty"`
	Color            string   `json:"color,omitempty"`
}

// PartRef is one entry in a loadout's ordered part list: a part ID plus
// an optional per-instance override.
type PartRef struct {
	PartID   string        `json:"part_id"`
	Override *PartOverride `json:"override,omitempty"`
}

// LoadoutDef is a data-authored loadout: an ordered list of part
// references (bottom-first, the Stages convention) plus its plans and
// per-loadout tuning. The normalized counterpart of today's inline
// Loadout struct — a loadout references parts by ID rather than inlining
// full Stage literals (ADR 0026 §1).
type LoadoutDef struct {
	ID                string    `json:"id"`
	Name              string    `json:"name,omitempty"`
	Role              string    `json:"role,omitempty"`
	Glyph             string    `json:"glyph,omitempty"`
	Color             string    `json:"color,omitempty"`
	Parts             []PartRef `json:"parts"`
	DecouplePlan      []int     `json:"decouple_plan,omitempty"`
	NosePayloadPlan   []int     `json:"nose_payload_plan,omitempty"`
	SlewRateDegPerSec float64   `json:"slew_rate_deg_per_sec,omitempty"`
	ScaleClass        string    `json:"scale_class,omitempty"`

	// Source is a runtime annotation ("embedded" / "user"), excluded from
	// JSON so it never affects round-trips or any future hash use.
	Source string `json:"-"`
}

// Catalog is the on-disk envelope shared by the embedded data files and
// user overlay files: a list of parts and/or loadouts. Both lists are
// optional, so a user file may add just parts, just loadouts, or both.
type Catalog struct {
	Parts    []Part       `json:"parts,omitempty"`
	Loadouts []LoadoutDef `json:"loadouts,omitempty"`
}

// CatalogWarning records a user overlay file that failed to load. Mirrors
// bodies.LoadWarning: embedded-catalog parse failures are hard errors (the
// embedded set must always load); user-file failures are warnings so one
// bad mod never rejects the whole catalog (ADR 0026 §3).
type CatalogWarning struct {
	Path string
	Err  error
}

func (w CatalogWarning) Error() string {
	return fmt.Sprintf("%s: %v", w.Path, w.Err)
}

// ToStage materializes a Part into a runtime Stage — a pure field copy.
// The part's own identity fields ride along (Name / Glyph / Color); the
// loadout-level LoadoutID is stamped by the loadout-assembly path
// (C1-3 NewFromLoadout), not here, since a part doesn't know which
// loadout references it. Tier (configurator metadata) has no Stage
// counterpart and is dropped. The comms attributes (command_source /
// antenna, ADR 0027) DO ride onto the Stage now (cycle 2 / C2-1).
func (p Part) ToStage() Stage {
	st := Stage{
		DryMass:              p.DryMassKg,
		FuelMass:             p.FuelMassKg,
		FuelCapacity:         p.FuelCapacityKg,
		Thrust:               p.ThrustN,
		Isp:                  p.IspS,
		MonopropMass:         p.MonopropMassKg,
		MonopropCap:          p.MonopropCapacityKg,
		RCSThrust:            p.RCSThrustN,
		RCSIsp:               p.RCSIspS,
		BallisticCoefficient: p.BallisticCoefficient,
		Name:                 p.Name,
		Glyph:                p.Glyph,
		Color:                p.Color,
		LaunchSpriteRowsPx:   p.LaunchSpriteRowsPx,
		LaunchSpriteWidthPx:  p.LaunchSpriteWidthPx,
		LaunchSpriteColor:    p.LaunchSpriteColor,
		FuelType:             p.FuelType,
		LaunchSpriteHasLegs:  p.LaunchSpriteHasLegs,
		CanSoftLand:          p.CanSoftLand,
		HasParachute:         p.HasParachute,
		CommandSource:        p.CommandSource,
	}
	if p.Antenna != nil {
		st.AntennaKind = p.Antenna.Kind
		st.AntennaRangeM = p.Antenna.RangeM
	}
	return st
}

// LoadCatalogOverlay re-resolves the runtime catalogs (Loadouts,
// LoadoutOrder, StageCatalog) from the embedded data MERGED with the user
// overlay (the XDG loadouts/ dir), and returns warnings for any malformed
// user files (ADR 0026 §2/§3 — bodies-pattern overlay, skip-bad-with-warning).
//
// Init (buildLoadouts / buildStageCatalog) loads the EMBEDDED catalog only,
// so package-var init and the golden tests stay deterministic. The app
// calls this once at startup to layer in user mods; tests that need only
// the embedded catalog skip it. User parts win on ID; user loadouts append
// (or replace on ID) after the embedded set, so LoadoutOrder lists them
// last. Embedded parse failures already panicked at init, so the error
// from LoadCatalogWithWarnings is not re-surfaced here.
func LoadCatalogOverlay() []CatalogWarning {
	parts, defs, warnings, _ := LoadCatalogWithWarnings()
	sc := make(map[string]StageModule, len(parts))
	for id, p := range parts {
		sc[id] = p.toStageModule()
	}
	lo := make(map[string]Loadout, len(defs))
	order := make([]string, 0, len(defs))
	for _, d := range defs {
		lo[d.ID] = resolveLoadout(d, parts)
		order = append(order, d.ID)
	}
	StageCatalog = sc
	Loadouts = lo
	LoadoutOrder = order
	return warnings
}

// LoadCatalog reads the embedded parts + loadouts catalog, merges any
// user overlay files, and returns the merged set. Warnings from malformed
// user files are dropped — call LoadCatalogWithWarnings to inspect them.
func LoadCatalog() (map[string]Part, []LoadoutDef, error) {
	parts, loadouts, _, err := LoadCatalogWithWarnings()
	return parts, loadouts, err
}

// LoadCatalogWithWarnings is the warning-aware variant. The returned
// warnings slice holds a CatalogWarning per user overlay file that failed
// to parse; embedded-catalog parse failures surface as a hard error (the
// embedded set must always load). This is the ADR 0026 "LoadAllWithWarnings
// style" entrypoint, mirroring bodies.LoadAllWithWarnings.
func LoadCatalogWithWarnings() (map[string]Part, []LoadoutDef, []CatalogWarning, error) {
	parts, loadouts, err := loadEmbeddedCatalog()
	if err != nil {
		return nil, nil, nil, err
	}
	parts, loadouts, warnings := mergeUserCatalog(parts, loadouts, userCatalogDir())
	return parts, loadouts, warnings, nil
}

func loadEmbeddedCatalog() (map[string]Part, []LoadoutDef, error) {
	parts := map[string]Part{}
	var loadouts []LoadoutDef
	// Two embedded files (parts + loadouts) folded into one merged catalog;
	// either may hold either list (the shared envelope).
	for _, name := range []string{"data/parts.json", "data/loadouts.json"} {
		data, err := catalogFS.ReadFile(name)
		if err != nil {
			return nil, nil, fmt.Errorf("read embedded %s: %w", name, err)
		}
		var cat Catalog
		if err := json.Unmarshal(data, &cat); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", name, err)
		}
		for _, p := range cat.Parts {
			parts[p.ID] = p
		}
		for i := range cat.Loadouts {
			cat.Loadouts[i].Source = "embedded"
			loadouts = append(loadouts, cat.Loadouts[i])
		}
	}
	return parts, loadouts, nil
}

// mergeUserCatalog overlays every *.json file in dir onto the embedded
// catalog. A user part wins on ID (replaces the embedded entry); a user
// loadout replaces on ID, else appends. A missing dir is fine (overlay is
// optional); a malformed file is skipped with a warning. Factored out (and
// taking dir) so it can be unit-tested against a temp dir.
func mergeUserCatalog(parts map[string]Part, loadouts []LoadoutDef, dir string) (map[string]Part, []LoadoutDef, []CatalogWarning) {
	if dir == "" {
		return parts, loadouts, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return parts, loadouts, nil
		}
		return parts, loadouts, []CatalogWarning{{Path: dir, Err: err}}
	}
	var warnings []CatalogWarning
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			warnings = append(warnings, CatalogWarning{Path: path, Err: err})
			continue
		}
		var cat Catalog
		if err := json.Unmarshal(data, &cat); err != nil {
			warnings = append(warnings, CatalogWarning{Path: path, Err: err})
			continue
		}
		for _, p := range cat.Parts {
			parts[p.ID] = p // user wins on ID
		}
		for _, l := range cat.Loadouts {
			l.Source = "user"
			replaced := false
			for i := range loadouts {
				if loadouts[i].ID == l.ID {
					loadouts[i] = l
					replaced = true
					break
				}
			}
			if !replaced {
				loadouts = append(loadouts, l)
			}
		}
	}
	return parts, loadouts, warnings
}

// userCatalogDir resolves the user loadout-overlay directory:
// $XDG_CONFIG_HOME/terminal-space-program/loadouts (or
// ~/.config/terminal-space-program/loadouts when XDG is unset). Its own
// subdir mirrors bodies' systems/ layout (ADR 0026 §2) and keeps vehicle
// overlays clear of the bodies overlay.
func userCatalogDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "loadouts")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "terminal-space-program", "loadouts")
}
