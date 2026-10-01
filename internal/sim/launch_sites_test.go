package sim

import "testing"

// TestDefaultLaunchSitePerSystem (#461): Lumen defaults to the equator,
// every other system (including unknown and empty) to KSC.
func TestDefaultLaunchSitePerSystem(t *testing.T) {
	cases := map[string]string{
		"Lumen": "Equator", "lumen": "Equator",
		"Sol": "KSC", "": "KSC", "Kepler-452": "KSC", "TRAPPIST-1": "KSC", "Alpha Centauri": "KSC", "nonesuch": "KSC",
	}
	for sys, want := range cases {
		if got := DefaultLaunchSite(sys).Key; got != want {
			t.Errorf("DefaultLaunchSite(%q) = %s, want %s", sys, got, want)
		}
	}
}
