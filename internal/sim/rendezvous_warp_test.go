package sim

import (
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
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

// Reaching τ INSIDE the proximity couple gate ends the standing intent
// (#252): drop to 1×, clear the arm (Proximity Co-Warp takes over via the
// wasCoupled hysteresis — no drop-and-recouple tick), and record the
// arrival for the S2 chip. The resolution lives in driveRendezvousCoast,
// which has the peer set the couple-range check needs.
func TestRendezvousCoupleRangeHandoffAtTau(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Hour)
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	near := peerAt(w, primary, st, 50, orbital.Vec3{X: 5000}, orbital.Vec3{}, "gern")
	near.ArmedTowardViewer = true
	near.RendezvousTau = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}
	// Hysteresis memory from the coasting ticks (the rendezvous branch
	// coupled the pair all along).
	prev := w.ComputeCoWarp([]CoWarpPeer{near}, nil).CoupledOwners
	if !prev["SHA256:gern"] {
		t.Fatal("precondition: pair coupled during the coast")
	}

	w.Clock.WarpIdx = 5
	w.Clock.SimTime = tau // reached the encounter, 5 km out
	near.SubspaceTime = tau
	w.DriveRendezvousWarp([]CoWarpPeer{near})

	if w.rendezvousWarpEngaged() {
		t.Error("coast still engaged after the proximity handoff")
	}
	// ADR 0037 §1 demotes the agreement here rather than ending it: the
	// driver still goes and the ship is still handed back, but the mutual
	// intent survives into the terminal phase.
	if w.RendezvousArm == nil || !w.RendezvousArm.Approach {
		t.Errorf("arm not demoted to the approach phase at the proximity handoff: %+v", w.RendezvousArm)
	}
	if w.Clock.WarpIdx != 0 {
		t.Errorf("did not drop to 1× at arrival: WarpIdx = %d", w.Clock.WarpIdx)
	}
	if w.LastRendezvousArrival == nil || w.LastRendezvousArrival.Owner != "SHA256:gern" {
		t.Errorf("arrival not recorded for the chip: %+v", w.LastRendezvousArrival)
	}
	// Same tick, no drop-and-recouple: the proximity branch continues the
	// couple on the hysteresis memory even with the arm gone.
	near.ArmedTowardViewer = false
	res := w.ComputeCoWarp([]CoWarpPeer{near}, prev)
	if !res.State.Coupled {
		t.Error("pair decoupled on the handoff tick (drop-and-recouple)")
	}
	if len(res.Released) != 0 {
		t.Errorf("spurious release on the handoff tick: %v", res.Released)
	}
}

// aheadAlongTrack moves w's active craft ~735 s ahead along its own orbit
// and stretches its speed by 0.2% (slightly longer period), so against an
// unmoved twin world the pair is ~5,400 km apart NOW — far outside the
// couple gate — with a genuine FUTURE closest approach inside the commit
// horizon (the ahead craft slowly falls back toward the other). A plain
// radial offset won't do: its closest approach is at t=0 and a waypoint
// could never be derived.
func aheadAlongTrack(t *testing.T, w *World) {
	t.Helper()
	c := w.ActiveCraft()
	mu := c.Primary.GravitationalParameter()
	st, ok := physics.KeplerStep(physics.StateVector{R: c.State.R, V: c.State.V, M: 1}, mu, 735)
	if !ok {
		t.Fatal("kepler step failed placing the along-track partner")
	}
	c.State.R, c.State.V = st.R, st.V.Scale(1.002)
}

// crossPeer builds the peer entry world `from` presents to the other
// world's viewer: from's live craft state and subspace time, a (stale by
// one exchange) Effective warp, and from's outgoing arm relayed as
// ArmedTowardViewer + RendezvousTau/CA — the two-world cross-feed the
// relay actually performs.
func crossPeer(from *World, owner, handle string, eff float64) []CoWarpPeer {
	c := from.ActiveCraft()
	p := CoWarpPeer{
		Owner: owner, Handle: handle, SubspaceTime: from.Clock.SimTime,
		EffWarp: eff, ArmedTowardViewer: from.RendezvousArm != nil,
		Crafts: []CoWarpCraft{{Primary: c.Primary.ID, R: c.State.R, V: c.State.V}},
	}
	if arm := from.RendezvousArm; arm != nil {
		p.RendezvousTau, p.RendezvousCA = arm.Tau, arm.CommittedCA
	}
	return []CoWarpPeer{p}
}

// The arm is a standing mutual intent (#252): reaching the committed τ far
// outside couple range must NOT clear it — the waypoint advances to a newly
// derived encounter, the pair stays coupled and rate-locked, the advance is
// recorded for the chip (a silent advance reads as broken), and both sides
// converge on the same next τ without a negotiation loop.
func TestRendezvousArmSurvivesTauOutsideCoupleRange(t *testing.T) {
	wa, _, sta := anchorWorld(t)
	wb, _, _ := anchorWorld(t)
	// The playtest shape: thousands of km at τ, far outside the 35 km gate.
	aheadAlongTrack(t, wb)
	tau := sta.Add(time.Hour)
	wa.EngageRendezvousWarp("SHA256:b", "b", tau, 5.4e6)
	wb.EngageRendezvousWarp("SHA256:a", "a", tau, 5.4e6)

	effA, effB := wa.EffectiveWarp(), wb.EffectiveWarp()
	prevA, prevB := map[string]bool{}, map[string]bool{}
	step := func() {
		pa := crossPeer(wb, "SHA256:b", "b", effB)
		pb := crossPeer(wa, "SHA256:a", "a", effA)
		wa.DriveRendezvousWarp(pa)
		wb.DriveRendezvousWarp(pb)
		ra, rb := wa.ComputeCoWarp(pa, prevA), wb.ComputeCoWarp(pb, prevB)
		wa.CoWarp, wb.CoWarp = ra.State, rb.State
		prevA, prevB = ra.CoupledOwners, rb.CoupledOwners
		effA, effB = wa.EffectiveWarp(), wb.EffectiveWarp()
	}
	step()
	if !wa.rendezvousWarpEngaged() || !wb.rendezvousWarpEngaged() {
		t.Fatal("precondition: shared coast engaged on both sides")
	}

	// Reach the committed encounter 5,600 km apart.
	wa.Clock.SimTime, wb.Clock.SimTime = tau, tau
	step()

	if wa.RendezvousArm == nil || wb.RendezvousArm == nil {
		t.Fatal("arm cleared at τ outside couple range — the standing intent must survive the waypoint")
	}
	if !wa.rendezvousWarpEngaged() || !wb.rendezvousWarpEngaged() {
		t.Fatal("coast released at τ outside couple range")
	}
	if !wa.RendezvousArm.Tau.After(tau) || !wb.RendezvousArm.Tau.After(tau) {
		t.Errorf("waypoint did not advance past the reached τ: a=%v b=%v (committed %v)",
			wa.RendezvousArm.Tau, wb.RendezvousArm.Tau, tau)
	}
	if wa.LastRendezvousWaypoint == nil {
		t.Error("no waypoint feedback recorded (a silent advance reads as broken)")
	}
	if !wa.CoWarp.Coupled || !wb.CoWarp.Coupled {
		t.Fatal("pair decoupled across the waypoint advance")
	}

	// The coast keeps resolving a shared rate above 1× (the #248 exemption
	// carries across the waypoint).
	step()
	if effA <= 1 || effB <= 1 {
		t.Errorf("coast not rate-locked above 1× past the waypoint: effA=%v effB=%v", effA, effB)
	}
	// τ authority: both sides re-derived independently; they must converge
	// on the same waypoint via the deterministic min-future-τ rule.
	step()
	step()
	if !wa.RendezvousArm.Tau.Equal(wb.RendezvousArm.Tau) {
		t.Errorf("waypoint τ did not converge: a=%v b=%v",
			wa.RendezvousArm.Tau, wb.RendezvousArm.Tau)
	}
}

// A waypoint advance re-bases the degrade baseline (#251 interaction):
// without the reset, the new waypoint's approach is measured against the
// PREVIOUS waypoint's coast-start baseline and instantly reads degraded.
func TestRendezvousWaypointAdvanceRebasesDegrade(t *testing.T) {
	w, primary, st := anchorWorld(t)
	tau := st.Add(time.Minute)
	a := w.ActiveCraft()
	peer := func(r, v orbital.Vec3, at time.Time) []CoWarpPeer {
		return []CoWarpPeer{{
			Owner: "SHA256:gern", Handle: "gern", SubspaceTime: at, EffWarp: 50,
			ArmedTowardViewer: true, RendezvousTau: tau,
			Crafts:            []CoWarpCraft{{Primary: primary, R: r, V: v}},
		}}
	}

	// Coast starts against a coincident partner: baseline ≈ 0.
	w.EngageRendezvousWarp("SHA256:gern", "gern", tau, 0)
	w.DriveRendezvousWarp(peer(a.State.R, a.State.V, st))
	if !w.rendezvousWarpEngaged() {
		t.Fatal("precondition: coast engaged")
	}
	if !w.RendezvousArm.degradeBaseSet {
		t.Fatal("precondition: degrade baseline captured at coast start")
	}

	// τ reached with the partner now thousands of km out along-track (a
	// future closest approach exists) → waypoint advances; the new
	// waypoint's approach must become the NEW baseline, not a huge "drift"
	// against the old ≈0 one.
	mu := a.Primary.GravitationalParameter()
	ahead, ok := physics.KeplerStep(physics.StateVector{R: a.State.R, V: a.State.V, M: 1}, mu, 735)
	if !ok {
		t.Fatal("kepler step failed placing the far partner")
	}
	w.Clock.SimTime = tau
	w.DriveRendezvousWarp(peer(ahead.R, ahead.V.Scale(1.002), tau))
	arm := w.RendezvousArm
	if arm == nil || !arm.Tau.After(tau) {
		t.Fatalf("waypoint did not advance (arm=%+v)", arm)
	}
	if w.RendezvousDegraded {
		t.Error("advance read as a degraded encounter — baseline not re-based")
	}
	if arm.degradeBaseSet && arm.degradeBaseCA <= coWarpCoupleRangeM {
		t.Errorf("degradeBaseCA = %v, want re-based to the new waypoint's approach (> couple range)",
			arm.degradeBaseCA)
	}
}

// A rendezvous flown as several maneuvers (waypoint after waypoint) keeps
// both subspace clocks inside the same-subspace tolerance throughout, with
// no Sync between maneuvers (#252 acceptance).
func TestRendezvousMultiManeuverHoldsSubspaceTolerance(t *testing.T) {
	wa, _, sta := anchorWorld(t)
	wb, _, _ := anchorWorld(t)
	aheadAlongTrack(t, wb)
	tau := sta.Add(30 * time.Minute)
	wa.EngageRendezvousWarp("SHA256:b", "b", tau, 5.4e6)
	wb.EngageRendezvousWarp("SHA256:a", "a", tau, 5.4e6)

	effA, effB := wa.EffectiveWarp(), wb.EffectiveWarp()
	prevA, prevB := map[string]bool{}, map[string]bool{}
	advances := 0
	for guard := 0; guard < 30000 && advances < 3; guard++ {
		pa := crossPeer(wb, "SHA256:b", "b", effB)
		pb := crossPeer(wa, "SHA256:a", "a", effA)
		wa.DriveRendezvousWarp(pa)
		wb.DriveRendezvousWarp(pb)
		ra, rb := wa.ComputeCoWarp(pa, prevA), wb.ComputeCoWarp(pb, prevB)
		wa.CoWarp, wb.CoWarp = ra.State, rb.State
		prevA, prevB = ra.CoupledOwners, rb.CoupledOwners
		effA, effB = wa.EffectiveWarp(), wb.EffectiveWarp()

		if wa.LastRendezvousWaypoint != nil || wb.LastRendezvousWaypoint != nil {
			advances++
			wa.LastRendezvousWaypoint, wb.LastRendezvousWaypoint = nil, nil
		}
		if wa.RendezvousArm == nil || wb.RendezvousArm == nil {
			t.Fatalf("standing intent dropped mid-sequence after %d advances", advances)
		}

		// The tick loop's job, emulated: each side advances at its own
		// resolved rate.
		wa.Clock.SimTime = wa.Clock.SimTime.Add(time.Duration(effA * float64(wa.Clock.BaseStep)))
		wb.Clock.SimTime = wb.Clock.SimTime.Add(time.Duration(effB * float64(wb.Clock.BaseStep)))
		if d := wa.Clock.SimTime.Sub(wb.Clock.SimTime); d > CoWarpSubspaceTolerance || -d > CoWarpSubspaceTolerance {
			t.Fatalf("subspace clocks diverged past tolerance mid-sequence: Δt=%v after %d advances", d, advances)
		}
	}
	if advances < 3 {
		t.Fatalf("only %d waypoint advances inside the tick budget — the coast is not sequencing maneuvers", advances)
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
