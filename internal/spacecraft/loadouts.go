package spacecraft

import "github.com/jasonfen/terminal-space-program/internal/bodies"

// Loadout describes a named craft archetype — propulsion numbers,
// dry/wet mass sizing, default RCS pool, and visual differentiation
// (glyph + color). v0.8.2 ship set + v0.9.1 Saturn-V multi-stage +
// v0.9.4 SLS / Falcon 9:
//
//   - S-IVB-1:    J-2-powered third stage. The v0.5.13+ default.
//   - ICPS:       RL-10-powered low-TWR transfer stage. Returns from
//     v0.5.6 — long burns, less mass.
//   - RCS-tug:    Pure-monoprop proximity-ops vehicle. No main engine;
//     navigates entirely on RCS. For docking maneuvers.
//   - Lander:     Throttleable descent-stage profile (LM-derived). Lower
//     thrust, lower Isp, sized for surface maneuvering.
//   - Saturn-V:   3-stage Apollo launch vehicle (S-IC / S-II / S-IVB).
//     v0.9.1+. TWR > 1 at sea level on stage 1.
//   - SLS-Block1: 3-stage NASA heavy-lift (SRBs / Core / ICPS). v0.9.4+.
//     SRBs and core fire in parallel in real life; we
//     approximate as sequential.
//   - Falcon-9:   2-stage SpaceX LV (Merlin 1D × 9 / Merlin Vacuum).
//     v0.9.4+. Smaller stack, higher lift-off TWR.
//   - Apollo-Stack: Saturn-V launch chain + LM + CSM payload, 5
//     stages. v0.10.1+. Mid-stage Lander decouples to a
//     controllable craft (payload separation); CSM is
//     the surviving core.
//
// Future loadouts land alongside this catalog and are referenced
// from Spacecraft.LoadoutID — a string lookup keeps the on-disk
// format human-editable and lets future modding overlays add craft
// types without code changes.
//
// v0.9.1+: Stages is the source of truth. Single-stage loadouts
// declare one entry; multi-stage (Saturn-V) declares the chain
// bottom-first (Stages[0] = S-IC booster, fires first; Stages[2] =
// S-IVB, fires last). The legacy flat fields (DryMass / Fuel / Isp
// / Thrust) are derived from Stages[0] for back-compat with pre-
// v0.9.1 readers — for single-stage loadouts they match the stage
// exactly; for multi-stage they reflect the bottom (firing) stage.
type Loadout struct {
	ID    string
	Name  string
	Role  string
	Glyph string
	Color string
	// Stages is the per-stage breakdown, bottom-first. Required
	// (must be non-empty); a one-element Stages declares a
	// single-stage craft.
	Stages []Stage
	// DecouplePlan (v0.12 Slice 2 / ADR 0007) is an optional
	// bottom-up list of staging group sizes — how many contiguous
	// bottom Stages each staging press releases as a single craft.
	// Nil ⇒ all-ones (one Stage per press, the historical default).
	// The Apollo Stack declares [1,1,1,2] so the descent + ascent LM
	// pair extracts together as a 2-stage craft (see ADR 0007). Sum
	// of the plan should be < len(Stages) (the top stage is the
	// surviving core). Copied onto the Spacecraft in NewFromLoadout.
	DecouplePlan []int
	// SlewRateDegPerSec (v0.10.0+) is the per-loadout attitude
	// angular-rate cap (deg/s, sim-time). Zero => the global
	// DefaultSlewRateDegPerSec. Loadout-level (not per-stage):
	// staging does not change the slew rate this cycle (attitude
	// dynamics are deferred). All catalog literals leave this unset
	// in v0.10.0; per-vehicle tuning is a follow-up dial.
	SlewRateDegPerSec float64

	// ScaleClass is the loadout's spawn-form scale hint (ADR 0014),
	// shared with bodies.System. Optional: an unset value normalizes
	// to bodies.ScaleReal via Scale(), so the existing real fleet needs
	// no per-literal change. The scale-matched Kern Stack sets
	// bodies.ScaleStrippedBack. Never used to filter craft by System —
	// any Loadout can be spawned in any System.
	ScaleClass bodies.ScaleClass
}

// DefaultSlewRateDegPerSec is the attitude slew-rate cap applied to
// any loadout that does not override it (Loadout.SlewRateDegPerSec
// == 0) and to legacy/test craft built without a loadout. 15°/s ≈ 12 s
// for a 180° flip — visible and deliberate, but snappy enough not to
// be tedious (raised from 5°/s after v0.10.0 playtest). Cosine loss
// is still a real consequence the player times burns around. v0.10.0+.
const DefaultSlewRateDegPerSec = 15.0

// Scale returns the loadout's normalized ScaleClass (empty =>
// bodies.ScaleReal). The spawn form compares this against the target
// System's Scale() to surface the Δv-to-orbit / "best for" hint; it is
// never used to filter the craft list (ADR 0014).
func (l Loadout) Scale() bodies.ScaleClass {
	return l.ScaleClass.Normalize()
}

// DryMass returns the bottom stage's dry mass (single-stage
// equivalent for pre-v0.9.1 readers; sum-across-stages is via
// SumDryMass(l.Stages) when the caller wants it).
func (l Loadout) DryMass() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].DryMass
}

// Fuel returns the bottom stage's fuel mass — same convention.
func (l Loadout) Fuel() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].FuelMass
}

// Isp returns the bottom stage's main-engine specific impulse.
func (l Loadout) Isp() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].Isp
}

// Thrust returns the bottom stage's main-engine thrust.
func (l Loadout) Thrust() float64 {
	if len(l.Stages) == 0 {
		return 0
	}
	return l.Stages[0].Thrust
}

// LoadoutS_IVB1 is the v0.5.13+ Apollo S-IVB / J-2 default.
const LoadoutSIVB1ID = "S-IVB-1"

// LoadoutICPS is the v0.5.6 RL-10 low-TWR transfer stage.
const LoadoutICPSID = "ICPS"

// LoadoutRCSTug is the pure-monoprop proximity-ops vehicle.
const LoadoutRCSTugID = "RCS-tug"

// LoadoutLander is the throttleable descent-stage profile.
const LoadoutLanderID = "Lander"

// LoadoutSaturnV is the 3-stage Apollo launch vehicle. v0.9.1+.
const LoadoutSaturnVID = "Saturn-V"

// LoadoutSLSBlock1 is the 3-stage NASA Space Launch System Block 1.
// v0.9.4+. Twin 5-segment solid boosters (SRBs) → 4× RS-25 core stage
// → ICPS. Same low-TWR upper-stage shape as Saturn V (Core TWR ~0.87
// after SRB sep), so gameplay translates: continuous-burn ascent,
// no coast-and-circularize.
const LoadoutSLSBlock1ID = "SLS-Block1"

// LoadoutFalcon9 is the 2-stage SpaceX Falcon 9 Block 5. v0.9.4+.
// 9× Merlin 1D first stage → 1× Merlin Vacuum second stage. Higher
// TWR than the heavy-lift options (~1.4 at lift-off) and a smaller
// stack — handles like a sport rocket compared to the Saturn V /
// SLS heavies.
const LoadoutFalcon9ID = "Falcon-9"

// LoadoutApolloStack is the v0.10.1+ full Apollo mission stack:
// Saturn-V launch chain with a Lunar Module + Command/Service
// Module payload on top. Stages bottom-first =
// [S-IC, S-II, S-IVB, Lander, CSM]. The decouple sequence is the
// real mission arc on top of the v0.9.1 staging machinery:
// drop S-IC → S-II → S-IVB (after the TLI burn) → Lander; the
// Lander spawns as its own controllable slate craft (payload
// separation) and the CSM is left as the player's surviving core
// to fly the rendezvous / return. The first three stages reuse the
// canonical Saturn-V tuning so ascent flies identically.
const LoadoutApolloStackID = "Apollo-Stack"

// LoadoutCapsule is the v0.12 Slice 3 (ADR 0008) standalone re-entry
// capsule: a single command-module-class stage with a recovery
// parachute and no engine landing capability. The clean, directly-
// spawnable test vehicle for the chute subsystem — spawn it, de-orbit,
// `space` to arm, watch it auto-deploy and splash down under V_CRIT
// without crashing. The CSM is only reachable via the full Apollo
// Stack → orbit → four decouples, too slow to iterate on.
const LoadoutCapsuleID = "Capsule"

// LoadoutKernStack is the v0.15 / ADR 0014 scale-matched vehicle for
// the stripped-back Lumen system: a simplified 4-stage Apollo analog —
// Boost → Transfer → single Lander → parachute Pod, bottom-first —
// sized to a ~6 km/s Cursor-landing-and-return budget on Lumen's
// ~1/10-linear scale (~3.4 km/s to Kern orbit vs Sol's ~9.4). It is the
// only Loadout tagged bodies.ScaleStrippedBack, so the spawn form's
// Δv-to-orbit hint flags it as best-for-Lumen — but, per ADR 0014, the
// tag never filters: the Kern Stack can still be spawned in Sol (where
// it can't reach orbit) and the real fleet in Lumen. DecouplePlan
// [1,1,1] drops Boost, Transfer and Lander one at a time; the engineless
// parachute Pod is the surviving core that splashes down (ADR 0008
// recovery model). Engine Isp values are KSP stock numbers collapsed to
// the model's single-Isp-per-stage convention (no altitude-varying Isp):
// Mainsail (Boost, sea-level 285), Poodle (Transfer, vac 350), Terrier
// (Lander, vac 345).
const LoadoutKernStackID = "Kern-Stack"

// stageRCS builds the per-stage RCS pool for a stage of the given
// dry mass via DefaultRCSLoadout — same scaling that single-stage
// craft used pre-v0.9.1, so existing loadouts inherit identical
// RCS budgets when wrapped into Stages: [{...}].
func stageRCS(dryMass float64) (mp, monoCap, rcsThrust, rcsIsp float64) {
	return DefaultRCSLoadout(dryMass)
}

// buildLoadouts loads the embedded loadout catalog (data/loadouts.json +
// data/parts.json) and resolves each LoadoutDef into a runtime Loadout,
// returning the by-ID map and the embedded display order. Runs once at
// package-var init (v0.23 / ADR 0026, C1-3 — loadouts are now data, not a
// Go literal). Panics if the embedded data fails to load: a malformed
// shipped catalog is a build/programmer error, like regexp.MustCompile.
// The user-overlay merge (skip-bad-with-warning) is wired in at a higher
// layer (C1-4); this init path reads embedded data only, so it stays
// deterministic.
func buildLoadouts() (map[string]Loadout, []string) {
	parts, defs, err := loadEmbeddedCatalog()
	if err != nil {
		panic("spacecraft: embedded loadout catalog failed to load: " + err.Error())
	}
	out := make(map[string]Loadout, len(defs))
	order := make([]string, 0, len(defs))
	for _, d := range defs {
		out[d.ID] = resolveLoadout(d, parts)
		order = append(order, d.ID)
	}
	return out, order
}

// resolveLoadout assembles a Loadout's Stages from its part references,
// reproducing the pre-migration build exactly: each referenced Part
// becomes a Stage (Part.ToStage), stamped with the loadout's ID, with the
// RCS pool DERIVED from dry mass (stageRCS — the catalog convention, not
// stored per part) and any per-instance override applied. Panics on an
// unknown part ID (a malformed embedded catalog — see buildLoadouts).
func resolveLoadout(d LoadoutDef, parts map[string]Part) Loadout {
	stages := make([]Stage, 0, len(d.Parts))
	for _, ref := range d.Parts {
		p, ok := parts[ref.PartID]
		if !ok {
			panic("spacecraft: loadout " + d.ID + " references unknown part " + ref.PartID)
		}
		st := p.ToStage()
		st.LoadoutID = d.ID // the loadout that produced this stage (jettison/HUD identity)
		if ref.Override != nil {
			applyPartOverride(&st, *ref.Override)
		}
		// RCS pool scales with dry mass (DefaultRCSLoadout) at build time,
		// exactly as the pre-migration stage()/stageWithBC helpers did.
		mp, monoCap, rcsThrust, rcsIsp := stageRCS(st.DryMass)
		st.MonopropMass, st.MonopropCap, st.RCSThrust, st.RCSIsp = mp, monoCap, rcsThrust, rcsIsp
		stages = append(stages, st)
	}
	var plan []int
	if len(d.DecouplePlan) > 0 {
		plan = append([]int(nil), d.DecouplePlan...)
	}
	return Loadout{
		ID:                d.ID,
		Name:              d.Name,
		Role:              d.Role,
		Glyph:             d.Glyph,
		Color:             d.Color,
		Stages:            stages,
		DecouplePlan:      plan,
		SlewRateDegPerSec: d.SlewRateDegPerSec,
		ScaleClass:        bodies.ScaleClass(d.ScaleClass),
	}
}

// applyPartOverride applies a loadout's minimal per-instance knobs to a
// stage built from a referenced part (ADR 0026 §1 — fuel fill / name /
// color only). FuelFillFraction scales the part's full capacity. No
// embedded loadout uses an override today (divergent stages are distinct
// parts); it's the sanctioned data hook for authored loadouts (C1-5 / C3).
func applyPartOverride(st *Stage, o PartOverride) {
	if o.FuelFillFraction != nil {
		st.FuelMass = st.FuelCapacity * *o.FuelFillFraction
	}
	if o.Name != "" {
		st.Name = o.Name
	}
	if o.Color != "" {
		st.Color = o.Color
	}
}

// VesselGlyph is the single marker every craft collapses to on the
// orbit map (ADR 0020). Vessels are distinguished by Colour, not shape:
// the orbital-marker vocabulary (▲ apo / ▼ peri / ◇ AN / ◆ DN / ⊕
// perilune / ✕ closest / Δ node) owns the geometric shapes, so a vessel
// must not reuse any of them or the player can't tell a ship from an
// apsis. ➤ (U+27A4, Dingbats — same well-supported block as the marker
// ✕) is a heading-chevron reserved for craft. Every Loadout/Stage glyph
// below references this constant; do not hand-pick a per-craft glyph.
const VesselGlyph = "➤"

// Loadouts indexes the launch catalog by ID, and LoadoutOrder is the
// canonical UI cycle order (the v0.8.2+ spawn form cycles through it).
// v0.23 / ADR 0026 (C1-3): both are now resolved from the embedded
// data/loadouts.json + data/parts.json at init via buildLoadouts, rather
// than hardcoded Go literals — the loadout catalog is data, the last
// hardcoded catalog in the project to move. The resolved Loadout shape
// (Stages bottom-first, RCS derived from dry mass, per-loadout plans) and
// every reader (NewFromLoadout / LookupLoadout / the spawn + staging +
// save paths) are unchanged: only the source of the data moved, so every
// loadout flies byte-identical (golden-tested). User overlays merge in at
// a higher layer (C1-4).
var Loadouts, LoadoutOrder = buildLoadouts()

// LookupLoadout returns the catalog entry for the given ID, or the
// S-IVB-1 default when the ID is empty / unknown. v0.8.2+: the
// fallback path keeps pre-v0.8.2 saves loadable — those entries
// have no LoadoutID and resolve to the default loadout.
func LookupLoadout(id string) Loadout {
	if l, ok := Loadouts[id]; ok {
		return l
	}
	return Loadouts[LoadoutSIVB1ID]
}

// NewFromLoadout constructs a Spacecraft from a loadout entry. The
// caller still has to set Primary + State (orbit), name, and any
// per-instance overrides. Convenience for spawn paths so they don't
// duplicate the field-setting boilerplate.
//
// v0.9.1+: Stages is the source of truth — copied from the catalog
// entry — and SyncFields populates the legacy flat fields
// (DryMass / Fuel / Thrust / Isp / Monoprop / RCSThrust / RCSIsp)
// from Stages so pre-v0.9.1 readers keep working without per-site
// changes.
func NewFromLoadout(loadoutID string) *Spacecraft {
	l := LookupLoadout(loadoutID)
	stages := make([]Stage, len(l.Stages))
	copy(stages, l.Stages)
	// Copy the decouple plan (own backing array) so StageActive's
	// positional reslice (DecouplePlan[1:]) never mutates the shared
	// catalog literal. Empty/nil plan stays nil ⇒ single-pop default.
	var plan []int
	if len(l.DecouplePlan) > 0 {
		plan = make([]int, len(l.DecouplePlan))
		copy(plan, l.DecouplePlan)
	}
	c := &Spacecraft{
		Name:                 l.Name,
		LoadoutID:            l.ID,
		Role:                 l.Role,
		Glyph:                l.Glyph,
		Color:                l.Color,
		Throttle:             1.0,
		BallisticCoefficient: DefaultBallisticCoefficient,
		Stages:               stages,
		DecouplePlan:         plan,
		SlewRateDegPerSec:    l.SlewRateDegPerSec,
	}
	c.SyncFields()
	return c
}

// NewFromStages constructs a Spacecraft from a player-assembled
// stage list (bottom-first, same convention as Loadout.Stages) —
// the v0.10.1+ stack-configurator path. Sibling of NewFromLoadout
// with no catalog entry behind it: LoadoutID is left empty (a
// custom craft is not a catalog archetype), and identity/visuals
// come from the top (core) stage so the slate HUD has a sensible
// name + marker for the vessel the player keeps flying.
//
// The caller still sets Primary + State. Returns nil when stages
// is empty — an empty stack is not a spawnable craft (callers
// reject before reaching the spawn path).
//
// Custom craft persist through save/load via the existing v6
// per-stage wire format (save schema v6, v0.9.1) — no migration:
// the flat shadow fields are re-derived by SyncFields on load and
// the empty LoadoutID resolves to the default only for those
// derived mirrors, never overriding the round-tripped Stages.
func NewFromStages(stages []Stage) *Spacecraft {
	if len(stages) == 0 {
		return nil
	}
	cp := make([]Stage, len(stages))
	copy(cp, stages)
	core := cp[len(cp)-1] // top stage = the surviving "core"
	name := core.Name
	if name == "" {
		name = "Custom"
	}
	glyph := core.Glyph
	if glyph == "" {
		glyph = VesselGlyph
	}
	color := core.Color
	if color == "" {
		color = "#FFD93D"
	}
	c := &Spacecraft{
		Name:                 name,
		LoadoutID:            "", // custom — no catalog archetype
		Role:                 "custom",
		Glyph:                glyph,
		Color:                color,
		Throttle:             1.0,
		BallisticCoefficient: DefaultBallisticCoefficient,
		Stages:               cp,
	}
	c.SyncFields()
	return c
}
