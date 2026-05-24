# ViewLaunch is a distinct ViewMode, not an extension of v0.10.7's anchor

v0.10.7 shipped a launch-phase improvement to the *orbit view* (`ViewTilted`
basis yaw-anchored to local-vertical while apoapsis ≤ 200 km). Playtest
exposed that orbit-scale rendering still can't deliver two reads the
launch phase needs: **early trajectory shape** (the gravity-turn arc
relative to the ground) and **orientation cues at human scale**. The
chosen response for v0.11 is a *separate* ViewMode — `ViewLaunch` — with
its own scene pipeline (chase-cam framing, body-fixed pad / LUT / trail
primitives, stage-aware rocket sprite, horizon-line ground). v0.10.7's
`LaunchAnchorPhi` and `LaunchMissionFloorM` stay as sim primitives and
gain `ViewLaunch` as a second consumer (release predicate); the
*rendering* of the launch phase moves out of the orbit screen's
`viewBasis` path.

## Considered Options

- **Replace v0.10.7.** Rip the anchor logic, route the launch phase
  entirely through `ViewLaunch`. Rejected because v0.10.7's
  anchored-orbit-view still serves one legitimate read — "ascent
  profile at orbit scale, with the developing orbit ellipse visible
  the moment one exists" — that a chase-cam *cannot* serve. The two
  views answer different questions; both should be available.
- **Coexist without sharing primitives.** Ship `ViewLaunch` as a new
  ViewMode alongside `ViewTilted`'s anchor; don't share the apoapsis
  predicate. Rejected because the two would inevitably drift apart on
  the meaning of "launch phase," and the apoapsis-floor + lat/lon
  arithmetic are the same math.
- **Subsume v0.10.7 (chosen).** v0.10.7's logic is reframed as
  reusable sim primitives (`LaunchAnchorPhi`, `LaunchMissionFloorM`);
  *rendering* of the launch phase lives in `ViewLaunch`. `ViewTilted`
  keeps the anchor as its yaw rule (unchanged behaviour at orbit
  scale); `ViewLaunch` consumes the same predicate to decide when to
  auto-release back to the player's stored ViewMode.

## Consequences

- Two ViewModes touch the launch phase by design: `ViewTilted` (orbit
  scale, ascent-profile read) and `ViewLaunch` (chase-cam, human
  scale, orientation + early-trajectory read). The duplication is
  intentional — they answer different questions.
- `ViewLaunch` auto-routes when the active Vessel is `Landed` (with
  manual ViewMode cycle as an escape hatch) and auto-releases at
  apoapsis > `LaunchMissionFloorM`. Same predicate as v0.10.7's anchor
  release; same beat as the existing ORBIT READY callout.
- New render primitives live in `widgets.Canvas`: horizon line from
  camera altitude, body-fixed glyph-at-lat-lon, body-fixed
  sprite-at-lat-lon, body-fixed trail buffer. These are general
  primitives — reusable for future surface POIs, ground-truth markers,
  etc.
- New catalog fields appear additively (no save migration):
  `Body.SurfaceColor` (defaults to `Color`) and
  `StageModule.LaunchSprite` (per-Stage ASCII art for the
  composed-from-stages rocket sprite).
- The decision to suppress `ViewTilt.Theta` inside `ViewLaunch` (pure
  side view) is deliberate — perspective tilt makes orientation
  reading *harder* at human scale because the viewer has to mentally
  un-project the tilt. Future playtest may surface a use case for
  re-enabling it; deferred.
- Scope is v0.11-grade — materially larger than v0.10.6 + v0.10.7
  combined. Targeted as the spine of the next cycle, not a v0.10
  lock-break. The full slice contract belongs in `v0.11-plan.md` when
  that doc is created (post-v0.10 closeout).
