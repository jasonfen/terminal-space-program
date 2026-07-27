package sim

import (
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// #221 part 2 (ADR 0027 v0.32 amendment §2): spawn-form band warnings
// are computed per-primary by sampling the PRODUCTION connectivity
// model over orbit phase × body rotation phase — never a second
// analytic formula that drifts the moment stations, occlusion, or a
// user overlay changes. The amendment's measured table is the oracle:
// Earth degrades at 5,000/10,000 km; Kern (~10× smaller) is EXACTLY
// INVERTED — degraded at 500–2,000 km, clean at 5,000+.

func bandWorld(t *testing.T) *World {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

func TestCommBandCoverageEarth(t *testing.T) {
	w := bandWorld(t)

	if cov, ok := w.CommBandCoverage("earth", 500e3, 0); !ok || cov < 1.0 {
		t.Errorf("Earth 500 km sits inside the home blanket: cov=%.3f ok=%v, want 1.0", cov, ok)
	}
	if cov, ok := w.CommBandCoverage("earth", 5000e3, 0); !ok || cov <= 0.05 || cov >= CommBandDegradedThreshold {
		t.Errorf("Earth 5,000 km is the measured dead band (~61%%): cov=%.3f ok=%v", cov, ok)
	}
	if cov, ok := w.CommBandCoverage("earth", 35786e3, 0); !ok || cov < CommBandDegradedThreshold {
		t.Errorf("equatorial geosync is clean: cov=%.3f ok=%v", cov, ok)
	}
}

func TestCommBandCoverageMarsOutOfReach(t *testing.T) {
	w := bandWorld(t)
	cov, ok := w.CommBandCoverage("mars", 5000e3, 0)
	if !ok {
		t.Fatalf("mars is in the active system; ok=false")
	}
	if cov != 0 {
		t.Errorf("Mars hosts no stations and sits beyond direct-basic reach of Earth's ring: cov=%.3f, want 0", cov)
	}
}

func TestCommBandCoverageKernInversion(t *testing.T) {
	w := bandWorld(t)
	kernIdx := -1
	for i, sys := range w.Systems {
		if sys.FindBody("kern") != nil {
			kernIdx = i
		}
	}
	if kernIdx < 0 {
		t.Skip("no system carries kern (Lumen catalog absent)")
	}
	w.SystemIdx = kernIdx

	low, ok := w.CommBandCoverage("kern", 500e3, 0)
	if !ok {
		t.Fatalf("kern lookup failed in its own system")
	}
	high, _ := w.CommBandCoverage("kern", 5000e3, 0)
	// The decisive inversion: an Earth-derived label would flag exactly
	// the wrong presets at Kern.
	if low >= CommBandDegradedThreshold {
		t.Errorf("Kern 500 km is in ITS band (blanket edge ≈300 km): cov=%.3f", low)
	}
	if high < CommBandDegradedThreshold {
		t.Errorf("Kern 5,000 km is clean: cov=%.3f", high)
	}
}

func TestCommBandCoverageUnknownBody(t *testing.T) {
	w := bandWorld(t)
	if _, ok := w.CommBandCoverage("no-such-body", 500e3, 0); ok {
		t.Errorf("unknown body must report ok=false")
	}
}

func TestCommBandCoverageAntennaAware(t *testing.T) {
	// Review finding (v0.32 batch): the sampler must model the antenna
	// actually being spawned — a Relay-Tug at the Moon links to Earth's
	// ring (√(1e9·5e9) ≈ 2.24e9 m > the ~3.84e8 m separation) and must
	// NOT be warned "out of network reach", while a direct-basic probe
	// there genuinely is out of reach (√(1e7·5e9) ≈ 2.24e8 m).
	w := bandWorld(t)
	basic, ok := w.CommBandCoverage("moon", 100e3, spacecraft.AntennaRangeDirectBasic)
	if !ok || basic != 0 {
		t.Errorf("direct-basic at the Moon: cov=%.3f ok=%v, want 0 (out of reach)", basic, ok)
	}
	relay, ok := w.CommBandCoverage("moon", 100e3, spacecraft.AntennaRangeRelayCislunar)
	if !ok || relay <= 0.2 {
		t.Errorf("relay-cislunar at the Moon reaches Earth's ring: cov=%.3f ok=%v, want well above 0", relay, ok)
	}
}
