// Package settings holds the player's global UI preferences — currently
// the default visibility of each orbit-screen Chip — persisted to a
// standalone settings.json under $XDG_CONFIG_HOME/terminal-space-program/.
//
// This is application preference, not game state: it lives in global
// config (the theme.json precedent), applies across all saves, and is
// deliberately kept out of the save envelope (ADR 0010, rejected
// alternative "Persist prefs in the save file"). It is also kept separate
// from theme.json — colour and visibility are distinct concerns.
//
// The package is pure data + persistence with no UI and no upward
// dependency: the tui reads a Settings value and the Settings screen
// mutates it through SetChip; nothing here imports the tui.
package settings

// Chip identifies one toggle-able orbit-screen Chip — the contextual
// blocks that ADR 0010 moves out of the slim HUD column and onto canvas
// corners. The string value is the stable JSON key in settings.json.
//
// The Navball is intentionally absent: it is a Panel, not a Chip
// (CONTEXT.md §"HUD & overlays"), and whether it gains a Settings toggle
// is a live v0.13 open question. If it does, adding a ChipNavball const
// to AllChips is sufficient — the on-disk map tolerates the new key.
type Chip string

const (
	ChipTarget          Chip = "target"
	ChipStages          Chip = "stages"
	ChipNodes           Chip = "nodes"
	ChipLaunch          Chip = "launch"
	ChipDescent         Chip = "descent"
	ChipChute           Chip = "chute"
	ChipCapture         Chip = "capture"
	ChipOrbitMetrics    Chip = "orbitMetrics"
	ChipFrameTransition Chip = "frameTransition"
	ChipAttitude        Chip = "attitude"
)

// AllChips is the canonical, display-ordered list of toggle-able Chips.
// The Settings screen (slice 3) iterates this — never the underlying map,
// whose iteration order is unspecified — so toggles render in a stable
// order. Append-only: order is part of the UI contract.
var AllChips = []Chip{
	ChipTarget,
	ChipStages,
	ChipNodes,
	ChipLaunch,
	ChipDescent,
	ChipChute,
	ChipCapture,
	ChipOrbitMetrics,
	ChipFrameTransition,
	ChipAttitude,
}

// chipLabels maps each Chip to the human-readable name the Settings
// screen shows. Kept here so the player-facing chip vocabulary lives in
// one place next to the identifiers.
var chipLabels = map[Chip]string{
	ChipTarget:          "Target",
	ChipStages:          "Stages",
	ChipNodes:           "Maneuver nodes",
	ChipLaunch:          "Launch",
	ChipDescent:         "Descent",
	ChipChute:           "Chute",
	ChipCapture:         "Capture preview",
	ChipOrbitMetrics:    "Orbit metrics",
	ChipFrameTransition: "Frame transition",
	ChipAttitude:        "Attitude",
}

// Label returns the display name for c, falling back to the raw key for
// any Chip without an explicit label (so a future const can't silently
// render blank).
func (c Chip) Label() string {
	if l, ok := chipLabels[c]; ok {
		return l
	}
	return string(c)
}

// Settings is the on-disk shape of settings.json. The zero value is a
// valid all-defaults configuration (every Chip visible), which is exactly
// what an absent file represents. A top-level struct (rather than a bare
// map) reserves room for future, non-visibility preferences — units, and
// so on — without a breaking change.
type Settings struct {
	// ChipVisibility records only the Chips the player has explicitly
	// overridden. A Chip absent from the map (or a nil map) is visible by
	// default, so the defaults-all-on behaviour costs zero bytes on disk
	// and unknown keys from a newer build are tolerated and ignored.
	ChipVisibility map[Chip]bool `json:"chips,omitempty"`
}

// Default returns the all-defaults Settings: every Chip visible, no
// overrides recorded. This is the in-memory equivalent of a missing
// settings.json and preserves the pre-ADR-0010 behaviour where every
// block showed.
func Default() Settings {
	return Settings{}
}

// ChipEnabled reports whether Chip c should be shown by default. Absent
// from the override map means visible — so a missing file, a partial
// file, and an unknown key all resolve to the all-on default.
//
// This answers only the Settings half of the slice-2 render rule
// (enabled && relevant && !declutter); relevance and declutter live in
// the tui.
func (s Settings) ChipEnabled(c Chip) bool {
	if s.ChipVisibility == nil {
		return true
	}
	v, ok := s.ChipVisibility[c]
	if !ok {
		return true
	}
	return v
}

// SetChip records an explicit visibility for c, allocating the override
// map on first use. The Settings screen calls this on a toggle, then
// persists via Save.
func (s *Settings) SetChip(c Chip, enabled bool) {
	if s.ChipVisibility == nil {
		s.ChipVisibility = make(map[Chip]bool, len(AllChips))
	}
	s.ChipVisibility[c] = enabled
}
