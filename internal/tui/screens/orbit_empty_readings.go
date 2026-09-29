package screens

import (
	"regexp"
	"strings"
)

// orbit_empty_readings.go: the Empty readings setting's row folding (ADR
// 0051 W6, #482). Which boxes fold under Full / Tidy / Compact is decided
// in navigationBoxesInOrder; this file only knows how to drop a box's
// trailing dash rows.

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// isDashRow reports whether a rendered box row carries no reading: at
// least one "—" value and nothing else but labels (tokens ending in ":").
func isDashRow(line string) bool {
	toks := strings.Fields(ansiSeq.ReplaceAllString(line, ""))
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
