package sim

import (
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Safe handback: the shared shape of returning someone their craft.
//
// ADR 0040 §3 needs it first, for Parcels — a component the owner released
// while the guest was not there to catch it. ADR 0038 §3/§4 then generalises
// the same two helpers to live undocks, which is why they live here rather
// than inside the Parcel path: build them once, in the layer both sides of
// the seam can reach.
//
// The two rules they encode were both measured on the 2026-08-02 dock
// session (#303, #304): a returned craft that wears the OTHER pilot's flight
// state can fire or swing on its own metres from a stack, and a returned
// craft placed at the composite's frozen state materialises in the wrong
// place — the pair silently re-fused nine seconds after undocking.

// SafeHandback puts a returned craft into the known-inert configuration every
// handback ends in: throttle zero, main engine selected, no attitude hold, no
// burn of any kind running. The same invariant every time, so "undock = safe
// ship, set up and go" is a rule a pilot can fly by — rather than inheriting
// whatever the other seat happened to have dialled in for a different
// situation (#303: 10% throttle, RCS, Target Retrograde, on a craft the pilot
// had left at 100%/main/prograde).
func SafeHandback(c *spacecraft.Spacecraft) {
	if c == nil {
		return
	}
	c.Throttle = 0
	c.EngineMode = spacecraft.EngineMain
	// BurnPrograde is the neutral default a fresh craft carries; there is no
	// separate "hold off" state, so this is what no-attitude-hold means.
	c.AttitudeMode = spacecraft.BurnPrograde
	c.RCSFineLevel = 0
	c.ActiveBurn = nil
	c.ManualBurn = nil
}

// SeparationPush nudges a returned craft clear of the stack it just left —
// 75 m and 0.15 m/s radially out, deliberately outside BOTH docking gates so
// neither proximity nor closing rate can re-fuse the pair while the pilot is
// still setting the safed ship up. Same magnitudes Deploy uses for the same
// reason; shared so the cross-player return path can't drift from the local
// one (#304: the cross-player path had no push at all, and the pair re-fused
// nine seconds after the undock).
func SeparationPush(c *spacecraft.Spacecraft) {
	if c == nil {
		return
	}
	const (
		separationM = DockingDistM * 1.5 // 75 m > the 50 m proximity gate
		pushVMS     = DockingVMS * 1.5   // 0.15 m/s > the 0.1 m/s velocity gate
	)
	out := radialOutUnit(c)
	c.State.R = c.State.R.Add(out.Scale(separationM))
	c.State.V = c.State.V.Add(out.Scale(pushVMS))
}

// PlaceAcrossSubspaceGap Kepler-propagates a craft's primary-relative state by
// dt seconds — forward or backward — so a craft handed between two players'
// Worlds materialises where it actually is at the RECEIVER's sim-time rather
// than at the sender's. At LEO speed a subspace skew of one second is ~7.6 km
// of error, and a Parcel can sit undelivered for hours.
//
// Same analytic step the ghost layer already uses for reports, and it carries
// the same honest staleness: it neither detects SOI exits nor knows about
// anything that would have happened to the craft in the gap. It cannot, and
// nothing else could either — the craft was coasting, unsimulated, on a
// ledger record. ok is false for a degenerate state, in which case the craft
// is left exactly where it was: no propagation beats a wrong one.
func PlaceAcrossSubspaceGap(c *spacecraft.Spacecraft, dtSeconds float64) bool {
	if c == nil || dtSeconds == 0 {
		return c != nil
	}
	st, ok := physics.KeplerStep(c.State, c.Primary.GravitationalParameter(), dtSeconds)
	if !ok {
		return false
	}
	c.State.R, c.State.V = st.R, st.V
	return true
}

// StackGuestOwner names the single cross-player guest riding in a stack, if
// there is one — the 2-party MVP's whole cross-player membership. ok is false
// for a plain composite. Used by the owner-seat release, which has to know
// whose components it is being asked to peel.
func StackGuestOwner(c *spacecraft.Spacecraft) (string, bool) {
	if c == nil {
		return "", false
	}
	for _, dc := range c.DockedComponents {
		if dc.Owner != "" {
			return dc.Owner, true
		}
	}
	return "", false
}

// GuestReleaseRefusal says, in the player's words, why the owner of the stack
// at idx cannot release its guest's component — or "" when the release will
// go through. It is the owner-seat sibling of UndockRefusal: UndockRefusal
// guards the LOCAL split (which must never touch a cross-player stack, since
// splitting locally would clone the guest's craft into this World), while this
// guards the release that travels through the dock ledger.
//
// ADR 0040 §3 makes the docker able to release regardless of guest presence —
// "you can always get your own ship back to yourself" — and §5 keeps #314's
// one exception: after a control transfer the guest's components sit at the
// BOTTOM of the stack, where a tail peel would hand each player the other's
// hardware. That refusal names the way out rather than merely refusing.
func (w *World) GuestReleaseRefusal(idx int) string {
	if idx < 0 || idx >= len(w.Crafts) || w.Crafts[idx] == nil {
		return "release: no vessel selected"
	}
	c := w.Crafts[idx]
	guestOwner, ok := StackGuestOwner(c)
	if !ok {
		return "release: this vessel carries nobody else's vessel"
	}
	if _, _, _, ok := guestTopBlock(c, guestOwner, 0); !ok {
		return "release: their vessel sits under yours — hand control back [J], then they release"
	}
	return ""
}
