package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// Cargo hand-over (ADR 0057 decision 5.4). While docked, `u` moves every cargo
// load aboard the stack into the stack's lowest Cargo Hold. One way, at once.
// The refusals are three facts, in the order a pilot would ask them.
const (
	CargoNotDocked  = "not docked"
	CargoNoHold     = "no hold aboard"
	CargoNoCargoAbd = "no cargo aboard"
)

// CargoHandedOverText is the flash for a successful hand-over: the mass in the
// readout contract's words (ADR 0049), so the flash and the hold row agree. Both
// the owner seat's key and the ledger's answer to the rider's key print it.
func CargoHandedOverText(kg float64) string { return readout.Mass(kg) + " cargo handed over" }

// HandOverCargo hands over the cargo THIS seat's hardware carries on the vessel
// at idx: every stage whose component is not tagged as another player's (all of
// them, in a single-player stack). Returns the kg moved, or a refusal naming the
// state of the vessel. Only the Dragon's owner presses `u` (ADR 0057 conflict
// 2a): in a cross-player stack the guest's cargo moves on the guest's own press,
// through HandOverGuestCargo.
func (w *World) HandOverCargo(idx int) (kg float64, refusal string) {
	if idx < 0 || idx >= len(w.Crafts) || w.Crafts[idx] == nil {
		return 0, CargoNotDocked
	}
	return w.handOverCargo(w.Crafts[idx], true, func(dc spacecraft.DockedComponent) bool { return dc.Owner == "" })
}

// HandOverGuestCargo is the rider's `u`, performed on the stack owner's World
// (the only place the composite exists): it moves the cargo carried by the
// guest's components only. guestCraftID 0 matches any craft of that owner.
func (w *World) HandOverGuestCargo(idx int, guestOwner string, guestCraftID uint64) (kg float64, refusal string) {
	if idx < 0 || idx >= len(w.Crafts) || w.Crafts[idx] == nil || guestOwner == "" {
		return 0, CargoNotDocked
	}
	return w.handOverCargo(w.Crafts[idx], false, func(dc spacecraft.DockedComponent) bool {
		return dc.Owner == guestOwner && (guestCraftID == 0 || dc.CraftID == guestCraftID)
	})
}

func (w *World) handOverCargo(c *spacecraft.Spacecraft, unknown bool, mine func(spacecraft.DockedComponent) bool) (float64, string) {
	if len(c.DockedComponents) < 2 {
		return 0, CargoNotDocked
	}
	hold := -1
	for i, st := range c.Stages {
		if st.CargoHold {
			hold = i // lowest wins: the first hit
			break
		}
	}
	if hold < 0 {
		return 0, CargoNoHold
	}
	mineAt := stageIsMine(c, unknown, mine)
	var total float64
	for i, st := range c.Stages {
		if mineAt(i) {
			total += st.CargoKg
		}
	}
	if total <= 0 {
		return 0, CargoNoCargoAbd
	}
	if !w.canCommand(c) { // a hand-over is a command (ADR 0027 §4)
		return 0, "NO SIGNAL"
	}
	for i := range c.Stages {
		if mineAt(i) && c.Stages[i].CargoKg > 0 {
			c.Stages[hold].HoldKg += c.Stages[i].CargoKg
			c.Stages[i].CargoKg = 0
		}
	}
	c.SyncFields()
	c.State.M = c.TotalMass()
	return total, ""
}

// stageIsMine maps each live stage to its component's owner tag, the same
// sequential tiling Undock trusts. When the components do not tile the live
// stack (staging pops stages without touching DockedComponents) the mapping is
// unknowable: unknown is the answer for every stage then (the owner seat treats
// them as its own, the rider's press treats them as not its own, so a rider
// can never move the owner's cargo on a guess).
func stageIsMine(c *spacecraft.Spacecraft, unknown bool, mine func(spacecraft.DockedComponent) bool) func(int) bool {
	var owners []bool
	for _, dc := range c.DockedComponents {
		if len(dc.Stages) == 0 {
			owners = nil
			break
		}
		for range dc.Stages {
			owners = append(owners, mine(dc))
		}
	}
	if len(owners) != len(c.Stages) {
		return func(int) bool { return unknown }
	}
	return func(i int) bool { return owners[i] }
}
