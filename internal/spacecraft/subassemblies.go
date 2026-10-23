package spacecraft

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Subassembly (ADR 0056 decisions 3 and 4) is a named block of one or more
// consecutive stages with its internal Dock Seam and Fuse marks and a role for
// the block as a whole: "payload" (released with Y / U) or "stage" (fires in
// the chain). It is the same catalog fragment a Design is
// ({parts, loadouts:[one]}) plus LoadoutDef.SubassemblyRole.
//
// Five ship embedded under data/subassemblies/. Player-saved ones live in
// $XDG_CONFIG_HOME/terminal-space-program/subassemblies/<slug>.json, a sibling
// of designs/. IDs are their own namespace: they are never merged into
// Loadouts or StageCatalog, so the subassembly "csm-lm" and the skipped part
// "csm-lm" do not collide. A user file wins on ID (ADR 0026's overlay rule).
type Subassembly struct {
	Design
}

const (
	SubassemblyPayload = "payload"
	SubassemblyStage   = "stage"
)

// Role is the block's role, "stage" when the file carries none.
func (s Subassembly) Role() string {
	if s.Loadout.SubassemblyRole == SubassemblyPayload {
		return SubassemblyPayload
	}
	return SubassemblyStage
}

// Shipped reports whether this block is the embedded one (no user file has
// replaced it).
func (s Subassembly) Shipped() bool { return s.Loadout.Source == "embedded" }

// shippedSubassemblyOrder is the order the shipped blocks list in.
var shippedSubassemblyOrder = []string{"comsat", "lm", "csm-lm", "dragon", "cargo-dragon", "orion"}

// SubassembliesDir resolves the player's subassemblies directory, a sibling of
// DesignsDir.
func SubassembliesDir() string {
	if d := DesignsDir(); d != "" {
		return filepath.Join(filepath.Dir(d), "subassemblies")
	}
	return ""
}

func embeddedSubassemblies() []Subassembly {
	entries, err := catalogFS.ReadDir("data/subassemblies")
	if err != nil {
		return nil
	}
	var out []Subassembly
	for _, e := range entries {
		data, err := catalogFS.ReadFile("data/subassemblies/" + e.Name())
		if err != nil {
			continue
		}
		var cat Catalog
		if json.Unmarshal(data, &cat) != nil {
			continue
		}
		if d, ok := designFromCatalog(cat); ok {
			d.Loadout.Source = "embedded"
			out = append(out, Subassembly{d})
		}
	}
	rank := func(id string) int {
		for i, s := range shippedSubassemblyOrder {
			if s == id {
				return i
			}
		}
		return len(shippedSubassemblyOrder)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i].ID()), rank(out[j].ID())
		if ri != rj {
			return ri < rj
		}
		return out[i].ID() < out[j].ID()
	})
	return out
}

// ListSubassemblies returns the shipped blocks followed by the player's, a
// user file replacing a shipped block of the same ID in place. Malformed user
// files are skipped with a warning.
func ListSubassemblies() ([]Subassembly, []CatalogWarning) {
	subs := embeddedSubassemblies()
	dir := SubassembliesDir()
	if dir == "" {
		return subs, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return subs, nil
		}
		return subs, []CatalogWarning{{Path: dir, Err: err}}
	}
	var warnings []CatalogWarning
	var user []Subassembly
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
			warnings = append(warnings, CatalogWarning{Path: path, Err: fmt.Errorf("subassembly file has no loadout")})
			continue
		}
		d.Loadout.Source = "user"
		user = append(user, Subassembly{d})
	}
	sort.SliceStable(user, func(i, j int) bool { return user[i].Name() < user[j].Name() })
	for _, u := range user {
		replaced := false
		for i := range subs {
			if subs[i].ID() == u.ID() {
				subs[i], replaced = u, true
				break
			}
		}
		if !replaced {
			subs = append(subs, u)
		}
	}
	return subs, warnings
}

// SaveSubassembly writes a block to the player's store, overwriting any file
// of the same ID.
func SaveSubassembly(s Subassembly) error {
	dir := SubassembliesDir()
	if dir == "" {
		return fmt.Errorf("cannot resolve subassemblies directory")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create subassemblies dir: %w", err)
	}
	data, err := s.marshal()
	if err != nil {
		return fmt.Errorf("serialize subassembly %q: %w", s.ID(), err)
	}
	return os.WriteFile(filepath.Join(dir, designFileName(s.ID())), data, 0o644)
}

// DeleteSubassembly removes the player's file for id. A shipped block has no
// file to remove; deleting the user file that replaced one restores it.
func DeleteSubassembly(id string) error {
	dir := SubassembliesDir()
	if dir == "" {
		return fmt.Errorf("cannot resolve subassemblies directory")
	}
	path := filepath.Join(dir, designFileName(id))
	if _, err := os.Stat(path); os.IsNotExist(err) {
		for _, s := range embeddedSubassemblies() {
			if s.ID() == id {
				return fmt.Errorf("built-in subassemblies can't be deleted")
			}
		}
		return fmt.Errorf("subassembly %q not found", id)
	}
	return os.Remove(path)
}
