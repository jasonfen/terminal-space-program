package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// landingFrame renders the landing view at 140x40 for a site aimed from a
// level lunar orbit (moonSiteDescentWorld), stripped of colour.
func landingFrame(t *testing.T, mutate func(*sim.World)) (*OrbitView, *sim.World, string) {
	t.Helper()
	w := moonSiteDescentWorld(t, false)
	if mutate != nil {
		mutate(w)
	}
	if entered, refusal := w.ToggleProximityView(); !entered {
		t.Fatalf("o refused on a site: %q", refusal)
	}
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	return v, w, ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
}

// ADR 0067 decision 8: the landing view draws the ground line and the flag,
// the ⌖ sits on the ⚑ (no nav error until S4) so only the ⚑ shows, the legend
// names the view, and the LANDING chip is 41 cells wide (39 inside) with the
// rows in the ADR's order. Re-measured at 140x40, not inherited (#482).
func TestLandingViewDrawsGroundFlagAndChip(t *testing.T) {
	for _, state := range []string{"idle", "firing"} {
		t.Run(state, func(t *testing.T) { landingChipCase(t, state == "firing") })
	}
}

func landingChipCase(t *testing.T, firing bool) {
	_, _, out := landingFrame(t, func(w *sim.World) {
		if c := w.ActiveCraft(); firing {
			c.Throttle = 1
			c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime}
		}
	})
	if !strings.Contains(out, "view: Landing") {
		t.Errorf("legend does not name the landing view\n%s", out)
	}
	if !strings.Contains(out, string(flagGlyph)) {
		t.Errorf("no ⚑ drawn\n%s", out)
	}
	lines := strings.Split(out, "\n")
	top, bottom := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "LANDING") && top < 0 {
			top = i - 1
		}
		// the chip's own bottom corner: the OVERHEAD inset (top-right) has one too
		if top >= 0 && bottom < 0 && i > top+1 && strings.HasPrefix(l, "│╰") {
			bottom = i
		}
	}
	if top < 0 || bottom < 0 {
		t.Fatalf("no LANDING chip\n%s", out)
	}
	// the chip's box starts at the same column on every row; measure the top border
	l := lines[top]
	a := strings.Index(l, "╭")
	b := strings.Index(l, "╮")
	if a < 0 || b < 0 {
		t.Fatalf("no chip border on %q", l)
	}
	if w := lipgloss.Width(l[a : b+len("╮")]); w != proximityChipInnerWidth+2 {
		t.Errorf("LANDING chip is %d cells wide, want %d", w, proximityChipInnerWidth+2)
	}
	body := strings.Join(lines[top:bottom+1], "\n")
	last := -1
	for _, label := range []string{"LANDING", "range:", "alt:", "alt rate:", "stop:", "fwd vel:", "lat vel:", "lpd:", "miss:", "cross:", "aim:", "hold:", "throttle:", "twr:", "rcs:", "monoprop:"} {
		at := strings.Index(body, label)
		if at < 0 || at < last {
			t.Errorf("chip row %q missing or out of order (at %d after %d)\n%s", label, at, last, body)
		}
		last = at
	}
}

// The computer's site is the real one with no error, so the ⌖ sits under the ⚑;
// with an error it moves by it (a modern vessel's 3 m is under the cell, an
// Apollo-era 2 km is not), and the ⚑ is revealed at 10 km exactly and not at
// 10 km + 1 m (guard 8, the equal case).
func TestLandingViewComputerSiteAndReveal(t *testing.T) {
	_, w, _ := landingFrame(t, nil)
	st, _ := w.ProximityState()
	if got := computerSiteWorld(st); got != st.TargetWorld {
		t.Errorf("computer's site = %v, want the real site %v while the nav error is zero", got, st.TargetWorld)
	}
	w.ActiveCraft().Landing = &spacecraft.LandingAutopilot{NavSeeded: true, NavAlongM: 2000, AimAcrossM: 300}
	st, _ = w.ProximityState()
	if d := computerSiteWorld(st).Sub(st.TargetWorld).Norm(); d < 2000 || d > 2100 {
		t.Errorf("computer's site is %.0f m from the flag, want hypot(2000, 300) = 2022", d)
	}
	for _, tc := range []struct {
		differs bool
		rangeM  float64
		want    bool
	}{{true, sim.SiteRevealRangeM, true}, {true, sim.SiteRevealRangeM + 1, false}, {false, 400e3, true}} {
		if got := flagRevealed(tc.differs, tc.rangeM); got != tc.want {
			t.Errorf("flagRevealed(differs=%v, %v m) = %v, want %v", tc.differs, tc.rangeM, got, tc.want)
		}
	}
}

// The entry fit is the landing view's own: 2.25 x the range, 50 m floor, and
// the floor's equal case.
func TestProximityFitRadiusSurface(t *testing.T) {
	cases := []struct {
		rangeM, want float64
	}{
		{10_000, 22_500},
		{sim.LandingViewFitFloorM / sim.LandingViewFitFrac, sim.LandingViewFitFloorM}, // the equal case: exactly on the floor
		{10, sim.LandingViewFitFloorM},
	}
	for _, c := range cases {
		got := proximityFitRadius(sim.ProximityState{Surface: true, RangeM: c.rangeM})
		if d := got - c.want; d > 1e-9 || d < -1e-9 {
			t.Errorf("range %g: fit %g, want %g", c.rangeM, got, c.want)
		}
	}
}

// Hint strip: an arrow-free legend under the Site+ hold (the arrows trim under
// q), the move-aim legend under Site+ auto, today's text otherwise.
func TestLandingViewHintStrip(t *testing.T) {
	_, _, plain := landingFrame(t, nil)
	if !strings.Contains(plain, proximityHintStripText) {
		t.Errorf("no hold: the strip should read today's text\n%s", plain)
	}
	if strings.Contains(plain, "move aim") {
		t.Error("move-aim legend shown without Site+")
	}
	_, _, site := landingFrame(t, func(w *sim.World) {
		w.ActiveCraft().AttitudeMode = spacecraft.BurnSite
	})
	if !strings.Contains(site, landingSiteHintStripText) {
		t.Errorf("Site+ hold: the strip should read the arrow-free legend\n%s", site)
	}
	if strings.Contains(site, "move aim") || strings.Contains(site, "[←→↑↓]") {
		t.Errorf("Site+ hold without N: no aim legend, the arrows trim\n%s", site)
	}
	_, _, auto := landingFrame(t, func(w *sim.World) {
		c := w.ActiveCraft()
		c.AttitudeMode = spacecraft.BurnSite
		c.Landing = &spacecraft.LandingAutopilot{Engaged: true}
	})
	if !strings.Contains(auto, landingAimHintStripText) {
		t.Errorf("Site+ auto: the strip should read the move-aim legend\n%s", auto)
	}
}

// lat vel names its side the way cross: does and has none when it rounds to nothing.
func TestLandingLatVel(t *testing.T) {
	for _, c := range []struct {
		mps  float64
		want string
	}{{0.4, "0.40 m/s R"}, {-0.4, "0.40 m/s L"}, {0, "0.00 m/s"}, {0.004, "0.00 m/s"}, {-0.004, "0.00 m/s"}} {
		if got := landingLatVel(c.mps); got != c.want {
			t.Errorf("landingLatVel(%g) = %q, want %q", c.mps, got, c.want)
		}
	}
}

// A long "short by 307.6 km (2436 m/s)" reading (28 cells) is clipped to the
// chip's value column rather than widening the 41-cell chip.
func TestLandingChipStopRowClipsLongReading(t *testing.T) {
	w := fastLowMoonDescentCraft(t, 20000, 2000, 1500)
	w.Sites = append(w.Sites, sim.Site{ID: 1, Body: w.ActiveCraft().Primary.EnglishName, System: w.System().Name, LatDeg: 1, LonDeg: 2, Name: "Test site"})
	w.NextSiteID = 1
	w.SetTargetSite(1)
	if entered, refusal := w.ToggleProximityView(); !entered {
		t.Fatalf("o refused: %q", refusal)
	}
	v := newProximityTestView(t, DesignWidth, DesignHeight)
	out := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
	for _, l := range strings.Split(out, "\n") {
		if strings.Contains(l, "stop:") {
			if !strings.Contains(l, "short by") || !strings.Contains(l, "…│") {
				t.Errorf("stop row = %q, want a clipped short by reading closing the chip border", l)
			}
			return
		}
	}
	t.Fatalf("no stop: row\n%s", out)
}

// P5 (playtest 3): the LANDING chip carries NAVIGATION's stop: reading, directly
// under alt rate:, the same cell (it calls the same builder). The 57-6 LM
// descent gives a tight stop, a short one (NO LAND) and a dry tank; the level
// orbit dashes.
func TestLandingChipStopRowMatchesNavigation(t *testing.T) {
	for _, tc := range []struct {
		name string
		fuel float64 // descent tank kg; <0 keeps the level orbit instead
		want string  // substring of the cell
	}{{"level orbit dashes", -1, "—"}, {"tight", 460, "(Descent)"}, {"short", 250, "(Descent)"}, {"dry", 0, "Descent dry"}} {
		t.Run(tc.name, func(t *testing.T) {
			w := moonSiteDescentWorld(t, false)
			if tc.fuel >= 0 {
				w = lmDescent57(t, tc.fuel)
				w.Sites = append(w.Sites, sim.Site{ID: 1, Body: w.ActiveCraft().Primary.EnglishName, System: w.System().Name, LatDeg: 1, LonDeg: 2, Name: "Test site"})
				w.NextSiteID = 1
				w.SetTargetSite(1)
			}
			if entered, refusal := w.ToggleProximityView(); !entered {
				t.Fatalf("o refused: %q", refusal)
			}
			v := newProximityTestView(t, DesignWidth, DesignHeight)
			out := ansi.Strip(v.Render(w, 0, DesignWidth, DesignHeight))
			_, stop := v.navigationImpactStopCells(w, w.ActiveCraft())
			stop = ansi.Strip(stop)
			if !strings.Contains(stop, tc.want) {
				t.Fatalf("stop cell %q lacks %q", stop, tc.want)
			}
			lines := strings.Split(out, "\n")
			for i, l := range lines {
				if strings.Contains(l, "alt rate:") {
					if !strings.Contains(lines[i+1], "stop:") || !strings.Contains(lines[i+1], stop) {
						t.Fatalf("row under alt rate: = %q, want stop: %q", lines[i+1], stop)
					}
					return
				}
			}
			t.Fatalf("no alt rate: row\n%s", out)
		})
	}
}

// P13: inside 100 m the LANDING chip's alt rate: row ends "  (need < 10)" in the
// Alert colour exactly when the crash rule (>= 10 m/s) would fire, and the
// longest plausible reading still fits the 39-cell chip.
func TestLandingChipAltRateNeedSuffix(t *testing.T) {
	v := NewOrbitView(alertMarkTheme())
	for _, tc := range []struct {
		alt, vDown float64
		want       bool
	}{{50, 9.99, false}, {50, 10, true}, {100, 10, true}, {100.01, 50, false}} {
		w := fastLowMoonDescentCraft(t, tc.alt, tc.vDown, 0)
		got := v.landingSpeedNeed(w.ActiveCraft())
		if want := alertMark + "  (need < 10)" + alertMark; tc.want != (got == want) || (!tc.want && got != "") {
			t.Errorf("alt %v v %v: suffix %q, want present=%v", tc.alt, tc.vDown, got, tc.want)
		}
	}
	row := chipRow("alt rate:", "-999.9 m/s"+strings.ReplaceAll(v.landingSpeedNeed(fastLowMoonDescentCraft(t, 50, 999.9, 0).ActiveCraft()), alertMark, ""))
	if n := lipgloss.Width(row); n > proximityChipInnerWidth {
		t.Errorf("alt rate row %q is %d cells, chip inner width %d", row, n, proximityChipInnerWidth)
	}
}
