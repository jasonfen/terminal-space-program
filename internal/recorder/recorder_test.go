package recorder

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/atomicfile"
)

var epoch = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)

func simAt(d time.Duration) time.Time { return epoch.Add(d) }

func days(n float64) time.Duration { return time.Duration(n * float64(day)) }

// wall is a settable wall clock.
type wall struct{ t time.Time }

func (w *wall) now() time.Time { return w.t }

func testHeader() Header {
	return Header{VesselID: 7, Name: "Falcon 9 + Dragon", System: "Sol", Primary: "Earth",
		Site: "Cape Canaveral 28.6N 80.6W", Liftoff: time.Date(2000, 1, 5, 14, 2, 11, 0, time.UTC), LiftoffKnown: true}
}

func openRec(t *testing.T, w *wall, windowDays int) (*Recording, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recordings", "0007-test.txt")
	r, err := Open(path, testHeader(), windowDays, w.now)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, path
}

func put(t *testing.T, r *Recording, sim time.Time, ev string) {
	t.Helper()
	if err := r.Write(Row{Sim: sim, Event: ev, TPlusKnown: true}); err != nil {
		t.Fatal(err)
	}
}

func fileLines(t *testing.T, path string) []string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
}

// rowsOnDisk returns the sim times of the dated lines, in file order.
func rowsOnDisk(t *testing.T, path string) []time.Time {
	t.Helper()
	var out []time.Time
	for _, l := range fileLines(t, path) {
		if tm, ok := rowTime([]byte(l)); ok {
			out = append(out, tm)
		}
	}
	return out
}

func countRewrites(t *testing.T) *int {
	n := 0
	rewrite = func(p string, d []byte, m os.FileMode) error { n++; return atomicfile.Write(p, d, m) }
	t.Cleanup(func() { rewrite = atomicfile.Write })
	return &n
}

// Guard 1.
func TestFormatRowMatchesGolden(t *testing.T) {
	const (
		h1  = `# Flight Recording  vessel 7 "Falcon 9 + Dragon"  Sol / Earth  pad Cape Canaveral 28.6N 80.6W`
		h2  = `# T+0 = 2000-01-05 14:02:11 (liftoff) · alt apo peri km above surface · spd m/s vs soi body · trims deg`
		h3  = `sim                  T+          alt      spd       mass  thr stg  soi    hold              ptrim htrim      apo    peri  event`
		lof = `2000-01-05 14:02:11  0:00:00     0.0        0    560,062  100   2  Earth  Prograde (SURF)     0.0   0.0        -       -  liftoff`
		// Other rows of the ADR mock that agree with the header columns.
		stg = `2000-01-05 14:04:49  0:02:38    66.5    2,250    123,462  100   1  Earth  Prograde (SURF)    12.0   0.0       94     -45  stage: F9-S1 jettisoned, F9-S2 lit`
		orb = `2000-01-05 14:11:23  0:09:12   250.0    7,759     45,960    0   1  Earth  Prograde           12.0   0.0      250     250  throttle_cut: orbit 250 x 250 km, incl 28.6`
	)
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	got := fileLines(t, path)
	if len(got) != 3 || got[0] != h1 || got[1] != h2 || got[2] != h3 {
		t.Fatalf("header = %q", got)
	}
	base := Row{Sim: time.Date(2000, 1, 5, 14, 2, 11, 0, time.UTC), TPlusKnown: true, MassKg: 560062, ThrottlePct: 100,
		Stages: 2, SOI: "Earth", Hold: "Prograde (SURF)", Event: "liftoff"}
	if g := Format(base); g != lof {
		t.Fatalf("liftoff row\n got %q\nwant %q", g, lof)
	}
	s := Row{Sim: time.Date(2000, 1, 5, 14, 4, 49, 0, time.UTC), TPlus: 158 * time.Second, TPlusKnown: true, AltM: 66500, SpdMS: 2250,
		MassKg: 123462, ThrottlePct: 100, Stages: 1, SOI: "Earth", Hold: "Prograde (SURF)", PitchTrimDeg: 12,
		ApoM: 94000, PeriM: -45000, ApsidesOK: true, Event: "stage: F9-S1 jettisoned, F9-S2 lit"}
	if g := Format(s); g != stg {
		t.Fatalf("stage row\n got %q\nwant %q", g, stg)
	}
	o := Row{Sim: time.Date(2000, 1, 5, 14, 11, 23, 0, time.UTC), TPlus: 552 * time.Second, TPlusKnown: true, AltM: 250000, SpdMS: 7759,
		MassKg: 45960, Stages: 1, SOI: "Earth", Hold: "Prograde", PitchTrimDeg: 12, ApoM: 250000, PeriM: 250000, ApsidesOK: true,
		Event: "throttle_cut: orbit 250 x 250 km, incl 28.6"}
	if g := Format(o); g != orb {
		t.Fatalf("orbit row\n got %q\nwant %q", g, orb)
	}
	_ = r
}

// Mass column: a Saturn V on the pad (2,901,847 kg) and the widest value (9,999,999) the
// column holds (9,999,999) both keep every later column under its header.
func TestMassColumnHoldsSaturnV(t *testing.T) {
	w := &wall{t: epoch}
	_, path := openRec(t, w, 7)
	hdr := fileLines(t, path)[2]
	for _, kg := range []float64{2901847, 9999999} {
		row := Format(Row{Sim: epoch, TPlusKnown: true, MassKg: kg, SOI: "Earth", Hold: "Prograde", Event: "x"})
		for _, c := range [][2]string{{"soi", "Earth"}, {"hold", "Prograde"}, {"event", "x"}} {
			if hi, ri := strings.Index(hdr, c[0]), strings.LastIndex(row, c[1]); ri != hi {
				t.Fatalf("mass %.0f: %q at %d in row, %q at %d in header\n%s\n%s", kg, c[1], ri, c[0], hi, hdr, row)
			}
		}
		if got := len(row); got != len(Format(Row{Sim: epoch, TPlusKnown: true, MassKg: 1, SOI: "Earth", Hold: "Prograde", Event: "x"})) {
			t.Fatalf("mass %.0f: row is %d bytes, differs from a 1 kg row", kg, got)
		}
	}
}

// Guard 2.
func TestPruneKeepsRowExactlyWindowOld(t *testing.T) {
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	anchor := time.Date(2000, 1, 20, 12, 0, 0, 0, time.UTC)
	cut := anchor.Add(-7 * day)
	for _, tm := range []time.Time{cut.Add(-time.Second), cut, anchor, anchor.Add(5 * day)} {
		put(t, r, tm, "")
	}
	if err := r.prune(anchor); err != nil {
		t.Fatal(err)
	}
	got := rowsOnDisk(t, path)
	want := []time.Time{cut, anchor, anchor.Add(5 * day)}
	if len(got) != len(want) {
		t.Fatalf("rows = %v, want %v", got, want)
	}
	for i := range want {
		if !got[i].Equal(want[i]) {
			t.Fatalf("row %d = %v, want %v", i, got[i], want[i])
		}
	}
	if l := fileLines(t, path); !strings.HasPrefix(l[0], "# Flight Recording") || !strings.HasPrefix(l[2], "sim ") {
		t.Fatalf("header lost: %q", l[:3])
	}
}

// Guard 3.
func TestOpenPrunesOnce(t *testing.T) {
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	put(t, r, simAt(0), "old")
	put(t, r, simAt(9*day), "new")
	r.Close()
	r2, err := Open(path, testHeader(), 7, w.now) // same wall instant: no minute has passed
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	if got := rowsOnDisk(t, path); len(got) != 1 || !got[0].Equal(simAt(9*day)) {
		t.Fatalf("rows after Open = %v, want only day 9", got)
	}
}

// Guard 4.
func TestPruneRateLimitedToWallMinute(t *testing.T) {
	w := &wall{t: epoch}
	r, _ := openRec(t, w, 7)
	n := countRewrites(t)
	put(t, r, simAt(0), "old")
	w.t = epoch.Add(100 * time.Second)
	put(t, r, simAt(9*day), "new") // oldest 9 d old, gate open: rewrite 1
	if *n != 1 {
		t.Fatalf("rewrites after first = %d, want 1", *n)
	}
	w.t = epoch.Add(159 * time.Second) // 59 s later
	put(t, r, simAt(20*day), "late")   // oldest (day 9) is stale again but the gate is shut
	if *n != 1 {
		t.Fatalf("rewrites at 59 s = %d, want 1", *n)
	}
	w.t = epoch.Add(160 * time.Second) // exactly 60 s
	put(t, r, simAt(20*day), "late")
	if *n != 2 {
		t.Fatalf("rewrites at exactly 60 s = %d, want 2", *n)
	}
}

// Guard 5.
func TestPruneNeedsWindowPlusDayOfSlack(t *testing.T) {
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	n := countRewrites(t)
	put(t, r, simAt(0), "old")
	w.t = epoch.Add(100 * time.Second)
	before, _ := os.Stat(path)
	put(t, r, simAt(8*day), "edge") // oldest exactly window + 1 day old
	after, _ := os.Stat(path)
	if *n != 0 || !os.SameFile(before, after) {
		t.Fatalf("exactly window+1d fired: rewrites=%d sameInode=%v", *n, os.SameFile(before, after))
	}
	put(t, r, simAt(8*day+time.Second), "over")
	after, _ = os.Stat(path)
	if *n != 1 || os.SameFile(before, after) { // the inode check can read a rewrite
		t.Fatalf("one second over did not rewrite: rewrites=%d sameInode=%v", *n, os.SameFile(before, after))
	}
}

// Guard 6.
func TestKillBetweenTmpAndRenameLeavesOldFile(t *testing.T) {
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	put(t, r, simAt(0), "old")
	put(t, r, simAt(10*day), "new")
	before, _ := os.ReadFile(path)
	calls := 0
	rewrite = func(p string, d []byte, m os.FileMode) error {
		calls++
		os.WriteFile(p+".tmp", d, m) // the new content lands beside the old file; the rename never happens
		return os.ErrDeadlineExceeded
	}
	t.Cleanup(func() { rewrite = atomicfile.Write })
	w.t = epoch.Add(100 * time.Second)
	if err := r.maybePrune(simAt(10 * day)); err == nil {
		t.Fatal("killed rewrite returned nil")
	}
	after, _ := os.ReadFile(path)
	if calls != 1 || string(after) != string(before) {
		t.Fatalf("rewrite calls=%d, live file changed=%v", calls, string(after) != string(before))
	}
	if _, err := os.Stat(path + ".tmp"); err != nil {
		t.Fatalf("tmp leftover missing: %v", err)
	}
	if len(rowsOnDisk(t, path)) != 2 {
		t.Fatal("old file does not parse whole")
	}
	put(t, r, simAt(10*day+time.Second), "after") // the append handle survives a failed prune
}

// Guard 7.
func TestRewindKeepsFutureRows(t *testing.T) {
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	step := func(d time.Duration) {
		w.t = w.t.Add(100 * time.Second)
		put(t, r, simAt(d), "")
	}
	step(10 * day)
	step(10*day + time.Hour)
	step(2 * day) // rewind: a load seam
	step(2*day + time.Hour)
	if got := rowsOnDisk(t, path); len(got) != 4 {
		t.Fatalf("rows after rewind = %v, want all 4", got)
	}
	if !r.oldest.Equal(simAt(2 * day)) {
		t.Fatalf("oldest = %v, want day 2", r.oldest)
	}
	r.lastPrune = time.Time{}
	if err := r.maybePrune(simAt(2 * day)); err != nil {
		t.Fatal(err)
	}
	if got := rowsOnDisk(t, path); len(got) != 4 {
		t.Fatalf("forced prune at anchor day 2 left %v", got)
	}
}

// Guard 8.
func TestRowVisibleBeforeClose(t *testing.T) {
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	put(t, r, simAt(time.Hour), "visible")
	if b, _ := os.ReadFile(path); !strings.Contains(string(b), "visible") {
		t.Fatalf("row not on disk before Close:\n%s", b)
	}
}

// Guard 9.
func TestSlugFoldsNonAlnum(t *testing.T) {
	for in, want := range map[string]string{
		"Falcon 9 + Dragon": "falcon-9-dragon",
		"iss-1":             "iss-1",
		"  --A__b  ":        "a-b",
	} {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

// Guard 10.
func TestWindowBelowOneReadsDefault(t *testing.T) {
	for _, tc := range []struct {
		days     int
		wantKept bool
	}{{0, true}, {-3, true}, {1, false}} { // equal case: 1 stays 1 and drops a 7-day-old row
		w := &wall{t: epoch}
		r, path := openRec(t, w, 7)
		anchor := simAt(30 * day)
		put(t, r, anchor.Add(-7*day), "")
		put(t, r, anchor, "")
		r.SetWindowDays(tc.days)
		if err := r.prune(anchor); err != nil {
			t.Fatal(err)
		}
		if kept := len(rowsOnDisk(t, path)) == 2; kept != tc.wantKept {
			t.Errorf("SetWindowDays(%d): 7-day-old row kept=%v, want %v", tc.days, kept, tc.wantKept)
		}
	}
}

// A kill mid-row leaves a tail with no newline; Open keeps it and the next
// row starts on its own line.
func TestTruncatedTailKept(t *testing.T) {
	w := &wall{t: epoch}
	r, path := openRec(t, w, 7)
	put(t, r, simAt(time.Hour), "whole")
	r.Close()
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("2000-01-0")
	f.Close()
	r, err := Open(path, testHeader(), 7, w.now)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	put(t, r, simAt(2*time.Hour), "next")
	l := fileLines(t, path)
	if len(l) != 6 || l[4] != "2000-01-0" || !strings.HasSuffix(l[5], "next") {
		t.Fatalf("lines = %q", l)
	}
}

func TestSetLiftoffRewritesHeaderOnly(t *testing.T) {
	w := &wall{t: epoch}
	path := filepath.Join(t.TempDir(), "0007-test.txt")
	h := testHeader()
	h.LiftoffKnown = false
	r, err := Open(path, h, 7, w.now)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	put(t, r, simAt(time.Hour), "a")
	put(t, r, simAt(2*time.Hour), "b")
	before := fileLines(t, path)
	if !strings.Contains(before[1], "first row (no liftoff on record)") {
		t.Fatalf("setup: header = %q", before[1])
	}
	lift := simAt(90 * time.Minute)
	if err := r.SetLiftoff(lift); err != nil {
		t.Fatal(err)
	}
	put(t, r, simAt(3*time.Hour), "c") // still appendable after the rewrite
	after := fileLines(t, path)
	if want := "# T+0 = " + lift.Format(dateLayout) + " (liftoff)" + headerSuffix; after[1] != want {
		t.Errorf("header T+0 = %q, want %q", after[1], want)
	}
	for i := range before {
		if i != 1 && after[i] != before[i] {
			t.Errorf("line %d changed: %q -> %q", i, before[i], after[i])
		}
	}
	if len(after) != len(before)+1 {
		t.Errorf("lines = %d, want %d", len(after), len(before)+1)
	}
}
