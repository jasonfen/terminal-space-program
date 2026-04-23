package planner

import "github.com/jasonfen/terminal-space-program/internal/orbital"

// LambertSolve would solve Lambert's problem: given two position vectors
// and a transfer time, return the initial velocity vector. Stubbed for
// v0.1 per plan §MVP — Phase 3 scope.
func LambertSolve(r1, r2 orbital.Vec3, dt float64, mu float64) (v1, v2 orbital.Vec3, err error) {
	return orbital.Vec3{}, orbital.Vec3{}, ErrNotImplemented
}
