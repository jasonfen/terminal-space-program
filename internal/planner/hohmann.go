package planner

import "errors"

// ErrNotImplemented is returned by Phase-3 stubs that slip past v0.1 per
// plan §MVP deferrals.
var ErrNotImplemented = errors.New("planner: not implemented in v0.1 (deferred to v0.2)")

// HohmannTransfer would compute Δv1, Δv2, and transfer time for a
// circular-to-circular coplanar Hohmann transfer. Stubbed for v0.1; the
// math is ~10 lines but the UI integration isn't ready.
func HohmannTransfer(r1, r2, mu float64) (dv1, dv2, tTransfer float64, err error) {
	return 0, 0, 0, ErrNotImplemented
}
