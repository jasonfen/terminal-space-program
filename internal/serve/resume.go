package serve

import (
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// heldMomentCap bounds one player's bank. An away session can bank for
// hours, and the newest moments are the ones that explain the world the
// player is opening their lid onto.
const heldMomentCap = 32

// bank is one player's unattended interval: when it opened, in their own
// sim-time, and the moments that fell during it.
type bank struct {
	since   time.Time // sim-time at the first tick the session was seen away
	moments []sim.SessionEvent
}

// awayMail holds the moments that fell while a player's session ran
// unattended, so they can be replayed to the player rather than rendered
// to an empty chair and aged out by the chip TTL (ADR 0036 S6).
//
// Keyed by fingerprint rather than by session, because the two ends of
// the interval are often different sessions: a reclaim tears the first
// one down and builds a second. The handoff is safe because
// displaceAbsent does not return until the displaced session's teardown
// has completed, so the writer is finished before the reader exists.
type awayMail struct {
	mu    sync.Mutex
	banks map[string]*bank
}

func newAwayMail() *awayMail { return &awayMail{banks: map[string]*bank{}} }

// hold banks moments for fp, opening the bank at simNow the first time.
// The opening instant is kept, not refreshed: it is what the elapsed
// readout is measured from, and a later tick resetting it would report
// the interval as a few seconds no matter how long it really ran.
func (a *awayMail) hold(fp string, simNow time.Time, moments []sim.SessionEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.banks[fp]
	if !ok {
		b = &bank{since: simNow}
		a.banks[fp] = b
	}
	b.moments = append(b.moments, moments...)
	if len(b.moments) > heldMomentCap {
		b.moments = b.moments[len(b.moments)-heldMomentCap:]
	}
}

// take empties fp's bank and returns it. ok is false when nothing was
// banked, which is the ordinary case for a session that never went away.
func (a *awayMail) take(fp string) ([]sim.SessionEvent, time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.banks[fp]
	if !ok {
		return nil, time.Time{}, false
	}
	delete(a.banks, fp)
	return b.moments, b.since, true
}

// peek reads a bank without emptying it (tests, and nothing else).
func (a *awayMail) peek(fp string) ([]sim.SessionEvent, time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	b, ok := a.banks[fp]
	if !ok {
		return nil, time.Time{}, false
	}
	return b.moments, b.since, true
}

// drop discards fp's bank. Called when a session ends for real rather
// than being displaced: the player's next connection is an ordinary
// reconnect into a saved world, not the far end of an interval they were
// continuously present for, and replaying one days later would describe a
// world the payload already reflects.
func (a *awayMail) drop(fp string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.banks, fp)
}

// bankOrReplay routes this tick's local moments: into the bank while
// nobody is watching, out of it when somebody is.
//
// A replayed moment is re-stamped. Its original timestamp is hours old,
// and the chip stack expires by wall clock — delivered as-is it would be
// expired on arrival, which is exactly the failure this slice exists to
// fix.
func (m *reportingModel) bankOrReplay(simNow, now time.Time) {
	if m.srv.isAway(m.owner) {
		m.srv.mail.hold(m.owner, simNow, m.localEvents)
		m.localEvents = nil
		return
	}
	held, since, ok := m.srv.mail.take(m.owner)
	if !ok {
		return
	}
	// The account comes first, then what it is accounting for.
	m.localEvents = append(m.localEvents, sim.SessionEvent{
		Kind: sim.SessionEventResumed, At: now, Elapsed: simNow.Sub(since),
	})
	for _, e := range held {
		e.At = now
		m.localEvents = append(m.localEvents, e)
	}
}
