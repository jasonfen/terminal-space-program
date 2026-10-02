package spacecraft

import "time"

// RendezvousPlan is the standing record of the last Rendezvous Burn the pilot
// planted from the Rendezvous Planner picker (Wave B review MEDIUM 2). The
// planted node vanishes when it fires, but the plan it carried (arrive at
// ArrivalTime, SeparationM from the target) is still what the pilot is flying
// toward, so the TARGET chip reads it instead of the 4 h closest-approach
// search, which cannot see a wait longer than its window.
//
// A reading is offered only while ArrivalTime is in the future, the target is
// the one the plan was made against, and either the planted node (NodeID)
// still exists or it Fired. The record is cleared (nil) as soon as the plan
// stops being the vessel's course: any other burn starts (manual, another
// node), the planted burn is cut short of its Δv, or the node is lost
// without firing. Persists in saves (schema v14).
type RendezvousPlan struct {
	NodeID           uint64
	TriggerTime      time.Time
	ArrivalTime      time.Time
	SeparationM      float64 // the row's predicted separation at ArrivalTime
	TargetCraftID    uint64
	TargetGhostOwner string

	// Fired: the planted node was dispatched (impulsive, or its finite burn
	// started). BurnActive: that finite burn is still in flight.
	Fired      bool
	BurnActive bool
}
