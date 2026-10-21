package relay

import "github.com/jasonfen/terminal-space-program/internal/sim"

// AckPersisted is called after a successful durable write of seat's World w.
// STUB: the real body lands with the tests (ADR 0040 amendment, 10a A1).
func (l *DockLedger) AckPersisted(seat string, w *sim.World) bool { return false }
