package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Cross-player capture and bump (ADR 0053 decision 9 and decision 8's
// cross-player paragraph, slice E). A friend's vessel is a Ghost: a Kepler
// state plus the hull it last reported. The approacher runs the SAME port
// gate (dockGateHolds) and the SAME five-second magnet against the ghost's
// reported port, in its own World, and moves only itself: the ghost is
// immovable and its owner is not asked. The dock itself is the ledger Claim
// the serve layer files when the pull completes (relay.DetectGuestContact).
//
// Everything here is driven by the serve layer once per tick, AFTER the
// ghost slate is refreshed, never from World.Tick: a ghost is evaluated at
// the viewer's sim-time at the end of the previous tick, and a port gap
// measured inside Tick would carry a whole tick of orbital motion (hundreds
// of metres) that is not there.

// GhostRef names a ghost: its owner's fingerprint and the remote craft ID.
// A ghost's craft ID is the OWNER's counter, so it can collide with a local
// vessel's; the pair is the key.
type GhostRef struct {
	Owner   string
	CraftID uint64
}

// GhostPortGeometry is a ghost's hull and ports as it reported them. Known
// is false for a ghost with no reported attitude or hull (an older report):
// no axis, no port, no capture.
func GhostPortGeometry(g Ghost) PortGeometry {
	pg := PortGeometry{
		Axis:    g.Attitude.Unit(),
		NoseM:   g.NoseM,
		WidthM:  g.WidthM,
		TopName: g.Name,
	}
	pg.Known = g.Attitude.Norm() > 1e-9 && g.NoseM > 0
	if !pg.Known {
		return pg
	}
	for _, p := range g.Ports {
		pg.Ports = append(pg.Ports, DockPort{Name: p.Name, End: "top", OffsetM: p.OffsetM})
	}
	return pg
}

// ghostPairGate is dockPairGate for the active vessel against a ghost: the
// three gate readings, with hasPort false when either side lacks a port or a
// known shape.
func ghostPairGate(a *spacecraft.Spacecraft, g Ghost) (hasPort bool, portRangeM, relSpeedMS, alignDeg float64) {
	ga, gg := VesselPortGeometry(a), GhostPortGeometry(g)
	relSpeedMS = a.State.V.Sub(g.Vel).Norm()
	if !ga.HasPort() || !gg.HasPort() || !ga.Known || !gg.Known {
		return false, 0, relSpeedMS, 0
	}
	pa := ga.PortPoint(a.State.R, 0)
	pg := gg.PortPoint(g.RelPos, 0)
	return true, pg.Sub(pa).Norm(), relSpeedMS, PortAlignmentDeg(ga.Axis, gg.Axis)
}

// ghostPairGateHolds is dockGateHolds for the active vessel and a ghost.
func ghostPairGateHolds(a *spacecraft.Spacecraft, g Ghost) bool {
	return dockGateHolds(ghostPairGate(a, g))
}

// GhostPullGate decides, for the viewer's seat, who of a pair runs the
// cross-player magnet (the approacher rule, relay/dock_approach.go). The serve
// layer installs one per tick; with none installed (a lone World, unit tests)
// every seat may pull, the pre-rule behaviour.
type GhostPullGate interface {
	// Approacher: the rule names this seat's vessel the approacher for the ghost.
	Approacher(GhostRef) bool
	// BeginPull registers (or refreshes) this seat as the pair's one puller;
	// false when the other seat holds the pair or the rule names it.
	BeginPull(GhostRef) bool
	// EndPull releases the registration.
	EndPull(GhostRef)
	// PullingToward: the ghost's seat is the live puller toward us.
	PullingToward(GhostRef) bool
}

// SetGhostPullGate installs the approacher rule for this tick (nil clears it).
func (w *World) SetGhostPullGate(g GhostPullGate) { w.ghostDock.gate = g }

// dropGhostMagnet ends any running pull and releases its registration.
func (w *World) dropGhostMagnet() {
	gd := &w.ghostDock
	if gd.magnet != nil && gd.gate != nil {
		gd.gate.EndPull(gd.magnet.ghost)
	}
	gd.magnet = nil
}

// ghostMagnet is a cross-player capture in progress. Transient, never saved.
type ghostMagnet struct {
	pullerID          uint64
	ghost             GhostRef
	startedAt, lastAt time.Time
}

// ghostDock is the World's cross-player contact state: the running pull, the
// completed-pull latch, the ledger's refusals and the bump latches.
type ghostDock struct {
	magnet *ghostMagnet
	// gate is the approacher rule, nil when none is installed.
	gate GhostPullGate
	// done is the ghost whose pull completed and whose Claim has been filed;
	// no new pull starts on it until the pair leaves the gate (a refused or
	// slow Claim must not loop the magnet).
	done *GhostRef
	// blocked is the set of ghosts the ledger would refuse a Claim against
	// (the re-arm latch, or one end already engaged). Written by the serve
	// layer each tick; the ring reads it so a latched pair is never green,
	// and the magnet does not start on one (the #372 rule, cross-player).
	blocked []GhostRef
	// refused is the blocked ghosts that held the gate this tick: the serve
	// layer files a Claim for each so the ledger arms its one-time notice.
	refused []GhostRef
	// bump latches the ghost hulls the active vessel is still overlapping.
	bump map[ghostBumpKey]bool
}

type ghostBumpKey struct {
	localID uint64
	ghost   GhostRef
}

// SetGhostDockBlocks records which ghosts the dock ledger would refuse this
// tick. See ghostDock.blocked.
func (w *World) SetGhostDockBlocks(refs []GhostRef) { w.ghostDock.blocked = refs }

// GhostGateRefusals returns the blocked ghosts that held the port gate on the
// last StepGhostCapture.
func (w *World) GhostGateRefusals() []GhostRef { return w.ghostDock.refused }

func (w *World) ghostBlocked(r GhostRef) bool {
	for _, b := range w.ghostDock.blocked {
		if b == r {
			return true
		}
	}
	return false
}

// GhostCapturing reports whether a magnet pull is running against this ghost
// (the chip's "capturing").
func (w *World) GhostCapturing(owner string, craftID uint64) bool {
	m := w.ghostDock.magnet
	return m != nil && m.ghost == (GhostRef{owner, craftID})
}

func (w *World) ghostByKey(r GhostRef) (Ghost, bool) { return w.ghostByRef(r.Owner, r.CraftID) }

// StepGhostCapture advances the cross-player capture one tick against the
// current ghost slate: a running pull is stepped (let go, keep pulling, or
// complete); otherwise the first eligible ghost that holds the port gate
// starts one. eligible, when non-nil, vets a ghost the caller will not claim
// (not co-warp coupled, say). done is true only on the tick a pull completes:
// the approacher's port is on the ghost's, and the caller files the Claim.
func (w *World) StepGhostCapture(eligible func(GhostRef) bool) (ref GhostRef, done bool) {
	gd := &w.ghostDock
	gd.refused = nil
	a := w.ActiveCraft()
	if a == nil || a.Landed || a.Crashed {
		w.dropGhostMagnet()
		gd.done = nil
		return GhostRef{}, false
	}
	if m := gd.magnet; m != nil {
		g, ok := w.ghostByKey(m.ghost)
		if !ok || m.pullerID != a.ID || !w.ghostPairEligible(a, g) || !ghostPairGateHolds(a, g) ||
			(eligible != nil && !eligible(m.ghost)) {
			w.dropGhostMagnet() // let go: both back in free flight, nothing claimed
			return GhostRef{}, false
		}
		// Refresh the pair's registration; if another seat has taken the pair
		// (a stale registration expired under us), this seat lets go too.
		if gd.gate != nil && !gd.gate.BeginPull(m.ghost) {
			gd.magnet = nil
			return GhostRef{}, false
		}
		return w.pullToGhost(a, g)
	}
	// A pull that completed stays latched until the pair leaves the gate.
	if gd.done != nil {
		if g, ok := w.ghostByKey(*gd.done); !ok || !ghostPairGateHolds(a, g) {
			gd.done = nil
		}
	}
	if w.dockMagnet != nil {
		return GhostRef{}, false // a local pull already has the vessel
	}
	for _, g := range w.Ghosts {
		r := GhostRef{g.Owner, g.CraftID}
		if !w.ghostPairEligible(a, g) || !ghostPairGateHolds(a, g) {
			continue
		}
		if gd.done != nil && *gd.done == r {
			continue
		}
		// Only the approacher pulls or files a Claim; the other seat is
		// hands-off and keeps its hold.
		if gd.gate != nil && !gd.gate.Approacher(r) {
			continue
		}
		if w.ghostBlocked(r) {
			gd.refused = append(gd.refused, r)
			continue
		}
		if eligible != nil && !eligible(r) {
			continue
		}
		if gd.gate != nil && !gd.gate.BeginPull(r) {
			continue
		}
		w.stampCraftID(a)
		now := w.Clock.SimTime
		gd.magnet = &ghostMagnet{pullerID: a.ID, ghost: r, startedAt: now, lastAt: now}
		return GhostRef{}, false
	}
	return GhostRef{}, false
}

// ghostPairEligible is the cross-player pair preconditions: the same SOI
// (a ghost carries no System of its own; the slate is already filtered to the
// viewer's), the active vessel flying.
func (w *World) ghostPairEligible(a *spacecraft.Spacecraft, g Ghost) bool {
	return a != nil && !a.Landed && !a.Crashed && g.PrimaryID == a.Primary.ID
}

// pullToGhost closes the port gap and turns the active vessel's nose onto the
// ghost's port axis, linear over DockMagnetDuration, exactly as stepDockMagnet
// does for a local pair; the ghost is not touched.
func (w *World) pullToGhost(a *spacecraft.Spacecraft, g Ghost) (GhostRef, bool) {
	m := w.ghostDock.magnet
	now := w.Clock.SimTime
	remaining := DockMagnetDuration - m.lastAt.Sub(m.startedAt)
	dt := now.Sub(m.lastAt)
	frac := 1.0
	if remaining > 0 && dt < remaining {
		frac = float64(dt) / float64(remaining)
	}
	if frac < 0 {
		frac = 0
	}
	m.lastAt = now
	// Turn first, then measure the gap: the port sits a stack length from the
	// position, so a turn after the measurement would move it again and leave
	// the vessel a few millimetres short of the port at completion.
	gg := GhostPortGeometry(g)
	a.CurrentAttitudeDir = turnToward(a.CurrentAttitudeDir, gg.Axis.Scale(-1), frac)
	ga := VesselPortGeometry(a)
	gap := gg.PortPoint(g.RelPos, 0).Sub(ga.PortPoint(a.State.R, 0))
	a.State.R = a.State.R.Add(gap.Scale(frac))
	if now.Sub(m.startedAt) < DockMagnetDuration {
		return GhostRef{}, false
	}
	ref := m.ghost
	w.dropGhostMagnet()
	w.ghostDock.done = &ref
	return ref, true
}

// ReleaseGhostCapture drops any cross-player pull and completed-pull latch:
// the viewer can no longer capture (it is a guest, or flies a stack that has
// one), so nothing is left half-done.
func (w *World) ReleaseGhostCapture() {
	w.dropGhostMagnet()
	w.ghostDock.done = nil
	w.ghostDock.refused = nil
}

// ghostMagnetPulls reports whether c is the vessel a cross-player pull has.
func (w *World) ghostMagnetPulls(c *spacecraft.Spacecraft) bool {
	return w.ghostDock.magnet != nil && c != nil && c.ID == w.ghostDock.magnet.pullerID
}

// ghostHullSegment is a ghost's hull as the axis segment from the tail of its
// bottom row to its nose, plus its width, from the reported shape.
func ghostHullSegment(g Ghost) (p0, p1 orbital.Vec3, width float64, ok bool) {
	pg := GhostPortGeometry(g)
	if !pg.Known {
		return orbital.Vec3{}, orbital.Vec3{}, 0, false
	}
	p0 = g.RelPos.Sub(pg.Axis.Scale(dockPortTipInsetM))
	p1 = g.RelPos.Add(pg.Axis.Scale(pg.NoseM - dockPortTipInsetM))
	return p0, p1, pg.WidthM, true
}

// CheckGhostBumps is checkBumps against the ghost slate: the active vessel
// whose hull touches a friend's reported hull outside the gate bounces off it.
// Only the active vessel is pushed, the ghost treated as immovable
// (dv = +n*(s + s')), and the bumped seat feels nothing and is not told
// (ADR 0053 "Settled here" item 2). Edge-triggered through ghostDock.bump.
func (w *World) CheckGhostBumps() {
	a := w.ActiveCraft()
	if a == nil || a.Landed || a.Crashed {
		return
	}
	for _, g := range w.Ghosts {
		if !w.ghostPairEligible(a, g) {
			continue
		}
		r := GhostRef{g.Owner, g.CraftID}
		key := ghostBumpKey{a.ID, r}
		dr := a.State.R.Sub(g.RelPos)
		// The 50 m zone is hull to hull (review finding 4), not anchor to
		// anchor: a friend's long stack's nose can be inside it from far off.
		if HullDistanceM(a.State.R, VesselPortGeometry(a), g.RelPos, GhostPortGeometry(g)) > DockingDistM {
			delete(w.ghostDock.bump, key)
			continue
		}
		a0, a1, wa, okA := hullSegment(a)
		g0, g1, wg, okG := ghostHullSegment(g)
		if !okA || !okG {
			continue
		}
		touch, nh := hullContact(a0, a1, wa, g0, g1, wg)
		if !touch {
			delete(w.ghostDock.bump, key)
			continue
		}
		if w.ghostDock.bump[key] || w.ghostBumpInGate(a, g, r) {
			continue
		}
		// A bump pushes only the bumper, and the bumper is the approacher:
		// the other seat's World sees the same overlap against the
		// approacher's ghost and must not kick itself as well.
		if w.ghostDock.gate != nil && !w.ghostDock.gate.Approacher(r) {
			continue
		}
		// No usable contact normal: fall back to the line between positions.
		if nh.Norm() == 0 {
			if dr.Norm() < 1e-9 {
				continue
			}
			nh = dr.Unit()
		}
		s := -a.State.V.Sub(g.Vel).Dot(nh)
		if s <= 0 {
			continue
		}
		sp := math.Max(s/2, bumpFloorMS)
		a.State.V = a.State.V.Add(nh.Scale(s + sp))
		if w.ghostDock.bump == nil {
			w.ghostDock.bump = map[ghostBumpKey]bool{}
		}
		w.ghostDock.bump[key] = true
		var when time.Time
		if w.Clock != nil {
			when = w.Clock.SimTime
		}
		w.LastBumpEvent = &BumpEvent{When: when, PartnerName: g.Name, SpeedMS: s}
	}
}

// ghostBumpInGate is bumpPairInGate for a ghost: the dock takes the touch
// instead of a bump while the pull is running on it or the gate holds and the
// ledger would take the Claim.
func (w *World) ghostBumpInGate(a *spacecraft.Spacecraft, g Ghost, r GhostRef) bool {
	if w.GhostCapturing(r.Owner, r.CraftID) {
		return true
	}
	// The friend is the one pulling toward us: the dock takes the touch.
	if w.ghostDock.gate != nil && w.ghostDock.gate.PullingToward(r) {
		return true
	}
	return ghostPairGateHolds(a, g) && !w.ghostBlocked(r)
}

// activeNearGhostHull reports whether the active vessel is inside DockingDistM
// of a friend's vessel in the same SOI: the warp-cap zone of decision 8 holds
// across players, since a hull the viewer cannot pass through is a hull.
func (w *World) activeNearGhostHull() bool {
	a := w.ActiveCraft()
	if a == nil || a.Landed {
		return false
	}
	for _, g := range w.Ghosts {
		if g.PrimaryID == a.Primary.ID &&
			HullDistanceM(a.State.R, VesselPortGeometry(a), g.RelPos, GhostPortGeometry(g)) <= DockingDistM {
			return true
		}
	}
	return false
}

// DockPortTipInsetM is how far a hull's drawn nose sits inside its length
// (half a row); the screens draw a ghost's reported rectangle with it so the
// rectangle's nose is the port's edge.
func DockPortTipInsetM() float64 { return dockPortTipInsetM }
