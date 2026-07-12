// Saves directory API — v0.26 / ADR 0033. The single fixed save.json
// slot becomes a saves/ directory of flat, independent, nameable
// Saves. Filenames are opaque (§B — the player-facing name lives in
// the envelope's Meta header); the directory is the source of truth
// (§C — the browser scans + header-parses, no sidecar index); the
// quicksave and autosave lanes are reserved filenames that named
// saves can never collide with (§D/§E). No SchemaVersion bump (§J) —
// Meta is additive envelope bookkeeping, the Payload shape is
// untouched.
package save

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// Meta is the envelope header the Saves browser lists games by —
// readable via ReadHeader without hydrating a World or touching the
// body catalog (ADR 0033 §C). All fields are additive/omit-on-zero so
// a pre-v0.26 v9 envelope (no meta key) still loads (§J); the legacy
// single-slot Save writer stamps no Meta at all.
//
// InGameEpoch is the simulation clock (Payload.SimTimeNano) at save
// time. Name is the player-facing Save name; reserved lanes
// (quicksave/autosave) leave it empty and are labelled by Lane instead.
type Meta struct {
	Name             string    `json:"name,omitempty"`
	InGameEpoch      time.Time `json:"in_game_epoch,omitzero"`
	ActiveVesselName string    `json:"active_vessel_name,omitempty"`
	SystemName       string    `json:"system_name,omitempty"`

	// SavedAt is the wall-clock write time — but it is NOT persisted
	// (json:"-"). The envelope's ClockT0 is the single on-disk source of
	// that timestamp; SavedAt is DERIVED from it on every read (ReadHeader
	// / List / the import), so assigning this field has no on-disk effect.
	// It stays on Meta only as the browser's ready-to-render sort +
	// display value. (Pre-v0.26.0 the two were persisted side by side —
	// pure duplication; de-duplicated before release, ADR 0033.)
	SavedAt time.Time `json:"-"`
}

// Lane classifies a saves-directory entry by its reserved-filename
// namespace (ADR 0033 §D): named saves are written only by explicit
// Save-As; quicksave/autosave are managed lanes that never collide
// with them.
type Lane string

const (
	LaneNamed     Lane = "named"
	LaneQuicksave Lane = "quicksave"
	LaneAutosave  Lane = "autosave"
)

// QuicksaveID is the fixed quicksave-lane filename F5 always targets
// (ADR 0033 §D).
const QuicksaveID = "quicksave.json"

// autosaveIDs is the rotating autosave ring (ADR 0033 §E) — three
// fixed slots, oldest SavedAt overwritten.
var autosaveIDs = [3]string{"autosave-1.json", "autosave-2.json", "autosave-3.json"}

// ErrReservedLane is returned when a named-save operation (Overwrite,
// Rename) targets a reserved quicksave/autosave filename — the lanes
// are managed and never manually overwritable or renamable (ADR 0033
// §D/§F).
var ErrReservedLane = errors.New("save: reserved quicksave/autosave lane")

// SaveInfo is one saves-directory listing entry: the opaque filename
// (the stable ID every targeted operation takes), its parsed Meta,
// and which lane the filename falls in.
//
// Unreadable flags an entry whose header would not parse — a corrupt
// file, or one written by a newer build (a post-downgrade schema-v(N+1)
// save). List surfaces these rather than dropping them (ADR 0033 §C):
// silently hiding a file makes a directory that clearly holds saves
// read as empty, and — worse — a newer-build save would vanish on a
// downgrade with no trace. The browser renders them dimmed and
// non-loadable, with Note as the reason; SavedAt/Meta are zero.
type SaveInfo struct {
	ID         string
	Meta       Meta
	Lane       Lane
	Unreadable bool
	Note       string // human-facing reason when Unreadable (e.g. "newer version", "corrupt")
}

// SavesDir returns the saves-directory path,
// $XDG_STATE_HOME/terminal-space-program/saves/ — the sibling of the
// legacy DefaultPath save.json (which is retained only for the
// first-run import probe, ADR 0033 §G).
func SavesDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "saves"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "terminal-space-program", "saves"), nil
}

// header is the ReadHeader unmarshal target: the envelope minus the
// Payload. encoding/json still tokenizes the whole file, but the
// Payload is never decoded — no World hydration, no body-catalog hit.
type header struct {
	Version int   `json:"version"`
	ClockT0 int64 `json:"clock_t0"`
	Meta    *Meta `json:"meta"`
}

// readRawHeader parses the envelope header without any Meta
// derivation — the autosave ring uses the raw form to tell a
// Meta-less file (rotation victim) from a stamped one.
func readRawHeader(path string) (header, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return header{}, fmt.Errorf("read save: %w", err)
	}
	var h header
	if err := json.Unmarshal(data, &h); err != nil {
		return header{}, fmt.Errorf("parse save: %w", err)
	}
	if h.Version < 1 || h.Version > SchemaVersion {
		return header{}, fmt.Errorf("%w: got %d, want 1..%d", ErrSchemaMismatch, h.Version, SchemaVersion)
	}
	return h, nil
}

// ReadHeader returns the envelope's Meta without hydrating the
// Payload: no World rebuild, no body-catalog load or hash check (ADR
// 0033 §C — the browser lists N files this way). SavedAt is always
// derived from the envelope's ClockT0 (wall-clock save time — the
// single source of truth, never persisted on Meta itself), so a
// stamped and a Meta-less pre-v0.26 envelope order identically. The
// in-game date is unknowable from a Meta-less header and InGameEpoch
// stays zero — only a full Payload read (Load) can recover it.
func ReadHeader(path string) (Meta, error) {
	h, err := readRawHeader(path)
	if err != nil {
		return Meta{}, err
	}
	var m Meta
	if h.Meta != nil {
		m = *h.Meta
	}
	if h.ClockT0 != 0 {
		m.SavedAt = time.Unix(0, h.ClockT0).UTC()
	}
	return m, nil
}

// laneOf classifies a filename into its reserved-namespace lane.
func laneOf(id string) Lane {
	if id == QuicksaveID {
		return LaneQuicksave
	}
	for _, a := range autosaveIDs {
		if id == a {
			return LaneAutosave
		}
	}
	return LaneNamed
}

// validateID rejects anything that isn't a bare .json filename inside
// the saves directory — IDs come from List/WriteNamed and never carry
// path separators.
func validateID(id string) error {
	if id == "" || id != filepath.Base(id) || !strings.HasSuffix(id, ".json") {
		return fmt.Errorf("save: invalid save id %q", id)
	}
	return nil
}

// idPath resolves a validated ID inside the saves directory.
func idPath(id string) (string, error) {
	if err := validateID(id); err != nil {
		return "", err
	}
	dir, err := SavesDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, id), nil
}

// List scans the saves directory and header-parses every entry,
// newest SavedAt first (ties broken by filename for determinism). A
// missing directory lists as empty — first run, before any save
// exists. A .json file whose header will not parse (corrupt, or a
// newer-build schema version after a downgrade) is surfaced as an
// Unreadable entry rather than silently dropped (§C) — it sorts to the
// bottom on a zero SavedAt and the browser shows it dimmed and
// non-loadable. Only non-.json strays and tmpfiles are skipped.
func List() ([]SaveInfo, error) {
	dir, err := SavesDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan saves dir: %w", err)
	}
	var infos []SaveInfo
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue // tmpfiles (.json.tmp) and strays
		}
		m, err := ReadHeader(filepath.Join(dir, name))
		if err != nil {
			infos = append(infos, SaveInfo{
				ID:         name,
				Lane:       laneOf(name),
				Unreadable: true,
				Note:       unreadableNote(err),
			})
			continue
		}
		infos = append(infos, SaveInfo{ID: name, Meta: m, Lane: laneOf(name)})
	}
	sort.Slice(infos, func(i, j int) bool {
		ti, tj := infos[i].Meta.SavedAt, infos[j].Meta.SavedAt
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return infos[i].ID < infos[j].ID
	})
	return infos, nil
}

// unreadableNote classifies a header read/parse failure into a short,
// player-facing reason for the browser. A schema-range rejection means
// the file was written by a different (usually newer) build; a
// permission/IO failure means the bytes couldn't be read at all (not
// that they're bad); anything else is malformed content.
func unreadableNote(err error) string {
	switch {
	case errors.Is(err, ErrSchemaMismatch):
		return "newer version"
	case errors.Is(err, os.ErrPermission):
		return "no access"
	default:
		return "corrupt"
	}
}

// stampMeta builds the Meta header for a write happening now. name is
// empty for the reserved lanes (§D — quicksave/autosaves carry full
// metadata but no player name). The system column is the active
// vessel's own System (ADR 0015 — per-Vessel binding), falling back
// to the world-level viewed system when there is no active craft.
func stampMeta(w *sim.World, name string) *Meta {
	// SavedAt is intentionally not stamped here — it is derived from the
	// envelope ClockT0 on read (see Meta). InGameEpoch is the sim clock.
	m := &Meta{
		Name:        name,
		InGameEpoch: w.Clock.SimTime,
	}
	if c := w.ActiveCraft(); c != nil {
		m.ActiveVesselName = c.Name
		if c.SystemIdx >= 0 && c.SystemIdx < len(w.Systems) {
			m.SystemName = w.Systems[c.SystemIdx].Name
		}
	}
	if m.SystemName == "" && w.SystemIdx >= 0 && w.SystemIdx < len(w.Systems) {
		m.SystemName = w.Systems[w.SystemIdx].Name
	}
	return m
}

// mintCounter disambiguates same-nanosecond mints within a process;
// the timestamp component keeps IDs unique across processes.
var mintCounter atomic.Uint64

// mintID mints an opaque named-save filename (ADR 0033 §B). The
// "save-" prefix keeps the namespace disjoint from the reserved
// quicksave/autosave filenames by construction, so WriteNamed can
// never target a reserved lane. Duplicate display names are allowed
// and never deduped — every Save-As is a new file.
func mintID() string {
	return fmt.Sprintf("save-%d-%d.json", time.Now().UnixNano(), mintCounter.Add(1))
}

// WriteNamed writes w as a new named save (Save-As, ADR 0033 §D —
// the only writer of the named lane), minting an opaque filename and
// stamping full Meta. Atomic via the shared tmp+rename path.
func WriteNamed(w *sim.World, name string) (SaveInfo, error) {
	dir, err := SavesDir()
	if err != nil {
		return SaveInfo{}, err
	}
	meta := stampMeta(w, name)
	f, err := buildFile(w, meta)
	if err != nil {
		return SaveInfo{}, err
	}
	id := mintID()
	if err := writeFileAtomic(filepath.Join(dir, id), f); err != nil {
		return SaveInfo{}, err
	}
	// Populate the derived SavedAt in the returned entry from the
	// envelope ClockT0 just written, so the caller sees the same
	// wall-clock time List will read back (SavedAt is never persisted).
	meta.SavedAt = time.Unix(0, f.ClockT0).UTC()
	return SaveInfo{ID: id, Meta: *meta, Lane: LaneNamed}, nil
}

// Overwrite rewrites the existing named save id in place with w,
// preserving its Meta.Name and refreshing the volatile fields
// (SavedAt, InGameEpoch, ActiveVesselName, SystemName). The target
// must exist — Overwrite is a browser action on a listed row, never a
// silent create — and must not be a reserved lane (§D/§F).
func Overwrite(id string, w *sim.World) error {
	if laneOf(id) != LaneNamed {
		return fmt.Errorf("%w: %s", ErrReservedLane, id)
	}
	path, err := idPath(id)
	if err != nil {
		return err
	}
	prev, err := ReadHeader(path)
	if err != nil {
		return fmt.Errorf("overwrite target: %w", err)
	}
	meta := stampMeta(w, prev.Name)
	f, err := buildFile(w, meta)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, f)
}

// WriteQuicksave writes w to the fixed quicksave lane (F5, ADR 0033
// §D) — always quicksave.json, overwritten in place, never a named
// save. Full Meta is stamped minus a player name so the browser can
// still render the metadata columns.
func WriteQuicksave(w *sim.World) error {
	dir, err := SavesDir()
	if err != nil {
		return err
	}
	f, err := buildFile(w, stampMeta(w, ""))
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, QuicksaveID), f)
}

// WriteAutosave writes w into the rotating autosave ring (ADR 0033
// §E): a missing slot is filled before any rotation, otherwise the
// slot with the oldest ClockT0 is overwritten — and an unreadable ring
// file counts as the first victim (it is the least valuable thing in
// the ring, so it goes first).
func WriteAutosave(w *sim.World) error {
	dir, err := SavesDir()
	if err != nil {
		return err
	}
	f, err := buildFile(w, stampMeta(w, ""))
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dir, autosaveVictim(dir)), f)
}

// autosaveVictim picks the ring slot the next autosave overwrites:
// first missing slot, else unreadable slot, else oldest ClockT0 (the
// wall-clock write time — SavedAt is derived from it, so comparing the
// envelope field directly is the same ordering without a Meta derive).
func autosaveVictim(dir string) string {
	for _, id := range autosaveIDs {
		if _, err := os.Stat(filepath.Join(dir, id)); err != nil {
			return id
		}
	}
	victim := autosaveIDs[0]
	var oldest int64
	haveOldest := false
	for _, id := range autosaveIDs {
		h, err := readRawHeader(filepath.Join(dir, id))
		if err != nil {
			return id // unreadable ring file — evict first
		}
		if !haveOldest || h.ClockT0 < oldest {
			victim, oldest, haveOldest = id, h.ClockT0, true
		}
	}
	return victim
}

// rawFile mirrors File with the Payload held as raw bytes, for
// envelope-only rewrites (Rename, the legacy import) that must not
// perturb World state they never decoded.
type rawFile struct {
	Version         int             `json:"version"`
	Generator       string          `json:"generator"`
	ClockT0         int64           `json:"clock_t0"`
	BodyCatalogHash string          `json:"body_catalog_hash"`
	Meta            *Meta           `json:"meta,omitempty"`
	Payload         json.RawMessage `json:"payload"`
}

// LoadID fully hydrates the save id from the saves directory — the
// same validated Load path (schema range, catalog hash, migrations)
// as the legacy single slot.
func LoadID(id string) (*sim.World, error) {
	path, err := idPath(id)
	if err != nil {
		return nil, err
	}
	return Load(path)
}

// Delete removes the save id. Reserved lanes are deletable (ADR 0033
// §F — they are managed, not precious); the caller confirms
// destructive actions.
func Delete(id string) error {
	path, err := idPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("delete save: %w", err)
	}
	return nil
}

// Rename sets the save's display name — a pure Meta rewrite (ADR 0033
// §B): same filename, Payload bytes carried through untouched as raw
// JSON, ClockT0 carried through so the rename doesn't reorder the list
// (SavedAt derives from it, so ordering is preserved for free).
// Reserved lanes refuse (§F). A Meta-less legacy file gains an empty
// Meta on rename to hold the name; the in-game date stays unknowable
// from the header and is left zero.
func Rename(id, name string) error {
	if laneOf(id) != LaneNamed {
		return fmt.Errorf("%w: %s", ErrReservedLane, id)
	}
	path, err := idPath(id)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read save: %w", err)
	}
	var rf rawFile
	if err := json.Unmarshal(data, &rf); err != nil {
		return fmt.Errorf("parse save: %w", err)
	}
	if rf.Meta == nil {
		rf.Meta = &Meta{}
	}
	rf.Meta.Name = name
	out, err := json.MarshalIndent(rf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal save: %w", err)
	}
	return writeAtomic(path, out)
}
