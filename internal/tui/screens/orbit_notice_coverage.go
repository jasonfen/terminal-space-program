package screens

import (
	"fmt"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// buildCoverageChip is the COVERAGE notice (ADR 0065 decision 6): while the
// active rung's current objective is lunar_coverage, the covered hours of
// the lap and the next gap, read from the same World.SiteCoverage memo the
// evaluator passes on (one source, so the row cannot disagree with the
// pass). Nil otherwise, so it is gone the frame the objective passes.
// coverageSource is what the COVERAGE notice and the linked-now overlay read:
// a targeted surface point wins; else the rung (#862: any in-progress mission
// whose current objective is lunar_coverage, not only the active one).
func coverageSource(w *sim.World) (primaryID, label string, lat, lon, lap, step float64, ok bool) {
	step = 60
	if t, found := w.TargetCoverage(); found {
		return t.BodyID, t.Label, t.Lat, t.Lon, t.LapS, step, true
	}
	for i := range w.Missions {
		m := w.Missions[i]
		if m.Status != missions.InProgress {
			continue
		}
		o, found := m.CurrentObjective()
		if !found || o.Kind != missions.KindLunarCoverage || o.Params.PrimaryID == "" {
			continue
		}
		p := o.Params
		primaryID, lat, lon, lap = p.PrimaryID, p.SiteLatDeg, p.SiteLonDeg, p.LapS
		if p.SampleS > 0 {
			step = p.SampleS
		}
		if lap <= 0 {
			lap = 108000
		}
		label = fmt.Sprintf("lat %.0f°", lat)
		switch lat {
		case -90:
			label = "south pole"
		case 90:
			label = "north pole"
		}
		return primaryID, label, lat, lon, lap, step, true
	}
	return "", "", 0, 0, 0, 0, false
}

func (v *OrbitView) buildCoverageChip(w *sim.World) []string {
	primaryID, label, lat, lon, lap, step, ok := coverageSource(w)
	if !ok {
		return nil
	}
	if r := []rune(label); len(r) > 22 {
		label = string(r[:21]) + "…"
	}
	cov, covOK := w.SiteCoverage(primaryID, lat, lon, lap, step)
	if !covOK || len(cov.Covered) == 0 {
		return nil
	}
	now := coverageNow(w.Clock.SimTime.Sub(cov.Epoch).Seconds(), cov.StepS, len(cov.Covered))
	body := primaryID
	sys := w.System()
	if b := sys.FindBody(primaryID); b != nil {
		body = b.EnglishName
	}
	relays := fmt.Sprintf("%d relays at the %s", cov.RelayCount, body)
	if cov.RelayCount == 1 {
		relays = "1 relay at the " + body
	}
	second := coverageSecondRow(cov.Covered, now, cov.StepS, relays)
	return []string{
		v.theme.Primary.Render("COVERAGE") + v.theme.Dim.Render(fmt.Sprintf("  %s %s", body, label)),
		v.theme.Dim.Render(fmt.Sprintf("  covered %s of %s", hm(coverageCoveredS(cov.Covered, lap)), hm(lap))),
		v.theme.Dim.Render(second),
	}
}

// coverageSecondRow derives the notice's second row from the sampled lap:
// the sample at now is the current state, the first uncovered sample at or
// after it the next gap, the run of uncovered samples after that its length.
func coverageSecondRow(covered []bool, now int, stepS float64, relays string) string {
	// The lap repeats (sample n-1 is sample 0 a lap on), so look around the
	// ring of n-1 samples: a gap ahead may lie past the array's end.
	m := len(covered) - 1
	if m < 1 {
		m = 1
	}
	at := func(i int) bool { return covered[i%m] }
	run := func(i int) int {
		n := 0
		for ; n < m && !at(i+n); n++ {
		}
		return n
	}
	dur := func(samples int) string {
		// Hours and minutes only: the lap is authored in hours, so 30 h reads
		// 30h, never 1d06h.
		m := int(float64(samples) * stepS / 60)
		if m%60 == 0 {
			return fmt.Sprintf("%dh", m/60)
		}
		if m < 60 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dh%02dm", m/60, m%60)
	}
	if !at(now) {
		return fmt.Sprintf("  gap now, %s left · %s", dur(run(now)), relays)
	}
	for k := 1; k < m; k++ {
		if !at(now + k) {
			return fmt.Sprintf("  gap in %s, lasts %s · %s", dur(k), dur(run(now+k)), relays)
		}
	}
	return "  no gap this lap · " + relays
}

// coverageNow is the sample index for the time elapsed since the memo's
// epoch. At high warp the memo is older than its lap between recomputes; the
// lap repeats (it is the relays' period), so the index wraps rather than
// clamping to the last sample, which would read "no gap" off a stale tail.
func coverageNow(elapsedS, stepS float64, n int) int {
	i := int(elapsedS / stepS)
	if i < 0 || n < 2 {
		return 0
	}
	return i % (n - 1)
}

// hm is a duration as 30h, or 1h35m when it is not a whole hour (the aimed
// lap is a relay period, not an authored round number).
func hm(sec float64) string {
	m := int(sec / 60)
	if m%60 == 0 {
		return fmt.Sprintf("%dh", m/60)
	}
	return fmt.Sprintf("%dh%02dm", m/60, m%60)
}

// coverageCoveredS is the linked time over the lap: the samples are the lap's
// ring plus its far end (the last sample is the first a lap on), so only the
// ring counts, scaled to the lap, so a fully linked lap reads the lap (#894).
func coverageCoveredS(covered []bool, lapS float64) float64 {
	m := len(covered) - 1
	if m < 1 {
		return 0
	}
	n := 0
	for _, c := range covered[:m] {
		if c {
			n++
		}
	}
	return float64(n) / float64(m) * lapS
}
