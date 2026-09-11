// Package readout is the single formatter for every number a flight
// surface prints: the ADR 0049 Readout Contract. It owns the duration
// humaniser, the SI ladder, the off-ladder precision helper and the
// label constants, so no two panels disagree on the arithmetic.
//
// This package replaces five duration formatters and four distance
// formatters that used to live in internal/tui/screens (compactDuration,
// formatDurationShort, formatPeriod, formatTCA, formatCountdown,
// formatRangeM, formatChipKm, formatAltKm, formatAltitude). Those are
// deleted at the call sites in a later stage of this ADR, not wrapped:
// this package is the only place ladder rungs, duration bands and
// significant-figure rules are decided.
package readout

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
)

// --- Label constants (ADR 0049 decision 6 rename table / action-plan
// decision 6). Labels are lowercase words; the codes that survive
// (fpa, Q, TCA, TWR, Ap/Pe, Δv/Δincl) keep their standard case. No
// underscores, no absolute-value bars, no "sas" anywhere player-facing.
// ---

const (
	LabelVert     = "vert:"      // v_vert -> vert: (the m/s already says speed)
	LabelHoriz    = "horiz:"     // v_horiz -> horiz:
	LabelFPA      = "fpa:"       // unchanged code; the ascent flight-path angle
	LabelOrbitFPA = "orbit fpa:" // fpa_orbit -> orbit fpa:
	LabelQ        = "Q:"         // unchanged code; dynamic pressure
	LabelTWR      = "TWR:"       // twr -> TWR:
	LabelHold     = "hold:"      // sas -> hold: (matches the ATTITUDE panel's word)
	LabelAp       = "Ap:"        // ap -> Ap:
	LabelPe       = "Pe:"        // pe -> Pe:
	LabelApo      = "apo:"       // t_to_apo -> apo: (value is a T- countdown)
	// LabelPeri is apo:'s obvious sibling for the ORBIT chip's t→Pe: row.
	// Not in the action-plan's own rename table (built from review
	// findings that never flagged this ORBIT-chip-only row), added on
	// gate review: leaving it as t→Pe: kept two dialects for one
	// quantity, apo:/peri: on SURFACE and t→Ap:/t→Pe: on ORBIT.
	LabelPeri = "peri:"
	// LabelEntry is the SOI PASS chip's SOI-entry countdown row (F4, gate
	// review): was "T-entry:", an unsigned duration under a label that
	// carried the direction word itself, two rows from `TCA: T-4h43m` for
	// the same kind of quantity on the same chip. Signed through
	// readout.Countdown like every other countdown in the contract.
	LabelEntry     = "entry:"
	LabelBurn      = "burn:"      // t_burn -> burn:
	LabelIncl      = "incl:"      // inclin. -> incl:
	LabelDeltaIncl = "Δincl:"     // Δi -> Δincl:
	// LabelDepart is ADR 0050 decision 5's `depart:` row: the angle
	// between the orbit a launch would reach (or, in flight, the live
	// orbit) and the plane the world beneath the player travels in.
	LabelDepart = "depart:"
	LabelApproach  = "approach:"  // Proximity View's CA: -> approach:
	LabelTCA       = "TCA:"       // unchanged code
	LabelRelSpeed  = "rel speed:" // |v_rel| -> rel speed:
	LabelArrival   = "arrival:"   // capture chip's "approach: N m/s relative" -> arrival:
	LabelImpact    = "impact:"    // impact in: -> impact:
	LabelAltitude  = "altitude:"  // unchanged label, now rides the SI ladder
	LabelPeriod    = "period:"    // unchanged label; the one duration that keeps seconds
	LabelDeltaV    = "Δv:"        // Δv budget -> Δv: <stage> / <vehicle> m/s
)

// auThreshold is the existing 0.1 AU distance-ladder boundary, reused
// verbatim from formatRangeM (orbit_chip_builders.go), which switched to
// AU at `rangeM > bodies.AU/10`. Gm covers up to (not including) this
// value; AU starts at it.
const auThreshold = bodies.AU / 10

// Nzero snaps a value whose magnitude rounds to zero at `decimals` places
// to +0, so a quantity that jitters across zero (v_vert, fpa, an altitude
// riding co-rotation noise) doesn't flicker a "-0" / "-0.0" sign from one
// frame to the next. Only the sign of an already-zero display changes;
// non-zero values pass through untouched. Ported verbatim from the
// `nzero` helper in orbit_chip_builders.go / formatRangeM.
//
// Uses math.RoundToEven, not math.Round, to decide "rounds to zero": Go's
// own %.*f verb rounds ties to even (confirmed by probing fmt.Sprintf, not
// assumed: an exact binary tie like -0.5 or -2.5 rounds to the nearest
// EVEN integer, e.g. -0.5 -> "-0", -2.5 -> "-2"), while math.Round always
// rounds ties away from zero. At exactly -0.5 the two disagreed (F2, gate
// review): math.Round(-0.5) is -1, not 0, so the old check said "doesn't
// round to zero" and let -0.5 pass through unsnapped, and fmt then printed
// it as "-0" anyway, exactly the flicker this function exists to prevent.
// Matching fmt's own rounding rule here closes that gap for every caller.
func Nzero(x float64, decimals int) float64 {
	scale := math.Pow(10, float64(decimals))
	if math.RoundToEven(x*scale) == 0 {
		return 0
	}
	return x
}

// --- Durations (ADR decision 1 / 2; action-plan decisions 1, 2, 12) ---

// Duration renders a duration's magnitude as two adjacent units,
// zero-padded on the second: "45s", "2m07s", "4h43m", "3d05h". It
// truncates rather than rounds, so a countdown built on it never claims
// more time than there is (4h43m59s renders "4h43m", not "4h44m"). The
// sign of d is ignored; use Countdown for a signed, launch-convention
// reading.
func Duration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	total := int64(d / time.Second) // truncating division, never rounds
	days := total / 86400
	hours := (total % 86400) / 3600
	mins := (total % 3600) / 60
	secs := total % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd%02dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh%02dm", hours, mins)
	case mins > 0:
		return fmt.Sprintf("%dm%02ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// Period renders a duration keeping all three units down to seconds:
// "45s", "3m45s", "1h34m28s". It is the one exception to the two-unit
// Duration rule (`period:` is the label it's reserved for) because a
// resonant or phasing orbit is tuned against the period to single-second
// precision; rounding away the seconds digit there costs real placement
// error over many revolutions.
func Period(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	total := int64(d / time.Second)
	hours := total / 3600
	mins := (total % 3600) / 60
	secs := total % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh%02dm%02ds", hours, mins, secs)
	case mins > 0:
		return fmt.Sprintf("%dm%02ds", mins, secs)
	default:
		return fmt.Sprintf("%ds", secs)
	}
}

// Countdown signs a duration using the launch convention: `T-` means
// until the event, `T+` means since it. Pass the signed time until the
// event: positive is a future event ("T-4h43m"), zero or negative is an
// elapsed or overdue one ("T+2m10s" for a node missed by 2m10s, "T+0s"
// for an event happening now). The label never carries the direction
// word; this sign does.
func Countdown(until time.Duration) string {
	if until > 0 {
		return "T-" + Duration(until)
	}
	return "T+" + Duration(-until)
}

// --- SI ladder: distance, mass, thrust (ADR decision 3; action-plan
// decisions 3, 4) ---

// sigDecimals returns the decimal count for a 4-significant-figure
// reading of a positive magnitude already expressed in the target unit,
// capped at maxDecimals. A 4-digit integer part needs 0 decimals, a
// single leading digit needs 3 (capped to maxDecimals where that's
// smaller). Values below 1 keep 1 leading digit ("0") for this purpose.
func sigDecimals(magnitude float64, maxDecimals int) int {
	dec := 4 - intDigits(magnitude)
	if dec < 0 {
		dec = 0
	}
	if dec > maxDecimals {
		dec = maxDecimals
	}
	return dec
}

// chooseDecimals is sigDecimals corrected for its own rounding: picking
// the decimal count from the PRE-rounding magnitude is one rounding
// step short of right at every intra-rung decade crossing (gate review
// F1). 9.9996 has one integer digit, so sigDecimals hands it 3 decimals
// for 4 significant figures, but fmt's own %.3f then rounds 9.9996 up
// to 10.000, which only needs 2 decimals at two integer digits. The
// naive result prints 10.000 (five significant figures) instead of the
// correct 10.00. Re-deriving the decimal count from the ROUNDED value
// catches this: round once at the naive count, recompute sigDecimals on
// what that rounding produced, and use the new count if it differs.
// Rounding can only carry a magnitude up by a fraction of one unit at
// the chosen precision, never by a full extra decade beyond that, so a
// single correction pass is always enough: no loop, and no interaction
// with intDigits' own Inf/NaN guard (both sigDecimals calls funnel
// through it, so a non-finite magnitude returns its fallback decimal
// count twice and exits after one pass either way).
func chooseDecimals(magnitude float64, maxDecimals int) int {
	dec := sigDecimals(magnitude, maxDecimals)
	scale := math.Pow(10, float64(dec))
	rounded := math.Round(magnitude*scale) / scale
	if newDec := sigDecimals(rounded, maxDecimals); newDec != dec {
		return newDec
	}
	return dec
}

// intDigits counts the digits left of the decimal point in a positive
// magnitude ("0" counts as 1 digit for anything below 1). Computed by
// repeated division rather than log10 to avoid float log10 landing on
// the wrong side of an exact power of ten.
//
// A formatter must be total over its input type: ±Inf never satisfies
// `v >= 10 { v /= 10 }` (dividing infinity by ten is still infinity), so
// without this guard the loop never terminates. That hangs whatever
// Bubble Tea View() called it with no panic and no log line, freezing
// the whole TUI, not reachable from a checked call site today, but
// "not reachable today" is a property of the current call sites, not of
// this package, which every future readout is about to route through.
// Falling back to 1 leaves fmt's own float verbs to print the value:
// Go's %f/%.*f family already renders "+Inf"/"-Inf"/"NaN" verbatim
// regardless of the requested precision, matching what the plain fmt
// calls this package replaced printed before the contract existed.
func intDigits(magnitude float64) int {
	if math.IsNaN(magnitude) || math.IsInf(magnitude, 0) {
		return 1
	}
	if magnitude < 1 {
		return 1
	}
	n := 1
	v := magnitude
	for v >= 10 {
		v /= 10
		n++
	}
	return n
}

type ladderRung struct {
	unit    string
	divisor float64
	// promoteAt is the rounded-value threshold, in this rung's own unit,
	// at which formatLadder promotes to the next rung rather than adding
	// a digit. 1000 for every ordinary rung: a value that would need a
	// 4th integer digit at 0 decimals promotes instead of losing the
	// contract's 4-significant-figure precision. km is the deliberate
	// exception, at 10000 (maintainer ruling, gate review addendum):
	// decision 3's own worked examples (`2590 km`) and decision 4's
	// (`Pe: 192.0 km`, `altitude: 500.0 km`) only ALL hold true at once
	// if the km rung runs a full extra decade, to 9999 km, before
	// promoting to Mm. The first pass here had read `2590 km` as a
	// transcription error against a uniform ladder; re-reading the whole
	// example set together shows the uniform ladder was the error.
	promoteAt float64
}

var distanceRungs = []ladderRung{
	{"m", 1, 1000},
	{"km", 1e3, 10000},
	{"Mm", 1e6, 1000},
	{"Gm", 1e9, 1000},
	{"AU", bodies.AU, 1000}, // last rung: promoteAt is never consulted
}

// distanceRungIndex picks the starting rung for a non-negative distance
// in meters, by the ADR's explicit thresholds: m below 1 km, km below
// 1e7 m (the extended km rung, see ladderRung.promoteAt), Mm below 1e9 m,
// Gm below the existing 0.1 AU threshold, AU above it. The Mm rung's own
// bottom is therefore 10 Mm, not 1 Mm: a value never renders as
// "5.000 Mm" because km already covers everything below 10 Mm. That is
// the deliberate consequence of the extended km rung, not a gap.
// formatLadder below still checks for a rounding-induced rollover past
// this starting guess (e.g. 9,999,600 m formats as 9999.6 km, which
// rounds to 10000 km and must promote to Mm even though 9999.6 itself
// was still under the 1e7 threshold this function checks).
func distanceRungIndex(av float64) int {
	switch {
	case av < 1e3:
		return 0
	case av < 1e7:
		return 1
	case av < 1e9:
		return 2
	case av < auThreshold:
		return 3
	default:
		return 4
	}
}

// formatLadder renders a signed value through a unit ladder, picking the
// rung by magnitude and then re-checking for a rounding-induced rollover
// into the next rung up (a value that rounds up to or past its rung's own
// promoteAt is re-expressed one rung higher, e.g. 9,999,600 m rounds to
// 10000 km at the extended km rung's own boundary and promotes: "10.00 Mm"
// rather than "10000 km", see distanceRungs' promoteAt comment). The
// `idx == 0` branch below forces the bottom rung (meters or kilograms) to
// whole-number precision: the concrete examples in the ADR for sub-1000
// low-rung values ("912 m", "999 kg") are 0-decimal, not the
// 4-significant-figure decimal count the rest of the ladder uses: a
// below-the-decimal-point reading at the finest unit carries no
// information a flight computer needs.
func formatLadder(signed float64, rungs []ladderRung, startIdx int, maxDecimals int) string {
	av := math.Abs(signed)
	idx := startIdx
	for {
		r := rungs[idx]
		val := av / r.divisor
		dec := maxDecimals
		if idx == 0 {
			dec = 0
		} else {
			dec = chooseDecimals(val, maxDecimals)
		}
		roundedUp := math.Round(val*math.Pow(10, float64(dec))) / math.Pow(10, float64(dec))
		if roundedUp >= r.promoteAt && idx < len(rungs)-1 {
			idx++
			continue
		}
		out := Nzero(signed/r.divisor, dec)
		return fmt.Sprintf("%.*f %s", dec, out, r.unit)
	}
}

// Distance renders a distance in meters through the SI ladder: m below 1
// km, km, Mm, Gm, then AU above the existing 0.1 AU threshold, at 4
// significant figures per rung (meters stay whole numbers, see
// formatLadder). The rung is always printed, so a unit change is never
// silent. A negative distance (a sub-surface periapsis) keeps its signed
// depth: "-120.0 km".
func Distance(m float64) string {
	av := math.Abs(m)
	return formatLadder(m, distanceRungs, distanceRungIndex(av), 3)
}

var massRungs = []ladderRung{
	{"kg", 1, 1000},
	{"t", 1e3, 1000}, // last rung (no kt): promoteAt is never consulted
}

// Mass renders a mass in kilograms through the ladder: kg below 1000 kg,
// then t, at 4 significant figures. There is no `kt` rung: a mass whose
// tonnage exceeds 4 significant figures still prints in t with more
// digits, since there is nowhere higher to promote it.
func Mass(kg float64) string {
	av := math.Abs(kg)
	idx := 0
	if av >= 1000 {
		idx = 1
	}
	return formatLadder(kg, massRungs, idx, 3)
}

// Thrust renders a thrust in newtons as kN, always (never raw N), at 4
// significant figures. There is no ladder to climb; kN is the one unit.
func Thrust(n float64) string {
	dec := chooseDecimals(math.Abs(n)/1000, 3)
	return fmt.Sprintf("%.*f kN", dec, Nzero(n/1000, dec))
}

// Pressure renders a dynamic pressure in pascals as kPa, always (never
// raw Pa or a ladder), at 4 significant figures: mirrors Thrust, the
// other single-unit off-ladder reading. Input in pascals so callers
// (Q on the ATMOSPHERE chip and the launch strip) pass the SI value they
// already compute, not a pre-divided kPa figure.
func Pressure(pa float64) string {
	dec := chooseDecimals(math.Abs(pa)/1000, 3)
	return fmt.Sprintf("%.*f kPa", dec, Nzero(pa/1000, dec))
}

// --- Off-ladder precision: speed, Δv, orbital angles (ADR decision 4;
// action-plan decision 12) ---

// precision2 is the off-ladder sibling of sigDecimals: 4 significant
// figures, capped at 2 decimals rather than 3. Shared by Speed, DeltaV
// and Angle, which never ride the SI ladder.
func precision2(magnitude float64) int {
	return chooseDecimals(magnitude, 2)
}

func formatMps(mps float64) string {
	dec := precision2(math.Abs(mps))
	return fmt.Sprintf("%.*f", dec, Nzero(mps, dec))
}

// formatDeltaVMps is DeltaV's per-number formatter, shared with
// DeltaVPair so both halves of a stage/vehicle row use the same rule.
func formatDeltaVMps(mps float64) string {
	av := math.Abs(mps)
	if av >= 100 {
		return fmt.Sprintf("%.0f", Nzero(mps, 0))
	}
	// A value just under the integer band can still round up into it at
	// the below-100 branch's own precision (99.996 rounds to 100.0 at 1
	// decimal): the same decade-crossing gate review F1 fixed in
	// chooseDecimals applies to DeltaV's OWN 100 m/s threshold, not just
	// to a ladder rung. Round once at the precision the below-100 branch
	// would use and re-check the threshold before committing to it.
	dec := precision2(av)
	scale := math.Pow(10, float64(dec))
	if math.Round(av*scale)/scale >= 100 {
		return fmt.Sprintf("%.0f", Nzero(mps, 0))
	}
	return fmt.Sprintf("%.*f", dec, Nzero(mps, dec))
}

// Speed renders a speed in m/s at 4 significant figures, capped at 2
// decimals, at every magnitude: "5519 m/s", "40.00 m/s", "0.12 m/s".
// Speeds never ride the SI ladder; they stay in m/s at every magnitude
// the game reaches.
//
// Adjudicated (gate review, see impl-notes/item4-A1-readout-pkg.md):
// decision 12's own worked examples (`22.50 m/s`, `28.60°`) are 2-decimal
// at two integer digits, so the ADR's lone `40.0 m/s` is a transcription
// of a pre-contract readout, not a contract output. `Speed` implements
// the stated rule at every magnitude, unlike `DeltaV` below, which the
// same decision 12 sentence pins to an integer at and above 100 m/s.
func Speed(mps float64) string {
	return formatMps(mps) + " m/s"
}

// SignedSpeed renders a relative rate (closing, approach) at Speed's same
// 4-sig-fig / 2-decimal-cap precision, but with an explicit sign on
// positive values and zero, matching the closing:/rate-style rows that
// need "toward" versus "away" legible without a separate word: "+3640
// m/s", "-12.34 m/s", "+0.00 m/s".
func SignedSpeed(mps float64) string {
	s := Speed(mps)
	if strings.HasPrefix(s, "-") {
		return s
	}
	return "+" + s
}

// DeltaV renders a Δv figure in m/s. Decision 12: "Δv keeps its integer
// m/s at every magnitude the game reaches (22.50 m/s only below 100)",
// so DeltaV is a whole number at |v| >= 100 and falls back to the
// 4-sig-fig / 2-decimal-cap helper below it: "5519 m/s", "550 m/s",
// "99.90 m/s", "22.50 m/s", "0.12 m/s".
//
// Adjudicated (gate review): this deliberately differs from Speed in the
// 100-999.9 m/s band, where Speed keeps one decimal (e.g. "550.0 m/s")
// and DeltaV does not ("550 m/s"). The VESSEL chip's stage Δv sits in
// that band constantly on a late stage; an integer there removes the
// jittering tenths digit the contract exists to remove.
func DeltaV(mps float64) string {
	return formatDeltaVMps(mps) + " m/s"
}

// DeltaVPair renders the two-number Δv row (action-plan decision 7): the
// active stage's remaining Δv, then the whole remaining stack's total,
// sharing one trailing unit: "3518 / 9412 m/s". Both numbers use
// DeltaV's integer-at-100-and-above rule. A single-stage vessel should
// call DeltaV instead for the one-number form ("3518 m/s").
func DeltaVPair(stageMps, vehicleMps float64) string {
	return formatDeltaVMps(stageMps) + " / " + formatDeltaVMps(vehicleMps) + " m/s"
}

// Angle renders an orbital angle in degrees at 4 significant figures,
// capped at 2 decimals: "28.60°", "0.00°". Used for inclination, Δincl,
// and any other orbital-element angle; distinct from TrimAngle and FPA,
// which are the integer pad/ascent controls.
func Angle(deg float64) string {
	dec := precision2(math.Abs(deg))
	return fmt.Sprintf("%.*f°", dec, Nzero(deg, dec))
}

// TrimAngle renders a signed, whole-degree pitch-trim readout with an
// explicit sign on positive values and zero, matching the existing
// PitchTrim formatter's "%+.1f°" convention (orbit_chip_builders.go:1392)
// at decision 12's integer precision: "+12°", "-10°", "+0°". Distinct
// from FPA, whose existing formatters print no plus sign.
func TrimAngle(deg float64) string {
	return fmt.Sprintf("%+.0f°", Nzero(deg, 0))
}

// FPA renders a whole-degree flight-path-angle readout with a minus sign
// only, no explicit plus: "45°", "-10°", "0°". Matches today's fpa
// formatters (orbit_chip_builders.go:1398, :1555; launch.go:1173), which
// use "%.0f°" with nzero applied and never a "+". Distinct from
// TrimAngle, which does carry an explicit sign.
func FPA(deg float64) string {
	return fmt.Sprintf("%.0f°", Nzero(deg, 0))
}

// Heading renders a compass heading as a zero-padded, unsigned
// three-digit degree string: "090°". Input is normalized into [0, 360)
// before rounding.
func Heading(deg float64) string {
	d := math.Mod(deg, 360)
	if d < 0 {
		d += 360
	}
	r := math.Round(d)
	if r >= 360 {
		r = 0
	}
	return fmt.Sprintf("%03.0f°", r)
}
