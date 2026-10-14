package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Docking ports (ADR 0053 §2-3, slice A). A vessel's position (State.R) is
// the BOTTOM of its stack: the composed sprite's row 0 sits at the anchor
// and rows climb along CurrentAttitudeDir. So the nose, and the port on
// it, is R + L*a_hat with L = composed stack rows x 1.5 m, not a half
// height from a centre. Nothing here is stored: every call re-derives the
// geometry from the stack, and the port flag from the catalog
// (spacecraft.Stage.Ports).
//
// The row arithmetic mirrors launchStackLayout in tui/screens (sim cannot
// import the screens). Its parity with the drawn sprite is pinned by
// TestDockPortNoseRowsMatchDrawnSprite in that package, so the port a
// pilot reads about is the nose they are looking at.

const (
	// DockStrideM is one launch-sprite row in metres (screens.vesselSubPixelM).
	DockStrideM = 1.5

	// dockTaperThresholdRows / dockDefaultWidthPx / dockMinWidthPx mirror
	// launch_sprite.go's taperThreshold, defaultSpriteWidthPx and
	// minRenderedStageWidthPx.
	dockTaperThresholdRows = 6
	dockDefaultWidthPx     = 2
	dockMinWidthPx         = 4
)

// StackRows is the composed sprite's total height in rows, taper and
// separator rows included: the same count launchStackLayout produces.
func StackRows(stages []spacecraft.Stage) int {
	rows := 0
	for i, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		rows += s.LaunchSpriteRowsPx
		if i+1 < len(stages) {
			up := stages[i+1]
			if s.LaunchSpriteRowsPx >= dockTaperThresholdRows && up.LaunchSpriteRowsPx >= dockTaperThresholdRows {
				rows++
			}
			if up.LaunchSpriteRowsPx > 0 {
				rows++
			}
		}
	}
	return rows
}

// StackWidthPx is the widest stage body in sprite pixels (rendered width,
// i.e. after the default and the minimum are applied).
func StackWidthPx(stages []spacecraft.Stage) int {
	maxW := 0
	for _, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		w := s.LaunchSpriteWidthPx
		if w <= 0 {
			w = dockDefaultWidthPx
		}
		if w < dockMinWidthPx {
			w = dockMinWidthPx
		}
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}

// DockPort is one docking port of a vessel, positioned along its stack.
type DockPort struct {
	// Name is the component the port sits on (its top stage's name), what
	// the chip header shows as "port: CM".
	Name string
	// End is the stack end the port is on ("top" = the nose).
	End string
	// OffsetM is the port's distance from the vessel position (the stack
	// bottom) along the attitude axis.
	OffsetM float64
}

// PortGeometry is a vessel's hull and ports, derived from its stack.
type PortGeometry struct {
	// Axis is the unit attitude direction (zero when the vessel's attitude
	// is uninitialised: no shape is known, see Known).
	Axis orbital.Vec3
	// NoseM / WidthM are the hull length (stack bottom to nose) and width.
	NoseM, WidthM float64
	// Ports is the free ports of the vessel, as a list: today none or one.
	Ports []DockPort
	// TopName is the top stage's name; Stacked is true for more than one
	// stage (a composite or a loaded carrier). Both explain a port-less
	// vessel on the chip ("S-IVB has no port" / "top is Relay Comsat-3").
	TopName string
	Stacked bool
	// Known is false when the attitude is uninitialised, so the axis, and
	// everything measured against it, cannot be trusted.
	Known bool
}

// HasPort reports whether the vessel carries at least one free port.
func (g PortGeometry) HasPort() bool { return len(g.Ports) > 0 }

// PortPoint is the world position of port i of a vessel whose position is r.
func (g PortGeometry) PortPoint(r orbital.Vec3, i int) orbital.Vec3 {
	return r.Add(g.Axis.Scale(g.Ports[i].OffsetM))
}

// PortGeometryOf derives a stack's hull and port from its stages and
// attitude. A vessel has a port when its TOP stage's part does (a
// composite's port is its top component's, a loaded carrier has none until
// its last payload is deployed).
func PortGeometryOf(stages []spacecraft.Stage, attitude orbital.Vec3) PortGeometry {
	g := PortGeometry{
		Axis:    attitude.Unit(),
		NoseM:   float64(StackRows(stages)) * DockStrideM,
		WidthM:  float64(StackWidthPx(stages)) * DockStrideM,
		Stacked: len(stages) > 1,
	}
	g.Known = attitude.Norm() > 1e-9
	if len(stages) == 0 {
		return g
	}
	top := stages[len(stages)-1]
	g.TopName = top.Name
	for _, p := range top.Ports() {
		g.Ports = append(g.Ports, DockPort{Name: top.Name, End: p.End, OffsetM: g.NoseM})
	}
	return g
}

// VesselPortGeometry is PortGeometryOf for a live vessel.
func VesselPortGeometry(c *spacecraft.Spacecraft) PortGeometry {
	if c == nil {
		return PortGeometry{}
	}
	return PortGeometryOf(c.Stages, c.CurrentAttitudeDir)
}

// dockRadialAxisDeg is how close to radial a port axis may point before
// "up across the port face" switches from RadialOut to AlongTrack (ADR
// 0053 decision 7): RadialOut projected off an axis it nearly coincides
// with is numerical noise.
const dockRadialAxisDeg = 10.0

// PortFaceBasis is the pair of directions across a port's face, in world
// axes: up/down and left/right as a pilot looking at the port sees them.
// axis is the unit vector the port faces out along (the target's nose
// direction for its port); f is the target's LVLH frame.
//
//	up    = RadialOut with its axis component removed, normalised
//	        (AlongTrack instead when the axis is within 10 degrees of radial)
//	right = perpendicular to both axis and up, signed to agree with
//	        CrossTrack
//
// ADR 0053 words "right" as CrossTrack with the axis component removed.
// That agrees with this whenever the axis lies in the radial/along-track
// plane, and this stays orthogonal to up when it does not, which matters
// because the off-axis readout splits one offset onto the two directions.
// ok is false only for a zero axis.
func PortFaceBasis(axis orbital.Vec3, f orbital.LVLHFrame) (up, right orbital.Vec3, ok bool) {
	a := axis.Unit()
	if a.Norm() == 0 {
		return orbital.Vec3{}, orbital.Vec3{}, false
	}
	seed := f.RadialOut
	if math.Abs(a.Dot(f.RadialOut)) > math.Cos(dockRadialAxisDeg*math.Pi/180) {
		seed = f.AlongTrack
	}
	up = seed.Sub(a.Scale(seed.Dot(a))).Unit()
	right = up.Cross(a).Unit()
	if right.Dot(f.CrossTrack) < 0 {
		right = right.Scale(-1)
	}
	return up, right, true
}

// PortAlignmentDeg is the angle between two noses facing each other: 0 when
// vessel A's axis is exactly opposite B's, i.e. angle(a_A, -a_B).
func PortAlignmentDeg(aA, aB orbital.Vec3) float64 {
	c := aA.Unit().Dot(aB.Unit().Scale(-1))
	if c > 1 {
		c = 1
	}
	if c < -1 {
		c = -1
	}
	return math.Acos(c) * 180 / math.Pi
}
