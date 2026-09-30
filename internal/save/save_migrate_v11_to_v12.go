// Schema v11 -> v12: the vocabulary settled on "rendezvous", so a planted
// Rendezvous Burn node's persisted keys were renamed:
//
//	meeting_arrival_sec -> rendezvous_arrival_sec
//	meeting_place_label -> rendezvous_orbit_label
//	meeting_laps        -> rendezvous_laps
//	advisory_key "meeting-burn" -> "rendezvous-burn" (a value, not a key)
//
// Values are unchanged. The typed Node decodes the old keys into the
// V11Meeting* shadow fields (a plain json.Unmarshal of a v11 envelope
// would otherwise drop them as unknown keys), and this pass moves them
// across.
package save

import "github.com/jasonfen/terminal-space-program/internal/sim"

// legacyMeetingAdvisoryKey is the pre-v12 advisory_key value of a planted
// Rendezvous Burn node.
const legacyMeetingAdvisoryKey = "meeting-burn"

// foldLegacyMeeting moves a decoded node's schema v11 keys into their v12
// fields and clears the shadows. A new-key value already present wins.
func (n *Node) foldLegacyMeeting() {
	if n.RendezvousArrivalSec == 0 {
		n.RendezvousArrivalSec = n.V11MeetingArrivalSec
	}
	if n.RendezvousOrbitLabel == "" {
		n.RendezvousOrbitLabel = n.V11MeetingPlaceLabel
	}
	if n.RendezvousLaps == 0 {
		n.RendezvousLaps = n.V11MeetingLaps
	}
	n.V11MeetingArrivalSec, n.V11MeetingPlaceLabel, n.V11MeetingLaps = 0, "", 0
	if n.AdvisoryKey == legacyMeetingAdvisoryKey {
		n.AdvisoryKey = sim.AdvisoryKeyRendezvousBurn
	}
}

// FoldLegacyMeetingKeys applies the v11 -> v12 rename to every node of one
// decoded craft. Exported for the server's parked dock payloads
// (session.json), which carry save.Craft outside any versioned envelope.
func (c *Craft) FoldLegacyMeetingKeys() {
	for i := range c.Nodes {
		c.Nodes[i].foldLegacyMeeting()
	}
}

// migrateV11PayloadToV12 renames the Rendezvous Burn node keys on every
// craft of a v11 payload.
func migrateV11PayloadToV12(p *Payload) {
	for i := range p.Crafts {
		p.Crafts[i].FoldLegacyMeetingKeys()
	}
}
