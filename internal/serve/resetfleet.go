package serve

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
)

// ResetFleet performs the startup fleet reset behind --reset-fleet:
// every enrolled player's slate is wiped to one default vessel on the
// shared 500x500 km ring (sessiondir.ResetFleet) and every subspace
// clock is aligned to a single epoch, so nobody needs a Sync after the
// reset.
//
// The epoch is the server's current sim time: the frontier across all
// persisted payloads, floored by the host's in-process clock — clocks
// only ever move forward, matching the join-at-the-frontier rule
// (ADR 0034). The caller must set the host's in-process world clock to
// the returned epoch (the host's world never lives in the session
// store) and should run this before accepting connections.
//
// One-shot by construction: nothing about the reset is persisted as
// pending state, so a restart without the flag changes nothing.
func (s *Server) ResetFleet(hostClock time.Time) ([]sessiondir.FleetResetEntry, time.Time, error) {
	epoch := hostClock
	if stored, ok := s.store.LatestSimTime(); ok && stored.After(epoch) {
		epoch = stored
	}
	entries, err := s.store.ResetFleet(epoch)
	return entries, epoch, err
}
