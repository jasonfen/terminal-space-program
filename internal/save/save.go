// Package save persists and restores the simulation World as JSON.
//
// The on-disk envelope ships a richer header from day one — version,
// generator, clock_t0, body_catalog_hash, payload — so future schema
// migrations and the v0.6 multiplayer `session` block can land without
// bumping every caller. See docs/state-of-game.md §3 v0.4.0 for the
// rationale.
package save

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/version"
)

// SchemaVersion is the on-disk version that Save writes today. v0.4.0
// shipped v1; v0.6.0 bumped to v2 to add ManeuverNode.Event for the
// burn-at-next scheduler; v0.6.5 bumped to v3 to add Payload.Missions
// for the mission-scaffold slice; v0.7.6 bumped to v4 to add
// per-node Throttle. v0.8.0 left the version at 4 — the new RCS
// fields ride along as omitempty additions, with the loader filling
// defaults for older saves. v0.8.1 bumped to v5 — the first
// non-additive migration: `Craft *Craft` → `Crafts []Craft` +
// `ActiveCraftIdx`. v0.9.1 bumps to v6 — Craft.Stages becomes the
// source of truth for propulsion + mass; pre-v6 craft entries
// migrate by wrapping the v5 flat fields into a single-element
// Stages slice (see migrateV5Craft). Load accepts any version in
// [1, SchemaVersion]; pre-v6 envelopes are translated on load.
// Bumps that need real migration logic should add a dedicated
// upgrade pass keyed off File.Version.
const SchemaVersion = 6

// File is the on-disk envelope.
type File struct {
	Version         int     `json:"version"`
	Generator       string  `json:"generator"`
	ClockT0         int64   `json:"clock_t0"`
	BodyCatalogHash string  `json:"body_catalog_hash"`
	Payload         Payload `json:"payload"`
}

// Payload carries the live simulation state. Anything derivable from
// the catalog (Systems, Calculator) is reconstructed on Load.
//
// v0.8.1 / schema v5: `Craft *Craft` (singular pointer) replaced by
// `Crafts []Craft` (slice) + `ActiveCraftIdx`. Pre-v5 saves with a
// non-nil singular `Craft` field are translated by `migrateV4ToV5`
// in save_migrate.go on load.
type Payload struct {
	SystemIdx      int                `json:"system_idx"`
	SimTimeNano    int64              `json:"sim_time_unix_nano"`
	BaseStepNano   int64              `json:"base_step_nano"`
	WarpIdx        int                `json:"warp_idx"`
	Paused         bool               `json:"paused"`
	Focus          Focus              `json:"focus"`
	Target         *Target            `json:"target,omitempty"`   // v0.9.0+ unified target slot. nil pointer (zero/None) → omitted on the wire.
	NavMode        int                `json:"nav_mode,omitempty"` // v0.9.3+ KSP-style SAS reference frame. NavOrbit=0 omitted; older saves load with NavOrbit.
	Craft          *Craft             `json:"craft,omitempty"`    // v1–v4 singular form; migrated to Crafts on load.
	Crafts         []Craft            `json:"crafts,omitempty"`
	ActiveCraftIdx int                `json:"active_craft_idx,omitempty"`
	Nodes          []Node             `json:"nodes,omitempty"`
	ActiveBurn     *ActiveBurn        `json:"active_burn,omitempty"`
	Missions       []missions.Mission `json:"missions,omitempty"`
}

// Focus mirrors sim.Focus by value.
type Focus struct {
	Kind    int `json:"kind"`
	BodyIdx int `json:"body_idx"`
}

// Target mirrors sim.Target by value. v0.9.0+. The zero value
// (Kind=0=TargetNone, BodyIdx=0, CraftIdx=0) is suppressed by the
// payload's `omitempty` tag, so saves predating v0.9.0 round-trip
// without writing the field — and load fills sim.World.Target with
// the zero value, matching pre-target behaviour.
type Target struct {
	Kind     int `json:"kind"`
	BodyIdx  int `json:"body_idx,omitempty"`
	CraftIdx int `json:"craft_idx,omitempty"`
}

// Vec3 is the wire form of orbital.Vec3.
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Craft mirrors spacecraft.Spacecraft. Primary is referenced by ID;
// the rehydrated value is looked up across loaded systems on Load.
//
// Monoprop / MonopropCapacity / RCSThrust / RCSIsp (v0.8.0+, schema
// v4) are omitempty so v1–v3 saves round-trip cleanly: absent → 0.0
// in JSON, populated from spacecraft.DefaultRCSLoadout(DryMass) at
// load time so older saves inherit RCS without a migration.
//
// Stages (v0.9.1+, schema v6) is the source of truth for propulsion
// + mass. Pre-v6 saves omit the field; the load path wraps the v5
// flat fields (DryMass / Fuel / Isp / Thrust / Monoprop / etc.)
// into a single-element Stages slice via migrateV5Craft so the
// rehydrated Spacecraft has Stages populated regardless of save
// vintage. The flat fields stay on the wire for v6 too — they're
// derived shadow-mirror values that round-trip with the same numbers
// SyncFields would compute, so a v6 save loaded into a hypothetical
// v5 reader (none in production, but possible for tooling) would
// still see a coherent craft.
type Craft struct {
	Name             string  `json:"name"`
	DryMass          float64 `json:"dry_mass"`
	Fuel             float64 `json:"fuel"`
	Isp              float64 `json:"isp"`
	Thrust           float64 `json:"thrust"`
	PrimaryID        string  `json:"primary_id"`
	R                Vec3    `json:"r"`
	V                Vec3    `json:"v"`
	M                float64 `json:"m"`
	Monoprop         float64 `json:"monoprop,omitempty"`
	MonopropCapacity float64 `json:"monoprop_capacity,omitempty"`
	RCSThrust        float64 `json:"rcs_thrust,omitempty"`
	RCSIsp           float64 `json:"rcs_isp,omitempty"`
	LoadoutID        string  `json:"loadout_id,omitempty"`
	Role             string  `json:"role,omitempty"`
	Glyph            string  `json:"glyph,omitempty"`
	Color            string  `json:"color,omitempty"`
	// v0.9.1+: per-stage breakdown, bottom-first. omitempty so pre-
	// v6 saves don't write the field and v6 saves of single-stage
	// craft still wire it out for consumers that want stage-level
	// detail.
	Stages []Stage `json:"stages,omitempty"`
	// v0.8.3+: docked-composite components for Undock to restore.
	// Empty for non-composite craft.
	DockedComponents []DockedComponent `json:"docked_components,omitempty"`
	// v0.8.1+ — per-craft burn state. Pre-v5 saves had Nodes /
	// ActiveBurn / etc. on the Payload (one shared list); the
	// migration on load splits the singular into the active craft's
	// fields.
	Nodes        []Node      `json:"nodes,omitempty"`
	ActiveBurn   *ActiveBurn `json:"active_burn,omitempty"`
	AttitudeMode int         `json:"attitude_mode,omitempty"`
	EngineMode   int         `json:"engine_mode,omitempty"`

	// Target (v0.9.3 polish): per-craft target binding. Pre-polish
	// saves had a single payload-level Target; the load path now
	// migrates that into the active craft's slot when no per-craft
	// targets are present. omitempty so legacy saves with no target
	// AND fresh untargeted craft both round-trip without writing the
	// field.
	Target *Target `json:"target,omitempty"`

	// PitchTrim (v0.9.2+, schema v6 additive): signed pitch-trim
	// offset in radians applied on top of the active BurnMode.
	// omitempty so legacy saves with no trim load with PitchTrim=0
	// (= no trim, the v0.9.2-pre behaviour).
	PitchTrim float64 `json:"pitch_trim,omitempty"`

	// CurrentAttitudeDir (v0.10.0+, schema v6 additive): the craft's
	// physical nose unit vector. Slew makes attitude load-bearing —
	// a craft can be caught mid-slew — so the real nose must round-
	// trip or a reload teleports it. Pre-v0.10.0 saves lack the key →
	// decodes to a zero Vec3 → the slew integrator's first-tick snap
	// guard seeds it from the commanded direction (no teleport, no
	// slew-from-garbage). No schema bump (additive). SlewRate is NOT
	// persisted — it is re-derived from the loadout on load.
	CurrentAttitudeDir Vec3 `json:"current_attitude_dir,omitempty"`

	// Landed (v0.9.2+, schema v6 additive): true when the craft is
	// parked on its primary's surface co-rotating with the ground.
	// Pre-v0.9.2 saves load with Landed=false (= normal integration,
	// the v0.9.2-pre behaviour).
	Landed bool `json:"landed,omitempty"`

	// LaunchLatDeg / LaunchLonDeg (v0.9.2+, schema v6 additive):
	// body-fixed (lat, lon) of the launchpad spawn. Only meaningful
	// when Landed=true.
	LaunchLatDeg float64 `json:"launch_lat_deg,omitempty"`
	LaunchLonDeg float64 `json:"launch_lon_deg,omitempty"`

	// Crashed / CanSoftLand / OnPad / LandedLatDeg / LandedLonDeg
	// (v0.11.4+, schema v6 additive — no bump, per ADR 0004). All
	// `omitempty`-default-false so pre-v0.11.4 saves round-trip
	// cleanly: existing in-flight vessels load with Crashed=false /
	// CanSoftLand=false / OnPad=false (= normal integration, no
	// soft-land qualification, no auto-route gate), which matches
	// pre-lifecycle behaviour. New vessels saved with these set
	// restore the destructive / soft-landed / on-pad state on load.
	Crashed      bool    `json:"crashed,omitempty"`
	CanSoftLand  bool    `json:"can_soft_land,omitempty"`
	OnPad        bool    `json:"on_pad,omitempty"`
	LandedLatDeg float64 `json:"landed_lat_deg,omitempty"`
	LandedLonDeg float64 `json:"landed_lon_deg,omitempty"`

	// ChuteState (v0.12 Slice 3 / ADR 0008, schema v6 additive — no
	// bump): the runtime parachute deploy state (0=Stowed, 1=Armed,
	// 2=Deployed). omitempty so pre-Slice-3 saves round-trip without
	// the field: absent ⇒ 0 ⇒ Stowed, correct for any vessel saved
	// before this slice. The per-Stage HasParachute capability rides
	// the Stage DTO; SyncFields re-derives the flat Spacecraft mirror.
	ChuteState int `json:"chute_state,omitempty"`

	// DecouplePlan (v0.12 Slice 2 / ADR 0007, schema v6 additive — no
	// bump): the remaining bottom-up staging group sizes. omitempty so
	// pre-v0.12 saves and craft with no plan (the common single-pop
	// case) round-trip without writing the field: absent ⇒ nil ⇒
	// single-pop. Persisted (not derived from the catalog) so a
	// mission saved mid-staging — e.g. an Apollo Stack with S-IC
	// already dropped, plan [1,1,2] remaining — restores the correct
	// grouping for the still-pending LM extraction.
	DecouplePlan []int `json:"decouple_plan,omitempty"`
}

// Stage mirrors spacecraft.Stage on the wire. v0.9.1+. All numeric
// fields are omitempty so a single-stage craft with default RCS pool
// + zero monoprop residual still serialises compactly.
type Stage struct {
	LoadoutID            string  `json:"loadout_id,omitempty"`
	Name                 string  `json:"name,omitempty"`
	Glyph                string  `json:"glyph,omitempty"`
	Color                string  `json:"color,omitempty"`
	DryMass              float64 `json:"dry_mass,omitempty"`
	FuelMass             float64 `json:"fuel_mass,omitempty"`
	FuelCapacity         float64 `json:"fuel_capacity,omitempty"`
	Thrust               float64 `json:"thrust,omitempty"`
	Isp                  float64 `json:"isp,omitempty"`
	MonopropMass         float64 `json:"monoprop_mass,omitempty"`
	MonopropCap          float64 `json:"monoprop_cap,omitempty"`
	RCSThrust            float64 `json:"rcs_thrust,omitempty"`
	RCSIsp               float64 `json:"rcs_isp,omitempty"`
	BallisticCoefficient float64 `json:"ballistic_coefficient,omitempty"`

	// CanSoftLand (v0.11.4-followup, schema v6 additive — no bump
	// per ADR 0004): per-Stage soft-land flag, round-tripped on the
	// wire so a saved Falcon-9 S1 (or Apollo-Stack Lander stage)
	// loads with the right surface-arrival-predicate gate even after
	// SyncFields re-derives the flat Spacecraft.CanSoftLand mirror.
	// Pre-v0.11.4 saves load with the field absent → default-false,
	// which matches every pre-v0.11.4 catalog stage.
	CanSoftLand bool `json:"can_soft_land,omitempty"`

	// HasParachute (v0.12 Slice 3 / ADR 0008, schema v6 additive — no
	// bump): per-Stage parachute capability, round-tripped on the wire
	// so a saved capsule (or the Apollo CSM stage) loads with the right
	// chute-route gate after SyncFields re-derives the flat
	// Spacecraft.HasParachute mirror. Pre-Slice-3 saves load with the
	// field absent → default-false, matching every pre-Slice-3 stage.
	HasParachute bool `json:"has_parachute,omitempty"`
}

// Node mirrors sim.ManeuverNode. Event (v0.6.0+, schema v2) is
// omitempty so v1 saves round-trip cleanly: the field is absent on
// disk and unmarshals to zero (TriggerAbsolute), which matches the
// pre-v0.6 behaviour. v2 saves with non-zero Event encode the integer
// directly. Throttle (v0.7.6+, schema v4) is omitempty so v1–v3
// saves round-trip cleanly — absent → 0.0 in JSON, mapped to 1.0
// (full throttle, the prior universal behaviour) in worldFromPayload.
type Node struct {
	TriggerTimeNano int64   `json:"trigger_time_unix_nano"`
	Mode            int     `json:"mode"`
	DV              float64 `json:"dv"`
	DurationNano    int64   `json:"duration_nano"`
	PrimaryID       string  `json:"primary_id"`
	Event           int     `json:"event,omitempty"`
	Throttle        float64 `json:"throttle,omitempty"`
	// TargetCraftIdx (v0.9.3+) is the one-based slate idx the node
	// is bound to for target-relative modes / TriggerNextClosest
	// Approach event. Zero = no target. Additive — pre-v0.9.3 saves
	// load with TargetCraftIdx=0, which is correct semantics for
	// non-target nodes. No schema bump required.
	TargetCraftIdx int `json:"target_craft_idx,omitempty"`
	// PlaneChangeRad (v0.12.x, schema v6 additive) — the signed rotation
	// angle for a BurnPlaneChange node (the `I` inclination plant + the
	// Slice 5 split-strategy plane change). Pre-v0.12 saves omit it;
	// absent → 0, the correct value for non-plane-change nodes. Was
	// dropped on save before v0.12 (a latent v0.10.4 gap); the split
	// strategy makes it load-bearing, so it now round-trips.
	PlaneChangeRad float64 `json:"plane_change_rad,omitempty"`
	// BurnDirUnit (v0.12.x, schema v6 additive) — the captured inertial
	// thrust direction for a BurnVector node (the fused-Lambert combined
	// departure). Additive/omitempty, following the CurrentAttitudeDir
	// precedent; absent → zero vector for non-BurnVector nodes. No migration.
	BurnDirUnit Vec3 `json:"burn_dir_unit,omitempty"`
}

// DockedComponent mirrors spacecraft.DockedComponent. v0.8.3+.
type DockedComponent struct {
	Name             string  `json:"name"`
	LoadoutID        string  `json:"loadout_id,omitempty"`
	Role             string  `json:"role,omitempty"`
	Glyph            string  `json:"glyph,omitempty"`
	Color            string  `json:"color,omitempty"`
	DryMass          float64 `json:"dry_mass"`
	FuelCapacity     float64 `json:"fuel_capacity,omitempty"`
	MonopropCapacity float64 `json:"monoprop_capacity,omitempty"`
	Isp              float64 `json:"isp,omitempty"`
	Thrust           float64 `json:"thrust,omitempty"`
	RCSThrust        float64 `json:"rcs_thrust,omitempty"`
	RCSIsp           float64 `json:"rcs_isp,omitempty"`
}

// ActiveBurn mirrors sim.ActiveBurn. Throttle (v0.7.6+, schema v4)
// is omitempty so v1–v3 saves with an in-flight burn round-trip
// cleanly: absent → 0.0 unmarshals → world.go's stepThrust defaults
// to 1.0 (the universal pre-v0.7.6 behaviour).
type ActiveBurn struct {
	Mode        int     `json:"mode"`
	DVRemaining float64 `json:"dv_remaining"`
	EndTimeNano int64   `json:"end_time_unix_nano"`
	PrimaryID   string  `json:"primary_id"`
	Throttle    float64 `json:"throttle,omitempty"`
	// TargetCraftIdx (v0.9.3+) — see Node.TargetCraftIdx; mirrored
	// onto in-flight finite burns so a save mid-rendezvous-burn
	// reloads with the burn still tracking its bound target.
	TargetCraftIdx int `json:"target_craft_idx,omitempty"`
	// PlaneChangeRad / BurnDirUnit (v0.12.x, schema v6 additive) — mirror
	// the ManeuverNode fields onto an in-flight burn so a save mid
	// plane-change / BurnVector burn reloads with the direction intact.
	PlaneChangeRad float64 `json:"plane_change_rad,omitempty"`
	BurnDirUnit    Vec3    `json:"burn_dir_unit,omitempty"`
}

// Errors returned by Load.
var (
	ErrSchemaMismatch  = errors.New("save: schema version mismatch")
	ErrCatalogMismatch = errors.New("save: body catalog hash mismatch")
	ErrCraftPrimary    = errors.New("save: craft primary not found in loaded systems")
)

// DefaultPath returns the platform-appropriate save path. Honors
// $XDG_STATE_HOME on linux/macOS; falls back to ~/.local/state. Windows
// users can set $XDG_STATE_HOME explicitly.
func DefaultPath() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "save.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "terminal-space-program", "save.json"), nil
}

// Save serialises w to path, creating parent directories as needed.
// Atomic on POSIX: writes to a sibling tmpfile and renames into place.
func Save(w *sim.World, path string) error {
	hash, err := bodies.CatalogHash()
	if err != nil {
		return err
	}
	f := File{
		Version:         SchemaVersion,
		Generator:       fmt.Sprintf("tsp %s", version.Version),
		ClockT0:         time.Now().UnixNano(),
		BodyCatalogHash: hash,
		Payload:         payloadFromWorld(w),
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal save: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create save dir: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmpfile: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename into place: %w", err)
	}
	return nil
}

// Load reads path, validates the envelope, and returns a fresh World
// hydrated from the payload. Errors with ErrSchemaMismatch on version
// drift, ErrCatalogMismatch on body-catalog drift, or ErrCraftPrimary
// when the craft references a primary that no longer exists.
func Load(path string) (*sim.World, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read save: %w", err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse save: %w", err)
	}
	// v0.6.0: accept any version in [1, SchemaVersion]. New fields land
	// on the wire as omitempty / zero-value-defaulting, so older saves
	// just unmarshal with the defaults. Bumps that need real migration
	// should switch on f.Version explicitly here.
	if f.Version < 1 || f.Version > SchemaVersion {
		return nil, fmt.Errorf("%w: got %d, want 1..%d", ErrSchemaMismatch, f.Version, SchemaVersion)
	}
	systems, err := bodies.LoadAll()
	if err != nil {
		return nil, err
	}
	currentHash, err := bodies.CatalogHash()
	if err != nil {
		return nil, err
	}
	if f.BodyCatalogHash != currentHash {
		return nil, fmt.Errorf("%w: save=%s current=%s", ErrCatalogMismatch, f.BodyCatalogHash, currentHash)
	}
	return worldFromPayload(f.Payload, systems)
}

func payloadFromWorld(w *sim.World) Payload {
	p := Payload{
		SystemIdx:    w.SystemIdx,
		SimTimeNano:  w.Clock.SimTime.UnixNano(),
		BaseStepNano: int64(w.Clock.BaseStep),
		WarpIdx:      w.Clock.WarpIdx,
		Paused:       w.Clock.Paused,
		Focus: Focus{
			Kind:    int(w.Focus.Kind),
			BodyIdx: w.Focus.BodyIdx,
		},
	}
	// v0.9.3 polish: per-craft target replaces the payload-level
	// Target slot. Each craft writes its own Target onto its Craft
	// record (see the loop below); the payload-level Target field
	// is no longer written. Read path retains backwards-compat
	// support for older saves that wrote the payload-level field.
	// v0.9.3+: persist NavMode. NavOrbit=0 round-trips as omitempty so
	// pre-v0.9.3 saves (which never carried the field) load with the
	// default-frame behaviour they were written under.
	p.NavMode = int(w.NavMode)
	// v0.8.1+: Crafts becomes the wire form. Each craft carries its
	// own Nodes / ActiveBurn / AttitudeMode / EngineMode (per-craft
	// burn state). Pre-v5 saves had these on the Payload; the load
	// path migrates the singular form into the active craft's
	// fields.
	for _, c := range w.Crafts {
		if c == nil {
			continue
		}
		wc := Craft{
			Name:               c.Name,
			DryMass:            c.DryMass,
			Fuel:               c.Fuel,
			Isp:                c.Isp,
			Thrust:             c.Thrust,
			PrimaryID:          c.Primary.ID,
			R:                  vec3From(c.State.R),
			V:                  vec3From(c.State.V),
			M:                  c.State.M,
			Monoprop:           c.Monoprop,
			MonopropCapacity:   c.MonopropCapacity,
			RCSThrust:          c.RCSThrust,
			RCSIsp:             c.RCSIsp,
			AttitudeMode:       int(c.AttitudeMode),
			EngineMode:         int(c.EngineMode),
			LoadoutID:          c.LoadoutID,
			Role:               c.Role,
			Glyph:              c.Glyph,
			Color:              c.Color,
			PitchTrim:          c.PitchTrim,
			CurrentAttitudeDir: vec3From(c.CurrentAttitudeDir),
			Landed:             c.Landed,
			LaunchLatDeg:       c.LaunchLatDeg,
			LaunchLonDeg:       c.LaunchLonDeg,
			Crashed:            c.Crashed,
			CanSoftLand:        c.CanSoftLand,
			OnPad:              c.OnPad,
			LandedLatDeg:       c.LandedLatDeg,
			LandedLonDeg:       c.LandedLonDeg,
			DecouplePlan:       c.DecouplePlan,
			ChuteState:         int(c.ChuteState),
		}
		// v0.9.1+: serialize Stages so v6 saves carry per-stage
		// detail. Single-stage craft still wire out a one-element
		// Stages — round-trips through the same migrate path that
		// v5 craft fall through.
		for _, s := range c.Stages {
			wc.Stages = append(wc.Stages, Stage{
				LoadoutID:            s.LoadoutID,
				Name:                 s.Name,
				Glyph:                s.Glyph,
				Color:                s.Color,
				DryMass:              s.DryMass,
				FuelMass:             s.FuelMass,
				FuelCapacity:         s.FuelCapacity,
				Thrust:               s.Thrust,
				Isp:                  s.Isp,
				MonopropMass:         s.MonopropMass,
				MonopropCap:          s.MonopropCap,
				RCSThrust:            s.RCSThrust,
				RCSIsp:               s.RCSIsp,
				BallisticCoefficient: s.BallisticCoefficient,
				CanSoftLand:          s.CanSoftLand,
				HasParachute:         s.HasParachute,
			})
		}
		for _, dc := range c.DockedComponents {
			wc.DockedComponents = append(wc.DockedComponents, DockedComponent{
				Name:             dc.Name,
				LoadoutID:        dc.LoadoutID,
				Role:             dc.Role,
				Glyph:            dc.Glyph,
				Color:            dc.Color,
				DryMass:          dc.DryMass,
				FuelCapacity:     dc.FuelCapacity,
				MonopropCapacity: dc.MonopropCapacity,
				Isp:              dc.Isp,
				Thrust:           dc.Thrust,
				RCSThrust:        dc.RCSThrust,
				RCSIsp:           dc.RCSIsp,
			})
		}
		for _, n := range c.Nodes {
			var trigNano int64
			if !n.TriggerTime.IsZero() {
				trigNano = n.TriggerTime.UnixNano()
			}
			wc.Nodes = append(wc.Nodes, Node{
				TriggerTimeNano: trigNano,
				Mode:            int(n.Mode),
				DV:              n.DV,
				DurationNano:    int64(n.Duration),
				PrimaryID:       n.PrimaryID,
				Event:           int(n.Event),
				Throttle:        n.Throttle,
				TargetCraftIdx:  n.TargetCraftIdx,
				PlaneChangeRad:  n.PlaneChangeRad,
				BurnDirUnit:     vec3From(n.BurnDirUnit),
			})
		}
		if c.ActiveBurn != nil {
			wc.ActiveBurn = &ActiveBurn{
				Mode:           int(c.ActiveBurn.Mode),
				DVRemaining:    c.ActiveBurn.DVRemaining,
				EndTimeNano:    c.ActiveBurn.EndTime.UnixNano(),
				PrimaryID:      c.ActiveBurn.PrimaryID,
				Throttle:       c.ActiveBurn.Throttle,
				TargetCraftIdx: c.ActiveBurn.TargetCraftIdx,
				PlaneChangeRad: c.ActiveBurn.PlaneChangeRad,
				BurnDirUnit:    vec3From(c.ActiveBurn.BurnDirUnit),
			}
		}
		// v0.9.3 polish: per-craft Target. Skip serialising when
		// the craft has no target so untargeted craft still write
		// out the same minimal JSON they did pre-polish.
		if c.Target.Kind != spacecraft.TargetNone {
			wc.Target = &Target{
				Kind:     int(c.Target.Kind),
				BodyIdx:  c.Target.BodyIdx,
				CraftIdx: c.Target.CraftIdx,
			}
		}
		p.Crafts = append(p.Crafts, wc)
	}
	p.ActiveCraftIdx = w.ActiveCraftIdx
	p.Missions = missions.Clone(w.Missions)
	return p
}

func worldFromPayload(p Payload, systems []bodies.System) (*sim.World, error) {
	if p.SystemIdx < 0 || p.SystemIdx >= len(systems) {
		return nil, fmt.Errorf("save: system_idx %d out of range (have %d systems)", p.SystemIdx, len(systems))
	}
	simT := time.Unix(0, p.SimTimeNano).UTC()
	clock := &sim.Clock{
		SimTime: simT,
		// v0.8.5.7+: RotationTime drives planet-rotation animation
		// (capped at RotationCapWarp). Old saves don't carry it;
		// initialise to SimTime so the rotation phase looks right
		// at load time. The cap-induced lag is forgotten on save /
		// reload, which is fine — rotation is an aesthetic, not
		// authoritative state.
		RotationTime: simT,
		WarpIdx:      p.WarpIdx,
		Paused:       p.Paused,
		BaseStep:     time.Duration(p.BaseStepNano),
	}
	w := &sim.World{
		Systems:   systems,
		SystemIdx: p.SystemIdx,
		Clock:     clock,
		Focus: sim.Focus{
			Kind:    sim.FocusKind(p.Focus.Kind),
			BodyIdx: p.Focus.BodyIdx,
		},
	}
	// v0.9.3 polish: per-craft Target supersedes the payload-level
	// slot. Per-craft restores happen below, in the wireCrafts loop.
	// The payload-level field is read here only for backwards-compat
	// — assigned to the active craft after Crafts are loaded (see the
	// reconcile block past the loop).
	// v0.9.3+: restore NavMode. Absent field → zero → NavOrbit (the
	// pre-v0.9.3 default frame).
	w.NavMode = sim.NavMode(p.NavMode)
	w.Calculator = orbital.ForSystem(w.System(), w.Clock.SimTime)

	// v0.8.1+: load path translates the pre-v5 singular Craft field
	// into the Crafts slice, and distributes pre-v5 payload-level
	// Nodes / ActiveBurn into the active craft's fields. v5 saves
	// arrive with everything already nested under each Craft.
	wireCrafts := p.Crafts
	if p.Craft != nil && len(wireCrafts) == 0 {
		wireCrafts = []Craft{*p.Craft}
	}
	for _, wc := range wireCrafts {
		primary, ok := bodies.LookupByID(systems, wc.PrimaryID)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrCraftPrimary, wc.PrimaryID)
		}
		// v0.8.0+: pre-RCS saves (v3 and earlier wire-out) carry zero
		// RCS fields. Populate from DefaultRCSLoadout(DryMass) so
		// older saves inherit a full RCS budget without a schema bump.
		monoprop := wc.Monoprop
		monoCap := wc.MonopropCapacity
		rcsThrust := wc.RCSThrust
		rcsIsp := wc.RCSIsp
		if monoCap == 0 && rcsThrust == 0 && rcsIsp == 0 {
			monoprop, monoCap, rcsThrust, rcsIsp = spacecraft.DefaultRCSLoadout(wc.DryMass)
		}
		// v0.9.1+: build Stages from the wire form, falling back to a
		// single-element migration of the v5 flat fields when the
		// wire entry doesn't carry Stages (pre-v6 saves OR v6 saves
		// where the flat fields predate the migration). Once Stages
		// is populated, SyncFields below re-derives the legacy flat
		// fields from Stages so consumers stay coherent.
		stages := wireStagesToSim(wc.Stages)
		if len(stages) == 0 {
			stages = migrateV5CraftToStages(wc, monoprop, monoCap, rcsThrust, rcsIsp)
		}
		c := &spacecraft.Spacecraft{
			Name:             wc.Name,
			DryMass:          wc.DryMass,
			Fuel:             wc.Fuel,
			Isp:              wc.Isp,
			Thrust:           wc.Thrust,
			Throttle:         1.0, // v0.7.3+: transient.
			Monoprop:         monoprop,
			MonopropCapacity: monoCap,
			RCSThrust:        rcsThrust,
			RCSIsp:           rcsIsp,
			Stages:           stages,
			Primary:          primary,
			State: physics.StateVector{
				R: vec3To(wc.R),
				V: vec3To(wc.V),
				M: wc.M,
			},
			AttitudeMode:       spacecraft.BurnMode(wc.AttitudeMode),
			EngineMode:         spacecraft.EngineMode(wc.EngineMode),
			LoadoutID:          wc.LoadoutID,
			Role:               wc.Role,
			Glyph:              wc.Glyph,
			Color:              wc.Color,
			PitchTrim:          wc.PitchTrim,
			CurrentAttitudeDir: vec3To(wc.CurrentAttitudeDir),
			Landed:             wc.Landed,
			LaunchLatDeg:       wc.LaunchLatDeg,
			LaunchLonDeg:       wc.LaunchLonDeg,
			Crashed:            wc.Crashed,
			CanSoftLand:        wc.CanSoftLand,
			OnPad:              wc.OnPad,
			LandedLatDeg:       wc.LandedLatDeg,
			LandedLonDeg:       wc.LandedLonDeg,
			DecouplePlan:       wc.DecouplePlan,
			ChuteState:         spacecraft.ChuteState(wc.ChuteState),
		}
		c.SyncFields()
		// v0.8.2+: pre-v0.8.2 saves carry no Glyph/Color; backfill
		// from the loadout catalog so older saves get the visual
		// differentiation without manual edits. LoadoutID empty
		// resolves to the S-IVB-1 default.
		if c.Glyph == "" || c.Color == "" {
			l := spacecraft.LookupLoadout(c.LoadoutID)
			if c.LoadoutID == "" {
				c.LoadoutID = l.ID
			}
			if c.Role == "" {
				c.Role = l.Role
			}
			if c.Glyph == "" {
				c.Glyph = l.Glyph
			}
			if c.Color == "" {
				c.Color = l.Color
			}
		}
		for _, dc := range wc.DockedComponents {
			c.DockedComponents = append(c.DockedComponents, spacecraft.DockedComponent{
				Name:             dc.Name,
				LoadoutID:        dc.LoadoutID,
				Role:             dc.Role,
				Glyph:            dc.Glyph,
				Color:            dc.Color,
				DryMass:          dc.DryMass,
				FuelCapacity:     dc.FuelCapacity,
				MonopropCapacity: dc.MonopropCapacity,
				Isp:              dc.Isp,
				Thrust:           dc.Thrust,
				RCSThrust:        dc.RCSThrust,
				RCSIsp:           dc.RCSIsp,
			})
		}
		// v0.8.1+: per-craft Nodes / ActiveBurn loaded directly from
		// each Craft entry.
		for _, n := range wc.Nodes {
			var trig time.Time
			if n.TriggerTimeNano != 0 {
				trig = time.Unix(0, n.TriggerTimeNano).UTC()
			}
			c.Nodes = append(c.Nodes, sim.ManeuverNode{
				TriggerTime:    trig,
				Mode:           spacecraft.BurnMode(n.Mode),
				DV:             n.DV,
				Duration:       time.Duration(n.DurationNano),
				PrimaryID:      n.PrimaryID,
				Event:          sim.TriggerEvent(n.Event),
				Throttle:       n.Throttle,
				TargetCraftIdx: n.TargetCraftIdx,
				PlaneChangeRad: n.PlaneChangeRad,
				BurnDirUnit:    vec3To(n.BurnDirUnit),
			})
		}
		if wc.ActiveBurn != nil {
			c.ActiveBurn = &sim.ActiveBurn{
				Mode:           spacecraft.BurnMode(wc.ActiveBurn.Mode),
				DVRemaining:    wc.ActiveBurn.DVRemaining,
				EndTime:        time.Unix(0, wc.ActiveBurn.EndTimeNano).UTC(),
				PrimaryID:      wc.ActiveBurn.PrimaryID,
				Throttle:       wc.ActiveBurn.Throttle,
				TargetCraftIdx: wc.ActiveBurn.TargetCraftIdx,
				PlaneChangeRad: wc.ActiveBurn.PlaneChangeRad,
				BurnDirUnit:    vec3To(wc.ActiveBurn.BurnDirUnit),
			}
		}
		// v0.9.3 polish: per-craft Target. Pre-polish saves omit the
		// field; nil pointer leaves the craft's Target at zero
		// (TargetNone) which is the fresh-craft default.
		if wc.Target != nil {
			c.Target = spacecraft.Target{
				Kind:     spacecraft.TargetKind(wc.Target.Kind),
				BodyIdx:  wc.Target.BodyIdx,
				CraftIdx: wc.Target.CraftIdx,
			}
		}
		w.Crafts = append(w.Crafts, c)
	}
	if p.ActiveCraftIdx >= 0 && p.ActiveCraftIdx < len(w.Crafts) {
		w.ActiveCraftIdx = p.ActiveCraftIdx
	}
	// v0.9.3 polish: backwards-compat. Pre-polish saves serialised a
	// single payload-level Target slot. Migrate it onto the active
	// craft so legacy saves load with the binding the player set
	// pre-polish. Skip when any craft already carries a per-craft
	// Target (a polish-era save) so we don't clobber.
	if p.Target != nil {
		legacyTarget := spacecraft.Target{
			Kind:     spacecraft.TargetKind(p.Target.Kind),
			BodyIdx:  p.Target.BodyIdx,
			CraftIdx: p.Target.CraftIdx,
		}
		anyPerCraft := false
		for _, c := range w.Crafts {
			if c != nil && c.Target.Kind != spacecraft.TargetNone {
				anyPerCraft = true
				break
			}
		}
		if !anyPerCraft {
			if active := w.ActiveCraft(); active != nil {
				active.Target = legacyTarget
			}
		}
	}
	// Sync world-level live cursor to the active craft's stored
	// target so readers (`w.Target.Kind == sim.TargetCraft` etc.)
	// see the right binding immediately after load.
	if active := w.ActiveCraft(); active != nil {
		w.Target = active.Target
	}
	// Pre-v5 payload-level Nodes / ActiveBurn → active craft's
	// fields. The migration assumes pre-v5 saves had a single craft
	// (which is correct: pre-v5 World had a single Craft pointer).
	if active := w.ActiveCraft(); active != nil {
		for _, n := range p.Nodes {
			var trig time.Time
			if n.TriggerTimeNano != 0 {
				trig = time.Unix(0, n.TriggerTimeNano).UTC()
			}
			active.Nodes = append(active.Nodes, sim.ManeuverNode{
				TriggerTime:    trig,
				Mode:           spacecraft.BurnMode(n.Mode),
				DV:             n.DV,
				Duration:       time.Duration(n.DurationNano),
				PrimaryID:      n.PrimaryID,
				Event:          sim.TriggerEvent(n.Event),
				Throttle:       n.Throttle,
				TargetCraftIdx: n.TargetCraftIdx,
				PlaneChangeRad: n.PlaneChangeRad,
				BurnDirUnit:    vec3To(n.BurnDirUnit),
			})
		}
		if p.ActiveBurn != nil && active.ActiveBurn == nil {
			active.ActiveBurn = &sim.ActiveBurn{
				Mode:           spacecraft.BurnMode(p.ActiveBurn.Mode),
				DVRemaining:    p.ActiveBurn.DVRemaining,
				EndTime:        time.Unix(0, p.ActiveBurn.EndTimeNano).UTC(),
				PrimaryID:      p.ActiveBurn.PrimaryID,
				Throttle:       p.ActiveBurn.Throttle,
				TargetCraftIdx: p.ActiveBurn.TargetCraftIdx,
				PlaneChangeRad: p.ActiveBurn.PlaneChangeRad,
				BurnDirUnit:    vec3To(p.ActiveBurn.BurnDirUnit),
			}
		}
	}
	// v0.6.5: missions persist with status. v3+ saves carry an explicit
	// (possibly-empty) Missions slice; v1/v2 saves wire-out as nil and
	// get the embedded starter catalog seeded fresh so older saves
	// gain the new feature without a manual edit. A failed catalog
	// load is non-fatal — missions are additive.
	if p.Missions != nil {
		w.Missions = missions.Clone(p.Missions)
	} else if cat, err := missions.DefaultCatalog(); err == nil {
		w.Missions = missions.Clone(cat.Missions)
	}
	return w, nil
}

func vec3From(v orbital.Vec3) Vec3 { return Vec3{X: v.X, Y: v.Y, Z: v.Z} }
func vec3To(v Vec3) orbital.Vec3   { return orbital.Vec3{X: v.X, Y: v.Y, Z: v.Z} }
