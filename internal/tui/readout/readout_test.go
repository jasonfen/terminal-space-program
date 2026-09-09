package readout

import (
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
// stay whole numbers — see notes). ADR decision 3, action-plan decisions
// 3 and 4. ---

func TestDistance(t *testing.T) {
	cases := []struct {
		name string
		m    float64
		want string
	}{
		{"zero", 0, "0 m"},
		{"sub-1km stays in meters, whole number", 912, "912 m"},
		{"pad altitude zero", 0, "0 m"},
		{"km rung, 1 decimal (contract example)", 500000, "500.0 km"},
		{"km rung another contract example", 438900, "438.9 km"},
		{"just under the km->Mm rollover, no bump", 999940, "999.9 km"},
		{"999,950 m rounds up across the rung into Mm", 999950, "1.000 Mm"},
		{"Mm rung, 2 decimals (Ap contract example)", 35790000, "35.79 Mm"},
		{"just below the 0.1 AU threshold stays Gm", bodies.AU/10 - 1, "14.96 Gm"},
		{"at the 0.1 AU threshold switches to AU", bodies.AU / 10, "0.100 AU"},
		{"well above threshold, AU rung 3 decimals", bodies.AU * 8.727, "8.727 AU"},
		{"negative distance keeps signed depth (sub-surface Pe)", -120000, "-120.0 km"},
		{"negative meters", -0.4, "0 m"}, // sub-metre noise nzero-snaps to +0
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Thrust(c.n); got != c.want {
				t.Errorf("Thrust(%v) = %q, want %q", c.n, got, c.want)
			}
		})
	}
}

// --- Speed / DeltaV: off-ladder, 4 sig figs capped at 2 decimals, always
// m/s. ADR decision 4, action-plan decision 12. The 40.0 vs 40.00 case is
// flagged ambiguity #1: we implement the stated rule, not the ADR's own
// example. ---

func TestSpeed(t *testing.T) {
	cases := []struct {
		name string
		mps  float64
		want string
	}{
		{"contract example, integer at 4 digits", 5519, "5519 m/s"},
		{"AMBIGUITY 1: rule gives 40.00, ADR's own example prints 40.0", 40.0, "40.00 m/s"},
		{"contract example, 2 decimals below 1", 0.12, "0.12 m/s"},
		{"AMBIGUITY 2 probe: rule gives 1 decimal in the 100s, not integer", 550, "550.0 m/s"},
		{"negative speed rounds toward zero sign correctly", -0.001, "0.00 m/s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Speed(c.mps); got != c.want {
				t.Errorf("Speed(%v) = %q, want %q", c.mps, got, c.want)
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
		{"contract example below 100, 2 decimals", 22.5, "22.50 m/s"},
		{"large budget, integer", 5519, "5519 m/s"},
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
	if got := DeltaVPair(3518, 9412); got != "3518 / 9412 m/s" {
		t.Errorf("DeltaVPair(3518, 9412) = %q, want %q", got, "3518 / 9412 m/s")
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Angle(c.deg); got != c.want {
				t.Errorf("Angle(%v) = %q, want %q", c.deg, got, c.want)
			}
		})
	}
}

// --- SteeringAngle / Heading: integer steering angles. ADR decision 4. ---

func TestSteeringAngle(t *testing.T) {
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
			if got := SteeringAngle(c.deg); got != c.want {
				t.Errorf("SteeringAngle(%v) = %q, want %q", c.deg, got, c.want)
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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Nzero(c.x, c.decimals); got != c.want {
				t.Errorf("Nzero(%v, %d) = %v, want %v", c.x, c.decimals, got, c.want)
			}
		})
	}
}
