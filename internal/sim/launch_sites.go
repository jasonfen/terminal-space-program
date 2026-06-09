package sim

import "strings"

// LaunchSitePreset bundles a named real-world launch site with its
// latitude + longitude (east-positive, relative to the body's prime
// meridian at simTime=0 — our pseudo-Greenwich convention; see
// SpawnSpec.LongitudeOffset). The sites are Earth-oriented; for a
// launchpad on another body, give an explicit lat/lon instead.
//
// v0.17: hoisted out of the spawn form (internal/tui/screens/spawn.go)
// so the form's LATITUDE cycle and the --launch-site CLI flag resolve
// the same set. Key is the short CLI token; Name is the display label.
type LaunchSitePreset struct {
	Key              string
	Name             string
	LatitudeDeg      float64
	LongitudeEastDeg float64
}

// LaunchSites is the canonical named-site list. Index 1 (KSC) is the
// spawn form's launchpad default, so opening the form with launchpad
// selected lands a Saturn V at the historical Apollo pad; Equator at
// index 0 is the textbook best-case baseline. KSC reuses the package
// DefaultLaunchpad* consts so the default has a single source.
var LaunchSites = []LaunchSitePreset{
	{Key: "Equator", Name: "Equator", LatitudeDeg: 0.0, LongitudeEastDeg: 0.0},
	{Key: "KSC", Name: "Cape Canaveral (KSC LC-39A)", LatitudeDeg: DefaultLaunchpadLatitude, LongitudeEastDeg: DefaultLaunchpadLongitudeEast},
	{Key: "Baikonur", Name: "Baikonur Cosmodrome", LatitudeDeg: 45.965, LongitudeEastDeg: 63.342},
	{Key: "Plesetsk", Name: "Plesetsk Cosmodrome", LatitudeDeg: 62.926, LongitudeEastDeg: 40.577},
	{Key: "North-Pole", Name: "North Pole", LatitudeDeg: 90.0, LongitudeEastDeg: 0.0},
}

// LaunchSiteByName resolves a launch site by its short Key or display
// Name, case-insensitively. Returns ok=false when no site matches.
func LaunchSiteByName(name string) (LaunchSitePreset, bool) {
	q := strings.TrimSpace(strings.ToLower(name))
	for _, s := range LaunchSites {
		if strings.ToLower(s.Key) == q || strings.ToLower(s.Name) == q {
			return s, true
		}
	}
	return LaunchSitePreset{}, false
}
