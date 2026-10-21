package relay

import "testing"

// TestClaimRefusesASecondRideOrSecondStackForOnePlayer (audit #13): the ledger's
// verbs and the World's single DockGuest slot assume a player is the guest of
// at most one dock and the owner of at most one stack. A Claim that broke
// either made [J]/[U]/the rider camera act on a map-order-random record.
func TestClaimRefusesASecondRideOrSecondStackForOnePlayer(t *testing.T) {
	const fpD = "SHA256:dave"

	// Same passive player claimed twice, by two approachers, with two vessels.
	l := NewDockLedger()
	if _, ok := l.Claim(fpA, "alice", 1, fpB, "bob", 10); !ok {
		t.Fatalf("first claim refused")
	}
	if _, ok := l.Claim(fpC, "carol", 2, fpB, "bob", 11); ok {
		t.Errorf("bob became the guest in a second dock")
	}
	if !l.ClaimRefused(fpC, 2, fpB, 11) {
		t.Errorf("ClaimRefused disagrees with Claim for a second ride")
	}

	// Same docker owning two stacks (different vessels, different guests).
	l = NewDockLedger()
	if _, ok := l.Claim(fpA, "alice", 1, fpB, "bob", 10); !ok {
		t.Fatalf("first claim refused")
	}
	if _, ok := l.Claim(fpA, "alice", 3, fpD, "dave", 12); ok {
		t.Errorf("alice came to own a second stack")
	}
	if !l.ClaimRefused(fpA, 3, fpD, 12) {
		t.Errorf("ClaimRefused disagrees with Claim for a second stack")
	}

	// Unrelated players still dock freely.
	if _, ok := l.Claim(fpC, "carol", 2, fpD, "dave", 12); !ok {
		t.Errorf("an unrelated pair was refused")
	}
}
