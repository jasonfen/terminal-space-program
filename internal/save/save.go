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
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/version"
)

// SchemaVersion is the on-disk version. v0.4.0 ships v1; later schema
// changes bump and migrate.
const SchemaVersion = 1

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
type Payload struct {
	SystemIdx    int         `json:"system_idx"`
	SimTimeNano  int64       `json:"sim_time_unix_nano"`
	BaseStepNano int64       `json:"base_step_nano"`
	WarpIdx      int         `json:"warp_idx"`
	Paused       bool        `json:"paused"`
	Focus        Focus       `json:"focus"`
	Craft        *Craft      `json:"craft,omitempty"`
	Nodes        []Node      `json:"nodes,omitempty"`
	ActiveBurn   *ActiveBurn `json:"active_burn,omitempty"`
}

// Focus mirrors sim.Focus by value.
type Focus struct {
	Kind    int `json:"kind"`
	BodyIdx int `json:"body_idx"`
}

// Vec3 is the wire form of orbital.Vec3.
type Vec3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Craft mirrors spacecraft.Spacecraft. Primary is referenced by ID;
// the rehydrated value is looked up across loaded systems on Load.
type Craft struct {
	Name      string  `json:"name"`
	DryMass   float64 `json:"dry_mass"`
	Fuel      float64 `json:"fuel"`
	Isp       float64 `json:"isp"`
	Thrust    float64 `json:"thrust"`
	PrimaryID string  `json:"primary_id"`
	R         Vec3    `json:"r"`
	V         Vec3    `json:"v"`
	M         float64 `json:"m"`
}

// Node mirrors sim.ManeuverNode.
type Node struct {
	TriggerTimeNano int64   `json:"trigger_time_unix_nano"`
	Mode            int     `json:"mode"`
	DV              float64 `json:"dv"`
	DurationNano    int64   `json:"duration_nano"`
	PrimaryID       string  `json:"primary_id"`
}

// ActiveBurn mirrors sim.ActiveBurn.
type ActiveBurn struct {
	Mode        int     `json:"mode"`
	DVRemaining float64 `json:"dv_remaining"`
	EndTimeNano int64   `json:"end_time_unix_nano"`
	PrimaryID   string  `json:"primary_id"`
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
	if f.Version != SchemaVersion {
		return nil, fmt.Errorf("%w: got %d, want %d", ErrSchemaMismatch, f.Version, SchemaVersion)
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
	if w.Craft != nil {
		p.Craft = &Craft{
			Name:      w.Craft.Name,
			DryMass:   w.Craft.DryMass,
			Fuel:      w.Craft.Fuel,
			Isp:       w.Craft.Isp,
			Thrust:    w.Craft.Thrust,
			PrimaryID: w.Craft.Primary.ID,
			R:         vec3From(w.Craft.State.R),
			V:         vec3From(w.Craft.State.V),
			M:         w.Craft.State.M,
		}
	}
	for _, n := range w.Nodes {
		p.Nodes = append(p.Nodes, Node{
			TriggerTimeNano: n.TriggerTime.UnixNano(),
			Mode:            int(n.Mode),
			DV:              n.DV,
			DurationNano:    int64(n.Duration),
			PrimaryID:       n.PrimaryID,
		})
	}
	if w.ActiveBurn != nil {
		p.ActiveBurn = &ActiveBurn{
			Mode:        int(w.ActiveBurn.Mode),
			DVRemaining: w.ActiveBurn.DVRemaining,
			EndTimeNano: w.ActiveBurn.EndTime.UnixNano(),
			PrimaryID:   w.ActiveBurn.PrimaryID,
		}
	}
	return p
}

func worldFromPayload(p Payload, systems []bodies.System) (*sim.World, error) {
	if p.SystemIdx < 0 || p.SystemIdx >= len(systems) {
		return nil, fmt.Errorf("save: system_idx %d out of range (have %d systems)", p.SystemIdx, len(systems))
	}
	clock := &sim.Clock{
		SimTime:  time.Unix(0, p.SimTimeNano).UTC(),
		WarpIdx:  p.WarpIdx,
		Paused:   p.Paused,
		BaseStep: time.Duration(p.BaseStepNano),
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
	w.Calculator = orbital.ForSystem(w.System(), w.Clock.SimTime)

	if p.Craft != nil {
		primary, ok := bodies.LookupByID(systems, p.Craft.PrimaryID)
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrCraftPrimary, p.Craft.PrimaryID)
		}
		w.Craft = &spacecraft.Spacecraft{
			Name:    p.Craft.Name,
			DryMass: p.Craft.DryMass,
			Fuel:    p.Craft.Fuel,
			Isp:     p.Craft.Isp,
			Thrust:  p.Craft.Thrust,
			Primary: primary,
			State: physics.StateVector{
				R: vec3To(p.Craft.R),
				V: vec3To(p.Craft.V),
				M: p.Craft.M,
			},
		}
	}
	for _, n := range p.Nodes {
		w.Nodes = append(w.Nodes, sim.ManeuverNode{
			TriggerTime: time.Unix(0, n.TriggerTimeNano).UTC(),
			Mode:        spacecraft.BurnMode(n.Mode),
			DV:          n.DV,
			Duration:    time.Duration(n.DurationNano),
			PrimaryID:   n.PrimaryID,
		})
	}
	if p.ActiveBurn != nil {
		w.ActiveBurn = &sim.ActiveBurn{
			Mode:        spacecraft.BurnMode(p.ActiveBurn.Mode),
			DVRemaining: p.ActiveBurn.DVRemaining,
			EndTime:     time.Unix(0, p.ActiveBurn.EndTimeNano).UTC(),
			PrimaryID:   p.ActiveBurn.PrimaryID,
		}
	}
	return w, nil
}

func vec3From(v orbital.Vec3) Vec3 { return Vec3{X: v.X, Y: v.Y, Z: v.Z} }
func vec3To(v Vec3) orbital.Vec3   { return orbital.Vec3{X: v.X, Y: v.Y, Z: v.Z} }
