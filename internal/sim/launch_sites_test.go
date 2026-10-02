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

// TestDefaultLaunchSiteKeyMissingFailsLoudly (review LOW 97): a default-site
// key that names no preset used to fall through to a magic index 1; it must
// now fail loudly, and every mapped key (and KSC) must exist.
func TestDefaultLaunchSiteKeyMissingFailsLoudly(t *testing.T) {
	for sys, key := range systemDefaultSiteKey {
		if _, ok := launchSiteIndex(LaunchSites, key); !ok {
			t.Errorf("system %q default key %q is not in LaunchSites", sys, key)
		}
	}
	if _, ok := launchSiteIndex(LaunchSites, "KSC"); !ok {
		t.Fatal("KSC preset missing")
	}
	saved := systemDefaultSiteKey["lumen"]
	systemDefaultSiteKey["lumen"] = "NoSuchPad"
	defer func() {
		systemDefaultSiteKey["lumen"] = saved
		if recover() == nil {
			t.Fatal("DefaultLaunchSiteIndex with an unknown key did not panic (silent fallback)")
		}
	}()
	DefaultLaunchSiteIndex("lumen")
}

// TestLaunchSiteLabelNamesKern (review LOW 98): Lumen's default pad reads as
// the Kern Space Center, every other pad keeps its preset name.
func TestLaunchSiteLabelNamesKern(t *testing.T) {
	eq, _ := LaunchSiteByName("Equator")
	if got := LaunchSiteLabel("Lumen", eq); got != "Kern Space Center" {
		t.Errorf("Lumen default pad label = %q, want Kern Space Center", got)
	}
	if got := LaunchSiteLabel("Sol", eq); got != "Equator" {
		t.Errorf("Sol Equator label = %q, want Equator", got)
	}
	ksc, _ := LaunchSiteByName("KSC")
	if got := LaunchSiteLabel("Lumen", ksc); got != ksc.Name {
		t.Errorf("Lumen KSC label = %q, want %q", got, ksc.Name)
	}
}
