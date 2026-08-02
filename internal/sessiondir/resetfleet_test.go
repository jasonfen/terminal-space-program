package sessiondir

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// enrollGuest mints an invite and redeems it, returning the guest's
// roster entry.
func enrollGuest(t *testing.T, s *Store, fp, handle string) Player {
	t.Helper()
	inv, err := s.MintInvite(handle)
	if err != nil {
		t.Fatalf("MintInvite(%s): %v", handle, err)
	}
	p, err := s.Enroll(inv.Code, fp, handle)
	if err != nil {
		t.Fatalf("Enroll(%s): %v", handle, err)
	}
	return p
}

// multiCraftWorld builds a world whose slate holds n craft, with the
// first craft's fuel partially drained — the "played for a while"
// state a reset must wipe.
func multiCraftWorld(t *testing.T, n int) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	earth := w.Systems[0].FindBody("Earth")
	if earth == nil {
		t.Fatal("no Earth in Sol")
	}
	for len(w.Crafts) < n {
		c := spacecraft.NewInLEOAtPhase(*earth, float64(len(w.Crafts)*10))
		c.SystemIdx = 0
		w.Crafts = append(w.Crafts, c)
	}
	w.EnsureCraftIDs()
	// Drain some fuel + monoprop so "reset restores full tanks" is a
	// real assertion, not a tautology.
	w.Crafts[0].Stages[0].FuelMass /= 2
	w.Crafts[0].Stages[0].MonopropMass = 0
	w.Crafts[0].SyncFields()
	return w
}

// resetStore builds a store with a host and two guests: alice has a
// 4-craft payload with drained tanks, bob has never connected (no
// payload). Returns the store and alice's pre-reset payload bytes.
func resetStore(t *testing.T) (*Store, []byte) {
	t.Helper()
	s := openStore(t)
	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	enrollGuest(t, s, "SHA256:alice", "alice")
	enrollGuest(t, s, "SHA256:bob", "bob")
	if err := s.SavePlayer("SHA256:alice", multiCraftWorld(t, 4)); err != nil {
		t.Fatalf("SavePlayer(alice): %v", err)
	}
	pre, err := os.ReadFile(s.payloadPath("SHA256:alice"))
	if err != nil {
		t.Fatalf("read alice payload: %v", err)
	}
	return s, pre
}

// (a) N players are spaced evenly by phase around the shared ring, on
// a 500x500 circular orbit, with every pairwise separation >= 50 km.
// (g) every reset payload loads back through the real save path,
// including catalog-rebuilt state (GroundStations).
func TestResetFleetSpacingAndRoundTrip(t *testing.T) {
	s, _ := resetStore(t)
	epoch := time.Date(2030, 3, 14, 12, 0, 0, 0, time.UTC)
	entries, err := s.ResetFleet(epoch)
	if err != nil {
		t.Fatalf("ResetFleet: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("entries = %d, want 3 (host + 2 guests)", len(entries))
	}

	// Host first at phase 0, then guests in roster order, 120° apart.
	wantPhases := []float64{0, 120, 240}
	for i, e := range entries {
		if math.Abs(e.PhaseDeg-wantPhases[i]) > 1e-9 {
			t.Errorf("entry %d (%s): phase = %v, want %v", i, e.Handle, e.PhaseDeg, wantPhases[i])
		}
	}
	if !entries[0].Host || entries[0].Handle != "jason" {
		t.Errorf("entry 0 should be the in-process host: %+v", entries[0])
	}

	// Guests' payloads: real load path, 500x500 circular, >= 50 km apart.
	type placed struct {
		handle string
		w      *sim.World
	}
	var loaded []placed
	for _, e := range entries[1:] {
		w, err := s.LoadPlayer(e.Fingerprint)
		if err != nil {
			t.Fatalf("LoadPlayer(%s) after reset: %v", e.Handle, err)
		}
		if len(w.GroundStations) == 0 {
			t.Errorf("%s: GroundStations not rebuilt on load", e.Handle)
		}
		loaded = append(loaded, placed{e.Handle, w})
	}
	earth := loaded[0].w.Systems[0].FindBody("Earth")
	wantR := earth.RadiusMeters() + 500e3
	wantV := math.Sqrt(earth.GravitationalParameter() / wantR)
	for _, p := range loaded {
		c := p.w.Crafts[0]
		if r := c.State.R.Norm(); math.Abs(r-wantR) > 1 {
			t.Errorf("%s: |R| = %f, want %f (500 km circular)", p.handle, r, wantR)
		}
		if v := c.State.V.Norm(); math.Abs(v-wantV) > 1e-3 {
			t.Errorf("%s: |V| = %f, want %f", p.handle, v, wantV)
		}
		if dot := math.Abs(c.State.R.Unit().Dot(c.State.V.Unit())); dot > 1e-9 {
			t.Errorf("%s: R·V = %g, want ~0 (circular)", p.handle, dot)
		}
	}
	// Separation: guests vs each other AND vs the host's slot-0 seed
	// (phase 0 = the fresh-enroll spawn spot). All craft states are
	// Earth-relative at the same epoch, so chords compare directly.
	hostSeed := spacecraft.NewInLEO(*earth)
	type pos struct {
		handle string
		r      orbital.Vec3
	}
	positions := []pos{{"host", hostSeed.State.R}}
	for _, p := range loaded {
		positions = append(positions, pos{p.handle, p.w.Crafts[0].State.R})
	}
	for i := 0; i < len(positions); i++ {
		for j := i + 1; j < len(positions); j++ {
			d := positions[i].r.Sub(positions[j].r).Norm()
			if d < FleetResetMinSeparationM {
				t.Errorf("%s ↔ %s separation %.0f m < %.0f m floor",
					positions[i].handle, positions[j].handle, d, FleetResetMinSeparationM)
			}
		}
	}
}

// (b) slate replacement: a 4-craft player ends with exactly one
// default S-IVB-1 with full tanks.
func TestResetFleetReplacesSlate(t *testing.T) {
	s, _ := resetStore(t)
	entries, err := s.ResetFleet(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResetFleet: %v", err)
	}
	var alice *FleetResetEntry
	for i := range entries {
		if entries[i].Handle == "alice" {
			alice = &entries[i]
		}
	}
	if alice == nil {
		t.Fatal("no entry for alice")
	}
	if alice.OldCraftCount != 4 {
		t.Errorf("alice OldCraftCount = %d, want 4", alice.OldCraftCount)
	}
	w, err := s.LoadPlayer("SHA256:alice")
	if err != nil {
		t.Fatalf("LoadPlayer(alice): %v", err)
	}
	if len(w.Crafts) != 1 {
		t.Fatalf("alice crafts = %d, want 1", len(w.Crafts))
	}
	c := w.Crafts[0]
	if c.LoadoutID != spacecraft.LoadoutSIVB1ID {
		t.Errorf("loadout = %q, want %q", c.LoadoutID, spacecraft.LoadoutSIVB1ID)
	}
	for i, st := range c.Stages {
		if st.FuelMass != st.FuelCapacity {
			t.Errorf("stage %d fuel %f / capacity %f — want full", i, st.FuelMass, st.FuelCapacity)
		}
		if st.MonopropMass != st.MonopropCap {
			t.Errorf("stage %d monoprop %f / capacity %f — want full", i, st.MonopropMass, st.MonopropCap)
		}
	}
}

// (c) enrollment survives untouched: roster (handles, fingerprints,
// roles), outstanding invites — only craft and clocks reset. Stale
// cross-player docks are cleared (they reference wiped craft).
func TestResetFleetPreservesEnrollment(t *testing.T) {
	s, _ := resetStore(t)
	if err := s.PromoteAdmin("SHA256:alice"); err != nil {
		t.Fatalf("PromoteAdmin: %v", err)
	}
	if _, err := s.MintInvite("carol"); err != nil {
		t.Fatalf("MintInvite: %v", err)
	}
	if err := s.SetDocks([]DockLink{{ID: 1, Owner: "SHA256:alice", GuestOwner: "SHA256:bob"}}); err != nil {
		t.Fatalf("SetDocks: %v", err)
	}
	before, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}

	if _, err := s.ResetFleet(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("ResetFleet: %v", err)
	}

	after, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta after: %v", err)
	}
	if len(after.Roster) != len(before.Roster) {
		t.Fatalf("roster length changed: %d → %d", len(before.Roster), len(after.Roster))
	}
	for i := range before.Roster {
		b, a := before.Roster[i], after.Roster[i]
		if b.Fingerprint != a.Fingerprint || b.Handle != a.Handle || b.Role != a.Role || !b.EnrolledAt.Equal(a.EnrolledAt) {
			t.Errorf("roster entry %d changed: %+v → %+v", i, b, a)
		}
	}
	if len(after.Invites) != len(before.Invites) {
		t.Errorf("invites changed: %d → %d", len(before.Invites), len(after.Invites))
	}
	if len(after.Docks) != 0 {
		t.Errorf("stale docks survived the reset: %+v", after.Docks)
	}
}

// (d) subspace alignment: every reset payload's clock equals the epoch.
func TestResetFleetAlignsSubspaceClocks(t *testing.T) {
	s, _ := resetStore(t)
	epoch := time.Date(2031, 7, 1, 6, 30, 0, 0, time.UTC)
	entries, err := s.ResetFleet(epoch)
	if err != nil {
		t.Fatalf("ResetFleet: %v", err)
	}
	for _, e := range entries {
		if e.Host {
			continue // in-process world; aligned by the caller
		}
		w, err := s.LoadPlayer(e.Fingerprint)
		if err != nil {
			t.Fatalf("LoadPlayer(%s): %v", e.Handle, err)
		}
		if !w.Clock.SimTime.Equal(epoch) {
			t.Errorf("%s: SimTime = %v, want %v", e.Handle, w.Clock.SimTime, epoch)
		}
	}
	// The store-wide frontier is now exactly the epoch.
	if got, ok := s.LatestSimTime(); !ok || !got.Equal(epoch) {
		t.Errorf("LatestSimTime = %v/%v, want %v", got, ok, epoch)
	}
}

// (e) the pre-reset payload survives as a backup, byte-identical.
func TestResetFleetBacksUpPreviousSave(t *testing.T) {
	s, pre := resetStore(t)
	entries, err := s.ResetFleet(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ResetFleet: %v", err)
	}
	var alice, bob FleetResetEntry
	for _, e := range entries {
		switch e.Handle {
		case "alice":
			alice = e
		case "bob":
			bob = e
		}
	}
	if alice.BackupPath == "" {
		t.Fatal("alice had a payload but no backup was recorded")
	}
	if !strings.HasPrefix(alice.BackupPath, filepath.Join(s.dir, "backup")) {
		t.Errorf("backup outside the session backup dir: %s", alice.BackupPath)
	}
	got, err := os.ReadFile(alice.BackupPath)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if !bytes.Equal(got, pre) {
		t.Error("backup does not match the pre-reset payload")
	}
	if bob.BackupPath != "" {
		t.Errorf("bob never had a payload; backup = %q, want none", bob.BackupPath)
	}
	if bob.OldCraftCount != 0 {
		t.Errorf("bob OldCraftCount = %d, want 0 (no payload)", bob.OldCraftCount)
	}
}

// The 50 km floor: a roster too large for >= 50 km even spacing on the
// 500 km ring is refused before anything is touched.
func TestResetFleetRefusesOvercrowdedRing(t *testing.T) {
	s := openStore(t)
	if _, err := s.EnsureHost("jason"); err != nil {
		t.Fatalf("EnsureHost: %v", err)
	}
	m, err := s.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	// The 500 km ring's circumference is ~43,171 km → 863 slots of
	// 50 km. 900 players cannot keep the guarantee.
	for i := 1; i < 900; i++ {
		m.Roster = append(m.Roster, Player{
			Fingerprint: fmt.Sprintf("SHA256:crowd%03d", i),
			Handle:      "crowd",
			Role:        RoleGuest,
		})
	}
	if err := s.writeMeta(m); err != nil {
		t.Fatalf("writeMeta: %v", err)
	}
	if _, err := s.ResetFleet(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("ResetFleet accepted 900 players on a 43,171 km ring")
	} else if !strings.Contains(err.Error(), "50") {
		t.Errorf("refusal should name the 50 km floor: %v", err)
	}
}
