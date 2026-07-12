package relay

import (
	"math"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Heartbeat is the maximum quiet interval (wall clock) between a
// session's reports. Change detection below catches every orbit
// change on the tick it happens, so the heartbeat's only job is
// bounding subspace-time staleness for the roster's Δt display — a
// coasting ghost's ORBIT is exact regardless (Kepler evaluation).
// 5s keeps Δt readable without meaningful traffic; a playtest may
// move it (v0.27 plan: tunable const with rationale).
const Heartbeat = 5 * time.Second

// Element-change tolerances for "did the orbit actually change".
// State vectors move every tick along a coast, but the derived
// Keplerian elements are constant apart from integrator drift —
// Verlet truncation wobbles them slightly, so exact comparison would
// re-report every tick. These bounds sit well above that drift and
// well below anything a real burn, stage, or SOI transition does.
const (
	relTolA   = 1e-6 // semimajor axis, relative
	absTolE   = 1e-6 // eccentricity, absolute
	absTolAng = 1e-6 // i / Ω / ω, radians
)

// Reporter watches one session's World and reports its craft set on
// element-changing events (burn end, staging, SOI transition — all
// of which move the derived elements) plus the heartbeat. It carries
// no goroutine: the session's own tick loop drives Tick, so reports
// happen at tick boundaries with no cross-goroutine World access.
type Reporter struct {
	Owner string

	store    *Store
	lastWall time.Time
	lastKeys []craftKey
}

// craftKey is the per-craft change-detection signature.
type craftKey struct {
	id      uint64
	system  string
	primary string
	landed  bool
	el      orbital.Elements
}

func NewReporter(store *Store, owner string) *Reporter {
	return &Reporter{Owner: owner, store: store}
}

// Tick inspects the world and reports if anything orbit-shaped
// changed or the heartbeat elapsed. now is wall clock (heartbeat
// cadence must not warp with sim time).
func (r *Reporter) Tick(w *sim.World, now time.Time) {
	keys, states := snapshotWorld(w)
	due := r.lastWall.IsZero() || now.Sub(r.lastWall) >= Heartbeat
	if !due && keysEqual(r.lastKeys, keys) {
		return
	}
	r.lastWall = now
	r.lastKeys = keys
	r.store.Report(CraftReport{
		Owner:        r.Owner,
		SubspaceTime: w.Clock.SimTime,
		Crafts:       states,
	})
}

// snapshotWorld converts the world's craft slate into wire states
// plus change-detection keys. Craft state is primary-relative already
// (spacecraft convention), so it goes onto the wire as-is.
func snapshotWorld(w *sim.World) ([]craftKey, []CraftState) {
	keys := make([]craftKey, 0, len(w.Crafts))
	states := make([]CraftState, 0, len(w.Crafts))
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		system := ""
		if c.SystemIdx >= 0 && c.SystemIdx < len(w.Systems) {
			system = w.Systems[c.SystemIdx].Name
		}
		var el orbital.Elements
		if !c.Landed {
			el = orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
		}
		keys = append(keys, craftKey{
			id:      c.ID,
			system:  system,
			primary: c.Primary.ID,
			landed:  c.Landed,
			el:      el,
		})
		states = append(states, CraftState{
			ID:      c.ID,
			Name:    c.Name,
			Glyph:   c.Glyph,
			System:  system,
			Primary: c.Primary.ID,
			R:       c.State.R,
			V:       c.State.V,
			Landed:  c.Landed,
		})
	}
	return keys, states
}

func keysEqual(a, b []craftKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].id != b[i].id || a[i].system != b[i].system ||
			a[i].primary != b[i].primary || a[i].landed != b[i].landed {
			return false
		}
		if !elementsClose(a[i].el, b[i].el) {
			return false
		}
	}
	return true
}

func elementsClose(x, y orbital.Elements) bool {
	scaleA := math.Max(math.Abs(x.A), math.Abs(y.A))
	if scaleA > 0 && math.Abs(x.A-y.A) > relTolA*scaleA {
		return false
	}
	return math.Abs(x.E-y.E) <= absTolE &&
		math.Abs(x.I-y.I) <= absTolAng &&
		math.Abs(x.Omega-y.Omega) <= absTolAng &&
		math.Abs(x.Arg-y.Arg) <= absTolAng
}
