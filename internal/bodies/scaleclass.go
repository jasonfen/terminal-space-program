package bodies

// ScaleClass is a coarse size/difficulty tag shared by a System and a
// Loadout (ADR 0014). It is purely a classification surfaced as the spawn
// form's craft hint — the integrator derives all dynamics from a Body's
// mass and radius, so a System needs no ScaleClass to work and craft are
// never filtered by it: any Loadout can fly in any System.
type ScaleClass string

const (
	// ScaleReal is the Sol-scale tag: Earth-class bodies, ~9.4 km/s to
	// orbit. The zero value / default — every System and Loadout that
	// does not set one normalizes to real via Scale().
	ScaleReal ScaleClass = "real"
	// ScaleStrippedBack is the Lumen-scale tag: ~1/10-linear bodies with
	// Earth-like surface gravity, ~3.4 km/s to orbit, modelled on the
	// Kerbal Space Program stock system.
	ScaleStrippedBack ScaleClass = "stripped-back"
)

// Normalize maps the empty/unset value to ScaleReal and returns any other
// value unchanged, so callers can compare scale classes without special-
// casing the zero value. Unknown non-empty strings pass through verbatim
// (forward-compatible with overlay-supplied tags).
func (c ScaleClass) Normalize() ScaleClass {
	if c == "" {
		return ScaleReal
	}
	return c
}

// Scale returns the System's normalized ScaleClass (empty => real). Systems
// loaded from JSON without a scaleClass field — the entire pre-Lumen
// catalog — report real.
func (s *System) Scale() ScaleClass {
	return s.ScaleClass.Normalize()
}
