package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// Wave B review fix (REVIEW MEDIUM 2): while a Rendezvous Burn from the picker
// is planted, or was the last one flown toward the current target, the TARGET
// chip's last row reads the PLAN's arrival and predicted separation, labelled
// "plan:" / "miss:" so the pilot can tell it is not the 4 h closest-approach
// search (which reads a pass inside its window, not a rendezvous further out).
func TestTargetBox_ReadsThePlannedRendezvous(t *testing.T) {
	v := NewOrbitView(launchThemeForTest())
	w := leadTestWorld(t, 82)
	rows := func() string { return ansi.Strip(strings.Join(v.buildTargetBox(w), "\n")) }

	base := rows()
	if !strings.Contains(base, "TCA:") || !strings.Contains(base, "approach:") || strings.Contains(base, "plan:") {
		t.Fatalf("no plan: want the closest-approach row, got:\n%s", base)
	}

	c := w.ActiveCraft()
	now := w.Clock.SimTime
	c.RendezvousPlan = &spacecraft.RendezvousPlan{
		TriggerTime:   now.Add(-time.Minute), // burn already flown
		ArrivalTime:   now.Add(6*time.Hour + 30*time.Minute),
		SeparationM:   120,
		TargetCraftID: w.Target.CraftID,
	}
	got := rows()
	for _, want := range []string{"plan:", "T-6h30m", "miss:", "120"} {
		if !strings.Contains(got, want) {
			t.Errorf("planned row lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "TCA:") || strings.Contains(got, "approach:") {
		t.Errorf("plan row should replace the closest-approach labels:\n%s", got)
	}

	// Arrival passed: back to the closest-approach row.
	c.RendezvousPlan.ArrivalTime = now.Add(-time.Second)
	if after := rows(); !strings.Contains(after, "TCA:") || strings.Contains(after, "plan:") {
		t.Errorf("expired plan still shown:\n%s", after)
	}
}
