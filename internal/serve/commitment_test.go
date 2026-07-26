package serve

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/relay"
)

const (
	fpA = "SHA256:aaa"
	fpB = "SHA256:bbb"
	fpC = "SHA256:ccc"
)

var simNoon = time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)

// armed builds a report for owner whose Rendezvous Warp intent names
// target, committed to tau.
func armed(owner, target string, tau time.Time) relay.CraftReport {
	return relay.CraftReport{
		Owner:            owner,
		SubspaceTime:     simNoon,
		RendezvousTarget: target,
		RendezvousTau:    tau,
	}
}

// A Reprieve is earned by an *Engaged* Rendezvous Warp, and a Rendezvous
// Warp is Engaged only once both players have Engaged toward each other
// (CONTEXT.md: "Once both have Engaged, their Subspaces rate-lock"). A
// lone arm is a pending invite: no coast has started, nobody is waiting
// on a coast, and the partner sees the prompt vanish rather than being
// stranded by it.
func TestCommitmentForRendezvous(t *testing.T) {
	tau := simNoon.Add(90 * time.Minute)
	cases := []struct {
		name    string
		reports []relay.CraftReport
		want    bool
	}{
		{
			name:    "mutually armed — the coast both players committed to",
			reports: []relay.CraftReport{armed(fpA, fpB, tau), armed(fpB, fpA, tau)},
			want:    true,
		},
		{
			name:    "armed alone, partner has not Engaged back",
			reports: []relay.CraftReport{armed(fpA, fpB, tau), {Owner: fpB, SubspaceTime: simNoon}},
			want:    false,
		},
		{
			name:    "partner armed toward us, we have not Engaged back",
			reports: []relay.CraftReport{{Owner: fpA, SubspaceTime: simNoon}, armed(fpB, fpA, tau)},
			want:    false,
		},
		{
			name:    "armed toward a player who is not reporting at all",
			reports: []relay.CraftReport{armed(fpA, fpB, tau)},
			want:    false,
		},
		{
			// Both armed, but not at each other — two unrelated pending
			// invites, not one mutual encounter.
			name:    "arms cross past each other",
			reports: []relay.CraftReport{armed(fpA, fpB, tau), armed(fpB, fpC, tau)},
			want:    false,
		},
		{
			name:    "no arm at all",
			reports: []relay.CraftReport{{Owner: fpA, SubspaceTime: simNoon}, {Owner: fpB, SubspaceTime: simNoon}},
			want:    false,
		},
		{
			name:    "not reporting",
			reports: nil,
			want:    false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := commitmentFor(tc.reports, nil, fpA)
			if ok != tc.want {
				t.Fatalf("commitmentFor = %v, want %v", ok, tc.want)
			}
			if !ok {
				return
			}
			if c.kind != commitRendezvous {
				t.Errorf("kind = %v, want commitRendezvous", c.kind)
			}
			if c.peer != fpB {
				t.Errorf("peer = %q, want %q", c.peer, fpB)
			}
			if want := 90 * time.Minute; c.toGo != want {
				t.Errorf("toGo = %v, want %v (sim-time left to the committed TCA)", c.toGo, want)
			}
		})
	}
}

// Both ends of a live cross-player dock are committed. The guest's craft
// is fused into the owner's Stack: if the guest drops, the owner is
// flying someone else's hardware they cannot give back; if the *owner*
// drops, the guest's craft stops being simulated inside a stack they
// cannot undock from, since the split happens on the owner's tick.
func TestCommitmentForDock(t *testing.T) {
	active := relay.DockRecord{
		ID: 1, Owner: fpA, OwnerHandle: "ansi",
		GuestOwner: fpB, GuestHandle: "vex", Phase: relay.DockActive,
	}
	cases := []struct {
		name     string
		docks    []relay.DockRecord
		fp       string
		want     bool
		wantPeer string
	}{
		{name: "guest riding another player's stack", docks: []relay.DockRecord{active}, fp: fpB, want: true, wantPeer: fpA},
		{name: "owner carrying another player's craft", docks: []relay.DockRecord{active}, fp: fpA, want: true, wantPeer: fpB},
		{
			// Mid-handshake, no craft has changed hands yet, and it resolves
			// within a tick or two under normal operation.
			name:  "pending dock",
			docks: []relay.DockRecord{{ID: 2, Owner: fpA, GuestOwner: fpB, Phase: relay.DockPending}},
			fp:    fpB,
			want:  false,
		},
		{name: "a dock between two other players", docks: []relay.DockRecord{active}, fp: fpC, want: false},
		{name: "no docks", docks: nil, fp: fpA, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, ok := commitmentFor(nil, tc.docks, tc.fp)
			if ok != tc.want {
				t.Fatalf("commitmentFor = %v, want %v", ok, tc.want)
			}
			if !ok {
				return
			}
			if c.kind != commitDock {
				t.Errorf("kind = %v, want commitDock", c.kind)
			}
			if c.peer != tc.wantPeer {
				t.Errorf("peer = %q, want %q", c.peer, tc.wantPeer)
			}
		})
	}
}

// The deliberate exclusion (ADR 0036): Proximity Co-Warp forms by itself
// whenever two players drift close and strands nobody when it lapses —
// the survivor decouples and warps on. It is also the one coupling whose
// state lives only on the World, so granting it a Reprieve would put a
// physics recompute in the sweeper. Two players flying alongside each
// other with no arm and no dock look exactly like this: uncommitted.
func TestProximityCoWarpEarnsNoReprieve(t *testing.T) {
	near := []relay.CraftReport{
		{Owner: fpA, SubspaceTime: simNoon, EffWarp: 100, Crafts: []relay.CraftState{{ID: 1, Primary: "Terra"}}},
		{Owner: fpB, SubspaceTime: simNoon, EffWarp: 100, Crafts: []relay.CraftState{{ID: 2, Primary: "Terra"}}},
	}
	if _, ok := commitmentFor(near, nil, fpA); ok {
		t.Error("proximity co-warp earned a Reprieve — it forms by itself and strands nobody, " +
			"and its state lives only on the World the sweeper must never touch")
	}
}

// A dock outlives a rendezvous coast in practice, so it wins when a
// session somehow holds both.
func TestCommitmentDockOutranksRendezvous(t *testing.T) {
	tau := simNoon.Add(10 * time.Minute)
	reports := []relay.CraftReport{armed(fpA, fpB, tau), armed(fpB, fpA, tau)}
	docks := []relay.DockRecord{{ID: 1, Owner: fpC, GuestOwner: fpA, Phase: relay.DockActive}}
	c, ok := commitmentFor(reports, docks, fpA)
	if !ok || c.kind != commitDock {
		t.Errorf("commitmentFor = (%v, %v), want a dock Commitment", c.kind, ok)
	}
}

func TestCommitmentExpiry(t *testing.T) {
	now := time.Date(2026, 7, 25, 16, 0, 0, 0, time.UTC)
	quiet := now.Add(-20 * time.Minute) // the peer stopped speaking 20 min ago
	cases := []struct {
		name string
		c    commitment
		want time.Time
	}{
		{
			// Flat, and counted from when the peer went quiet — not from now,
			// or an absent session would renew its own cap every sweep.
			name: "dock: flat cap from the last time the peer spoke",
			c:    commitment{kind: commitDock},
			want: quiet.Add(dockReprieveCap),
		},
		{
			// The coast is what must finish. Its worst case is 1× real time,
			// so the sim-time left to the TCA is an upper bound on the wall
			// time left — any warp only makes it arrive sooner.
			name: "rendezvous: the coast's worst-case wall time plus a margin",
			c:    commitment{kind: commitRendezvous, toGo: 90 * time.Minute},
			want: now.Add(90*time.Minute + rendezvousTauOvershoot),
		},
		{
			// A TCA committed weeks of sim-time out is normally minutes of
			// wall time under warp, but a paused or 1×-pinned session would
			// hold the connection open for weeks on that arithmetic.
			name: "rendezvous: a far-off TCA still hits the unattended ceiling",
			c:    commitment{kind: commitRendezvous, toGo: 30 * 24 * time.Hour},
			want: quiet.Add(maxUnattendedReprieve),
		},
		{
			// τ passed but the arm has not cleared yet (arrival lands on the
			// session's own tick): let it land, don't extend indefinitely.
			name: "rendezvous: the TCA already went by",
			c:    commitment{kind: commitRendezvous, toGo: -5 * time.Minute},
			want: now.Add(-5*time.Minute + rendezvousTauOvershoot),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.expiry(now, quiet); !got.Equal(tc.want) {
				t.Errorf("expiry = %v, want %v", got, tc.want)
			}
		})
	}
}

// The empirical bound the whole feature exists for: the 2026-07-25
// playtest coast ran 2h19m of real time at 1×, because co-warp pins a
// coupled pair to the slower player's rate. A ceiling that cannot cover
// an observed real coast would reintroduce exactly the "strands your
// partner" regression the Reprieve is here to prevent.
func TestReprieveCoversAnObservedCoast(t *testing.T) {
	const observed = 2*time.Hour + 19*time.Minute
	now := time.Date(2026, 7, 25, 16, 0, 0, 0, time.UTC)
	c := commitment{kind: commitRendezvous, toGo: observed}
	if got := c.expiry(now, now); got.Sub(now) < observed {
		t.Errorf("a %v coast gets only %v of Reprieve — the partner is stranded mid-coast",
			observed, got.Sub(now))
	}
}
