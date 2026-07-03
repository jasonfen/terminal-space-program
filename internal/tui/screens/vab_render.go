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

// Render returns the VAB screen for the current mode. width is the terminal
// width.
func (v *VAB) Render(width int) string {
	switch v.mode {
	case vabModeNaming:
		return v.renderNaming(width)
	case vabModeLoad:
		return v.renderLoad(width)
	case vabModeTarget:
		return v.renderTarget(width)
	default:
		return v.renderBuild(width)
	}
}

// renderBuild lays out the VAB as two side-by-side columns — the component
// palette on the left, the vehicle (glyph view) on the right — with a live
// stats strip and a full-width footer (ADR 0030 §2). The columns are joined
// per-line so a single linear cursor reads naturally down whichever column
// has focus.
func (v *VAB) renderBuild(width int) string {
	if width < 64 {
		width = 64 // floor: keep both columns usable on a narrow terminal
	}
	palW := clampI(width*42/100, 30, 48)
	vehW := width - palW - 3 // 3 = " │ " separator
	if vehW < 28 {
		vehW = 28
	}

	var head []string
	head = append(head, v.theme.Title.Render("terminal-space-program — Vehicle Assembly (VAB)"))
	name := v.name
	if name == "" {
		name = "(unsaved)"
	}
	head = append(head, v.theme.Dim.Render("design: ")+v.theme.Primary.Render(name))

	body := v.joinColumns(v.renderPaletteColumn(palW), v.renderVehicleColumn(vehW), palW)

	var foot []string
	if v.flash != "" {
		foot = append(foot, "", v.theme.Warning.Render(v.flash))
	}
	foot = append(foot, v.theme.Dim.Render(strings.Repeat("─", clampI(width, 40, 100))))
	foot = append(foot, v.theme.Footer.Render(
		"[tab] column  [↑/↓] move  [←/→] swap  [PgUp/Dn] section  [a] add  [n] new stage  [x] remove"))
	foot = append(foot, v.theme.Footer.Render(
		"[+/−] qty  ['['/']'] reorder  [y] duplicate  [enter] crack part  [d] dock seam  [c] fuse  [t] target  [s] save  [o] open  [esc] back"))

	return strings.Join(head, "\n") + "\n\n" + body + "\n" + strings.Join(foot, "\n")
}

// joinColumns stitches the left and right column line-lists side by side,
// padding the left to leftW (display width) and inserting a dim divider, so
// the divider stays vertically aligned regardless of ANSI styling or wide
// runes.
func (v *VAB) joinColumns(left, right []string, leftW int) string {
	sep := v.theme.Dim.Render(" │ ")
	n := maxInt(len(left), len(right))
	var b strings.Builder
	for i := 0; i < n; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		pad := leftW - lipgloss.Width(l)
		if pad < 0 {
			pad = 0
		}
		b.WriteString(l)
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(sep)
		b.WriteString(r)
		if i < n-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderPaletteColumn is the left column: a kind-grouped, windowed component
// palette plus a live inspector for the item under the cursor (ADR 0030 §7).
func (v *VAB) renderPaletteColumn(w int) []string {
	hdr := "PALETTE  components · parts"
	var lines []string
	if v.focus == focusPalette {
		lines = append(lines, v.theme.Warning.Render("▶ "+hdr))
	} else {
		lines = append(lines, v.theme.Dim.Render("  "+hdr))
	}
	lines = append(lines, v.renderPalette(w)...)
	lines = append(lines, "")
	lines = append(lines, v.renderInspector(w)...)
	return lines
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
			marker = v.theme.Warning.Render("→ ")
			styled = v.theme.Warning.Render(label)
		case i == v.paletteIdx:
			marker = v.theme.Primary.Render("→ ")
			styled = v.theme.Primary.Render(label)
		default:
			styled = v.theme.Dim.Render(label)
		}
		lines = append(lines, marker+glyph+styled)
	}
	return lines
}

// renderInspector shows the full stats of the palette item under the cursor,
// its description, and what it would add to the active stage (ADR 0030 §7).
func (v *VAB) renderInspector(w int) []string {
	if v.paletteIdx < 0 || v.paletteIdx >= len(v.palette) {
		return nil
	}
	it := v.palette[v.paletteIdx]
	lines := []string{v.theme.Dim.Render("┌ inspect")}
	add := func(s string) { lines = append(lines, v.theme.Dim.Render("│ ")+s) }
	if it.isComponent {
		c := v.comps[it.id]
		add(v.componentStyle(it.id).Render(v.componentGlyph(it.id)) + " " + v.theme.Primary.Render(v.compName(c)))
		switch c.Kind {
		case spacecraft.ComponentEngine:
			add(v.theme.Dim.Render(fmt.Sprintf("engine · %s", c.FuelType)))
			add(v.theme.Dim.Render(fmt.Sprintf("%.0f kN · Isp %.0f s · dry %.0f kg", c.ThrustN/1000, c.IspS, c.DryMassKg)))
		case spacecraft.ComponentTank:
			add(v.theme.Dim.Render(fmt.Sprintf("tank · %s", c.FuelType)))
			add(v.theme.Dim.Render(fmt.Sprintf("%.0f kg fuel · dry %.0f kg", c.FuelCapacityKg, c.DryMassKg)))
		case spacecraft.ComponentCommandCore:
			add(v.theme.Dim.Render(fmt.Sprintf("command-core · %s · dry %.0f kg", c.CommandSource, c.DryMassKg)))
		case spacecraft.ComponentAntenna:
			add(v.theme.Dim.Render(fmt.Sprintf("antenna · %s · dry %.0f kg", c.AntennaKind, c.DryMassKg)))
		default:
			add(v.theme.Dim.Render(fmt.Sprintf("structure · dry %.0f kg", c.DryMassKg)))
		}
		for _, ln := range wrapText(c.Description, w-2) {
			add(v.theme.Dim.Render(ln))
		}
		add(v.inspectAddLine(w - 2))
	} else if m, ok := spacecraft.StageCatalog[it.id]; ok {
		add(v.theme.Primary.Render(m.Glyph + " " + m.Name))
		add(v.theme.Dim.Render("catalog part · " + m.Tier))
		add(v.theme.Dim.Render("→ adds as a new opaque stage"))
	} else {
		add(v.theme.Primary.Render(it.id))
	}
	return lines
}

// inspectAddLine previews where the selected palette item would land and
// whether it is fuel-compatible with the active stage.
func (v *VAB) inspectAddLine(w int) string {
	it := v.palette[v.paletteIdx]
	if len(v.stages) == 0 {
		return v.theme.Dim.Render(truncWidth("→ adds to a new stage", w))
	}
	i := clampI(v.stageIdx, 0, len(v.stages)-1)
	if v.stages[i].isCatalog() {
		return v.theme.Dim.Render(truncWidth("→ starts a new stage (block is opaque)", w))
	}
	if warn := v.fuelConflict(v.stages[i].components, it.id); warn != "" {
		return v.theme.Warning.Render(truncWidth("✗ "+warn, w))
	}
	return v.theme.Dim.Render(truncWidth(fmt.Sprintf("→ adds to S%d", i+1), w))
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
func (v *VAB) renderVehicleColumn(w int) []string {
	hdr := "VEHICLE  top → bottom"
	var lines []string
	if v.focus == focusStack {
		lines = append(lines, v.theme.Warning.Render("▶ "+hdr))
	} else {
		lines = append(lines, v.theme.Dim.Render("  "+hdr))
	}
	stages := v.resolvedStages()
	stats := spacecraft.StackStats(stages)
	lines = append(lines, v.theme.Primary.Render(truncWidth(v.targetReadout(stats), w)))
	if hint := v.tankHint(); hint != "" {
		lines = append(lines, v.theme.Warning.Render(truncWidth("  ↳ "+hint, w)))
	}
	lines = append(lines, "")
	if len(v.stages) == 0 {
		lines = append(lines, v.theme.Dim.Render("(empty — pick a part and press [a])"))
		return lines
	}
	rows := v.stackRows()
	for idx, r := range rows {
		cursorOn := idx == v.stackCursor
		sel := cursorOn && v.focus == focusStack
		if r.isHeader() {
			// A dock seam below the stage above (i+1) sits between it and this
			// stage — render the divider just before this header (top-down).
			if up := r.stageIdx + 1; up < len(v.stages) && v.stages[up].dockSeamBelow {
				lines = append(lines, v.theme.Warning.Render(truncWidth("── dock seam (Undock to release) ──", w)))
			}
			lines = append(lines, v.stageHeaderLine(r.stageIdx, stats, sel, cursorOn, w))
		} else {
			groups := v.rowGroups(r.stageIdx)
			if r.group < len(groups) {
				lines = append(lines, v.groupLine(groups[r.group], sel, cursorOn, w))
			}
		}
	}
	for _, warn := range v.Warnings() {
		lines = append(lines, v.theme.Dim.Render(truncWidth("⚠ "+warn, w)))
	}
	return lines
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
		marker = v.theme.Warning.Render("→ ")
		text = v.theme.Warning.Render(text)
	case cursorOn:
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
	name = truncWidth(name, w-6)
	marker := "    "
	if cursorOn {
		if sel {
			marker = v.theme.Warning.Render("  → ")
			name = v.theme.Warning.Render(name)
		} else {
			marker = "  → "
			name = v.theme.Primary.Render(name)
		}
	} else {
		name = v.theme.Dim.Render(name)
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
	return fmt.Sprintf("%d components", len(vs.components))
}

func (v *VAB) renderNaming(width int) string {
	var lines []string
	lines = append(lines, v.theme.Title.Render("terminal-space-program — save design"))
	lines = append(lines, "")
	lines = append(lines, "  "+v.theme.Primary.Render("name: ")+v.theme.Warning.Render(v.name+"▏"))
	lines = append(lines, "")
	if v.flash != "" {
		lines = append(lines, "  "+v.theme.Warning.Render(v.flash))
		lines = append(lines, "")
	}
	lines = append(lines, v.theme.Footer.Render("[enter] save  [esc] cancel"))
	return strings.Join(lines, "\n")
}

// renderTarget is the Σ Δv target input modal (ADR 0032 §8).
func (v *VAB) renderTarget(width int) string {
	var lines []string
	lines = append(lines, v.theme.Title.Render("terminal-space-program — Σ Δv target"))
	lines = append(lines, "")
	lines = append(lines, "  "+v.theme.Primary.Render("target Σ Δv (m/s): ")+v.theme.Warning.Render(v.targetInput+"▏"))
	lines = append(lines, "")
	lines = append(lines, "  "+v.theme.Dim.Render(fmt.Sprintf("current Σ Δv: %.0f m/s", v.Stats().TotalDV)))
	lines = append(lines, "")
	if v.flash != "" {
		lines = append(lines, "  "+v.theme.Warning.Render(v.flash))
		lines = append(lines, "")
	}
	lines = append(lines, v.theme.Footer.Render("[enter] set  [empty ⏎] clear  [esc] cancel"))
	return strings.Join(lines, "\n")
}

func (v *VAB) renderLoad(width int) string {
	var lines []string
	lines = append(lines, v.theme.Title.Render("terminal-space-program — load design"))
	lines = append(lines, "")
	if len(v.designs) == 0 {
		lines = append(lines, "  "+v.theme.Dim.Render("(no saved designs yet)"))
	} else {
		for i, d := range v.designs {
			marker := "  "
			row := fmt.Sprintf("%s  (%d stages)", d.Name(), len(d.Loadout.Parts))
			if i == v.loadIdx {
				marker = v.theme.Warning.Render("→ ")
				row = v.theme.Warning.Render(row)
			} else {
				row = v.theme.Dim.Render(row)
			}
			lines = append(lines, "  "+marker+row)
		}
	}
	lines = append(lines, "")
	if v.flash != "" {
		lines = append(lines, "  "+v.theme.Warning.Render(v.flash))
		lines = append(lines, "")
	}
	lines = append(lines, v.theme.Footer.Render("[↑/↓] pick  [enter] load  [x] delete  [esc] back"))
	return strings.Join(lines, "\n")
}
