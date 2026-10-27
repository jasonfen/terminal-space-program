// Package sim: the Flight Recording hooks (ADR 0062). Rows go to the Active
// Vessel's file only, written as things happen. Every entry point is nil-safe:
// with World.Recording == nil nothing is read or written.
package sim

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/recorder"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const (
	recBurnSample  = time.Second
	recCoastSample = 60 * time.Second
	recWallCap     = 20 // ticks: one numbers row per wall-second at BaseStep 50 ms
)

// recState is the recorder's per-World bookkeeping.
type recState struct {
	accrued  time.Duration // sim-time since the last numbers row
	ticks    int           // ticks since the last numbers row
	vesselID uint64        // whose file Recording is
	t0       time.Time     // T+0 when the vessel has no liftoff stamp: its first row
	stalled  *ActiveBurn   // the burn whose stall row was already written
}

// OpenActiveRecording closes any open recording and opens the Active Vessel's
// file under RecordingsDir (empty = recording off). The caller (tui, serve)
// sets RecordingsDir and RecordingWindowDays first.
func (w *World) OpenActiveRecording() error {
	w.CloseRecording()
	c := w.ActiveCraft()
	if w.RecordingsDir == "" || c == nil || w.Clock == nil {
		return nil
	}
	// A staged vessel is renamed mid-flight; its file keeps the name it was
	// created under, so look the ID up before minting a new slug.
	prefix := fmt.Sprintf("%04d-", c.ID)
	path := filepath.Join(w.RecordingsDir, prefix+recorder.Slug(c.Name)+".txt")
	if m, _ := filepath.Glob(filepath.Join(w.RecordingsDir, prefix+"*.txt")); len(m) > 0 {
		path = m[0]
	}
	h := recorder.Header{VesselID: c.ID, Name: c.Name, System: w.System().Name, Primary: c.Primary.EnglishName,
		Liftoff: c.LiftoffSimTime, LiftoffKnown: !c.LiftoffSimTime.IsZero()}
	if c.HasLaunchSite {
		h.Site = fmt.Sprintf("%s %s", latStr(c.LaunchLatDeg), lonStr(c.LaunchLonDeg))
	}
	r, err := recorder.Open(path, h, w.RecordingWindowDays, time.Now)
	if err != nil {
		return err
	}
	w.Recording = r
	w.rec = recState{vesselID: c.ID}
	return nil
}

// CloseRecording closes the open file, if any.
func (w *World) CloseRecording() {
	if w.Recording != nil {
		_ = w.Recording.Close()
		w.Recording = nil
	}
}

func latStr(d float64) string {
	return fmt.Sprintf("%.1f%s", math.Abs(d), map[bool]string{true: "N", false: "S"}[d >= 0])
}
func lonStr(d float64) string {
	return fmt.Sprintf("%.1f%s", math.Abs(d), map[bool]string{true: "E", false: "W"}[d >= 0])
}

// recFor is the guard every hook goes through: the row's vessel must be the
// Active Vessel and a recording must be open.
func (w *World) recFor(c *spacecraft.Spacecraft) bool {
	return w.Recording != nil && w.Clock != nil && c != nil && c == w.ActiveCraft()
}

func (w *World) recTPlus(c *spacecraft.Spacecraft, sim time.Time) (time.Duration, bool) {
	if !c.LiftoffSimTime.IsZero() {
		return sim.Sub(c.LiftoffSimTime), true
	}
	if c.OnPad {
		return 0, false
	}
	if w.rec.t0.IsZero() {
		w.rec.t0 = sim
	}
	return sim.Sub(w.rec.t0), true
}

// holdText is the GUIDANCE box's hold cell: name and frame tag.
func (w *World) holdText(c *spacecraft.Spacecraft) string {
	name, frame := w.AttitudeHoldParts(c.AttitudeMode)
	tag := "ORBIT"
	switch frame {
	case NavSurface:
		tag = "SURF"
	case NavTarget:
		tag = "TGT"
	case NavPort:
		tag = "PORT"
	}
	return fmt.Sprintf("%s (%s)", name, tag)
}

// recApsides: altitudes of apo/peri above the surface, ok false when the
// orbit is not a bound ellipse or the vessel is on the ground.
func recApsides(c *spacecraft.Spacecraft) (apo, peri, incDeg float64, ok bool) {
	if c.Landed || c.Crashed {
		return 0, 0, 0, false
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
	if el.E >= 1 || el.A <= 0 {
		return 0, 0, 0, false
	}
	r := c.Primary.RadiusMeters()
	return el.A*(1+el.E) - r, el.A*(1-el.E) - r, el.I * 180 / math.Pi, true
}

func (w *World) rowFor(c *spacecraft.Spacecraft, event string) recorder.Row {
	sim := w.Clock.SimTime
	tp, known := w.recTPlus(c, sim)
	apo, peri, _, ok := recApsides(c)
	return recorder.Row{
		Sim: sim, TPlus: tp, TPlusKnown: known,
		AltM: c.Altitude(), SpdMS: c.State.V.Norm(), MassKg: c.TotalMass(), ThrottlePct: c.Throttle * 100,
		Stages: len(c.Stages), SOI: c.Primary.EnglishName, Hold: w.holdText(c),
		PitchTrimDeg: c.PitchTrim * 180 / math.Pi, HeadTrimDeg: c.HeadingTrim * 180 / math.Pi,
		ApoM: apo, PeriM: peri, ApsidesOK: ok, Event: event,
	}
}

// recordRowFor writes one row for c if c is the Active Vessel and recording.
func (w *World) recordRowFor(c *spacecraft.Spacecraft, event string) {
	if w.recFor(c) {
		_ = w.Recording.Write(w.rowFor(c, event))
	}
}

// recordRow writes one row for the Active Vessel.
func (w *World) recordRow(event string) { w.recordRowFor(w.ActiveCraft(), event) }

// recordSample is the tick hook: a numbers row every second while the Active
// Vessel thrusts, every 60 s otherwise, never more than one per recWallCap ticks.
func (w *World) recordSample(simDelta time.Duration) {
	c := w.ActiveCraft()
	if !w.recFor(c) {
		return
	}
	w.rec.accrued += simDelta
	w.rec.ticks++
	n := recCoastSample
	if c.ActiveBurn != nil || c.ManualBurn != nil {
		n = recBurnSample
	}
	if w.rec.accrued >= n && w.rec.ticks >= recWallCap {
		w.rec.accrued, w.rec.ticks = 0, 0
		w.recordRow("")
	}
}

// orbitText is "orbit 250 x 250 km, incl 28.6", or "" when not a bound orbit.
func orbitText(c *spacecraft.Spacecraft) string {
	apo, peri, inc, ok := recApsides(c)
	if !ok {
		return ""
	}
	return fmt.Sprintf("orbit %s x %s km, incl %.1f", recorder.Grouped(apo/1000), recorder.Grouped(peri/1000), inc)
}

// recordAction writes the verb row (ADR 0062 decision 3), with the curated
// verbs' numbers. stage, undock, deploy and spawn_craft stay bare: their
// lifecycle rows carry the vessel names.
func (w *World) recordAction(a missions.Action) {
	c := w.ActiveCraft()
	if !w.recFor(c) {
		return
	}
	text := string(a)
	var detail string
	switch a {
	case missions.ActionThrottleCut, missions.ActionThrottleFull:
		detail = orbitText(c)
	case missions.ActionPlanTransfer, missions.ActionPlanCircularize, missions.ActionPlanIncl,
		missions.ActionPlanRendezvous, missions.ActionRefinePlan:
		if len(c.Nodes) > 0 {
			n := c.Nodes[len(c.Nodes)-1]
			detail = fmt.Sprintf("Δv %s %s", recorder.Grouped(n.DV), n.Mode)
			if tp, ok := w.recTPlus(c, n.TriggerTime); ok && !n.TriggerTime.IsZero() {
				detail += ", burn T+" + recorder.TPlus(tp)
			}
			if n.Duration > 0 {
				s := int(n.Duration.Seconds())
				detail += fmt.Sprintf(" (%dm%02ds)", s/60, s%60)
			}
		}
	case missions.ActionCycleTarget, missions.ActionClearTarget:
		detail = w.TargetName()
	}
	if detail != "" {
		text += ": " + detail
	}
	w.recordRowFor(c, text)
}

// recordSOI writes the SOI-switch row after c.Primary has changed from `from`.
func (w *World) recordSOI(c *spacecraft.Spacecraft, from string) {
	w.recordRowFor(c, fmt.Sprintf("soi: %s → %s, %s m/s vs %s", from, c.Primary.EnglishName,
		recorder.Grouped(c.State.V.Norm()), c.Primary.EnglishName))
}

// recordSurface writes touchdown or crash, with the pre-clamp impact speed.
func (w *World) recordSurface(c *spacecraft.Spacecraft, impactMS float64) {
	if !w.recFor(c) {
		return
	}
	kind, lat, lon := "touchdown", c.LandedLatDeg, c.LandedLonDeg
	if c.Crashed {
		kind = "crash"
		lat, lon = c.SurfaceLatLon()
	}
	w.recordRowFor(c, fmt.Sprintf("%s: %s %s %s, %s m/s", kind, c.Primary.EnglishName, latStr(lat), lonStr(lon), recorder.Grouped(impactMS)))
}

// recordLiftoff is called from leavePad right after the stamp, so the row's
// sim time is the stamp and T+ reads 0:00:00 whichever clock the site ran on.
func (w *World) recordLiftoff(c *spacecraft.Spacecraft) {
	if !w.recFor(c) {
		return
	}
	_ = w.Recording.SetLiftoff(c.LiftoffSimTime)
	w.recordRowFor(c, "liftoff")
}

// recordBurnComplete writes the burn-complete row before the ActiveBurn is cleared.
func (w *World) recordBurnComplete(c *spacecraft.Spacecraft) {
	ab := c.ActiveBurn
	if !w.recFor(c) || ab == nil {
		return
	}
	t := fmt.Sprintf("burn complete: %s of %s m/s", recorder.Grouped(ab.PlannedDV-ab.DVRemaining), recorder.Grouped(ab.PlannedDV))
	if o := orbitText(c); o != "" {
		t += ", " + o
	}
	w.recordRowFor(c, t)
}

// recordBurnStalled writes the fuel-out row once per burn.
func (w *World) recordBurnStalled(c *spacecraft.Spacecraft) {
	ab := c.ActiveBurn
	if !w.recFor(c) || ab == nil || w.rec.stalled == ab {
		return
	}
	w.rec.stalled = ab
	w.recordRowFor(c, fmt.Sprintf("burn stalled: fuel out, %s of %s m/s", recorder.Grouped(ab.PlannedDV-ab.DVRemaining), recorder.Grouped(ab.PlannedDV)))
}

// recordActiveSwitch is SetActiveCraftIdx's outgoing half: the last row of the
// vessel being left names the vessel being taken.
func (w *World) recordActiveSwitch(to *spacecraft.Spacecraft) {
	if a := w.ActiveCraft(); to != nil && a != nil && to.ID != a.ID {
		w.recordRow("active vessel → " + to.Name)
	}
}

// swapRecording is the incoming half: when the Active Vessel's file is not the
// open one, switch files and write the vessel's first row. Only with a
// RecordingsDir; a Recording set by hand stays put.
func (w *World) swapRecording() {
	c := w.ActiveCraft()
	if w.RecordingsDir == "" || c == nil || (w.Recording != nil && w.rec.vesselID == c.ID) {
		return
	}
	if w.OpenActiveRecording() == nil {
		w.recordRow("active vessel → " + c.Name)
	}
}

// recordStage writes the lifecycle stage row: what left and what is lit now.
func (w *World) recordStage(c *spacecraft.Spacecraft, dropped []spacecraft.Stage) {
	if !w.recFor(c) {
		return
	}
	names := make([]string, len(dropped))
	for i, s := range dropped {
		names[i] = s.Name
	}
	w.recordRowFor(c, fmt.Sprintf("stage: %s jettisoned, %s lit", strings.Join(names, "+"), c.Stages[0].Name))
}

// recordSplit writes undock or batch release for the restored vessels (the
// lead first, already Active).
func (w *World) recordSplit(restored []*spacecraft.Spacecraft, composite string, batch bool) {
	if len(restored) < 2 || w.Recording == nil {
		return
	}
	if batch {
		w.recordRow(fmt.Sprintf("release: %d of %s", len(restored)-1, composite))
		return
	}
	names := make([]string, 0, len(restored)-1)
	for _, s := range restored[1:] {
		names = append(names, s.Name)
	}
	w.recordRow("undock: " + strings.Join(names, ", "))
}
