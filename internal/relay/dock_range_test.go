package relay

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// #336: ADR 0038 §4 promised a check of "the ghost-side range reading
// immediately after a slate change", on the theory that the kilometre reads
// measured live were weighted toward display-layer lag. Nothing in the release
// touched the ghost path and no test covered the moment, so the question was
// left open. This is that check, kept as a regression pin rather than a
// finding: the reads were real unpropagated placement error, and the §4
// placement fix (SeparationPush + PlaceAcrossSubspaceGap) removed them.
//
// The invariant, from both seats and with and without a subspace skew: across
// the tick a cross-player undock lands on, a ghost range reading is either
// TRUTHFUL or ABSENT — never a plausible wrong number. Absence is structural
// and lasts one frame: the serve tick builds w.Ghosts from the report store
// BEFORE reconcileDocking mutates the slate, so on the frame of the split the
// partner's last report still predates it. ResolveTargetGhost /
// TargetStateRelativeToActivePrimary both return ok=false for a ghost that
// isn't in the slate — nothing caches a last-known position — so the readout
// goes blank for that frame and resolves on the next.
func TestGhostRangeIsTruthfulAcrossASlateChange(t *testing.T) {
	// The pair's true separation the instant they part: SeparationPush, the
	// only thing that moves them apart, and independent of the ghost path.
	const wantSep = sim.DockingDistM * 1.5 // 75 m
	const tolM = 2.0                       // a couple of metres of Kepler/report slack

	for _, skew := range []time.Duration{0, 5 * time.Second} {
		t.Run(skew.String(), func(t *testing.T) {
			store := NewStore()
			ledger := NewDockLedger()
			const guestID = 1200
			wA, wB := alignedPair(t, guestID)
			dockerID := wA.ActiveCraft().ID
			now := time.Now()

			ledger.Claim(fpA, "alice", dockerID, fpB, "bob", guestID)
			reports := reportMap(store, wA, wB, now)
			ledger.Reconcile(wB, fpB, reports)
			ledger.Reconcile(wA, fpA, reports)
			compositeID := wA.Crafts[0].ID
			if !ledger.RequestUndock(fpB, guestID) {
				t.Fatalf("RequestUndock refused")
			}
			// The guest's subspace clock sits ahead of the docker's — the
			// ordinary case, and the one that produced the kilometre reads.
			wB.Clock.SimTime = wA.Clock.SimTime.Add(skew)

			// The tick the split lands on, in the serve layer's order:
			// report, build ghosts, THEN reconcile. Both sessions.
			t1 := now.Add(time.Second)
			reports = reportMap(store, wA, wB, t1)
			ghostsA := GhostsFor(wA, store.Snapshot(fpA), nil)
			ledger.Reconcile(wA, fpA, reports) // A splits the guest out
			ghostsB := GhostsFor(wB, store.Snapshot(fpB), nil)
			ledger.Reconcile(wB, fpB, reports) // B adopts it

			check := func(label string, ghosts []sim.Ghost, from *sim.World, localID uint64, owner string, id uint64) (found bool) {
				t.Helper()
				local, _, ok := from.CraftByID(localID)
				if !ok {
					t.Fatalf("%s: local craft %d is not in the slate", label, localID)
				}
				for _, g := range ghosts {
					if g.Owner != owner || g.CraftID != id {
						continue
					}
					if d := local.State.R.Sub(g.RelPos).Norm(); d < wantSep-tolM || d > wantSep+tolM {
						t.Errorf("%s: ghost range reads %.1f m, want the %.0f m separation — a plausible wrong number is worse than none", label, d, wantSep)
					}
					return true
				}
				return false
			}

			// Same frame as the split: truthful if present at all.
			check("docker→returned craft, same frame", ghostsA, wA, dockerID, fpB, guestID)
			check("guest→stack, same frame", ghostsB, wB, guestID, fpA, compositeID)

			// Next frame, once both sides have reported their new slates: both
			// readings must be there AND right, subspace skew and all.
			reportMap(store, wA, wB, t1.Add(50*time.Millisecond))
			if !check("docker→returned craft, next frame", GhostsFor(wA, store.Snapshot(fpA), nil), wA, dockerID, fpB, guestID) {
				t.Errorf("the docker still had no ghost of the craft it just released a frame later")
			}
			if !check("guest→stack, next frame", GhostsFor(wB, store.Snapshot(fpB), nil), wB, guestID, fpA, compositeID) {
				t.Errorf("the guest still had no ghost of the stack it just left a frame later")
			}
		})
	}
}
