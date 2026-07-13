package serve

import (
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// presence tracks who is live right now and the recent join/leave/
// sync moments (v0.27 S6). Sessions mutate it from their own
// goroutines; readers copy under the lock.
type presence struct {
	mu     sync.Mutex
	online map[string]int // fingerprint → live session count
	events []sim.SessionEvent
}

const presenceEventCap = 32

func newPresence() *presence {
	return &presence{online: map[string]int{}}
}

func (p *presence) markOnline(fp string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.online[fp]++
}

func (p *presence) markOffline(fp string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.online[fp] > 1 {
		p.online[fp]--
	} else {
		delete(p.online, fp)
	}
}

func (p *presence) isOnline(fp string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.online[fp] > 0
}

// event appends a session moment, trimming the ring.
func (p *presence) event(kind sim.SessionEventKind, owner, handle string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, sim.SessionEvent{Kind: kind, Owner: owner, Handle: handle, At: time.Now()})
	if len(p.events) > presenceEventCap {
		p.events = p.events[len(p.events)-presenceEventCap:]
	}
}

// eventsFor copies recent events, excluding the viewer's own.
func (p *presence) eventsFor(viewer string) []sim.SessionEvent {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []sim.SessionEvent
	for _, e := range p.events {
		if e.Owner != viewer {
			out = append(out, e)
		}
	}
	return out
}
