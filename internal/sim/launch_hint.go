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
	// sinceForecast counts ticks remaining before the next full
	// DescentCorridorFor call — see launchHintForecastEveryTicks.
	sinceForecast int
}

// launchHintForecastEveryTicks throttles the crossing state machine's
// forward propagation. DescentCorridorFor's forecast is up to ~1000
// integrator sub-steps; running it on every 50 ms tick measured at
// ~70 µs — roughly 87 % of the entire sim tick — to maintain a one-line
// advisory chip that arms and disarms on a multi-second scale. Ten ticks
// (half a second at 1×) is still far finer than the thing being observed
// and cuts the cost by an order of magnitude.
//
// It only ever delays the CHIP, never the flight: the surface view's own
// DESCENT CORRIDOR block calls DescentCorridorFor directly every frame,
// so the numbers a player actually lands on stay exact and live under
// thrust. The cheap half of the gate (descentKinematics) still runs every
// tick, so "stopped falling" — the transition that retires the chip — is
// noticed immediately.
const launchHintForecastEveryTicks = 10

// updateLaunchHint steps the crossing state machine. Called once per
// Tick, after integration (same placement as updateProximityHint), so it
// reads the DescentCorridorFor forecast off final post-tick positions.
func (w *World) updateLaunchHint() {
	// Cheap gate first: not falling at all ⇒ no crossing, and no reason to
	// propagate anything. This is the common case (on the pad, climbing,
	// coasting, near-circular) and it now costs a dot product instead of a
	// full impact forecast.
	if _, falling := descentKinematics(w.ActiveCraft()); !falling {
		w.launchHint = launchHint{}
		return
	}
	// Armed and already answered: the chip is not rendering and cannot
	// render again until this crossing ends, and the cheap gate above is
	// what ends it. The forecast could only refine exactly WHEN the
	// impact solution lapses — which nothing reads — so skip it entirely.
	if w.launchHint.inside && w.launchHint.dismissed {
		return
	}
	if w.launchHint.sinceForecast > 0 {
		w.launchHint.sinceForecast--
		return
	}
	w.launchHint.sinceForecast = launchHintForecastEveryTicks - 1

	_, descending := DescentCorridorFor(w.ActiveCraft(), DescentPredictHorizon)
	switch {
	case descending && !w.launchHint.inside:
		// Arm the crossing WITHOUT clearing `dismissed`. A player who
		// pressed [V] a moment before the forecast resolved has already
		// answered this crossing's question, and re-asking it because the
		// state machine happened to arm afterwards is exactly the re-nudge
		// the once-per-crossing discipline exists to prevent.
		// proximityHint behaves this way already — it clears dismissed
		// only when the crossing genuinely ENDS.
		w.launchHint.inside = true
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
