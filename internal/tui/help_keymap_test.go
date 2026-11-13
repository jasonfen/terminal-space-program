package tui

import (
	"os"
	"strings"
	"testing"
)

// TestControlsDocNamesThrottleTarget (ADR 0058 decision 14 / guard 15.[13]):
// docs/controls.md carries the `c` throttle-target row and the "selected
// throttle" wording on the throttle keys, mirroring F1.
func TestControlsDocNamesThrottleTarget(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `c` | Throttle target", "selected throttle", "never a lit solid"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}

// TestControlsDocNamesBatchRow (ADR 0059 decision 9): docs/controls.md mirrors
// F1's VESSEL `batch` row word for word and says what `t` does with a batch.
func TestControlsDocNamesBatchRow(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| `batch` | U releases a stack of payloads at once; it is one row in the target menu. About 30 vessels per player keeps the map quick |",
		"a released batch is one row (its nearest member)",
		// ADR 0066 S2: the target menu rows and the quick start.
		"| `t` / `T` | Open the target menu / clear your target. The menu lists VESSELS, SITES on this body, PARTNERS and BODIES",
		"| `r` / `d` (target menu) |",
		"| `+` / `-` (target menu) | On a flag you own: `+` adds an LZ (nine at most)",
		"| `←` / `→` (target menu) | On a site with more than one LZ: choose which, `auto` then `LZ-1` up.",
		// ADR 0066 S3: the y card and the flag and wreck markers.
		"| `F3` | **Surface marks** card: show or hide landed vessels (`v`), flags (`f`), wrecks (`w`), partners' marks (`p`) and coverage now (`c`)",
		"| `⚑` | a flag: where a vessel touched down",
		"| `☠` | a wreck: where a vessel crashed",
		// ADR 0066 S9: the COVERAGE notice follows the target; the overlay row.
		"| `COVERAGE` | The notice for the site or landed vessel you aimed at, or for the lunar coverage rung",
		"tinted green where the ground is linked this instant (`F3` `c` switches it off)",
		"Press `t` twice to target the Moon",
		"| `recover:` heavy | Falcon Heavy: run the core at 50 to 70 % until BECO, or no boostback closes |",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}

// TestControlsMdHasRecordingWindowRow (ADR 0062 decision 9): docs/controls.md
// carries the Settings Recording window row. New function: no existing
// test pins Settings rows of controls.md by name.
func TestControlsMdHasRecordingWindowRow(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| Settings → **Recording window** | How much of a vessel's Flight Recording is kept: sim-days, default 7; older rows are dropped from the file as it is written |"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}

// ADR 0063 §1: docs/controls.md carries the `A` Inertial row and the
// Port-hold fallback sentence. New function: each controls.md row here is
// pinned by its own test.
func TestControlsDocNamesInertialHold(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `A` | Hold the nose where it points now (`hold: Inertial (ORBIT)`)", "the arrows then turn the held nose instead of trimming", "a Port hold goes to Inertial with it"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}

// ADR 0063 §4/§8: the `U` row and the Undocking paragraph of docs/controls.md
// say newest seam, 0.05 m/s and the 5 m re-arm, and no longer 0.15 m/s / 100 m.
func TestControlsDocUndockRowsAreCurrent(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"| `U` | Undock the newest seam", "0.05 m/s relative, shared by mass", "opening\n  at 0.05 m/s. The pair cannot re-dock until it clears 5 m"} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
	for _, stale := range []string{"0.15 m/s", "clears 100 m"} {
		if strings.Contains(string(doc), stale) {
			t.Errorf("docs/controls.md still says %q", stale)
		}
	}
}

// TestControlsDocNamesApsisWarpButton: docs/controls.md mirrors F1's
// MOUSE row for the » after NAVIGATION's Ap / Pe.
func TestControlsDocNamesApsisWarpButton(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"| `»` after `Ap` / `Pe` (NAVIGATION) | Warp to 30 s before that apsis",
		"| `alt+g` | Warp to 30 s before the nearer apsis",
		"On an open chute `space` cuts it, for good",
		"| `alt+s` | Reset the camera to the whole system",
		"| ground | Coasting into the ground (engine off, periapsis below the surface), warp glides down",
	} {
		if !strings.Contains(string(doc), want) {
			t.Errorf("docs/controls.md lacks %q", want)
		}
	}
}

// TestControlsDocNamesBrakeCueWarp (fix I): the G row says it warps to the
// brake cue on a descent. New function: no existing controls.md pin covers G.
func TestControlsDocNamesBrakeCueWarp(t *testing.T) {
	doc, err := os.ReadFile("../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	if want := "warps to 30 s before the brake cue"; !strings.Contains(string(doc), want) {
		t.Errorf("docs/controls.md lacks %q", want)
	}
}
