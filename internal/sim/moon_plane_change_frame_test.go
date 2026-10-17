package sim

import (
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Playtest 2026-10-17 (v0.57.0): "hohmann transfer to moon still seems to
// be missing the initial encounter". These tests are RED on purpose until
// the fixes land; see the GitHub issues.
//
// Since #674 the [H] departure flies to the planned orbit, and every
// lunar-capable vessel enters the Moon's SOI (50 of 50 flights, 7 vessels,
// 7 epochs). What goes wrong is the split's plane change, the burn
// between the departure and the capture:
//
//  1. It is planted centred on the predicted SOI entry (splitCaptureAim
//     models it as an impulse at exactly that instant, in Earth's frame).
//     The departure is now accurate enough that the vessel crosses into
//     the Moon's SOI within seconds of ignition, so the burn runs in the
//     Moon's frame (40 of 50 flights, 6 more straddle the boundary). There
//     it rotates the Moon-relative velocity, a different burn altogether.
//  2. When it does run in Earth's frame, the executor re-aims its chord
//     direction every tick from the current velocity. The split's plane
//     change turns a ~200 m/s velocity through 57 degrees with ~180 m/s,
//     and re-aimed it bleeds the speed instead (209 -> 136 m/s; held at
//     the ignition direction it keeps 209, which is what the planner and
//     the HUD model).
//
// Either way the flown perilune is not the HUD's, and the capture node
// (timed for the HUD's perilune) fires 1.0 to 2.2 h after the vessel has
// already swung past the Moon. Outcome over 50 flights: 20 hit the Moon
// before the capture, 24 capture into an orbit whose periapsis is below
// the surface, 6 end in a clear orbit.

// splitPCFlight is one [H] Moon transfer flown with G the way a player
// does, through the plane change and on into the Moon's SOI.
type splitPCFlight struct {
	planned     bool
	pcPlanned   string  // PrimaryID the plane change was planted in
	pcBurnFrame string  // frame the vessel was in when the plane change ignited
	pcEndFrame  string  // frame when the plane change ended
	pcVPre      float64 // speed just before the plane change (m/s, ignition frame)
	pcVPost     float64 // speed just after it (m/s, same frame when pcEndFrame matches)
	pcModelDir  orbital.Vec3
	pcAchieved  orbital.Vec3 // flown delta-v with the gravity-only drift removed
	pcDV        float64
	hudPeKm     float64 // Moon perilune altitude the HUD's planted chain shows after the departure
	flownPeKm   float64 // Moon perilune altitude of the flown path, read at SOI entry after the plane change
	pcEndAt     time.Time // sim time the plane change ended
	entryAt     time.Time // sim time the vessel crossed into the Moon's SOI
}

func flySplitThroughPlaneChange(t *testing.T, w *World) splitPCFlight {
	t.Helper()
	var f splitPCFlight
	c := w.ActiveCraft()
	mi := moonIndex(w)
	w.SetTargetBody(mi)
	if _, err := w.PlanTransfer(mi); err != nil {
		t.Fatalf("PlanTransfer: %v", err)
	}
	if w.LastTransfer.Strategy != "split" {
		return f
	}
	pcIdx := -1
	for i, n := range c.Nodes {
		if n.Mode == spacecraft.BurnPlaneChange {
			pcIdx = i
		}
	}
	if pcIdx < 0 {
		return f
	}
	f.planned = true
	f.pcPlanned = c.Nodes[pcIdx].PrimaryID
	toNextIgnition := func() spacecraft.ManeuverNode {
		next := c.Nodes[0]
		if !w.EngageAutoWarp() {
			t.Fatal("G refused to engage on a planted burn")
		}
		tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil })
		n := len(c.Nodes)
		tickUntil(w, 5_000_000, func() bool { return len(c.Nodes) < n })
		return next
	}
	finishBurn := func() {
		for k := 0; k < 3; k++ {
			tickUntil(w, 5_000_000, func() bool { return c.ActiveBurn == nil || c.BurnStalled() })
			if !c.BurnStalled() {
				return
			}
			if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
				t.Fatalf("stage: %v", err)
			}
		}
	}
	toNextIgnition()
	finishBurn()
	f.hudPeKm = hudMoonPerilune(w)

	// The vessel is at ignition: the node popped this tick, so the state
	// is (to one tick) the pre-burn state.
	pc := toNextIgnition()
	f.pcBurnFrame = c.Primary.ID
	pre := c.State
	start := w.Clock.SimTime
	f.pcDV = pc.DV
	f.pcModelDir = spacecraft.NodeBurnDirection(pc, pre.R, pre.V)
	f.pcVPre = pre.V.Norm()
	finishBurn()
	f.pcEndFrame = c.Primary.ID
	f.pcEndAt = w.Clock.SimTime
	f.pcVPost = c.State.V.Norm()
	if f.pcEndFrame == f.pcBurnFrame {
		drift, ok := physics.KeplerStep(pre, c.Primary.GravitationalParameter(), w.Clock.SimTime.Sub(start).Seconds())
		if ok {
			f.pcAchieved = c.State.V.Sub(drift.V)
		}
	}
	tickUntil(w, 5_000_000, func() bool { return c.Primary.ID == "moon" })
	f.entryAt = w.Clock.SimTime
	if c.Primary.ID != "moon" {
		f.flownPeKm = math.Inf(1)
		return f
	}
	el := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
	f.flownPeKm = (el.Periapsis() - c.Primary.RadiusMeters()) / 1e3
	return f
}

// hudMoonPerilune walks the planted chain (what the HUD reads) and returns
// the Moon-frame periapsis altitude (km) of the coast that arrives at the
// capture node, before the capture burn. NaN when the chain has no Moon
// arrival.
func hudMoonPerilune(w *World) float64 {
	pe := math.NaN()
	var prev *planChainStep
	w.walkPlantedChain(func(st planChainStep) bool {
		if st.Node.PrimaryID == "moon" && prev != nil && !st.Missed {
			s, p := w.propagateStateWithPrimary(prev.State, prev.Primary, prev.Clock, st.Clock.Sub(prev.Clock).Seconds())
			if p.ID == "moon" {
				el := orbital.ElementsFromState(s.R, s.V, p.GravitationalParameter())
				pe = (el.Periapsis() - p.RadiusMeters()) / 1e3
			}
			return false
		}
		cp := st
		prev = &cp
		return true
	})
	return pe
}

// moonTransferEpochs are the default vessel's starts: a fresh game (the
// lunar window) and the clock advanced by whole days.
func moonTransferEpochs() []struct {
	name  string
	setup func(w *World)
} {
	eps := []struct {
		name  string
		setup func(w *World)
	}{{"fresh game", func(w *World) { w.AdjustStartForLunarTransferWindow(DefaultLunarTransferLead) }}}
	for _, d := range []int{0, 3, 7, 11, 15, 19, 23} {
		d := d
		eps = append(eps, struct {
			name  string
			setup func(w *World)
		}{fmt.Sprintf("+%dd", d), func(w *World) { w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(d) * 24 * time.Hour) }})
	}
	return eps
}

// TestSplitPlaneChangeBurnsInTheFrameItWasPlannedFor: the split's plane
// change is planned, and its aim measured, in Earth's frame, so it must
// burn there, start to finish. Measured at v0.57.0 on the default Apollo
// TLI-1: 6 of 8 starts ignite it inside the Moon's SOI and 1 more crosses
// in mid-burn.
func TestSplitPlaneChangeBurnsInTheFrameItWasPlannedFor(t *testing.T) {
	for _, ep := range moonTransferEpochs() {
		w := mustWorld(t)
		ep.setup(w)
		f := flySplitThroughPlaneChange(t, w)
		if !f.planned {
			t.Logf("%s: [H] did not plant a split plane change; nothing to check", ep.name)
			continue
		}
		t.Logf("%s: plane change planted in %s, ignited in %s, ended in %s", ep.name, f.pcPlanned, f.pcBurnFrame, f.pcEndFrame)
		if f.pcBurnFrame != f.pcPlanned || f.pcEndFrame != f.pcPlanned {
			t.Errorf("%s: the plane change planted in %s's frame burned in %s's frame (ended in %s's)",
				ep.name, f.pcPlanned, f.pcBurnFrame, f.pcEndFrame)
		}
	}
}

// TestSplitPlaneChangeEndsWellBeforeSOIEntry: the plane change finishes a
// real margin (about PlaneChangeMargin, 15 min; the test asks for 7) before the
// vessel crosses into the Moon's SOI, not on the boundary: a burn that ends
// on the boundary is one tick or one ephemeris wobble from running across it
// (#701).
func TestSplitPlaneChangeEndsWellBeforeSOIEntry(t *testing.T) {
	for _, ep := range moonTransferEpochs() {
		w := mustWorld(t)
		ep.setup(w)
		f := flySplitThroughPlaneChange(t, w)
		if !f.planned || f.entryAt.IsZero() || f.pcEndAt.IsZero() {
			continue
		}
		gap := f.entryAt.Sub(f.pcEndAt)
		t.Logf("%s: plane change ended %v before the SOI entry", ep.name, gap.Round(time.Second))
		if gap < 7*time.Minute {
			t.Errorf("%s: the plane change ended %v before the SOI entry, want at least 7m", ep.name, gap.Round(time.Second))
		}
	}
}

// TestPlaneChangeBurnTurnsTheVelocityInsteadOfBleedingIt: a flown plane
// change delivers the planted delta-v along the direction the node gives
// at ignition (the impulse the planner and the HUD model) and so keeps
// the speed. Measured at v0.57.0 on the default vessel's starts where it
// burns wholly in Earth's frame (+3d): 57 deg plane change, |v| 208.8 ->
// 136.0 m/s, achieved delta-v 28.6 deg off the planted direction, because
// the executor re-aims the chord at the current velocity every tick
// (SimulateFiniteBurn re-aimed: 135.6; held fixed: 208.7).
func TestPlaneChangeBurnTurnsTheVelocityInsteadOfBleedingIt(t *testing.T) {
	checked := 0
	for _, ep := range moonTransferEpochs() {
		w := mustWorld(t)
		ep.setup(w)
		f := flySplitThroughPlaneChange(t, w)
		if !f.planned || f.pcBurnFrame != f.pcEndFrame || f.pcAchieved.Norm() == 0 {
			continue // crossed a frame mid-burn: the frame test owns that
		}
		checked++
		ang := math.Acos(math.Max(-1, math.Min(1, f.pcAchieved.Unit().Dot(f.pcModelDir.Unit())))) * 180 / math.Pi
		t.Logf("%s (%s frame): |v| %.1f -> %.1f m/s, achieved delta-v %.1f of %.1f m/s, %.1f deg off the planted direction",
			ep.name, f.pcBurnFrame, f.pcVPre, f.pcVPost, f.pcAchieved.Norm(), f.pcDV, ang)
		if math.Abs(f.pcVPost-f.pcVPre) > 0.02*f.pcVPre || ang > 3 {
			t.Errorf("%s: the plane change changed the speed %.1f -> %.1f m/s and pushed %.1f deg off its planted direction; a plane change turns the velocity, it does not slow it",
				ep.name, f.pcVPre, f.pcVPost, ang)
		}
	}
	if checked == 0 {
		t.Fatal("no start flew its plane change within one frame: nothing measured")
	}
}

// TestFlownMoonPeriluneIsTheOneTheHUDShows: after the departure the HUD's
// planted chain shows the Moon perilune the plan will fly (about the 200
// km capture orbit). Flying the plane change must deliver that encounter,
// within 500 km. Measured at v0.57.0 on the default vessel: HUD 321 to 2396
// km, flown -928 to +1868 km (7 of 8 starts more than 500 km off, 4 of them
// into the surface).
func TestFlownMoonPeriluneIsTheOneTheHUDShows(t *testing.T) {
	for _, ep := range moonTransferEpochs() {
		w := mustWorld(t)
		ep.setup(w)
		f := flySplitThroughPlaneChange(t, w)
		if !f.planned {
			continue
		}
		t.Logf("%s: HUD Moon perilune %.0f km, flown %.0f km (plane change burned in %s -> %s)",
			ep.name, f.hudPeKm, f.flownPeKm, f.pcBurnFrame, f.pcEndFrame)
		if math.IsNaN(f.hudPeKm) {
			t.Errorf("%s: the HUD shows no Moon perilune after the departure", ep.name)
			continue
		}
		if math.Abs(f.flownPeKm-f.hudPeKm) > 500 {
			t.Errorf("%s: the HUD promised a Moon perilune of %.0f km; the flown path's is %.0f km", ep.name, f.hudPeKm, f.flownPeKm)
		}
	}
}

// TestHMoonTransferEndsInAClearLunarOrbit is the player's check: target
// the Moon, [H], fly every burn with G (staging when a burn stalls) and
// end in a bound lunar orbit clear of the surface, with the capture
// burned at the perilune (within 15 minutes of it). Measured at v0.57.0 on
// the default vessel: 4 of 8 starts hit the Moon before the capture, 2
// capture into an orbit with its periapsis 331 and 683 km below the
// surface, and the 2 that end clear (Pe 414, 440 km, e 0.77) burned the
// capture 1.17 h after the perilune; every capture ignites 1.2 to 2.2 h
// late.
func TestHMoonTransferEndsInAClearLunarOrbit(t *testing.T) {
	for _, ep := range moonTransferEpochs() {
		w := mustWorld(t)
		ep.setup(w)
		c := w.ActiveCraft()
		mi := moonIndex(w)
		w.SetTargetBody(mi)
		if _, err := w.PlanTransfer(mi); err != nil {
			t.Fatalf("%s: PlanTransfer: %v", ep.name, err)
		}
		lateH := math.NaN()
		for leg := 0; leg < 6 && len(c.Nodes) > 0 && !c.Crashed; leg++ {
			next := c.Nodes[0]
			if !w.EngageAutoWarp() {
				break
			}
			tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil || c.Crashed })
			n := len(c.Nodes)
			tickUntil(w, 5_000_000, func() bool { return len(c.Nodes) < n || c.Crashed })
			if c.Crashed {
				break
			}
			if next.PrimaryID == "moon" && c.Primary.ID == "moon" {
				if tp, ok := orbital.TimeToPeriapsisHyperbolic(orbital.Vec3State{R: c.State.R, V: c.State.V}, c.Primary.GravitationalParameter()); ok {
					lateH = -tp / 3600
				}
			}
			for k := 0; k < 3; k++ {
				tickUntil(w, 5_000_000, func() bool { return c.ActiveBurn == nil || c.BurnStalled() || c.Crashed })
				if !c.BurnStalled() {
					break
				}
				if _, _, err := w.StageActive(w.ActiveCraftIdx); err != nil {
					break
				}
			}
			if next.Mode == spacecraft.BurnPlaneChange {
				tickUntil(w, 5_000_000, func() bool { return c.Primary.ID == "moon" || c.Crashed })
			}
		}
		switch {
		case c.Crashed:
			t.Errorf("%s: the vessel hit the Moon before the capture burn", ep.name)
		case c.Primary.ID != "moon":
			t.Errorf("%s: the flight ended in %s's frame, not the Moon's", ep.name, c.Primary.ID)
		default:
			el := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
			peKm := (el.Periapsis() - c.Primary.RadiusMeters()) / 1e3
			t.Logf("%s: captured e %.3f, Pe alt %.0f km; capture ignited %.2f h after the flown perilune", ep.name, el.E, peKm, lateH)
			if el.E >= 1 || peKm <= 0 {
				t.Errorf("%s: lunar orbit e %.3f with periapsis %.0f km altitude: not a clear capture (capture burned %.2f h after the perilune)",
					ep.name, el.E, peKm, lateH)
			}
			if !(math.Abs(lateH) <= 0.25) {
				t.Errorf("%s: the capture burn ignited %+.2f h from the flown perilune (+ is after it, the vessel already past the Moon)", ep.name, lateH)
			}
		}
	}
}

// postBurnImpulsive is PostBurnState with the burn applied as the ideal
// impulse at the node: the geometry tests (plane-match node line, circular
// plane change, plane-match then Hohmann) check where the planner puts the
// burn and how it sizes it, not what a long finite burn held on one
// direction leaves of it (#702: that is the flown, and now the predicted,
// burn, and is pinned by the held-direction tests).
func postBurnImpulsive(w *World, n spacecraft.ManeuverNode) (physics.StateVector, string) {
	n.Duration = 0
	return w.PostBurnState(n)
}
