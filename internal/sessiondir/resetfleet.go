package sessiondir

// Fleet reset (--reset-fleet): wipe every enrolled player's craft
// slate down to the one default vessel a fresh enrollment receives,
// all on the same Earth 500x500 km circular equatorial orbit in Sol,
// spaced evenly around the ring, with every player's subspace clock
// set to a single shared epoch. Enrollment — handles, fingerprints,
// roles, outstanding invites — is untouched; each player's previous
// payload is backed up before being replaced. Stale cross-player dock
// cross-refs are cleared: they reference craft the reset just wiped.
//
// The host plays in-process (their world never lives in players/), so
// the host's roster entry claims ring slot 0 — which is exactly the
// fresh-start seed placement (spacecraft.NewInLEO == phase 0) — and no
// payload is written for it; the caller aligns the in-process world's
// clock to the returned epoch instead.

import (
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

const (
	// FleetResetAltitudeM is the shared ring's altitude: 500 km, the
	// same circular orbit a fresh enrollment's default vessel spawns
	// into (spacecraft.NewInLEO), so a reset slate is indistinguishable
	// from a first join apart from the phase slot.
	FleetResetAltitudeM = 500e3

	// FleetResetMinSeparationM is the guaranteed along-track distance
	// between ring neighbours: 50 km. It MUST exceed the co-warp
	// DECOUPLE gate (coWarpDecoupleRangeM = 42 km in
	// internal/sim/cowarp.go) — the wider hysteresis bound above the
	// 35 km couple gate and far above the docking range — so no two
	// reset vessels can spawn coupled, in docking range, or even
	// inside the couple neighbourhood. At the 500 km ring's ~43,171 km
	// circumference, even spacing keeps this for up to 863 players;
	// ResetFleet refuses larger rosters rather than shrink the gap.
	FleetResetMinSeparationM = 50_000.0
)

// FleetResetEntry reports what ResetFleet did for one enrolled player.
type FleetResetEntry struct {
	Fingerprint string
	Handle      string
	Role        string
	// Host marks the in-process host entry: it holds ring slot 0 but no
	// payload is written (the host's world lives in-process; the caller
	// aligns it).
	Host bool
	// OldCraftCount is the craft count of the player's previous payload:
	// 0 when they had none, -1 when it existed but could not be read
	// (it is still backed up and replaced).
	OldCraftCount int
	// PhaseDeg is the player's slot on the shared ring, degrees prograde
	// from the fresh-start seed spot.
	PhaseDeg float64
	// BackupPath is where the previous payload was copied, empty when
	// there was none.
	BackupPath string
}

// ResetFleet wipes every enrolled player's slate to one full-tanks
// default vessel on the shared 500x500 km ring, evenly phased, with
// every written payload's subspace clock set to epoch. Payloads are
// produced by the real save machinery and verified to load back
// through the real load path before the reset is considered done.
// It refuses — before touching anything — a roster too large to keep
// FleetResetMinSeparationM between neighbours.
func (s *Store) ResetFleet(epoch time.Time) ([]FleetResetEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, err := s.readMeta()
	if err != nil {
		return nil, err
	}
	if len(m.Roster) == 0 {
		return nil, errors.New("sessiondir: reset-fleet: empty roster")
	}

	// Probe one fresh world up front: it supplies Earth (for the ring
	// geometry check) and fails fast if worldgen itself is broken,
	// before any payload is disturbed.
	probe, err := sim.NewWorld()
	if err != nil {
		return nil, fmt.Errorf("sessiondir: reset-fleet: %w", err)
	}
	earth := probe.Systems[0].FindBody("Earth")
	if earth == nil {
		return nil, errors.New("sessiondir: reset-fleet: no Earth in Sol")
	}

	n := len(m.Roster)
	ringR := earth.RadiusMeters() + FleetResetAltitudeM
	separation := 2 * math.Pi * ringR / float64(n)
	if separation < FleetResetMinSeparationM {
		return nil, fmt.Errorf(
			"sessiondir: reset-fleet: %d players spaced evenly on the %.0f km ring would sit %.1f km apart — below the %.0f km co-warp/docking floor",
			n, 2*math.Pi*ringR/1000, separation/1000, FleetResetMinSeparationM/1000)
	}

	// Ring slots: the host first (slot 0 — the fresh-start seed spot,
	// matching its in-process world), then everyone else in roster
	// order. EnsureHost prepends the host, but don't depend on it.
	ordered := make([]Player, 0, n)
	for _, p := range m.Roster {
		if p.Fingerprint == HostFingerprint {
			ordered = append(ordered, p)
		}
	}
	for _, p := range m.Roster {
		if p.Fingerprint != HostFingerprint {
			ordered = append(ordered, p)
		}
	}

	backupStamp := time.Now().UTC().Format("20060102T150405Z")
	entries := make([]FleetResetEntry, 0, n)
	for i, p := range ordered {
		e := FleetResetEntry{
			Fingerprint: p.Fingerprint,
			Handle:      p.Handle,
			Role:        p.Role,
			PhaseDeg:    360 * float64(i) / float64(n),
		}
		if p.Fingerprint == HostFingerprint {
			// The host's world runs in-process and is fresh every --serve
			// start; slot 0 is exactly that world's seed placement, so
			// there is nothing to write — only the clock, which the
			// caller aligns to epoch.
			e.Host = true
			entries = append(entries, e)
			continue
		}

		src := s.payloadPath(p.Fingerprint)
		if data, err := os.ReadFile(src); err == nil {
			// Back up before anything else touches the file. The backup
			// is the raw previous envelope, byte-identical.
			bdir := filepath.Join(s.dir, "backup")
			if err := os.MkdirAll(bdir, 0o755); err != nil {
				return nil, fmt.Errorf("sessiondir: reset-fleet: backup dir: %w", err)
			}
			bak := filepath.Join(bdir, backupStamp+"-"+filepath.Base(src))
			if err := os.WriteFile(bak, data, 0o644); err != nil {
				return nil, fmt.Errorf("sessiondir: reset-fleet: backup %s: %w", p.Handle, err)
			}
			e.BackupPath = bak
			if old, err := save.Load(src); err == nil {
				e.OldCraftCount = len(old.Crafts)
			} else {
				e.OldCraftCount = -1 // unreadable (e.g. catalog mismatch) — reset proceeds
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("sessiondir: reset-fleet: read %s: %w", p.Handle, err)
		}

		w, err := resetWorld(e.PhaseDeg, epoch)
		if err != nil {
			return nil, fmt.Errorf("sessiondir: reset-fleet: %s: %w", p.Handle, err)
		}
		if err := save.Save(w, src); err != nil {
			return nil, fmt.Errorf("sessiondir: reset-fleet: write %s: %w", p.Handle, err)
		}
		// Round-trip guard: the payload must come back through the real
		// load path (schema, catalog hash) before the reset counts.
		if _, err := save.Load(src); err != nil {
			return nil, fmt.Errorf("sessiondir: reset-fleet: %s: reset payload fails to load back: %w", p.Handle, err)
		}
		entries = append(entries, e)
	}

	// Cross-player docks reference craft that no longer exist in any
	// payload; a reconnect must not resume docked-as-guest into a
	// wiped stack. Roster and invites are deliberately untouched.
	if len(m.Docks) > 0 {
		m.Docks = nil
		if err := s.writeMeta(m); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// resetWorld builds the world a reset player resumes into: the default
// fresh start (sim.NewWorld — same systems, missions, ground stations
// a fresh enrollment gets) whose single seed vessel is re-placed at
// the given ring phase and whose clock is set to the shared epoch.
// Craft state is primary-relative, so the placement is epoch-safe.
func resetWorld(phaseDeg float64, epoch time.Time) (*sim.World, error) {
	w, err := sim.NewWorld()
	if err != nil {
		return nil, err
	}
	earth := w.Systems[0].FindBody("Earth")
	if earth == nil {
		return nil, errors.New("no Earth in Sol")
	}
	c := spacecraft.NewInLEOAtPhase(*earth, phaseDeg)
	c.SystemIdx = 0
	w.Crafts = []*spacecraft.Spacecraft{c}
	w.ActiveCraftIdx = 0
	w.EnsureCraftIDs()
	w.Clock.SimTime = epoch
	return w, nil
}
