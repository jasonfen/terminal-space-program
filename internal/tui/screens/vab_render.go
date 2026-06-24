package screens

import (
	"fmt"
	"strings"

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
	default:
		return v.handleBuildKey(key)
	}
}

func (v *VAB) handleBuildKey(key string) VABAction {
	switch key {
	case "esc":
		return VABActionCancel
	case "tab":
		if v.focus == focusPalette {
			v.focus = focusStack
		} else {
			v.focus = focusPalette
		}
	case "up":
		v.moveCursor(-1)
	case "down":
		v.moveCursor(+1)
	case "left":
		v.focus = focusPalette
		v.paletteIdx = wrapIdx(v.paletteIdx-1, len(v.palette))
	case "right":
		v.focus = focusPalette
		v.paletteIdx = wrapIdx(v.paletteIdx+1, len(v.palette))
	case "a":
		v.addSelected()
	case "n":
		v.newStage()
	case "x":
		v.removeFromCurrent()
	case "d":
		v.toggleDockSeam()
	case "c":
		v.toggleDecoupleFuse()
	case "s":
		v.flash = ""
		v.mode = vabModeNaming
	case "o":
		v.enterLoadMode()
	}
	return VABActionNone
}

func (v *VAB) moveCursor(step int) {
	if v.focus == focusStack {
		if len(v.stages) > 0 {
			v.stageIdx = wrapIdx(v.stageIdx+step, len(v.stages))
		}
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
	case "space":
		v.name += " "
	default:
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
	default:
		return v.renderBuild(width)
	}
}

func (v *VAB) renderBuild(width int) string {
	var lines []string
	lines = append(lines, v.theme.Title.Render("terminal-space-program — Vehicle Assembly (VAB)"))
	name := v.name
	if name == "" {
		name = "(unsaved)"
	}
	lines = append(lines, v.theme.Dim.Render("design: ")+v.theme.Primary.Render(name))
	lines = append(lines, "")

	// STACK pane (bottom→top), per-stage Δv, seam + decouple markers.
	stackHdr := "STACK (bottom → top)"
	if v.focus == focusStack {
		lines = append(lines, v.theme.Warning.Render("▶ "+stackHdr))
	} else {
		lines = append(lines, v.theme.Primary.Render("  "+stackHdr))
	}
	stages := v.resolvedStages()
	stats := spacecraft.StackStats(stages)
	if len(v.stages) == 0 {
		lines = append(lines, "  "+v.theme.Dim.Render("(empty — pick a part below and press [a] to add)"))
	} else {
		for i := len(v.stages) - 1; i >= 0; i-- {
			vs := v.stages[i]
			st := stages[i]
			if i >= 1 && vs.dockSeamBelow {
				lines = append(lines, "  "+v.theme.Warning.Render("── dock seam ──  (above = nose payload, [U]ndock to release)"))
			}
			cursor := "  "
			if i == v.stageIdx {
				cursor = v.theme.Warning.Render("→ ")
			}
			label := v.stageLabel(vs)
			eng := fmt.Sprintf("%.0fkN @ %.0fs", st.Thrust/1000, st.Isp)
			if st.Thrust == 0 {
				eng = "no engine"
			}
			tags := ""
			if vs.decoupleFused && i >= 1 {
				tags = v.theme.Dim.Render("  ⛓ fused-decouple")
			}
			row := fmt.Sprintf("S%d %-18s dry %.0fkg fuel %.0fkg  %s  Δv %.0f m/s",
				i+1, label, st.DryMass, st.FuelMass, eng, stats.StageDV[i])
			if i == v.stageIdx && v.focus == focusStack {
				row = v.theme.Warning.Render(row)
			} else {
				row = v.theme.Primary.Render(row)
			}
			lines = append(lines, "  "+cursor+row+tags)
		}
	}

	// STATS summary.
	lines = append(lines, "")
	lines = append(lines, "  "+v.theme.Primary.Render(fmt.Sprintf(
		"Σ  total Δv %.0f m/s   mass %.0f kg   liftoff TWR %.2f (g₀)",
		stats.TotalDV, stats.TotalMass, stats.LiftoffTWR)))

	// Soft-validation warnings.
	for _, w := range v.Warnings() {
		lines = append(lines, "  "+v.theme.Dim.Render("⚠ "+w))
	}

	// PALETTE pane.
	lines = append(lines, "")
	palHdr := "PALETTE (components by kind · catalog parts)"
	if v.focus == focusPalette {
		lines = append(lines, v.theme.Warning.Render("▶ "+palHdr))
	} else {
		lines = append(lines, v.theme.Primary.Render("  "+palHdr))
	}
	lines = append(lines, v.renderPalette()...)

	// Flash + footer.
	lines = append(lines, "")
	if v.flash != "" {
		lines = append(lines, "  "+v.theme.Warning.Render(v.flash))
	}
	lines = append(lines, v.theme.Dim.Render(strings.Repeat("─", 64)))
	lines = append(lines, v.theme.Footer.Render(
		"[tab] pane  [←/→ ↑/↓] move  [a] add  [n] new stage  [x] remove"))
	lines = append(lines, v.theme.Footer.Render(
		"[d] dock seam  [c] fuse decouple  [s] save  [o] open  [esc] back"))
	return strings.Join(lines, "\n")
}

// renderPalette lists the palette around the cursor with kind grouping
// headers, windowed so a long catalog doesn't overflow the screen.
func (v *VAB) renderPalette() []string {
	if len(v.palette) == 0 {
		return []string{"  " + v.theme.Dim.Render("(no components yet — catalog parts only)")}
	}
	const window = 8
	start := v.paletteIdx - window/2
	if start < 0 {
		start = 0
	}
	end := start + window
	if end > len(v.palette) {
		end = len(v.palette)
		start = end - window
		if start < 0 {
			start = 0
		}
	}
	var lines []string
	lastKind := "?"
	for i := start; i < end; i++ {
		it := v.palette[i]
		kind, label := v.paletteItemLabel(it)
		if kind != lastKind {
			lines = append(lines, "  "+v.theme.Dim.Render("· "+kind+" ·"))
			lastKind = kind
		}
		marker := "  "
		row := label
		if i == v.paletteIdx {
			marker = v.theme.Warning.Render("→ ")
			if v.focus == focusPalette {
				row = v.theme.Warning.Render(row)
			} else {
				row = v.theme.Primary.Render(row)
			}
		} else {
			row = v.theme.Dim.Render(row)
		}
		lines = append(lines, "    "+marker+row)
	}
	return lines
}

// paletteItemLabel returns the group label and one-line description for a
// palette entry.
func (v *VAB) paletteItemLabel(it vabPaletteItem) (kind, label string) {
	if !it.isComponent {
		m, ok := spacecraft.StageCatalog[it.id]
		if !ok {
			return "catalog part", it.id
		}
		return "catalog part", fmt.Sprintf("%s %s [%s]", m.Glyph, m.Name, m.Tier)
	}
	c := v.comps[it.id]
	switch c.Kind {
	case spacecraft.ComponentEngine:
		return "engine", fmt.Sprintf("%s  %.0fkN @ %.0fs  %s  dry %.0fkg", v.compName(c), c.ThrustN/1000, c.IspS, c.FuelType, c.DryMassKg)
	case spacecraft.ComponentTank:
		return "tank", fmt.Sprintf("%s  %.0fkg %s  dry %.0fkg", v.compName(c), c.FuelCapacityKg, c.FuelType, c.DryMassKg)
	case spacecraft.ComponentCommandCore:
		return "command-core", fmt.Sprintf("%s  %s  dry %.0fkg", v.compName(c), c.CommandSource, c.DryMassKg)
	case spacecraft.ComponentAntenna:
		return "antenna", fmt.Sprintf("%s  %s  dry %.0fkg", v.compName(c), c.AntennaKind, c.DryMassKg)
	default:
		return "structure", fmt.Sprintf("%s  dry %.0fkg", v.compName(c), c.DryMassKg)
	}
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
