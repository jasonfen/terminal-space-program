package tui

import (
	"os"
	"strings"
	"testing"
)

// Review LOW 123 (docs half): controls.md says the arrow trims work "in every
// flight view", so it must also say who takes the arrows instead (the picker,
// in its own section), or the two statements read as a contradiction.
func TestControlsDocSaysWhoOwnsTheArrowsInsteadOfTheTrims(t *testing.T) {
	b, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	if strings.Contains(doc, "in every view |") {
		t.Error(`controls.md still says the arrows trim "in every view" without the modal exceptions`)
	}
	i := strings.Index(doc, "### Rendezvous Planner (`K`)")
	j := strings.Index(doc, "### Vehicle Assembly Building")
	if i < 0 || j < i || !strings.Contains(doc[i:j], "holds the plain arrows") {
		t.Error("the Rendezvous Planner section does not say it holds the plain arrows (no trims, no pan) while open")
	}
}
