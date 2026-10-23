package spacecraft

import "math"

// FuelType constants (v0.11.5). The renderer maps each value to a
// flame colour in internal/tui/screens/launch_sprite.go; the in-game
// vocabulary lives in CONTEXT.md under "Maneuver & thrust".
const (
	FuelTypeKerolox    = "kerolox"    // RP-1 + LOX (F-1, Merlin) — orange
	FuelTypeHydrolox   = "hydrolox"   // LH2 + LOX (J-2, RS-25, RL-10) — pale cyan
	FuelTypeHypergolic = "hypergolic" // Aerozine 50 + N2O4 (LM, SPS) — yellow-amber
	FuelTypeSolid      = "solid"      // APCP (SLS SRB) — orange-red
)

// Command-source constants (v0.23 / ADR 0027). A stage's CommandSource
// declares whether it is a control point and, if so, whether crewed.
// Empty ("") means none (the stage is not a command source). A vessel is
// Controllable iff ≥1 stage is a command source; Crewed iff any is crewed
// (else it is an unmanned probe, comms-gated in the connectivity slices).
const (
	CommandNone   = ""       // not a command source (tank / engine / spent booster)
	CommandCrewed = "crewed" // a crewed pod — flown by its crew, never comms-gated
	CommandProbe  = "probe"  // an uncrewed probe core — needs a connection to be commanded
)

// IsCommandSource reports whether a CommandSource value makes a stage a
// control point (crewed or probe — not none/empty/unknown).
func IsCommandSource(src string) bool {
	return src == CommandCrewed || src == CommandProbe
}

// Antenna-kind constants (v0.23 / ADR 0027). A stage's AntennaKind
// declares its comms hardware: a direct antenna can use the network
// (reach a ground station, possibly via relays) but cannot forward
// traffic; a relay antenna can also forward for other vessels. Empty
// ("") means no antenna.
const (
	AntennaNone   = ""       // no antenna
	AntennaDirect = "direct" // can use the network, cannot forward for others
	AntennaRelay  = "relay"  // can use AND forward (a comm-sat / ground-station relay)
)

// Ports lists the docking ports this stage carries (ADR 0053 §3, ADR 0056
// decision 7), today none or one nose port. It returns the stage's own
// stored field, set where the stage is built (Part.ToStage, BuildStage,
// ComposeStage) and persisted in the save (schema 15). There is no
// fallback to a catalog lookup by name: a stage with no ports has none.
func (s Stage) Ports() []PartPort { return s.PartPorts }

// HasPort reports whether the stage carries at least one docking port.
func (s Stage) HasPort() bool { return len(s.Ports()) > 0 }

// Stage describes one decouplable propulsion module on a spacecraft.
// v0.9.1+. The Stages slice on Spacecraft is the source of truth for
// dry mass / fuel / engine numbers; the historical flat fields
// (DryMass, Fuel, Thrust, Isp, Monoprop, MonopropCapacity,
// RCSThrust, RCSIsp) are derived shadow-mirror values refreshed by
// SyncFields after any stage mutation. Read sites keep using the
// flat fields; write sites must mutate the relevant stage entry and
// call SyncFields to keep the mirror coherent.
//
// Convention: Stages[0] is the BOTTOM stage — the next to be
// jettisoned by World.StageActive, and the stage the chute, legs and
// drag are read from. The engine, tank and thrusters belong to the lit
// stage (LitStageIdx), which is Stages[0] unless an inert stage sits
// under a powered one.
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
	// resolves via LoadoutID lookup (default: VesselGlyph /
	// "#FFD93D" like the S-IVB-1 main loadout). Every catalog craft
	// uses VesselGlyph — vessels are told apart by Colour, not shape
	// (ADR 0020 reserves the geometric glyphs for orbital markers).
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

	// LaunchSpriteDerived marks a stage whose sprite fields were filled from
	// its mass and chemistry (a parts-built stage) rather than authored in
	// the catalog. The launch drawing shades a same-colour neighbour only
	// when one of the two stages is derived, so catalog loadouts paint
	// exactly as authored.
	LaunchSpriteDerived bool `json:",omitempty"`

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

	// LitWithAbove (ADR 0058 decision 1) marks this stage as a Booster:
	// it lights together with the stage above it (see LitStages). Copied
	// from the loadout's part reference at resolve; absent means serial.
	LitWithAbove bool `json:",omitempty"`

	// HasParachute (v0.12 Slice 3, ADR 0008): per-stage parachute
	// capability, the exact mirror of CanSoftLand. Set in StageCatalog
	// (today: the `csm` stage and the standalone re-entry capsule);
	// SyncFields re-derives Spacecraft.HasParachute from Stages[0] on
	// every staging / dock / load so the capability rides the hardware
	// across a decouple. `omitempty`.
	HasParachute bool `json:",omitempty"`

	// CommandSource (v0.23 / ADR 0027) declares whether this stage is a
	// control point: CommandCrewed / CommandProbe / CommandNone (empty).
	// Authored on the Part (ADR 0026 catalog) and carried here so it
	// rides the hardware across staging/dock/save; SyncFields derives the
	// vessel-level Crewed / Controllable mirrors from the whole stack.
	CommandSource string `json:",omitempty"`

	// AntennaKind / AntennaRangeM (v0.23 / ADR 0027) declare this stage's
	// comms hardware: kind (AntennaNone / AntennaDirect / AntennaRelay) plus a
	// rated range in metres — the distance at which it reaches an identical
	// antenna (the CommNet combinability model; see sim.commLinkRangeM).
	// Authored on the Part; SyncFields derives the vessel-level antenna (the
	// longest-ranged one in the stack) for the connectivity graph.
	AntennaKind   string  `json:",omitempty"`
	AntennaRangeM float64 `json:",omitempty"`

	// PartPorts (ADR 0056 decision 7) is the stage's own docking-port
	// list, read through Ports(). Named PartPorts because a Go type cannot
	// hold a field and a method both called Ports. Copied with the stage
	// on dock, undock, deploy and jettison, and persisted in the save
	// (schema 15).
	PartPorts []PartPort `json:",omitempty"`

	// CargoKg / HoldKg / CargoHold (ADR 0057 decision 5): inert cargo mass.
	// CargoKg is what this stage carries to deliver; HoldKg is what its
	// Cargo Hold has received (a hand-over moves CargoKg into the lowest
	// stage with CargoHold, sim.HandOverCargo); CargoHold is the flag that
	// lets the stage receive. Cargo is its own mass term (Spacecraft.Cargo),
	// not DryMass. Copied with the stage on dock, undock, deploy and
	// jettison, and persisted additively inside schema 15 (omitempty).
	CargoKg   float64 `json:",omitempty"`
	HoldKg    float64 `json:",omitempty"`
	CargoHold bool    `json:",omitempty"`
}

// SumDryMass returns the total dry mass across every stage in kg.
func SumDryMass(stages []Stage) float64 {
	var s float64
	for _, st := range stages {
		s += st.DryMass
	}
	return s
}

// SumCargoMass returns the total cargo mass across every stage in kg: what the
// stages carry to deliver plus what their holds have received (ADR 0057 5.2).
func SumCargoMass(stages []Stage) float64 {
	var s float64
	for _, st := range stages {
		s += st.CargoKg + st.HoldKg
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

// LitStageIdx returns the index of the lit stage: the lowest stage with an
// engine (Thrust > 0), or 0 when no stage has one. The lit stage is what the
// pilot flies with (engine, tank, thrusters); the bottom stage (Stages[0])
// stays what staging drops and what the chute, legs and drag read from. The
// two differ only when an inert stage sits under a powered one (ADR 0057).
func LitStageIdx(stages []Stage) int {
	for i := range stages {
		if stages[i].Thrust > 0 {
			return i
		}
	}
	return 0
}

// LitStages returns the lit set (ADR 0058): the lit stage plus each stage
// above it while the stage below is marked LitWithAbove. Bottom-first.
func LitStages(stages []Stage) []int {
	if len(stages) == 0 {
		return nil
	}
	i := LitStageIdx(stages)
	set := []int{i}
	for ; stages[i].LitWithAbove && i+1 < len(stages); i++ {
		set = append(set, i+1)
	}
	return set
}

// LitEngine returns the lit set's summed thrust and thrust-weighted blend
// Isp (Isp_eff = ΣT / Σ(T/Isp), as aggregateComponents). A member with an
// empty tank is dead weight and drops out; when every tank is dry the lit
// stage's own engine is kept, as for any dry vessel.
func LitEngine(stages []Stage) (thrust, isp float64) {
	set := LitStages(stages)
	if len(set) == 0 {
		return 0, 0
	}
	if len(set) == 1 { // unmarked: exact, no T/(T/Isp) rounding
		return stages[set[0]].Thrust, stages[set[0]].Isp
	}
	var flow float64 // Σ T_i/Isp_i
	for _, i := range set {
		if st := stages[i]; st.FuelMass > 0 && st.Thrust > 0 && st.Isp > 0 {
			thrust += st.Thrust
			flow += st.Thrust / st.Isp
		}
	}
	if flow == 0 {
		lit := stages[LitStageIdx(stages)]
		return lit.Thrust, lit.Isp
	}
	return thrust, thrust / flow
}

// litPhaseBurn is the propellant each member of set burns, at full throttle
// and its own mass flow T_i/(Isp_i·g0), until the first tank empties: the
// first phase of a lit set (ADR 0058 decision 7). A member with no fuel or
// no engine burns nothing. The first tank to empty burns exactly its
// FuelMass, so a one-stage set reproduces "burn the whole tank".
func litPhaseBurn(stages []Stage, set []int) []float64 {
	if len(set) == 1 { // unmarked: the whole tank, whatever the engine
		return []float64{stages[set[0]].FuelMass}
	}
	burn := make([]float64, len(set))
	tMin := math.Inf(1)
	for _, i := range set {
		if st := stages[i]; st.FuelMass > 0 && st.Thrust > 0 && st.Isp > 0 {
			tMin = math.Min(tMin, st.FuelMass*st.Isp/st.Thrust)
		}
	}
	for k, i := range set {
		if st := stages[i]; st.FuelMass > 0 && st.Thrust > 0 && st.Isp > 0 {
			burn[k] = st.FuelMass * (tMin / (st.FuelMass * st.Isp / st.Thrust))
		}
	}
	return burn
}

// LitSetStage is the lit stage with its Thrust and Isp replaced by the lit
// set's (LitEngine): the one-Stage view of the engine a vessel flies with,
// for readouts that print a stage's engine. Zero Stage for an empty slice.
func LitSetStage(stages []Stage) Stage {
	st := LitStageOf(stages)
	st.Thrust, st.Isp = LitEngine(stages)
	return st
}

// LitStageOf returns a copy of the lit stage of stages, or the zero Stage
// for an empty slice.
func LitStageOf(stages []Stage) Stage {
	if len(stages) == 0 {
		return Stage{}
	}
	return stages[LitStageIdx(stages)]
}

// LitStage returns the lit stage's index on s (see LitStageIdx).
func (s *Spacecraft) LitStage() int { return LitStageIdx(s.Stages) }

// SyncFields refreshes the historical flat fields on s from the
// current Stages slice. Mass + propellant fields sum across all
// stages (the player's HUD wants total propellant); engine
// fields (Thrust, Isp, RCSThrust, RCSIsp) read from the lit stage
// (LitStageIdx: the lowest stage with an engine; Stages[0] on every
// vessel whose bottom stage is powered). No-op when Stages is empty —
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
	s.Cargo = SumCargoMass(s.Stages)
	s.MonopropCapacity = SumMonopropCap(s.Stages)
	lit := s.Stages[LitStageIdx(s.Stages)]
	s.Thrust, s.Isp = LitEngine(s.Stages)
	s.RCSThrust = lit.RCSThrust
	s.RCSIsp = lit.RCSIsp
	bottom := s.Stages[0]
	// v0.11.4-followup: bottom-stage authoritative for soft-land
	// capability. Matches the F9 model (S1 has landing gear, S2
	// doesn't) and the Apollo Stack model (Lander stage decouples
	// into its own slate craft carrying CanSoftLand=true).
	s.CanSoftLand = bottom.CanSoftLand
	// v0.12 Slice 3 (ADR 0008): parachute capability mirrors the
	// bottom stage too — the chute becomes "active" only once the
	// chute-bearing stage is the surviving core (Stages[0]).
	s.HasParachute = bottom.HasParachute
	// v0.23 / ADR 0027: comms mirrors derived across the WHOLE stack
	// (not just the bottom) — a probe core or relay antenna anywhere on
	// the vessel makes the vessel controllable / relay-capable. Crewed
	// wins over probe (a crewed pod is never comms-gated); the antenna is
	// the longest-ranged one carried.
	s.Crewed = false
	s.Controllable = false
	s.AntennaKind = AntennaNone
	s.AntennaRangeM = 0
	for _, st := range s.Stages {
		if IsCommandSource(st.CommandSource) {
			s.Controllable = true
			if st.CommandSource == CommandCrewed {
				s.Crewed = true
			}
		}
		if st.AntennaKind != AntennaNone && st.AntennaRangeM > s.AntennaRangeM {
			s.AntennaKind = st.AntennaKind
			s.AntennaRangeM = st.AntennaRangeM
		}
	}
}

// ActiveStageFuel returns the lit set's main-engine propellant in kg (the
// lit stage's tank on an unmarked vessel, ADR 0058: every lit tank on a
// marked one). Used by the engine-cutoff path to decide whether the active
// engine still has fuel.
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
	var f float64
	for _, i := range LitStages(s.Stages) {
		f += s.Stages[i].FuelMass
	}
	return f
}

// BurnFuel decrements the lit set's main-engine fuel by amount (kg), split
// across the lit tanks by each engine's mass flow T_i/Isp_i (so each tank
// drains at its own rate, ADR 0058), each clamped to its own tank. Refreshes
// the flat shadow fields. v0.9.1+ replacement for the pre-staging pattern
// `c.Fuel -= amount`. Returns the amount actually burned (clamped).
// shortcut: a tank that empties mid-call leaves its unburned share unburned
// for that call, not moved to the other tank; S2 steps are sub-second.
func (s *Spacecraft) BurnFuel(amount float64) float64 {
	if len(s.Stages) == 0 || amount <= 0 {
		return 0
	}
	set := LitStages(s.Stages)
	if len(set) == 1 {
		lit := set[0]
		if amount > s.Stages[lit].FuelMass {
			amount = s.Stages[lit].FuelMass
		}
		s.Stages[lit].FuelMass -= amount
		s.SyncFields()
		return amount
	}
	var wsum float64
	for _, i := range set {
		if st := s.Stages[i]; st.FuelMass > 0 && st.Isp > 0 {
			wsum += st.Thrust / st.Isp
		}
	}
	if wsum == 0 {
		return 0
	}
	var burned float64
	for _, i := range set {
		st := &s.Stages[i]
		if st.FuelMass <= 0 || st.Isp <= 0 {
			continue
		}
		d := math.Min(amount*(st.Thrust/st.Isp)/wsum, st.FuelMass)
		st.FuelMass -= d
		burned += d
	}
	s.SyncFields()
	return burned
}

// BurnMonoprop decrements the lit stage's RCS propellant by amount
// (kg), clamped to [0, lit.MonopropMass]. Refreshes the flat
// shadow fields. v0.9.1+ replacement for `s.Monoprop -= amount`.
// Returns the amount actually burned (clamped).
func (s *Spacecraft) BurnMonoprop(amount float64) float64 {
	if len(s.Stages) == 0 || amount <= 0 {
		return 0
	}
	lit := s.LitStage()
	if amount > s.Stages[lit].MonopropMass {
		amount = s.Stages[lit].MonopropMass
	}
	s.Stages[lit].MonopropMass -= amount
	s.SyncFields()
	return amount
}

// drivenMembers returns the lit set's firing members (tank and engine live)
// with each one's throttle for a core throttle of core: the top member is the
// core, a solid is pinned at 1, a liquid booster takes BoosterThrottle or
// follows the core (ADR 0058 decision 2). Nil for a single-stage set: that
// path keeps today's one-throttle arithmetic.
func (s *Spacecraft) drivenMembers(core float64) (idx []int, theta []float64) {
	set := LitStages(s.Stages)
	if len(set) < 2 {
		return nil, nil
	}
	core = math.Max(0, math.Min(1, core))
	for k, i := range set {
		st := s.Stages[i]
		if st.FuelMass <= 0 || st.Thrust <= 0 || st.Isp <= 0 {
			continue
		}
		th := core
		if k < len(set)-1 {
			th = s.boosterThrottleOr(core)
			if st.FuelType == FuelTypeSolid {
				th = 1
			}
		}
		idx, theta = append(idx, i), append(theta, th)
	}
	return idx, theta
}

func (s *Spacecraft) boosterThrottleOr(core float64) float64 {
	if s.BoosterThrottle == nil {
		return core
	}
	return math.Max(0, math.Min(1, *s.BoosterThrottle))
}

// LitThrottle is the fraction of s.Thrust the lit set delivers for a core
// throttle of core: ΣT_i·θ_i / ΣT_i. A single-stage set returns core as is.
func (s *Spacecraft) LitThrottle(core float64) float64 {
	idx, theta := s.drivenMembers(core)
	if idx == nil {
		return core
	}
	var sum, full float64
	for k, i := range idx {
		sum += s.Stages[i].Thrust * theta[k]
		full += s.Stages[i].Thrust
	}
	if full == 0 {
		return 0
	}
	return sum / full
}

// BurnFuelAt debits each lit tank for dt seconds at its own group's throttle
// (flow T_i·θ_i/(Isp_i·g0)), each clamped to its tank, and returns the mass
// burned. A single-stage set is BurnFuel(MassFlowRateAt(core)·dt).
func (s *Spacecraft) BurnFuelAt(core, dt float64) float64 {
	idx, theta := s.drivenMembers(core)
	if idx == nil {
		return s.BurnFuel(s.MassFlowRateAt(core) * dt)
	}
	var burned float64
	for k, i := range idx {
		st := &s.Stages[i]
		d := math.Min(st.Thrust*theta[k]/(st.Isp*g0)*dt, st.FuelMass)
		st.FuelMass -= d
		burned += d
	}
	s.SyncFields()
	return burned
}

// SolidLit reports whether a pinned solid is burning in a multi-stage lit set.
func (s *Spacecraft) SolidLit() bool {
	set := LitStages(s.Stages)
	for k := 0; k < len(set)-1; k++ {
		if st := s.Stages[set[k]]; st.FuelType == FuelTypeSolid && st.FuelMass > 0 {
			return true
		}
	}
	return false
}

// HasLiquidBooster reports whether the lit set has a marked liquid booster
// with its own throttle group.
func (s *Spacecraft) HasLiquidBooster() bool {
	set := LitStages(s.Stages)
	for k := 0; k < len(set)-1; k++ {
		if s.Stages[set[k]].FuelType != FuelTypeSolid {
			return true
		}
	}
	return false
}

// BoosterThrottleEff is the liquid booster group's throttle: BoosterThrottle,
// or the core's when it follows.
func (s *Spacecraft) BoosterThrottleEff() float64 {
	return s.boosterThrottleOr(s.EffectiveThrottle())
}

// LiquidCut reports that every liquid group is at 0: the core, and the
// liquid booster when the set has one. On a single-throttle vessel it is
// "throttle is 0".
func (s *Spacecraft) LiquidCut() bool {
	return s.EffectiveThrottle() == 0 && (!s.HasLiquidBooster() || s.BoosterThrottleEff() == 0)
}
