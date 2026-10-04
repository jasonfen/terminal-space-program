package sim

import (
	"math"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// planeCrossingResult is the craft's two crossings of a target plane as
// primary-relative positions (the frame the craft's own orbit lives in).
type planeCrossingResult struct {
	anRel, dnRel         orbital.Vec3
	anPrimary, dnPrimary bodies.CelestialBody
	hasAN, hasDN         bool
}

// TargetPlaneSolves counts the crossing solves that actually ran on this
// World (the call count the cache test asserts on).
func (w *World) TargetPlaneSolves() int { return w.tpnSolves }

// planeCrossingCache memoises one planeCrossings result. The crossings are
// points ON the craft's orbit, stored primary-relative, so they depend on
// the orbit and the target plane and not on the clock: a coasting craft
// keeps its orbit while its state moves along it. The key is therefore the
// orbit itself (angular momentum and eccentricity vectors, compared with a
// tolerance because Verlet drifts them in the last digits), the target
// plane normal, the craft and its primary. A burn, an SOI change or a new
// target changes one of those and forces a solve.
type planeCrossingCache struct {
	valid   bool
	craftID uint64
	primary string
	h, e    orbital.Vec3
	n       orbital.Vec3
	val     planeCrossingResult
}

const (
	planeCacheHTol = 1e-6 // relative, on |h|
	planeCacheETol = 1e-6 // absolute, on the eccentricity vector
	planeCacheNTol = 1e-7 // absolute, on the unit plane normal
)

func orbitInvariants(c *spacecraft.Spacecraft, mu float64) (h, e orbital.Vec3) {
	h = c.State.R.Cross(c.State.V)
	e = c.State.V.Cross(h).Scale(1 / mu).Sub(c.State.R.Scale(1 / c.State.R.Norm()))
	return h, e
}

func (k *planeCrossingCache) matches(c *spacecraft.Spacecraft, h, e, n orbital.Vec3) bool {
	if !k.valid || k.craftID != c.ID || k.primary != c.Primary.ID {
		return false
	}
	hn := k.h.Norm()
	return hn > 0 &&
		h.Sub(k.h).Norm() <= planeCacheHTol*hn &&
		e.Sub(k.e).Norm() <= planeCacheETol &&
		n.Sub(k.n).Norm() <= planeCacheNTol
}

// planeCrossings finds the craft's AN / DN against the plane with unit
// normal nTarget: the frame build, two TimeToNodeCrossing solves and the
// SOI-aware propagation to each crossing.
func (w *World) planeCrossings(c *spacecraft.Spacecraft, nTarget orbital.Vec3) planeCrossingResult {
	mu := c.Primary.GravitationalParameter()
	var h, e orbital.Vec3
	usable := mu > 0 && c.State.R.Norm() > 0
	if usable {
		h, e = orbitInvariants(c, mu)
		if w.tpnCache.matches(c, h, e, nTarget) {
			return w.tpnCache.val
		}
	}
	w.tpnSolves++
	var r planeCrossingResult
	planeFrame := orbital.FrameFromNormal(nTarget)
	stateTF := orbital.Vec3State{
		R: planeFrame.FromWorld(c.State.R),
		V: planeFrame.FromWorld(c.State.V),
	}
	tAN := orbital.TimeToNodeCrossing(stateTF, mu, true)
	tDN := orbital.TimeToNodeCrossing(stateTF, mu, false)
	if tAN >= 0 {
		post, prim := w.propagateCraftWithPrimary(tAN)
		r.anRel, r.anPrimary, r.hasAN = post.R, prim, true
	}
	if tDN >= 0 {
		post, prim := w.propagateCraftWithPrimary(tDN)
		r.dnRel, r.dnPrimary, r.hasDN = post.R, prim, true
	}
	// Only an orbit that stayed in one primary's frame is clock-independent;
	// a crossing past an SOI boundary lands in a moving body's frame.
	if usable && !math.IsNaN(h.Norm()) &&
		(!r.hasAN || r.anPrimary.ID == c.Primary.ID) &&
		(!r.hasDN || r.dnPrimary.ID == c.Primary.ID) {
		w.tpnCache = planeCrossingCache{valid: true, craftID: c.ID, primary: c.Primary.ID,
			h: h, e: e, n: nTarget, val: r}
	}
	return r
}
