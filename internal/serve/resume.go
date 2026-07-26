package serve

import (
	"fmt"
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

const (
	// heldMomentCap bounds one player's bank. An away session can bank for
	// hours, and the newest moments are the ones that explain the world the
	// player is opening their lid onto.
	heldMomentCap = 32

	// maxReplayedMoments bounds what reaches the canvas at once. Every
	// replayed moment is re-stamped to the same instant, so they render as
	// one block for the full chip TTL; a whole bank would exceed a normal
	// terminal's height and bury the orbit view it is meant to explain.
	maxReplayedMoments = 6

	// minReportedAway is the shortest interval worth announcing. A player
	// who paused, read for a minute and carried on has technically been
	// unattended by the frames-drained measure, and "resumed — 0s ran while
	// you were away" is noise about nothing.
	minReportedAway = time.Minute
)

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
	// Only the session currently holding this slot may touch the bank.
	//
	// A displaced session keeps ticking until its program unwinds, and it
	// leaves the registry at the *start* of the reclaim — closing its
	// connection is what unblocks its loop, so it reliably gets ticks in
	// afterwards. Without this guard it reads as not-away (no registry
	// entry), takes the replay branch, and drains the bank into a slice
	// discarded seconds later, leaving the returning player with nothing.
	// The hazard is not the writer outliving the reader; it is the dying
	// writer becoming a reader.
	if _, live := m.srv.live.get(m.owner); !live {
		return
	}
	if m.srv.isAway(m.owner) {
		// Copied, not moved. isAway keys on frames drained, so a player
		// sitting on a paused, static screen reads as away while being right
		// there; moving their moments would take chips off the screen of
		// someone watching. Banking a copy keeps a misclassification free,
		// which is what the short away threshold assumes.
		m.srv.mail.hold(m.owner, simNow, m.localEvents)
		return
	}
	held, since, ok := m.srv.mail.take(m.owner)
	if !ok {
		return
	}
	// Anything still inside its TTL was on screen a moment ago — the copy
	// above means a player who never really left would otherwise see their
	// own moments twice.
	unseen := held[:0]
	for _, e := range held {
		if now.Sub(e.At) > localEventTTL {
			unseen = append(unseen, e)
		}
	}
	held = unseen
	// The chip stack is corner-anchored on the canvas and every replayed
	// moment lands at once: a full bank would bury the orbit view and clip
	// the other chips. Newest kept, the rest counted.
	dropped := 0
	if len(held) > maxReplayedMoments {
		dropped = len(held) - maxReplayedMoments
		held = held[len(held)-maxReplayedMoments:]
	}
	elapsed := simNow.Sub(since)
	if len(held) == 0 && elapsed < minReportedAway {
		return // nothing happened, and no time to speak of: no account owed
	}
	// The account comes first, then what it is accounting for.
	resumed := sim.SessionEvent{Kind: sim.SessionEventResumed, At: now, Elapsed: elapsed}
	if dropped > 0 {
		resumed.Detail = fmt.Sprintf("+%d earlier", dropped)
	}
	m.localEvents = append(m.localEvents, resumed)
	for _, e := range held {
		e.At = now
		m.localEvents = append(m.localEvents, e)
	}
}
