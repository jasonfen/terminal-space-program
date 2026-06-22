package save

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// TestMigrateV8PayloadToV9DropsOldMissions — ADR 0025. A pre-v9 payload's
// Missions are the old single-predicate shape (no nested objectives). The
// Mission shape changed fundamentally, so the migration drops the
// low-value old progress, leaving Missions nil so worldFromPayload
// reseeds from the new catalog rather than rehydrating empty husks.
func TestMigrateV8PayloadToV9DropsOldMissions(t *testing.T) {
	p := &Payload{
		Missions: []missions.Mission{
			{ID: "old-circ", Name: "Old circularize", Status: missions.Passed},
			{ID: "old-flyby", Name: "Old flyby", Status: missions.InProgress},
		},
	}
	migrateV8PayloadToV9(p)
	if p.Missions != nil {
		t.Fatalf("expected Missions dropped to nil, got %d", len(p.Missions))
	}
}
