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
func (s *SpawnCraft) Reset(systemBodies []bodies.CelestialBody, defaultParentID string) {
	s.fieldIdx = 0
	s.loadoutIdx = 0
	s.customStages = nil
	s.partIdx = 0
	s.nosePayloadCount = 0
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

// loadoutChoiceCount is the number of CRAFT TYPE rows — every
// catalog loadout plus the synthetic "Custom…" entry at the end.
func loadoutChoiceCount() int { return len(spacecraft.LoadoutOrder) + 1 }

// IsCustomSelected reports whether the cursor is on the synthetic
// "Custom…" CRAFT TYPE entry (the last row). v0.10.1+.
func (s *SpawnCraft) IsCustomSelected() bool {
	return s.loadoutIdx == len(spacecraft.LoadoutOrder)
}

// SelectedLoadoutID returns the loadout ID for the current cursor,
// or "" when the synthetic Custom entry is selected (the caller
// then reads SelectedCustomStages instead). v0.10.1+.
func (s *SpawnCraft) SelectedLoadoutID() string {
	if s.IsCustomSelected() {
		return ""
	}
	if s.loadoutIdx < 0 || s.loadoutIdx >= len(spacecraft.LoadoutOrder) {
		return spacecraft.LoadoutOrder[0]
	}
	return spacecraft.LoadoutOrder[s.loadoutIdx]
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
		// +1 row for the synthetic "Custom…" entry. v0.10.1+.
		s.loadoutIdx = wrapIdx(s.loadoutIdx+step, loadoutChoiceCount())
	case stackFieldIdx:
		// STACK field: ←/→ moves the catalog part-picker cursor.
		s.partIdx = wrapIdx(s.partIdx+step, len(spacecraft.StageCatalogOrder))
	case 1:
		s.posMode = spawnPosMode(wrapIdx(int(s.posMode)+step, 3))
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

	// Field 0: craft type — list with the cursor row highlighted.
	lines = append(lines, s.fieldHeader(0, "CRAFT TYPE"))
	lines = append(lines, "")
	for i, id := range spacecraft.LoadoutOrder {
		l := spacecraft.Loadouts[id]
		row := fmt.Sprintf("%s %s  %s  — %s",
			l.Glyph, l.Name, l.Role, propulsionSummary(l))
		marker := "  "
		if i == s.loadoutIdx {
			marker = s.theme.Warning.Render("→ ")
			if s.fieldIdx == 0 {
				row = s.theme.Warning.Render(row)
			} else {
				row = s.theme.Primary.Render(row)
			}
		} else {
			row = s.theme.Dim.Render(row)
		}
		lines = append(lines, "  "+marker+row)
	}
	// v0.10.1+: synthetic "Custom…" row — opens the STACK editor.
	{
		customRow := "✎ Custom…  build-your-own  — assemble a stage stack"
		marker := "  "
		if s.IsCustomSelected() {
			marker = s.theme.Warning.Render("→ ")
			if s.fieldIdx == 0 {
				customRow = s.theme.Warning.Render(customRow)
			} else {
				customRow = s.theme.Primary.Render(customRow)
			}
		} else {
			customRow = s.theme.Dim.Render(customRow)
		}
		lines = append(lines, "  "+marker+customRow)
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
		"[tab] field  [←/→] cycle  [enter] spawn  [esc] cancel"))

	return strings.Join(lines, "\n")
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
