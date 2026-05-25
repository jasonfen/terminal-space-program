# Body rotation uses one convention everywhere

Through v0.11.1, the codebase carried three disagreeing notions of how a
body rotates in the world frame:

- **Physical spin axis** (`render.BodyRotationAxisWorld`, used by
  `BodySpinOmegaWorld`, the spawn velocity in `surfaceSpawnPosVel`, the
  landed integrator, the spacecraft burn-direction math, and the navball).
- **Renderer convention** (`render.BodyFixedToWorld` /
  `WorldToBodyFixed` and the parallel-coded `projectPixelToLatLon` that
  drives every per-body texture painter, lighting/eclipse, and the
  navball texture). Snyder forward orthographic at ViewTop's sub-observer
  point, mapping the body's (east, north, toward-camera) axes onto world
  (X, Y, Z). For an AxialAzimuth-0 tilted body this rotates body-fixed
  points about `(0, sin(tilt), cos(tilt))` rather than the physical
  `(sin(tilt), 0, cos(tilt))` — axes 32.6° apart for Earth.
- **Z-aligned drag ω** (`physics.AtmosphereOmega`). Magnitude
  approximation along world +Z, kept "for back-compat with v0.8.4."

The v0.11.1 `/verify` surfaced the integrator/renderer disagreement
concretely: a Saturn V on KSC, 3 sim-seconds after ignition (altitude
~10 m), placed `BodyFixedToWorld(pad_lat, pad_lon, simTime+3)` and the
rocket's integrated `c.State.R` ~770 m apart tangentially — exactly
`|Δω × R_eq| · 3 s` for the 32.6°-separated axes. Slice 2's 9-cell LUT
sprite made the bug visible; Slice 1's single-pixel pad marker hid it;
v0.10.7's orbit-scale launch anchor was sub-pixel.

## Considered Options

- **Move overlays into the integrator frame.** Draw LUT / pad / trail
  off `c.State.R` instead of `BodyFixedToWorld`. Rejected: the trail's
  body-fixed re-projection invariant (which makes the launch site
  visibly rotate with the ground during ascent) is the whole point of
  storing trail points as `(lat, lon, alt, sampledAt)`. Papering over
  the convention mismatch at the screen layer breaks that invariant.
- **Re-derive spawn ω from the renderer's convention.** Local to
  `sim/spawn.go`, `sim/landed.go`, and `spacecraft.surfaceSpawnPosVel`.
  Fixes the 770 m drift. Rejected: introduces a third "convention ω"
  diverging from both the physical spin axis and `AtmosphereOmega`,
  permanently embedding the renderer's projection quirk in physics
  code that has no other reason to care about how textures are
  projected.
- **Renderer adopts the physical spin axis everywhere (chosen).**
  Rewrite `BodyFixedToWorld` / `WorldToBodyFixed` as pure rotations
  about `BodyRotationAxisWorld` from epoch. Rewrite
  `projectPixelToLatLon` to take a screen-up direction parameter
  (projection of body-local-north onto the canvas), so per-body texture
  painters paint the correct (lat, lon) at each pixel for any view.
  Concurrently, fold `physics.AtmosphereOmega` into the same physical
  spin-axis convention — one ω, one rotation, every consumer.

## Consequences

- **One rotation convention across the codebase.** Texture pipeline,
  integrator, drag, spawn/landed, navball, burn-direction, and surface
  overlays all read the same body-rotation source of truth
  (`BodyRotationAxisWorld` and `BodySpinOmegaWorld`). A future
  maintainer needing to ask "which ω does X use?" has one answer.
- **Visible texture rotation for tilted bodies.** Earth's continents in
  ViewTop paint at a different orientation than they did pre-v0.11.2 —
  the projection correctly accounts for the 23.4° tilt now. ViewRight
  on Earth shows the equator edge-on with the spin axis visibly tilted;
  ViewTop shows the tilt as a visible roll. This is the correct
  presentation; the pre-v0.11.2 rendering was a side-effect of treating
  body-local-north as canvas-up regardless of view geometry.
- **`bodyEpochOffsetDeg` sweep needed.** The existing per-body offsets
  were tuned against the pre-v0.11.2 projection to land iconic
  features at the visible disk centre. v0.11.2 re-tunes them against
  the corrected projection — offsets now express only the iconic-face
  choice, no embedded projection-correction term.
- **Drag direction shifts for tilted bodies.** `AtmosphereOmega` gains
  a Z-axis tilt (and loses the Z-aligned-magnitude approximation). For
  a vessel at Earth's equator the change is a few percent in magnitude
  plus a small north/south component to the drag force. The CONTEXT.md
  "engineering caveat" on Air-Relative Velocity describing the
  Z-aligned shortcut becomes obsolete and gets removed in the v0.11.2
  vocabulary update.
- **Snapshot tests on every per-body painter regenerate.** The painted
  texture orientation changes for any body with non-zero tilt. The
  navball overlay regenerates too. Drag-direction tests on tilted-body
  vessels need expected-value updates.
- **Sim/spawn/landed/launch overlays change zero source lines.** They
  continue to call `BodyFixedToWorld` and `BodySpinOmegaWorld`; the
  functions are now physically correct, so the 770 m drift simply goes
  away. The launch-view's trail re-projection invariant is preserved.
- **ADR-sized but bounded.** Surface area: `BodyFixedToWorld`,
  `WorldToBodyFixed`, `projectPixelToLatLon`, 8 per-body painters,
  `lighting.go`, `navball.go`, `physics/drag.go` + `AtmosphereOmega`,
  and the test suites for each. No save migration (rotation conventions
  aren't persisted). No new public types; the public surface of the
  render package stays as it was.
- **Lands as v0.11.2.** v0.11.1 (Slice 2 — LUT + all-vessels-in-SOI)
  holds without tag until v0.11.2 ships the fix; v0.11.1's 4 commits
  on `v0.11.1-lut-soi` stay parked. v0.11.2 ships them together as the
  next cycle tag. Slice 3 (stage-aware launch sprites) becomes v0.11.3;
  slice 4 (polish + edge-cases) becomes v0.11.4.
