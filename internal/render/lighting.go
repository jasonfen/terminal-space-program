package render

import (
	"fmt"
	"math"
	"strconv"

	"github.com/charmbracelet/lipgloss"
)

// Solar lighting (v0.9.6). Every textured body except the Sun gets a
// day/night terminator: pixels facing the Sun keep their real color,
// pixels on the far side fade toward a dim floor across a smooth
// terminator band. Phase B (eclipse.go) layers a global umbra/penumbra
// dim factor on top so a moon inside its parent's shadow goes "blood
// moon" dark.
//
// The model deliberately reuses the existing projection pipeline: the
// sub-solar point is computed by calling SubObserverPointDeg with the
// Sun direction instead of the camera direction (see orbit.go), so the
// pixel→(lat,lon) inverse (projectPixelToLatLon) and the sub-solar
// point share one frame — including the per-body epoch longitude
// offset, which cancels in the (lon − ssLon) difference below.
const (
	// nightFloor is the minimum illumination on the anti-solar side.
	// Non-zero so the dark hemisphere stays a dim version of the
	// body's real color (disk outline + identity stay readable)
	// rather than collapsing to black.
	nightFloor = 0.18
	// terminatorSoft is the half-width of the terminator band in
	// cosΘ units. ~0.15 gives a band a few px wide on a 32px disk —
	// visibly soft without washing the day/night line out.
	terminatorSoft = 0.15
	// umbraFloor is the floor applied once the eclipse factor is
	// folded in (Phase B). Darker than nightFloor so a fully
	// eclipsed body reads as distinctly dimmer than its own night
	// side, not merely "night".
	umbraFloor = 0.10
)

// SolarLight carries the per-body lighting state the texture wrapper
// needs. SubSolar{Lat,Lon}Deg is the sub-solar point in the same
// (lat,lon) frame projectPixelToLatLon produces. EclipseFactor is the
// Phase-B global dim multiplier (1.0 = no eclipse).
type SolarLight struct {
	SubSolarLatDeg, SubSolarLonDeg float64
	EclipseFactor                  float64
}

// FactorAt returns the illumination factor for the pixel at (dx,dy)
// on a disk of radius r, given the camera sub-observer point
// (subLatDeg, subLonDeg) the shader used to paint it. The result is
// the smooth day/night term times EclipseFactor, clamped to
// [umbraFloor, 1].
//
// subLatDeg/subLonDeg MUST be the camera sub-observer values passed
// to the shader (not the sub-solar ones): projectPixelToLatLon then
// yields the pixel's (lat,lon) in the same frame as SubSolar*, so the
// spherical-cosine angular distance below is frame-consistent and the
// epoch offset cancels.
func (s *SolarLight) FactorAt(dx, dy, r int, subLatDeg, subLonDeg float64) float64 {
	lat, lon, ok := projectPixelToLatLon(dx, dy, r, subLatDeg, subLonDeg)
	if !ok && (lat == 0 && lon == 0) {
		// Degenerate radius (r < 1). No meaningful geometry — treat
		// as fully lit so we never darken a body to nothing on a
		// pathological frame. (The pole case also returns ok=false
		// but with a valid lat; the cos(lat) term zeroes the lon
		// contribution there so the formula stays correct.)
		return clampF(1*s.eclipse(), umbraFloor, 1)
	}

	latR := lat * math.Pi / 180.0
	lonR := lon * math.Pi / 180.0
	ssLatR := s.SubSolarLatDeg * math.Pi / 180.0
	ssLonR := s.SubSolarLonDeg * math.Pi / 180.0

	// Angular distance from the sub-solar point (spherical law of
	// cosines). cosΘ: +1 = sub-solar (noon), 0 = terminator,
	// −1 = anti-solar (midnight). The (lonR − ssLonR) difference
	// cancels the per-body epoch longitude offset that
	// SubObserverPointDeg bakes into both lon and ssLon.
	cosTheta := math.Sin(latR)*math.Sin(ssLatR) +
		math.Cos(latR)*math.Cos(ssLatR)*math.Cos(lonR-ssLonR)

	// Smoothstep across the terminator band, then lift onto
	// [nightFloor, 1].
	t := clampF((cosTheta+terminatorSoft)/(2*terminatorSoft), 0, 1)
	smooth := t * t * (3 - 2*t)
	f := nightFloor + (1-nightFloor)*smooth

	return clampF(f*s.eclipse(), umbraFloor, 1)
}

// eclipse returns EclipseFactor, defaulting a zero value to 1.0 so a
// SolarLight built without Phase B still behaves (no eclipse).
func (s *SolarLight) eclipse() float64 {
	if s.EclipseFactor <= 0 {
		return 1.0
	}
	return s.EclipseFactor
}

// Shade multiplies an "#RRGGBB" lipgloss color's RGB channels by f
// (clamped to [0,1]) and returns the darkened color. Malformed or
// non-hex input is returned unchanged — render-path code must never
// panic, and an un-darkened pixel is a far better failure mode than a
// crash mid-frame.
func Shade(c lipgloss.Color, f float64) lipgloss.Color {
	f = clampF(f, 0, 1)
	s := string(c)
	if len(s) != 7 || s[0] != '#' {
		return c
	}
	rv, errR := strconv.ParseUint(s[1:3], 16, 8)
	gv, errG := strconv.ParseUint(s[3:5], 16, 8)
	bv, errB := strconv.ParseUint(s[5:7], 16, 8)
	if errR != nil || errG != nil || errB != nil {
		return c
	}
	scaleCh := func(v uint64) int {
		out := int(math.Round(float64(v) * f))
		if out < 0 {
			out = 0
		} else if out > 255 {
			out = 255
		}
		return out
	}
	return lipgloss.Color(fmt.Sprintf("#%02X%02X%02X",
		scaleCh(rv), scaleCh(gv), scaleCh(bv)))
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
