package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0053 "Settled here" item 4: undock parts the pair along the seam, from
// the seam. The lead stays put; the component above it appears where its hull
// was drawn (nose to the seam, flipped, pointing back along -a_hat) and drifts
// off at 0.05 m/s (relative, shared by mass) along +a_hat. No 75 m teleport.
func TestUndockPlacesTopComponentAtSeamAndPushesAlongAxis(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	w := p.w
	leadR, leadV := p.a.State.R, p.a.State.V
	axis := p.axis
	w.DockCrafts(0, 1) // fixture: the dock itself is covered elsewhere
	if len(w.Crafts) != 1 || len(w.Crafts[0].DockedComponents) != 2 {
		t.Fatalf("fixture dock failed: %d vessels", len(w.Crafts))
	}
	compV := w.Crafts[0].State.V
	if !w.Undock(0) {
		t.Fatal("Undock refused a two-component composite")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate = %d after undock, want 2", len(w.Crafts))
	}
	lead, top := w.Crafts[0], w.Crafts[1]
	if lead.Name != "Alpha" || top.Name != "Bravo" {
		t.Fatalf("restored %q then %q, want Alpha then Bravo", lead.Name, top.Name)
	}
	if d := lead.State.R.Sub(leadR).Norm(); d > 1e-6 {
		t.Errorf("the lead moved %.3f m: it must stay where it was", d)
	}
	// ADR 0063 §8: the lead recoils by its share of the spring (mass-weighted).
	mLead, mTop := lead.TotalMass(), top.TotalMass()
	if d := lead.State.V.Sub(compV).Sub(axis.Scale(-mTop / (mLead + mTop) * UndockSpringMS)).Norm(); d > 1e-9 {
		t.Errorf("the lead's velocity change is off its mass share by %.6f m/s", d)
	}
	_ = leadV
	// RCS Tug 4 rows + separator 1 + NTR tug 8 rows = 13 composite rows
	// (hand count); the flipped block's bottom row is composite row 12, less
	// what the seam gives up: its one separator row (1.5 m) shrinks to 0.6 m.
	wantR := leadR.Add(axis.Scale(12*DockStrideM - (DockStrideM - DockSeamGapM)))
	if d := top.State.R.Sub(wantR).Norm(); d > 1e-6 {
		t.Errorf("top component placed %.3f m from where its hull was drawn (want %+v, got %+v)", d, wantR, top.State.R)
	}
	if got := PortAlignmentDeg(top.CurrentAttitudeDir, axis.Scale(-1)); got < 179.999 {
		// top points along -axis: angle(top, +axis) must be 180.
		t.Errorf("top component nose is not pointing back along -a_hat: %+v", top.CurrentAttitudeDir)
	}
	push := top.State.V.Sub(lead.State.V)
	if d := push.Sub(axis.Scale(0.05)).Norm(); d > 1e-9 {
		t.Errorf("relative push = %+v, want 0.05 m/s along +a_hat %+v", push, axis.Scale(0.05))
	}
	// The hulls start in contact, not 75 m apart.
	if r := top.State.R.Sub(lead.State.R).Norm(); r > 30 {
		t.Errorf("components %.1f m apart straight after undock: a teleport, not a parting", r)
	}
	_ = orbital.Vec3{}
	_ = spacecraft.Stage{}
}
