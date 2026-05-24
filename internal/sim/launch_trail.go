// Package sim — v0.11.0+ launch-trail breadcrumb buffer.
//
// LaunchTrail is the body-fixed (lat, lon, alt, sampledAt) FIFO that
// the chase-cam scene re-projects each render so the trace visibly
// rotates with the body — the geographic launch site stays
// geographic. Stored on World; cleared at session open / auto-release
// / hand-off; survives a manual `v` cycle out + back.
//
// Plan reference: docs/v0.11-plan.md → Slice v0.11.0 (Trail is
// body-fixed locked decision).
package sim

import "time"

// TrailPoint is one sample in the LaunchTrail FIFO. Stored in
// body-fixed coordinates so the renderer re-projects via
// render.BodyFixedToWorld at current sim-time — the geographic
// launch site stays geographic as the body rotates underneath the
// inertial frame.
type TrailPoint struct {
	LatDeg, LonDeg, AltM float64
	SampledAt            time.Time
}
