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
func (v *OrbitView) buildCoverageChip(w *sim.World) []string {
	m := w.ActiveMission()
	if m == nil {
		return nil
	}
	o, ok := m.CurrentObjective()
	if !ok || o.Kind != missions.KindLunarCoverage || o.Params.PrimaryID == "" {
		return nil
	}
	p := o.Params
	lap, step := p.LapS, p.SampleS
	if lap <= 0 {
		lap = 108000
	}
	if step <= 0 {
		step = 60
	}
	cov, ok := w.SiteCoverage(p.PrimaryID, p.SiteLatDeg, p.SiteLonDeg, lap, step)
	if !ok || len(cov.Covered) == 0 {
		return nil
	}
	covered := 0
	for _, c := range cov.Covered {
		if c {
			covered++
		}
	}
	now := coverageNow(w.Clock.SimTime.Sub(cov.Epoch).Seconds(), cov.StepS, len(cov.Covered))
	body := p.PrimaryID
	sys := w.System()
	if b := sys.FindBody(p.PrimaryID); b != nil {
		body = b.EnglishName
	}
	relays := fmt.Sprintf("%d relays at the %s", cov.RelayCount, body)
	if cov.RelayCount == 1 {
		relays = "1 relay at the " + body
	}
	second := coverageSecondRow(cov.Covered, now, cov.StepS, relays)
	site := fmt.Sprintf("lat %.0f°", p.SiteLatDeg)
	switch p.SiteLatDeg {
	case -90:
		site = "south pole"
	case 90:
		site = "north pole"
	}
	hours := int(float64(covered) * cov.StepS / 3600)
	return []string{
		v.theme.Primary.Render("COVERAGE") + v.theme.Dim.Render(fmt.Sprintf("  %s %s  %dh of %dh", body, site, hours, int(lap/3600))),
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
