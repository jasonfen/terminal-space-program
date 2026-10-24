package spacecraft

import "time"

// RaiseOrder is a commanded spiral to TargetAltM (ADR 0059 decision 4).
// A StartAt in the future means the order is waiting (the spread uses it);
// the tick starts it once SimTime >= StartAt. BatchID names the launch batch
// that issued it, 0 for a single vessel.
type RaiseOrder struct {
	TargetAltM float64
	StartAt    time.Time
	Started    bool
	BatchID    uint64
}
