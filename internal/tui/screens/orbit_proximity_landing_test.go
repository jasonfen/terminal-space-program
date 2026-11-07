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
// the ◇ sits on the ⚑ (no nav error until S4) so only the ⚑ shows, the legend
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
		if top >= 0 && bottom < 0 && strings.Contains(l, "╰") && i > top+1 && strings.Contains(l, "╯") {
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
	for _, label := range []string{"LANDING", "range:", "alt:", "alt rate:", "fwd vel:", "lat vel:", "lpd:", "miss:", "cross:", "aim:", "hold:", "throttle:", "rcs:", "monoprop:"} {
		at := strings.Index(body, label)
		if at < 0 || at < last {
			t.Errorf("chip row %q missing or out of order (at %d after %d)\n%s", label, at, last, body)
		}
		last = at
	}
}

// The computer's site is the real one until the nav error exists (S4), so the
// ◇ sits under the ⚑.
func TestLandingViewComputerSiteIsRealSite(t *testing.T) {
	_, w, _ := landingFrame(t, nil)
	st, _ := w.ProximityState()
	if got := computerSiteWorld(st); got != st.TargetWorld {
		t.Errorf("computer's site = %v, want the real site %v while the nav error is zero", got, st.TargetWorld)
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

// Hint strip: the move-aim legend only under the Site+ hold, today's text otherwise.
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
		t.Errorf("Site+ hold: the strip should read the move-aim legend\n%s", site)
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
