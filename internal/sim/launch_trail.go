// Package sim — v0.11.0+ launch-trail breadcrumb buffer.
//
// LaunchTrail is the body-fixed (lat, lon, alt, sampledAt) FIFO that
// the chase-cam scene re-projects each render so the trace visibly
// rotates with the body — the geographic launch site stays
// geographic. Stored on World; cleared at session open / switch-end
// release / hand-off; survives a manual `v` cycle out + back.
//
// Plan reference: designdocs/terminal-space-program/v0.11-plan.md → Slice v0.11.0 (Trail is
// body-fixed locked decision).
package sim

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// TrailPoint is one sample in the LaunchTrail FIFO. Stored in
// body-fixed coordinates so the renderer re-projects via
// render.BodyFixedToWorld at current sim-time — the geographic
// launch site stays geographic as the body rotates underneath the
// inertial frame.
type TrailPoint struct {
	LatDeg, LonDeg, AltM float64
	SampledAt            time.Time
}

// launchTrailCap is the FIFO cap on LaunchTrail. 256 points at the
// 1 s sampling cadence give ~4 minutes of trail — long enough to
// cover the full ascent from pad to a ~200 km apoapsis at typical
// climb profiles. Older samples evict from the front.
const launchTrailCap = 256

// launchTrailIntervalSec is the minimum sim-time gap between
// consecutive trail samples. The empty-buffer case bypasses the gap
// (the first sample appends immediately on session open).
const launchTrailIntervalSec = 1.0

// maybeSampleLaunchTrail is called from tickLaunchView when a
// session is active. Appends a body-fixed sample of the active
// craft's current position to LaunchTrail if (a) the buffer is empty
// or (b) at least launchTrailIntervalSec of sim-time has elapsed
// since the most recent sample. FIFO-evicts the oldest sample when
// the cap is reached.
func (w *World) maybeSampleLaunchTrail() {
	c := w.ActiveCraft()
	if c == nil {
		return
	}
	if len(w.LaunchTrail) > 0 {
		gap := w.Clock.SimTime.Sub(w.LaunchTrail[len(w.LaunchTrail)-1].SampledAt).Seconds()
		if gap < launchTrailIntervalSec {
			return
		}
	}
	w.LaunchTrail = append(w.LaunchTrail, craftBodyFixedSample(w, c))
	if len(w.LaunchTrail) > launchTrailCap {
		w.LaunchTrail = w.LaunchTrail[len(w.LaunchTrail)-launchTrailCap:]
	}
}

// craftBodyFixedSample converts the craft's current primary-relative
// inertial position to a body-fixed (lat, lon, alt) sample by
// inverting render.BodyFixedToWorld at the current sim-time. The
// returned SampledAt is the current sim-time stamp so the renderer
// can later judge sample age for fading / culling.
func craftBodyFixedSample(w *World, c *spacecraft.Spacecraft) TrailPoint {
	r := c.State.R
	rMag := r.Norm()
	altM := rMag - c.Primary.RadiusMeters()
	var unit render.Vec3
	if rMag > 0 {
		unit = render.Vec3{X: r.X / rMag, Y: r.Y / rMag, Z: r.Z / rMag}
	}
	lat, lon := render.WorldToBodyFixed(c.Primary, unit, w.Clock.SimTime)
	return TrailPoint{
		LatDeg:    lat,
		LonDeg:    lon,
		AltM:      altM,
		SampledAt: w.Clock.SimTime,
	}
}
