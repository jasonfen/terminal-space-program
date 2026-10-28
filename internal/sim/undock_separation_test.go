package sim

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// buildStack docks n ported RCS Tugs into a single composite at w.Crafts[0]
// and returns it, nose set along the core's velocity so every Undock has an
// axis. The docks themselves are fixtures (DockCrafts); the capture that
// normally precedes them is covered in docking_port_test.go.
func buildStack(t *testing.T, w *World, n int) *spacecraft.Spacecraft {
	t.Helper()
	if n < 2 {
		t.Fatalf("buildStack: n must be >= 2, got %d", n)
	}
	earth := w.Systems[0].FindBody("Earth")
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	a := spacecraft.NewFromLoadout("RCS-tug")
	a.Name = "Core"
	a.Primary = *earth
	a.State = physics.StateVector{R: r, V: v, M: a.TotalMass()}
	a.CurrentAttitudeDir = v.Unit()
	w.Crafts = []*spacecraft.Spacecraft{a}
	w.ActiveCraftIdx = 0
	w.stampCraftID(a)

	for i := 1; i < n; i++ {
		b := spacecraft.NewFromLoadout("RCS-tug")
		b.Name = "Comp"
		b.Primary = *earth
		b.State = physics.StateVector{R: r, V: v, M: b.TotalMass()}
		w.stampCraftID(b)
		w.Crafts = append(w.Crafts, b)
		placePortToPort(w.Crafts[0], b, 0.3)
		w.DockCrafts(0, len(w.Crafts)-1)
	}
	if len(w.Crafts) != 1 {
		t.Fatalf("buildStack: expected 1 composite after %d docks, got %d craft", n, len(w.Crafts))
	}
	if got := len(w.Crafts[0].DockedComponents); got != n {
		t.Fatalf("buildStack: composite has %d components, want %d", got, n)
	}
	return w.Crafts[0]
}

// TestUndockSeparationClearsDockingGates is the #342 regression test, restated
// for the port gate (ADR 0053): undocking a stack of n components must leave
// no pair inside the docking gate, at every n from a plain two-component split
// up to five. The hulls now part from contact rather than 75 m apart, so what
// keeps them from re-capturing is the springs (0.15 m/s relative, over the
// gate's 0.1) and the hull/port geometry, not distance; and that must hold
// WITHOUT the re-arm latch, which is only the backstop.
func TestUndockSeparationClearsDockingGates(t *testing.T) {
	// Instrument check: the same predicate does return true for a pair that
	// sits in the gate, so a "false" below means something.
	ctrlA := spacecraft.NewFromLoadout("RCS-tug")
	ctrlB := spacecraft.NewFromLoadout("RCS-tug")
	ctrlA.CurrentAttitudeDir = orbital.Vec3{Y: 1}
	placePortToPort(ctrlA, ctrlB, 0.3)
	if !dockPairGateHolds(ctrlA, ctrlB) {
		t.Fatal("control pair at 0.3 m port to port reads out of the gate: the predicate cannot return true")
	}

	for n := 2; n <= 5; n++ {
		n := n
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			w, _ := NewWorld()
			buildStack(t, w, n)

			undockAll(t, w, 0) // one seam per U (ADR 0063 §4)
			if len(w.Crafts) != n {
				t.Fatalf("expected %d craft after undock, got %d", n, len(w.Crafts))
			}
			w.localReArms = nil // the latch is the backstop, not the protection
			for i := 0; i < len(w.Crafts); i++ {
				for j := i + 1; j < len(w.Crafts); j++ {
					if dockPairGateHolds(w.Crafts[i], w.Crafts[j]) {
						t.Errorf("n=%d: components %d and %d are inside the docking gate straight after undock", n, i, j)
					}
				}
			}
			if startsMagnet(w) {
				t.Errorf("n=%d: checkDocking began re-capturing the just-undocked stack", n)
			}
			if len(w.Crafts) != n {
				t.Errorf("n=%d: slate count = %d after the post-undock scan, want %d (unchanged)", n, len(w.Crafts), n)
			}
		})
	}
}

// undockAll presses U on the vessel at idx until it is no longer a composite.
func undockAll(t *testing.T, w *World, idx int) {
	t.Helper()
	for i := 0; len(w.Crafts[idx].DockedComponents) >= 2; i++ {
		if i > 20 || !w.Undock(idx) {
			t.Fatalf("Undock stuck on %d components: %q", len(w.Crafts[idx].DockedComponents), w.UndockRefusal(idx))
		}
	}
}

// dockedLoadoutPair docks two catalog loadouts port to port (fixture dock) and
// returns the world plus the two pre-dock vessels (a leads, b is partner).
func dockedLoadoutPair(t *testing.T, idA, idB string) (*World, *spacecraft.Spacecraft, *spacecraft.Spacecraft) {
	t.Helper()
	w, _ := NewWorld()
	earth := w.Systems[0].FindBody("Earth")
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	v := orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}
	a := spacecraft.NewFromLoadout(idA)
	b := spacecraft.NewFromLoadout(idB)
	a.Name, b.Name = "Alpha", "Bravo"
	for _, s := range []*spacecraft.Spacecraft{a, b} {
		s.Primary = *earth
		s.State = physics.StateVector{R: r, V: v, M: s.TotalMass()}
	}
	a.CurrentAttitudeDir = v.Unit()
	w.Crafts = []*spacecraft.Spacecraft{a, b}
	w.ActiveCraftIdx = 0
	w.stampCraftID(a)
	w.stampCraftID(b)
	placePortToPort(a, b, 0.3)
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("fixture dock left %d vessels", len(w.Crafts))
	}
	return w, a, b
}

// ADR 0063 §8: the spring is shared by mass at 0.05 m/s relative, momentum
// conserving. Numbers computed from the loadouts' own masses (full tanks), the
// ADR table's heavy/light columns, plus the equal-mass case (0.025 each).
func TestUndockSpringSharedByMass(t *testing.T) {
	if UndockSpringMS != 0.05 {
		t.Fatalf("UndockSpringMS = %v, want 0.05", UndockSpringMS)
	}
	for _, c := range []struct{ a, b string }{
		{"RCS-tug", "RCS-tug"},
		{"Apollo-TLI", "Dragon"},
		{"Dragon", "Cargo-Dragon"},
		{"Apollo-TLI", "RCS-tug"},
	} {
		c := c
		t.Run(c.a+"+"+c.b, func(t *testing.T) {
			w, a, b := dockedLoadoutPair(t, c.a, c.b)
			mA, mB := a.TotalMass(), b.TotalMass()
			comp := w.Crafts[0]
			axis := stackAxis(comp)
			v0 := comp.State.V
			if !w.Undock(0) {
				t.Fatal("Undock refused")
			}
			lead, top := w.Crafts[0], w.Crafts[1]
			dvLead := lead.State.V.Sub(v0).Dot(axis)
			dvTop := top.State.V.Sub(v0).Dot(axis)
			wantLead, wantTop := -mB/(mA+mB)*0.05, mA/(mA+mB)*0.05
			if math.Abs(dvLead-wantLead) > 1e-9 || math.Abs(dvTop-wantTop) > 1e-9 {
				t.Errorf("lead %+.6f top %+.6f m/s, want %+.6f / %+.6f", dvLead, dvTop, wantLead, wantTop)
			}
			if p := mA*dvLead + mB*dvTop; math.Abs(p) > 1e-6 {
				t.Errorf("momentum change %.3g kg m/s, want 0", p)
			}
			if rel := dvTop - dvLead; math.Abs(rel-0.05) > 1e-9 {
				t.Errorf("relative speed %.6f, want 0.05", rel)
			}
			if c.a == "RCS-tug" && c.b == "RCS-tug" && (math.Abs(dvLead+0.025) > 1e-9 || math.Abs(dvTop-0.025) > 1e-9) {
				t.Errorf("equal masses: %.4f / %.4f, want -0.025 / +0.025", dvLead, dvTop)
			}
		})
	}
}

// ADR 0063 §4: U opens the newest seam only. A flown three-component chain
// gives two vessels (the rest stays joined); a second U opens the next seam.
func TestUndockOpensNewestSeamOnly(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 3)
	last := w.Crafts[0].DockedComponents[2]
	if w.IsBatch(0) {
		t.Fatal("a flown dock chain reads as an authored batch")
	}
	if !w.Undock(0) {
		t.Fatal("Undock refused")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate = %d after one U on 3 components, want 2", len(w.Crafts))
	}
	if n := len(w.Crafts[0].DockedComponents); n != 2 {
		t.Errorf("flown stack has %d components left, want 2", n)
	}
	if got := w.Crafts[1]; got.ID != last.CraftID || len(got.DockedComponents) != 0 {
		t.Errorf("released vessel id %d (want %d), %d components", got.ID, last.CraftID, len(got.DockedComponents))
	}
	if !w.Undock(0) || len(w.Crafts) != 3 {
		t.Fatalf("second U: slate = %d, want 3", len(w.Crafts))
	}
}

// An authored payload stack (components carry no vessel IDs) is still a batch
// at three components; one flown vessel docked on top is not part of it.
func TestIsBatchIsAuthoredStacksOnly(t *testing.T) {
	w, idx := batchFixture(t, 90, false)
	if !w.IsBatch(idx) {
		t.Fatal("setup: the authored stack is not a batch")
	}
	c := w.Crafts[idx]
	for i := 1; i < len(c.DockedComponents); i++ {
		c.DockedComponents[i].CraftID = uint64(100 + i) // the same stack, flown into place
	}
	if w.IsBatch(idx) {
		t.Error("a stack of flown vessels reads as a batch")
	}
}

// ADR 0063 §5: every piece comes back with the ID it docked with, the flown
// vessel keeps its ID, and the target slot resolves again with no re-target.
func TestUndockRestoresDockedIDs(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	w := p.w
	idA, idB := p.a.ID, p.b.ID
	w.SetTargetCraft(1)
	w.DockCrafts(0, 1)
	if w.Crafts[0].ID != idA {
		t.Fatalf("setup: composite id %d, want %d", w.Crafts[0].ID, idA)
	}
	w.Undock(0)
	if w.Crafts[0].ID != idA || w.Crafts[1].ID != idB {
		t.Errorf("ids after undock = %d,%d want %d,%d", w.Crafts[0].ID, w.Crafts[1].ID, idA, idB)
	}
	if c, _, ok := w.targetCraft(); !ok || c != w.Crafts[1] {
		t.Error("the partner target did not resolve again after U")
	}
}

func TestUndockStampsFreshIDWhenRecordHasNoneOrIsLive(t *testing.T) {
	for _, c := range []struct {
		name string
		edit func(w *World)
	}{
		{"record without an id", func(w *World) { w.Crafts[0].DockedComponents[1].CraftID = 0 }},
		{"id already live", func(w *World) {
			x := spacecraft.NewFromLoadout("RCS-tug")
			x.ID = w.Crafts[0].DockedComponents[1].CraftID
			w.Crafts = append(w.Crafts, x)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			w, _ := NewWorld()
			buildStack(t, w, 2)
			w.NextCraftID = 50
			want := w.Crafts[0].DockedComponents[1].CraftID
			c.edit(w)
			w.Undock(0)
			got := w.Crafts[1].ID
			if got == 0 || got == want || got < 50 {
				t.Errorf("released id = %d, want a fresh stamp >= 50 (docked-with %d)", got, want)
			}
		})
	}
}

// ADR 0063 §7: each piece gets its pre-latch hold back; the flown vessel's
// nav frame comes back with a port hold; a record without the field is
// Inertial and the released piece does not swing to prograde.
func TestUndockRestoresPreLatchHolds(t *testing.T) {
	t.Run("modes round trip", func(t *testing.T) {
		w, _, _ := dockedLoadoutPair(t, "RCS-tug", "RCS-tug")
		w.Crafts[0].DockedComponents[0].AttitudeMode = modePtr(spacecraft.BurnRetrograde)
		w.Crafts[0].DockedComponents[1].AttitudeMode = modePtr(spacecraft.BurnNormalPlus)
		w.Undock(0)
		if got := w.Crafts[0].AttitudeMode; got != spacecraft.BurnRetrograde {
			t.Errorf("flown vessel hold = %v, want Retrograde", got)
		}
		if got := w.Crafts[1].AttitudeMode; got != spacecraft.BurnNormalPlus {
			t.Errorf("released vessel hold = %v, want Normal+", got)
		}
	})
	t.Run("recorded at the latch", func(t *testing.T) {
		p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
		p.a.AttitudeMode = spacecraft.BurnRetrograde
		p.b.AttitudeMode = spacecraft.BurnRadialOut
		p.w.DockCrafts(0, 1)
		dcs := p.w.Crafts[0].DockedComponents
		if dcs[0].AttitudeMode == nil || *dcs[0].AttitudeMode != spacecraft.BurnRetrograde ||
			dcs[1].AttitudeMode == nil || *dcs[1].AttitudeMode != spacecraft.BurnRadialOut {
			t.Errorf("recorded modes = %v / %v", dcs[0].AttitudeMode, dcs[1].AttitudeMode)
		}
	})
	t.Run("port hold brings PORT back", func(t *testing.T) {
		p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
		w := p.w
		w.SetTargetCraft(1)
		w.setNavMode(NavPort)
		p.a.AttitudeMode = spacecraft.BurnPortIn
		w.DockCrafts(0, 1)
		w.setNavMode(NavOrbit) // the latch dropped the frame
		w.Undock(0)
		if w.Crafts[0].AttitudeMode != spacecraft.BurnPortIn || w.NavMode != NavPort {
			t.Errorf("hold %v nav %v, want Port+ in PORT", w.Crafts[0].AttitudeMode, w.NavMode)
		}
	})
	t.Run("old record is Inertial and does not slew", func(t *testing.T) {
		w, _, _ := dockedLoadoutPair(t, "RCS-tug", "RCS-tug")
		w.Crafts[0].DockedComponents[0].AttitudeMode = nil
		w.Crafts[0].DockedComponents[1].AttitudeMode = nil
		w.Undock(0)
		top := w.Crafts[1]
		if top.AttitudeMode != spacecraft.BurnInertial || w.Crafts[0].AttitudeMode != spacecraft.BurnInertial {
			t.Fatalf("holds = %v / %v, want Inertial", w.Crafts[0].AttitudeMode, top.AttitudeMode)
		}
		nose := top.CurrentAttitudeDir.Unit()
		tickFor(w, 10)
		if dot := top.CurrentAttitudeDir.Unit().Dot(nose); dot < 1-1e-9 {
			t.Errorf("released piece swung off its nose in 10 s (dot %.9f)", dot)
		}
	})
}

func modePtr(m spacecraft.BurnMode) *spacecraft.BurnMode { return &m }

// ADR 0063 §6: the stack that held Port+ before the latch parts with
// misalign 0 degrees and zero lateral one tick after U.
func TestUndockPartsAlongTheLatchedPortAxis(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	w := p.w
	w.SetTargetCraft(1)
	w.setNavMode(NavPort)
	p.a.AttitudeMode = spacecraft.BurnPortIn
	p.b.HoldInertial() // a partner that docks nose-on held that nose (a Prograde hold would have turned it away)
	axis := p.axis
	w.DockCrafts(0, 1)
	tickFor(w, 5) // the stack rides docked for a while
	w.Undock(0)
	w.Tick()
	lead, top := w.Crafts[0], w.Crafts[1]
	if mis := PortAlignmentDeg(lead.CurrentAttitudeDir, top.CurrentAttitudeDir); mis > 1e-6 {
		t.Errorf("misalign %.6f deg one tick after U, want 0", mis)
	}
	d := top.State.R.Sub(lead.State.R)
	if lat := d.Sub(axis.Scale(d.Dot(axis))).Norm(); lat > 1e-3 {
		t.Errorf("lateral %.6f m one tick after U, want 0", lat)
	}
}

// ADR 0063 §5, recorder: the flown vessel's file continues through dock and
// undock, the released vessel opens no file and no second session marker.
func TestRecordingContinuesThroughDockAndUndock(t *testing.T) {
	p := newPortPair(t, "rcs-tug", "ntr-tug", 0.3, 0, 0)
	w := p.w
	w.SetRecordingMarker("session start: test")
	dir := recAttach(t, w)
	w.DockCrafts(0, 1)
	w.Undock(0)
	w.Tick()
	files, _ := filepath.Glob(filepath.Join(dir, "*.txt"))
	if len(files) != 1 {
		t.Fatalf("recording files after dock+undock = %v, want 1", files)
	}
	b, _ := os.ReadFile(files[0])
	if n := strings.Count(string(b), "session start"); n != 1 {
		t.Errorf("session start rows = %d, want 1", n)
	}
	if !strings.Contains(string(b), "undock: Bravo") {
		t.Error("the undock row is missing from the flown vessel's file")
	}
}

// visitorOnto docks a lone RCS tug (the active, leading vessel) onto the
// existing composite at slate index 0 and returns the new composite's index.
func visitorOnto(t *testing.T, w *World) *spacecraft.Spacecraft {
	t.Helper()
	comp := w.Crafts[0]
	v := spacecraft.NewFromLoadout("RCS-tug")
	v.Name, v.Primary = "Visitor", comp.Primary
	v.State = physics.StateVector{R: comp.State.R, V: comp.State.V, M: v.TotalMass()}
	v.CurrentAttitudeDir = comp.CurrentAttitudeDir
	w.Crafts = []*spacecraft.Spacecraft{v, comp}
	w.stampCraftID(v)
	w.ActiveCraftIdx = 0
	placePortToPort(v, comp, 0.3)
	w.SetTargetCraft(1)
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("fixture dock left %d vessels", len(w.Crafts))
	}
	return w.Crafts[0]
}

// ADR 0063 §4 (batch review finding 1): the flown vessel docking onto a
// composite lists [Visitor, Core, Comp]; the newest seam is Visitor|Core, so U
// releases the whole Core+Comp block as one composite with Core's docked-with
// ID, the target resolves, and the docked Proximity scene names the same
// partner at the same seam.
func TestUndockOpensTheSeamTheFlownVesselMade(t *testing.T) {
	w, _ := NewWorld()
	buildStack(t, w, 2)
	coreID, compID := w.Crafts[0].DockedComponents[0].CraftID, w.Crafts[0].DockedComponents[1].CraftID
	c := visitorOnto(t, w)
	if n := len(c.DockedComponents); n != 3 || c.NewestSeamStart() != 1 {
		t.Fatalf("setup: %d components, newest seam start %d, want 3 and 1", n, c.NewestSeamStart())
	}
	st, ok := w.dockedProximityState(c)
	seams := DockSeams(c)
	if !ok || st.TargetName != "Core" || st.SeamOffsetM != float64(StackRows(c.Stages[:seams[0]]))*DockStrideM {
		t.Errorf("docked scene = %q at %.1f m (ok %v), want Core at the Visitor|Core seam", st.TargetName, st.SeamOffsetM, ok)
	}
	if n, ok := w.DockedPartnerName(w.Target.CraftID); !ok || n != "Core" {
		t.Errorf("DockedPartnerName = %q %v, want Core", n, ok)
	}
	if !w.Undock(0) {
		t.Fatal("Undock refused")
	}
	if len(w.Crafts) != 2 {
		t.Fatalf("slate = %d, want 2", len(w.Crafts))
	}
	lead, rel := w.Crafts[0], w.Crafts[1]
	if lead.Name != "Visitor" || len(lead.DockedComponents) != 0 {
		t.Errorf("flown vessel = %q with %d components, want the lone Visitor", lead.Name, len(lead.DockedComponents))
	}
	if rel.ID != coreID || len(rel.DockedComponents) != 2 || rel.DockedComponents[1].CraftID != compID {
		t.Errorf("released id %d with %d components, want Core's id %d keeping [Core, Comp]", rel.ID, len(rel.DockedComponents), coreID)
	}
	if c, _, ok := w.targetCraft(); !ok || c != rel {
		t.Error("the Core target did not resolve to the released composite")
	}
}

// Batch review finding 2: a stage dropped while docked must not erase the
// flown vessel's pre-latch hold (rebuildComponentAround), and a bottom
// component dropping shifts the newest-seam marker with the components.
func TestStageDropWhileDockedKeepsThePreLatchHold(t *testing.T) {
	w, _, _ := dockedLoadoutPair(t, "Apollo-TLI", "RCS-tug")
	c := w.Crafts[0]
	if len(c.DockedComponents[0].Stages) < 2 {
		t.Fatal("setup: the lead needs two stages")
	}
	c.DockedComponents[0].AttitudeMode = modePtr(spacecraft.BurnRetrograde)
	c.Stages = c.Stages[1:] // a bottom stage left while docked
	c.ReconcileDockedComponents()
	if m := c.DockedComponents[0].AttitudeMode; m == nil || *m != spacecraft.BurnRetrograde {
		t.Fatalf("lead record after the drop = %v, want Retrograde", m)
	}
	w.Undock(0)
	if got := w.Crafts[0].AttitudeMode; got != spacecraft.BurnRetrograde {
		t.Errorf("flown vessel hold after U = %v, want Retrograde", got)
	}

	// The marker follows a whole component leaving the bottom.
	d := &spacecraft.Spacecraft{
		Stages:           []spacecraft.Stage{{Name: "B"}, {Name: "C"}},
		DockedComponents: []spacecraft.DockedComponent{{Name: "A", Stages: []spacecraft.Stage{{Name: "A"}}}, {Name: "B", Stages: []spacecraft.Stage{{Name: "B"}}}, {Name: "C", Stages: []spacecraft.Stage{{Name: "C"}}}},
		NewestSeam:       2,
	}
	if !d.ReconcileDockedComponents() || d.NewestSeam != 1 || d.NewestSeamStart() != 1 {
		t.Errorf("after A left: changed marker = %d (start %d), want 1", d.NewestSeam, d.NewestSeamStart())
	}
}

// Batch review finding 3: a flown vessel docked as lead onto an authored
// carrier is not a batch (the lead carries a live ID); the same carrier alone
// still is; and no vessel can fly onto a tiled carrier through the port gate,
// because its seams consume every port (VesselPortGeometry), so the pair is
// only reachable through the fixture dock.
func TestFlownLeadOnAnAuthoredCarrierIsNotABatch(t *testing.T) {
	w, idx := batchFixture(t, 90, false)
	if !w.IsBatch(idx) {
		t.Fatal("setup: the carrier alone is not a batch")
	}
	carrier := w.Crafts[idx]
	if g := VesselPortGeometry(carrier); g.HasPort() {
		t.Errorf("a tiled carrier exposes a free port %+v: the gate could admit a visitor", g.Ports)
	}
	v := spacecraft.NewFromLoadout("RCS-tug")
	v.Name, v.Primary = "Visitor", carrier.Primary
	v.State = physics.StateVector{R: carrier.State.R, V: carrier.State.V, M: v.TotalMass()}
	v.CurrentAttitudeDir = carrier.CurrentAttitudeDir
	w.Crafts = append(w.Crafts, v)
	w.stampCraftID(v)
	w.SetActiveCraftIdx(len(w.Crafts) - 1)
	w.DockCrafts(len(w.Crafts)-1, idx)
	if w.IsBatch(w.ActiveCraftIdx) {
		t.Error("a flown lead on an authored carrier reads as a batch")
	}
}
