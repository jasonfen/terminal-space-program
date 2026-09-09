package readout

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// --- Duration: two-unit, truncating, zero-padded second unit. ADR 0049
// decision 1 / action-plan decision 1. ---

func TestDuration(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"zero", 0, "0s"},
		{"seconds only", 45 * time.Second, "45s"},
		{"59s below the minute crossing", 59 * time.Second, "59s"},
		{"60s crosses into minutes", 60 * time.Second, "1m00s"},
		{"minutes, seconds zero-padded", 2*time.Minute + 7*time.Second, "2m07s"},
		{"hours, minutes not seconds", 4*time.Hour + 43*time.Minute, "4h43m"},
		{"truncates, never rounds", 4*time.Hour + 43*time.Minute + 59*time.Second, "4h43m"},
		{"days, hours zero-padded", 3*24*time.Hour + 5*time.Hour, "3d05h"},
		{"negative treated as magnitude", -45 * time.Second, "45s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Duration(c.d); got != c.want {
				t.Errorf("Duration(%v) = %q, want %q", c.d, got, c.want)
			}
		})
	}
}

// --- Period: three-unit, keeps seconds at every scale. ADR decision 1,
// action-plan decision 1 example "1h34m28s". ---

func TestPeriod(t *testing.T) {
	cases := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"seconds only", 45 * time.Second, "45s"},
		{"minutes and seconds", 3*time.Minute + 45*time.Second, "3m45s"},
		{"hours keep seconds (contract example)", 1*time.Hour + 34*time.Minute + 28*time.Second, "1h34m28s"},
		{"hours zero-padded minutes and seconds", 6*time.Hour + 4*time.Minute + 21*time.Second, "6h04m21s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Period(c.d); got != c.want {
				t.Errorf("Period(%v) = %q, want %q", c.d, got, c.want)
			}
		})
	}
}

// --- Countdown: T- until, T+ since (launch convention). ADR decision 2,
// action-plan decision 2. Overdue/elapsed and zero cases pinned per the
// task brief. ---

func TestCountdown(t *testing.T) {
	cases := []struct {
		name  string
		until time.Duration // positive: event in the future
		want  string
	}{
		{"future event reads T-", 4*time.Hour + 43*time.Minute, "T-4h43m"},
		{"overdue event reads T+", -(2*time.Minute + 10*time.Second), "T+2m10s"},
		{"just-fired reads T+ (already right per ADR)", -3 * time.Second, "T+3s"},
		{"zero reads T+0s (elapsed, not future)", 0, "T+0s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Countdown(c.until); got != c.want {
				t.Errorf("Countdown(%v) = %q, want %q", c.until, got, c.want)
			}
		})
	}
}

// --- Distance: SI ladder, m / km / Mm / Gm / AU, 4 sig figs (meters
// stay whole numbers, see notes below). ADR decision 3, action-plan
// decisions 3 and 4. ---

func TestDistance(t *testing.T) {
	cases := []struct {
		name string
		m    float64
		want string
	}{
		{"zero", 0, "0 m"},
		{"sub-1km stays in meters, whole number", 912, "912 m"},
		{"pad altitude zero", 0, "0 m"},
		{"1000 m crosses into km", 1000, "1.000 km"},
		{"km rung, 1 decimal (contract example)", 500000, "500.0 km"},
		{"km rung another contract example", 438900, "438.9 km"},
		{"just under the km->Mm rollover, no bump", 999940, "999.9 km"},

		// Extended km rung (gate review addendum, maintainer overrule of
		// the original F1-adjacent ruling): decision 3's own worked
		// examples only ALL hold at once if km runs a full extra decade,
		// to 9999 km, before promoting to Mm. `2590 km` is decision 3's
		// own example, verbatim. The Mm rung's own bottom is therefore
		// 10 Mm, not 1 Mm: a value never renders as "5.000 Mm" because km
		// already covers everything below 10 Mm. Deliberate, not a gap.
		{"decision 3's own example, verbatim: 4 digits stay in km", 2590000, "2590 km"},
		{"4-digit km reading, no promotion (was the old bug's own case)", 1200000, "1200 km"},
		{"at the new boundary: 9999 km is the last value that stays km", 9999000, "9999 km"},
		{"just past 9999 km rounds up and must still promote to Mm", 9999600, "10.00 Mm"},
		{"exactly 10 Mm: the Mm rung's own bottom, km covers everything below it", 1e7, "10.00 Mm"},
		{"Mm rung, 2 decimals (Ap contract example)", 35790000, "35.79 Mm"},
		{"decision 3's own Mm example, verbatim", 10660000, "10.66 Mm"},
		{"just below the 0.1 AU threshold stays Gm", bodies.AU/10 - 1, "14.96 Gm"},
		{"at the 0.1 AU threshold switches to AU", bodies.AU / 10, "0.100 AU"},
		{"well above threshold, AU rung 3 decimals", bodies.AU * 8.727, "8.727 AU"},
		{"negative distance keeps signed depth (sub-surface Pe)", -120000, "-120.0 km"},
		{"negative meters", -0.4, "0 m"}, // sub-metre noise nzero-snaps to +0

		// F1 (gate review, item4-A-review.md): the decimal count must come
		// from the ROUNDED value, not the pre-rounding one, or an
		// intra-rung decade crossing prints one significant figure too
		// many (9.9996 rounding to "10.000 km" instead of "10.00 km").
		{"F1: one integer digit rounds up to two, decimals must drop 3->2", 9999.6, "10.00 km"},
		{"F1: two integer digits round up to three, decimals must drop 2->1", 99996, "100.0 km"},
		{"F1: same crossing one rung up (Mm), now past the extended km boundary", 99.996e6, "100.0 Mm"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Distance(c.m); got != c.want {
				t.Errorf("Distance(%v) = %q, want %q", c.m, got, c.want)
			}
		})
	}
}

// --- Mass: kg / t ladder, no kt rung. ADR decision 3. ---

func TestMass(t *testing.T) {
	cases := []struct {
		name string
		kg   float64
		want string
	}{
		{"kg rung, whole number", 999, "999 kg"},
		{"crosses into tonnes at 1000 kg", 1000, "1.000 t"},
		{"contract example, 4 digits", 2160000, "2160 t"},
		{"contract example, 2 decimals", 12950, "12.95 t"},
		{"contract example, another 4-digit reading", 2928000, "2928 t"},
		{"F1: rounds up across the top rung, no rung to promote into", 999999, "1000 t"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Mass(c.kg); got != c.want {
				t.Errorf("Mass(%v) = %q, want %q", c.kg, got, c.want)
			}
		})
	}
}

// --- Thrust: kN everywhere, never raw N. ADR decision 3. ---

func TestThrust(t *testing.T) {
	cases := []struct {
		name string
		n    float64
		want string
	}{
		{"contract example", 1023000, "1023 kN"},
		{"small thrust still kN", 500, "0.500 kN"},
		{"F1: rounds up a decade, decimals must drop", 999960, "1000 kN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Thrust(c.n); got != c.want {
				t.Errorf("Thrust(%v) = %q, want %q", c.n, got, c.want)
			}
		})
	}
}

// TestPressure pins the ATMOSPHERE chip's / launch strip's Q reading:
// always kPa (never raw Pa), 4 significant figures, mirroring Thrust's
// own single-unit off-ladder shape. Input is pascals.
func TestPressure(t *testing.T) {
	cases := []struct {
		name string
		pa   float64
		want string
	}{
		{"zero on the pad", 0, "0.000 kPa"},
		{"typical max-Q figure", 23100, "23.10 kPa"},
		{"four-digit kPa", 1023000, "1023 kPa"},
		{"F1: rounds up a decade, decimals must drop", 999960, "1000 kPa"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Pressure(c.pa); got != c.want {
				t.Errorf("Pressure(%v) = %q, want %q", c.pa, got, c.want)
			}
		})
	}
}

// --- Speed / DeltaV: off-ladder, 4 sig figs capped at 2 decimals, always
// m/s. ADR decision 4, action-plan decision 12. Speed uses the rule at
// every magnitude, including the 40.00 m/s case (adjudicated at the gate
// review: the ADR's own 22.50/28.60 examples are 2-decimal at two
// integer digits, so its lone "40.0 m/s" is a pre-contract transcription,
// not a contract output). DeltaV instead goes integer at |v| >= 100
// (also adjudicated at the gate review), so the two deliberately diverge
// in the 100-999.9 m/s band. ---

func TestSpeed(t *testing.T) {
	cases := []struct {
		name string
		mps  float64
		want string
	}{
		{"contract example, integer at 4 digits", 5519, "5519 m/s"},
		{"the 4-sig-fig rule at every magnitude, including 40", 40.0, "40.00 m/s"},
		{"contract example, 2 decimals below 1", 0.12, "0.12 m/s"},
		{"stays decimal in the 100s, unlike DeltaV", 550, "550.0 m/s"},
		{"negative speed rounds toward zero sign correctly", -0.001, "0.00 m/s"},
		{"F1: two integer digits round up to three, decimals must drop", 99.996, "100.0 m/s"},
		{"F1: three integer digits round up to four, decimals must drop", 999.96, "1000 m/s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Speed(c.mps); got != c.want {
				t.Errorf("Speed(%v) = %q, want %q", c.mps, got, c.want)
			}
		})
	}
}

// TestSignedSpeed pins the closing:/rate-style rows added in ADR 0049
// stage A2's gate-review follow-up: same precision as Speed, but always
// signed, including at exactly zero and at a negative value that Nzero
// snaps to zero (the pre-snap sign must not survive as a bare "-0.00").
func TestSignedSpeed(t *testing.T) {
	cases := []struct {
		name string
		mps  float64
		want string
	}{
		{"positive gets an explicit +", 3640, "+3640 m/s"},
		{"negative keeps its own sign", -12.34, "-12.34 m/s"},
		{"exact zero reads +0.00", 0, "+0.00 m/s"},
		{"a negative that snaps to zero still reads +0.00, not -0.00", -0.001, "+0.00 m/s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SignedSpeed(c.mps); got != c.want {
				t.Errorf("SignedSpeed(%v) = %q, want %q", c.mps, got, c.want)
			}
		})
	}
}

func TestDeltaV(t *testing.T) {
	cases := []struct {
		name string
		mps  float64
		want string
	}{
		{"large budget, integer", 5519, "5519 m/s"},
		{"integer in the 100s, unlike Speed", 550, "550 m/s"},
		{"just below the integer crossing, 2 decimals", 99.9, "99.90 m/s"},
		{"at the integer crossing", 100, "100 m/s"},
		{"contract example below 100, 2 decimals", 22.5, "22.50 m/s"},
		{"contract example well below 100", 0.12, "0.12 m/s"},
		{"F1: rounds up across DeltaV's own 100 m/s integer threshold", 99.996, "100 m/s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeltaV(c.mps); got != c.want {
				t.Errorf("DeltaV(%v) = %q, want %q", c.mps, got, c.want)
			}
		})
	}
}

func TestDeltaVPair(t *testing.T) {
	cases := []struct {
		name           string
		stage, vehicle float64
		want           string
	}{
		{"contract example, both in the integer band", 3518, 9412, "3518 / 9412 m/s"},
		{"stage below 100, vehicle above", 45.5, 3200, "45.50 / 3200 m/s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeltaVPair(c.stage, c.vehicle); got != c.want {
				t.Errorf("DeltaVPair(%v, %v) = %q, want %q", c.stage, c.vehicle, got, c.want)
			}
		})
	}
}

// --- Angle: orbital angles, 4 sig figs capped at 2 decimals. ADR
// decision 4. ---

func TestAngle(t *testing.T) {
	cases := []struct {
		name string
		deg  float64
		want string
	}{
		{"contract example", 28.6, "28.60°"},
		{"contract example, zero", 0, "0.00°"},
		{"contract example, delta-incl", 31.2, "31.20°"},
		{"negative zero snaps positive", -0.001, "0.00°"},
		{"F1: two integer digits round up to three, decimals must drop", 99.996, "100.0°"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Angle(c.deg); got != c.want {
				t.Errorf("Angle(%v) = %q, want %q", c.deg, got, c.want)
			}
		})
	}
}

// --- TrimAngle / FPA / Heading: integer steering angles. ADR decision 4.
// Split at the gate review after checking the current renderers: pitch
// trim (orbit_chip_builders.go:1392) is "%+.1f°" today, explicit sign;
// fpa (orbit_chip_builders.go:1398, :1555; launch.go:1173) is "%.0f°",
// no plus. TrimAngle keeps the explicit sign, FPA does not. ---

func TestTrimAngle(t *testing.T) {
	cases := []struct {
		name string
		deg  float64
		want string
	}{
		{"contract example, negative pitch trim", -10, "-10°"},
		{"positive trim signed", 12, "+12°"},
		{"zero trim", 0, "+0°"},
		{"rounds to nearest degree", -9.6, "-10°"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := TrimAngle(c.deg); got != c.want {
				t.Errorf("TrimAngle(%v) = %q, want %q", c.deg, got, c.want)
			}
		})
	}
}

func TestFPA(t *testing.T) {
	cases := []struct {
		name string
		deg  float64
		want string
	}{
		{"positive fpa, no plus sign (unlike TrimAngle)", 45, "45°"},
		{"contract example, negative", -10, "-10°"},
		{"zero, no plus sign", 0, "0°"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FPA(c.deg); got != c.want {
				t.Errorf("FPA(%v) = %q, want %q", c.deg, got, c.want)
			}
		})
	}
}

func TestHeading(t *testing.T) {
	cases := []struct {
		name string
		deg  float64
		want string
	}{
		{"contract example, due east", 90, "090°"},
		{"zero-padded single digit", 5, "005°"},
		{"three digits, no padding needed", 270, "270°"},
		{"wraps negative into 0-360", -10, "350°"},
		{"wraps 360 to 0", 360, "000°"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Heading(c.deg); got != c.want {
				t.Errorf("Heading(%v) = %q, want %q", c.deg, got, c.want)
			}
		})
	}
}

// --- Nzero: -0.0 snaps to 0.0, matching the existing helper this
// package replaces (orbit_chip_builders.go's nzero). ---

func TestNzero(t *testing.T) {
	cases := []struct {
		name     string
		x        float64
		decimals int
		want     float64
	}{
		{"negative value that rounds to zero snaps positive", -0.001, 1, 0},
		{"non-zero value passes through", -1.5, 1, -1.5},
		{"exact zero stays zero", 0, 2, 0},
		{"value that rounds to zero at 0 decimals", -0.4, 0, 0},
		{"value that does not round to zero", -0.6, 0, -0.6},
		// F2 (gate review): math.Round always rounds an exact tie away
		// from zero, but fmt's own %.*f rounds ties to even, so at
		// exactly -0.5 the two disagreed and Nzero let the value through
		// unsnapped, which fmt then printed as "-0" anyway.
		{"F2: exact tie at -0.5 must snap (fmt rounds it to even, -0)", -0.5, 0, 0},
		{"F2: exact tie at -2.5 must NOT snap (fmt rounds it to -2, non-zero)", -2.5, 0, -2.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Nzero(c.x, c.decimals); got != c.want {
				t.Errorf("Nzero(%v, %d) = %v, want %v", c.x, c.decimals, got, c.want)
			}
		})
	}
}

// TestDistanceF2ExactTieDoesNotLeakNegativeZero pins the end-to-end
// symptom the gate review demonstrated: Distance(-0.5) used to print
// "-0 m" (Nzero didn't snap it, then fmt's own tie-to-even rounding
// produced the "-0" the whole Nzero mechanism exists to prevent).
func TestDistanceF2ExactTieDoesNotLeakNegativeZero(t *testing.T) {
	if got := Distance(-0.5); got != "0 m" {
		t.Errorf("Distance(-0.5) = %q, want \"0 m\" (no negative-zero leak)", got)
	}
}

// TestFormattersAreTotalOverInfAndNaN pins F8 (gate review, item4-A-review.md):
// intDigits divided by ten until the value dropped below ten, and ±Inf
// never does, so Distance/Speed/Angle/Mass/Thrust all hung forever on an
// infinite input before this fix. A hang inside a Bubble Tea View() call
// freezes the whole TUI with no panic and no log line — worse than any
// wrong string this package could print. "Not reachable from a checked
// call site today" is a property of the call sites, not of the package,
// which is the one choke point every future flight readout routes
// through, so a formatter must be total over its input type.
//
// Each case runs the call on its own goroutine behind a timeout rather
// than just asserting the returned string, so a regression that
// reintroduces the loop fails as a timeout, not as a suite that never
// finishes (which would look like an unrelated CI hang, not a pinned
// test failure).
func TestFormattersAreTotalOverInfAndNaN(t *testing.T) {
	inputs := []struct {
		name string
		v    float64
	}{
		{"+Inf", math.Inf(1)},
		{"-Inf", math.Inf(-1)},
		{"NaN", math.NaN()},
	}
	formatters := []struct {
		name string
		fn   func(float64) string
	}{
		{"Distance", Distance},
		{"Speed", Speed},
		{"Angle", Angle},
		{"Mass", Mass},
		{"Thrust", Thrust},
		{"Pressure", Pressure},
		{"DeltaV", DeltaV},
	}
	for _, f := range formatters {
		for _, in := range inputs {
			t.Run(f.name+"/"+in.name, func(t *testing.T) {
				done := make(chan string, 1)
				go func() { done <- f.fn(in.v) }()
				select {
				case got := <-done:
					if got == "" {
						t.Errorf("%s(%s) returned an empty string", f.name, in.name)
					}
				case <-time.After(2 * time.Second):
					t.Fatalf("%s(%s) did not return within 2s: intDigits likely regressed to an unbounded loop", f.name, in.name)
				}
			})
		}
	}
}
