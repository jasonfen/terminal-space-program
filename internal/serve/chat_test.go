package serve

import (
	"strings"
	"testing"
)

// ADR 0035 §1: chat lives in its own capped ring beside presence, so a
// chat flood can never evict join/leave/sync moments — and so the
// viewer's own lines are visible without weakening eventsFor's
// deliberate own-event exclusion.

func TestChatRingOwnLinesVisible(t *testing.T) {
	c := newChatRing()
	c.post("fpA", "alice", "", "", "burning in 30")
	lines := c.linesFor("fpA")
	if len(lines) != 1 {
		t.Fatalf("sender must see their own line (ADR 0035: 'you must see what you said'); got %d lines", len(lines))
	}
	if lines[0].Text != "burning in 30" || lines[0].Handle != "alice" {
		t.Fatalf("line mangled: %+v", lines[0])
	}
}

func TestChatRingBroadcastVisibleToAll(t *testing.T) {
	c := newChatRing()
	c.post("fpA", "alice", "", "", "node is 200m off")
	for _, viewer := range []string{"fpA", "fpB", "fpC"} {
		if got := c.linesFor(viewer); len(got) != 1 {
			t.Fatalf("broadcast must reach %s; got %d lines", viewer, len(got))
		}
	}
}

func TestChatRingDMVisibility(t *testing.T) {
	c := newChatRing()
	c.post("fpA", "alice", "fpB", "bob", "hold your burn")
	if got := c.linesFor("fpA"); len(got) != 1 {
		t.Fatalf("DM sender must see their own line; got %d", len(got))
	}
	if got := c.linesFor("fpB"); len(got) != 1 {
		t.Fatalf("DM recipient must see the line; got %d", len(got))
	}
	if got := c.linesFor("fpC"); len(got) != 0 {
		t.Fatalf("a DM must never reach a third party; got %d lines", len(got))
	}
	// The recipient needs the sender's handle; the sender's own echo
	// needs the target handle for the visibly-distinct "→bob:" render.
	if l := c.linesFor("fpB")[0]; l.Handle != "alice" || l.ToHandle != "bob" {
		t.Fatalf("DM handles mangled: %+v", l)
	}
}

func TestChatRingCapEvictsOldest(t *testing.T) {
	c := newChatRing()
	for i := 0; i < chatLineCap+8; i++ {
		c.post("fpA", "alice", "", "", strings.Repeat("x", i%9+1))
	}
	lines := c.linesFor("fpB")
	if len(lines) != chatLineCap {
		t.Fatalf("ring must trim to chatLineCap=%d; got %d", chatLineCap, len(lines))
	}
	// The survivor set is the newest cap-many posts, oldest first.
	if lines[0].Text == "x" {
		t.Fatalf("oldest lines must be evicted, ring still starts at the first post")
	}
}

func TestChatRingTextRuneCap(t *testing.T) {
	c := newChatRing()
	// Multibyte runes: a byte-based truncation would split one in half.
	long := strings.Repeat("ü", chatMessageRuneCap+40)
	c.post("fpA", "alice", "", "", long)
	got := c.linesFor("fpA")[0].Text
	if n := len([]rune(got)); n != chatMessageRuneCap {
		t.Fatalf("text must truncate to %d runes; got %d", chatMessageRuneCap, n)
	}
	if !strings.HasPrefix(long, got) {
		t.Fatalf("truncation must be rune-safe (prefix of the original)")
	}
}

func TestChatRingIndependentOfPresence(t *testing.T) {
	// The whole point of the separate ring: chat volume must not evict
	// presence moments, and presence filtering rules must not leak in.
	p := newPresence()
	c := newChatRing()
	p.event(0, "fpA", "alice", "")
	for i := 0; i < chatLineCap*2; i++ {
		c.post("fpA", "alice", "", "", "spam")
	}
	if got := p.eventsFor("fpB"); len(got) != 1 {
		t.Fatalf("chat flood must never evict presence events; got %d", len(got))
	}
}
