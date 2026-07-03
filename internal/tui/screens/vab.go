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

	// stackCursor is the single linear cursor over the vehicle column's
	// flattened rows (ADR 0030 §3): stage-header rows and their component
	// groups interleaved in display order, so ↑/↓ walk stages AND their
	// components with no separate horizontal axis. stageIdx is kept synced
	// to the cursor's stage so the existing model ops act on the right stage.
	stackCursor int

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
	v.stackCursor = 0
	v.paletteIdx = 0
	v.focus = focusStack // vehicle-primary: common edits happen in the vehicle column (ADR 0032 §2)
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
		v.focusStageInStack(v.stageIdx)
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
		v.focusStageInStack(v.stageIdx)
		return
	}
	if warn := v.fuelConflict(v.stages[v.stageIdx].components, id); warn != "" {
		v.flash = warn
		return
	}
	v.stages[v.stageIdx].components = append(v.stages[v.stageIdx].components, id)
	v.focusStageInStack(v.stageIdx)
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

// newStage appends an empty composed stage on top, brings focus to the
// vehicle column, and lands the cursor on the stage's first editable row (the
// engine placeholder) so the n → ←/→ → +/− build loop needs no palette trip
// (ADR 0032 §5).
func (v *VAB) newStage() {
	v.stages = append(v.stages, vabStage{})
	v.stageIdx = len(v.stages) - 1
	v.focus = focusStack
	v.focusStageInStack(v.stageIdx)
	rows := v.stackRows()
	if h := v.stackCursor; h+1 < len(rows) && !rows[h+1].isHeader() && rows[h+1].stageIdx == v.stageIdx {
		v.stackCursor = h + 1
	}
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
	v.focus = focusStack // a loaded vehicle becomes the active column
	v.focusStageInStack(v.stageIdx)
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

// --- linear stack cursor, canonical fold, and editing ops (ADR 0030) ---

// vabGroup is a kind-folded run of identical components within a stage
// (canonical fold-all, ADR 0030 §4): every instance of one component ID,
// counted. Groups display ordered by kind (vabKindOrder) then ID.
type vabGroup struct {
	compID string
	kind   string
	count  int
	// placeholder marks a synthetic "engine —" / "tank —" prompt row on a
	// stage missing that propulsion kind (ADR 0032 §5): compID == "", count
	// == 0. ←/→ cycles it from none through the catalog; the first real pick
	// appends the component and the row becomes an ordinary group.
	placeholder bool
}

// vabRow is one navigable row in the vehicle column's flattened display list
// (ADR 0030 §3): a stage header, or a component group under it. group == -1
// marks the stage-header row; group >= 0 indexes that stage's groups.
type vabRow struct {
	stageIdx int
	group    int
}

func (r vabRow) isHeader() bool { return r.group < 0 }

// componentGroups folds a composed stage's flat component list into canonical
// kind-ordered ×N groups (ADR 0030 §4). Nil for an atomic catalog stage — an
// opaque block has no addressable components. Order is physically free to
// canonicalize because aggregation sums regardless of order (ADR 0029 §2).
func (v *VAB) componentGroups(i int) []vabGroup {
	if i < 0 || i >= len(v.stages) || v.stages[i].isCatalog() {
		return nil
	}
	counts := map[string]int{}
	var firstSeen []string
	for _, id := range v.stages[i].components {
		if counts[id] == 0 {
			firstSeen = append(firstSeen, id)
		}
		counts[id]++
	}
	groups := make([]vabGroup, 0, len(firstSeen))
	for _, id := range firstSeen {
		groups = append(groups, vabGroup{compID: id, kind: v.comps[id].Kind, count: counts[id]})
	}
	sort.SliceStable(groups, func(a, b int) bool {
		if ra, rb := kindRank(groups[a].kind), kindRank(groups[b].kind); ra != rb {
			return ra < rb
		}
		return groups[a].compID < groups[b].compID
	})
	return groups
}

// placeholderKinds returns the propulsion kinds (engine / tank) that stage i
// should surface as placeholder rows (ADR 0032 §5). A truly-empty composed
// stage prompts for both; once one propulsion kind is present the OTHER stays
// prompted (so the n → ←/→ → +/− loop needs no palette trip), but a stage
// carrying only non-propulsion parts (a structure/core payload) stays clean.
// Nil for atomic catalog stages — an opaque block has no editable rows.
func (v *VAB) placeholderKinds(i int) []string {
	if i < 0 || i >= len(v.stages) || v.stages[i].isCatalog() {
		return nil
	}
	comps := v.stages[i].components
	hasEngine, hasTank := false, false
	for _, id := range comps {
		switch v.comps[id].Kind {
		case spacecraft.ComponentEngine:
			hasEngine = true
		case spacecraft.ComponentTank:
			hasTank = true
		}
	}
	empty := len(comps) == 0
	var ks []string
	if !hasEngine && (empty || hasTank) {
		ks = append(ks, spacecraft.ComponentEngine)
	}
	if !hasTank && (empty || hasEngine) {
		ks = append(ks, spacecraft.ComponentTank)
	}
	return ks
}

// rowGroups is the vehicle column's display/navigation group list for stage i:
// the canonical real groups plus any placeholder rows, sorted into kind order
// so a placeholder slots into its kind's position (ADR 0032 §5). Row-addressed
// ops (swap / quantity / remove) and the renderer index into this list.
func (v *VAB) rowGroups(i int) []vabGroup {
	groups := v.componentGroups(i)
	for _, k := range v.placeholderKinds(i) {
		groups = append(groups, vabGroup{kind: k, placeholder: true})
	}
	sort.SliceStable(groups, func(a, b int) bool {
		if ra, rb := kindRank(groups[a].kind), kindRank(groups[b].kind); ra != rb {
			return ra < rb
		}
		return groups[a].compID < groups[b].compID
	})
	return groups
}

// stackRows builds the vehicle column's navigable rows in DISPLAY order (top
// stage first): each stage header followed by its component groups (real and
// placeholder). The linear stack cursor (stackCursor) indexes this slice.
func (v *VAB) stackRows() []vabRow {
	var rows []vabRow
	for i := len(v.stages) - 1; i >= 0; i-- {
		rows = append(rows, vabRow{stageIdx: i, group: -1})
		for g := range v.rowGroups(i) {
			rows = append(rows, vabRow{stageIdx: i, group: g})
		}
	}
	return rows
}

// currentRow returns the flattened row under the stack cursor (clamped); false
// when the stack is empty.
func (v *VAB) currentRow() (vabRow, bool) {
	rows := v.stackRows()
	if len(rows) == 0 {
		return vabRow{}, false
	}
	v.stackCursor = clampI(v.stackCursor, 0, len(rows)-1)
	return rows[v.stackCursor], true
}

// syncStageIdx keeps the legacy active-stage index aligned with the cursor so
// the existing model ops act on the stage the cursor is in.
func (v *VAB) syncStageIdx() {
	if r, ok := v.currentRow(); ok {
		v.stageIdx = r.stageIdx
	}
}

// headerRowIndex finds the flattened-row index of stage i's header row.
func (v *VAB) headerRowIndex(i int) int {
	for idx, r := range v.stackRows() {
		if r.stageIdx == i && r.isHeader() {
			return idx
		}
	}
	return 0
}

// focusStageInStack moves the stack cursor onto stage i's header (after add /
// new / structural edits so the vehicle view follows the change). It does NOT
// change which column has focus — building stays in the palette.
func (v *VAB) focusStageInStack(i int) {
	v.stageIdx = i
	v.stackCursor = v.headerRowIndex(i)
}

// moveStackCursor moves the linear cursor by step (clamped, no wrap — a
// vehicle has a top and a bottom) and re-syncs the active stage.
func (v *VAB) moveStackCursor(step int) {
	rows := v.stackRows()
	if len(rows) == 0 {
		return
	}
	v.stackCursor = clampI(v.stackCursor+step, 0, len(rows)-1)
	v.syncStageIdx()
}

// removeStageAt splices out stage i.
func (v *VAB) removeStageAt(i int) {
	if i < 0 || i >= len(v.stages) {
		return
	}
	v.stages = append(v.stages[:i], v.stages[i+1:]...)
}

// removeOneComponent removes the first occurrence of compID from stage i.
func (v *VAB) removeOneComponent(i int, compID string) {
	if i < 0 || i >= len(v.stages) {
		return
	}
	cs := v.stages[i].components
	for k, id := range cs {
		if id == compID {
			v.stages[i].components = append(cs[:k], cs[k+1:]...)
			return
		}
	}
}

// removeUnderCursor deletes what the cursor is on: one instance of a component
// group, or the whole stage when the cursor is on a stage header (ADR 0030
// §3) — replacing the baseline's pop-last-only remove.
func (v *VAB) removeUnderCursor() {
	r, ok := v.currentRow()
	if !ok {
		return
	}
	if r.isHeader() {
		v.removeStageAt(r.stageIdx)
	} else {
		groups := v.rowGroups(r.stageIdx)
		// A placeholder row has no real component to remove — leave it be.
		if r.group < len(groups) && !groups[r.group].placeholder {
			v.removeOneComponent(r.stageIdx, groups[r.group].compID)
		}
	}
	v.clampCursor()
	v.syncStageIdx()
	v.flash = ""
}

// quantityDelta bumps the count of the component group under the cursor up or
// down (ADR 0030 §5) — the cluster ergonomics that pair with the ×N fold.
// Adding one more of the SAME component never conflicts on fuel chemistry.
func (v *VAB) quantityDelta(d int) {
	r, ok := v.currentRow()
	if !ok || r.isHeader() {
		v.flash = "select a component (↑/↓) to change its count"
		return
	}
	groups := v.rowGroups(r.stageIdx)
	if r.group >= len(groups) {
		return
	}
	if groups[r.group].placeholder {
		v.flash = "pick a component (←/→) before setting its count"
		return
	}
	id := groups[r.group].compID
	if d > 0 {
		v.stages[r.stageIdx].components = append(v.stages[r.stageIdx].components, id)
	} else {
		v.removeOneComponent(r.stageIdx, id)
	}
	v.clampCursor()
	v.syncStageIdx()
	v.flash = ""
}

// stageEngineChem returns the fuel chemistry of stage i's engine(s) — the
// chemistry leader that fuelled non-engine rows follow (ADR 0032 §4). Empty
// when the stage has no fuelled engine yet.
func (v *VAB) stageEngineChem(i int) string {
	if i < 0 || i >= len(v.stages) {
		return ""
	}
	for _, id := range v.stages[i].components {
		if c, ok := v.comps[id]; ok && c.Kind == spacecraft.ComponentEngine && c.FuelType != "" {
			return c.FuelType
		}
	}
	return ""
}

// swapCandidates lists the component IDs a row of the given kind may cycle
// through, sorted for a stable order (ADR 0032 §4). The engine kind is the
// chemistry leader — every engine is a candidate. Other fuelled kinds (tanks)
// are filtered to the stage's engine chemistry so a swap never lands them in
// an incompatible state; non-fuelled kinds cycle their whole kind.
func (v *VAB) swapCandidates(i int, kind string) []string {
	chem := v.stageEngineChem(i)
	var ids []string
	for id, c := range v.comps {
		if c.Kind != kind {
			continue
		}
		if kind != spacecraft.ComponentEngine && c.FuelType != "" && chem != "" && c.FuelType != chem {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// replaceGroup swaps every instance of oldID in stage i for newID — a fold
// group cycles as a unit, preserving its ×N count.
func (v *VAB) replaceGroup(i int, oldID, newID string) {
	if i < 0 || i >= len(v.stages) {
		return
	}
	for k, id := range v.stages[i].components {
		if id == oldID {
			v.stages[i].components[k] = newID
		}
	}
}

// swapRow cycles the component of the fold row under the cursor within its
// kind (ADR 0032 §3/§4, the maneuver-form idiom): the engine row leads
// chemistry (cycles all engines), fuelled rows cycle compatible-only, a
// placeholder row cycles from none through the catalog and the first pick adds
// the component. Header rows, the palette, and empty candidate sets are
// no-ops. Only the vehicle column edits — ←/→ in the palette does nothing.
func (v *VAB) swapRow(dir int) {
	if v.focus != focusStack {
		return
	}
	r, ok := v.currentRow()
	if !ok || r.isHeader() {
		return
	}
	groups := v.rowGroups(r.stageIdx)
	if r.group < 0 || r.group >= len(groups) {
		return
	}
	g := groups[r.group]
	cands := v.swapCandidates(r.stageIdx, g.kind)
	if len(cands) == 0 {
		v.flash = fmt.Sprintf("no compatible %s to swap to", g.kind)
		return
	}
	if g.placeholder {
		// Cycle [none, cand0, cand1, …] starting at none; landing back on none
		// keeps the placeholder, any other lands a real component.
		idx := wrapIdx(dir, len(cands)+1)
		if idx == 0 {
			return
		}
		v.stages[r.stageIdx].components = append(v.stages[r.stageIdx].components, cands[idx-1])
		v.syncStageIdx()
		v.flash = ""
		return
	}
	var next string
	switch cur := indexOfStr(cands, g.compID); {
	case cur < 0 && dir >= 0:
		// Current component fell out of the candidate set (e.g. a tank left
		// stranded by a chemistry-crossing engine swap) — step in from the edge
		// so one ←/→ repairs it.
		next = cands[0]
	case cur < 0:
		next = cands[len(cands)-1]
	default:
		next = cands[wrapIdx(cur+dir, len(cands))]
	}
	if next == g.compID {
		return
	}
	v.replaceGroup(r.stageIdx, g.compID, next)
	v.clampCursor()
	v.syncStageIdx()
	v.flash = ""
}

// indexOfStr returns the index of s in xs, or -1.
func indexOfStr(xs []string, s string) int {
	for i, x := range xs {
		if x == s {
			return i
		}
	}
	return -1
}

// stageDV is the isolated Δv of a single resolved stage (a one-stage stack),
// used for the crack-open before/after delta.
func (v *VAB) stageDV(st spacecraft.Stage) float64 {
	return spacecraft.StackStats([]spacecraft.Stage{st}).TotalDV
}

// crackOpen converts the atomic catalog stage under the cursor into its
// authored seed components in place (ADR 0032 §6), so the player can start
// from a shipped part and tweak it. The seam / decouple flags ride along. The
// flash shows the honest Δv delta — the composed aggregate may differ from the
// opaque part (the seed is seed-only, never the part's stats, §6). A part with
// no seed, or a non-catalog / non-header row, is a no-op with an explanatory
// flash. There is no un-crack key: delete + re-add the part reverses it.
func (v *VAB) crackOpen() {
	r, ok := v.currentRow()
	if !ok || !r.isHeader() {
		return // enter only cracks a stage header
	}
	vs := v.stages[r.stageIdx]
	if !vs.isCatalog() {
		return // a composed stage has nothing to crack open
	}
	m, known := spacecraft.StageCatalog[vs.catalogPartID]
	name := vs.catalogPartID
	if known && m.Name != "" {
		name = m.Name
	}
	if !known || len(m.VabSeed) == 0 {
		v.flash = fmt.Sprintf("%q has no decomposition", name)
		return
	}
	before := v.stageDV(v.resolveStage(vs))
	cracked := vabStage{
		components:    append([]string(nil), m.VabSeed...),
		dockSeamBelow: vs.dockSeamBelow,
		decoupleFused: vs.decoupleFused,
	}
	v.stages[r.stageIdx] = cracked
	after := v.stageDV(v.resolveStage(cracked))
	v.flash = fmt.Sprintf("cracked %q: Δv %.0f→%.0f", name, before, after)
	v.syncStageIdx()
}

// reorderStage moves the cursor's stage one position toward the top (dir +1)
// or the bottom (dir -1) of the stack (ADR 0030 §5). The whole vabStage rides
// along with its seam / decouple flags, so a seam stays attached to the stage
// it was set on.
func (v *VAB) reorderStage(dir int) {
	r, ok := v.currentRow()
	if !ok {
		return
	}
	i := r.stageIdx
	j := i + dir
	if j < 0 || j >= len(v.stages) {
		return
	}
	v.stages[i], v.stages[j] = v.stages[j], v.stages[i]
	v.focus = focusStack
	v.focusStageInStack(j)
	v.flash = ""
}

// duplicateStage clones the cursor's stage and inserts the copy directly above
// it (ADR 0030 §5). The clone starts with fresh boundary flags so it doesn't
// inherit surprising staging.
func (v *VAB) duplicateStage() {
	r, ok := v.currentRow()
	if !ok {
		return
	}
	i := r.stageIdx
	src := v.stages[i]
	clone := vabStage{
		components:    append([]string(nil), src.components...),
		catalogPartID: src.catalogPartID,
	}
	ns := make([]vabStage, 0, len(v.stages)+1)
	ns = append(ns, v.stages[:i+1]...)
	ns = append(ns, clone)
	ns = append(ns, v.stages[i+1:]...)
	v.stages = ns
	v.focus = focusStack
	v.focusStageInStack(i + 1)
	v.flash = ""
}

// clampCursor re-clamps the stack cursor after a structural edit.
func (v *VAB) clampCursor() {
	rows := v.stackRows()
	v.stackCursor = clampI(v.stackCursor, 0, maxInt(0, len(rows)-1))
}

// jumpSection jumps by a whole section (ADR 0030 §7, PgUp/PgDn): between stage
// headers in the vehicle column, or between kind groups in the palette.
func (v *VAB) jumpSection(dir int) {
	if v.focus == focusStack {
		v.jumpStage(dir)
		return
	}
	v.jumpKind(dir)
}

// jumpStage moves the stack cursor to the previous / next stage header.
func (v *VAB) jumpStage(dir int) {
	rows := v.stackRows()
	if len(rows) == 0 {
		return
	}
	v.stackCursor = clampI(v.stackCursor, 0, len(rows)-1)
	for i := v.stackCursor + dir; i >= 0 && i < len(rows); i += dir {
		if rows[i].isHeader() {
			v.stackCursor = i
			v.syncStageIdx()
			return
		}
	}
}

// jumpKind moves the palette cursor to the first item of the previous / next
// kind section.
func (v *VAB) jumpKind(dir int) {
	if len(v.palette) == 0 {
		return
	}
	var starts []int
	lastKind := "\x00"
	for i, it := range v.palette {
		if k, _ := v.paletteItemLabel(it); k != lastKind {
			starts = append(starts, i)
			lastKind = k
		}
	}
	cur := 0
	for s, idx := range starts {
		if idx <= v.paletteIdx {
			cur = s
		}
	}
	v.paletteIdx = starts[clampI(cur+dir, 0, len(starts)-1)]
}
