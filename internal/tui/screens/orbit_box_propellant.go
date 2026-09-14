package screens

import (
	"fmt"
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// orbit_box_propellant.go — the PROPELLANT instrument box (ADR 0051
// decision 1, slice 2a): fuel with mass, Δv with Δv→circ and its burn
// time, monoprop with RCS Δv. Replaces buildVesselChip's fuel/mass/Δv/
// monoprop rows and buildLaunchChip's Δv→circ/burn: rows.
//
// Every row is always present (decision 2): with no active craft, or no
// monoprop capacity, the cell reads a dash rather than the row dropping
// — a change from the retired buildVesselChip, which omitted the
// monoprop row outright for a craft with no RCS tank.
func (v *OrbitView) buildPropellantBox(w *sim.World) []string {
	title := v.theme.Primary.Render("PROPELLANT")
	c := w.ActiveCraft()
	if c == nil {
		return []string{
			title,
			chipRow2("fuel:", "—", "mass:", "—"),
			chipRow2(readout.LabelDeltaV, "—", "Δv→circ:", "—"),
			chipRow2("monoprop:", "—", "rcs Δv:", "—"),
		}
	}
	fuelLabel := readout.Mass(c.Fuel)
	if pct, kg, ok := activeStageFuel(c); ok {
		fuelLabel = fmt.Sprintf("%.0f%% (%s)", pct, readout.Mass(kg))
	}
	monopropLabel, rcsLabel := "—", "—"
	if c.MonopropCapacity > 0 {
		monopropLabel = readout.Mass(c.Monoprop)
		rcsLabel = readout.DeltaV(c.RCSDeltaV())
	}
	return []string{
		title,
		chipRow2("fuel:", fuelLabel, "mass:", readout.Mass(c.TotalMass())),
		chipRow2(readout.LabelDeltaV, deltaVReadout(c), "Δv→circ:", v.deltaVToCircLabel(c)),
		chipRow2("monoprop:", monopropLabel, "rcs Δv:", rcsLabel),
	}
}

// deltaVToCircLabel renders PROPELLANT's Δv→circ cell: the Δv a
// circularisation burn at apoapsis would cost, plus its burn time AT
// MAXIMUM THRUST (C5 — "at full" is gone; "max" is the one word for a
// full-throttle figure everywhere on the instruments, 13b). Dash outside
// a sub-orbital climb (isSubOrbitalClimb, C4): the same rows serve an
// air or airless ascent, and closing #454's gap is exactly using this
// body-agnostic predicate here INSTEAD of the old atmosphere-gated
// buildLaunchChip/buildDescentChip split. A circular orbit's periapsis
// sits above the surface, so isSubOrbitalClimb is already false there —
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
	label := readout.DeltaV(dvCirc)
	if c.Thrust > 0 && c.TotalMass() > 0 {
		tBurnSec := dvCirc * c.TotalMass() / c.Thrust // max thrust (C5), never the current throttle
		label += "  " + readout.Duration(secondsToDuration(tBurnSec))
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
