package screens

import (
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// GK Q4 (#589): the VAB's key list in plain words, same keys; a third KEYS row
// is accepted. At 140x40 the vehicle column stays usable.
func TestVABFooterUsesPlainWords(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	out := v.Render(140, 40)
	for _, want := range []string{
		"[tab] other column", "[←/→] change part", "[PgUp/Dn] jump section",
		"[+/-] count", "[ [ / ] ] move stage down / up", "[y] copy stage",
		"[enter] open part into pieces", "[d] payload from here up",
		"[c] drops with stage below", "[t] Δv goal", "[s] save", "[o] open",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("KEYS missing %q:\n%s", want, out)
		}
	}
	for _, gone := range []string{"crack part", "dock seam", "[c] fuse", "] swap", "[+/−] qty", "duplicate", "reorder"} {
		if strings.Contains(out, gone) {
			t.Errorf("KEYS still uses the builder's private word %q", gone)
		}
	}
	// No KEYS row is cut off or wrapped mid-hint at the Design Size, and the
	// vehicle column still shows its engine row and the first stage.
	for _, ln := range strings.Split(out, "\n") {
		if w := len([]rune(ln)); w > 140 {
			t.Errorf("row is %d wide, over the 140 Design Size: %q", w, ln)
		}
	}
	if !strings.Contains(out, "S1") || !strings.Contains(out, "engine") {
		t.Errorf("the vehicle column lost its stage rows:\n%s", out)
	}
	if n := len(strings.Split(strings.TrimRight(out, "\n"), "\n")); n != 40 {
		t.Errorf("screen is %d rows, want 40", n)
	}
}

// The two grammar slips (GK fix-without-asking).
func TestVABGrammar(t *testing.T) {
	for _, c := range []struct {
		kind, want string
	}{{"engine", "an engine"}, {"tank", "a tank"}, {"core", "a core"}} {
		if got := aOrAn(c.kind) + " " + c.kind; got != c.want {
			t.Errorf("article for %q = %q, want %q", c.kind, got, c.want)
		}
	}
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	if out := v.Render(140, 40); strings.Contains(out, "picks a engine") {
		t.Errorf("INSPECT still says 'picks a engine':\n%s", out)
	}
	if got := stageCount(1); got != "1 stage" {
		t.Errorf("stageCount(1) = %q", got)
	}
	if got := stageCount(3); got != "3 stages" {
		t.Errorf("stageCount(3) = %q", got)
	}
}

// GK Q2 (#589): x in the open-design list asks first, in the saves voice.
// Enter does nothing (Q7), n / esc keep the file, only y deletes.
func TestVABDesignDeleteAsksFirst(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.addComponentToCurrent("eng")
	v.name = "Moon Lifter"
	if err := spacecraft.SaveDesign(v.toDesign()); err != nil {
		t.Fatalf("SaveDesign: %v", err)
	}
	onDisk := func() int { v.refreshDesigns(); return len(v.designs) }
	v.enterLoadMode()
	if onDisk() != 1 {
		t.Fatalf("setup: %d designs on disk, want 1", onDisk())
	}
	v.enterLoadMode()

	v.HandleKey("x")
	out := v.Render(140, 40)
	if !strings.Contains(out, "delete design 'Moon Lifter'? This cannot be undone. [y/n]") {
		t.Errorf("x raised no ask:\n%s", out)
	}
	if onDisk() != 1 {
		t.Fatal("x deleted the design before any answer")
	}
	v.mode = vabModeLoad
	for _, k := range []string{"enter", "w", "x", "up"} {
		v.HandleKey(k)
		if !strings.Contains(v.Render(140, 40), "[y/n]") || onDisk() != 1 || v.mode != vabModeLoad {
			t.Fatalf("%q answered or dismissed the ask (designs %d)", k, onDisk())
		}
	}
	for _, k := range []string{"n", "esc"} {
		v.HandleKey(k)
		if strings.Contains(v.Render(140, 40), "[y/n]") || onDisk() != 1 {
			t.Fatalf("%q did not cancel cleanly (designs %d)", k, onDisk())
		}
		if v.mode != vabModeLoad {
			t.Fatalf("%q left the design list (mode %v), it should only drop the ask", k, v.mode)
		}
		v.HandleKey("x")
	}
	v.HandleKey("y")
	if onDisk() != 0 {
		t.Error("y did not delete the design")
	}
}
