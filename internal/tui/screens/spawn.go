package screens

import (
	"fmt"
	"strconv"
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

	// ALTITUDE (ADR 0044 / S4): a typed number of whole kilometres, not a
	// preset ladder. altM is the single source of truth — every operation
	// that could change it (Reset's default, a committed typed value, an
	// arrow-stepped sim.OrbitStops entry, a PARENT BODY change) routes
	// through setAltitude, which is the only place this screen calls
	// sim.ClampToOrbitBand. altNote / altBandEmpty are that call's last
	// result, kept in lockstep with altM so Render never has to re-derive
	// them.
	altM       float64 // metres above the parent's mean radius
	altEditing bool    // the typed-edit box is open (§1 state machine)
	altInput   string  // in-progress digits while altEditing
	// altLeftBox arms the ADR's third mockup frame: the player has just
	// stepped back out of the edit box, so the next Enter LAUNCHES rather
	// than reopening it. Any other key clears it, so the arming can never
	// outlive the moment — a player who tabs away and back gets "Enter to
	// edit" again, and can never launch by an Enter they meant for the box.
	altLeftBox   bool
	altNote      string // sim.ClampToOrbitBand's note, verbatim (raised/lowered/no-orbit)
	altBandEmpty bool   // true when the current parent has NO legal orbit altitude (Phobos/Deimos)

	latIdx     int // v0.9.2+: latitude preset cursor when posMode=launchpad
	retrograde bool

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

	// designStages (ADR 0031 / S11 review fix) caches each saved design's
	// resolved Stages, parallel to designs, resolved ONCE at Reset rather than
	// re-resolving (which re-parses the embedded catalog + stats the overlay
	// dir) on every render/cycle inside the launch gate.
	designStages [][]spacecraft.Stage

	// Grouping memo (ADR 0031 / S10 review fix). groupedLoadouts() is reached
	// many times per render (the row loop + every index accessor); it depends
	// only on the filter inputs (showAll, systemScale), so cache the result and
	// rebuild only when those change. groupsValid guards the never-computed
	// zero value (showAll=false / scale="" is a real state, not "uncomputed").
	groups        []loadoutGroup
	groupsShowAll bool
	groupsScale   bodies.ScaleClass
	groupsValid   bool

	// bandCoverage (#221, ADR 0027 v0.32 amendment §2) samples the live
	// CommNet model for a (bodyID, altitude, antenna) — injected at Reset
	// (the App passes World.CommBandCoverage) so the form itself never
	// derives coverage; a second formula here would drift from the model.
	// nil (tests, no world) ⇒ no warning is ever claimed. bandCache
	// memoises per (parent, altitude, antenna) — sampling costs ~400
	// connectivity solves, fine once per cursor position, wasteful per
	// render frame.
	bandCoverage func(bodyID string, altM, antennaRangeM float64) (float64, bool)
	bandCache    map[bandCacheKey]float64
}

type bandCacheKey struct {
	parentIdx     int
	altM          float64 // ADR 0044 / S4: metres, not a ladder index
	antennaRangeM float64
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

// The ALTITUDE field (ADR 0044 / S4) is a typed number of whole
// kilometres, not a preset ladder — see setAltitude / stepAltitude below.
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
func (s *SpawnCraft) Reset(systemBodies []bodies.CelestialBody, defaultParentID string, designs []spacecraft.Design, systemScale bodies.ScaleClass, bandCoverage func(bodyID string, altM, antennaRangeM float64) (float64, bool)) {
	s.bandCoverage = bandCoverage
	s.bandCache = map[bandCacheKey]float64{}
	s.fieldIdx = 0
	s.loadoutIdx = 0
	s.customStages = nil
	s.partIdx = 0
	s.nosePayloadCount = 0
	s.designs = designs
	// Resolve each design's stages once here (S11 review fix) so the launch
	// gate reads a cached slice instead of re-resolving the catalog per frame.
	s.designStages = make([][]spacecraft.Stage, len(designs))
	for i, d := range designs {
		l, _ := d.Resolve()
		s.designStages[i] = l.Stages
	}
	s.groupsValid = false // invalidate the grouping memo for the new system/scale
	s.systemScale = systemScale
	s.showAll = false
	s.posMode = posOrbit
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
	s.altEditing = false
	s.altInput = ""
	s.altLeftBox = false
	// setAltitude must run AFTER parentBodies/parentIdx are set (it clamps
	// against the current parent) — 500km matches the v0.8.1 sister-spawn
	// default and the pre-S4 ladder's default rung.
	s.setAltitude(500_000)
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
	// Memoized on the filter inputs (S10 review fix): a render hits this many
	// times but the grouping changes only when showAll / systemScale change.
	if s.groupsValid && s.groupsShowAll == s.showAll && s.groupsScale == s.systemScale {
		return s.groups
	}
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
	s.groups, s.groupsShowAll, s.groupsScale, s.groupsValid = groups, s.showAll, s.systemScale, true
	return s.groups
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
		// Read the stages resolved once at Reset (S11 review fix) — no per-frame
		// catalog re-resolve.
		i := s.loadoutIdx - s.visibleCatalogCount() - 1
		if i >= 0 && i < len(s.designStages) {
			return s.designStages[i]
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

// SelectedAltitudeM returns the chosen altitude above the parent's mean
// radius (m) — already clamped into the current parent's Orbit Band (ADR
// 0044 §4/§5) by setAltitude, the only place this screen runs that
// arithmetic. Signature unchanged from the pre-S4 ladder so app.go's call
// site needs no change.
func (s *SpawnCraft) SelectedAltitudeM() float64 {
	return s.altM
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

// CapturingText reports whether the ALTITUDE typed-edit box is currently
// open — the free-text surface App.capturingText() (app.go) must query
// (review finding #5) so the global boss-key intercept and keyboard-layout
// normalization are bypassed while a digit is being typed here, the same
// way they already are for the boss shell and the Saves browser's
// name-entry field. Named/shaped to match SavesScreen.CapturingText and
// SessionScreen.CapturingText.
func (s *SpawnCraft) CapturingText() bool { return s.altEditing }

// HandleKey maps a raw key string to a SpawnAction. Tab cycles
// fields; ←/→ edit the focused field; Enter commits; Esc cancels.
//
// ADR 0044 / S4: while the ALTITUDE typed-edit box is open, this function
// hands off entirely to handleAltInputKey — a dedicated routine that can
// only ever return SpawnActionNone, so a half-typed number can never launch
// and Esc can never cancel the whole form out from under a box the player
// meant to keep (see handleAltInputKey's doc comment for both invariants).
func (s *SpawnCraft) HandleKey(key string) SpawnAction {
	if s.altEditing {
		return s.handleAltInputKey(key)
	}
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
	// Any key other than Enter disarms the just-left-the-box state, so the
	// "Enter now launches" frame lasts exactly as long as the player's
	// attention stayed on it — tab away, step an arrow, change the parent,
	// and Enter goes back to opening the box.
	if key != "enter" {
		s.altLeftBox = false
	}
	switch key {
	case "esc":
		return SpawnActionCancel
	case "enter":
		// ADR 0044 §6: over an Empty Orbit Band (Phobos/Deimos) with
		// POSITION on orbit, Enter is dead everywhere in the form — the sim
		// would refuse the spawn, so the form must never confirm it. This
		// guard is field-independent on purpose: the player could be
		// focused on VESSEL TYPE or DIRECTION and still be parked on a
		// no-orbit body.
		if s.posMode == posOrbit && s.altBandEmpty {
			return SpawnActionNone
		}
		// ADR 0044 §1: focused on ALTITUDE in orbit mode, Enter is the edit
		// box's own key rather than the form's launch key — you step into
		// the box on purpose. Once you have stepped back out of it, the
		// very next Enter launches (the ADR's third mockup frame), so the
		// natural type-number-then-go gesture is Enter, digits, Enter,
		// Enter. altLeftBox is cleared by literally any other key below, so
		// this armed state cannot survive a change of mind.
		if s.fieldIdx == 3 && s.posMode == posOrbit && !s.altLeftBox {
			s.beginAltEdit()
			return SpawnActionNone
		}
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
		// current system ↔ show all systems' craft). No-op in the Custom STACK
		// editor so it can't yank the player mid-build — the sibling stack keys
		// [a]/[x]/[d] are likewise field-guarded (S11 review fix). Re-point to
		// the top of the freshly-filtered list (the old index may now be hidden
		// / out of range) and keep the current field focus. Re-validate the
		// launch gate so a launchpad selection can't survive onto a craft that
		// can't lift off the parent — the same snap-back cycleField applies.
		if s.fieldIdx != stackFieldIdx {
			s.showAll = !s.showAll
			s.loadoutIdx = 0
			if s.posMode == posLaunchpad && !s.launchpadAllowed() {
				s.posMode = posOrbit
			}
		}
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
			// ADR 0044 §4: the typed altitude follows the player across a
			// parent change, re-clamped only when the new body can't hold
			// it. setAltitude is idempotent when the value is already
			// in-band, so this is a no-op (empty note) in the common case.
			s.setAltitude(s.altM)
		}
	case 3:
		if s.posMode == posLaunchpad {
			s.latIdx = wrapIdx(s.latIdx+step, len(sim.LaunchSites))
		} else {
			// ADR 0044 §2: arrows walk the body's derived Orbit Stops
			// rather than a hardcoded ladder.
			s.stepAltitude(step)
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

// currentSystem reconstructs the bodies.System that sim.OrbitBandFor /
// sim.OrbitStops / sim.ClampToOrbitBand need (ParentOf + a Bodies[0]
// fallback for planets with no authored parentId) from the form's flat
// parent list. parentBodies is exactly the systemBodies Reset received, so
// this round-trips the same data those functions read — deliberately NOT a
// second injected System reference through Reset, and NOT a re-derivation
// of the band arithmetic itself, which stays in package sim.
func (s *SpawnCraft) currentSystem() bodies.System {
	return bodies.System{Bodies: s.parentBodies}
}

// setAltitude is the ONE place this screen calls sim.ClampToOrbitBand (ADR
// 0044 §4/§5) — every altitude-affecting operation (Reset's 500km default,
// a committed typed value, an arrow-stepped Orbit Stop, a PARENT BODY
// change) routes through here, so altM / altNote / altBandEmpty can never
// drift out of sync with each other or reimplement the sim's arithmetic. A
// nil current parent (bare test fixtures with no body list) is a
// pass-through: no clamp is possible without body data.
func (s *SpawnCraft) setAltitude(altM float64) {
	body := s.currentParent()
	if body == nil {
		s.altM = altM
		s.altNote = ""
		s.altBandEmpty = false
		return
	}
	clamped, note, ok := sim.ClampToOrbitBand(s.currentSystem(), *body, altM)
	s.altM = clamped
	s.altNote = note
	s.altBandEmpty = !ok
}

// altitudeEpsilonM is the float slack used when comparing the current
// altitude against a candidate sim.OrbitStops value in stepAltitude.
//
// Review finding #9: this used to be 1m — far finer than altKmLabel's
// whole-kilometre display. At Lumen's Mote the synchronous stop sits at
// 42.1387km but displays as "42 km"; a player who reads that and retypes
// 42 lands at exactly 42.000km, 139m away from the real stop. With a 1m
// epsilon that 139m gap reads as "not on the stop", so a `→` from there
// crept forward to the real 42.1387km value — an invisible move that still
// displays "42 km" and looks like the key did nothing. Raising the epsilon
// to half a kilometre makes a re-typed *displayed* value count as being on
// its stop, so stepping from it moves to the NEXT stop over instead of
// re-discovering the one already shown.
//
// This intentionally equals orbitDedupeToleranceM (also 500m), the gap
// dedupeSortedStops guarantees between any two adjacent kept stops (it
// drops a candidate only when strictly closer than that to the one before
// it, so the closest two real stops can ever legally sit is exactly
// 500m apart). At that exact boundary, stepAltitude's strict "<"/">"
// comparison against altM±epsilon can fail to see a real neighbour sitting
// precisely 500m away and hold one stop further out instead — an extra
// arrow press needed at a razor's-edge case, never a wrong destination or
// a dead key. That failure mode is preferable to the 1m epsilon's "the key
// visibly does nothing" bug this constant exists to fix.
const altitudeEpsilonM = 500.0

// stepAltitude moves to the next sim.OrbitStops entry in the given
// direction (ADR 0044 §2): the nearest stop strictly beyond the current
// altitude, so a typed in-between value (e.g. 4400km) steps to a real
// neighbour rather than snapping to a ladder index. Stepping past either
// end HOLDS at that end rather than wrapping — a deliberate S4 change from
// the old seven-rung ladder, which did wrap: the Orbit Stops range now
// spans floor-to-ceiling, which can be several orders of magnitude at a
// single body, so wrapping from a high orbit straight down to the floor
// (or vice versa) would be a far bigger and more disorienting jump than
// the old ladder's wrap ever produced. An Empty band (no stops) leaves the
// altitude untouched — there is nothing to step to.
func (s *SpawnCraft) stepAltitude(dir int) {
	body := s.currentParent()
	if body == nil {
		return
	}
	stops := sim.OrbitStops(s.currentSystem(), *body)
	if len(stops) == 0 {
		return
	}
	idx := 0
	if dir > 0 {
		idx = len(stops) - 1 // already at/above the top stop: hold
		for i, v := range stops {
			if v > s.altM+altitudeEpsilonM {
				idx = i
				break
			}
		}
	} else {
		idx = 0 // already at/below the bottom stop: hold
		for i := len(stops) - 1; i >= 0; i-- {
			if stops[i] < s.altM-altitudeEpsilonM {
				idx = i
				break
			}
		}
	}
	s.setAltitude(stops[idx])
}

// beginAltEdit opens the ALTITUDE typed-edit box (ADR 0044 §1) with an
// empty buffer — the mockup's box always starts fresh ("[4400_] km" typed
// from nothing, not "500" backspaced away first), and it keeps
// commitAltInput's empty-buffer case unambiguous: an Enter with nothing
// typed always means "leave it alone," never "I meant to keep editing the
// old digits."
func (s *SpawnCraft) beginAltEdit() {
	s.altEditing = true
	s.altInput = ""
}

// maxAltInputDigits caps the typed ALTITUDE buffer (review finding #6).
// Neptune's Orbit Ceiling — ⅔ of the way to its SOI (ADR 0044 §3) — is the
// largest legal altitude anywhere in the shipped catalog (Sol, Lumen,
// Alpha Centauri, Kepler-452, TRAPPIST-1 combined) at ~57,768,751 km, an
// 8-digit number. 8 digits comfortably exceeds that (any 8-digit buffer
// tops out at 99,999,999km, ~73% more headroom) without leaving room for a
// held digit key to grow the "[%s_] km" line past the modal's width at 80
// columns (an unbounded buffer did exactly that, and commitAltInput's
// strconv.Atoi silently swallowed the resulting overflow). Capping the
// buffer makes that overflow branch structurally unreachable rather than a
// silent no-op — see commitAltInput's doc comment.
const maxAltInputDigits = 8

// handleAltInputKey drives the ALTITUDE edit box while it is open (ADR 0044
// §1). Both of the ADR's invariants are enforced structurally by this
// function's signature alone: every branch returns SpawnActionNone, so —
// (1) a half-typed number can never launch the form (there is no branch
// that returns SpawnActionConfirm), and (2) Esc can never cancel the whole
// form out from under the player (there is no branch that returns
// SpawnActionCancel; Esc only discards the buffer and closes the box).
// Only digits and backspace mutate the buffer; every other key (letters,
// arrows, tab) is ignored outright rather than parsed — sub-kilometre
// precision is deliberately not offered (25km is the smallest legal
// altitude anywhere in the game, so tenths could never matter), and
// non-digit input has no sane partial interpretation to fall back to. A
// digit typed once the buffer already holds maxAltInputDigits characters
// is ignored the same way a non-digit key is (review finding #6) — a held
// key can no longer grow the buffer without bound.
func (s *SpawnCraft) handleAltInputKey(key string) SpawnAction {
	switch key {
	case "esc":
		s.altEditing = false
		s.altInput = ""
		s.altLeftBox = true
	case "enter":
		s.commitAltInput()
		s.altEditing = false
		s.altInput = ""
		s.altLeftBox = true
	case "backspace":
		if n := len(s.altInput); n > 0 {
			s.altInput = s.altInput[:n-1]
		}
	default:
		if len(key) == 1 && key[0] >= '0' && key[0] <= '9' && len(s.altInput) < maxAltInputDigits {
			s.altInput += key
		}
	}
	return SpawnActionNone
}

// commitAltInput parses the typed buffer into a new altitude (ADR 0044
// §1). An empty buffer reverts to the prior value rather than clearing to
// zero or erroring — there is no "no altitude" reading the way VAB's Σ Δv
// target has a "no target" state (vab.go's setTarget), so the empty case
// here just leaves altM untouched and closes the box. A malformed buffer
// can't occur (handleAltInputKey only ever appends digits), so a parse
// error is a no-op rather than a user-facing rejection. The err branch
// below is now structurally unreachable in practice (review finding #6):
// handleAltInputKey caps the buffer at maxAltInputDigits, and an 8-digit
// all-9s string ("99999999") parses cleanly into an int well under
// strconv.Atoi's overflow range on every supported platform, so the
// silent-discard case this comment used to describe as merely unlikely is
// now ruled out by construction.
func (s *SpawnCraft) commitAltInput() {
	if s.altInput == "" {
		return
	}
	km, err := strconv.Atoi(s.altInput)
	if err != nil {
		return
	}
	s.setAltitude(float64(km) * 1000)
}

// altKmLabel formats an altitude (metres) as the whole-kilometre label
// every ALTITUDE state shows. Sub-kilometre precision is deliberately
// never offered (ADR 0044 §1).
//
// Review finding #7: this used to format with a bare "%.0f", showing e.g.
// "32097122 km" directly above a clamp note reading "32,097,122 km" (the
// notes use sim.CommaKm's comma grouping) — two number formats on one
// screen. Uses the same sim.CommaKm formatter as the notes rather than
// growing a second, non-grouped implementation here.
func altKmLabel(altM float64) string {
	return sim.CommaKm(altM) + " km"
}

// Render returns the modal form. Width is the terminal width.
func (s *SpawnCraft) Render(width int) string {
	var lines []string

	const titleText = "terminal-space-program — spawn vessel"
	lines = append(lines, s.theme.Title.Render(titleText))
	lines = append(lines, "")

	// Field 0: craft type — catalog loadouts grouped under category headers
	// (ADR 0031 / S8), then a trailing "Custom & Designs" group with the
	// synthetic "Custom…" entry and any saved VAB designs. Headers are
	// non-selectable; the cursor (loadoutIdx) walks only the selectable rows,
	// which follow groupedLoadouts()'s flattened order — so `idx` below tracks
	// the running selectable index and the catalog rows end exactly at
	// visibleCatalogCount() (the Custom index — the filtered count, not
	// len(LoadoutOrder)), keeping IsCustomSelected / IsDesignSelected in step.
	lines = append(lines, s.fieldHeader(0, "VESSEL TYPE"))
	// ADR 0031 / S10: the scale-class system filter note + [f] hint.
	if s.showAll {
		lines = append(lines, "  "+s.theme.Dim.Render(
			"showing all systems' vessels — [f] filter to this system"))
	} else if hidden := len(spacecraft.LoadoutOrder) - s.visibleCatalogCount(); hidden > 0 {
		noun := "vessels"
		if hidden == 1 {
			noun = "vessel"
		}
		lines = append(lines, "  "+s.theme.Dim.Render(fmt.Sprintf(
			"%d %s from other systems hidden — [f] show all", hidden, noun)))
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
	// Trailing "Custom & Designs" group — never filtered (ADR 0031). idx is now
	// visibleCatalogCount(), so the Custom row lands on the Custom index.
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
		lines = append(lines, "  "+s.altitudeValueLine(dimAlt))
		if !dimAlt {
			lines = append(lines, s.altitudeNoteLines(width)...)
		}
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

// bandWarning classifies the focused (parent, altitude) against the
// sampled CommNet coverage (#221) FOR THE CRAFT BEING SPAWNED: a crewed
// loadout is never comms-gated so it gets no warning at all, and the
// sampler models the selected craft's own best antenna — a Relay-Tug at
// the Moon links home and must not be warned off the exact spawn the band
// pressure exists to motivate (v0.32 review finding). A value in the
// degraded band names the band AND the fix (relays) for an ordinary
// probe; zero coverage is the out-of-range case and must not advise a
// relay. #283: for a relay-class craft (one that itself carries
// AntennaRelay hardware) the degraded tier is reframed as neutral
// information — "relays advised" is circular when the craft being spawned
// IS the relay, and the ⚠ warns the player off exactly the deployment
// that fixes the gap. The coverage number stays; only the warning framing
// and the fix-suffix go. Empty when the sampler is absent, the position
// mode doesn't orbit, the current parent has no legal orbit altitude at
// all, or coverage is clean — the form never guesses.
//
// ADR 0044 / S4: the cache key is now altitude in metres (bandCacheKey),
// not a ladder index — Render calls this only from altitudeNoteLines,
// which returns before reaching here while the edit box is open, so
// sampling (≈400 connectivity solves) runs on commit, never per keystroke.
//
// Returns the line text plus whether it should render with warning
// styling (⚠ lines) or neutral styling (the relay-class reframe).
func (s *SpawnCraft) bandWarning() (text string, isWarning bool) {
	if s.bandCoverage == nil || s.posMode != posOrbit || s.altBandEmpty {
		return "", false
	}
	if s.parentIdx < 0 || s.parentIdx >= len(s.parentBodies) {
		return "", false
	}
	crewed, antennaRangeM, relayClass := spawnCommsProfile(s.selectedCraftStages())
	if crewed {
		return "", false // crewed vessels are never comms-gated
	}
	key := bandCacheKey{parentIdx: s.parentIdx, altM: s.altM, antennaRangeM: antennaRangeM}
	cov, cached := s.bandCache[key]
	if !cached {
		c, ok := s.bandCoverage(s.parentBodies[s.parentIdx].ID, s.SelectedAltitudeM(), antennaRangeM)
		if !ok {
			return "", false
		}
		cov = c
		s.bandCache[key] = cov
	}
	switch {
	case cov <= 0:
		return "⚠ out of network reach — no signal at this body", true
	case cov < sim.CommBandDegradedThreshold:
		pct := int(cov*100 + 0.5)
		if relayClass {
			return fmt.Sprintf("coverage from here: ~%d%%", pct), false
		}
		return fmt.Sprintf("⚠ degraded comms band — ~%d%% coverage, relays advised", pct), true
	}
	return "", false
}

// altitudeValueLine renders field 3's value in orbit/alongside mode (ADR
// 0044 §1/§6): the dimmed (ignored) label in alongside mode, the typed-edit
// box while it's open, a "no orbit here" placeholder over an Empty Orbit
// Band, or the normal focused/unfocused cycle-field display otherwise.
func (s *SpawnCraft) altitudeValueLine(dimmed bool) string {
	label := altKmLabel(s.altM)
	if dimmed {
		return s.fieldValueDimmed(3, label, true)
	}
	if s.altEditing {
		return s.theme.Warning.Render(fmt.Sprintf("[%s_] km", s.altInput)) +
			"  " + s.theme.Footer.Render("Enter keeps · Esc reverts")
	}
	if s.altBandEmpty {
		return s.theme.Dim.Render("——  no orbit here")
	}
	val := s.fieldValue(3, label)
	if s.fieldIdx == 3 {
		hint := "Enter to edit"
		if s.altLeftBox {
			hint = "Enter now launches"
		}
		val += "  " + s.theme.Footer.Render(hint)
	}
	return val
}

// altitudeNoteLines renders the feedback line(s) under ALTITUDE (ADR 0044
// §4/§6). An Empty Orbit Band's ✕ explanation (from sim.ClampToOrbitBand's
// note, verbatim, word-wrapped to width) wins outright since Enter is dead
// there and nothing else applies. Otherwise the clamp move (↳, also
// verbatim) and the comms band warning (⚠) are independent facts about the
// SAME number and both render when both are live, clamp first.
//
// Review finding #2: this used to let the clamp note win outright, on the
// premise (the ADR's own scope-exclusion note) that "the ↳ line replaces
// the vertical space the ⚠ line already occupies." That reasoning holds
// for a TRANSIENT clamp — the player just typed or arrowed past an edge,
// the note explains the bounce, and it will go quiet again once they move
// off it. It does not hold for a body whose Orbit Band ceiling sits below
// the 500km Reset default (Enceladus at 157km, Lumen's Mote at 75km): the
// form opens ALREADY clamped, so altNote is non-empty from the very first
// frame, and the comms warning — a standing property of the spawn, not
// feedback about a keypress — never rendered at all unless the player
// happened to nudge an arrow first. Showing both (one extra line, in the
// already-tracked #373 form-height budget) means a comms-relevant spawn at
// a low-ceiling body is never silently hidden behind a number.
func (s *SpawnCraft) altitudeNoteLines(width int) []string {
	if s.altEditing {
		return nil
	}
	if s.altBandEmpty {
		innerWidth := width - 6
		wrapped := wrapText(s.altNote, innerWidth)
		out := make([]string, 0, len(wrapped))
		for i, ln := range wrapped {
			text := "✕ " + ln
			if i > 0 {
				text = "  " + ln
			}
			out = append(out, "    "+s.theme.Alert.Render(text))
		}
		return out
	}
	var out []string
	if s.altNote != "" {
		out = append(out, "    "+s.theme.Warning.Render("↳ "+s.altNote))
	}
	if warn, isWarning := s.bandWarning(); warn != "" {
		style := s.theme.Warning
		if !isWarning {
			style = s.theme.Dim
		}
		out = append(out, "  "+style.Render(warn))
	}
	return out
}

// spawnCommsProfile derives the comms-relevant shape of a stage list the
// way vessel construction does: crewed if any stage carries a crewed
// command source, the best stage antenna's rated range (zero → the caller
// lets the sampler assume the EnsureCommandSource direct-basic backfill
// every non-debris vessel receives), and relayClass (#283) — whether the
// BUILT VESSEL, once resolved, actually forwards CommNet traffic.
//
// Review finding on #283/PR #390: relayClass used to be "any stage carries
// AntennaRelay hardware," which diverges from what the vessel resolves to.
// Spacecraft.SyncFields (internal/spacecraft/stage.go) picks the
// longest-ranged antenna across the whole stack as THE vessel's antenna —
// a custom loadout pairing a short relay antenna with a longer-ranged
// Direct dish resolves to Direct, not Relay. And commnet.go's forwarding
// gate additionally requires Controllable (a command source somewhere on
// the stack), not just relay hardware. So relayClass now runs the exact
// same resolution (via SyncFields, reused rather than re-derived) and the
// exact same forwarding condition, so the form and CommNet can't drift
// apart again: relayClass is true only when the resolved antenna is
// AntennaRelay AND the vessel is Controllable — anything else (a
// Direct-winning mixed loadout, or a relay antenna with no command source)
// falls back to the ordinary ⚠ consumer warning.
func spawnCommsProfile(stages []spacecraft.Stage) (crewed bool, antennaRangeM float64, relayClass bool) {
	craft := spacecraft.Spacecraft{Stages: stages}
	craft.SyncFields()
	return craft.Crewed, craft.AntennaRangeM, craft.AntennaKind == spacecraft.AntennaRelay && craft.Controllable
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
