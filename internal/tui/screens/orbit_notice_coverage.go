package screens

import (
	"fmt"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
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
	now := int(w.Clock.SimTime.Sub(cov.Epoch).Seconds() / cov.StepS)
	if now < 0 {
		now = 0
	}
	if now >= len(cov.Covered) {
		now = len(cov.Covered) - 1
	}
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
	run := func(i int) int {
		n := 0
		for ; i+n < len(covered) && !covered[i+n]; n++ {
		}
		return n
	}
	dur := func(samples int) string {
		return strings.TrimSuffix(readout.Duration(time.Duration(float64(samples)*stepS)*time.Second), "00m")
	}
	if !covered[now] {
		return fmt.Sprintf("  gap now, %s left · %s", dur(run(now)), relays)
	}
	for i := now; i < len(covered); i++ {
		if !covered[i] {
			return fmt.Sprintf("  gap in %s, lasts %s · %s", dur(i-now), dur(run(i)), relays)
		}
	}
	return "  no gap this lap · " + relays
}
