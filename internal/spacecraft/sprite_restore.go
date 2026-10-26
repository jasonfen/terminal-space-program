package spacecraft

import "sort"

// Restoring a stage's launch-sprite dimensions after a load.
//
// A saved Stage does not carry LaunchSpriteRowsPx / Width / Color / HasLegs
// (the save mirrors the stage field by field, and the sprite fields were never
// in the mirror), so a vessel that came back from a save, a session payload or
// a cross-player hand-over had a hull of ZERO rows: no drawn silhouette, and,
// since ADR 0053 derives the nose and the docking port from those rows, a port
// 0.75 m BEHIND the vessel's position, a hull length of 0 and a ghost whose
// shape is "unknown". RestoreSprite puts the dimensions back the way the
// catalog (or, for a parts-built stage the catalog does not know, the mass rule
// that built it) would have given them, keyed like the port lookup:
// (LoadoutID, Name), then a name-only fallback.

type spriteDims struct {
	rows, width int
	diameter    float64
	color       string
	legs        bool
	derived     bool
	fuelType    string
}

func spriteDimsOf(s Stage) spriteDims {
	return spriteDims{
		rows: s.LaunchSpriteRowsPx, width: s.LaunchSpriteWidthPx, diameter: s.DiameterM, color: s.LaunchSpriteColor,
		legs: s.LaunchSpriteHasLegs, derived: s.LaunchSpriteDerived, fuelType: s.FuelType,
	}
}

type spriteIndex struct {
	// byLoadout is authoritative: loadout ID, then the stage's display name.
	byLoadout map[string]map[string]spriteDims
	// byName is the fallback for stages with no loadout behind them (catalog
	// modules, custom VAB stages). Parts that share a name (the Descent
	// stages, the S-IVBs) share a hull, so the first in ID order stands for
	// all of them.
	byName map[string]spriteDims
}

var spriteIdx = buildSpriteIndex()

func buildSpriteIndex() spriteIndex {
	idx := spriteIndex{
		byLoadout: make(map[string]map[string]spriteDims, len(Loadouts)),
		byName:    map[string]spriteDims{},
	}
	for id, l := range Loadouts {
		names := make(map[string]spriteDims, len(l.Stages))
		for _, s := range l.Stages {
			if _, seen := names[s.Name]; !seen {
				names[s.Name] = spriteDimsOf(s)
			}
		}
		idx.byLoadout[id] = names
	}
	ids := make([]string, 0, len(StageCatalog))
	for id := range StageCatalog {
		ids = append(ids, id)
	}
	sort.Strings(ids) // map order must not decide which twin wins
	for _, id := range ids {
		s, ok := BuildStage(id)
		if !ok || s.Name == "" || s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		if _, seen := idx.byName[s.Name]; !seen {
			idx.byName[s.Name] = spriteDimsOf(s)
		}
	}
	return idx
}

func (idx spriteIndex) lookup(loadoutID, name string) (spriteDims, bool) {
	if names, ok := idx.byLoadout[loadoutID]; ok && loadoutID != "" {
		if d, ok := names[name]; ok && d.rows > 0 {
			return d, true
		}
	}
	d, ok := idx.byName[name]
	return d, ok && d.rows > 0
}

// RestoreSprite fills the stage's launch-sprite fields when a load left them
// empty. A stage that already has rows is untouched. Catalog stages get the
// catalog's dimensions; any other stage with mass gets the dimensions the mass
// rule derives (the rule a parts-built stage was given when it was built); a
// massless stage stays undrawn.
func (s *Stage) RestoreSprite() {
	if s.LaunchSpriteRowsPx > 0 {
		return
	}
	if d, ok := spriteIdx.lookup(s.LoadoutID, s.Name); ok {
		s.LaunchSpriteRowsPx, s.LaunchSpriteWidthPx, s.LaunchSpriteColor = d.rows, d.width, d.color
		s.LaunchSpriteHasLegs, s.LaunchSpriteDerived = d.legs, d.derived
		s.DiameterM = d.diameter
		if s.FuelType == "" {
			s.FuelType = d.fuelType
		}
		return
	}
	mass := s.DryMass + s.FuelCapacity
	if mass <= 0 {
		return
	}
	rows, width := derivedSpriteDims(mass)
	s.LaunchSpriteRowsPx, s.LaunchSpriteWidthPx = rows, width
	if s.LaunchSpriteColor == "" {
		s.LaunchSpriteColor = derivedSpriteColor(s.FuelType)
	}
	s.LaunchSpriteDerived = true
}
