package spacecraft

// FuelType constants (v0.11.5). The renderer maps each value to a
// flame colour in internal/tui/screens/launch_sprite.go; the in-game
// vocabulary lives in CONTEXT.md under "Maneuver & thrust".
const (
	FuelTypeKerolox    = "kerolox"    // RP-1 + LOX (F-1, Merlin) — orange
	FuelTypeHydrolox   = "hydrolox"   // LH2 + LOX (J-2, RS-25, RL-10) — pale cyan
	FuelTypeHypergolic = "hypergolic" // Aerozine 50 + N2O4 (LM, SPS) — yellow-amber
	FuelTypeSolid      = "solid"      // APCP (SLS SRB) — orange-red
)

// Stage describes one decouplable propulsion module on a spacecraft.
// v0.9.1+. The Stages slice on Spacecraft is the source of truth for
// dry mass / fuel / engine numbers; the historical flat fields
// (DryMass, Fuel, Thrust, Isp, Monoprop, MonopropCapacity,
// RCSThrust, RCSIsp) are derived shadow-mirror values refreshed by
// SyncFields after any stage mutation. Read sites keep using the
// flat fields; write sites must mutate the relevant stage entry and
// call SyncFields to keep the mirror coherent.
//
// Convention: Stages[0] is the BOTTOM stage — the currently-firing
// engine and the next to be jettisoned by World.StageActive.
// Stages[len-1] is the TOP stage (the player's "core" — the only
// one left after every lower stage has been decoupled).
//
// During ascent on a Saturn V (Stages = [S-IC, S-II, S-IVB]), the
// S-IC bottom stage provides the firing thrust + Isp; total mass
// sums dry + fuel across all three stages. Pressing `space` pops
// Stages[0] (S-IC) and spawns it as a passive Spacecraft; the
// active craft is now [S-II, S-IVB] with S-II as the new bottom
// stage / firing engine.
//
// Single-stage craft carry exactly one Stage; on staging that
// stage is the bottom + top simultaneously. World.StageActive
// declines to drop the only stage of a single-stage craft (no-op
// + status flash) so the player doesn't accidentally jettison
// their core.
type Stage struct {
	// DryMass is the empty-tank mass of this stage in kg —
	// engine + structure, no propellant.
	DryMass float64

	// FuelMass is the current main-engine propellant mass in kg.
	// Decrements during finite burns; the burn engine reads from
	// Stages[0].FuelMass (the bottom stage's tank).
	FuelMass float64

	// FuelCapacity is the max main-engine propellant load in kg.
	// Determines undocking proportional shares + spawn-form
	// readouts.
	FuelCapacity float64

	// Thrust is the main-engine thrust in N. Zero disables the
	// main engine for this stage (RCS-only stages or empty
	// boosters).
	Thrust float64

	// Isp is the main-engine specific impulse in seconds. Used
	// by the rocket equation in finite-burn integration.
	Isp float64

	// MonopropMass is the current RCS-pool propellant mass in kg.
	// Per-stage: each stage carries its own RCS budget. The
	// active craft's RCS reads from Stages[0].MonopropMass when
	// the bottom stage has RCS, falling back to upper stages
	// only after the bottom is empty (or via a future explicit
	// RCS-source-stage selector — for v0.9.1, bottom-only).
	MonopropMass float64

	// MonopropCap is the max RCS-pool capacity in kg.
	MonopropCap float64

	// RCSThrust is the per-stage RCS thrust in N.
	RCSThrust float64

	// RCSIsp is the per-stage RCS specific impulse in seconds.
	RCSIsp float64

	// BallisticCoefficient (v0.9.2.1+) is C_D · A / m in m²/kg —
	// the multiplicative factor in the drag equation
	// a = -0.5 · ρ · |v_rel|² · BC · v̂_rel. Per-stage so a Saturn
	// V's S-IC booster (huge cross-section, ~3 Mkg mass, BC ≈ 8e-6)
	// drags differently than its S-IVB upper stage (small craft,
	// ~120 kkg mass, BC ≈ 6e-5). Zero falls back to
	// DefaultBallisticCoefficient at the Spacecraft level.
	BallisticCoefficient float64

	// LoadoutID names the catalog entry that originally produced
	// this stage. Used by save round-trip + spawn-as-passive on
	// jettison so the dropped stage gets the right glyph + colour.
	// Empty when the stage came from a manual construction; the
	// jettison path falls back to a generic "stage" identity.
	LoadoutID string

	// Name is the per-stage display label (e.g. "S-IC", "S-II",
	// "S-IVB"). Used by the STAGES HUD block + the spawn-as-
	// passive jettison path. Empty falls back to LoadoutID.
	Name string

	// Glyph + Color override the canvas marker for this stage
	// when it's jettisoned and spawned as a passive craft. Empty
	// resolves via LoadoutID lookup (default: "▲" / "#FFD93D"
	// like the S-IVB-1 main loadout).
	Glyph string
	Color string

	// LaunchSpriteRowsPx is the per-stage height (in braille
	// sub-pixels) of this stage's silhouette in the ViewLaunch
	// chase-cam scene. Stack composes bottom-to-top from Stages[0]
	// along CurrentAttitudeDir; each stage paints a
	// (spriteWidthPx × LaunchSpriteRowsPx) filled rectangle of
	// braille dots via PlotColored. Zero means "no sprite, fall
	// back to the vessel-level Glyph render." Pivoted from ASCII
	// glyphs to braille pixels in v0.11.3 after playtest showed
	// box-drawing characters smear at gravity-turn angles
	// (see designdocs/terminal-space-program/v0.11-plan.md "Resolved at slice-open").
	LaunchSpriteRowsPx int `json:",omitempty"`

	// LaunchSpriteWidthPx (v0.11.5) is the per-stage width (in
	// braille sub-pixels) of this stage's ViewLaunch silhouette.
	// Zero falls back to the renderer's default width (2 px — the
	// pre-v0.11.5 universal constant), so un-catalogued and pre-
	// v0.11.5 stages keep their original 2-wide rectangle. Practical
	// range [1, 5] sub-pixels; catalog values are stylised, not
	// physics-derived. Each stage paints a width × LaunchSpriteRowsPx
	// rectangle centred on the stack axis — no auto-clamping based on
	// neighbour widths; the catalog author tunes which stage-to-stage
	// boundaries hard-step vs taper (see inter-stage taper rule in
	// launch_sprite.go).
	LaunchSpriteWidthPx int `json:",omitempty"`

	// LaunchSpriteColor (v0.11.5-followup) overrides Color for the
	// launch-sprite body / engine bell / inter-stage taper / landing
	// legs. Decouples slate HUD identity (Color, glyph palette) from
	// silhouette identity: catalog authors can keep distinct slate
	// hues per stage while painting the full stack in a unified
	// rocket-body palette so a 5-stage Apollo Stack doesn't read as
	// a rainbow gradient on screen. Empty falls back to Color so
	// un-catalogued and pre-v0.11.5 stages keep their existing
	// silhouette colour.
	LaunchSpriteColor string `json:",omitempty"`

	// FuelType (v0.11.5) names the main-engine propellant chemistry
	// so the renderer can tint the exhaust flame to a per-fuel
	// palette. Values: "kerolox" (RP-1 + LOX, orange — F-1, Merlin),
	// "hydrolox" (LH2 + LOX, pale cyan — J-2, RS-25, RL-10),
	// "hypergolic" (Aerozine 50 + N2O4, yellow-amber — LM descent,
	// SPS), "solid" (APCP, orange-red — SLS SRB). Empty/unset
	// preserves pre-v0.11.5 amber flame (render.ColorWarning).
	// Stages with Thrust == 0 (pure-monoprop RCS tugs) leave this
	// unset — no main engine ⇒ no flame ⇒ no fuel-type read.
	FuelType string `json:",omitempty"`

	// LaunchSpriteHasLegs (v0.11.5) opts a stage into the diagonal
	// landing-leg silhouette. When true AND the stage is Stages[0],
	// the renderer paints two splayed diagonal lines of sub-pixels
	// from the stage's bottom corners outward and downward, mirrored
	// about the stack axis, in Stages[0].Color. Painted in the
	// (stack-dir, width-dir) basis so legs lean with the rocket
	// through gravity turns (the v0.11.3 direction-agnostic
	// invariant). Suppressed when the stage is NOT at Stages[0] —
	// upper-stage landing legs don't read as the iconic Lander
	// silhouette. Today's only true entry: the Apollo Lander.
	LaunchSpriteHasLegs bool `json:",omitempty"`

	// CanSoftLand (v0.11.4-followup): per-stage flag for the
	// surface-arrival predicate. v0.11.4's first cut put this on
	// the Loadout level, which broke the Apollo-Stack flow — the
	// Lander stage couldn't carry its own soft-land capability
	// across a decouple into a freshly-spawned slate craft. Moving
	// the flag to the Stage level matches how other "per-stage
	// engineering" attributes work (BallisticCoefficient,
	// LaunchSpriteRowsPx) and SyncFields can re-derive
	// Spacecraft.CanSoftLand from Stages[0] on every staging /
	// dock / save-load. Set true on stages designed to land —
	// today: `lander` and `f9-s1` in StageCatalog.
	CanSoftLand bool `json:",omitempty"`

	// HasParachute (v0.12 Slice 3, ADR 0008): per-stage parachute
	// capability, the exact mirror of CanSoftLand. Set in StageCatalog
	// (today: the `csm` stage and the standalone re-entry capsule);
	// SyncFields re-derives Spacecraft.HasParachute from Stages[0] on
	// every staging / dock / load so the capability rides the hardware
	// across a decouple. `omitempty`.
	HasParachute bool `json:",omitempty"`
}

// SumDryMass returns the total dry mass across every stage in kg.
func SumDryMass(stages []Stage) float64 {
	var s float64
	for _, st := range stages {
		s += st.DryMass
	}
	return s
}

// SumFuelMass returns the total main-engine propellant across
// every stage in kg.
func SumFuelMass(stages []Stage) float64 {
	var s float64
	for _, st := range stages {
		s += st.FuelMass
	}
	return s
}

// SumFuelCapacity returns the total main-engine fuel-tank capacity
// across every stage in kg.
func SumFuelCapacity(stages []Stage) float64 {
	var s float64
	for _, st := range stages {
		s += st.FuelCapacity
	}
	return s
}

// SumMonopropMass returns the total RCS propellant across every
// stage in kg.
func SumMonopropMass(stages []Stage) float64 {
	var s float64
	for _, st := range stages {
		s += st.MonopropMass
	}
	return s
}

// SumMonopropCap returns the total RCS-tank capacity across every
// stage in kg.
func SumMonopropCap(stages []Stage) float64 {
	var s float64
	for _, st := range stages {
		s += st.MonopropCap
	}
	return s
}

// SyncFields refreshes the historical flat fields on s from the
// current Stages slice. Mass + propellant fields sum across all
// stages (the player's HUD wants total propellant); engine
// fields (Thrust, Isp, RCSThrust, RCSIsp) read from Stages[0]
// (the bottom = currently-firing). No-op when Stages is empty —
// callers that build Spacecraft without Stages (legacy test
// fixtures with literal Spacecraft{}) keep their flat-field
// values intact.
//
// Call SyncFields after any mutation that changes a stage entry
// (burn, RCS pulse, decouple, dock). Reads are direct field
// access; the sync runs only on writes so the per-tick read
// path stays free of indirection.
func (s *Spacecraft) SyncFields() {
	if len(s.Stages) == 0 {
		return
	}
	s.DryMass = SumDryMass(s.Stages)
	s.Fuel = SumFuelMass(s.Stages)
	s.Monoprop = SumMonopropMass(s.Stages)
	s.MonopropCapacity = SumMonopropCap(s.Stages)
	bottom := s.Stages[0]
	s.Thrust = bottom.Thrust
	s.Isp = bottom.Isp
	s.RCSThrust = bottom.RCSThrust
	s.RCSIsp = bottom.RCSIsp
	// v0.11.4-followup: bottom-stage authoritative for soft-land
	// capability. Matches the F9 model (S1 has landing gear, S2
	// doesn't) and the Apollo Stack model (Lander stage decouples
	// into its own slate craft carrying CanSoftLand=true).
	s.CanSoftLand = bottom.CanSoftLand
	// v0.12 Slice 3 (ADR 0008): parachute capability mirrors the
	// bottom stage too — the chute becomes "active" only once the
	// chute-bearing stage is the surviving core (Stages[0]).
	s.HasParachute = bottom.HasParachute
}

// ActiveStageFuel returns the bottom (currently-firing) stage's
// main-engine propellant in kg. Used by the engine-cutoff path to
// decide whether the active engine still has fuel.
//
// v0.9.4+: replaces direct checks against s.Fuel (which is the
// SUMMED propellant across all stages). For a 3-stage Saturn V
// with a dry S-IC and full S-II + S-IVB, s.Fuel reads ~549,000
// kg even though the firing engine has nothing to burn — the
// engine kept thrusting "for free" until the player staged.
//
// Falls back to s.Fuel when Stages is empty (legacy / test
// fixtures constructed without Stages); single-stage craft are
// unaffected since the sum equals the bottom stage's fuel.
func (s *Spacecraft) ActiveStageFuel() float64 {
	if len(s.Stages) == 0 {
		return s.Fuel
	}
	return s.Stages[0].FuelMass
}

// BurnFuel decrements the bottom-stage main-engine fuel by amount
// (kg), clamped to [0, Stages[0].FuelMass]. Refreshes the flat
// shadow fields. v0.9.1+ replacement for the pre-staging pattern
// `c.Fuel -= amount`. Returns the amount actually burned (clamped).
func (s *Spacecraft) BurnFuel(amount float64) float64 {
	if len(s.Stages) == 0 || amount <= 0 {
		return 0
	}
	if amount > s.Stages[0].FuelMass {
		amount = s.Stages[0].FuelMass
	}
	s.Stages[0].FuelMass -= amount
	s.SyncFields()
	return amount
}

// BurnMonoprop decrements the bottom-stage RCS propellant by amount
// (kg), clamped to [0, Stages[0].MonopropMass]. Refreshes the flat
// shadow fields. v0.9.1+ replacement for `s.Monoprop -= amount`.
// Returns the amount actually burned (clamped).
func (s *Spacecraft) BurnMonoprop(amount float64) float64 {
	if len(s.Stages) == 0 || amount <= 0 {
		return 0
	}
	if amount > s.Stages[0].MonopropMass {
		amount = s.Stages[0].MonopropMass
	}
	s.Stages[0].MonopropMass -= amount
	s.SyncFields()
	return amount
}
