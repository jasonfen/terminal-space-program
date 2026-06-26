package spacecraft

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Design persistence (ADR 0029 §4, the VAB cycle). A custom vehicle the
// player builds in the VAB is CATALOG DATA, not save state: it lives in an
// app-managed designs store (its own directory, written and deleted by the
// VAB), serialized in the SAME catalog-fragment format as the ADR 0026
// modder overlays. Consequences:
//
//   - No save-schema bump — a spawned craft still persists its full
//     Stages[] inline, so loading a save never needs the design (ADR 0029 §4).
//   - Global across saves (KSP .craft-like).
//   - Portable: a design file IS a catalog fragment, so copying it into the
//     modder overlay dir publishes it as a mod (see TestDesignPortability).
//
// The store is deliberately SEPARATE from the hand-authored modder overlay
// (loadouts/) so app-written files never pollute that channel and a design
// can never silently override a built-in by ID collision: designs live in
// their own namespace and are NEVER merged into the global Loadouts map.
// The spawn form (S4) lists them ALONGSIDE catalog loadouts; Design.Resolve
// produces a flyable Loadout on demand.

// Design is one saved custom vehicle: a Loadout plus the design-local
// composed Parts it references (each carrying a Components list; ADR 0029
// §1). The on-disk form is a Catalog fragment {parts, loadouts:[one]}.
type Design struct {
	Loadout LoadoutDef
	Parts   []Part
}

// ID / Name read the design's identity off its loadout. ID is the stable
// key (and file stem); Name is the display label.
func (d Design) ID() string   { return d.Loadout.ID }
func (d Design) Name() string { return d.Loadout.Name }

// marshal serializes the design as an indented catalog fragment — the
// portable on-disk format (also valid as a modder overlay file).
func (d Design) marshal() ([]byte, error) {
	cat := Catalog{Parts: d.Parts, Loadouts: []LoadoutDef{d.Loadout}}
	return json.MarshalIndent(cat, "", "  ")
}

// designFromCatalog reads a Design out of a parsed catalog fragment. A
// design file must carry exactly one loadout (its own); the parts are its
// composed stages.
func designFromCatalog(cat Catalog) (Design, bool) {
	if len(cat.Loadouts) == 0 {
		return Design{}, false
	}
	return Design{Loadout: cat.Loadouts[0], Parts: cat.Parts}, true
}

// Resolve turns a design into a flyable Loadout, resolved against the LIVE
// catalog (embedded + modder overlay) so its references — both design-local
// composed parts and existing atomic catalog parts — resolve correctly. The
// design's own composed parts are aggregated against the live component
// catalog (ADR 0029 §2). A dangling part reference is skip-bad (a warning +
// an empty Loadout), never a panic, so a hand-edited design file can't crash
// the app.
func (d Design) Resolve() (Loadout, []CatalogWarning) {
	comps, globalParts, _, warnings, err := loadMergedCatalog()
	if err != nil {
		return Loadout{}, append(warnings, CatalogWarning{Path: "designs", Err: err})
	}
	// Aggregate the design's own composed parts against the live components.
	designParts := make(map[string]Part, len(d.Parts))
	for _, p := range d.Parts {
		designParts[p.ID] = p
	}
	designParts, aggWarn := aggregateComponents(designParts, comps)
	warnings = append(warnings, aggWarn...)
	// A composed part that fails to aggregate (unknown component, mixed fuel)
	// is left UNAGGREGATED with zero scalars — resolving it would build a
	// flyable-but-dead ghost stage (0 thrust / 0 mass), and the spawn path
	// only errors on an EMPTY loadout, never a degenerate one. Reject the
	// whole design instead so the caller surfaces the failure rather than
	// spawning dead weight (e.g. a design referencing a since-removed overlay
	// component, or a hand-edited mixed-fuel stage).
	if len(aggWarn) > 0 {
		return Loadout{}, warnings
	}
	// Merge the global catalog parts with the design's design-scoped parts.
	merged := make(map[string]Part, len(globalParts)+len(designParts))
	for id, p := range globalParts {
		merged[id] = p
	}
	for id, p := range designParts {
		merged[id] = p
	}
	// Every loadout ref must resolve; a dangling ref is skip-bad, not a panic
	// (resolveLoadout panics on unknown IDs — fine for the embedded catalog,
	// not for user designs).
	for _, ref := range d.Loadout.Parts {
		if _, ok := merged[ref.PartID]; !ok {
			warnings = append(warnings, CatalogWarning{Path: "design:" + d.ID(), Err: fmt.Errorf("references unknown part %q", ref.PartID)})
			return Loadout{}, warnings
		}
	}
	return resolveLoadout(d.Loadout, merged), warnings
}

// DesignsDir resolves the app-managed designs directory:
// $XDG_CONFIG_HOME/terminal-space-program/designs (or
// ~/.config/terminal-space-program/designs when XDG is unset). A sibling of
// the modder overlay's loadouts/ dir but a DISTINCT namespace (ADR 0029 §4)
// so app-written designs never collide with hand-authored mods.
func DesignsDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "designs")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "terminal-space-program", "designs")
}

// designFileName maps a design ID to its file name, replacing path-unsafe
// runes so a free-form name can never escape the designs dir.
func designFileName(id string) string {
	safe := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', ' ', '.':
			return '-'
		}
		return r
	}, id)
	if safe == "" {
		safe = "design"
	}
	return safe + ".json"
}

// SaveDesign writes a design to the store, creating the dir on first use.
// Overwrites any existing design with the same ID (the VAB's save-over-name).
func SaveDesign(d Design) error {
	dir := DesignsDir()
	if dir == "" {
		return fmt.Errorf("cannot resolve designs directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create designs dir: %w", err)
	}
	data, err := d.marshal()
	if err != nil {
		return fmt.Errorf("serialize design %q: %w", d.ID(), err)
	}
	return os.WriteFile(filepath.Join(dir, designFileName(d.ID())), data, 0o644)
}

// ListDesigns reads every design in the store, skipping malformed files with
// a warning (the ADR 0026 skip-bad-with-warning convention). A missing
// store dir yields no designs and no warnings.
func ListDesigns() ([]Design, []CatalogWarning) {
	dir := DesignsDir()
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, []CatalogWarning{{Path: dir, Err: err}}
	}
	var designs []Design
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
		d, ok := designFromCatalog(cat)
		if !ok {
			warnings = append(warnings, CatalogWarning{Path: path, Err: fmt.Errorf("design file has no loadout")})
			continue
		}
		designs = append(designs, d)
	}
	return designs, warnings
}

// LoadDesign returns the stored design with the given ID, or ok=false if
// none matches. Warnings from other malformed files in the store are
// surfaced so the caller can report them.
func LoadDesign(id string) (Design, bool, []CatalogWarning) {
	designs, warnings := ListDesigns()
	for _, d := range designs {
		if d.ID() == id {
			return d, true, warnings
		}
	}
	return Design{}, false, warnings
}

// DeleteDesign removes a design file from the store. Returns an error if no
// design with that ID exists (so the VAB can report a stale delete).
func DeleteDesign(id string) error {
	dir := DesignsDir()
	if dir == "" {
		return fmt.Errorf("cannot resolve designs directory")
	}
	path := filepath.Join(dir, designFileName(id))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return fmt.Errorf("design %q not found", id)
	}
	return os.Remove(path)
}
