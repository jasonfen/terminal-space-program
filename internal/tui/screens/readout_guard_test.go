package screens

import (
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
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
//
// Isp (specific impulse) is explicitly exempted from the `%.0fs` pattern,
// by name, via isIspException below, not by narrowing the pattern itself
// (gate review F3): specific impulse is measured in seconds as its own
// unit, not a duration readout this contract governs, but an earlier
// pass "fixed" the two sites that print it glued to a kN thrust figure
// (spawn.go's part-picker summaries, vab_render.go's stage header) by
// rewriting `"%.0fkN @ %.0fs"` as `"%.0fkN @ %.0f" + "s"`, byte-identical
// output, whose only effect was hiding the field from this exact grep.
// That is a worse problem than the one it dodged: a documented bypass
// idiom any future raw-seconds readout could copy. Both files are
// reverted to main (verified: `git diff origin/main` on both is empty);
// the honest fix is this visible, named exemption instead.
//
// isIspException used to key off the LINE's text (`.Isp` or `Isp `
// appearing anywhere on it, comment included), which a second review
// pass found trivially spoofable: `fmt.Sprintf("... %.0fs", a) // Isp`
// passes regardless of what `a` actually is, since a trailing comment
// is not a safe key: anyone can write one. Re-keyed to an explicit
// file:line allowlist (ispExceptionLines) of the real Isp sites, with
// the old text check kept only as a second, AND'd guard against a
// stale entry: if code moves and the allowlisted line number no
// longer actually prints Isp, the exception no longer applies and the
// line fails loudly instead of silently exempting whatever now sits
// there.
//
// Scope widened to all of internal/tui (F11, gate review): the guard used
// to scan only its own package directory, and app.go's rendezvous-nudge
// flash carried the exact T+/T- inversion decision 2 exists to remove,
// undetected for the whole PR because it lived one directory up. Walks
// internal/tui recursively rather than just internal/tui/screens, and
// explicitly skips internal/tui/readout: that package IS the contract's
// implementation, so its own format verbs (e.g. Thrust's "%.0f kN") are
// not player-facing dialect survivors, they're the fix.
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

	root := ".." // internal/tui: this test's own working directory is internal/tui/screens
	var srcFiles []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "readout" {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		srcFiles = append(srcFiles, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir(%q): %v", root, err)
	}
	if len(srcFiles) == 0 {
		t.Fatal("no non-test .go files found under internal/tui, guard scope is empty, check the working directory")
	}

	for _, path := range srcFiles {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(%q): %v", path, err)
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			for _, p := range patterns {
				if !strings.Contains(line, p) {
					continue
				}
				if isIspException(p, path, i+1, line) {
					continue
				}
				t.Errorf("%s:%d: found old-dialect pattern %q, route this readout through internal/tui/readout instead", path, i+1, p)
			}
		}
	}
}

// ispExceptionLines is the exhaustive allowlist of source lines where
// `%.0fs` is specific impulse, keyed "basename.go:line", not a
// trailing comment, which any line could carry regardless of what it
// prints. Four real sites as of this PR: vab_render.go's stage header
// and spawn.go's two per-stage engine summaries pass a `.Isp` struct
// field as the format argument on the same line; spawn.go's scale-hint
// summary spells "Isp" directly in the format string. Update this map,
// not isIspException, when a real Isp call site moves or a new one is
// added.
var ispExceptionLines = map[string]bool{
	"vab_render.go:493": true,
	"spawn.go:1098":     true,
	"spawn.go:1118":     true,
	"spawn.go:1488":     true,
}

// isIspException reports whether a `%.0fs` hit at path:lineNo is
// specific impulse (Isp), not a raw-seconds duration reading. Keyed by
// file:line against ispExceptionLines (gate review: a trailing `//
// Isp` comment is spoofable, a line number naming a specific,
// reviewed call site is not), then double-checked against the line's
// own code (comment stripped) for the field or word an Isp line
// actually prints, so a stale allowlist entry (code moved, a
// different line now sits at that number) fails loudly instead of
// silently exempting whatever is there now.
func isIspException(pattern, path string, lineNo int, line string) bool {
	if pattern != `%.0fs` {
		return false
	}
	if !ispExceptionLines[filepath.Base(path)+":"+strconv.Itoa(lineNo)] {
		return false
	}
	code := line
	if i := strings.Index(code, "//"); i >= 0 {
		code = code[:i]
	}
	return strings.Contains(code, ".Isp") || strings.Contains(code, "Isp ")
}
