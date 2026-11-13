// The pad card's `recover:` row (ADR 0069 decisions 12 and 13): where each
// droppable stage flies home from the drop. One-line reason for a new file:
// spawn.go is 1,800 lines and this row is one cohesive field with its own state.

package screens

import (
	"fmt"
	"strconv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// recoverFieldIdx is the form-field index of the `recover:` row. It is reachable
// (Tab includes it) only on a launchpad spawn of a craft with a recoverable stage.
const recoverFieldIdx = 7

// maxShipKmDigits caps the typed ship distance ("3000" is the longest).
const maxShipKmDigits = 4

// SetRecoverLZ injects the lookup the row uses to offer `LZ`: the LZ site nearest
// the pad (its name), or false when none is near. nil (tests, no world) offers
// only ship and none.
// need is how many LZs must be free (0: the site whether or not it has room).
func (s *SpawnCraft) SetRecoverLZ(f func(parentID string, latDeg, lonDeg float64, need int) (name string, ok bool)) {
	s.recoverLZ = f
}

// recoverGroups are the stage indices the row offers a choice for, empty unless
// the position is the launchpad.
func (s *SpawnCraft) recoverGroups() []int {
	if s.posMode != posLaunchpad {
		return nil
	}
	return sim.RecoverableStages(s.selectedCraftStages())
}

// recoverOffered reports whether the row exists for the current craft and position.
func (s *SpawnCraft) recoverOffered() bool { return len(s.recoverGroups()) > 0 }

// recoverHalves is how many LZs group k needs: one a half, two for a booster pair.
func (s *SpawnCraft) recoverHalves(k int) int {
	if groups, stages := s.recoverGroups(), s.selectedCraftStages(); k >= 0 && k < len(groups) {
		return sim.StageHalves(stages[groups[k]])
	}
	return 1
}

// recoverLZName is the LZ site the `LZ` choice would resolve to for group k at
// the selected launch site with room for its halves, "" when none is near or the
// site is full (k < 0: the site whether or not it has room).
func (s *SpawnCraft) recoverLZName(k int) string {
	if s.recoverLZ == nil || s.latIdx < 0 || s.latIdx >= len(sim.LaunchSites) {
		return ""
	}
	site := sim.LaunchSites[s.latIdx]
	need := 0
	if k >= 0 {
		need = s.recoverHalves(k)
	}
	if name, ok := s.recoverLZ(s.SelectedParentID(), site.LatitudeDeg, site.LongitudeEastDeg, need); ok {
		return name
	}
	return ""
}

// recoverPick is group k's choice: what the player set, else the default (the
// first stage goes to the LZ when there is one, the next to a ship).
func (s *SpawnCraft) recoverPick(k int) spacecraft.RecoverPick {
	groups := s.recoverGroups()
	if k < 0 || k >= len(groups) {
		return spacecraft.RecoverPick{}
	}
	if p, ok := s.recoverSet[groups[k]]; ok && s.recoverModeOffered(k, p.Mode) {
		return p
	}
	lz := s.recoverLZName(k) != ""
	switch {
	case k == 0 && lz:
		return spacecraft.RecoverPick{Mode: spacecraft.RecoverLZ}
	case k > 0 && lz:
		return spacecraft.RecoverPick{Mode: spacecraft.RecoverShip, ShipKm: sim.RecoveryShipDefaultKm}
	}
	return spacecraft.RecoverPick{}
}

// recoverModeOffered: LZ needs a site near the pad.
func (s *SpawnCraft) recoverModeOffered(k int, m spacecraft.RecoverMode) bool {
	return m != spacecraft.RecoverLZ || s.recoverLZName(k) != ""
}

// SelectedRecover is the row's choices indexed by stage, for the spawn spec;
// nil off the pad or when the craft has nothing to recover.
func (s *SpawnCraft) SelectedRecover() []spacecraft.RecoverPick {
	groups := s.recoverGroups()
	if len(groups) == 0 {
		return nil
	}
	out := make([]spacecraft.RecoverPick, groups[len(groups)-1]+1)
	for k, i := range groups {
		out[i] = s.recoverPick(k)
	}
	return out
}

// recoverLabel names group k on the row: boosters for a marked stage, else core.
func (s *SpawnCraft) recoverLabel(k int) string {
	groups := s.recoverGroups()
	if stages := s.selectedCraftStages(); k < len(groups) && stages[groups[k]].LitWithAbove {
		return "boosters"
	}
	return "core"
}

// recoverModes are the choices the row cycles, in order.
func (s *SpawnCraft) recoverModes(k int) []spacecraft.RecoverMode {
	if s.recoverLZName(k) != "" {
		return []spacecraft.RecoverMode{spacecraft.RecoverLZ, spacecraft.RecoverShip, spacecraft.RecoverNone}
	}
	return []spacecraft.RecoverMode{spacecraft.RecoverShip, spacecraft.RecoverNone}
}

// stepRecover moves the focused group's choice through the modes.
func (s *SpawnCraft) stepRecover(step int) {
	groups := s.recoverGroups()
	if len(groups) == 0 {
		return
	}
	s.recCur = min(s.recCur, len(groups)-1)
	p := s.recoverPick(s.recCur)
	modes := s.recoverModes(s.recCur)
	at := 0
	for i, m := range modes {
		if m == p.Mode {
			at = i
		}
	}
	p.Mode = modes[wrapIdx(at+step, len(modes))]
	if p.Mode == spacecraft.RecoverShip && p.ShipKm <= 0 {
		p.ShipKm = sim.RecoveryShipDefaultKm
	}
	if s.recoverSet == nil {
		s.recoverSet = map[int]spacecraft.RecoverPick{}
	}
	s.recoverSet[groups[s.recCur]] = p
}

// handleRecoverInputKey drives the ship-distance box: digits and backspace edit,
// enter keeps, esc leaves the old number. Like the ALTITUDE box it can never
// launch the form or cancel it.
func (s *SpawnCraft) handleRecoverInputKey(key string) SpawnAction {
	switch key {
	case "esc":
		s.recEditing, s.recInput, s.recLeftBox = false, "", true
	case "enter":
		if km, err := strconv.Atoi(s.recInput); err == nil && km > 0 {
			groups := s.recoverGroups()
			if s.recCur < len(groups) {
				p := s.recoverPick(s.recCur)
				p.Mode, p.ShipKm = spacecraft.RecoverShip, float64(min(km, int(sim.RecoveryShipMaxKm)))
				if s.recoverSet == nil {
					s.recoverSet = map[int]spacecraft.RecoverPick{}
				}
				s.recoverSet[groups[s.recCur]] = p
			}
		}
		s.recEditing, s.recInput, s.recLeftBox = false, "", true
	case "backspace":
		if n := len(s.recInput); n > 0 {
			s.recInput = s.recInput[:n-1]
		}
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && len(s.recInput) < maxShipKmDigits {
			s.recInput += key
		}
	}
	return SpawnActionNone
}

// recoverLines is the `recover:` row and the note under it.
func (s *SpawnCraft) recoverLines() []string {
	groups := s.recoverGroups()
	focused := s.fieldIdx == recoverFieldIdx
	var parts []string
	hasLZ := false
	for k := range groups {
		p := s.recoverPick(k)
		var v string
		switch p.Mode {
		case spacecraft.RecoverLZ:
			v, hasLZ = "LZ", true
		case spacecraft.RecoverShip:
			v = fmt.Sprintf("ship %.0f km", p.ShipKm)
			if focused && s.recEditing && k == s.recCur {
				v = fmt.Sprintf("ship [%s_] km", s.recInput)
			}
		default:
			v = "none"
		}
		tok := s.recoverLabel(k) + " " + v
		if focused && k == s.recCur {
			tok = s.theme.Warning.Render("‹ " + tok + " ›")
		}
		parts = append(parts, tok)
	}
	// one stage a line: the box is 38 cells inside and "boosters LZ   core ship 550 km" is 40
	lines := []string{"  recover:  " + parts[0]}
	for _, p := range parts[1:] {
		lines = append(lines, "            "+p)
	}
	if hasLZ {
		lines = append(lines, "  "+s.theme.Dim.Render("LZ: "+s.recoverLZName(-1)))
	} else if name := s.recoverLZName(-1); name != "" && s.recoverLZName(0) == "" {
		lines = append(lines, "  "+s.theme.Dim.Render(name+": no free LZ")) // R2: a full site is not offered
	}
	return lines
}
