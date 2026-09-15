package screens

import (
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/readout"
)

// placeOnConic puts the active craft on the coplanar conic with the given
// apsis radii, at true anomaly nu. Returns the orbital period.
func placeOnConic(w *sim.World, rPeri, rApo, nu float64) float64 {
	c := w.ActiveCraft()
	c.Landed = false
	mu := c.Primary.GravitationalParameter()
	a := (rPeri + rApo) / 2
	e := (rApo - rPeri) / (rApo + rPeri)
	p := a * (1 - e*e)
	r := p / (1 + e*math.Cos(nu))
	// Perifocal position/velocity, then used directly as the equatorial
	// frame (ω = Ω = i = 0) — enough for an apsis-timing readout.
	h := math.Sqrt(mu * p)
	c.State.R.X, c.State.R.Y, c.State.R.Z = r*math.Cos(nu), r*math.Sin(nu), 0
	c.State.V.X = -mu / h * math.Sin(nu)
	c.State.V.Y = mu / h * (e + math.Cos(nu))
	c.State.V.Z = 0
	return 2 * math.Pi * math.Sqrt(a*a*a/mu)
}

// apsisRow pulls the value of the named ORBIT chip row out of a render.
func apsisRow(t *testing.T, out, label string) string {
	t.Helper()
	re := regexp.MustCompile(regexp.QuoteMeta(label) + `\s*(\S+)`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no %s row in the render:\n%s", label, out)
	}
	return m[1]
}

// navigationApCell pulls NAVIGATION's whole Ap: cell (altitude, plus an
// optional trend glyph and T- countdown, all on one cell since ADR 0051
// decision 10 folds the retired ORBIT chip's separate apo: row into it)
// out of a render: everything from "Ap:" up to the next two-space gap
// before the row's second label ("Pe:").
func navigationApCell(t *testing.T, out string) string {
	t.Helper()
	re := regexp.MustCompile(`Ap:\s*(.+?)\s{2,}Pe:`)
	m := re.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no Ap: cell in the render:\n%s", out)
	}
	return strings.TrimSpace(m[1])
}

// #286: on a perfectly circular orbit every point is at the same radius,
// so there is no apoapsis or periapsis to count down to. The readout used
// to print exactly half a period, frozen — a number that looks live and
// isn't, which players tried to phase off. It has to say so instead.
//
// Spawn presets produce exactly-circular orbits, so this is what a player
// sees on a fresh spawn before any burn adds eccentricity.
func TestOrbitChipApsisTimesDegenerateOnCircularOrbit(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	r := w.ActiveCraft().Primary.RadiusMeters() + 500e3

	// Two craft positions a quarter turn apart on the SAME circular orbit —
	// the live symptom was both reading an identical, unmoving P/2. ADR
	// 0051 folds the countdown onto NAVIGATION's Ap: cell (decision 10);
	// the regression is now "no T- countdown appears" rather than a
	// separate apo:/peri: row reading a literal dash.
	var seen []string
	for _, nu := range []float64{0, math.Pi / 2} {
		period := placeOnConic(w, r, r, nu)
		out := v.Render(w, 0, 200, 60)
		cell := navigationApCell(t, out)
		if strings.Contains(cell, "T-") || strings.Contains(cell, "T+") {
			t.Errorf("circular orbit at ν=%.2f: Ap cell %q carries a countdown, want none (apsides are undefined at e=0)",
				nu, cell)
		}
		seen = append(seen, cell)
		_ = period
	}
	if len(seen) == 2 && seen[0] != seen[1] {
		t.Errorf("Ap cell differed between two points on the same circular orbit (only the altitude should print, and it's the same radius): %q vs %q", seen[0], seen[1])
	}
}

// The companion guard: the fix must not swallow real apsis timers. The
// live remedy in #286 was a single 0.1 m/s prograde pulse that raised Ap
// by 0.4 km and restored 1 s/s ticking — that orbit still has to read a
// duration, and one that moves as the craft advances.
func TestOrbitChipApsisTimesLiveOnSlightlyEccentricOrbit(t *testing.T) {
	v := NewOrbitView(plainTheme())
	v.Resize(200, 60)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	primaryR := w.ActiveCraft().Primary.RadiusMeters()
	rPeri, rApo := primaryR+500.0e3, primaryR+500.4e3

	placeOnConic(w, rPeri, rApo, 0)
	atPeri := navigationApCell(t, v.Render(w, 0, 200, 60))
	placeOnConic(w, rPeri, rApo, math.Pi/2)
	quarterOn := navigationApCell(t, v.Render(w, 0, 200, 60))

	if !strings.Contains(atPeri, "T-") && !strings.Contains(atPeri, "T+") {
		t.Fatalf("0.4 km of apsis separation read as degenerate (no countdown on the Ap cell): %q", atPeri)
	}
	if !strings.Contains(quarterOn, "T-") && !strings.Contains(quarterOn, "T+") {
		t.Fatalf("0.4 km of apsis separation read as degenerate (no countdown on the Ap cell): %q", quarterOn)
	}
	if atPeri == quarterOn {
		t.Errorf("Ap cell frozen at %q across a quarter orbit — the countdown is not tracking position", atPeri)
	}
}

// TestOrbitChipSubSurfacePeriapsisIsWarningColoured pins F12 (gate
// review): decision 5's "a sub-surface periapsis keeps its signed depth;
// the row turns Warning" had no call-site test anywhere in the suite
// (readout's own tests cover the string content, not which theme style a
// real chip applies to it). No extra word or glyph marks the row per the
// ADR, so the ONLY observable difference is the colour: this uses
// plainThemeColored (distinguishable ANSI per style, unlike
// chipTestTheme's no-op styles) and checks the RAW (non-ANSI-stripped)
// output: the Pe: row must carry escape codes, and the Ap: row (not
// sub-surface, the control) must not, proving the colouring is targeted
// rather than a blanket style leaking onto every row.
func TestOrbitChipSubSurfacePeriapsisIsWarningColoured(t *testing.T) {
	// go test's stdout is not a TTY, so termenv's lazy env detection
	// (cached process-wide via sync.Once) settles on the colorless
	// Ascii profile the first time anything asks, which makes every
	// theme's Render a no-op regardless of what color it was given.
	// SetColorProfile bypasses that detection outright. Same idiom as
	// TestOrbitRenderDiskCacheHitMatchesUncachedAfterPan: read the
	// process's ambient profile first (lazily triggering detection if
	// nothing has yet) and restore exactly that in t.Cleanup, so every
	// test that runs after this one sees the same color output it
	// would have without this test existing.
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	v := NewOrbitView(plainThemeColored())
	v.Resize(120, 40)
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	c := w.ActiveCraft()
	// Switch to the Moon: an atmosphered primary's shouldShowLaunchHUD
	// gate reads "periapsis below the atmosphere cutoff" as still
	// ascending, at ANY true anomaly, and hands the row to the LAUNCH/
	// SURFACE chip instead (exactly what decision 5 also covers there),
	// but not what this test means to exercise. An airless body has no
	// such gate, so a sub-surface periapsis reaches the plain ORBIT chip.
	for _, b := range w.System().Bodies {
		if b.ID == "moon" {
			c.Primary = b
			break
		}
	}
	primaryR := c.Primary.RadiusMeters()
	// Periapsis 50 km below the surface (an impactor / de-orbit
	// trajectory), apoapsis 500 km above it, so Ap: stays a normal
	// reading and only Pe: should carry the Warning colour.
	placeOnConic(w, primaryR-50e3, primaryR+500e3, math.Pi)

	// ADR 0051 puts Ap: and Pe: on the SAME row now (chipRow2), not
	// separate lines, so the targeted-colouring check splits one row at
	// the (always plain-text) "Pe:" label instead of comparing two rows.
	lines := v.buildNavigationBox(w)
	var apPeRow string
	for _, l := range lines {
		stripped := stripANSI(l)
		if strings.Contains(stripped, readout.LabelAp) && strings.Contains(stripped, readout.LabelPe) {
			apPeRow = l
			break
		}
	}
	if apPeRow == "" {
		t.Fatalf("could not find the Ap:/Pe: row in NAVIGATION:\n%s", strings.Join(lines, "\n"))
	}
	peIdx := strings.Index(apPeRow, "Pe:")
	if peIdx < 0 {
		t.Fatalf("could not locate the plain-text 'Pe:' label in row: %q", apPeRow)
	}
	apPortion, pePortion := apPeRow[:peIdx], apPeRow[peIdx:]
	if !strings.Contains(stripANSI(pePortion), "-") {
		t.Fatalf("setup broken: Pe cell does not read as sub-surface (no signed depth): %q", pePortion)
	}
	// plainThemeColored's Warning style is Foreground(Color("3")), which
	// termenv.ANSI renders as the literal SGR sequence "\x1b[33m", pinned
	// specifically rather than "carries some colour at all", so swapping
	// in a different style (e.g. Dim, "\x1b[90m") still fails this check
	// instead of passing as "some style was applied".
	const wantWarningPrefix = "\x1b[33m"
	if !strings.Contains(pePortion, wantWarningPrefix) {
		t.Errorf("sub-surface Pe cell not wrapped in Warning (%q): %q", wantWarningPrefix, pePortion)
	}
	if apPortion != stripANSI(apPortion) {
		t.Errorf("Ap cell (not sub-surface) unexpectedly carries colour codes, colouring is not targeted: %q", apPortion)
	}
}
