package sim

// launchHint (issue #348 §4) is the once-per-crossing state behind the
// "you can jump to the surface view now" chip — the launch/surface
// toggle key's half of the hint discipline ToggleProximityView's
// proximityHint already established. The gate is sim.DescentCorridorFor
// itself (issue #348 §4: don't invent a second descending test), so this
// struct adds only what that gate doesn't already give: a per-crossing
// dismiss, so jumping to the surface view in response to the hint
// doesn't leave it re-asking every frame the descent continues.
// Session state; never persisted.
type launchHint struct {
	// inside is true between the trajectory first forecasting a ground
	// impact inside the horizon and it no longer doing so (touchdown,
	// crash, or the craft climbing/coasting clear of an impact solution).
	inside bool
	// dismissed records that the player already acted on this crossing by
	// entering the launch/surface view. Cleared when the crossing ends,
	// exactly like proximityHint.dismissed.
	dismissed bool
}

// updateLaunchHint steps the crossing state machine. Called once per
// Tick, after integration (same placement as updateProximityHint), so it
// reads the DescentCorridorFor forecast off final post-tick positions.
func (w *World) updateLaunchHint() {
	_, descending := DescentCorridorFor(w.ActiveCraft(), DescentPredictHorizon)
	switch {
	case descending && !w.launchHint.inside:
		w.launchHint = launchHint{inside: true}
	case !descending:
		w.launchHint = launchHint{}
	}
}

// LaunchHintActive reports whether the "jump key is available" chip
// should render this frame: descending, not already answered, and not
// already in the view it advertises.
func (w *World) LaunchHintActive() bool {
	return w.launchHint.inside && !w.launchHint.dismissed && w.ViewMode != ViewLaunch
}
