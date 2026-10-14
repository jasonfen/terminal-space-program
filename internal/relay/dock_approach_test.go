package relay

import (
	"testing"
	"time"
)

var (
	seatA = SeatCraft{"SHA256:aaa", 1}
	seatB = SeatCraft{"SHA256:bbb", 7}
)

// The rule is a pure function of the published intents: both seats get the
// same answer whichever asks first, and the precedence is initiator, then
// targeted, then the stable key.
func TestApproacherPrecedence(t *testing.T) {
	cases := []struct {
		name string
		a, b func(*ApproachBoard)
		want SeatCraft
	}{
		{"neither: lower key", func(*ApproachBoard) {}, func(*ApproachBoard) {}, seatA},
		{"only B targets A", nil, func(b *ApproachBoard) { b.Publish(seatB, seatA, "") }, seatB},
		{"only A targets B", func(b *ApproachBoard) { b.Publish(seatA, seatB, "") }, nil, seatA},
		{"both target: lower key", func(b *ApproachBoard) { b.Publish(seatA, seatB, "") },
			func(b *ApproachBoard) { b.Publish(seatB, seatA, "") }, seatA},
		{"B initiator beats A targeting", func(b *ApproachBoard) { b.Publish(seatA, seatB, "") },
			func(b *ApproachBoard) { b.Publish(seatB, SeatCraft{}, seatA.Owner) }, seatB},
		{"initiator toward someone else does not count", func(b *ApproachBoard) { b.Publish(seatA, seatB, "") },
			func(b *ApproachBoard) { b.Publish(seatB, SeatCraft{}, "SHA256:zzz") }, seatA},
	}
	for _, c := range cases {
		var b ApproachBoard
		if c.a != nil {
			c.a(&b)
		}
		if c.b != nil {
			c.b(&b)
		}
		if got := b.Approacher(seatA, seatB); got != c.want {
			t.Errorf("%s: Approacher(A,B) = %v, want %v", c.name, got, c.want)
		}
		if got := b.Approacher(seatB, seatA); got != c.want {
			t.Errorf("%s: Approacher(B,A) = %v, want %v (must not depend on argument order)", c.name, got, c.want)
		}
	}
}

// An intent published for an older vessel of the same owner does not count.
func TestApproacherIgnoresIntentForAnotherCraft(t *testing.T) {
	var b ApproachBoard
	b.Publish(SeatCraft{seatB.Owner, 99}, seatA, "")
	if got := b.Approacher(seatA, seatB); got != seatA {
		t.Fatalf("a stale-craft intent decided the pair: %v", got)
	}
}

// Once a pull runs the pair has one puller; the passive seat cannot start a
// second even if its intent now outranks, and the latch frees on EndPull or
// when the registration goes stale.
func TestPullLatchIsOnePullerPerPair(t *testing.T) {
	var b ApproachBoard
	now := time.Unix(1000, 0)
	b.now = func() time.Time { return now }
	b.Publish(seatA, seatB, "")
	if b.BeginPull(seatB, seatA) {
		t.Fatal("the non-approacher was allowed to begin a pull")
	}
	if !b.BeginPull(seatA, seatB) {
		t.Fatal("the approacher was refused")
	}
	// B now targets A too and... still loses: the latch holds A.
	b.Publish(seatB, seatA, seatA.Owner)
	if b.BeginPull(seatB, seatA) {
		t.Fatal("a second puller was registered on a pair that has one")
	}
	if p, ok := b.PullerOf(seatA, seatB); !ok || p != seatA {
		t.Fatalf("puller = %v %v", p, ok)
	}
	b.EndPull(seatB, seatA) // not the puller: no effect
	if _, ok := b.PullerOf(seatA, seatB); !ok {
		t.Fatal("a non-puller's EndPull released the latch")
	}
	b.EndPull(seatA, seatB)
	if _, ok := b.PullerOf(seatA, seatB); ok {
		t.Fatal("EndPull left the pair latched")
	}
	// A vanished puller's registration expires.
	b.Publish(seatA, seatB, "")
	b.BeginPull(seatA, seatB)
	now = now.Add(approachStale + time.Second)
	if _, ok := b.PullerOf(seatA, seatB); ok {
		t.Fatal("a registration from a seat that stopped ticking still latches the pair")
	}
}

// Two seats racing to Claim one pair end in one record, whichever is first.
func TestLedgerTakesOneClaimPerPairInEitherOrder(t *testing.T) {
	for _, aFirst := range []bool{true, false} {
		l := NewDockLedger()
		ca := func() bool {
			_, ok := l.Claim(seatA.Owner, "a", seatA.CraftID, seatB.Owner, "b", seatB.CraftID)
			return ok
		}
		cb := func() bool {
			_, ok := l.Claim(seatB.Owner, "b", seatB.CraftID, seatA.Owner, "a", seatA.CraftID)
			return ok
		}
		var first, second bool
		if aFirst {
			first, second = ca(), cb()
		} else {
			first, second = cb(), ca()
		}
		if !first || second || l.RecordCount() != 1 {
			t.Errorf("aFirst=%v: first=%v second=%v records=%d, want true false 1", aFirst, first, second, l.RecordCount())
		}
	}
}
