package screens

import (
	"fmt"
	"math"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// orbit_box_propellant.go, the PROPELLANT instrument box (ADR 0051
// decision 1, slice 2a): fuel with mass, Δv with Δv→circ and its burn
// time, monoprop with RCS Δv. Replaces buildVesselChip's fuel/mass/Δv/
// monoprop rows and buildLaunchChip's Δv→circ/burn: rows.
//
// The fourth row, cargo:/hold:, is permanent too (ADR 0057 5.3).
//
// Every row is always present (decision 2): with no active craft, or no
// monoprop capacity, the cell reads a dash rather than the row dropping,
// a change from the retired buildVesselChip, which omitted the
// monoprop row outright for a craft with no RCS tank.
func (v *OrbitView) buildPropellantBox(w *sim.World) []string {
	title := v.theme.Primary.Render("PROPELLANT")
	c := w.ActiveCraft()
	if c == nil {
		return []string{
			title,
			chipRow2(propellantCols, "fuel:", "—", "mass:", "—"),
			chipRow2(propellantCols, readout.LabelDeltaV, "—", "Δv→circ:", "—"),
			chipRow2(propellantCols, "monoprop:", "—", "rcs:", "—"),
			chipRow2(propellantCols, "cargo:", "—", "hold:", "—"),
		}
	}
	// The lit stage's tank as a percentage only; its mass was a second
	// reading of the same thing, and mass: beside it already says how heavy
	// the vessel is (Jason 2026-10-10: "just keep %").
	// With a booster pair lit the row names both tanks, bottom-first
	// (ADR 0058 amendment 2026-10-25): boosters: / core: / mass:.
	pcts := litSetFuelPcts(c)
	fuelLabel := readout.Mass(c.Fuel)
	if len(pcts) > 0 {
		fuelLabel = pcts[0]
	}
	massLabel := readout.Mass(c.TotalMass())
	monopropLabel, rcsLabel := "—", "—"
	if c.MonopropCapacity > 0 {
		monopropLabel = readout.Mass(c.Monoprop)
		rcsLabel = readout.DeltaV(c.RCSDeltaV())
	}
	cargoLabel, holdLabel := cargoHoldLabels(c)
	fuelRow := chipRow2(propellantCols, "fuel:", fuelLabel, "mass:", massLabel)
	if len(pcts) == 2 {
		fuelRow = chipRow3(propellantLitSetCols, "boosters:", pcts[0], "core:", pcts[1], "mass:", massLabel)
	}
	return []string{
		title,
		fuelRow,
		chipRow2(propellantCols, readout.LabelDeltaV, deltaVReadout(c), "Δv→circ:", v.deltaVToCircLabel(c)),
		// #478 A5: "rcs Δv:" shortens to "rcs:" — the m/s already says
		// what kind of number follows, the same reasoning as the node
		// row's own Δv drop. monoprop: stays spelled out, it names a
		// real substance.
		chipRow2(propellantCols, "monoprop:", monopropLabel, "rcs:", rcsLabel),
		chipRow2(propellantCols, "cargo:", cargoLabel, "hold:", holdLabel),
	}
}

// litSetFuelPcts is activeStageFuel over the lit set: one percentage per
// tank with capacity, bottom-first. A vessel with one lit stage gets one.
func litSetFuelPcts(c *spacecraft.Spacecraft) []string {
	var pcts []string
	for _, i := range spacecraft.LitStages(c.Stages) {
		if st := c.Stages[i]; st.FuelCapacity > 0 {
			pcts = append(pcts, fmt.Sprintf("%.0f%%", 100*st.FuelMass/st.FuelCapacity))
		}
	}
	return pcts
}

// cargoHoldLabels renders PROPELLANT's cargo: and hold: cells (ADR 0057
// decision 5.3), read live from the stages. cargo: is what the vessel
// carries to deliver; a dash when no stage has cargo capacity (nothing to
// tell "empty" from "none"). hold: is what its Cargo Hold has received; an
// empty hold reads 0 kg (this vessel can receive), a dash when no stage
// has a hold.
func cargoHoldLabels(c *spacecraft.Spacecraft) (cargoLabel, holdLabel string) {
	var cargoKg, holdKg float64
	hasHold := false
	for _, st := range c.Stages {
		cargoKg += st.CargoKg
		holdKg += st.HoldKg
		hasHold = hasHold || st.CargoHold
	}
	cargoLabel, holdLabel = "—", "—"
	if cargoKg > 0 {
		cargoLabel = readout.Mass(cargoKg)
	}
	if hasHold {
		holdLabel = readout.Mass(holdKg)
	}
	return cargoLabel, holdLabel
}

// deltaVToCircLabel renders PROPELLANT's Δv→circ cell: the Δv a
// circularisation burn at apoapsis would cost, plus its burn time on the
// lit stage AT MAXIMUM THRUST (C5, "at full" is gone; "max" is the one word for a
// full-throttle figure everywhere on the instruments, 13b). Dash outside
// a sub-orbital climb (isSubOrbitalClimb, C4): the same rows serve an
// air or airless ascent, and closing #454's gap is exactly using this
// body-agnostic predicate here INSTEAD of the old atmosphere-gated
// buildLaunchChip/buildDescentChip split. A circular orbit's periapsis
// sits above the surface, so isSubOrbitalClimb is already false there,
// no separate "is this circular" check is needed.
func (v *OrbitView) deltaVToCircLabel(c *spacecraft.Spacecraft) string {
	if !isSubOrbitalClimb(c) {
		return "—"
	}
	el, _, _, ok := craftLiveElements(c)
	if !ok {
		return "—"
	}
	mu := c.Primary.GravitationalParameter()
	primaryR := c.Primary.RadiusMeters()
	rApo := el.Apoapsis()
	if rApo <= primaryR || el.A <= 0 {
		return "—"
	}
	vAtApo := math.Sqrt(mu * (2/rApo - 1/el.A))
	vCircAtApo := math.Sqrt(mu / rApo)
	dvCirc := vCircAtApo - vAtApo
	if dvCirc <= 0 {
		return "—"
	}
	// The unit goes: the cell has 12 cells of room at the pinned column and
	// "41542 m/s  8m32s" needs 17 (#746); the Δv: row above already says m/s.
	label := strings.TrimSuffix(readout.DeltaV(dvCirc), " m/s")
	// The rocket-equation time at max thrust (C5, never the current
	// throttle), the same BurnTimeForDV the C key plants and the burn then
	// flies. It was Δv·m/F at today's mass, which ignores the fuel the burn
	// sheds and read long: 1m11s for a burn that flew 1m01s on an S-IVB
	// (Jason 2026-10-10: "doesn't seem to align to actual").
	if d := c.BurnTimeForDV(dvCirc); d > 0 {
		label += " " + readout.Duration(d)
	}
	return label
}

// craftLiveElements resolves the active craft's current osculating
// orbital elements in its primary's frame, along with the derived
// apoapsis/periapsis altitudes above the surface. ok is false for a
// Landed craft (whose co-rotation state is not a real orbit, #375) or a
// degenerate/hyperbolic/undefined state. Shared by PROPELLANT's Δv→circ
// and NAVIGATION's Ap/Pe/incl/period cells so both boxes read one
// derivation of "the live orbit" rather than two that could drift apart.
func craftLiveElements(c *spacecraft.Spacecraft) (el orbital.Elements, apoAltM, periAltM float64, ok bool) {
	if c == nil || c.Landed {
		return orbital.Elements{}, 0, 0, false
	}
	mu := c.Primary.GravitationalParameter()
	frame := orbital.ReferenceFrameForPrimary(c.Primary)
	el = orbital.ElementsFromStateInFrame(c.State.R, c.State.V, mu, frame)
	if math.IsNaN(el.A) || math.IsInf(el.A, 0) || el.A <= 0 || el.E >= 1 {
		return el, 0, 0, false
	}
	primaryR := c.Primary.RadiusMeters()
	return el, el.Apoapsis() - primaryR, el.Periapsis() - primaryR, true
}
