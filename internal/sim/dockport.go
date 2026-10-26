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
	// dockPortTipInsetM is how far the port sits inside the hull length
	// L = rows*1.5 m. The composer draws row r as a dot at r*1.5 m from the
	// anchor, so a hull occupies -0.75 m to L-0.75 m along the axis, and its
	// nose, the edge a second hull's nose touches, is at L-0.75 m. ADR 0053
	// decision 2 says R+L*a_hat; that overshoots the drawn nose by half a
	// row, more than the 0.5 m gate and a 1.5 m visible gap between two
	// docked hulls. TestProximityPortMarkLandsOnDrawnNose measures this.
	dockPortTipInsetM = DockStrideM / 2

	dockTaperThresholdRows = 6
	dockDefaultWidthPx     = 2
	dockMinWidthPx         = 4
)

// StackRows is the composed sprite's total height in rows, taper and
// separator rows included: the same count launchStackLayout produces.
func StackRows(stages []spacecraft.Stage) int {
	rows, sideTop := 0, 0
	for i, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		if boosterPair(stages, i) {
			// Two side columns (ADR 0058 decision 10): no height, taper or
			// separator of their own; they only set a floor on the height.
			sideTop = max(sideTop, rows+s.LaunchSpriteRowsPx)
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
	return max(rows, sideTop)
}

// boosterPair mirrors launch_sprite.go's boosterPairAt: stages[i] is marked
// and the stage above it is drawn, so it paints as two columns beside it.
func boosterPair(stages []spacecraft.Stage, i int) bool {
	return stages[i].LitWithAbove && i+1 < len(stages) && stages[i+1].LaunchSpriteRowsPx > 0
}

// StackWidthPx is the widest stage body in sprite pixels (rendered width,
// i.e. after the default and the minimum are applied).
func StackWidthPx(stages []spacecraft.Stage) int {
	maxW := 0
	for i, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		w := s.LaunchSpriteWidthPx
		if w <= 0 {
			w = dockDefaultWidthPx
		}
		authored := w
		if w < dockMinWidthPx {
			w = dockMinWidthPx
		}
		if boosterPair(stages, i) {
			// Side columns: the extent is core width + 2 columns + 2 gaps.
			col := max(1, authored/2)
			up := stages[i+1].LaunchSpriteWidthPx
			if up <= 0 {
				up = dockDefaultWidthPx
			}
			w = max(up, dockMinWidthPx) + 2*col + 2
		}
		if w > maxW {
			maxW = w
		}
	}
	return maxW
}

// stageWidthM is one stage's hull width in metres: its part's published
// diameter (ADR 0061), else the sprite rule, max(px, 4) x one row.
func stageWidthM(s spacecraft.Stage) float64 {
	if s.DiameterM > 0 {
		return s.DiameterM
	}
	w := s.LaunchSpriteWidthPx
	if w <= 0 {
		w = dockDefaultWidthPx
	}
	return float64(max(w, dockMinWidthPx)) * DockStrideM
}

// StackWidthM is the hull width in metres: the widest drawn stage. A booster
// pair reads core + 2 boosters + 2 gaps of one sprite column (DockStrideM).
func StackWidthM(stages []spacecraft.Stage) float64 {
	maxW := 0.0
	for i, s := range stages {
		if s.LaunchSpriteRowsPx <= 0 {
			continue
		}
		w := stageWidthM(s)
		if boosterPair(stages, i) {
			w = stageWidthM(stages[i+1]) + 2*w + 2*DockStrideM
		}
		maxW = max(maxW, w)
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
	// OffsetM is the port's distance from the vessel position along the
	// attitude axis: the drawn nose, half a row inside the hull length
	// (see dockPortTipInsetM).
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
	// PortsInUse is true when the vessel's top stage carries a port that is
	// not free because it sits at a dock seam (a docked composite): Ports is
	// empty, and the chip says the port is in use rather than that there is
	// none.
	PortsInUse bool
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
// attitude. A vessel has a port when its TOP stage's part does. This is the
// raw stack: VesselPortGeometry removes the ports a dock seam consumes, so a
// docked composite or a loaded carrier has none until the partner leaves.
func PortGeometryOf(stages []spacecraft.Stage, attitude orbital.Vec3) PortGeometry {
	g := PortGeometry{
		Axis:    attitude.Unit(),
		NoseM:   float64(StackRows(stages)) * DockStrideM,
		WidthM:  StackWidthM(stages),
		Stacked: len(stages) > 1,
	}
	g.Known = attitude.Norm() > 1e-9
	if len(stages) == 0 {
		return g
	}
	top := stages[len(stages)-1]
	g.TopName = top.Name
	for _, p := range top.Ports() {
		g.Ports = append(g.Ports, DockPort{Name: top.Name, End: p.End, OffsetM: g.NoseM - dockPortTipInsetM})
	}
	return g
}

// VesselPortGeometry is PortGeometryOf for a live vessel, less the ports a
// dock seam has consumed. A composite (a nose-to-nose fuse, `D`, or a spawn
// nose payload plan) is drawn with every block above a seam flipped so its top
// meets the seam (ADR 0053 "Settled here" item 1). Each seam therefore
// consumes the nose port of the block below it and of the block above it, and
// the far end of the stack is the flipped partner's ENGINE end, which never has
// a port. With one nose port per part that leaves a composite NO free port; it
// exposes only the ports not used at a seam, as a list (review finding 2). A
// lone vessel, or a composite whose seams the record does not tile (an old
// save, drawn linear), exposes its top stage's, and a vessel gets its port back
// when the partner leaves.
func VesselPortGeometry(c *spacecraft.Spacecraft) PortGeometry {
	if c == nil {
		return PortGeometry{}
	}
	g := PortGeometryOf(c.Stages, c.CurrentAttitudeDir)
	if len(DockSeams(c)) > 0 && len(g.Ports) > 0 {
		g.Ports = nil
		g.PortsInUse = true
	}
	return g
}

// PortFaceBasis is orbital.PortFaceBasis: the pair of directions across a
// port's face (up/down, left/right). The function lives in orbital so the
// spacecraft layer (the PORT-frame burn modes, ADR 0053 slice C) and this
// package (the off-axis readout) share ONE definition of "up" and "right"
// and the readout can never disagree with the keys.
func PortFaceBasis(axis orbital.Vec3, f orbital.LVLHFrame) (up, right orbital.Vec3, ok bool) {
	return orbital.PortFaceBasis(axis, f)
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
