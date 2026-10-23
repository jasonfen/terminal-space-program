package screens

// The scope's width ladder (ADR 0055 A4): 2, 4, 8, 16, 32, 64 m across the
// disc. e is the ring's far edge from your crosshair (lateral miss + 0.5 m);
// R is half the step.
const (
	centerlineMinStepM = 2.0
	centerlineMaxStepM = 64.0
	// The ring must sit inside 95% of the radius to fit a step, and the
	// scope only steps back in when it would sit inside 45% (so, at the
	// next step down, inside 90%): the gap is the no-flicker band.
	centerlineStepOutFrac = 0.95
	centerlineStepInFrac  = 0.45
)

// centerlineStep ranges the disc. prev == 0 is an entry (a Framing Event):
// the smallest step that holds e. Otherwise it holds prev until e leaves its
// band, then moves a step at a time.
func centerlineStep(prev, e float64) float64 {
	if prev < centerlineMinStepM {
		prev = centerlineMinStepM
		for prev < centerlineMaxStepM && e > centerlineStepOutFrac*prev/2 {
			prev *= 2
		}
		return prev
	}
	for prev < centerlineMaxStepM && e > centerlineStepOutFrac*prev/2 {
		prev *= 2
	}
	for prev > centerlineMinStepM && e < centerlineStepInFrac*prev/2 {
		prev /= 2
	}
	return prev
}
