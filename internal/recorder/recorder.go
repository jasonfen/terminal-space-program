// Package recorder writes a vessel's Flight Recording: one text file of
// fixed-column rows, appended as things happen and pruned in place to a
// rolling window of sim-days (ADR 0062).
package recorder

import (
	"bytes"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/jasonfen/terminal-space-program/internal/atomicfile"
)

const (
	dateLayout   = "2006-01-02 15:04:05"
	defaultDays  = 7
	pruneEvery   = 60 * time.Second
	pruneSlack   = 24 * time.Hour
	day          = 24 * time.Hour
	headerSuffix = " · alt apo peri km above surface · spd m/s vs soi body · trims deg"
)

// rewrite is the prune's atomic write; a var so a test can kill it between
// the tmp file and the rename.
var rewrite = atomicfile.Write

// Row is one line of the recording. Sim is the sim-clock instant.
type Row struct {
	Sim                       time.Time
	TPlus                     time.Duration
	TPlusKnown                bool
	AltM, SpdMS, MassKg       float64
	ThrottlePct               float64
	Stages                    int
	SOI, Hold                 string
	PitchTrimDeg, HeadTrimDeg float64
	ApoM, PeriM               float64
	ApsidesOK                 bool
	Event                     string
}

// Header describes the vessel; it is written once, when the file is created.
type Header struct {
	VesselID                    uint64
	Name, System, Primary, Site string
	Liftoff                     time.Time
	LiftoffKnown                bool
}

// Recording is one open recording file.
type Recording struct {
	path      string
	days      int
	now       func() time.Time
	f         *os.File
	oldest    time.Time // zero when the file holds no rows
	newest    time.Time
	lastPrune time.Time
}

// Open creates or reopens path (writing the header on creation), prunes it
// once without the rate limit, anchored on the newest row on file, and
// leaves it open for append.
func Open(path string, h Header, windowDays int, now func() time.Time) (*Recording, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.WriteFile(path, []byte(formatHeader(h)), 0o644); err != nil {
			return nil, err
		}
	}
	r := &Recording{path: path, days: windowDays, now: now}
	if err := r.prune(time.Time{}); err != nil {
		return nil, err
	}
	r.lastPrune = now()
	return r, nil
}

// Write appends one row (one unbuffered Write), then considers a prune
// anchored on this row's sim time.
func (r *Recording) Write(row Row) error {
	if r.f == nil {
		return os.ErrClosed
	}
	if _, err := r.f.Write([]byte(Format(row) + "\n")); err != nil {
		return err
	}
	if r.oldest.IsZero() || row.Sim.Before(r.oldest) {
		r.oldest = row.Sim
	}
	if row.Sim.After(r.newest) {
		r.newest = row.Sim
	}
	return r.maybePrune(row.Sim)
}

// SetWindowDays changes the window (< 1 reads as 7) and re-arms the next
// prune check.
func (r *Recording) SetWindowDays(n int) {
	r.days = n
	r.lastPrune = time.Time{}
}

// SetLiftoff rewrites the header's T+0 line to name t as the liftoff (the
// header is written at file creation, before a pad vessel has lifted off).
// Rows are untouched; the rewrite is atomic like a prune.
func (r *Recording) SetLiftoff(t time.Time) (err error) {
	if r.f == nil {
		return os.ErrClosed
	}
	r.f.Close()
	r.f = nil
	defer func() {
		f, oerr := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err == nil {
			err = oerr
		}
		r.f = f
	}()
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	for i, l := range lines {
		if bytes.HasPrefix(l, []byte("# T+0 = ")) {
			lines[i] = []byte("# T+0 = " + t.UTC().Format(dateLayout) + " (liftoff)" + headerSuffix + "\n")
			break
		}
	}
	return rewrite(r.path, bytes.Join(lines, nil), 0o644)
}

// Close closes the file; later Writes fail.
func (r *Recording) Close() error {
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

func (r *Recording) window() time.Duration {
	if r.days < 1 {
		return defaultDays * day
	}
	return time.Duration(r.days) * day
}

func (r *Recording) maybePrune(anchor time.Time) error {
	if r.now().Sub(r.lastPrune) < pruneEvery {
		return nil
	}
	if r.oldest.IsZero() || !r.oldest.Before(anchor.Add(-r.window()-pruneSlack)) {
		return nil
	}
	r.lastPrune = r.now()
	return r.prune(anchor)
}

// prune drops rows whose own sim time is before anchor-window, rewriting
// the file atomically only when something goes, and reopens it for append.
// A zero anchor means the newest row on file. Lines whose first column is
// not a date (header, truncated tail) are kept.
func (r *Recording) prune(anchor time.Time) (err error) {
	if r.f != nil {
		r.f.Close()
		r.f = nil
	}
	defer func() {
		f, oerr := os.OpenFile(r.path, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err == nil {
			err = oerr
		}
		r.f = f
	}()
	data, err := os.ReadFile(r.path)
	if err != nil {
		return err
	}
	lines := bytes.SplitAfter(data, []byte("\n"))
	times := make([]time.Time, len(lines))
	r.oldest, r.newest = time.Time{}, time.Time{}
	for i, l := range lines {
		t, ok := rowTime(l)
		if !ok {
			continue
		}
		times[i] = t
		if r.oldest.IsZero() || t.Before(r.oldest) {
			r.oldest = t
		}
		if t.After(r.newest) {
			r.newest = t
		}
	}
	if anchor.IsZero() {
		anchor = r.newest
	}
	cutoff := anchor.Add(-r.window())
	var out bytes.Buffer
	dropped := 0
	r.oldest, r.newest = time.Time{}, time.Time{}
	for i, l := range lines {
		if t := times[i]; !t.IsZero() {
			if t.Before(cutoff) {
				dropped++
				continue
			}
			if r.oldest.IsZero() || t.Before(r.oldest) {
				r.oldest = t
			}
			if t.After(r.newest) {
				r.newest = t
			}
		}
		out.Write(l)
	}
	needNL := len(data) > 0 && data[len(data)-1] != '\n'
	if dropped == 0 && !needNL {
		return nil
	}
	if needNL {
		out.WriteByte('\n')
	}
	return rewrite(r.path, out.Bytes(), 0o644)
}

func rowTime(line []byte) (time.Time, bool) {
	if len(line) < len(dateLayout) {
		return time.Time{}, false
	}
	t, err := time.Parse(dateLayout, string(line[:len(dateLayout)]))
	return t, err == nil
}

// Slug folds a vessel name to lower-case alphanumerics joined by single
// dashes.
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, c := range strings.ToLower(name) {
		if unicode.IsLetter(c) || unicode.IsDigit(c) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(c)
		} else {
			dash = true
		}
	}
	return b.String()
}

func formatHeader(h Header) string {
	l1 := fmt.Sprintf("# Flight Recording  vessel %d %q  %s / %s", h.VesselID, h.Name, h.System, h.Primary)
	if h.Site != "" {
		l1 += "  pad " + h.Site
	}
	t0 := "first row (no liftoff on record)"
	if h.LiftoffKnown {
		t0 = h.Liftoff.UTC().Format(dateLayout) + " (liftoff)"
	}
	return l1 + "\n# T+0 = " + t0 + headerSuffix + "\n" + columns("sim", "T+", "alt", "spd", "mass", "thr", "stg", "soi", "hold", "ptrim", "htrim", "apo", "peri", "event") + "\n"
}

// columns lays out one line in the recording's fixed columns.
func columns(sim, tp, alt, spd, mass, thr, stg, soi, hold, pt, ht, apo, peri, event string) string {
	s := fmt.Sprintf("%-19s  %-8s %6s  %7s  %7s  %3s %3s  %-5s  %-16s  %5s %5s  %7s  %6s", sim, tp, alt, spd, mass, thr, stg, soi, hold, pt, ht, apo, peri)
	if event != "" {
		s += "  " + event
	}
	return s
}

// Format renders one row, without the newline.
func Format(r Row) string {
	tp, apo, peri := "-", "-", "-"
	if r.TPlusKnown {
		tp = fmtTPlus(r.TPlus)
	}
	if r.ApsidesOK {
		apo, peri = grouped(r.ApoM/1000), grouped(r.PeriM/1000)
	}
	return columns(r.Sim.UTC().Format(dateLayout), tp, fmtAlt(r.AltM/1000), grouped(r.SpdMS), grouped(r.MassKg),
		grouped(r.ThrottlePct), strconv.Itoa(r.Stages), r.SOI, r.Hold,
		fmt.Sprintf("%.1f", r.PitchTrimDeg), fmt.Sprintf("%.1f", r.HeadTrimDeg), apo, peri, r.Event)
}

func fmtTPlus(d time.Duration) string {
	s := int64(d / time.Second)
	if s < 0 {
		s = 0
	}
	if s >= 86400 {
		return fmt.Sprintf("%dd%02d:%02d:%02d", s/86400, s%86400/3600, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s%3600/60, s%60)
}

// fmtAlt: one decimal under 1,000 km, none above.
func fmtAlt(km float64) string {
	if math.Abs(km) < 1000 {
		return fmt.Sprintf("%.1f", km)
	}
	return grouped(km)
}

// grouped rounds to an integer with thousands separators.
func grouped(v float64) string {
	n := int64(math.Round(v))
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	if neg {
		s = "-" + s
	}
	return s
}
