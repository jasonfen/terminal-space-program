package screens

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadoutDialectGuard is the ADR 0049 stage A2 guard: it greps this
// package's own non-test source for survivors of the pre-contract number
// dialect the internal/tui/readout package (stage A1) replaces, and fails
// on any hit. The first five patterns come straight off the ADR's own
// before examples: `%.0fs` / `%.2fh` (raw-seconds and decimal-hour
// durations), `km alt` (the dropped `alt` suffix), `_vert` (the
// v_vert/v_horiz underscore labels) and `|v_rel|` (the absolute-value-bars
// label).
//
// The next five were added on gate review, once the audit went against
// the rename table and the tree rather than the first pass's own report:
// the ADR's own list was blind to them, so they lived on past the first
// five going to zero. `v_z` (the launch strip's third spelling of vert:'s
// quantity), `Δi:` (the un-renamed sibling of Δincl:), `t→` (the ORBIT
// chip's apo:/peri: countdowns spelled with an arrow instead of the
// signed T- convention every other countdown uses), a bare `downrange %.`
// (the launch strip's off-ladder distance), and `%.1f°` (orbital and
// geographic angles bypassing readout.Angle's 4-significant-figure rule).
//
// Two more were added at stage A3b's render-and-read pass, found only by
// rendering real captures and reading every row rather than by grepping
// for the ADR's own named examples: `%.1fs` (the ATTITUDE chip's
// `manual:` row — a raw-seconds-with-a-decimal duration one digit finer
// than the first five patterns' `%.0fs`, so it slipped through both
// passes) and `%.0f N` (the maneuver planner's `thrust:`/`Isp:` summary
// row printing raw Newtons two lines below its own `burnDescr` line's
// correctly-converted `at 1023 kN` for the same c.Thrust value).
//
// Deliberately does NOT scan _test.go files (a pinned test string, or a
// %g-style assertion helper, isn't a player-facing readout) and does NOT
// include `(locked)`: PR B (the heading-trim / #453 amendment) owns the
// pad row and that string is meant to survive this PR.
func TestReadoutDialectGuard(t *testing.T) {
	patterns := []string{
		`%.0fs`,
		`%.2fh`,
		`km alt`,
		`_vert`,
		`|v_rel|`,
		`v_z`,
		`Δi:`,
		`t→`,
		`downrange %.`,
		`%.1f°`,
		`%.1fs`,
		`%.0f N`,
	}

	dir := "."
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", dir, err)
	}

	var srcFiles []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		srcFiles = append(srcFiles, name)
	}
	if len(srcFiles) == 0 {
		t.Fatal("no non-test .go files found in internal/tui/screens, guard scope is empty, check the working directory")
	}

	for _, name := range srcFiles {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		content := string(data)
		for _, p := range patterns {
			if strings.Contains(content, p) {
				lineNo := 1 + strings.Count(content[:strings.Index(content, p)], "\n")
				t.Errorf("%s:%d: found old-dialect pattern %q, route this readout through internal/tui/readout instead", name, lineNo, p)
			}
		}
	}
}
