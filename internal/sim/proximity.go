package sim

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Proximity View (ADR 0043) — the sim half. This file owns three things:
// resolving the target-centred LVLH scene the screen draws, the
// enter/leave toggle (including its refusals), and the once-per-crossing
// hint that tells the player the view exists at the moment it starts
// being useful.
//
// It computes no relative-motion math of its own: the target's state
// comes from TargetStateRelativeToActivePrimary (the same resolver the
// TARGET chip, the rendezvous planner and the co-warp gate read), and the
// basis comes from orbital.LVLHBasis. Two sources of truth for "how far
// apart are we" is exactly how a readout and a picture drift apart.

// ProximityState is the resolved close-range scene: where to put the
// camera, which way is which, and the three numbers the pilot flies on.
// Everything is evaluated at the world's current sim-time, so a caller
// rebuilds it per frame and the LVLH frame rotates with the target's
// orbit for free.
type ProximityState struct {
	// Frame is the target-centred LVLH basis. Its axes are expressed in
	// world inertial axes (the primary-relative frame shares them), so
	// the screen can hand AlongTrack / RadialOut straight to the canvas.
	Frame orbital.LVLHFrame

	// TargetWorld / CraftWorld are the inertial (system-primary-frame)
	// positions of the target and the active vessel — the canvas centre
	// and the one other thing this slice draws.
	TargetWorld orbital.Vec3
	CraftWorld  orbital.Vec3

	// RelPos / RelVel are the ACTIVE VESSEL relative to the TARGET, in
	// world axes. Sign convention: positive along Frame.AlongTrack means
	// you are AHEAD of the target; positive along Frame.RadialOut means
	// you are ABOVE it.
	RelPos orbital.Vec3
	RelVel orbital.Vec3
	// RelPortVel is the active vessel's PORT POINT relative to the target's
	// (ADR 0055 slice 0), set only when HasPort; the zero vector otherwise.
	// VRelMS and ClosingMS are read from it then, so the chip, the dock gate
	// and the ring share one velocity. RelVel stays the stack-bottom relative
	// velocity (the drawn drift arrow).
	RelPortVel orbital.Vec3

	// RangeM / VRelMS / ClosingMS are the readout triple. ClosingMS is
	// positive while the gap is shrinking — the same sign convention the
	// TARGET box's "close:" row (PROXIMITY's own row keeps the "closing:"
	// spelling, #478 A4 only shortened TARGET's) has always used.
	RangeM float64
	VRelMS float64
	// HullGapM is the distance between the two HULLS (HullDistanceM), the
	// measure of every "inside 50 m" rule and of the chip's range: row.
	// RangeM above stays anchor to anchor (stack bottom to stack bottom) for
	// closing, the camera fit and the map TARGET box; HullGapM equals it
	// when either shape is unknown.
	HullGapM  float64
	ClosingMS float64

	// TargetName is the label to put on the centred vessel — a local
	// vessel's name or a remote player's ghost label.
	TargetName string

	// Docking ports (ADR 0053 slice A). Derived from the two stacks on
	// every call, never stored. The numbers below are meaningful only when
	// HasPort is true (both vessels carry a port and both attitudes are
	// known) and are zero otherwise.
	//
	// PortRangeM is port to port, not hull to hull (RangeM is stack bottom
	// to stack bottom). AlignDeg is angle(a_own, -a_target): 0 when the
	// noses point straight at each other. OffAxisUpM / OffAxisRightM are
	// the TARGET's port relative to yours, split across the target's port
	// face (PortFaceBasis): positive up / right means push that way to
	// centre on it, negative down / left.
	PortRangeM float64
	// PortAxialM is delta . a_own (ADR 0055 A6): how far their port is ahead
	// of the plane through yours along your axis. Negative once you overshoot.
	PortAxialM float64
	AlignDeg   float64
	// MisalignUpDeg / MisalignRightDeg (ADR 0055 decision 3) split the lean of
	// your axis off parallel-to-theirs across the target's port face: the
	// components of a_own along PortFaceBasis(a_target) up / right, as asin
	// degrees. Positive = nose above / right of parallel. One definition for
	// the navball's pitch / yaw row and the Centerline view's standoff cross.
	MisalignUpDeg    float64
	MisalignRightDeg float64
	OffAxisUpM       float64
	OffAxisRightM    float64
	HasPort          bool
	// ReArmLatched is true while a same-World re-arm latch holds this pair
	// apart (#343, #794): the port row says so for as long as it does.
	ReArmLatched bool

	// PortName is the target's top component ("CM"), empty when the target
	// has no port. TargetHasPort / OwnHasPort say which side lacks one;
	// PortUnknown is a ghost, or a vessel whose attitude is uninitialised,
	// whose shape is not known. PortNote is the chip's port: row text when
	// HasPort is false ("none (S-IVB has no port)").
	PortName      string
	TargetHasPort bool
	OwnHasPort    bool
	PortUnknown   bool
	PortNote      string

	// Own / Target are the two hulls' derived geometry, for the screen to
	// draw ports on.
	Own, Target PortGeometry
}

// ProximityState resolves the close-range scene against the current
// Target. ok=false whenever the view has nothing to show; use
// ProximityRefusal for the player-facing reason.
func (w *World) ProximityState() (ProximityState, bool) {
	c := w.ActiveCraft()
	if c == nil {
		return ProximityState{}, false
	}
	// Craft and ghost targets both resolve here (HasRelativeTarget's two
	// kinds); a body target returns ok=false, which is the availability
	// gate. Ghosts come back Kepler-propagated to this world's sim-time —
	// the same state the ghost's own drawing and the rendezvous-warp
	// coast read, so the picture can't disagree with them.
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return ProximityState{}, false
	}
	frame, ok := orbital.LVLHBasis(rT, vT)
	if !ok {
		return ProximityState{}, false
	}
	primaryPos := w.BodyPosition(c.Primary)
	rel := c.State.R.Sub(rT)
	relV := c.State.V.Sub(vT)
	rangeM := rel.Norm()
	var closing float64
	if rangeM > 0 {
		// -d|rel|/dt: positive while the gap shrinks.
		closing = -rel.Dot(relV) / rangeM
	}
	st := ProximityState{
		Frame:       frame,
		TargetWorld: primaryPos.Add(rT),
		CraftWorld:  primaryPos.Add(c.State.R),
		RelPos:      rel,
		RelVel:      relV,
		RangeM:      rangeM,
		VRelMS:      relV.Norm(),
		ClosingMS:   closing,
		TargetName:  w.TargetName(),
	}
	w.resolveProximityPorts(&st, c, rT, vT)
	if w.Target.Kind == TargetCraft {
		st.ReArmLatched = w.isLocalReArmed(c.ID, w.Target.CraftID)
	}
	st.HullGapM = rangeM
	if st.Own.Known && st.Target.Known {
		st.HullGapM = HullDistanceM(c.State.R, st.Own, rT, st.Target)
	}
	return st, true
}

// resolveProximityPorts fills the docking-port half of the scene (ADR 0053
// decisions 2, 3 and 6). The aimed port is the target's first free port; a
// vessel's own is its first. Both are ports, not "the nose": a later
// multi-port vessel changes which entry is chosen, not these formulas.
func (w *World) resolveProximityPorts(st *ProximityState, own *spacecraft.Spacecraft, rT, vT orbital.Vec3) {
	st.Own = VesselPortGeometry(own)
	st.OwnHasPort = st.Own.HasPort()

	tc, _, isCraft := w.ResolveTargetCraft()
	var tgtRate orbital.Vec3 // a ghost carries no attitude rate
	switch {
	case isCraft:
		st.Target = VesselPortGeometry(tc)
		tgtRate = tc.AttitudeRate
	case w.Target.Kind == TargetGhost:
		// A ghost's hull is what its owner last reported (ADR 0053
		// decision 9). A report without one, or a ghost that no longer
		// resolves, leaves the shape unknown: no axis, no port, no capture.
		g, _, ok := w.ResolveTargetGhost()
		if !ok {
			st.PortUnknown = true
			st.PortNote = "unknown"
			return
		}
		st.Target = GhostPortGeometry(g)
	default:
		st.PortUnknown = true
		st.PortNote = "unknown"
		return
	}
	st.TargetHasPort = st.Target.HasPort()
	if st.TargetHasPort {
		st.PortName = st.Target.Ports[0].Name
	}
	if !st.Own.Known || !st.Target.Known {
		st.PortUnknown = true
		st.PortNote = "unknown"
		return
	}
	switch {
	case !st.TargetHasPort:
		st.PortNote = "none (" + portlessReason(st.Target) + ")"
		return
	case !st.OwnHasPort:
		st.PortNote = "none (yours: " + portlessReason(st.Own) + ")"
		return
	}
	st.HasPort = true
	pa := st.Own.PortPoint(own.State.R, 0)
	pb := st.Target.PortPoint(rT, 0)
	delta := pb.Sub(pa)
	st.PortRangeM = delta.Norm()
	st.PortAxialM = delta.Dot(st.Own.Axis.Unit())
	st.AlignDeg = PortAlignmentDeg(st.Own.Axis, st.Target.Axis)
	if up, right, ok := PortFaceBasis(st.Target.Axis, st.Frame); ok {
		st.OffAxisUpM = delta.Dot(up)
		st.OffAxisRightM = delta.Dot(right)
		st.MisalignUpDeg = misalignDeg(st.Own.Axis.Unit().Dot(up))
		st.MisalignRightDeg = misalignDeg(st.Own.Axis.Unit().Dot(right))
	}
	// ADR 0055 slice 0: rel speed: and closing: read the PORT POINTS' relative
	// velocity, the rate of change of the separation port range and lateral:
	// measure, through the one reader the dock gate uses (portRelVelocity).
	// closing keeps its sign: positive while the port separation shrinks.
	st.RelPortVel = portRelVelocity(own.State.V, own.AttitudeRate, st.Own, vT, tgtRate, st.Target)
	st.VRelMS = st.RelPortVel.Norm()
	if st.PortRangeM > 0 {
		st.ClosingMS = delta.Dot(st.RelPortVel) / st.PortRangeM
	}
}

// misalignDeg is asin of a unit-vector component, clamped, in degrees.
func misalignDeg(c float64) float64 {
	return math.Asin(math.Max(-1, math.Min(1, c))) * 180 / math.Pi
}

// portlessReason is the parenthesis of a "none (...)" port row: a lone
// stage names itself, a stack names its top, because a stack's port is
// its top component's and a loaded carrier has none until the last payload
// is deployed.
func portlessReason(g PortGeometry) string {
	if g.PortsInUse {
		return "its port is in use"
	}
	if g.Stacked {
		return "top is " + g.TopName
	}
	return g.TopName + " has no port"
}

// ProximityAvailable reports whether Proximity View has a scene to draw.
func (w *World) ProximityAvailable() bool {
	_, ok := w.ProximityState()
	return ok
}

// ProximityRefusal returns the player-facing reason Proximity View can't
// open, or "" when it can. A silent no-op reads as a broken key, so every
// path that refuses hands back a sentence naming the fix.
func (w *World) ProximityRefusal() string {
	if w.ProximityAvailable() {
		return ""
	}
	if w.ActiveCraft() == nil {
		return "no vessel to fly"
	}
	switch w.Target.Kind {
	case TargetNone:
		return "no target — [t] cycles to a nearby vessel"
	case TargetBody, TargetSite:
		return "needs a VESSEL target, not a body — [t] cycles to nearby vessels"
	}
	// A craft/ghost target that didn't resolve: a stale ref (they left,
	// the vessel is gone) or a degenerate orbit with no defined local
	// horizontal. Both read the same from the seat — the aim slot points
	// at something the view can't find.
	return "target vessel isn't resolvable from here"
}

// ToggleProximityView is the jump key's whole behaviour: press to enter,
// press again to return to the map exactly as it was. entered reports
// which half fired; refusal is non-empty only when entry was refused
// (leaving never refuses — you can always get back to the map).
//
// The return trip restores the ViewMode the player jumped FROM, which is
// what makes this a toggle rather than another stop on the `v` cycle. The
// map's zoom and pan are restored screen-side (Proximity keeps its own
// zoom-memory slot); this half owns only the ViewMode.
func (w *World) ToggleProximityView() (entered bool, refusal string) {
	if w.ViewMode == ViewProximity {
		back := w.ProxReturnView
		if back == ViewProximity {
			// Defensive: never toggle out of Proximity into Proximity.
			back = ViewTilted
		}
		if back == ViewLaunch && !w.LaunchSessionActive {
			// The chase-cam we jumped from has since been released (an
			// active-vessel switch onto a flying vessel, or a `v` cycle out
			// of the stack). Restoring ViewLaunch anyway would drop the
			// player into a scene with no session behind it — T+ frozen at
			// zero, no trail, no max-Q — which reads as a broken view
			// rather than a return. Fall through to the map underneath it.
			back = w.baseProjection(ViewLaunch)
		}
		w.ViewMode = back
		return false, ""
	}
	if reason := w.ProximityRefusal(); reason != "" {
		return false, reason
	}
	// The return address may legitimately be ViewLaunch — the two jump keys
	// are documented to stack. What keeps that from becoming the mutual
	// capture issue #348's review found is the mirror-image invariant on
	// the other side: routeToLaunchView never records a SCENE in
	// PrevViewMode, only the projection underneath it (see its doc
	// comment), so this chain is always at most one hop deep and always
	// bottoms out on the map.
	w.ProxReturnView = w.ViewMode
	w.ViewMode = ViewProximity
	// Entering answers the hint, so it stops asking until the pair
	// separates and closes again.
	w.proxHint.dismissed = true
	return true, ""
}

// proximityHint is the once-per-crossing state behind the "you can jump
// to Proximity View now" chip. The chip is a standing block, not an
// event, so the flicker risk is a range oscillating on the threshold
// rather than a burst of toasts — hence a hysteresis band (arm at the
// co-warp couple range, re-arm only past the decouple range) instead of a
// cooldown. Session state; never persisted.
type proximityHint struct {
	// inside is true between crossing INTO the hint band and separating
	// back out past the release range.
	inside bool
	// target is the aim slot the current crossing belongs to. Retargeting
	// is a new question, so it restarts the crossing.
	target Target
	// dismissed records that the player already acted on this crossing by
	// entering Proximity View. Cleared when the crossing ends.
	dismissed bool
}

// proximityHintRangeM / proximityHintReleaseM are the hint's hysteresis
// band, deliberately the co-warp couple/decouple pair rather than numbers
// of their own: 35 km is already the range at which the game declares two
// players to be in the same neighbourhood (their warps lock), so it is
// exactly the moment "you are now flying relative to them" becomes true.
// Inventing a second proximity radius would give the player two different
// answers to the same question.
const (
	proximityHintRangeM   = coWarpCoupleRangeM
	proximityHintReleaseM = coWarpDecoupleRangeM
)

// updateProximityHint steps the crossing state machine. Called once per
// Tick, after integration, so it reads final positions.
func (w *World) updateProximityHint() {
	st, ok := w.ProximityState()
	if !ok {
		w.proxHint = proximityHint{}
		return
	}
	if w.proxHint.target != w.Target {
		w.proxHint = proximityHint{target: w.Target}
	}
	switch {
	case !w.proxHint.inside && st.RangeM <= proximityHintRangeM:
		w.proxHint.inside = true
	case w.proxHint.inside && st.RangeM >= proximityHintReleaseM:
		w.proxHint = proximityHint{target: w.Target}
	}
}

// ProximityHintActive reports whether the "jump key is available" chip
// should render this frame: inside the band, not already answered, and
// not already in the view it advertises.
func (w *World) ProximityHintActive() bool {
	if !w.proxHint.inside || w.proxHint.dismissed || w.ViewMode == ViewProximity {
		return false
	}
	return w.ProximityAvailable()
}

// ProximityDockGateReady reports whether the pair holds the port gate right
// now (ADR 0053 decision 5: port within 0.5 m, relative speed under 0.1 m/s,
// noses within 4 degrees of facing, both carrying a port) AND isn't held
// apart by a same-World re-arm latch (#343). It reads dockGateHolds, the
// very predicate checkDocking fuses on and the magnet keeps pulling on, so the
// ring's green state and the game's actual "you are about to dock" moment can
// never drift apart (issue #348 §1, the #421 rule). A pair with no port on
// either side, or a ghost whose shape is unknown, is never ready.
//
// #372: the latch had once drifted out of this predicate; keeping the same
// isLocalReArmed check here is what makes a just-undocked pair read "not
// ready" while checkDocking refuses to fuse it.
func (w *World) ProximityDockGateReady(st ProximityState) bool {
	if !PortGateTermsHold(st) {
		return false
	}
	if c := w.ActiveCraft(); c != nil && w.Target.Kind == TargetCraft {
		if w.isLocalReArmed(c.ID, w.Target.CraftID) {
			return false
		}
	}
	// The cross-player mirror: a ghost the ledger would refuse a Claim on
	// (its re-arm latch, an end already engaged) is never ready (#372).
	if w.Target.Kind == TargetGhost && w.ghostBlocked(GhostRef{w.Target.GhostOwner, w.Target.CraftID}) {
		return false
	}
	return true
}

// ProximityDriftHorizonMin / ProximityDriftHorizonMax bound the no-burn
// drift forecast (issue #348 §1): "a few minutes to tens of minutes" in
// the issue's own words. The screen picks a horizon inside this band
// from the current zoom (see orbit_proximity.go's
// proximityDriftHorizonFor) and hands it to ProximityDriftPath — the sim
// owns the physics of walking the horizon forward, the screen owns how
// far ahead is worth looking at the player's current scale.
const (
	ProximityDriftHorizonMin = 2 * time.Minute
	ProximityDriftHorizonMax = 30 * time.Minute
)

// proximityDriftSamples is the target number of points on the drawn
// drift curve — plenty for a dashed polyline over a few-minutes-to-tens-
// of-minutes horizon at docking-relevant relative speeds (impactPathSamples
// uses 180 for a full 30-minute descent arc that can pass through
// atmosphere; this curve is always vacuum two-body, so it needs far less
// density to read smoothly).
const proximityDriftSamples = 60

// proximityDriftSubStepCap bounds the drift forecast's integrator
// sub-step, seconds. predictMaxSubStep alone can return up to
// predictMaxSubStepCap (120 s) for a long-period orbit, which is far
// coarser than the whole drift horizon can afford to lose to a single
// step's dt² truncation on the Verlet fallback path (reached only for a
// non-Kepler-eligible state — an atmosphere-skimming or hyperbolic
// craft). 5 s keeps a full 30-minute horizon under proximityDriftMaxSubSteps.
const proximityDriftSubStepCap = 5.0

// proximityDriftMaxSubSteps caps total integrator work per forecast so a
// degenerate state (tiny period → tiny sub-step) can't stall the render
// loop — same role as impactMaxSubSteps.
const proximityDriftMaxSubSteps = 1024

// ProximityDriftPoint is one sample of the no-burn relative-drift
// forecast: the active vessel's position relative to the target,
// expressed in the LVLH frame the TARGET'S OWN STATE defines AT THAT
// FUTURE INSTANT — not today's frame. orbital.LVLHBasis's doc comment
// explains why the basis has to rotate with the target; a caller that
// reused today's frame for every future sample would draw a curve that
// lies (a target-and-chaser pair sitting still relative to each other in
// the CO-ROTATING frame — e.g. two craft on the same circular orbit at a
// fixed phase offset — would instead sweep around like an inertial
// orbit, because the frozen frame keeps rotating relative to the actual
// physics while the physics itself doesn't).
//
// Local follows LVLHFrame.ToFrame's axis order: X = along-track,
// Y = radial-out, Z = cross-track, all metres.
type ProximityDriftPoint struct {
	T     time.Duration
	Local orbital.Vec3
}

// ProximityDriftPath forward-propagates BOTH the active vessel and the
// target — using predictStep, the repo's one propagator, exactly like
// forwardBallisticPath's established reuse pattern (descent.go) — and
// differences their states at each sample instant, expressed in the
// target's LVLH frame resolved fresh at that instant. No CW
// linearization: this is the exact two-body difference, sampled.
//
// The target's own drag coefficient isn't generally available (a ghost
// target has no Spacecraft at all; even a local craft target's bc is a
// simplifying stand-in for whatever the OTHER player is actually flying)
// so it propagates with bc=0 — a reasonable default since proximity ops
// are, by construction, well clear of any atmosphere dense enough for
// the choice to matter over a few-to-tens-of-minutes horizon.
//
// Both states propagate under the ACTIVE vessel's primary/mu — the same
// simplification TargetStateRelativeToActivePrimary already makes for
// every other relative-vector readout this view draws from (range,
// |v_rel|, closing): Proximity View exists for two craft sharing a
// neighbourhood, which in practice means sharing a primary.
//
// ok is false only for the same degenerate-input cases the rest of this
// file already refuses (no active craft, no resolvable vessel/ghost
// target, no defined LVLH frame at the current or a future instant) or a
// non-positive horizon.
func (w *World) ProximityDriftPath(horizon time.Duration) ([]ProximityDriftPoint, bool) {
	c := w.ActiveCraft()
	if c == nil {
		return nil, false
	}
	rT, vT, ok := w.TargetStateRelativeToActivePrimary()
	if !ok {
		return nil, false
	}
	frame0, ok := orbital.LVLHBasis(rT, vT)
	if !ok {
		return nil, false
	}
	primary := c.Primary
	mu := primary.GravitationalParameter()
	total := horizon.Seconds()
	if mu <= 0 || total <= 0 {
		return nil, false
	}

	craftState := c.State
	targetState := physics.StateVector{R: rT, V: vT}
	craftBC := c.EffectiveBallisticCoefficient()
	const targetBC = 0.0

	dt := predictMaxSubStep(orbitalPeriod(craftState, mu))
	if td := predictMaxSubStep(orbitalPeriod(targetState, mu)); td < dt {
		dt = td
	}
	if dt > proximityDriftSubStepCap {
		dt = proximityDriftSubStepCap
	}
	steps := int(math.Ceil(total / dt))
	if steps < 1 {
		steps = 1
	}
	if steps > proximityDriftMaxSubSteps {
		steps = proximityDriftMaxSubSteps
		dt = total / float64(steps)
	}
	sampleEvery := steps / proximityDriftSamples
	if sampleEvery < 1 {
		sampleEvery = 1
	}

	points := make([]ProximityDriftPoint, 0, proximityDriftSamples+2)
	points = append(points, ProximityDriftPoint{Local: frame0.ToFrame(craftState.R.Sub(rT))})

	elapsed := 0.0
	for i := 0; i < steps; i++ {
		craftState = predictStep(craftState, mu, dt, primary, craftBC)
		targetState = predictStep(targetState, mu, dt, primary, targetBC)
		elapsed += dt
		if (i+1)%sampleEvery != 0 && i != steps-1 {
			continue
		}
		frame, ok := orbital.LVLHBasis(targetState.R, targetState.V)
		if !ok {
			// Degenerate future frame (radial fall / zero speed) — stop
			// extending the curve rather than draw a bogus point; every
			// sample taken so far is still valid.
			break
		}
		points = append(points, ProximityDriftPoint{
			T:     time.Duration(elapsed * float64(time.Second)),
			Local: frame.ToFrame(craftState.R.Sub(targetState.R)),
		})
	}
	if len(points) < 2 {
		return nil, false
	}
	return points, true
}
