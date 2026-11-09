// Package sim: the Flight Recording hooks (ADR 0062). Rows go to the Active
// Vessel's file only, written as things happen. Every entry point is nil-safe:
// with World.Recording == nil nothing is read or written.
package sim

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/planner"
	"github.com/jasonfen/terminal-space-program/internal/recorder"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
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
	hold     string        // the hold last on file ("" = not yet seen)
	warp     int           // Clock.WarpIdx+1 last on file (0 = not yet seen)
	chute    string        // chute state last on file ("" = not yet seen)
	nav      string        // nav frame last on file ("" = not yet seen)
	ap       string        // autopilot state last on file ("" = not yet seen)
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
	if !h.LiftoffKnown {
		h.FirstRow = w.Clock.SimTime
	}
	if c.HasLaunchSite {
		h.Site = fmt.Sprintf("%s %s", latStr(c.LaunchLatDeg), lonStr(c.LaunchLonDeg))
	}
	r, err := recorder.Open(path, h, w.RecordingWindowDays, time.Now, w.Clock.SimTime)
	if err != nil {
		w.noteRecordingError(err)
		return err
	}
	// A vessel that lifted off while another was Active was stamped without a
	// recordLiftoff; its header still says "no liftoff on record" (#817).
	if h.LiftoffKnown && !r.T0().Equal(h.Liftoff) {
		if err := r.SetLiftoff(h.Liftoff); err != nil {
			w.noteRecordingError(err)
		}
	}
	w.Recording = r
	w.rec = recState{vesselID: c.ID, t0: r.T0(), hold: w.holdText(c), warp: w.Clock.WarpIdx + 1, chute: strings.ToLower(c.ChuteState.String()),
		nav: w.NavMode.Tag(), ap: w.apLabel(c)}
	if w.recMarker != "" && !w.recMarked[c.ID] {
		if w.recMarked == nil {
			w.recMarked = map[uint64]bool{}
		}
		w.recMarked[c.ID] = true
		w.recordRow(w.recMarker)
	}
	return nil
}

// NoteRecordingError records the first recording failure (a full disk, a
// read-only saves dir) for the App to flash once; later ones stay quiet.
func (w *World) NoteRecordingError(err error) {
	if err == nil || w.recFaulted {
		return
	}
	w.recFaulted, w.RecordingFault = true, err.Error()
}

func (w *World) noteRecordingError(err error) { w.NoteRecordingError(err) }

// SetRecordingMarker sets the row each vessel's file gets the first time it is
// opened under this World (`session start: <version>`, or `loaded save <id>`
// after a load), and forgets which vessels already got one.
func (w *World) SetRecordingMarker(text string) {
	w.recMarker, w.recMarked = text, nil
}

// NewGameFolder creates and returns a fresh game folder name under dir, named
// for the wall-clock minute (2026-10-27-1843); a taken name gets -2, -3, ...
func NewGameFolder(dir string, now time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := now.Format("2006-01-02-1504")
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = fmt.Sprintf("%s-%d", base, n)
		}
		err := os.Mkdir(filepath.Join(dir, name), 0o755)
		if err == nil {
			return name, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
}

// RemoveAbandonedGameFolder deletes dir/folder when every file in it holds at
// most one dated row and that row is a session marker ("session start:" or
// "loaded save"): the first-tick folder of a game that was abandoned by an
// immediate load (#817). Anything else in the folder keeps it. Best effort.
func RemoveAbandonedGameFolder(dir, folder string) {
	if folder == "" || folder != filepath.Base(folder) || folder == "." || folder == ".." {
		return
	}
	path := filepath.Join(dir, folder)
	ents, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(path, e.Name()))
		if err != nil || e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			return
		}
		rows := 0
		for _, l := range strings.Split(string(b), "\n") {
			if _, ok := recorder.RowTime(l); !ok {
				continue
			}
			if rows++; rows > 1 || !(strings.Contains(l, "session start:") || strings.Contains(l, "loaded save ")) {
				return
			}
		}
	}
	_ = os.RemoveAll(path)
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
	return fmt.Sprintf("%s (%s)", name, frame.Tag())
}

// recApsides: altitudes of apo/peri above the surface, ok false when the
// orbit is not a bound ellipse or the vessel is on the ground.
func recApsides(c *spacecraft.Spacecraft) (apo, peri, incDeg float64, ok bool) {
	// Less than a metre up is the ground: the liftoff row is written the
	// instant Landed clears, when the pad's co-rotation "orbit" is underground.
	if c.Landed || c.Crashed || c.Altitude() < 1 {
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
	// thr reads thrust, not the lever: 0 unless the engine is firing.
	thr := 0.0
	if c.ActiveBurn != nil || c.ManualBurn != nil {
		thr = c.Throttle * 100
	}
	row := recorder.Row{
		Sim: sim, TPlus: tp, TPlusKnown: known,
		AltM: c.Altitude(), SpdMS: c.State.V.Norm(), MassKg: c.TotalMass(), ThrottlePct: thr,
		Stages: len(c.Stages), SOI: c.Primary.EnglishName, Hold: w.holdText(c),
		PitchTrimDeg: c.PitchTrim * 180 / math.Pi, HeadTrimDeg: c.HeadingTrim * 180 / math.Pi,
		ApoM: apo, PeriM: peri, ApsidesOK: ok, Event: event,
	}
	w.recordTarget(c, &row)
	return row
}

// recordCloseHorizonSec is the closest-approach search, the TARGET box's 4 h.
const recordCloseHorizonSec = 4 * 3600.0

// recordTarget fills the row's target columns from the readings the TARGET
// box shows, for a vessel target on c's primary (2026-11-03: a lunar
// rendezvous had to be rebuilt from an autosave because the file said nothing
// about the CSM). Landed, only range and relative speed are read.
func (w *World) recordTarget(c *spacecraft.Spacecraft, row *recorder.Row) {
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok || c != w.ActiveCraft() {
		return
	}
	row.Target = w.TargetName()
	row.RangeM = rT.Sub(c.State.R).Norm()
	row.RelMS = vT.Sub(c.State.V).Norm()
	row.CloseM, row.DInclDeg, row.LeadDeg = math.NaN(), math.NaN(), math.NaN()
	if c.Landed {
		return
	}
	// The search flies both on c's primary, so like the TARGET box it runs
	// only for a target sharing it.
	if w.TargetSharesActivePrimary() {
		mu := c.Primary.GravitationalParameter()
		tca, d, _, err := planner.NextClosestApproach(orbital.Vec3State{R: c.State.R, V: c.State.V},
			orbital.Vec3State{R: rT, V: vT}, c.Primary, mu, recordCloseHorizonSec)
		switch {
		case err != nil:
		case planner.ApproachBeyond(tca, recordCloseHorizonSec):
			row.TCA = time.Duration(recordCloseHorizonSec * float64(time.Second)) // close stays NaN: tca reads >4:00:00
		default:
			row.CloseM, row.TCA = d, time.Duration(tca*float64(time.Second))
		}
	}
	if n, ok := w.TargetPlaneNormal(); ok {
		h := c.State.R.Cross(c.State.V)
		if hn, nn := h.Norm(), n.Norm(); hn > 0 && nn > 0 {
			a := math.Acos(math.Max(-1, math.Min(1, h.Dot(n)/(hn*nn)))) * 180 / math.Pi
			row.DInclDeg = math.Min(a, 180-a) // the TARGET box's Δincl
		}
	}
	if lead, ok := w.TargetLeadAngleDeg(); ok {
		row.LeadDeg = lead
	}
}

// recordRowFor writes one row for c if c is the Active Vessel and recording.
func (w *World) recordRowFor(c *spacecraft.Spacecraft, event string) {
	if w.recFor(c) {
		w.recordHoldChange(c)
		w.noteRecordingError(w.Recording.Write(w.rowFor(c, event)))
	}
}

// recordHoldChange writes `hold: <label>` once when the Active Vessel's hold
// label differs from the last one on file, whatever changed it (key, nav-mode
// remap, dangling-port reset, undock). Called before every row and each tick.
func (w *World) recordHoldChange(c *spacecraft.Spacecraft) {
	w.recordWarpChange(c)
	w.recordChuteChange(c)
	w.recordNavChange(c)
	w.recordAutopilotChange(c)
	h := w.holdText(c)
	if h == w.rec.hold {
		return
	}
	seen := w.rec.hold != ""
	w.rec.hold = h
	if seen {
		w.noteRecordingError(w.Recording.Write(w.rowFor(c, "hold: "+h)))
	}
}

// recordWarpChange writes `warp: <N>x` when Clock.WarpIdx differs from the last
// one on file, whatever moved it (keys, auto-warp hand-offs, a picker). The
// 10x burn clamp does not move the index, so it writes nothing.
func (w *World) recordWarpChange(c *spacecraft.Spacecraft) {
	n := w.Clock.WarpIdx + 1
	if n == w.rec.warp {
		return
	}
	seen := w.rec.warp != 0
	w.rec.warp = n
	if seen {
		w.noteRecordingError(w.Recording.Write(w.rowFor(c, fmt.Sprintf("warp: %gx", WarpFactors[w.Clock.WarpIdx]))))
	}
}

// recordChuteChange writes `chute: armed` / `chute: deployed` when the chute
// state differs from the last one on file. Arming is a key in the tui and
// deploying happens mid-integration, so both are caught here, once a tick.
func (w *World) recordChuteChange(c *spacecraft.Spacecraft) {
	st := strings.ToLower(c.ChuteState.String())
	if st == w.rec.chute {
		return
	}
	seen := w.rec.chute != ""
	w.rec.chute = st
	if seen && c.ChuteState != spacecraft.ChuteStowed {
		w.noteRecordingError(w.Recording.Write(w.rowFor(c, "chute: "+st)))
	}
}

// recordRCS writes one row per fired RCS pulse, after the Δv is applied.
func (w *World) recordRCS(c *spacecraft.Spacecraft, mode spacecraft.BurnMode) {
	w.recordRowFor(c, fmt.Sprintf("rcs: %s %g m/s", mode, c.RCSPulseDV()))
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
	w.recordHoldChange(c)
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
	case missions.ActionThrottleUp, missions.ActionThrottleDown:
		detail = fmt.Sprintf("%d%%", int(math.Round(c.Throttle*100)))
	case missions.ActionToggleBurn:
		detail = "cut"
		if c.ManualBurn != nil {
			detail = "lit"
		}
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

// recordSurface writes touchdown or crash at the contact site, with the
// pre-clamp impact speed.
func (w *World) recordSurface(c *spacecraft.Spacecraft, impactMS, lat, lon float64) {
	if !w.recFor(c) {
		return
	}
	kind := "touchdown"
	if c.Crashed {
		kind = "crash"
	}
	w.recordRowFor(c, fmt.Sprintf("%s: %s %s %s, %s m/s", kind, c.Primary.EnglishName, latStr(lat), lonStr(lon), recorder.Grouped(impactMS)))
}

// recordLiftoff is called from leavePad right after the stamp, so the row's
// sim time is the stamp and T+ reads 0:00:00 whichever clock the site ran on.
func (w *World) recordLiftoff(c *spacecraft.Spacecraft) {
	if !w.recFor(c) {
		return
	}
	w.noteRecordingError(w.Recording.SetLiftoff(c.LiftoffSimTime))
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

// RecordEvent writes a free-text row for the Active Vessel (S6: `loaded save
// <name>`). Nil-safe.
func (w *World) RecordEvent(text string) { w.recordRow(text) }

// bumpSpeedText is the bump row's closing speed: one decimal under 10 m/s
// (judged after rounding, so 9.95 reads "10", never "10.0"), grouped integer
// from there.
func bumpSpeedText(s float64) string {
	if math.Round(s*10)/10 < 10 {
		return fmt.Sprintf("%.1f", s)
	}
	return recorder.Grouped(s)
}

// recordBump writes the hull-bump row for c (the Active Vessel, one of the
// pair), after the velocity kick so the numbers columns are the post-kick state.
func (w *World) recordBump(c *spacecraft.Spacecraft, partner string, speedMS float64, latched bool) {
	t := fmt.Sprintf("bump: %s, %s m/s", partner, bumpSpeedText(speedMS))
	if latched {
		t += ", re-arm latched"
	}
	w.recordRowFor(c, t)
}

// InputSnap is the Active Vessel's trajectory inputs at one moment, taken
// before a key is handled; RecordInputChanges diffs against it.
type InputSnap struct {
	w                   *World
	c                   *spacecraft.Spacecraft
	pitch, head         float64
	portPitch, portYaw  float64
	aimAlong, aimAcross float64
	rodOn               bool
	rodRate             float64
	inertial            orbital.Vec3
}

// InputSnapshot reads the Active Vessel's trajectory inputs.
func (w *World) InputSnapshot() InputSnap {
	c := w.ActiveCraft()
	if c == nil {
		return InputSnap{}
	}
	s := InputSnap{w: w, c: c, pitch: c.PitchTrim, head: c.HeadingTrim,
		portPitch: c.PortPitchTrim, portYaw: c.PortYawTrim, inertial: c.InertialDir}
	if ap := c.Landing; ap != nil {
		s.aimAlong, s.aimAcross, s.rodOn, s.rodRate = ap.AimAlongM, ap.AimAcrossM, ap.ROD, ap.RODRateMps
	}
	return s
}

// NoteTrimReset labels the next RecordInputChanges rows as a `|` reset.
func (w *World) NoteTrimReset() { w.recReset = true }

// recDeg is a trim in degrees as the file prints it: no trailing zeros, U+2212
// for minus, no "\u22120".
func recDeg(rad float64) string {
	d := math.Round(rad*180/math.Pi*100) / 100
	return recSigned(d, "") + "\u00b0"
}

// recSigned prints v with U+2212 for minus and no sign on zero; plus adds "+" to positives.
func recSigned(v float64, plus string) string {
	s := strconv.FormatFloat(math.Abs(v), 'f', -1, 64)
	switch {
	case s == "0":
		return s
	case v < 0:
		return "\u2212" + s
	}
	return plus + s
}

// recDist is a distance the way the HUD's miss: signs it: + long, U+2212 short.
func recDist(m float64) string {
	switch {
	case readout.Distance(math.Abs(m)) == readout.Distance(0):
		return readout.Distance(0)
	case m > 0:
		return "+" + readout.Distance(m)
	}
	return "\u2212" + readout.Distance(-m)
}

// recCross is the HUD's cross: spelling: the distance, then R or L.
func recCross(m float64) string {
	switch {
	case readout.Distance(math.Abs(m)) == readout.Distance(0):
		return readout.Distance(0)
	case m > 0:
		return readout.Distance(m) + " R"
	}
	return readout.Distance(-m) + " L"
}

// landReadText is the readout the pilot flies by under [SITE], as the
// TARGET box signs it.
func landReadText(lb LandBeside) string {
	if !lb.MissOK {
		return ""
	}
	t := "miss " + recDist(lb.MissM) + " cross " + recCross(lb.CrossM)
	if lb.Planned {
		t += " (plan)"
	}
	return t
}

// siteTail is " \u00b7 miss ... cross ..." while the SITE frame is up and the
// reading exists, else "".
func (w *World) siteTail() string {
	if w.NavMode != NavSite {
		return ""
	}
	if lb, ok := w.LandBesideReading(); ok && lb.MissOK {
		return " \u00b7 " + landReadText(lb)
	}
	return ""
}

// RecordInputChanges writes one row per input family the key just handled
// changed (trims, aim, descent rate, inertial nudge); a key that changed
// nothing writes nothing. Called once per key press, never per tick.
func (w *World) RecordInputChanges(before InputSnap) {
	c := w.ActiveCraft()
	reset := w.recReset
	w.recReset = false
	if before.w != w || before.c != c || !w.recFor(c) {
		return
	}
	w.recordHoldChange(c) // a frame or autopilot change the key made lands before its trim row
	label := func(l string) string {
		if reset {
			return l + " reset"
		}
		return l
	}
	switch {
	case c.PortPitchTrim != before.portPitch || c.PortYawTrim != before.portYaw:
		w.recordRowFor(c, fmt.Sprintf("%s: port pitch %s yaw %s", label("trim"), recDeg(c.PortPitchTrim), recDeg(c.PortYawTrim)))
	case c.PitchTrim != before.pitch || c.HeadingTrim != before.head:
		t := fmt.Sprintf("%s: pitch %s hdg %s", label("trim"), recDeg(c.PitchTrim), recDeg(c.HeadingTrim))
		var marks []string
		if before.head == 0 && c.HeadingTrim != 0 {
			marks = append(marks, "heading seeded")
			if before.pitch*c.PitchTrim < 0 {
				marks = append(marks, "pitch sign flipped")
			}
		}
		if len(marks) > 0 {
			t += " (" + strings.Join(marks, ", ") + ")"
		}
		w.recordRowFor(c, t+w.siteTail())
	}
	if c.InertialDir != before.inertial && before.inertial.Norm() > 0 {
		a := math.Acos(math.Max(-1, math.Min(1, c.InertialDir.Unit().Dot(before.inertial.Unit())))) * 180 / math.Pi
		w.recordRowFor(c, fmt.Sprintf("trim: inertial nudged %s", recDeg(a*math.Pi/180)))
	}
	if ap := c.Landing; ap != nil {
		if ap.AimAlongM != before.aimAlong || ap.AimAcrossM != before.aimAcross {
			w.recordRowFor(c, fmt.Sprintf("%s: %s cross %s%s", label("aim"), recDist(ap.AimAlongM), recCross(ap.AimAcrossM), w.siteTail()))
		}
		switch {
		case ap.ROD && (!before.rodOn || ap.RODRateMps != before.rodRate):
			w.recordRowFor(c, "rod: "+recSigned(ap.RODRateMps, "+")+" m/s")
		case !ap.ROD && before.rodOn:
			w.recordRowFor(c, "rod: off")
		}
	}
}

// apLabel names the landing autopilot's state for the file.
func (w *World) apLabel(c *spacecraft.Spacecraft) string {
	switch ap := c.Landing; {
	case ap == nil || !ap.Engaged:
		return "off"
	case ap.Nose:
		return "Site+ nose hold"
	}
	return "Site+ auto"
}

// recordNavChange writes `nav: <frame>` when the nav frame differs from the
// last one on file, whether `;` or the automatic switch moved it.
func (w *World) recordNavChange(c *spacecraft.Spacecraft) {
	n := w.NavMode.Tag()
	if n == w.rec.nav {
		return
	}
	seen := w.rec.nav != ""
	w.rec.nav = n
	if seen {
		w.noteRecordingError(w.Recording.Write(w.rowFor(c, "nav: "+n)))
	}
}

// recordAutopilotChange writes `autopilot: <state>` on engage, disengage and
// the nose hold, whatever changed it (keys, landing end, link loss).
func (w *World) recordAutopilotChange(c *spacecraft.Spacecraft) {
	a := w.apLabel(c)
	if a == w.rec.ap {
		return
	}
	seen := w.rec.ap != ""
	w.rec.ap = a
	if seen {
		w.noteRecordingError(w.Recording.Write(w.rowFor(c, "autopilot: "+a)))
	}
}

// recordDeorbit writes the plan line N and B plant.
func (w *World) recordDeorbit(p *DeorbitPlan) {
	aim, side, long := "stop", "long", p.LongM
	if p.Impact {
		aim = "impact"
	}
	if long < 0 {
		side, long = "short", -long
	}
	w.recordRow(fmt.Sprintf("deorbit: %s, \u0394v %s m/s retro in %s, %s %s %s", p.Site, recorder.Grouped(p.DV),
		recorder.TPlus(p.In), aim, readout.Distance(long), side))
}
