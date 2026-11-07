package tui

import (
	"math"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// One-line reason for a new file: the key wiring of a new binding (q in SITE,
// Z/X as ROD, z/x as the raw throttle), which no existing app test owns.

// siteHoldApp is an App whose active vessel flies over the Moon with a flag
// aimed, in the SITE frame.
func siteHoldApp(t *testing.T) (*App, *spacecraft.Spacecraft) {
	t.Helper()
	a, err := New(nil)
	if err != nil {
		t.Fatal(err)
	}
	w := a.world
	c := w.ActiveCraft()
	for _, b := range w.System().Bodies {
		if b.ID == "moon" {
			c.Primary = b
		}
	}
	mu, r := c.Primary.GravitationalParameter(), c.Primary.RadiusMeters()+100e3
	c.State.R = orbital.Vec3{X: r}
	c.State.V = orbital.Vec3{Y: math.Sqrt(mu / r)}
	w.Sites = append(w.Sites, sim.Site{ID: 1, Body: c.Primary.EnglishName, System: w.System().Name, LatDeg: 0, LonDeg: 10, Name: "Flag-1"})
	w.NextSiteID = 1
	w.SetTargetSite(1)
	w.NavMode = sim.NavPort
	return a, c
}

// ADR 0067 decision 5: ; reaches SITE and flashes it; q there with a vessel
// that cannot out-thrust the Moon's gravity refuses with the fact.
func TestSiteFrameFlashAndUnderWeightRefusal(t *testing.T) {
	a, c := siteHoldApp(t)
	pressKey(a, ';')
	if a.world.NavMode != sim.NavSite || a.statusMsg != "nav: SITE" {
		t.Fatalf("; -> nav %v, flash %q, want SITE and %q", a.world.NavMode, a.statusMsg, "nav: SITE")
	}
	c.Thrust = 1 // a thrust far under weight
	pressKey(a, 'q')
	if a.statusMsg != "site hold: thrust under weight" {
		t.Errorf("q flash = %q, want %q", a.statusMsg, "site hold: thrust under weight")
	}
	if c.Landing != nil && c.Landing.Engaged {
		t.Error("q engaged an autopilot that cannot brake")
	}
}

// ADR 0067 decisions 5 and 10, playtest Q4a: with the autopilot engaged in
// final, e / q step a rate of descent, Z / X / z / x take the throttle, and a
// hold key takes the nose.
func TestSiteHoldKeysUnderTheAutopilot(t *testing.T) {
	a, c := siteHoldApp(t)
	c.Landing = &spacecraft.LandingAutopilot{Engaged: true, Phase: spacecraft.PhaseFinal} // Z/X latch a rate only in approach and final; z/x hand over only past the coast
	c.Throttle = 0.5
	a.world.NavMode = sim.NavSite // e / q are the sink keys in SITE
	c.AttitudeMode = spacecraft.BurnSite
	pressKey(a, 'e') // e = sink slower; from the rate the vessel is falling (zero here)
	if !c.Landing.ROD || c.Landing.RODRateMps != 0 || c.Throttle != 0.5 {
		t.Errorf("e: ROD=%v rate=%v throttle=%v, want ROD at 0 with the throttle untouched", c.Landing.ROD, c.Landing.RODRateMps, c.Throttle)
	}
	pressKey(a, 'q') // q = sink faster, one way: it does not hand the sink back
	if !c.Landing.ROD || c.Landing.RODRateMps != -sim.RODStepMps {
		t.Errorf("q: ROD=%v rate=%v, want ROD at %v", c.Landing.ROD, c.Landing.RODRateMps, -sim.RODStepMps)
	}
	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}, Alt: true})
	if got, want := c.Landing.RODRateMps, -sim.RODStepMps+sim.RODFineStepMps; got < want-1e-9 || got > want+1e-9 {
		t.Errorf("alt+e: rate %v, want %v (a 0.3 m/s click)", got, want)
	}
	pressKey(a, 'Z') // Z takes the throttle (+10%), steering kept, ROD over
	if !c.Landing.ThrottleHeld || c.Landing.ROD || !c.Landing.Engaged || c.Throttle < 0.59 || c.Throttle > 0.61 {
		t.Errorf("Z: held=%v ROD=%v engaged=%v throttle=%v, want the throttle at 0.6 and the autopilot still steering", c.Landing.ThrottleHeld, c.Landing.ROD, c.Landing.Engaged, c.Throttle)
	}
	pressKey(a, 'z')
	if c.Throttle != 1 {
		t.Errorf("z: throttle %v, want full", c.Throttle)
	}
	pressKey(a, 's')
	if c.Landing.Engaged {
		t.Error("a hold key left the autopilot engaged")
	}
	// off: Z / X are the 10% throttle keys again
	c.Throttle = 0.5
	pressKey(a, 'Z')
	if c.Throttle < 0.59 || c.Throttle > 0.61 {
		t.Errorf("Z with the autopilot off: throttle %v, want 0.6", c.Throttle)
	}
}

// The footer lines and the Event Flash are facts in house style (ADR 0067
// decision 10): the sign is + long / U+2212 short, the flash names the true
// distance, and an out-of-reach landing says so.
func TestSiteHoldTextIsFacts(t *testing.T) {
	short := siteHoldRefusal(&sim.SiteHoldReachError{AlongM: -43000})
	if want := "stop −" + readout.Distance(43000) + " at reserve throttle"; short != want || !strings.Contains(short, "−") {
		t.Errorf("short refusal = %q, want %q", short, want)
	}
	if long := siteHoldRefusal(&sim.SiteHoldReachError{AlongM: 43000}); !strings.HasPrefix(long, "stop +") {
		t.Errorf("long refusal = %q, want a + sign", long)
	}
	if across := siteHoldRefusal(&sim.SiteHoldReachError{AlongM: 100, CrossM: -12000}); !strings.Contains(across, " L at reserve throttle") {
		t.Errorf("left-across refusal = %q, want an L", across)
	}
	if got, want := landingFlash(sim.LandingEvent{CraftName: "Lander-1", SiteName: "Flag-1", DistanceM: 38}), "Lander-1 landed "+readout.Distance(38)+" from Flag-1"; got != want {
		t.Errorf("landed flash = %q, want %q", got, want)
	}
	if got := landingFlash(sim.LandingEvent{CraftName: "B", SiteName: "F", DistanceM: 1900, OutOfReach: true}); !strings.HasSuffix(got, "(out of reach)") {
		t.Errorf("out-of-reach flash = %q", got)
	}
	if got := landingFlash(sim.LandingEvent{CraftName: "B", SiteName: "F", DistanceM: 20, Crashed: true, OutOfReach: true}); !strings.Contains(got, "crashed") || strings.Contains(got, "out of reach") {
		t.Errorf("crash flash = %q, want crashed and no out-of-reach", got)
	}
}

// ADR 0067 playtest P1: b with Site+ engaged, in any phase, switches the
// autopilot off like a hold key: surface retrograde, then b means what it does
// on the engine, and the footer states the fact.
func TestBTakesTheEngineFromSitePlus(t *testing.T) {
	cases := []struct {
		name    string
		phase   spacecraft.LandingPhase
		burning bool
		want    string
		lit     bool
	}{
		{"coast, engine dark", spacecraft.PhaseCoast, false, "Site+ off: engine lit by hand", true},
		{"brake, engine lit", spacecraft.PhaseBrake, true, "Site+ off: engine cut by hand", false},
		{"final, engine lit", spacecraft.PhaseFinal, true, "Site+ off: engine cut by hand", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, c := siteHoldApp(t)
			c.AttitudeMode = spacecraft.BurnSite
			c.Landing = &spacecraft.LandingAutopilot{Engaged: true, Phase: tc.phase}
			if tc.burning {
				a.world.StartManualBurn()
				if c.ManualBurn == nil {
					t.Fatal("setup: engine did not light")
				}
			}
			pressKey(a, 'b')
			if c.Landing.Engaged {
				t.Error("b left Site+ engaged")
			}
			if c.AttitudeMode != spacecraft.BurnSurfaceRetrograde {
				t.Errorf("hold = %v, want surface retrograde", c.AttitudeMode)
			}
			if (c.ManualBurn != nil) != tc.lit {
				t.Errorf("engine lit = %v, want %v", c.ManualBurn != nil, tc.lit)
			}
			if a.statusMsg != tc.want {
				t.Errorf("footer = %q, want %q", a.statusMsg, tc.want)
			}
		})
	}
	// Site+ off: b is the plain toggle with no Site+ footer.
	a, c := siteHoldApp(t)
	pressKey(a, 'b')
	if c.ManualBurn == nil || strings.Contains(a.statusMsg, "Site+") {
		t.Errorf("plain b: lit=%v footer %q", c.ManualBurn != nil, a.statusMsg)
	}
}

// Playtest P3: N on a site engages Site+ on SITE in one press.
func TestNEngagesSitePlus(t *testing.T) {
	a, c := siteHoldApp(t)
	a.world.NavMode = sim.NavSurface
	pressKey(a, 'N')
	if c.Landing == nil || !c.Landing.Engaged || c.AttitudeMode != spacecraft.BurnSite || a.world.NavMode != sim.NavSite {
		t.Errorf("after N (%q): landing=%+v hold=%v nav=%v, want Site+ engaged on SITE", a.statusMsg, c.Landing, c.AttitudeMode, a.world.NavMode)
	}
}
