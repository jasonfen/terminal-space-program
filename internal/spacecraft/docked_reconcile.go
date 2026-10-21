package spacecraft

// ReconcileDockedComponents keeps the composite's docked-component breakdown
// in step with the live stack after stages left it from the BOTTOM (staging,
// a Decouple Plan group, a dropped Dragon trunk, a lit-stage drop). The
// components are bottom-to-top, so the stages that left are the leading ones:
// they are trimmed off the front of the component list, a component whose
// stages are all gone is removed, and a component that lost only some is
// rebuilt around what remains (its flat mirrors and bottom-stage identity).
//
// Without this the components sum to more stages than the stack holds, the
// tiling check Undock relies on fails, and Undock drops to the legacy flat
// rebuild that carries no probe core, antenna or port (#707).
//
// It touches nothing unless every component records its stages and they
// claim MORE than the live stack (fewer, or a component without a breakdown,
// is not a bottom drop and cannot be repaired from here). Once fewer than two
// components remain the vessel is no longer a composite and the list is
// cleared, as Deploy does (unless the one left is a cross-player guest's). Reports whether it changed anything. Idempotent.
func (c *Spacecraft) ReconcileDockedComponents() bool {
	if c == nil || len(c.DockedComponents) == 0 {
		return false
	}
	sum := 0
	for _, dc := range c.DockedComponents {
		if len(dc.Stages) == 0 {
			return false
		}
		sum += len(dc.Stages)
	}
	excess := sum - len(c.Stages)
	if excess <= 0 {
		return false
	}
	comps := append([]DockedComponent(nil), c.DockedComponents...)
	for excess > 0 && len(comps) > 0 {
		n := len(comps[0].Stages)
		if n <= excess {
			excess -= n
			comps = comps[1:]
			continue
		}
		comps[0] = rebuildComponentAround(comps[0], comps[0].Stages[excess:])
		excess = 0
	}
	// A lone survivor tagged with a cross-player Owner stays: it is the only
	// record that the stack is now a guest's alone (the ledger hands it back).
	if len(comps) < 2 && !(len(comps) == 1 && comps[0].Owner != "") {
		comps = nil
	}
	c.DockedComponents = comps
	return true
}

// rebuildComponentAround returns dc re-recorded around the stages that are
// left of it: same name, role, owner and craft ID, with the flat mirrors and
// the bottom-stage identity (loadout, glyph, colour) taken from the survivors.
func rebuildComponentAround(dc DockedComponent, left []Stage) DockedComponent {
	tmp := &Spacecraft{Stages: append([]Stage(nil), left...)}
	tmp.SyncFields()
	bottom := tmp.Stages[0]
	out := tmp.AsDockedComponent()
	out.Name, out.Role = dc.Name, dc.Role
	out.Owner, out.CraftID = dc.Owner, dc.CraftID
	out.LoadoutID, out.Glyph, out.Color = bottom.LoadoutID, bottom.Glyph, bottom.Color
	return out
}

// PartsOfStages summarises the hardware a group of stages carries as one
// stage's worth: the strongest command source (crewed over probe), the
// longest-ranged antenna, and every docking port. Used by Undock's flat
// fallback so collapsing a component to a single stage never strips its parts.
func PartsOfStages(stages []Stage) (command, antennaKind string, antennaRangeM float64, ports []PartPort) {
	for _, st := range stages {
		switch {
		case st.CommandSource == CommandCrewed:
			command = CommandCrewed
		case st.CommandSource == CommandProbe && command != CommandCrewed:
			command = CommandProbe
		}
		if st.AntennaKind != AntennaNone && (st.AntennaRangeM > antennaRangeM ||
			(st.AntennaRangeM == antennaRangeM && st.AntennaKind == AntennaRelay)) {
			antennaKind, antennaRangeM = st.AntennaKind, st.AntennaRangeM
		}
		ports = append(ports, st.PartPorts...)
	}
	return command, antennaKind, antennaRangeM, ports
}
