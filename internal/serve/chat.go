package serve

import (
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// chatRing holds the transient chat lines (ADR 0035 §1): a capped ring
// separate from presence.events, so a chat flood can never evict
// join/leave/sync moments, and so the sender sees their own lines
// without weakening eventsFor's deliberate own-event exclusion.
// Sessions post from their own goroutines; readers copy under the lock.
type chatRing struct {
	mu    sync.Mutex
	lines []sim.ChatLine
}

// chatLineCap bounds the ring. Sized so the 3–4 visible lines a chip
// stack shows can never miss during cross-traffic; eviction only drops
// lines already far older than the display TTL.
const chatLineCap = 32

// chatMessageRuneCap bounds one message. The mint input's 24 is far too
// short for "node is 200m off, burning in 30" (ADR 0035 impl. note).
const chatMessageRuneCap = 120

func newChatRing() *chatRing {
	return &chatRing{}
}

// post appends a line, truncating to chatMessageRuneCap runes and
// trimming the ring. to/toHandle address a DM (empty = broadcast).
// Roster validation happens in the intent handler, which owns store and
// presence access — the ring is storage.
func (c *chatRing) post(owner, handle, to, toHandle, text string) {
	if r := []rune(text); len(r) > chatMessageRuneCap {
		text = string(r[:chatMessageRuneCap])
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.lines = append(c.lines, sim.ChatLine{
		Owner: owner, Handle: handle, To: to, ToHandle: toHandle,
		Text: text, At: time.Now(),
	})
	if len(c.lines) > chatLineCap {
		c.lines = c.lines[len(c.lines)-chatLineCap:]
	}
}

// linesFor copies the lines the viewer may see: every broadcast, plus
// DMs they sent or received. Unlike presence.eventsFor, the viewer's
// own lines are included — in chat you must see what you said.
func (c *chatRing) linesFor(viewer string) []sim.ChatLine {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []sim.ChatLine
	for _, l := range c.lines {
		if l.To != "" && l.To != viewer && l.Owner != viewer {
			continue // a DM between two other players
		}
		out = append(out, l)
	}
	return out
}
