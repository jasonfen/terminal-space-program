package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// rawFlags holds the parsed command-line values that shape the starting
// scenario, plus the set of flags the user explicitly passed (so a 0 value
// can be told apart from "unset" for the float flags). Kept separate from
// flag registration so buildScenario is unit-testable without os.Args.
type rawFlags struct {
	system      string
	body        string // --orbit / --parent / --body
	altitude    string
	loadout     string
	launchSite  string
	inclination float64
	lat         float64
	lon         float64
	retrograde  bool
	launchpad   bool

	set map[string]bool // flag names the user actually provided
}

// buildScenario turns parsed flags into a *sim.StartScenario, or nil when no
// scenario flag was given (→ the default start). Surface placement
// (--launchpad/--launch-site/--lat/--lon) and orbital placement
// (--altitude/--inclination/--retrograde) are mutually exclusive. Returns a
// descriptive error on a conflict or an unknown launch site.
func buildScenario(r rawFlags) (*sim.StartScenario, error) {
	latSet, lonSet := r.set["lat"], r.set["lon"]
	orbitalSet := r.set["altitude"] || r.set["inclination"] || r.set["retrograde"]
	surfaceSet := r.launchpad || r.launchSite != "" || latSet || lonSet
	anySet := orbitalSet || surfaceSet ||
		r.system != "" || r.body != "" || r.loadout != ""
	if !anySet {
		return nil, nil // no scenario flags → standard default start
	}
	if orbitalSet && surfaceSet {
		return nil, fmt.Errorf("orbital flags (--altitude/--inclination/--retrograde) can't be combined with surface flags (--launchpad/--launch-site/--lat/--lon)")
	}

	s := &sim.StartScenario{
		SystemName: r.system,
		BodyID:     r.body,
		Loadout:    r.loadout,
	}

	if surfaceSet {
		s.Surface = true
		switch {
		case r.launchSite != "":
			if latSet || lonSet {
				return nil, fmt.Errorf("--launch-site can't be combined with --lat/--lon")
			}
			site, ok := sim.LaunchSiteByName(r.launchSite)
			if !ok {
				return nil, fmt.Errorf("unknown launch site %q (have: %s)", r.launchSite, strings.Join(launchSiteKeys(), ", "))
			}
			s.LatDeg, s.LonDeg = site.LatitudeDeg, site.LongitudeEastDeg
		case latSet || lonSet:
			// Numeric site; a missing component defaults to 0 (equator /
			// prime meridian).
			s.LatDeg, s.LonDeg = r.lat, r.lon
		default:
			// --launchpad alone → the form's KSC default, so a bare
			// --launchpad lands somewhere sensible rather than the equator.
			s.LatDeg = sim.DefaultLaunchpadLatitude
			s.LonDeg = sim.DefaultLaunchpadLongitudeEast
		}
		return s, nil
	}

	// Orbital placement (also the path when only --system/--body/--loadout
	// are given — a plain orbital spawn at the default altitude).
	if r.altitude != "" {
		m, err := parseDistanceM(r.altitude)
		if err != nil {
			return nil, fmt.Errorf("--altitude: %w", err)
		}
		s.AltitudeM = m
	}
	s.InclDeg = r.inclination
	s.Retrograde = r.retrograde
	return s, nil
}

// parseDistanceM parses an altitude with an optional unit suffix: "400km",
// "400000m", or a bare number treated as kilometres. Returns metres.
func parseDistanceM(s string) (float64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := 1000.0 // bare number → kilometres
	switch {
	case strings.HasSuffix(s, "km"):
		s, mult = strings.TrimSuffix(s, "km"), 1000
	case strings.HasSuffix(s, "m"):
		s, mult = strings.TrimSuffix(s, "m"), 1
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid distance %q", s)
	}
	if val < 0 {
		return 0, fmt.Errorf("altitude must be non-negative")
	}
	return val * mult, nil
}

// launchSiteKeys lists the short CLI tokens for the named launch sites.
func launchSiteKeys() []string {
	keys := make([]string, len(sim.LaunchSites))
	for i, s := range sim.LaunchSites {
		keys[i] = s.Key
	}
	return keys
}
