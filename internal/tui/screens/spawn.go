package screens

import (
	"fmt"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// SpawnCraft is the modal form opened by `n` on the orbit screen.
// v0.8.2+: four fields — craft type, parent body, altitude, and
// direction (prograde / retrograde). v0.8.3+ added a POSITION
// toggle (orbit / alongside). v0.9.2+ extended POSITION with a
// third option (launchpad) and the ALTITUDE field doubles as a
// LATITUDE picker when launchpad is selected. Tab cycles field
// focus; ←/→ edit the focused field; Enter spawns; Esc cancels.
type SpawnCraft struct {
	theme    Theme
	fieldIdx int // 0=loadout, 1=position, 2=parent, 3=alt/lat, 4=direction, 5=stack(custom only)

	loadoutIdx   int
	posMode      spawnPosMode           // v0.9.2+: tri-state — orbit / alongside / launchpad
	parentBodies []bodies.CelestialBody // populated by Reset
	parentIdx    int
	altIdx       int
	latIdx       int // v0.9.2+: latitude preset cursor when posMode=launchpad
	retrograde   bool

	// v0.10.1+ stack configurator. loadoutIdx == len(LoadoutOrder)
	// is the synthetic "Custom…" entry; when it's selected a STACK
	// field (idx 5) becomes reachable. customStages is the working
	// stack bottom-first (Loadout.Stages convention); partIdx is the
	// catalog part-picker cursor over StageCatalogOrder.
	customStages []spacecraft.Stage
	partIdx      int

	// nosePayloadCount (v0.14 / ADR 0011) is the Dock Seam: how many
	// contiguous TOP stages of customStages form a docked nose payload
	// (released by Undock, not Staging) rather than linear firing-core
	// stages. 0 ⇒ a plain linear craft (the historical default). [d]
	// cycles it; adding the composite "CSM+LM" pick pre-sets it to the
	// LM's stage count. Clamped to [0, len-1] so the core keeps ≥1 stage.
	nosePayloadCount int

	// designs (v0.24 / ADR 0029) are the saved VAB designs offered as CRAFT
	// TYPE rows AFTER the synthetic "Custom…" entry — the "design once,
	// launch many" split (the VAB is the design surface, the spawn form the
	// launch surface). Injected at Reset by the App (spacecraft.ListDesigns)
	// so the form has no filesystem side effects and tests stay isolated.
	designs []spacecraft.Design

	// systemScale (v0.24 / ADR 0031 / S10) is the active System's Scale Class,
	// injected at Reset. Catalog loadouts whose Scale() differs are hidden from
	// the CRAFT TYPE picker by default (real fleet in Sol, stripped-back fleet
	// in Lumen) — amending ADR 0014's no-filter rule. The empty value
	// normalizes to ScaleReal, so a system without a Scale Class shows the real
	// fleet.
	systemScale bodies.ScaleClass
	// showAll (v0.24 / ADR 0031 / S10) is the opt-out: when true the scale
	// filter is off and every catalog loadout is listed (ADR 0014's
	// spawn-anywhere escape hatch, now an explicit toggle). Reset to false on
	// each open; flipped by the [f] key.
	showAll bool
}

// stackFieldIdx is the form-field index of the STACK editor — only
// reachable (Tab includes it) when the Custom loadout is selected.
const stackFieldIdx = 5

// spawnPosMode enumerates the v0.8.3 / v0.9.2 spawn-position modes.
type spawnPosMode int

const (
	posOrbit     spawnPosMode = iota // pre-v0.8.3 default — circular orbit
	posAlongside                     // v0.8.3+ — within docking gate of active craft
	posLaunchpad                     // v0.9.2+ — surface co-rotating, altitude 0
)

// altitudePresets are the cycle values for the altitude field —
// km above the parent's mean radius. Hand-picked across orders of
// magnitude so the cycle covers LEO-style parking orbits, GEO-ish
// transfer alts, and high-Earth / interplanetary capture orbits.
var altitudePresets = []int{200, 500, 1000, 2000, 5000, 10000, 35786}

// The form's LATITUDE field cycles through the shared named-site list
// sim.LaunchSites (v0.17: hoisted to internal/sim so the form and the
// --launch-site CLI flag resolve the same set). Picking "Cape Canaveral"
// lands the craft on KSC LC-39A, not just at "the right latitude."

// NewSpawnCraft constructs the screen.
func NewSpawnCraft(th Theme) *SpawnCraft { return &SpawnCraft{theme: th} }

// Reset prepares the form for a fresh open. systemBodies is the
// list of bodies in the active system; defaultParentID is the body
// the parent-field cursor lands on initially (typically the active
// craft's current primary). v0.8.2+: replaces the v0.8.2-pre
// no-arg Reset.
func (s *SpawnCraft) Reset(systemBodies []bodies.CelestialBody, defaultParentID string, designs []spacecraft.Design, systemScale bodies.ScaleClass) {
	s.fieldIdx = 0
	s.loadoutIdx = 0
	s.customStages = nil
	s.partIdx = 0
	s.nosePayloadCount = 0
	s.designs = designs
	s.systemScale = systemScale
	s.showAll = false
	s.posMode = posOrbit
	s.altIdx = 1 // 500 km — matches the v0.8.1 sister-spawn default
	s.latIdx = 1 // 28.6° KSC — matches the v0.9.2 launchpad default
	s.retrograde = false
	s.parentBodies = systemBodies
	s.parentIdx = 0
	for i, b := range systemBodies {
		if b.ID == defaultParentID {
			s.parentIdx = i
			break
		}
	}
}

// SpawnAction enumerates the form's outcomes.
type SpawnAction int

const (
	SpawnActionNone    SpawnAction = iota
	SpawnActionCancel              // esc
	SpawnActionConfirm             // enter — caller reads accessors
)

// visibleCatalogCount is the number of catalog loadout rows currently shown —
// after the scale-class system filter (ADR 0031 / S10). The Custom entry sits
// at this index and saved designs follow it, so all the CRAFT TYPE index math
// keys off this, NOT len(LoadoutOrder) (which is the unfiltered total).
func (s *SpawnCraft) visibleCatalogCount() int {
	return len(s.orderedLoadoutIDs())
}

// loadoutChoiceCount is the number of selectable CRAFT TYPE rows — the visible
// catalog loadouts (after the system filter), the synthetic "Custom…" entry,
// then every saved design (v0.24). Headers are not counted (ADR 0031 / S8/S10).
func (s *SpawnCraft) loadoutChoiceCount() int {
	return s.visibleCatalogCount() + 1 + len(s.designs)
}

// loadoutVisible reports whether a catalog loadout is shown given the active
// system's Scale Class and the show-all toggle (ADR 0031 / S10): with the
// filter on, only loadouts whose Scale() matches the system are shown; show-all
// lists everything. The empty system scale normalizes to ScaleReal.
func (s *SpawnCraft) loadoutVisible(id string) bool {
	return s.showAll || spacecraft.Loadouts[id].Scale() == s.systemScale.Normalize()
}

// craftCategory is one CRAFT TYPE display group (ADR 0031 / S8): a stable key
// authored on each Loadout via the catalog `category` field, and the header
// label shown in the spawn form. The slice order below IS the on-screen group
// order; the key→label→order mapping is a fixed UI table, not data.
type craftCategory struct {
	key   string
	label string
}

var craftCategories = []craftCategory{
	{"launch-vehicles", "Launch Vehicles"},
	{"mission-stacks", "Crewed Mission Stacks"},
	{"upper-stages", "Upper Stages"},
	{"landers-capsules", "Landers & Capsules"},
	{"tugs-relays", "Tugs & Relays"},
	{"satellites-payloads", "Satellites & Payloads"},
}

// craftCategoryOtherLabel is the trailing bucket for loadouts whose Category is
// empty or matches no known key — so a future / mod-authored loadout never
// vanishes from the picker (ADR 0031).
const craftCategoryOtherLabel = "Other"

// loadoutGroup is a rendered CRAFT TYPE category: a header label and the
// catalog loadout IDs under it, in display order.
type loadoutGroup struct {
	label string
	ids   []string
}

// groupedLoadouts arranges the VISIBLE catalog loadouts into ordered category
// groups — the spawn form's CRAFT TYPE display and cursor order (ADR 0031).
// Visibility is the scale-class system filter (S10): with the filter on, only
// loadouts matching the active system's scale appear; show-all lists every
// loadout. Within a group, loadouts keep LoadoutOrder order. A loadout whose
// Category matches no known key is collected into a trailing "Other" group, so
// the flattened result is a permutation of the visible set (each appears once).
// Empty groups are omitted.
func (s *SpawnCraft) groupedLoadouts() []loadoutGroup {
	known := make(map[string]bool, len(craftCategories))
	groups := make([]loadoutGroup, 0, len(craftCategories)+1)
	for _, c := range craftCategories {
		known[c.key] = true
		var ids []string
		for _, id := range spacecraft.LoadoutOrder {
			if spacecraft.Loadouts[id].Category == c.key && s.loadoutVisible(id) {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			groups = append(groups, loadoutGroup{label: c.label, ids: ids})
		}
	}
	var other []string
	for _, id := range spacecraft.LoadoutOrder {
		if !known[spacecraft.Loadouts[id].Category] && s.loadoutVisible(id) {
			other = append(other, id)
		}
	}
	if len(other) > 0 {
		groups = append(groups, loadoutGroup{label: craftCategoryOtherLabel, ids: other})
	}
	return groups
}

// orderedLoadoutIDs is the flattened grouped catalog order — the visible loadout
// IDs in the sequence the CRAFT TYPE cursor walks. The index into this slice is
// loadoutIdx for the catalog rows (0 .. len-1); the Custom entry and saved
// designs follow at visibleCatalogCount() and beyond.
func (s *SpawnCraft) orderedLoadoutIDs() []string {
	ids := make([]string, 0, len(spacecraft.LoadoutOrder))
	for _, g := range s.groupedLoadouts() {
		ids = append(ids, g.ids...)
	}
	return ids
}

// selectedDesign returns the saved design under the cursor, or nil when a
// design row is not selected. v0.24 / ADR 0029.
func (s *SpawnCraft) selectedDesign() *spacecraft.Design {
	if !s.IsDesignSelected() {
		return nil
	}
	i := s.loadoutIdx - s.visibleCatalogCount() - 1
	if i < 0 || i >= len(s.designs) {
		return nil
	}
	return &s.designs[i]
}

// craftLiftsOff reports whether a craft with the given bottom-stage thrust (N)
// and total wet mass (kg) can lift off body — its surface TWR ≥ 1, i.e. thrust
// ≥ weight = m·g with g = μ/R² (ADR 0031 / S9, the physics surface-launch
// gate). A nil body or non-positive mass/radius reads as "can't lift off".
func craftLiftsOff(bottomThrustN, wetMassKg float64, body *bodies.CelestialBody) bool {
	if body == nil || wetMassKg <= 0 {
		return false
	}
	r := body.RadiusMeters()
	if r <= 0 {
		return false
	}
	g := body.GravitationalParameter() / (r * r)
	return bottomThrustN >= wetMassKg*g
}

// selectedCraftStages returns the bottom-first stages of the currently selected
// craft — a catalog loadout, the Custom stack, or a saved design (resolved
// against the live catalog) — for the launch gate. Empty when no craft / no
// stages are determinable.
func (s *SpawnCraft) selectedCraftStages() []spacecraft.Stage {
	switch {
	case s.IsCustomSelected():
		return s.customStages
	case s.IsDesignSelected():
		if d := s.selectedDesign(); d != nil {
			l, _ := d.Resolve()
			return l.Stages
		}
		return nil
	default:
		return spacecraft.LookupLoadout(s.SelectedLoadoutID()).Stages
	}
}

// launchpadAllowed reports whether POSITION=launchpad is offered for the
// current craft + parent: the craft must lift off the selected parent (surface
// TWR ≥ 1; ADR 0031 / S9). Permissive when the craft's stages can't be
// determined (e.g. an empty Custom stack) — the gate never blocks spuriously.
func (s *SpawnCraft) launchpadAllowed() bool {
	stages := s.selectedCraftStages()
	if len(stages) == 0 {
		return true
	}
	wet := spacecraft.SumDryMass(stages) + spacecraft.SumFuelMass(stages)
	return craftLiftsOff(stages[0].Thrust, wet, s.currentParent())
}

// IsCustomSelected reports whether the cursor is on the synthetic
// "Custom…" CRAFT TYPE entry — the row right after the visible catalog
// loadouts (the index shifts with the system filter; ADR 0031 / S10). v0.10.1+.
func (s *SpawnCraft) IsCustomSelected() bool {
	return s.loadoutIdx == s.visibleCatalogCount()
}

// IsDesignSelected reports whether the cursor is on a saved-design row (the
// rows after "Custom…"). v0.24 / ADR 0029.
func (s *SpawnCraft) IsDesignSelected() bool {
	v := s.visibleCatalogCount()
	return s.loadoutIdx > v && s.loadoutIdx <= v+len(s.designs)
}

// SelectedDesignID returns the saved design's ID under the cursor, or "" when
// a design row is not selected. v0.24 / ADR 0029.
func (s *SpawnCraft) SelectedDesignID() string {
	if !s.IsDesignSelected() {
		return ""
	}
	return s.designs[s.loadoutIdx-s.visibleCatalogCount()-1].ID()
}

// SelectedLoadoutID returns the loadout ID for the current cursor, or "" when
// the synthetic Custom entry or a saved design is selected (the caller then
// reads SelectedCustomStages / SelectedDesignID instead). v0.10.1+.
func (s *SpawnCraft) SelectedLoadoutID() string {
	if s.IsCustomSelected() || s.IsDesignSelected() {
		return ""
	}
	// Map loadoutIdx through the grouped, filtered display order — the cursor
	// walks the visible grouped sequence (ADR 0031 / S8/S10).
	ids := s.orderedLoadoutIDs()
	if len(ids) == 0 {
		return ""
	}
	if s.loadoutIdx < 0 || s.loadoutIdx >= len(ids) {
		return ids[0]
	}
	return ids[s.loadoutIdx]
}

// SelectedCustomStages returns a copy of the player-assembled stack
// (bottom-first), or nil when Custom is not selected. The spawn
// path treats a nil/empty result as "no custom craft". v0.10.1+.
func (s *SpawnCraft) SelectedCustomStages() []spacecraft.Stage {
	if !s.IsCustomSelected() || len(s.customStages) == 0 {
		return nil
	}
	out := make([]spacecraft.Stage, len(s.customStages))
	copy(out, s.customStages)
	return out
}

// CustomStackEmpty reports Custom-selected-but-no-stages — the one
// confirm state the caller must reject (an empty stack is not a
// spawnable craft). v0.10.1+.
func (s *SpawnCraft) CustomStackEmpty() bool {
	return s.IsCustomSelected() && len(s.customStages) == 0
}

// SelectedParentID returns the body ID the cursor is on, or empty
// if the body list is unset (caller falls back to the active
// craft's primary).
func (s *SpawnCraft) SelectedParentID() string {
	if s.parentIdx < 0 || s.parentIdx >= len(s.parentBodies) {
		return ""
	}
	return s.parentBodies[s.parentIdx].ID
}

// SelectedAltitudeM returns the chosen altitude above the parent's
// mean radius (m).
func (s *SpawnCraft) SelectedAltitudeM() float64 {
	if s.altIdx < 0 || s.altIdx >= len(altitudePresets) {
		return 500e3
	}
	return float64(altitudePresets[s.altIdx]) * 1000
}

// SelectedRetrograde reports whether the player picked retrograde.
func (s *SpawnCraft) SelectedRetrograde() bool { return s.retrograde }

// SelectedAlongside reports whether the player picked the
// "alongside active craft" position. v0.8.3+.
func (s *SpawnCraft) SelectedAlongside() bool { return s.posMode == posAlongside }

// SelectedLaunchpad reports whether the player picked the
// surface-launchpad position. v0.9.2+.
func (s *SpawnCraft) SelectedLaunchpad() bool { return s.posMode == posLaunchpad }

// SelectedLatitudeDeg returns the chosen surface latitude (degrees
// north) when SelectedLaunchpad is true. Defaults to KSC LC-39A
// (28.6083°N) when the cursor is out of range. v0.9.2+.
func (s *SpawnCraft) SelectedLatitudeDeg() float64 {
	if s.latIdx < 0 || s.latIdx >= len(sim.LaunchSites) {
		return 28.6083
	}
	return sim.LaunchSites[s.latIdx].LatitudeDeg
}

// SelectedLongitudeEastDeg returns the chosen surface longitude
// offset (degrees east of pseudo-Greenwich). Defaults to KSC
// (-80.604°E) when the cursor is out of range. v0.9.2+.
func (s *SpawnCraft) SelectedLongitudeEastDeg() float64 {
	if s.latIdx < 0 || s.latIdx >= len(sim.LaunchSites) {
		return -80.604
	}
	return sim.LaunchSites[s.latIdx].LongitudeEastDeg
}

// fieldOrder returns the field indices in visual (top-to-bottom) order,
// which is also the Tab order. The STACK editor (stackFieldIdx) renders
// directly below CRAFT TYPE (idx 0) and only exists while Custom is
// selected, so it slots in right after 0 — not at the end where its
// numeric index would otherwise put it. (Tabbing by numeric index made
// Tab from Custom jump past the visually-adjacent STACK picker to
// POSITION, only reaching STACK on the 5th tab — the reported bug.)
func (s *SpawnCraft) fieldOrder() []int {
	if s.IsCustomSelected() {
		return []int{0, stackFieldIdx, 1, 2, 3, 4}
	}
	return []int{0, 1, 2, 3, 4}
}

// HandleKey maps a raw key string to a SpawnAction. Tab cycles
// fields; ←/→ edit the focused field; Enter commits; Esc cancels.
func (s *SpawnCraft) HandleKey(key string) SpawnAction {
	// Navigation follows fieldOrder (visual order). Locate the current
	// field in it; if the focus is no longer reachable — e.g. the player
	// cycled off Custom while parked on STACK — snap back to CRAFT TYPE so
	// a stale idx can't strand the cursor.
	order := s.fieldOrder()
	cur := 0
	found := false
	for i, f := range order {
		if f == s.fieldIdx {
			cur, found = i, true
			break
		}
	}
	if !found {
		s.fieldIdx, cur = order[0], 0
	}
	switch key {
	case "esc":
		return SpawnActionCancel
	case "enter":
		return SpawnActionConfirm
	case "tab", "down":
		s.fieldIdx = order[(cur+1)%len(order)]
	case "shift+tab", "up":
		s.fieldIdx = order[(cur-1+len(order))%len(order)]
	case "left", "h":
		s.cycleField(-1)
	case "right", "l":
		s.cycleField(+1)
	case "a":
		// Add the picked catalog module on top of the working stack.
		// Form-local: only meaningful on the STACK field. A module is
		// usually one stage; the "lander" pick expands to the 2-stage LM
		// (Descent + Ascent) so it lands as one vessel.
		if s.fieldIdx == stackFieldIdx && s.IsCustomSelected() {
			if id := s.pickedPartID(); id != "" {
				if stages, ok := spacecraft.BuildModule(id); ok {
					s.customStages = append(s.customStages, stages...)
					// v0.14 / ADR 0011: a composite pick (CSM+LM) marks its
					// own top stages as the docked nose payload, so the
					// player gets the assembled composite without setting
					// the seam by hand. Clamped below.
					if top := spacecraft.ModuleNosePayloadTop(id); top > 0 {
						s.nosePayloadCount = top
					}
					s.clampNosePayload()
				}
			}
		}
	case "x":
		// Remove the top (last-added) stage.
		if s.fieldIdx == stackFieldIdx && s.IsCustomSelected() && len(s.customStages) > 0 {
			s.customStages = s.customStages[:len(s.customStages)-1]
			s.clampNosePayload()
		}
	case "d":
		// v0.14 / ADR 0011: cycle the Dock Seam — how many TOP stages form
		// the docked nose payload. Walks 0 (linear) → 1 → … → len-1 → 0,
		// keeping the core at ≥1 stage.
		if s.fieldIdx == stackFieldIdx && s.IsCustomSelected() && len(s.customStages) > 1 {
			s.nosePayloadCount = (s.nosePayloadCount + 1) % len(s.customStages)
		}
	case "f":
		// ADR 0031 / S10: toggle the scale-class system filter (filter to the
		// current system ↔ show all systems' craft). Re-point to the top of the
		// freshly-filtered list and focus CRAFT TYPE, so the changed list reads
		// clearly and the cursor can't strand on a now-hidden row or the STACK
		// field (which is unreachable once the cursor leaves Custom).
		s.showAll = !s.showAll
		s.loadoutIdx = 0
		s.fieldIdx = 0
	}
	return SpawnActionNone
}

// clampNosePayload keeps the Dock Seam in range after the stack changes:
// the nose payload may take at most len-1 stages (the core keeps ≥1), and
// an empty/1-stage stack can't have a seam. v0.14 / ADR 0011.
func (s *SpawnCraft) clampNosePayload() {
	if len(s.customStages) < 2 {
		s.nosePayloadCount = 0
		return
	}
	if s.nosePayloadCount > len(s.customStages)-1 {
		s.nosePayloadCount = len(s.customStages) - 1
	}
	if s.nosePayloadCount < 0 {
		s.nosePayloadCount = 0
	}
}

// SelectedNosePayloadPlan returns the Dock Seam as a top-release group
// list for SpawnSpec.NosePayloadPlan (ADR 0011), or nil when no seam is
// set / Custom isn't selected — i.e. a plain linear custom craft. The
// single-entry list mirrors a Loadout's bottom-up DecouplePlan. v0.14.
func (s *SpawnCraft) SelectedNosePayloadPlan() []int {
	if !s.IsCustomSelected() || s.nosePayloadCount <= 0 {
		return nil
	}
	if s.nosePayloadCount >= len(s.customStages) {
		return nil // would leave the core empty — treat as linear
	}
	return []int{s.nosePayloadCount}
}

// pickedPartID returns the catalog ID under the part-picker cursor.
func (s *SpawnCraft) pickedPartID() string {
	if s.partIdx < 0 || s.partIdx >= len(spacecraft.StageCatalogOrder) {
		return ""
	}
	return spacecraft.StageCatalogOrder[s.partIdx]
}

// cycleField nudges the focused field's value by step (typically
// ±1). Each field has its own wrap-around behaviour. v0.8.3+:
// added the position toggle (orbit / alongside). v0.9.2+: position
// is now a tri-state cycle (orbit / alongside / launchpad), and
// field 3 doubles as latitude when posMode=launchpad.
func (s *SpawnCraft) cycleField(step int) {
	switch s.fieldIdx {
	case 0:
		// +1 row for "Custom…", + the saved designs (v0.24). v0.10.1+.
		s.loadoutIdx = wrapIdx(s.loadoutIdx+step, s.loadoutChoiceCount())
	case stackFieldIdx:
		// STACK field: ←/→ moves the catalog part-picker cursor.
		s.partIdx = wrapIdx(s.partIdx+step, len(spacecraft.StageCatalogOrder))
	case 1:
		s.posMode = spawnPosMode(wrapIdx(int(s.posMode)+step, 3))
		// ADR 0031 / S9: skip launchpad in the cycle when the selected craft
		// can't lift off the selected parent (one extra step in the same
		// direction — only launchpad is gated, so one suffices).
		if s.posMode == posLaunchpad && !s.launchpadAllowed() {
			s.posMode = spawnPosMode(wrapIdx(int(s.posMode)+step, 3))
		}
	case 2:
		if len(s.parentBodies) > 0 {
			s.parentIdx = wrapIdx(s.parentIdx+step, len(s.parentBodies))
		}
	case 3:
		if s.posMode == posLaunchpad {
			s.latIdx = wrapIdx(s.latIdx+step, len(sim.LaunchSites))
		} else {
			s.altIdx = wrapIdx(s.altIdx+step, len(altitudePresets))
		}
	case 4:
		s.retrograde = !s.retrograde
	}
	// ADR 0031 / S9: a launchpad selection the new craft/parent can't support
	// (after cycling CRAFT TYPE or PARENT) snaps back to orbit, so the form
	// never confirms a pad spawn for a craft that can't lift off.
	if s.posMode == posLaunchpad && !s.launchpadAllowed() {
		s.posMode = posOrbit
	}
}

func wrapIdx(i, n int) int {
	if n <= 0 {
		return 0
	}
	for i < 0 {
		i += n
	}
	return i % n
}

// Render returns the modal form. Width is the terminal width.
func (s *SpawnCraft) Render(width int) string {
	var lines []string

	const titleText = "terminal-space-program — spawn craft"
	lines = append(lines, s.theme.Title.Render(titleText))
	lines = append(lines, "")

	// Field 0: craft type — catalog loadouts grouped under category headers
	// (ADR 0031 / S8), then a trailing "Custom & Designs" group with the
	// synthetic "Custom…" entry and any saved VAB designs. Headers are
	// non-selectable; the cursor (loadoutIdx) walks only the selectable rows,
	// which follow groupedLoadouts()'s flattened order — so `idx` below tracks
	// the running selectable index and the catalog rows end exactly at
	// len(LoadoutOrder) (the Custom index), keeping the index arithmetic in
	// IsCustomSelected / IsDesignSelected unchanged.
	lines = append(lines, s.fieldHeader(0, "CRAFT TYPE"))
	// ADR 0031 / S10: the scale-class system filter note + [f] hint.
	if s.showAll {
		lines = append(lines, "  "+s.theme.Dim.Render(
			"showing all systems' craft — [f] filter to this system"))
	} else if hidden := len(spacecraft.LoadoutOrder) - s.visibleCatalogCount(); hidden > 0 {
		lines = append(lines, "  "+s.theme.Dim.Render(fmt.Sprintf(
			"%d craft from other systems hidden — [f] show all", hidden)))
	}
	lines = append(lines, "")
	idx := 0
	for _, g := range s.groupedLoadouts() {
		lines = append(lines, "  "+s.theme.Primary.Render(g.label))
		for _, id := range g.ids {
			l := spacecraft.Loadouts[id]
			row := fmt.Sprintf("%s %s  %s  %s  — %s",
				l.Glyph, l.Name, crewTag(l), l.Role, propulsionSummary(l))
			lines = append(lines, s.craftRow(idx, row))
			idx++
		}
	}
	// Trailing "Custom & Designs" group — never filtered (ADR 0031). idx is
	// now len(LoadoutOrder), so the Custom row lands on the Custom index.
	lines = append(lines, "  "+s.theme.Primary.Render("Custom & Designs"))
	lines = append(lines, s.craftRow(idx, "✎ Custom…  build-your-own  — assemble a stage stack"))
	idx++
	for _, d := range s.designs {
		lines = append(lines, s.craftRow(idx,
			fmt.Sprintf("✎ %s  saved design  — %d stages", d.Name(), len(d.Loadout.Parts))))
		idx++
	}

	// v0.10.1+ STACK editor — only when Custom is selected. Shows the
	// working stack bottom→top, the catalog part-picker, and the
	// add/remove key hints. Field idx 5 (stackFieldIdx).
	if s.IsCustomSelected() {
		lines = append(lines, "")
		lines = append(lines, s.fieldHeader(stackFieldIdx, "STACK (bottom → top)"))
		if len(s.customStages) == 0 {
			lines = append(lines, "  "+s.theme.Dim.Render("(empty — pick a part below and press [a] to add)"))
		} else {
			// v0.14 / ADR 0011: the Dock Seam splits the stack into the
			// linear firing core (bottom) and the docked nose payload (top
			// nosePayloadCount stages). seam == len means no seam (linear).
			seam := len(s.customStages) - s.nosePayloadCount
			for i := len(s.customStages) - 1; i >= 0; i-- {
				if s.nosePayloadCount > 0 && i == seam-1 {
					lines = append(lines, "  "+s.theme.Warning.Render(
						"── dock seam ──  (above = nose payload, [U]ndock to release)"))
				}
				st := s.customStages[i]
				var tag string
				switch {
				case s.nosePayloadCount > 0 && i >= seam:
					tag = "nose payload"
				case i == 0:
					tag = "bottom/fires first"
				case s.nosePayloadCount > 0 && i == seam-1:
					tag = "core survivor"
				case i == len(s.customStages)-1:
					tag = "top/core"
				default:
					tag = "mid"
				}
				eng := fmt.Sprintf("%.0fkN @ %.0fs", st.Thrust/1000, st.Isp)
				if st.Thrust == 0 {
					eng = "RCS-only"
				}
				lines = append(lines, "  "+s.theme.Primary.Render(
					fmt.Sprintf("%s %-7s  dry %.0fkg fuel %.0fkg  %s  (%s)",
						st.Glyph, st.Name, st.DryMass, st.FuelMass, eng, tag)))
			}
		}
		// Catalog part-picker line.
		lines = append(lines, "")
		pid := s.pickedPartID()
		if m, ok := spacecraft.StageCatalog[pid]; ok {
			// Show combined mass/engine for the module the pick contributes —
			// a multi-stage module (the 2-stage lander) reads as one unit
			// with its bottom stage's engine firing first.
			stages, _ := spacecraft.BuildModule(pid)
			name := m.Name
			eng := "RCS-only"
			if len(stages) > 0 && stages[0].Thrust > 0 {
				eng = fmt.Sprintf("%.0fkN @ %.0fs", stages[0].Thrust/1000, stages[0].Isp)
			}
			if len(stages) > 1 {
				name = fmt.Sprintf("%s (%d-stage)", m.Name, len(stages))
			}
			pickLabel := fmt.Sprintf("%s %s  [%s]  dry %.0fkg fuel %.0fkg  %s",
				m.Glyph, name, m.Tier,
				spacecraft.SumDryMass(stages), spacecraft.SumFuelMass(stages), eng)
			lines = append(lines, "  "+s.fieldValue(stackFieldIdx, "part: "+pickLabel))
		}
		lines = append(lines, "  "+s.theme.Footer.Render(
			"[←/→] pick part  [a] add on top  [x] remove top  [d] dock seam"))
	}

	// Field 1: position mode — tri-state cycle. orbit (uses PARENT
	// + ALTITUDE + DIRECTION below); alongside (drops inside
	// docking gate, all three ignored); launchpad (surface, parent
	// + LATITUDE only — direction ignored).
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(1, "POSITION"))
	var posLabel string
	switch s.posMode {
	case posAlongside:
		posLabel = "alongside active (within docking gate)"
	case posLaunchpad:
		posLabel = "launchpad (surface, co-rotating)"
	default:
		posLabel = "circular orbit"
	}
	lines = append(lines, "  "+s.fieldValue(1, posLabel))
	// ADR 0031 / S9: when the selected craft can't lift off the selected
	// parent, the cycle skips launchpad — note why, so the missing option
	// doesn't read as a bug.
	if !s.launchpadAllowed() {
		note := "launchpad unavailable — TWR < 1 on this body"
		if pb := s.currentParent(); pb != nil {
			note = fmt.Sprintf("launchpad unavailable — can't lift off %s (TWR < 1)", pb.EnglishName)
		}
		lines = append(lines, "  "+s.theme.Dim.Render(note))
	}

	// Field-3 + field-4 dim/disable masks vary by mode:
	// - orbit:     all three orbit-defining fields enabled
	// - alongside: parent + alt + direction all dimmed
	// - launchpad: parent + latitude (replaces alt) enabled,
	//              direction dimmed
	dimParent := s.posMode == posAlongside
	dimAlt := s.posMode != posOrbit
	dimDir := s.posMode != posOrbit

	// Field 2: parent body — single-line cycle.
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(2, "PARENT BODY"))
	parentLabel := "(none)"
	if pb := s.currentParent(); pb != nil {
		parentLabel = fmt.Sprintf("%s  (μ %.2e, R %.0f km)",
			pb.EnglishName, pb.GravitationalParameter(), pb.RadiusMeters()/1000)
	}
	lines = append(lines, "  "+s.fieldValueDimmed(2, parentLabel, dimParent))

	// Field 3: altitude (orbit) or launch site (launchpad) — preset cycle.
	lines = append(lines, "")
	if s.posMode == posLaunchpad {
		lines = append(lines, s.fieldHeader(3, "LAUNCH SITE"))
		site := sim.LaunchSites[s.latIdx]
		hemi := "N"
		latAbs := site.LatitudeDeg
		if latAbs < 0 {
			hemi = "S"
			latAbs = -latAbs
		}
		lonHemi := "E"
		lonAbs := site.LongitudeEastDeg
		if lonAbs < 0 {
			lonHemi = "W"
			lonAbs = -lonAbs
		}
		// Special case: Equator + North Pole have no meaningful
		// longitude (great circle / pole) — show coords without
		// the longitude when the offset is 0 to keep the label
		// readable.
		var siteLabel string
		if site.LongitudeEastDeg == 0 && (site.LatitudeDeg == 0 || site.LatitudeDeg == 90) {
			siteLabel = fmt.Sprintf("%s  (%.2f° %s)", site.Name, latAbs, hemi)
		} else {
			siteLabel = fmt.Sprintf("%s  (%.2f° %s, %.2f° %s)",
				site.Name, latAbs, hemi, lonAbs, lonHemi)
		}
		lines = append(lines, "  "+s.fieldValueDimmed(3, siteLabel, false))
	} else {
		lines = append(lines, s.fieldHeader(3, "ALTITUDE"))
		altLabel := fmt.Sprintf("%d km", altitudePresets[s.altIdx])
		lines = append(lines, "  "+s.fieldValueDimmed(3, altLabel, dimAlt))
	}

	// Field 4: direction — toggle. Ignored in launchpad mode.
	lines = append(lines, "")
	lines = append(lines, s.fieldHeader(4, "DIRECTION"))
	dirLabel := "prograde"
	if s.retrograde {
		dirLabel = "retrograde"
	}
	lines = append(lines, "  "+s.fieldValueDimmed(4, dirLabel, dimDir))

	lines = append(lines, "")
	lines = append(lines, s.theme.Dim.Render(strings.Repeat("─", 60)))
	lines = append(lines, s.theme.Footer.Render(
		"[tab] field  [←/→] cycle  [f] system filter  [enter] spawn  [esc] cancel"))

	return strings.Join(lines, "\n")
}

// craftRow renders one selectable CRAFT TYPE row (a catalog loadout, the
// Custom entry, or a saved design) at selectable index `idx`: the cursor
// marker plus the row's selection styling — warning when the cursor is on it
// and CRAFT TYPE is focused, primary when selected-but-unfocused, dim
// otherwise. Factored from the three formerly-duplicated row blocks (ADR 0031
// / S8). Group headers are rendered inline by Render and are not rows.
func (s *SpawnCraft) craftRow(idx int, label string) string {
	marker := "  "
	row := s.theme.Dim.Render(label)
	if s.loadoutIdx == idx {
		marker = s.theme.Warning.Render("→ ")
		if s.fieldIdx == 0 {
			row = s.theme.Warning.Render(label)
		} else {
			row = s.theme.Primary.Render(label)
		}
	}
	return "  " + marker + row
}

// fieldHeader returns the header label, highlighted when the field
// is focused.
func (s *SpawnCraft) fieldHeader(idx int, label string) string {
	if s.fieldIdx == idx {
		return s.theme.Warning.Render("▶ " + label)
	}
	return s.theme.Primary.Render("  " + label)
}

// fieldValue returns the rendered value, with cycle hints when the
// field is focused.
func (s *SpawnCraft) fieldValue(idx int, label string) string {
	if s.fieldIdx == idx {
		return s.theme.Warning.Render("◀  " + label + "  ▶")
	}
	return label
}

// fieldValueDimmed is fieldValue with an "inactive" state — used
// for orbit-defining fields when POSITION = alongside makes them
// irrelevant. v0.8.3+.
func (s *SpawnCraft) fieldValueDimmed(idx int, label string, dimmed bool) string {
	if dimmed {
		return s.theme.Dim.Render(label + "  (ignored)")
	}
	return s.fieldValue(idx, label)
}

// currentParent returns the body the cursor is on, or nil.
func (s *SpawnCraft) currentParent() *bodies.CelestialBody {
	if s.parentIdx < 0 || s.parentIdx >= len(s.parentBodies) {
		return nil
	}
	return &s.parentBodies[s.parentIdx]
}

// crewTag is the spawn-form crewed/uncrewed label for a loadout, derived from
// its Crewed predicate — any stage with a crewed command source (ADR 0031 /
// S9). Plain text (no per-tag color) so it composes with the row's
// selection styling without nested ANSI.
func crewTag(l spacecraft.Loadout) string {
	if l.Crewed() {
		return "crewed"
	}
	return "uncrewed"
}

// propulsionSummary one-lines a loadout's main-engine + RCS shape
// for the form preview. Pure-RCS craft (Thrust=0) call it out
// explicitly so the player knows `b` won't fire on that loadout.
//
// v0.9.1+ multi-stage loadouts list a stage count next to the dry
// mass so the player can see at a glance that the Saturn-V is a
// 3-stage chain instead of a single tank.
//
// ADR 0014 Slice D appends a Scale Class hint — the Δv-to-orbit /
// "best for" line — branching on the loadout's Scale() tag. It is a
// display hint only: the craft list is never filtered by scale, so a
// real-fleet craft can still be spawned in a stripped-back System (it
// will simply be over-powered) and vice-versa.
func propulsionSummary(l spacecraft.Loadout) string {
	dry := spacecraft.SumDryMass(l.Stages)
	fuel := spacecraft.SumFuelMass(l.Stages)
	bottomThrust := l.Thrust()
	bottomIsp := l.Isp()
	stageNote := ""
	if len(l.Stages) > 1 {
		stageNote = fmt.Sprintf(" (%d stages)", len(l.Stages))
	}
	var summary string
	if bottomThrust == 0 {
		summary = fmt.Sprintf("dry %.0fkg%s, RCS-only", dry, stageNote)
	} else {
		summary = fmt.Sprintf("dry %.0fkg, fuel %.0fkg%s, %.0fkN @ Isp %.0fs",
			dry, fuel, stageNote, bottomThrust/1000, bottomIsp)
	}
	return summary + " · " + scaleHint(l.Scale())
}

// scaleHint maps a normalized ScaleClass to its spawn-form "best for"
// line. The Δv-to-orbit figures come from ADR 0014: ~9.4 km/s for the
// real (Sol) fleet, ~3.4 km/s for the stripped-back (Lumen) fleet. An
// unrecognized tag falls through to the real-scale wording so a future
// overlay class never renders blank.
func scaleHint(scale bodies.ScaleClass) string {
	switch scale {
	case bodies.ScaleStrippedBack:
		return "stripped-back scale, ~3.4 km/s to orbit"
	default:
		return "real scale, ~9.4 km/s to orbit"
	}
}
