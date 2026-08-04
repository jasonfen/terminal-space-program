package sim

import (
	"math"
	"testing"
)

// leadAngleWorld builds a two-craft world sharing the active craft's
// primary, with the sister craft placed at a signed along-track offset
// `deltaDeg` from the active craft on the SAME circular orbital plane
// (no inclination). Positive deltaDeg means the sister sits ahead of the
// active craft in the direction of its own motion (mirrors
// rendezvousSmallLagWorld's rotateAboutAxis pattern, generalized to an
// arbitrary signed angle rather than a fixed small lag). Target is bound
// to the sister craft (idx 1); active stays at idx 0.
func leadAngleWorld(t *testing.T, deltaDeg float64) *World {
	t.Helper()
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	sister := w.Crafts[1]
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := deltaDeg * math.Pi / 180
	sister.State.R = rotateAboutAxis(active.State.R, axis, angle)
	sister.State.V = rotateAboutAxis(active.State.V, axis, angle)
	sister.Primary = active.Primary
	return w
}

// TestTargetLeadAngleDeg_IssueRepro is the issue's own reproduction: two
// craft on the same 500 km circular 0deg-inclination orbit, separated
// along-track. Asserts the sign reads correctly for both directions and
// that the two craft's readings of each other are opposite in sign
// (issue #287, decisions section: "positive means the target is ahead of
// you along-track").
func TestTargetLeadAngleDeg_IssueRepro(t *testing.T) {
	const lead = 82.0
	w := leadAngleWorld(t, lead)

	// From the active craft's (idx 0) point of view, the sister (idx 1)
	// sits `lead` degrees ahead along-track.
	got, ok := w.TargetLeadAngleDeg()
	if !ok {
		t.Fatal("expected ok=true for a same-primary craft target")
	}
	if math.Abs(got-lead) > 0.5 {
		t.Errorf("active-craft view: lead angle = %.2f, want ~%.1f (target ahead)", got, lead)
	}

	// Flip perspective: make the sister active and target the original
	// craft. From the sister's point of view, the original craft is
	// `lead` degrees BEHIND it along-track, so the reading must be the
	// opposite sign of the first reading.
	w.ActiveCraftIdx = 1
	w.SetTargetCraft(0)
	flipped, ok := w.TargetLeadAngleDeg()
	if !ok {
		t.Fatal("expected ok=true for the flipped perspective")
	}
	if math.Abs(flipped-(-lead)) > 0.5 {
		t.Errorf("sister-craft view: lead angle = %.2f, want ~%.1f (target behind)", flipped, -lead)
	}
	if (got > 0) == (flipped > 0) {
		t.Errorf("the two craft's readings must be opposite in sign: got=%.2f flipped=%.2f", got, flipped)
	}
}

// TestTargetLeadAngleDeg_Wrap180 checks the +/-180 deg wrap boundary: a
// target placed just past +180 deg of lead (i.e., actually trailing by
// just under 180 deg from the other side) must read as a small negative
// number, not wrap around to a bogus large positive one, and vice versa.
func TestTargetLeadAngleDeg_Wrap180(t *testing.T) {
	cases := []struct {
		name     string
		deltaDeg float64
		wantDeg  float64
	}{
		{"just_under_positive_180", 179.5, 179.5},
		{"just_under_negative_180", -179.5, -179.5},
		{"just_past_positive_180_wraps_negative", 190, -170},
		{"just_past_negative_180_wraps_positive", -190, 170},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := leadAngleWorld(t, c.deltaDeg)
			got, ok := w.TargetLeadAngleDeg()
			if !ok {
				t.Fatal("expected ok=true")
			}
			if got > 180 || got <= -180 {
				t.Errorf("lead angle %.2f outside (-180, 180] range", got)
			}
			if math.Abs(got-c.wantDeg) > 0.5 {
				t.Errorf("lead angle = %.2f, want ~%.1f", got, c.wantDeg)
			}
		})
	}
}

// TestTargetLeadAngleDeg_NonCoplanar checks a non-coplanar target still
// yields a sensible along-track answer via projection into the active
// craft's orbital plane, rather than refusing or returning garbage. The
// expected value is derived independently of the production formula:
// for a target displaced by along-track angle `delta` on a plane tilted
// by inclination `incl` from the active craft's plane (about the line of
// nodes = the active craft's own radius vector), the projected along-
// track angle is atan2(sin(delta)*cos(incl), cos(delta)) — a standard
// projected-angle identity, not a restatement of the vector cross/dot
// form the implementation uses.
func TestTargetLeadAngleDeg_NonCoplanar(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	sister := w.Crafts[1]

	aHat := active.State.R.Unit()
	nHat := active.State.R.Cross(active.State.V).Unit()
	pHat := nHat.Cross(aHat).Unit() // in-plane, perpendicular to aHat, aligned with motion

	deltaDeg := 60.0
	inclDeg := 30.0
	delta := deltaDeg * math.Pi / 180
	incl := inclDeg * math.Pi / 180

	r := active.State.R.Norm()
	sister.State.R = aHat.Scale(r * math.Cos(delta)).
		Add(pHat.Scale(r * math.Sin(delta) * math.Cos(incl))).
		Add(nHat.Scale(r * math.Sin(delta) * math.Sin(incl)))
	// Velocity is irrelevant to the along-track angle computation (only
	// positions are used) but give the sister a state consistent enough
	// to resolve as a normal craft target; reuse the active craft's speed
	// tangent to its own new radius direction as a stand-in.
	sister.State.V = pHat.Scale(active.State.V.Norm())
	sister.Primary = active.Primary

	want := math.Atan2(math.Sin(delta)*math.Cos(incl), math.Cos(delta)) * 180 / math.Pi

	got, ok := w.TargetLeadAngleDeg()
	if !ok {
		t.Fatal("expected ok=true for a non-coplanar same-primary target")
	}
	if math.Abs(got-want) > 0.5 {
		t.Errorf("non-coplanar lead angle = %.2f, want ~%.2f (projected)", got, want)
	}
}

// TestTargetLeadAngleDeg_DifferentPrimaries_NotMeaningful verifies the
// cross-SOI refusal path: when the target craft orbits a different
// primary than the active craft, the phase angle is not meaningful (no
// shared reference frame to measure it in), so the function must report
// ok=false rather than invent a number.
func TestTargetLeadAngleDeg_DifferentPrimaries_NotMeaningful(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	sister := w.Crafts[1]
	other := otherBody(t, w)
	sister.Primary = other
	_ = active

	if _, ok := w.TargetLeadAngleDeg(); ok {
		t.Error("expected ok=false when the target craft orbits a different primary")
	}
}

// TestTargetLeadAngleDeg_BodyTarget_NotMeaningful verifies the field is
// craft-target-only: a body target (not a craft) must never produce a
// lead angle.
func TestTargetLeadAngleDeg_BodyTarget_NotMeaningful(t *testing.T) {
	w := mustWorld(t)
	w.SetTargetBody(1)
	if _, ok := w.TargetLeadAngleDeg(); ok {
		t.Error("expected ok=false for a body target")
	}
}

// TestTargetLeadAngleDeg_GhostTarget mirrors the craft-target repro but
// through the ghost path (a remote player's craft, ADR 0034), which the
// TARGET chip renders through the same field.
func TestTargetLeadAngleDeg_GhostTarget(t *testing.T) {
	w := rendezvousTwoCraftWorld(t)
	active := w.Crafts[0]
	const lead = 45.0
	h := active.State.R.Cross(active.State.V)
	axis := h.Unit()
	angle := lead * math.Pi / 180
	ghostR := rotateAboutAxis(active.State.R, axis, angle)
	ghostV := rotateAboutAxis(active.State.V, axis, angle)

	g := ghostShapedLike(w, active.Primary, ghostR, ghostV, "SHA256:gern", "gern", 99)
	w.Ghosts = []Ghost{g}
	w.SetTargetGhost(g.Owner, g.CraftID)

	got, ok := w.TargetLeadAngleDeg()
	if !ok {
		t.Fatal("expected ok=true for a same-primary ghost target")
	}
	if math.Abs(got-lead) > 0.5 {
		t.Errorf("ghost target lead angle = %.2f, want ~%.1f", got, lead)
	}
}
