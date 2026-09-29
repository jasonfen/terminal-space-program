package screens

import (
	"regexp"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// orbit_empty_readings.go: the Empty readings setting's row folding (ADR
// 0051 W6, #482). Which boxes fold under Full / Tidy / Compact is decided
// in navigationBoxesInOrder; this file only knows how to drop a box's
// trailing dash rows.

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// multiWordLabels are the row labels that contain a space, so tokenising
// on whitespace would split them and leave a word that does not end in
// ":". Every label a builder passes to chipRow* must appear here if it has
// a space; TestMultiWordLabelsListIsComplete reads the source to prove it.
var multiWordLabels = []string{
	readout.LabelOrbitFPA, // "orbit fpa:"
	readout.LabelRelSpeed, // "rel speed:"
	"τ in:",               // rendezvous row (orbit_chip_builders.go)
}

// isDashRow reports whether a rendered box row carries no reading: at
// least one "—" value and nothing else but labels (tokens ending in ":",
// plus the two-word labels in multiWordLabels).
func isDashRow(line string) bool {
	line = ansiSeq.ReplaceAllString(line, "")
	for _, l := range multiWordLabels {
		line = strings.ReplaceAll(line, l, "")
	}
	toks := strings.Fields(line)
	dashes := 0
	for _, t := range toks {
		switch {
		case t == "—":
			dashes++
		case strings.HasSuffix(t, ":"):
		default:
			return false
		}
	}
	return dashes > 0
}

// foldTrailingDashRows drops dash rows from the BOTTOM of a box only,
// stopping at the first row with a reading, so a label above a folded row
// never shifts and a dash row in the middle stays. The title (row 0) is
// never dropped.
func foldTrailingDashRows(lines []string) []string {
	n := len(lines)
	for n > 1 && isDashRow(lines[n-1]) {
		n--
	}
	return lines[:n]
}
