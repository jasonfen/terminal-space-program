package screens

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// HandleKey routes a raw key to the active mode. The VAB owns its own keymap
// (handled here, like SpawnCraft) so its keys never fall through to the
// orbit flight controls.
func (v *VAB) HandleKey(key string) VABAction {
	switch v.mode {
	case vabModeNaming:
		return v.handleNamingKey(key)
	case vabModeLoad:
		return v.handleLoadKey(key)
	case vabModeTarget:
		return v.handleTargetKey(key)
	default:
		return v.handleBuildKey(key)
	}
}

func (v *VAB) handleBuildKey(key string) VABAction {
	switch key {
	case "esc":
		return VABActionCancel
	// ←/→ (and h/l for vim parity) edit the focused vehicle row — swap its
	// component within kind (ADR 0032 §3). They no longer switch columns.
	case "left", "h":
		v.swapRow(-1)
	case "right", "l":
		v.swapRow(+1)
	// tab / shift+tab are the only column switch now (ADR 0032 §3).
	case "tab", "shift+tab":
		if v.focus == focusPalette {
			v.focus = focusStack
		} else {
			v.focus = focusPalette
		}
	case "up", "k":
		v.moveCursor(-1)
	case "down", "j":
		v.moveCursor(+1)
	case "pgup":
		v.jumpSection(-1)
	case "pgdown":
		v.jumpSection(+1)
	case "a":
		v.addSelected()
	case "n":
		v.newStage()
	case "x":
		v.removeUnderCursor()
	case "+", "=":
		v.quantityDelta(+1)
	case "-", "_":
		v.quantityDelta(-1)
	case "]":
		v.reorderStage(+1)
	case "[":
		v.reorderStage(-1)
	case "y":
		v.duplicateStage()
	case "enter":
		v.crackOpen()
	case "d":
		v.toggleDockSeam()
	case "c":
		v.toggleDecoupleFuse()
	case "t":
		v.enterTargetMode()
	case "s":
		v.flash = ""
		v.mode = vabModeNaming
	case "o":
		v.enterLoadMode()
	}
	return VABActionNone
}

// enterTargetMode opens the Σ Δv target numeric input, pre-filling the current
// target (ADR 0032 §8).
func (v *VAB) enterTargetMode() {
	if v.target > 0 {
		v.targetInput = strconv.FormatFloat(v.target, 'f', 0, 64)
	} else {
		v.targetInput = ""
	}
	v.flash = ""
	v.mode = vabModeTarget
}

// handleTargetKey drives the Σ Δv target input (maneuver-form idiom): digits +
// one decimal point, enter to set (empty clears), esc to cancel.
func (v *VAB) handleTargetKey(key string) VABAction {
	switch key {
	case "esc":
		v.mode = vabModeBuild
	case "enter":
		if v.setTarget(v.targetInput) {
			v.mode = vabModeBuild
		}
	case "backspace":
		if r := []rune(v.targetInput); len(r) > 0 {
			v.targetInput = string(r[:len(r)-1])
		}
	default:
		if len(key) == 1 {
			if c := key[0]; (c >= '0' && c <= '9') || c == '.' {
				v.targetInput += key
			}
		}
	}
	return VABActionNone
}

// moveCursor drives the single linear cursor in the active column: the
// flattened stage/group cursor in the vehicle column (clamped, no wrap), or
// the palette list (wraps).
func (v *VAB) moveCursor(step int) {
	if v.focus == focusStack {
		v.moveStackCursor(step)
		return
	}
	if len(v.palette) > 0 {
		v.paletteIdx = wrapIdx(v.paletteIdx+step, len(v.palette))
	}
}

func (v *VAB) handleNamingKey(key string) VABAction {
	switch key {
	case "esc":
		v.mode = vabModeBuild
	case "enter":
		if strings.TrimSpace(v.name) == "" {
			v.flash = "name can't be empty"
			return VABActionNone
		}
		if err := spacecraft.SaveDesign(v.toDesign()); err != nil {
			v.flash = "save failed: " + err.Error()
		} else {
			v.flash = fmt.Sprintf("saved design %q", v.name)
		}
		v.mode = vabModeBuild
	case "backspace":
		if r := []rune(v.name); len(r) > 0 {
			v.name = string(r[:len(r)-1])
		}
	default:
		// Bubble Tea reports the spacebar as " " (a single rune), not "space",
		// so the single-rune default covers it along with every printable
		// character; multi-rune keys (arrows, "tab", …) are ignored.
		if len([]rune(key)) == 1 {
			v.name += key
		}
	}
	return VABActionNone
}

func (v *VAB) handleLoadKey(key string) VABAction {
	switch key {
	case "esc":
		v.mode = vabModeBuild
	case "up":
		if len(v.designs) > 0 {
			v.loadIdx = wrapIdx(v.loadIdx-1, len(v.designs))
		}
	case "down":
		if len(v.designs) > 0 {
			v.loadIdx = wrapIdx(v.loadIdx+1, len(v.designs))
		}
	case "enter":
		if v.loadIdx >= 0 && v.loadIdx < len(v.designs) {
			v.loadDesign(v.designs[v.loadIdx])
			v.flash = fmt.Sprintf("loaded %q", v.name)
		}
	case "x":
		if v.loadIdx >= 0 && v.loadIdx < len(v.designs) {
			id := v.designs[v.loadIdx].ID()
			if err := spacecraft.DeleteDesign(id); err == nil {
				v.refreshDesigns()
				v.flash = fmt.Sprintf("deleted %q", id)
			} else {
				v.flash = "delete failed: " + err.Error()
			}
		}
	}
	return VABActionNone
}

func (v *VAB) enterLoadMode() {
	v.refreshDesigns()
	v.loadIdx = 0
	v.mode = vabModeLoad
	v.flash = ""
}

func (v *VAB) refreshDesigns() {
	designs, _ := spacecraft.ListDesigns()
	v.designs = designs
	if v.loadIdx >= len(v.designs) {
		v.loadIdx = 0
	}
}

// TitleScreen is the Title Row's screen name for the VAB's current mode
// (B11 / G9 Q1).
func (v *VAB) TitleScreen() string {
	switch v.mode {
	case vabModeNaming:
		return "Save design"
	case vabModeLoad:
		return "Load design"
	case vabModeTarget:
		return "Σ Δv target"
	}
	return "Vehicle Assembly (VAB)"
}

// Render returns the VAB screen for the current mode inside the shared form
// frame (B11 / G9 Q4). width x height is the whole framed block (the App's
// Title Row sits above it).
func (v *VAB) Render(width, height int) string {
	switch v.mode {
	case vabModeNaming:
		return v.renderNaming(width, height)
	case vabModeLoad:
		return v.renderLoad(width, height)
	case vabModeTarget:
		return v.renderTarget(width, height)
	default:
		return v.renderBuild(width, height)
	}
}

// vabBuildHints are the build screen's two key-hint rows. #373: they used to
// be single un-wrapped strings that ran off the right edge at 104 columns
// and hid "[s] save" / "[o] open" outright; wrapFooterHints wraps whole
// "[key] hint" pairs instead of cutting mid-token.
const (
	vabHints1 = "[tab] column  [↑/↓] move  [←/→] swap  [PgUp/Dn] section  [a] add  [n] new stage  [x] remove"
	vabHints2 = "[+/−] qty  ['['/']'] reorder  [y] duplicate  [enter] crack part  [d] dock seam  [c] fuse  [t] target  [s] save  [o] open"
)

// renderBuild lays out the VAB as three titled boxes in the frame (B11 / G9
// Q4): PALETTE and INSPECT stacked on the left, VEHICLE on the right, and a
// KEYS box along the bottom (the hint rows are too long for the frame's
// one-row bottom edge, which carries just the way out). The boxes are
// aligned per row so a single linear cursor reads naturally down whichever
// column has focus (ADR 0030 §2).
func (v *VAB) renderBuild(width, height int) string {
	inner := width - 2*frameInset
	palW := clampI(inner*42/100, 30, 52)
	vehW := inner - palW - 1
	if vehW < 28 {
		vehW = 28
	}

	var keys []string
	if v.flash != "" {
		keys = append(keys, v.theme.Warning.Render(v.flash))
	}
	for _, ln := range wrapFooterHints(vabHints1, inner-2) {
		keys = append(keys, v.theme.Footer.Render(ln))
	}
	for _, ln := range wrapFooterHints(vabHints2, inner-2) {
		keys = append(keys, v.theme.Footer.Render(ln))
	}
	keysBox := formBox(v.theme, "KEYS", keys, inner)

	// The vehicle column windows around the cursor into whatever height the
	// keys box leaves (#501): frame rows, keys box, then the vehicle box's
	// own top edge, title and bottom edge.
	vehRows := 0
	if height > 0 {
		vehRows = height - 2 - len(keysBox) - 3
	}

	name := v.name
	if name == "" {
		name = "(unsaved)"
	}
	vehLines := append([]string{v.theme.Dim.Render("design: ") + v.theme.Primary.Render(name)},
		v.renderVehicleColumn(vehW-2, vehRows-1)...)

	left := formBox(v.theme, v.focusTitle("PALETTE  components · parts", focusPalette), v.renderPalette(palW-2), palW)
	left = append(left, v.renderInspector(palW)...)
	right := formBox(v.theme, v.focusTitle("VEHICLE  top → bottom", focusStack), vehLines, vehW)

	body := joinBoxes(left, right, palW, 1)
	body = append(body, keysBox...)
	return formFrame(v.theme, body, width, height, v.theme.Footer.Render("[esc] back to the menu"))
}

// focusTitle is a box title that goes bold cyan while its column has focus
// (B11 / G9 Q5: focus needs no triangle of its own).
func (v *VAB) focusTitle(label string, col vabFocus) string {
	if v.focus == col {
		return v.theme.Title.Render(label)
	}
	return label
}

// renderPalette windows the palette around the cursor with kind-section
// headers; the glyph is colored by kind (the shared legend, ADR 0030 §6) and
// the label is just the name — full stats live in the inspector.
func (v *VAB) renderPalette(w int) []string {
	if len(v.palette) == 0 {
		return []string{v.theme.Dim.Render("  (catalog parts only)")}
	}
	const window = 9
	start := v.paletteIdx - window/2
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(v.palette) {
		end = len(v.palette)
		start = maxInt(0, end-window)
	}
	var lines []string
	lastKind := "\x00"
	for i := start; i < end; i++ {
		it := v.palette[i]
		kind, label := v.paletteItemLabel(it)
		if kind != lastKind {
			lines = append(lines, v.theme.Dim.Render("· "+kind+" ·"))
			lastKind = kind
		}
		glyph := "  "
		if it.isComponent {
			glyph = v.componentStyle(it.id).Render(v.componentGlyph(it.id)) + " "
		}
		label = truncWidth(label, w-5)
		marker := "  "
		var styled string
		switch {
		case i == v.paletteIdx && v.focus == focusPalette:
			marker = v.theme.Primary.Render("▸") + " "
			styled = v.theme.Warning.Render(label)
		case i == v.paletteIdx:
			marker = v.theme.Primary.Render("▸") + " "
			styled = v.theme.Primary.Render(label)
		default:
			styled = label // readable default foreground, not the disabled grey (#500)
		}
		lines = append(lines, marker+glyph+styled)
	}
	return lines
}

// renderInspector is the INSPECT box (a formBox, the same rounded box every
// form uses) describing the item
// under the cursor of the FOCUSED column: the palette item, or the vehicle
// row (component group or stage) when the vehicle column has focus (#501,
// ADR 0030 §7).
func (v *VAB) renderInspector(w int) []string {
	if w < 12 {
		return nil
	}
	inner := w - 2 // inside the box borders
	var body []string
	add := func(s string) { body = append(body, s) }
	addText := func(text string) { add(truncWidth(text, inner)) }

	switch {
	case v.focus == focusStack && v.inspectStackRow(inner, add, addText):
	case v.paletteIdx < 0 || v.paletteIdx >= len(v.palette):
		return nil
	default:
		it := v.palette[v.paletteIdx]
		if it.isComponent {
			v.inspectComponent(it.id, inner, add, addText)
			add(v.inspectAddLine(inner))
		} else if m, ok := spacecraft.StageCatalog[it.id]; ok {
			add(v.theme.Primary.Render(truncWidth(m.Name, inner)))
			addText("catalog part · " + m.Tier)
			addText("adds as a new opaque stage")
		} else {
			add(v.theme.Primary.Render(truncWidth(it.id, inner)))
		}
	}

	return formBox(v.theme, "INSPECT", body, w)
}

// inspectComponent writes the component's name line, stats and description.
func (v *VAB) inspectComponent(id string, inner int, add func(string), addText func(string)) {
	c := v.comps[id]
	add(v.componentStyle(id).Render(v.componentGlyph(id)) + " " + v.theme.Primary.Render(truncWidth(v.compName(c), inner-2)))
	switch c.Kind {
	case spacecraft.ComponentEngine:
		addText(fmt.Sprintf("engine · %s", c.FuelType))
		addText(fmt.Sprintf("%.0f kN · Isp %.0f s · dry %.0f kg", c.ThrustN/1000, c.IspS, c.DryMassKg))
	case spacecraft.ComponentTank:
		addText(fmt.Sprintf("tank · %s", c.FuelType))
		addText(fmt.Sprintf("%.0f kg fuel · dry %.0f kg", c.FuelCapacityKg, c.DryMassKg))
	case spacecraft.ComponentCommandCore:
		addText(fmt.Sprintf("command-core · %s · dry %.0f kg", c.CommandSource, c.DryMassKg))
	case spacecraft.ComponentAntenna:
		addText(fmt.Sprintf("antenna · %s · dry %.0f kg", c.AntennaKind, c.DryMassKg))
	default:
		addText(fmt.Sprintf("structure · dry %.0f kg", c.DryMassKg))
	}
	for _, ln := range wrapText(c.Description, inner) {
		add(ln)
	}
}

// inspectStackRow describes the vehicle row under the stack cursor; false when
// the stack is empty (the caller falls back to the palette item).
func (v *VAB) inspectStackRow(inner int, add func(string), addText func(string)) bool {
	r, ok := v.currentRow()
	if !ok {
		return false
	}
	if r.isHeader() {
		i := r.stageIdx
		stats := spacecraft.StackStats(v.resolvedStages())
		add(v.theme.Primary.Render(truncWidth(fmt.Sprintf("Stage S%d", i+1), inner)))
		addText(v.stageLabel(v.stages[i]))
		if i < len(stats.StageDV) {
			addText(fmt.Sprintf("Δv %.0f m/s", stats.StageDV[i]))
		}
		return true
	}
	groups := v.rowGroups(r.stageIdx)
	if r.group >= len(groups) {
		return false
	}
	g := groups[r.group]
	if g.placeholder {
		add(v.theme.Primary.Render(truncWidth(g.kind+": empty slot", inner)))
		addText(fmt.Sprintf("[←/→] picks a %s for S%d", g.kind, r.stageIdx+1))
		return true
	}
	v.inspectComponent(g.compID, inner, add, addText)
	if g.count > 1 {
		addText(fmt.Sprintf("×%d in S%d", g.count, r.stageIdx+1))
	} else {
		addText(fmt.Sprintf("in S%d", r.stageIdx+1))
	}
	return true
}

// inspectAddLine previews where the selected palette item would land and
// whether it is fuel-compatible with the active stage.
func (v *VAB) inspectAddLine(w int) string {
	it := v.palette[v.paletteIdx]
	if len(v.stages) == 0 {
		return truncWidth("adds to a new stage", w)
	}
	i := clampI(v.stageIdx, 0, len(v.stages)-1)
	if v.stages[i].isCatalog() {
		return truncWidth("starts a new stage (block is opaque)", w)
	}
	if warn := v.fuelConflict(v.stages[i].components, it.id); warn != "" {
		return v.theme.Warning.Render(truncWidth("✗ "+warn, w))
	}
	return truncWidth(fmt.Sprintf("adds to S%d", i+1), w)
}

// paletteItemLabel returns the kind (section header / jump key) and the short
// display name for a palette entry. Full stats are in the inspector.
func (v *VAB) paletteItemLabel(it vabPaletteItem) (kind, label string) {
	if !it.isComponent {
		m, ok := spacecraft.StageCatalog[it.id]
		if !ok {
			return "catalog part", it.id
		}
		return "catalog part", fmt.Sprintf("%s [%s]", m.Name, m.Tier)
	}
	c := v.comps[it.id]
	return c.Kind, v.compName(c)
}

// renderVehicleColumn is the right column: the stats strip and the glyph
// vehicle view — stage headers with their kind-folded component groups, seam
// and decouple markers, and soft-validation warnings (ADR 0030 §1-§4).
func (v *VAB) renderVehicleColumn(w, h int) []string {
	var lines []string
	stages := v.resolvedStages()
	stats := spacecraft.StackStats(stages)
	lines = append(lines, v.theme.Primary.Render(truncWidth(v.targetReadout(stats), w)))
	if hint := v.tankHint(); hint != "" {
		lines = append(lines, v.theme.Warning.Render(truncWidth("  ↳ "+hint, w)))
	}
	lines = append(lines, "")
	if len(v.stages) == 0 {
		lines = append(lines, v.theme.Dim.Render("(empty: [n] new stage · [tab] palette · [a] add)"))
		return lines
	}
	rows := v.stackRows()
	var body []string     // scrollable rows; cursorLine indexes the cursor's row
	var blockStart []bool // parallel to body: true where a stage block begins (dock seam or header)
	push := func(line string, starts bool) {
		body = append(body, line)
		blockStart = append(blockStart, starts)
	}
	cursorLine := 0
	for idx, r := range rows {
		cursorOn := idx == v.stackCursor
		sel := cursorOn && v.focus == focusStack
		if cursorOn {
			cursorLine = len(body)
		}
		if r.isHeader() {
			// A dock seam below the stage above (i+1) sits between it and this
			// stage — render the divider just before this header (top-down).
			if up := r.stageIdx + 1; up < len(v.stages) && v.stages[up].dockSeamBelow {
				push(v.theme.Warning.Render(truncWidth("── dock seam (Undock to release) ──", w)), true)
				if cursorOn {
					cursorLine = len(body) - 1
				}
				push(v.stageHeaderLine(r.stageIdx, stats, sel, cursorOn, w), false)
			} else {
				push(v.stageHeaderLine(r.stageIdx, stats, sel, cursorOn, w), true)
			}
		} else {
			groups := v.rowGroups(r.stageIdx)
			if r.group < len(groups) {
				push(v.groupLine(groups[r.group], sel, cursorOn, w), false)
			}
		}
	}
	var tail []string
	for _, warn := range v.Warnings() {
		tail = append(tail, v.theme.Warning.Render(truncWidth("⚠ "+warn, w)))
	}
	if h > 0 {
		body = v.windowRows(body, blockStart, cursorLine, h-len(lines)-len(tail), w)
	}
	lines = append(lines, body...)
	return append(lines, tail...)
}

// snapToBlock nudges a window start onto a stage-block boundary so the first
// visible row is a stage header, not an orphaned component (R4 #5). It
// prefers moving up to the owning header, then down to the next header, and
// at the very bottom the window may come up a row or two short. When neither
// keeps the cursor visible it leaves the start alone (cursor wins).
func snapToBlock(blockStart []bool, start, cursor, size, total int) int {
	if start <= 0 || start >= len(blockStart) || blockStart[start] {
		return start
	}
	for b := start - 1; b >= 0; b-- {
		if blockStart[b] {
			if b+size <= total && cursor < b+size {
				return b
			}
			break
		}
	}
	for b := start + 1; b < len(blockStart) && b <= cursor; b++ {
		if blockStart[b] {
			return b
		}
	}
	return start
}

// windowRows shows at most n of rows, scrolled so cursorLine stays visible,
// with a dim "↑/↓ N more" row on each clipped side (the cue rows count against
// n). Rows pass through untouched when they already fit.
func (v *VAB) windowRows(rows []string, blockStart []bool, cursorLine, n, w int) []string {
	if n < 3 || len(rows) <= n {
		return rows
	}
	// Reserve a cue row per side that ends up clipped; settle by trial.
	for _, cues := range []int{0, 1, 2} {
		size := n - cues
		start := clampI(cursorLine-size/2, 0, len(rows)-size)
		start = snapToBlock(blockStart, start, cursorLine, size, len(rows))
		end := clampI(start+size, 0, len(rows))
		up, down := start > 0, end < len(rows)
		if need := btoi(up) + btoi(down); need > cues {
			continue
		}
		out := append([]string(nil), rows[start:end]...)
		if up {
			out = append([]string{v.theme.Dim.Render(truncWidth(fmt.Sprintf("  ↑ %d more", start), w))}, out...)
		}
		if down {
			out = append(out, v.theme.Dim.Render(truncWidth(fmt.Sprintf("  ↓ %d more", len(rows)-end), w)))
		}
		return out
	}
	return rows[:n]
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// stageHeaderLine renders one stage header: its number, fuel chemistry (or
// catalog name), engine summary, per-stage Δv, and a fused-decouple marker.
func (v *VAB) stageHeaderLine(i int, stats spacecraft.VehicleStats, sel, cursorOn bool, w int) string {
	st := v.resolveStage(v.stages[i])
	eng := "no engine"
	if st.Thrust > 0 {
		eng = fmt.Sprintf("%.0fkN@%.0fs", st.Thrust/1000, st.Isp)
	}
	chem := "—"
	if v.stages[i].isCatalog() {
		chem = "catalog"
		if m, ok := spacecraft.StageCatalog[v.stages[i].catalogPartID]; ok {
			chem = m.Name
		}
	} else if ft := v.stageFuelType(v.stages[i].components); ft != "" {
		chem = ft
	}
	tag := ""
	if v.stages[i].decoupleFused && i >= 1 {
		tag = " ⛓"
	}
	text := truncWidth(fmt.Sprintf("S%d  %s · %s · Δv %.0f%s", i+1, chem, eng, stats.StageDV[i], tag), w-2)
	marker := "  "
	switch {
	case sel:
		marker = v.theme.Primary.Render("▸") + " "
		text = v.theme.Warning.Render(text)
	case cursorOn:
		marker = v.theme.Primary.Render("▸") + " "
		text = v.theme.Primary.Render(text)
	default:
		text = v.theme.Primary.Render(text)
	}
	return marker + text
}

// groupLine renders one kind-folded component group: a kind-colored glyph, the
// component name, and a ×N count when clustered (ADR 0030 §4).
func (v *VAB) groupLine(g vabGroup, sel, cursorOn bool, w int) string {
	var glyph, name string
	if g.placeholder {
		// "engine —" / "tank —": a dim prompt row ←/→ fills in (ADR 0032 §5).
		glyph = v.theme.Dim.Render(glyphForKind(g.kind))
		name = g.kind + " —"
	} else {
		glyph = v.componentStyle(g.compID).Render(v.componentGlyph(g.compID))
		name = v.compName(v.comps[g.compID])
		if g.count > 1 {
			name += fmt.Sprintf(" ×%d", g.count)
		}
	}
	name = truncWidth(name, w-10)
	marker := "    "
	if cursorOn {
		cur := "  " + v.theme.Primary.Render("▸") + " "
		if sel {
			marker = cur
			// ‹ name › is the one "left/right swaps this" mark (B11 / G9 Q5).
			name = v.theme.Warning.Render("‹ " + name + " ›")
		} else {
			marker = cur
			name = v.theme.Primary.Render(name)
		}
	}
	return marker + glyph + " " + name
}

// truncWidth truncates plain (un-styled) text to a display width, appending an
// ellipsis. Apply BEFORE styling so ANSI codes aren't counted or cut.
func truncWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// wrapText word-wraps plain text to a column width, returning the lines (empty
// for an empty string). Used by the inspector for descriptions.
func wrapText(s string, w int) []string {
	if strings.TrimSpace(s) == "" || w <= 0 {
		return nil
	}
	var lines []string
	var cur string
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

// wrapFooterHints word-wraps a footer key-hint row ("[key] description  [key]
// description  …", hints separated by two spaces) to a column width,
// returning the lines. Unlike wrapText, the wrap unit is a whole hint —
// "[s] save", not "[s]" and "save" separately — so a hint's key and its
// description can never land on different lines. #373: the VAB's second
// footer row (132 chars) used to be emitted as one un-wrapped string and got
// cut off mid-token at 104 columns, hiding "[s] save" / "[o] open" outright.
func wrapFooterHints(s string, w int) []string {
	if strings.TrimSpace(s) == "" || w <= 0 {
		return nil
	}
	hints := strings.Split(s, "  ")
	var lines []string
	var cur string
	for _, h := range hints {
		switch {
		case cur == "":
			cur = h
		case lipgloss.Width(cur)+2+lipgloss.Width(h) <= w:
			cur += "  " + h
		default:
			lines = append(lines, cur)
			cur = h
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func (v *VAB) compName(c spacecraft.Component) string {
	if c.Name != "" {
		return c.Name
	}
	return c.ID
}

// stageLabel summarizes a working stage for the stack list.
func (v *VAB) stageLabel(vs vabStage) string {
	if vs.isCatalog() {
		if m, ok := spacecraft.StageCatalog[vs.catalogPartID]; ok {
			return m.Name + " (catalog)"
		}
		return vs.catalogPartID
	}
	if len(vs.components) == 0 {
		return "(empty)"
	}
	if len(vs.components) == 1 {
		return "1 component"
	}
	return fmt.Sprintf("%d components", len(vs.components))
}

// vabModalBox puts a modal's lines in one titled box, centred-ish at the top
// of the frame, with the legend on the bottom edge.
func (v *VAB) vabModalBox(title string, lines []string, width, height int, legend string) string {
	inner := width - 2*frameInset
	return formFrame(v.theme, formBox(v.theme, title, lines, clampI(inner, 30, 100)), width, height, v.theme.Footer.Render(legend))
}

func (v *VAB) renderNaming(width, height int) string {
	var lines []string
	lines = append(lines, "  "+v.theme.Primary.Render("name: ")+v.theme.Warning.Render(v.name+"▏"))
	lines = append(lines, "")
	if v.flash != "" {
		lines = append(lines, "  "+v.theme.Warning.Render(v.flash))
		lines = append(lines, "")
	}
	// #501: show the vehicle being named, windowed to what the prompt leaves.
	vehW := clampI(width-2*frameInset-4, 28, 96)
	room := 0
	if height > 0 {
		room = maxInt(0, height-len(lines)-6)
	}
	if room >= 6 || height <= 0 {
		for _, ln := range v.renderVehicleColumn(vehW, room) {
			lines = append(lines, "  "+ln)
		}
	}
	return v.vabModalBox("SAVE DESIGN", lines, width, height, "[enter] save · [esc] cancel")
}

// renderTarget is the Σ Δv target input modal (ADR 0032 §8).
func (v *VAB) renderTarget(width, height int) string {
	var lines []string
	lines = append(lines, "  "+v.theme.Primary.Render("target Σ Δv (m/s): ")+v.theme.Warning.Render(v.targetInput+"▏"))
	lines = append(lines, "")
	lines = append(lines, "  "+v.theme.Dim.Render(fmt.Sprintf("current Σ Δv: %.0f m/s", v.Stats().TotalDV)))
	if v.flash != "" {
		lines = append(lines, "", "  "+v.theme.Warning.Render(v.flash))
	}
	return v.vabModalBox("Σ Δv TARGET", lines, width, height, "[enter] set · [empty ⏎] clear · [esc] cancel")
}

func (v *VAB) renderLoad(width, height int) string {
	var lines []string
	if len(v.designs) == 0 {
		lines = append(lines, "  "+v.theme.Dim.Render("(no saved designs yet)"))
	} else {
		for i, d := range v.designs {
			marker := "  "
			row := fmt.Sprintf("%s  (%d stages)", d.Name(), len(d.Loadout.Parts))
			if i == v.loadIdx {
				marker = v.theme.Primary.Render("▸") + " "
				row = v.theme.Warning.Render(row)
			} else {
				row = v.theme.Dim.Render(row)
			}
			lines = append(lines, "  "+marker+row)
		}
	}
	if v.flash != "" {
		lines = append(lines, "", "  "+v.theme.Warning.Render(v.flash))
	}
	return v.vabModalBox("LOAD DESIGN", lines, width, height, "[↑/↓] pick · [enter] load · [x] delete · [esc] back")
}
