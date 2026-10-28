package sim

import (
	"fmt"
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The batch release (ADR 0059 decision 3). A stack of two or more payloads
// above its lead leaves by a rotation fan, not the docking spring: the whole
// composite tumbles at BatchFanRateRadPerS about its centre of mass, frozen at
// the instant of release, so each piece keeps the sideways speed of its place
// in the stack and the carrier recoils the other way.

// BatchFanRateRadPerS is the tumble rate. The smallest rate at which no two
// pieces come closer than their release spacing, with margin (ADR 0059).
const BatchFanRateRadPerS = 2.5e-3

// batchFanMinAngleDeg is how far the stack must be off the velocity line. A
// stack along the velocity makes every kick radial, which returns every orbit
// and would hold the 10x clamp for ever.
const batchFanMinAngleDeg = 10.0

// BatchFanRefusalText is the player's reason when the stack lies along the velocity.
const BatchFanRefusalText = "release: stack along the velocity; hold radial (q) first"

// stackAxis is the composite's drawn axis: its attitude, else prograde.
func stackAxis(c *spacecraft.Spacecraft) orbital.Vec3 {
	if a := c.CurrentAttitudeDir.Unit(); a.Norm() != 0 {
		return a
	}
	return progradeUnit(c)
}

// IsBatch reports whether the vessel at idx is a batch: a same-player
// composite with two or more components above its lead whose record tiles the
// live stack, every component authored, the lead's included (the VAB or spawn
// form built it, so none carries the ID of a vessel that flew in; ADR 0063 §4). A pair,
// a CSM with its LM, a single-payload carrier or a flown dock chain is not,
// and keeps U's newest-seam release. Like Undock it repairs a stack that lost bottom stages
// (a spent F9-S1) before looking.
func (w *World) IsBatch(idx int) bool {
	if idx < 0 || idx >= len(w.Crafts) || w.Crafts[idx] == nil {
		return false
	}
	c := w.Crafts[idx]
	c.ReconcileDockedComponents()
	if len(c.DockedComponents) < 3 || DockSeams(c) == nil || StackHasGuest(c) {
		return false
	}
	for _, comp := range c.DockedComponents {
		if comp.CraftID != 0 {
			return false
		}
	}
	return true
}

// BatchFanRefusal says why a batch release refuses, or "" when it will go.
func BatchFanRefusal(c *spacecraft.Spacecraft) string {
	v := c.State.V
	if v.Norm() == 0 {
		return ""
	}
	// The 1e-9 is rounding slack so a stack at exactly 10 degrees releases.
	if math.Abs(stackAxis(c).Dot(v.Unit())) > math.Cos(batchFanMinAngleDeg*math.Pi/180)+1e-9 {
		return BatchFanRefusalText
	}
	return ""
}

// batchCentresM is each component's hull centre along the stack axis, measured
// from the carrier's hull centre. It mirrors where undockSplit places the
// pieces: a component's anchor sits at its top row ((rows through it - 1) x
// DockStrideM), its hull hangs back along -axis, and a hull spans half a row
// less than its length at the anchor end (dockPortTipInsetM). nil when the
// record does not tile.
func batchCentresM(c *spacecraft.Spacecraft) []float64 {
	if DockSeams(c) == nil {
		return nil
	}
	out := make([]float64, len(c.DockedComponents))
	off := 0
	for i, comp := range c.DockedComponents {
		off += len(comp.Stages)
		half := StackLengthM(comp.Stages, nil)/2 - dockPortTipInsetM
		if i == 0 {
			out[0] = half
			continue
		}
		out[i] = StackLengthM(c.Stages[:off], DockSeams(c)) - DockStrideM - half
	}
	base := out[0]
	for i := range out {
		out[i] -= base
	}
	return out
}

// applyBatchFan adds v_i = ω (d_i - d_cm) s to every restored piece, s the
// velocity's component perpendicular to the stack. Momentum is conserved by
// the definition of d_cm; the residual spin is dropped (the game has no spin
// state).
func (w *World) applyBatchFan(c *spacecraft.Spacecraft, restored []*spacecraft.Spacecraft, axis orbital.Vec3) {
	d := batchCentresM(c)
	if len(d) != len(restored) {
		return
	}
	v := c.State.V
	s := v.Sub(axis.Scale(axis.Dot(v)))
	if s.Norm() == 0 {
		return
	}
	s = s.Unit()
	var m, md float64
	for i, r := range restored {
		m += r.TotalMass()
		md += r.TotalMass() * d[i]
	}
	if m <= 0 {
		return
	}
	cm := md / m
	for i, r := range restored {
		r.State.V = r.State.V.Add(s.Scale(BatchFanRateRadPerS * (d[i] - cm)))
	}
}

// stampBatch mints the batch id from NextCraftID (one counter, ADR 0012),
// marks the lead, and stamps the one-orbit clamp exemption (decision 6) on the
// lead and every member. An unbound orbit has no period, so no exemption.
func (w *World) stampBatch(c *spacecraft.Spacecraft, restored []*spacecraft.Spacecraft) {
	if w.NextCraftID == 0 {
		w.NextCraftID = 1
	}
	id := w.NextCraftID
	w.NextCraftID++
	var until time.Time
	if w.Clock != nil {
		if T := orbitalPeriod(c.State, c.Primary.GravitationalParameter()); !math.IsInf(T, 0) {
			until = w.Clock.SimTime.Add(time.Duration(T * float64(time.Second)))
		}
	}
	for i, r := range restored {
		r.BatchID, r.BatchLead, r.BatchClampExemptUntil = id, i == 0, until
	}
}

// ReleaseBatch releases the whole batch at idx by the fan. The lead stays at
// idx (and active); the pieces are named and appended to the slate. False when
// the vessel is not a batch or either refusal holds.
func (w *World) ReleaseBatch(idx int) bool {
	if !w.IsBatch(idx) || w.UndockRefusal(idx) != "" || BatchFanRefusal(w.Crafts[idx]) != "" {
		return false
	}
	return w.undockSplit(idx, true)
}

// BatchReleaseCard is what the U card prints: the payload name, how many
// pieces leave, and the attitude-free facts (the speed step between
// neighbours and the top speed relative to the carrier, in cm/s).
func (w *World) BatchReleaseCard(idx int) (name string, n int, note string, ok bool) {
	if !w.IsBatch(idx) {
		return "", 0, "", false
	}
	c := w.Crafts[idx]
	d := batchCentresM(c)
	if d == nil {
		return "", 0, "", false
	}
	n = len(d) - 1
	seam := (d[2] - d[1]) * BatchFanRateRadPerS * 100
	top := (d[n] - d[0]) * BatchFanRateRadPerS * 100
	return c.DockedComponents[1].Name, n,
		fmt.Sprintf("they fan out sideways, %.1f cm/s a seam, top %.0f", seam, top), true
}
