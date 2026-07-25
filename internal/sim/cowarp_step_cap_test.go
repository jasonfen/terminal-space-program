package sim

import (
	"testing"
	"time"
)

// #244: the same-subspace window is a fixed span of sim-time, but warp
// moves in ×10 rungs — at the top two the sim-time one tick covers is
// wider than the window itself, so a coupled player could step clean out
// of their own subspace before the min-warp clamp (derived from the
// partner's already-stale report) could bite. Once out, ComputeCoWarp
// stops coupling them, which releases the clamp entirely.
//
// The invariant these tests pin: while coupled or armed, one tick must
// never advance more sim-time than the window can hold.

// stepSeconds is the sim-time one tick covers at the world's effective
// warp — the quantity the window has to be able to contain.
func stepSeconds(w *World) float64 {
	return w.Clock.BaseStep.Seconds() * w.EffectiveWarp()
}

// A coupled player who selects the top rung must be held to a rate whose
// per-tick advance still fits the window. The partner is reported at max
// warp too, so the ordinary min-warp clamp cannot mask the cap.
func TestCoupledWarpCannotOutstepSubspaceWindow(t *testing.T) {
	w, _, _ := anchorWorld(t)
	top := WarpFactors[len(WarpFactors)-1]
	w.Clock.WarpIdx = len(WarpFactors) - 1
	w.CoWarp = CoWarpState{Coupled: true, MinWarp: top, Partners: []string{"gern"}}

	if got := stepSeconds(w); got > coWarpSubspaceToleranceSec {
		t.Errorf("one tick advances %.0f s at %v×, wider than the %.0f s subspace window — "+
			"a coupled player can step out of their own subspace in a single tick",
			got, w.EffectiveWarp(), coWarpSubspaceToleranceSec)
	}
}

// Same bound while merely armed: an armed player waiting for their
// partner to join must stay inside the window too, or their own invite
// stops surfacing (refreshRendezvousInvite gates on sameSubspace) and the
// rendezvous dies before it starts.
func TestArmedWarpCannotOutstepSubspaceWindow(t *testing.T) {
	w, _, st := anchorWorld(t)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	w.RendezvousArm = &RendezvousArm{
		TargetOwner: "SHA256:gern",
		Handle:      "gern",
		Tau:         st.Add(4 * time.Hour),
	}

	if got := stepSeconds(w); got > coWarpSubspaceToleranceSec {
		t.Errorf("one tick advances %.0f s at %v× while armed, wider than the %.0f s window",
			got, w.EffectiveWarp(), coWarpSubspaceToleranceSec)
	}
}

// The cap is derived from BaseStep and the tolerance rather than written
// down as a rung, so it stays correct if either moves. Halving BaseStep
// must let the capped rate double.
func TestSubspaceStepCapTracksBaseStep(t *testing.T) {
	w, _, _ := anchorWorld(t)
	w.Clock.WarpIdx = len(WarpFactors) - 1
	w.CoWarp = CoWarpState{Coupled: true, MinWarp: WarpFactors[len(WarpFactors)-1]}

	coarse := w.EffectiveWarp()
	w.Clock.BaseStep /= 2
	fine := w.EffectiveWarp()

	if fine <= coarse {
		t.Errorf("halving BaseStep left the cap at %v (was %v) — cap is not derived from the step",
			fine, coarse)
	}
	if got := stepSeconds(w); got > coWarpSubspaceToleranceSec {
		t.Errorf("one tick advances %.0f s after the step change, wider than the %.0f s window",
			got, coWarpSubspaceToleranceSec)
	}
}

// Regression guard: solo play is untouched. A player neither coupled nor
// armed keeps the full ladder — the cap exists to protect a shared
// subspace, and there is no shared subspace here.
func TestSoloWarpIsNotSubspaceCapped(t *testing.T) {
	w, _, _ := anchorWorld(t)
	top := WarpFactors[len(WarpFactors)-1]
	w.Clock.WarpIdx = len(WarpFactors) - 1

	if got := w.EffectiveWarp(); got != top {
		t.Errorf("EffectiveWarp = %v, want the full %v× — solo play must not be subspace-capped", got, top)
	}
}

// The cap only ever reduces: a coupled player who selected a rate already
// inside the window keeps exactly what they chose.
func TestSubspaceCapNeverRaisesSelectedWarp(t *testing.T) {
	w, _, _ := anchorWorld(t)
	w.Clock.WarpIdx = 1 // 10×
	w.CoWarp = CoWarpState{Coupled: true, MinWarp: WarpFactors[len(WarpFactors)-1]}

	if got := w.EffectiveWarp(); got != 10 {
		t.Errorf("EffectiveWarp = %v, want the selected 10× left untouched", got)
	}
}
