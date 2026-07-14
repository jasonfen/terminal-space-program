package screens

import (
	"math"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// fitScaleFor builds a throwaway view of the same canvas size and fits it
// to a world's current Framing-Event radius, returning the resulting
// px/m scale — the value the real screen must land on after a Framing
// Event on that focus. Mirrors the orbit_camera_contract_test approach of
// comparing against an explicit FitTo.
func fitScaleFor(t *testing.T, w *sim.World, cols, rows int) float64 {
	t.Helper()
	exp := NewOrbitView(ghostTestTheme())
	exp.Resize(cols, rows)
	exp.canvas.FitTo(w.FocusZoomRadius())
	return exp.canvas.Scale()
}

// Spectate fits the camera exactly once to the ghost's DRAWN orbit extent
// (a Framing Event, ADR 0021), then tracks the ghost — a subsequent ghost
// report that moves it never re-fits. v0.28 S6.
func TestSpectateFitsGhostOrbitOnce(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, ownR, _ := leoWorld(t)
	g := circularGhost(w, ownR.Norm()*1.6)
	w.Ghosts = []sim.Ghost{g}

	// Enter Spectate: focus the ghost. The next Render is a Framing Event
	// fitting the canvas to the ghost's orbit extent (2a).
	w.SpectateGhost(g.Owner, g.CraftID)
	if w.Focus.Kind != sim.FocusGhost {
		t.Fatalf("SpectateGhost did not set FocusGhost: %+v", w.Focus)
	}
	v.Render(w, 0, 200, 60)
	got := v.canvas.Scale()

	// The fit must frame the GHOST's extent, not the own-craft focus.
	wantGhost := fitScaleFor(t, w, 200, 60)
	if math.Abs(got-wantGhost) > wantGhost*1e-9 {
		t.Errorf("spectate scale %.6e != ghost-extent fit %.6e", got, wantGhost)
	}
	// Sanity: the ghost sits on a 1.6× larger orbit than the own craft, so
	// its extent fit is a genuinely different (wider) frame than own-craft.
	wOwn, _, _ := leoWorld(t)
	wOwn.Focus = sim.Focus{Kind: sim.FocusCraft}
	if craftFit := fitScaleFor(t, wOwn, 200, 60); math.Abs(got-craftFit) <= craftFit*1e-9 {
		t.Errorf("spectate reused the own-craft fit (%.6e); expected a ghost-extent frame", craftFit)
	}

	// The camera centers on (tracks) the ghost.
	if d := v.canvas.CenterWorld().Sub(g.Pos).Norm(); d > 1 {
		t.Errorf("spectate center %.0f km off the ghost — focus tracking broke", d/1e3)
	}

	// A ghost REPORT lands: it moves along its orbit (new RelPos/Pos). This
	// is ambient sim state, not a Framing Event — the scale must not re-fit.
	moved := g
	rel := orbital.Vec3{Y: ownR.Norm() * 1.6} // quarter turn round the circle
	moved.RelPos = rel
	moved.Pos = w.BodyPosition(w.ActiveCraft().Primary).Add(rel)
	w.Ghosts = []sim.Ghost{moved}
	v.Render(w, 0, 200, 60)

	if s := v.canvas.Scale(); math.Abs(s-got) > got*1e-9 {
		t.Errorf("ghost report re-fit the camera: %.6e -> %.6e (ADR 0021: report corrections never re-fit)", got, s)
	}
	// Center still tracks the ghost's new position.
	if d := v.canvas.CenterWorld().Sub(moved.Pos).Norm(); d > 1 {
		t.Errorf("center did not track the ghost's report: %.0f km off", d/1e3)
	}
}

// Exiting Spectate via the return-to-own-craft focus key ([f] → CycleFocus)
// restores own-craft framing with a fresh Framing Event. v0.28 S6.
func TestSpectateFocusReturnRestoresOwnCraft(t *testing.T) {
	v := NewOrbitView(ghostTestTheme())
	v.Resize(200, 60)

	w, ownR, _ := leoWorld(t)
	g := circularGhost(w, ownR.Norm()*1.6)
	w.Ghosts = []sim.Ghost{g}
	w.SpectateGhost(g.Owner, g.CraftID)
	v.Render(w, 0, 200, 60)
	spectateScale := v.canvas.Scale()

	// The "return-to-own-craft focus key" — CycleFocus off a ghost focus
	// snaps home rather than advancing into the body cycle.
	w.CycleFocus(true)
	if w.Focus.Kind != sim.FocusCraft {
		t.Fatalf("focus-return did not restore own craft: %+v", w.Focus)
	}
	v.Render(w, 0, 200, 60)

	// A fresh Framing Event fits to the own craft — a different frame than
	// the ghost's wider orbit.
	wantCraft := fitScaleFor(t, w, 200, 60)
	if got := v.canvas.Scale(); math.Abs(got-wantCraft) > wantCraft*1e-9 {
		t.Errorf("own-craft framing not restored: scale %.6e != craft fit %.6e", got, wantCraft)
	}
	if math.Abs(v.canvas.Scale()-spectateScale) <= spectateScale*1e-9 {
		t.Error("focus-return left the camera at the spectate frame — no re-fit happened")
	}

	// [F] (CycleFocus back) and [g] (ResetFocus) also leave a ghost focus.
	w.SpectateGhost(g.Owner, g.CraftID)
	w.CycleFocus(false)
	if w.Focus.Kind == sim.FocusGhost {
		t.Error("[F] did not exit Spectate")
	}
	w.SpectateGhost(g.Owner, g.CraftID)
	w.ResetFocus()
	if w.Focus.Kind != sim.FocusSystem {
		t.Errorf("[g] did not reset a ghost focus to system: %+v", w.Focus)
	}
}
