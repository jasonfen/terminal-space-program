// Package spacecraft — v0.11.4+ CanSoftLand catalog-flag tests
// (ADR 0004). Pins which loadouts qualify as soft-land candidates
// so a future catalog change doesn't silently flip an unrelated
// vessel's lifecycle behaviour.

package spacecraft

import "testing"

// TestLanderAndFalcon9CanSoftLand — the two v0.11.4 launchable
// consumers of the surface-arrival predicate flip CanSoftLand=true
// at the Loadout level, and NewFromLoadout propagates the flag
// onto a freshly spawned Spacecraft.
func TestLanderAndFalcon9CanSoftLand(t *testing.T) {
	for _, id := range []string{LoadoutLanderID, LoadoutFalcon9ID} {
		l := LookupLoadout(id)
		if !l.CanSoftLand {
			t.Errorf("Loadouts[%q].CanSoftLand = false, want true", id)
		}
		c := NewFromLoadout(id)
		if !c.CanSoftLand {
			t.Errorf("NewFromLoadout(%q).CanSoftLand = false, want true (propagation from loadout)", id)
		}
	}
}

// TestSaturnVAndS_IVB1DoNotSoftLand — non-lander loadouts must
// stay CanSoftLand=false so the surface-arrival predicate routes
// any contact through the Crashed branch. Saturn V and the
// default S-IVB-1 stand in for "everything else."
func TestSaturnVAndS_IVB1DoNotSoftLand(t *testing.T) {
	for _, id := range []string{LoadoutSaturnVID, LoadoutSIVB1ID} {
		l := LookupLoadout(id)
		if l.CanSoftLand {
			t.Errorf("Loadouts[%q].CanSoftLand = true, want false", id)
		}
		c := NewFromLoadout(id)
		if c.CanSoftLand {
			t.Errorf("NewFromLoadout(%q).CanSoftLand = true, want false", id)
		}
	}
}
