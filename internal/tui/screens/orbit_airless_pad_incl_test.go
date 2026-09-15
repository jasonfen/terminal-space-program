// Package screens: ADR 0050 decision 9 / issue #454 (readout half). An
// airless pad (Moon, Glyph, ...) never showed the LAUNCH HUD at all, since
// shouldShowLaunchHUD requires an atmosphere and buildDescentChip ran
// instead (both retired under ADR 0051), so a Luna pad had no heading:/
// incl:/Δincl: rows however long the player warped for. ADR 0051 decision
// 1 unifies the atmospheric and airless cases for good: NAVIGATION (and
// GUIDANCE for heading:) render for every primary, no atmosphere gate at
// all, so #454's fix is now structural rather than a shared-helper call.

package screens

import (
	"strings"
	"testing"
)

// TestNavigationBoxShowsHeadingAndInclWhileLandedOnMoon is #454's
// regression, migrated onto the live NAVIGATION/GUIDANCE boxes: a Landed
// craft on an airless body gets the same heading:/incl: readout an
// atmospheric pad does, with the Inclination Floor "(min N°)" tag.
func TestNavigationBoxShowsHeadingAndInclWhileLandedOnMoon(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.Resize(200, 80)
	w, _ := spawnLandedOnMoon(t, 10, 0)

	guidanceJoined := strings.Join(v.buildGuidanceBox(w), "\n")
	if !strings.Contains(guidanceJoined, "heading:") {
		t.Errorf("expected a 'heading:' row on GUIDANCE while Landed on an airless body:\n%s", guidanceJoined)
	}

	navJoined := strings.Join(v.buildNavigationBox(w), "\n")
	if !strings.Contains(navJoined, "incl:") {
		t.Errorf("expected an 'incl:' row on NAVIGATION while Landed on an airless body:\n%s", navJoined)
	}
	if !strings.Contains(navJoined, "(min ") {
		t.Errorf("expected the incl: row to carry the '(min N°)' Inclination Floor tag, same as the atmospheric pad:\n%s", navJoined)
	}
}
