package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The capture (ADR 0053 decision 5, slice B). Docking is flown: nose to
// nose, port within DockPortRangeM, relative speed under DockingVMS, noses
// within DockAlignDeg of pointing at each other. The tick that gate first
// holds starts the magnet, a DockMagnetDuration pull of the flown vessel the
// last half metre onto the port axis; completing it is the dock. The old
// 50 m / 0.1 m/s click is gone: DockingDistM now only means "clear of the
// pair" (the ring, the push distances, the re-arm distance).

const (
	// DockPortRangeM is the gate's port-to-port range term.
	DockPortRangeM = 0.5
	// DockAlignDeg is the gate's alignment term: angle(a_A, -a_B) at most.
	DockAlignDeg = 4.0
	// DockMagnetDuration is how long the pull takes, in sim time.
	DockMagnetDuration = 5 * time.Second
)

// dockGateHolds is THE docking gate: the three terms of ADR 0053 decision 5
// plus "both carry a port". The fuse (checkDocking), the magnet's keep-going
// test and the Proximity ring (ProximityDockGateReady) all call it, so the
// ring can never disagree with the dock (the #421 rule). The re-arm latch and
// the pair preconditions (same System and Primary, not Landed, not debris
// with debris) are the callers' business.
func dockGateHolds(hasPort bool, portRangeM, relSpeedMS, alignDeg float64) bool {
	return hasPort &&
		portRangeM <= DockPortRangeM &&
		relSpeedMS < DockingVMS &&
		alignDeg <= DockAlignDeg
}

// PortGateTermsHold is the gate for a Proximity scene, without the re-arm
// latch: the Proximity ring is amber when this holds and
// ProximityDockGateReady (the same plus "not latched") does not.
func PortGateTermsHold(st ProximityState) bool {
	return dockGateHolds(st.HasPort, st.PortRangeM, st.VRelMS, st.AlignDeg)
}

// attitudeRateOf is the angular velocity (rad/s, world axes) that carries
// unit vector from onto to in secs. Zero when either end is undefined or the
// nose did not move.
func attitudeRateOf(from, to orbital.Vec3, secs float64) orbital.Vec3 {
	if secs <= 0 || from.Norm() == 0 || to.Norm() == 0 {
		return orbital.Vec3{}
	}
	axis := from.Cross(to)
	n := axis.Norm()
	if n < 1e-15 {
		return orbital.Vec3{}
	}
	ang := math.Atan2(n, from.Dot(to))
	return axis.Scale(ang / n / secs)
}

// portPointVelocity is the inertial velocity of port i of a vessel whose
// stack bottom moves at v and whose nose turns at omega: the stack pivots
// about its bottom, so the port point carries omega x (axis * offset) on top
// of v. A vessel with no known shape or no port reads v.
func portPointVelocity(v, omega orbital.Vec3, g PortGeometry, i int) orbital.Vec3 {
	if !g.Known || i >= len(g.Ports) {
		return v
	}
	return v.Add(omega.Cross(g.Axis.Scale(g.Ports[i].OffsetM)))
}

// portRelVelocity is THE relative velocity of the dock gate, the chip's
// rel speed: / closing: and the Proximity ring: own port point minus the
// target's, so it is the rate of change of exactly the separation that port
// range and lateral: measure. The chip (ProximityState), dockPairGate and
// ghostPairGate all call it, so the three cannot drift apart. Either side
// without a known port reads its stack-bottom velocity.
func portRelVelocity(vOwn, wOwn orbital.Vec3, gOwn PortGeometry, vTgt, wTgt orbital.Vec3, gTgt PortGeometry) orbital.Vec3 {
	return portPointVelocity(vOwn, wOwn, gOwn, 0).Sub(portPointVelocity(vTgt, wTgt, gTgt, 0))
}

// PortGapM is the distance between two vessels' first ports, the measure of
// the re-arm latch (ReArmDistM). ok is false when either vessel has no port
// or no known shape, so the caller can hold rather than guess. The cross-player
// latch calls it with a ghost's reported geometry.
func PortGapM(ga PortGeometry, ra orbital.Vec3, gb PortGeometry, rb orbital.Vec3) (gap float64, ok bool) {
	if !ga.HasPort() || !gb.HasPort() || !ga.Known || !gb.Known {
		return 0, false
	}
	return gb.PortPoint(rb, 0).Sub(ga.PortPoint(ra, 0)).Norm(), true
}

// dockPairGate is the gate's three readings for two vessels, from their
// stacks and attitudes. hasPort is false when either vessel is port-less or
// either attitude is uninitialised (no shape is known).
func dockPairGate(a, b *spacecraft.Spacecraft) (hasPort bool, portRangeM, relSpeedMS, alignDeg float64) {
	ga, gb := VesselPortGeometry(a), VesselPortGeometry(b)
	relSpeedMS = portRelVelocity(a.State.V, a.AttitudeRate, ga, b.State.V, b.AttitudeRate, gb).Norm()
	if !ga.HasPort() || !gb.HasPort() || !ga.Known || !gb.Known {
		return false, 0, relSpeedMS, 0
	}
	pa := ga.PortPoint(a.State.R, 0)
	pb := gb.PortPoint(b.State.R, 0)
	return true, pb.Sub(pa).Norm(), relSpeedMS, PortAlignmentDeg(ga.Axis, gb.Axis)
}

// dockPairGateHolds is dockGateHolds for two vessels.
func dockPairGateHolds(a, b *spacecraft.Spacecraft) bool {
	return dockGateHolds(dockPairGate(a, b))
}

// dockMagnet is the capture in progress. Transient, never saved (a load
// drops it and the pair is simply back in free flight, nothing fused).
type dockMagnet struct {
	// pullerID is the vessel being moved (the one being flown); otherID is
	// left alone.
	pullerID, otherID uint64
	startedAt, lastAt time.Time
}

// DockCapturing reports whether a magnet pull is running between the active
// vessel and the vessel with id otherID (the chip's "capturing").
func (w *World) DockCapturing(otherID uint64) bool {
	m := w.dockMagnet
	c := w.ActiveCraft()
	if m == nil || c == nil {
		return false
	}
	return (c.ID == m.pullerID && otherID == m.otherID) || (c.ID == m.otherID && otherID == m.pullerID)
}

// dockMagnetPulls reports whether c is the vessel the magnet is pulling.
// Its RCS pulses and attitude commands are ignored and its slew is
// suspended while this holds.
func (w *World) dockMagnetPulls(c *spacecraft.Spacecraft) bool {
	return w.dockMagnet != nil && c != nil && c.ID == w.dockMagnet.pullerID
}

// dockPairEligible is today's pair preconditions, unchanged (v0.16 / ADR
// 0015, v0.12 Slice 2 / ADR 0007, #467): same System and Primary, neither
// Landed, not debris with debris. Crashed wrecks never dock either.
func dockPairEligible(a, b *spacecraft.Spacecraft) bool {
	if a == nil || b == nil {
		return false
	}
	// Vessels in different Systems share no inertial frame.
	if a.SystemIdx != b.SystemIdx {
		return false
	}
	// A different SOI is a different frame even if the numbers are close.
	if a.Primary.ID != b.Primary.ID {
		return false
	}
	// A Landed vessel must not fuse by proximity: a surface-staged pair
	// sits co-located at one lat/lon, and a liftoff re-fuses with the
	// parked stage otherwise (ADR 0007).
	if a.Landed || b.Landed || a.Crashed || b.Crashed {
		return false
	}
	// #467: debris never docks debris.
	if a.Role == spacecraft.RoleJettisonedStage && b.Role == spacecraft.RoleJettisonedStage {
		return false
	}
	return true
}

// startDockMagnet begins the pull the tick the gate first holds. The puller
// is the vessel being flown: the active vessel if it is one of the pair,
// else the lighter one.
func (w *World) startDockMagnet(i, j int) {
	if w.ghostDock.magnet != nil {
		return // a cross-player pull already has the vessel (docking_ghost.go)
	}
	a, b := w.Crafts[i], w.Crafts[j]
	puller, other := a, b
	switch {
	case w.ActiveCraftIdx == j:
		puller, other = b, a
	case w.ActiveCraftIdx == i:
	case b.TotalMass() < a.TotalMass():
		puller, other = b, a
	}
	// The pull tracks its pair by stable ID; a vessel built outside the
	// spawn paths may not have one yet.
	w.stampCraftID(puller)
	w.stampCraftID(other)
	now := w.Clock.SimTime
	w.dockMagnet = &dockMagnet{
		pullerID: puller.ID, otherID: other.ID,
		startedAt: now, lastAt: now,
	}
}

// stepDockMagnet advances a running pull one tick. It lets go when the pair
// leaves the gate (the other vessel turned, or pulsed away); otherwise it
// closes the port gap and the angle between the noses linearly to zero over
// DockMagnetDuration and docks when the time is up. Returns the slate pair
// docked, if it completed.
func (w *World) stepDockMagnet() (int, int, bool) {
	m := w.dockMagnet
	puller, pi, pOK := w.craftByID(m.pullerID)
	other, oi, oOK := w.craftByID(m.otherID)
	if !pOK || !oOK || !dockPairEligible(puller, other) || !dockPairGateHolds(puller, other) {
		w.dockMagnet = nil // let go: both back in free flight, nothing fused
		return 0, 0, false
	}
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

	// Close the gap by the fraction of what is left that this tick covers:
	// linear to zero, and robust to the other vessel's own drift.
	ga, gb := VesselPortGeometry(puller), VesselPortGeometry(other)
	pa := ga.PortPoint(puller.State.R, 0)
	gap := gb.PortPoint(other.State.R, 0).Sub(pa)
	// Turn the nose onto the port axis (anti-parallel to theirs) by the same
	// fraction of the remaining angle, about the port point: the stack pivots
	// on its bottom, so on a long stack a turn about State.R swings the port
	// sideways out of the gate.
	puller.CurrentAttitudeDir = turnToward(puller.CurrentAttitudeDir, gb.Axis.Scale(-1), frac)
	puller.State.R = puller.State.R.Add(pa.Sub(VesselPortGeometry(puller).PortPoint(puller.State.R, 0)))
	puller.State.R = puller.State.R.Add(gap.Scale(frac))

	if now.Sub(m.startedAt) < DockMagnetDuration {
		return 0, 0, false
	}
	w.dockMagnet = nil
	lo, hi := pi, oi
	if lo > hi {
		lo, hi = hi, lo
	}
	w.DockCrafts(lo, hi)
	return lo, hi, true
}

// turnToward rotates unit vector cur toward target by frac (0..1) of the
// angle between them, about their mutual perpendicular. frac >= 1 lands on
// target exactly.
func turnToward(cur, target orbital.Vec3, frac float64) orbital.Vec3 {
	c, t := cur.Unit(), target.Unit()
	if frac >= 1 {
		return t
	}
	cos := math.Max(-1, math.Min(1, c.Dot(t)))
	ang := math.Acos(cos)
	if ang < 1e-12 {
		return t
	}
	axis := c.Cross(t)
	if axis.Norm() == 0 {
		return t
	}
	return orbital.Rotate(c, axis.Unit(), ang*frac).Unit()
}
