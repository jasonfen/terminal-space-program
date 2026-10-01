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

// systemDefaultSiteKey maps a star system's name (lower-case) to the key of
// its default launch site. Systems not listed here use KSC. Lumen mirrors
// Kerbol, whose space centre sits on the equator, so a default Lumen launch
// starts equatorial (#461): a plain ascent from Kern then arrives flat at
// Cursor instead of in a 32 degree tilted orbit. This is a Go-side table on
// purpose: a catalog field would move body_catalog_hash.
var systemDefaultSiteKey = map[string]string{
	"lumen": "Equator",
}

// DefaultLaunchSiteIndex returns the index into LaunchSites of the default
// launch site for the named system (case-insensitive; "" is the default
// system). Unlisted systems get KSC (index 1).
func DefaultLaunchSiteIndex(systemName string) int {
	key, ok := systemDefaultSiteKey[strings.ToLower(strings.TrimSpace(systemName))]
	if !ok {
		key = "KSC"
	}
	for i, s := range LaunchSites {
		if s.Key == key {
			return i
		}
	}
	return 1
}

// DefaultLaunchSite is LaunchSites[DefaultLaunchSiteIndex(systemName)].
func DefaultLaunchSite(systemName string) LaunchSitePreset {
	return LaunchSites[DefaultLaunchSiteIndex(systemName)]
}
