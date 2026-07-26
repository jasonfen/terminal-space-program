package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

// Hold-τ degrade recompute: while the coast runs, a partner holding the
// committed encounter raises no flag; one that drifts a couple-radius past
// the committed approach at τ does — and τ is never re-targeted.
func TestRendezvousDegradeHeldEncounter(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Minute) // short dt so the approach at τ ≈ the offset
	a := w.ActiveCraft()
	same := func(dR orbital.Vec3) []CoWarpCraft {
		return []CoWarpCraft{{Primary: primary, R: a.State.R.Add(dR), V: a.State.V}}
	}
	peer := func(crafts []CoWarpCraft) CoWarpPeer {
		return CoWarpPeer{
			Owner: "SHA256:gern", Handle: "gern", SubspaceTime: st, EffWarp: 50,
			ArmedTowardViewer: true, Crafts: crafts,
		}
	}

	// Committed approach 0; partner coincident at τ → no degrade.
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	w.DriveRendezvousWarp([]CoWarpPeer{peer(same(orbital.Vec3{}))})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}
	if w.RendezvousDegraded {
		t.Error("degraded while the partner holds the committed encounter")
	}

	// Partner drifts 50 km off the committed approach → degrade + readout.
	tauBefore := w.RendezvousArm.Tau
	w.DriveRendezvousWarp([]CoWarpPeer{peer(same(orbital.Vec3{X: 50000}))})
	if !w.RendezvousDegraded {
		t.Error("no degrade flag after the partner drifted 50 km off τ")
	}
	if w.RendezvousApproachM < 40000 {
		t.Errorf("RendezvousApproachM = %v, want ~50 km", w.RendezvousApproachM)
	}
	if !w.RendezvousArm.Tau.Equal(tauBefore) {
		t.Error("τ was re-targeted — the encounter must be held, only warned")
	}
}

// Engage records the arm and is forward-only: an encounter at or behind
// SimTime is refused (the laggard Syncs forward; you never warp backward).
func TestEngageRendezvousWarpForwardOnly(t *testing.T) {
	w, _, st := anchorWorld(t)

	if w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(-time.Hour), 0) {
		t.Error("engaged toward a past encounter")
	}
	if w.RendezvousArm != nil {
		t.Error("arm set despite forward-only refusal")
	}
	if !w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(72*time.Hour), 0) {
		t.Fatal("refused a future encounter")
	}
	if w.RendezvousArm == nil || w.RendezvousArm.TargetOwner != "SHA256:gern" {
		t.Errorf("arm = %+v, want targeting gern", w.RendezvousArm)
	}
	// Engaging does NOT start the coast on its own — that waits for mutual.
	if w.AutoWarp != nil {
		t.Error("Auto-Warp started at engage (should wait for the partner)")
	}
}

// The shared coast starts only once the partner has Engaged back: armed
// but unpartnered holds warp unchanged (no solo drift).
func TestDriveRendezvousWarpWaitsForPartner(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(72*time.Hour), 0)
	peer := armPeer(w, primary, st, 50, "gern")
	peer.ArmedTowardViewer = false // partner hasn't Engaged yet

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.AutoWarp != nil {
		t.Error("coast started before the partner Engaged (solo drift)")
	}
}

// Both armed toward each other, same subspace → the shared Auto-Warp to
// the committed τ engages and unpauses.
func TestDriveRendezvousWarpStartsOnMutual(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	w.Clock.Paused = true
	peer := armPeer(w, primary, st, 50, "gern") // ArmedTowardViewer = true

	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.AutoWarp == nil || !w.AutoWarp.Rendezvous {
		t.Fatalf("shared coast not engaged on mutual arm: %+v", w.AutoWarp)
	}
	if !w.AutoWarp.T.Equal(tau) {
		t.Errorf("coast target = %v, want committed τ %v", w.AutoWarp.T, tau)
	}
	if w.Clock.Paused {
		t.Error("engaging the coast did not unpause")
	}
}

// Partner retracts (cancels) mid-coast → both release: the arm clears and
// the Auto-Warp drops.
func TestDriveRendezvousWarpCancelsOnRetract(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer}) // engaged
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast should be engaged")
	}

	peer.ArmedTowardViewer = false // partner retracted
	w.DriveRendezvousWarp([]CoWarpPeer{peer})
	if w.rendezvousWarpEngaged() {
		t.Error("coast survived the partner's retract")
	}
	if w.RendezvousArm != nil {
		t.Error("arm survived the partner's retract (both should release)")
	}
}

// Cancel from the viewer's side clears the arm and releases the coast
// without touching Selected Warp.
func TestDisengageRendezvousWarp(t *testing.T) {
	w, primary, st := anchorWorld(t)
	w.Clock.WarpIdx = 3
	w.EngageRendezvousWarp("SHA256:gern", "gern", st.Add(72*time.Hour), 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	w.DisengageRendezvousWarp()
	if w.RendezvousArm != nil || w.rendezvousWarpEngaged() {
		t.Error("cancel left arm/coast engaged")
	}
	if w.Clock.WarpIdx != 3 {
		t.Errorf("cancel touched Selected Warp: WarpIdx = %d, want 3", w.Clock.WarpIdx)
	}
}

// Arrival at τ hands off: drop to 1×, clear the arm (proximity co-warp
// takes over), and record the arrival for the S2 chip.
func TestRendezvousWarpArrivalHandsOff(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(72 * time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	peer := armPeer(w, primary, st, 50, "gern")
	w.DriveRendezvousWarp([]CoWarpPeer{peer})

	w.Clock.WarpIdx = 5
	w.Clock.SimTime = tau // reached the encounter
	w.resolveAutoWarp()

	if w.rendezvousWarpEngaged() {
		t.Error("coast still engaged past τ")
	}
	if w.RendezvousArm != nil {
		t.Error("arm not cleared at arrival (won't hand off to proximity)")
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("did not drop to 1× at arrival: WarpIdx = %d", w.Clock.WarpIdx)
	}
	if w.LastRendezvousArrival == nil || w.LastRendezvousArrival.Owner != "SHA256:gern" {
		t.Errorf("arrival not recorded for the chip: %+v", w.LastRendezvousArrival)
	}
}

// A mutually-armed pair must be able to raise the shared coast's rate.
//
// Regression for #248: engaging a rendezvous max-seeds the warp baseline,
// but mutual arms also couple the pair, and the couple applies min-wins
// over each partner's last *reported* Effective warp — which is their own
// post-clamp rate, always at least a tick stale. Two players who both
// engage at 1x therefore clamp each other to 1x and report 1x forever:
// min-wins can only ratchet down, so there is no path up and no
// tie-breaker. The coast then runs in real time (a 3 h encounter takes
// 3 h of wall clock).
func TestRendezvousCoastRaisesSharedRate(t *testing.T) {
	wa, _, sta := anchorWorld(t)
	wb, _, stb := anchorWorld(t)
	tau := sta.Add(3 * time.Hour)

	wa.EngageRendezvousWarp("SHA256:b", "b", tau, 0)
	wb.EngageRendezvousWarp("SHA256:a", "a", tau, 0)

	peer := func(owner, handle string, st time.Time, eff float64) []CoWarpPeer {
		return []CoWarpPeer{{
			Owner: owner, Handle: handle, SubspaceTime: st,
			EffWarp: eff, ArmedTowardViewer: true, RendezvousTau: tau,
		}}
	}

	// Each tick every side sees the other's PREVIOUS tick's reported
	// Effective warp — the staleness relay.Report actually has.
	effA, effB := wa.EffectiveWarp(), wb.EffectiveWarp()
	prevA, prevB := map[string]bool{}, map[string]bool{}
	for tick := 0; tick < 8; tick++ {
		pa := peer("SHA256:b", "b", stb, effB)
		pb := peer("SHA256:a", "a", sta, effA)
		wa.DriveRendezvousWarp(pa)
		wb.DriveRendezvousWarp(pb)
		ra := wa.ComputeCoWarp(pa, prevA)
		rb := wb.ComputeCoWarp(pb, prevB)
		wa.CoWarp, wb.CoWarp = ra.State, rb.State
		prevA, prevB = ra.CoupledOwners, rb.CoupledOwners
		effA, effB = wa.EffectiveWarp(), wb.EffectiveWarp()
	}

	if !wa.rendezvousWarpEngaged() || !wb.rendezvousWarpEngaged() {
		t.Fatal("precondition: the shared coast never engaged")
	}
	if !wa.CoWarp.Coupled {
		t.Fatal("precondition: mutual arms did not couple the pair")
	}
	if effA <= 1 || effB <= 1 {
		t.Errorf("shared coast pinned at its starting rate (effA=%v effB=%v): "+
			"min-wins over a stale report cannot ratchet up, so a %v encounter "+
			"runs in real time", effA, effB, tau.Sub(sta))
	}
}
