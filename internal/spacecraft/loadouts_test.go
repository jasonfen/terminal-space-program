package spacecraft

import "testing"

// TestLoadoutsCatalogShape — every entry in LoadoutOrder must map
// to a non-empty Loadouts entry, and each entry must have non-empty
// ID / Name / Glyph / Color so the HUD render path can rely on them.
func TestLoadoutsCatalogShape(t *testing.T) {
	for _, id := range LoadoutOrder {
		l, ok := Loadouts[id]
		if !ok {
			t.Errorf("LoadoutOrder references %q but Loadouts has no entry", id)
			continue
		}
		if l.ID != id {
			t.Errorf("loadout %q: ID field = %q, mismatched", id, l.ID)
		}
		if l.Name == "" || l.Glyph == "" || l.Color == "" {
			t.Errorf("loadout %q: empty visual field — Name=%q Glyph=%q Color=%q",
				id, l.Name, l.Glyph, l.Color)
		}
	}
}

// TestLookupLoadoutFallback — empty / unknown IDs should fall back
// to the S-IVB-1 default so legacy saves don't break.
func TestLookupLoadoutFallback(t *testing.T) {
	l := LookupLoadout("")
	if l.ID != LoadoutSIVB1ID {
		t.Errorf("empty ID resolved to %q, want %q", l.ID, LoadoutSIVB1ID)
	}
	l = LookupLoadout("not-a-real-loadout")
	if l.ID != LoadoutSIVB1ID {
		t.Errorf("unknown ID resolved to %q, want %q", l.ID, LoadoutSIVB1ID)
	}
}

// TestNewFromLoadoutPopulatesAll — NewFromLoadout must set propulsion
// numbers + visual fields + RCS pool from the catalog entry. Caller
// is still responsible for Primary + State.
func TestNewFromLoadoutPopulatesAll(t *testing.T) {
	c := NewFromLoadout(LoadoutICPSID)
	if c.LoadoutID != LoadoutICPSID {
		t.Errorf("LoadoutID = %q, want %q", c.LoadoutID, LoadoutICPSID)
	}
	if c.Glyph == "" || c.Color == "" {
		t.Error("Glyph / Color not populated from loadout")
	}
	if c.MonopropCapacity <= 0 {
		t.Error("RCS pool not populated")
	}
	if c.Throttle != 1.0 {
		t.Errorf("Throttle = %v, want 1.0 (default full)", c.Throttle)
	}
}

// TestPureRCSTugHasNoMainEngine — the RCS-tug loadout is monoprop-
// only; main Thrust / Isp must be zero so manual-burn paths cleanly
// no-op.
func TestPureRCSTugHasNoMainEngine(t *testing.T) {
	c := NewFromLoadout(LoadoutRCSTugID)
	if c.Thrust != 0 || c.Isp != 0 {
		t.Errorf("RCS-tug has main engine: Thrust=%v Isp=%v", c.Thrust, c.Isp)
	}
	if c.Fuel != 0 {
		t.Errorf("RCS-tug shipped with main fuel: %v kg", c.Fuel)
	}
}
