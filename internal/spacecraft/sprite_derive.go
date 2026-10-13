package spacecraft

import "math"

// Launch-sprite size and colour for a stage built from parts (#596).
// Catalog stages author LaunchSpriteRowsPx / Width / Color by hand; a stage
// the player assembles in the VAB has no author, so composePart derives
// them from the stage's mass and fuel chemistry. Rows are on the same
// scale the catalog uses (one row is one launch-view sub-pixel, 1.5 m).

const (
	minDerivedRows = 3
	maxDerivedRows = 28

	// Anchors for the height rule: the catalog S-IVB (120 t, 12 rows) and
	// S-IC (2290 t, 24 rows). Height is log-linear in mass between them
	// (about 9.4 rows per decade), so a parts-built stage lands near the
	// catalog stage it imitates; the catalog S-II (480 t, 20 rows) comes
	// out at 18.
	derivAnchorMassKg = 120000.0
	derivAnchorRows   = 12.0
	derivRowsPerDec   = 9.4
	// Width: 3 at the anchor, +1.5 per decade (S-II 4, S-IC 5).
	derivAnchorWidth = 3.0
	derivWidthPerDec = 1.5
)

// derivedSpriteDims returns the launch-sprite rows and width for a stage
// of the given total mass (dry + tank capacity, kg).
func derivedSpriteDims(massKg float64) (rows, width int) {
	if massKg < 1 {
		massKg = 1
	}
	dec := math.Log10(massKg / derivAnchorMassKg)
	rows = int(math.Round(derivAnchorRows + derivRowsPerDec*dec))
	if rows < minDerivedRows {
		rows = minDerivedRows
	}
	if rows > maxDerivedRows {
		rows = maxDerivedRows
	}
	width = int(math.Round(derivAnchorWidth + derivWidthPerDec*dec))
	if width < 2 {
		width = 2
	}
	if width > 5 {
		width = 5
	}
	return rows, width
}

// derivedSpriteColor is the body colour of a parts-built stage, by fuel
// chemistry: the same hue families as the exhaust flames (kerolox orange,
// hydrolox cyan) in muted body-paint tones, so it never reads as the
// vessel's yellow, Target green, a warning amber or the planned-burn
// periwinkle. A stage with no propellant is neutral grey.
func derivedSpriteColor(fuelType string) string {
	switch fuelType {
	case FuelTypeKerolox:
		return "#D98A5F" // terracotta
	case FuelTypeHydrolox:
		return "#8FC4D6" // ice blue
	case FuelTypeHypergolic:
		return "#C9B58A" // khaki
	case FuelTypeSolid:
		return "#A89CB8" // slate mauve
	}
	return "#C8C8C8"
}

// applyDerivedSprite fills any sprite field the Part leaves unset, from the
// Part's own mass and chemistry. Authored values always win. A massless
// Part (an empty stage) stays undrawn.
func applyDerivedSprite(p *Part) {
	mass := p.DryMassKg + p.FuelCapacityKg
	if mass <= 0 {
		return
	}
	rows, width := derivedSpriteDims(mass)
	if p.LaunchSpriteRowsPx == 0 {
		p.LaunchSpriteRowsPx = rows
	}
	if p.LaunchSpriteWidthPx == 0 {
		p.LaunchSpriteWidthPx = width
	}
	if p.LaunchSpriteColor == "" {
		p.LaunchSpriteColor = derivedSpriteColor(p.FuelType)
	}
}
