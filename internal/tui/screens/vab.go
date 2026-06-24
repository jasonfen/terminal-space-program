package screens

import (
	"fmt"
	"sort"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// VAB is the Vehicle Assembly screen (ADR 0029 §5, the Axis B cycle-4
// endgame). The player composes finer Components (engine / tank /
// command-core / antenna / structure) into stages, stacks stages into a
// vehicle, marks N dock seams (nose payloads) and decouple groups, and reads
// live Δv / TWR / mass — then saves the result as a portable Design
// (internal/spacecraft designs store). Reached from the pause menu.
//
// Architecture: a screen sub-model like SpawnCraft / SettingsScreen. It
// reads the live component catalog (injected at Reset) and owns the designs
// store interaction (save / load / delete) — the store is app-managed
// catalog data, not world state, so this does not violate the
// screens-don't-mutate-the-world rule (ADR 0029 §4).
type VAB struct {
	theme Theme

	// comps is the live component catalog (spacecraft.Components), injected
	// at Reset so the model is testable without touching package globals.
	comps map[string]spacecraft.Component

	palette    []vabPaletteItem
	paletteIdx int

	stages   []vabStage
	stageIdx int

	focus vabFocus
	mode  vabMode

	name string

	// load-mode picker state.
	designs []spacecraft.Design
	loadIdx int

	// flash is a transient one-line notice (validation reject / save result).
	flash string
}

// VABAction enumerates the screen's outcomes the App acts on. The screen
// owns save / load / delete against the designs store directly; the App only
// needs to know when to leave.
type VABAction int

const (
	VABActionNone   VABAction = iota
	VABActionCancel           // esc from build mode — back to the menu/orbit
)

// vabFocus selects which pane the up/down keys drive.
type vabFocus int

const (
	focusPalette vabFocus = iota
	focusStack
)

// vabMode is the screen's interaction mode.
type vabMode int

const (
	vabModeBuild  vabMode = iota
	vabModeNaming         // typing a name to save
	vabModeLoad           // picking a saved design to load / delete
)

// vabStage is one stage under assembly: either a composition of component
// IDs (the fine-parts path) or a reference to an existing atomic catalog
// Part reused whole. The boundary flags describe the boundary BELOW this
// stage (between it and the stage beneath), so they ride along on add /
// remove without a parallel slice to keep in sync.
type vabStage struct {
	components    []string
	catalogPartID string
	dockSeamBelow bool // a dock seam here → this stage starts a nose-payload group (Undock release)
	decoupleFused bool // this stage decouples together with the group below (one staging press)
}

func (vs vabStage) isCatalog() bool { return vs.catalogPartID != "" }

// vabPaletteItem is one entry in the component/part palette.
type vabPaletteItem struct {
	isComponent bool
	id          string
}

// vabKindOrder is the palette grouping order for components.
var vabKindOrder = []string{
	spacecraft.ComponentEngine,
	spacecraft.ComponentTank,
	spacecraft.ComponentCommandCore,
	spacecraft.ComponentAntenna,
	spacecraft.ComponentStructure,
}

// NewVAB constructs the screen.
func NewVAB(th Theme) *VAB { return &VAB{theme: th} }

// Reset opens the VAB fresh with the given live component catalog (typically
// spacecraft.Components). Starts with an empty stack in build mode.
func (v *VAB) Reset(comps map[string]spacecraft.Component) {
	v.comps = comps
	v.stages = nil
	v.stageIdx = 0
	v.paletteIdx = 0
	v.focus = focusPalette
	v.mode = vabModeBuild
	v.name = ""
	v.flash = ""
	v.buildPalette()
}

// buildPalette assembles the flat palette: components grouped by kind (in
// vabKindOrder, then by ID), followed by single-stage atomic catalog parts
// (reusable opaque blocks; multi-stage modules are spawn-form conveniences,
// not VAB parts — ADR 0029 §7).
func (v *VAB) buildPalette() {
	v.palette = nil
	byKind := map[string][]string{}
	for id, c := range v.comps {
		byKind[c.Kind] = append(byKind[c.Kind], id)
	}
	for _, k := range vabKindOrder {
		ids := byKind[k]
		sort.Strings(ids)
		for _, id := range ids {
			v.palette = append(v.palette, vabPaletteItem{isComponent: true, id: id})
		}
	}
	for _, id := range spacecraft.StageCatalogOrder {
		if stages, ok := spacecraft.BuildModule(id); ok && len(stages) == 1 {
			v.palette = append(v.palette, vabPaletteItem{isComponent: false, id: id})
		}
	}
	if v.paletteIdx >= len(v.palette) {
		v.paletteIdx = 0
	}
}

// --- model operations (key-handling maps onto these; tested directly) ---

// addSelected adds the palette item under the cursor: a component to the
// current stage (creating one if needed), or a catalog part as a new atomic
// stage on top.
func (v *VAB) addSelected() {
	if v.paletteIdx < 0 || v.paletteIdx >= len(v.palette) {
		return
	}
	it := v.palette[v.paletteIdx]
	if !it.isComponent {
		v.stages = append(v.stages, vabStage{catalogPartID: it.id})
		v.stageIdx = len(v.stages) - 1
		v.focus = focusStack
		return
	}
	v.addComponentToCurrent(it.id)
}

// addComponentToCurrent appends a component to the current stage, enforcing
// single-fuel-per-stage (ADR 0029 §3): a chemistry conflict is rejected with
// a flash, not applied. Adding to an atomic catalog stage starts a fresh
// composed stage instead (you can't crack open an opaque block).
func (v *VAB) addComponentToCurrent(id string) {
	if len(v.stages) == 0 {
		v.stages = append(v.stages, vabStage{})
		v.stageIdx = 0
	}
	if v.stageIdx < 0 || v.stageIdx >= len(v.stages) {
		v.stageIdx = len(v.stages) - 1
	}
	if v.stages[v.stageIdx].isCatalog() {
		v.stages = append(v.stages, vabStage{components: []string{id}})
		v.stageIdx = len(v.stages) - 1
		v.focus = focusStack
		return
	}
	if warn := v.fuelConflict(v.stages[v.stageIdx].components, id); warn != "" {
		v.flash = warn
		return
	}
	v.stages[v.stageIdx].components = append(v.stages[v.stageIdx].components, id)
	v.focus = focusStack
	v.flash = ""
}

// fuelConflict reports a chemistry clash between a stage's existing fuelled
// components and a new one (empty == matches anything).
func (v *VAB) fuelConflict(existing []string, newID string) string {
	nc, ok := v.comps[newID]
	if !ok || nc.FuelType == "" {
		return ""
	}
	cur := v.stageFuelType(existing)
	if cur != "" && cur != nc.FuelType {
		return fmt.Sprintf("can't mix %s with %s in one stage", cur, nc.FuelType)
	}
	return ""
}

// stageFuelType returns the (assumed-consistent) fuel chemistry of a stage's
// components, or empty if none are fuelled.
func (v *VAB) stageFuelType(comps []string) string {
	for _, id := range comps {
		if c, ok := v.comps[id]; ok && c.FuelType != "" {
			return c.FuelType
		}
	}
	return ""
}

// newStage appends an empty composed stage on top and selects it.
func (v *VAB) newStage() {
	v.stages = append(v.stages, vabStage{})
	v.stageIdx = len(v.stages) - 1
	v.focus = focusStack
}

// removeFromCurrent pops the last component off the current composed stage,
// or removes the whole stage when it is empty or an atomic catalog block.
func (v *VAB) removeFromCurrent() {
	if len(v.stages) == 0 {
		return
	}
	if v.stageIdx < 0 || v.stageIdx >= len(v.stages) {
		v.stageIdx = len(v.stages) - 1
	}
	st := &v.stages[v.stageIdx]
	if !st.isCatalog() && len(st.components) > 0 {
		st.components = st.components[:len(st.components)-1]
		return
	}
	v.stages = append(v.stages[:v.stageIdx], v.stages[v.stageIdx+1:]...)
	if v.stageIdx >= len(v.stages) {
		v.stageIdx = len(v.stages) - 1
	}
	if v.stageIdx < 0 {
		v.stageIdx = 0
	}
}

// toggleDockSeam flips the dock seam below the current stage (the N-seam
// generalization of the spawn `d`-key; closes ADR 0028 §8). A seam below the
// bottom stage is meaningless, so stage 0 is a no-op.
func (v *VAB) toggleDockSeam() {
	if v.stageIdx >= 1 && v.stageIdx < len(v.stages) {
		v.stages[v.stageIdx].dockSeamBelow = !v.stages[v.stageIdx].dockSeamBelow
	}
}

// toggleDecoupleFuse flips whether the current stage decouples together with
// the group below it (one staging press releases both).
func (v *VAB) toggleDecoupleFuse() {
	if v.stageIdx >= 1 && v.stageIdx < len(v.stages) {
		v.stages[v.stageIdx].decoupleFused = !v.stages[v.stageIdx].decoupleFused
	}
}

// nosePayloadPlan derives the top-down nose-payload group sizes from the
// dock-seam flags (the ADR 0028 / catalog.go convention: each entry is a
// count of contiguous TOP stages forming one docked payload, ordered
// top-down). Nil when no seams are set.
func (v *VAB) nosePayloadPlan() []int {
	var seams []int
	for i := 1; i < len(v.stages); i++ {
		if v.stages[i].dockSeamBelow {
			seams = append(seams, i)
		}
	}
	if len(seams) == 0 {
		return nil
	}
	// Group sizes bottom-up between consecutive seams; the last runs to the top.
	var bottomUp []int
	for j := 0; j < len(seams); j++ {
		end := len(v.stages)
		if j+1 < len(seams) {
			end = seams[j+1]
		}
		bottomUp = append(bottomUp, end-seams[j])
	}
	plan := make([]int, len(bottomUp))
	for i := range bottomUp {
		plan[i] = bottomUp[len(bottomUp)-1-i] // reverse → top-down
	}
	return plan
}

// decouplePlan derives the bottom-up staging group sizes from the
// decouple-fuse flags (catalog.go convention: how many contiguous bottom
// stages each staging press releases; the surviving top core is excluded).
// Nil when every stage decouples singly (the all-ones default).
func (v *VAB) decouplePlan() []int {
	n := len(v.stages)
	if n <= 1 {
		return nil
	}
	fused := false
	var groups []int
	cur := 1
	for i := 1; i < n; i++ {
		if v.stages[i].decoupleFused {
			cur++
			fused = true
		} else {
			groups = append(groups, cur)
			cur = 1
		}
	}
	groups = append(groups, cur) // top group = surviving core (excluded)
	if !fused {
		return nil // all-ones default
	}
	return groups[:len(groups)-1]
}

// applyNosePayloadPlan sets the dock-seam flags from a top-down plan (the
// inverse of nosePayloadPlan), used when loading a design.
func (v *VAB) applyNosePayloadPlan(plan []int) {
	for i := range v.stages {
		v.stages[i].dockSeamBelow = false
	}
	cum := 0
	for _, g := range plan {
		cum += g
		idx := len(v.stages) - cum
		if idx >= 1 && idx < len(v.stages) {
			v.stages[idx].dockSeamBelow = true
		}
	}
}

// applyDecouplePlan sets the decouple-fuse flags from a bottom-up plan (the
// inverse of decouplePlan), used when loading a design.
func (v *VAB) applyDecouplePlan(plan []int) {
	for i := range v.stages {
		v.stages[i].decoupleFused = false
	}
	if len(plan) == 0 {
		return
	}
	idx := 0
	for _, g := range plan {
		for k := 0; k < g && idx < len(v.stages); k++ {
			v.stages[idx].decoupleFused = k > 0
			idx++
		}
	}
	for k := idx; k < len(v.stages); k++ {
		v.stages[k].decoupleFused = k > idx // surviving core fuses together
	}
}

// --- resolution + stats ---

// resolveStage materializes one working stage into a runtime Stage: an
// atomic catalog block via BuildStage, or a composition via ComposeStage
// (RCS derived from dry mass, matching the spawn path).
func (v *VAB) resolveStage(vs vabStage) spacecraft.Stage {
	if vs.isCatalog() {
		if st, ok := spacecraft.BuildStage(vs.catalogPartID); ok {
			return st
		}
		return spacecraft.Stage{}
	}
	st, _ := spacecraft.ComposeStage(vs.components, v.comps)
	return st
}

// resolvedStages is the whole stack as runtime Stages, bottom→top.
func (v *VAB) resolvedStages() []spacecraft.Stage {
	out := make([]spacecraft.Stage, 0, len(v.stages))
	for _, vs := range v.stages {
		out = append(out, v.resolveStage(vs))
	}
	return out
}

// Stats is the live Δv / TWR / mass readout for the current stack.
func (v *VAB) Stats() spacecraft.VehicleStats {
	return spacecraft.StackStats(v.resolvedStages())
}

// Warnings returns the soft (non-blocking) validation notes (ADR 0029 §5):
// empty stack, no engine, no command source (defaults to a probe core on
// spawn per EnsureCommandSource), liftoff TWR < 1.
func (v *VAB) Warnings() []string {
	if len(v.stages) == 0 {
		return []string{"empty stack — add a component or part with [a]"}
	}
	stages := v.resolvedStages()
	var w []string
	// An invalid composed stage (a component missing from the live catalog,
	// e.g. a design loaded after a mod was removed, or a mixed-fuel stage)
	// resolves to a silent zero Stage — surface why instead of showing a
	// blank "no engine / dry 0kg" row with no explanation.
	for i, vs := range v.stages {
		if vs.isCatalog() {
			continue
		}
		if _, warn := spacecraft.ComposeStage(vs.components, v.comps); warn != "" {
			w = append(w, fmt.Sprintf("stage %d invalid: %s", i+1, warn))
		}
	}
	hasEngine, hasCommand := false, false
	for _, st := range stages {
		if st.Thrust > 0 {
			hasEngine = true
		}
		if spacecraft.IsCommandSource(st.CommandSource) {
			hasCommand = true
		}
	}
	if !hasEngine {
		w = append(w, "no engine — vehicle can't maneuver")
	}
	if !hasCommand {
		w = append(w, "no command source — will default to a probe core on spawn")
	}
	if vs := spacecraft.StackStats(stages); vs.LiftoffTWR > 0 && vs.LiftoffTWR < 1 {
		w = append(w, fmt.Sprintf("liftoff TWR %.2f < 1 — won't lift off under g₀", vs.LiftoffTWR))
	}
	return w
}

// --- design (de)serialization ---

// toDesign builds the portable Design for the current stack: a composed
// Part per composed stage (design-scoped auto IDs _<slug>_s<n>), an atomic
// reference per catalog stage, plus the decouple + nose-payload plans.
func (v *VAB) toDesign() spacecraft.Design {
	id := slugifyDesign(v.name)
	var parts []spacecraft.Part
	refs := make([]spacecraft.PartRef, 0, len(v.stages))
	for i, vs := range v.stages {
		if vs.isCatalog() {
			refs = append(refs, spacecraft.PartRef{PartID: vs.catalogPartID})
			continue
		}
		pid := fmt.Sprintf("_%s_s%d", id, i)
		parts = append(parts, spacecraft.Part{
			ID:         pid,
			Name:       fmt.Sprintf("%s S%d", v.name, i+1),
			Glyph:      spacecraft.VesselGlyph,
			Color:      "#FFD93D",
			Components: append([]string(nil), vs.components...),
		})
		refs = append(refs, spacecraft.PartRef{PartID: pid})
	}
	return spacecraft.Design{
		Loadout: spacecraft.LoadoutDef{
			ID:              id,
			Name:            v.name,
			Role:            "custom",
			Glyph:           spacecraft.VesselGlyph,
			Color:           "#FFD93D",
			Parts:           refs,
			DecouplePlan:    v.decouplePlan(),
			NosePayloadPlan: v.nosePayloadPlan(),
		},
		Parts: parts,
	}
}

// loadDesign rebuilds the working stack from a saved Design — composed
// stages keep their component lists; non-composed refs become atomic catalog
// stages — then restores the seam / decouple flags.
func (v *VAB) loadDesign(d spacecraft.Design) {
	v.name = d.Name()
	byID := map[string]spacecraft.Part{}
	for _, p := range d.Parts {
		byID[p.ID] = p
	}
	v.stages = nil
	for _, ref := range d.Loadout.Parts {
		// A part present in the design's own Parts list is a composed stage
		// (toDesign only emits design-local composed Parts); anything else is
		// a reference to an atomic catalog part. Discriminate on PRESENCE, not
		// component count — an empty composed stage (0 components) is still a
		// composed stage and must not round-trip into a bogus catalog ref.
		if p, ok := byID[ref.PartID]; ok {
			v.stages = append(v.stages, vabStage{components: append([]string(nil), p.Components...)})
		} else {
			v.stages = append(v.stages, vabStage{catalogPartID: ref.PartID})
		}
	}
	v.applyNosePayloadPlan(d.Loadout.NosePayloadPlan)
	v.applyDecouplePlan(d.Loadout.DecouplePlan)
	v.stageIdx = 0
	v.focus = focusStack
	v.mode = vabModeBuild
}

// slugifyDesign turns a free-form design name into a filesystem- and
// ID-safe slug.
func slugifyDesign(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r == ' ' || r == '-' || r == '_':
			return '-'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		s = "design"
	}
	return s
}
