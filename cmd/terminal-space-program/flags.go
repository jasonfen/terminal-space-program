package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// rawFlags holds the parsed command-line values that shape the starting
// scenario, plus the set of flags the user explicitly passed (so a 0 value
// can be told apart from "unset" for the float flags). Kept separate from
// flag registration so buildScenario is unit-testable without os.Args.
type rawFlags struct {
	system      string
	body        string // --orbit / --parent / --body
	altitude    string
	loadout     string
	launchSite  string
	inclination float64
	lat         float64
	lon         float64
	retrograde  bool
	launchpad   bool

	set map[string]bool // flag names the user actually provided
}

// buildScenario turns parsed flags into a *sim.StartScenario, or nil when no
// scenario flag was given (→ the default start). Surface placement
// (--launchpad/--launch-site/--lat/--lon) and orbital placement
// (--altitude/--inclination/--retrograde) are mutually exclusive. Returns a
// descriptive error on a conflict or an unknown launch site.
func buildScenario(r rawFlags) (*sim.StartScenario, error) {
	latSet, lonSet := r.set["lat"], r.set["lon"]
	orbitalSet := r.set["altitude"] || r.set["inclination"] || r.set["retrograde"]
	surfaceSet := r.launchpad || r.launchSite != "" || latSet || lonSet
	anySet := orbitalSet || surfaceSet ||
		r.system != "" || r.body != "" || r.loadout != ""
	if !anySet {
		return nil, nil // no scenario flags → standard default start
	}
	if orbitalSet && surfaceSet {
		return nil, fmt.Errorf("orbital flags (--altitude/--inclination/--retrograde) can't be combined with surface flags (--launchpad/--launch-site/--lat/--lon)")
	}

	s := &sim.StartScenario{
		SystemName: r.system,
		BodyID:     r.body,
		Loadout:    r.loadout,
	}

	if surfaceSet {
		s.Surface = true
		switch {
		case r.launchSite != "":
			if latSet || lonSet {
				return nil, fmt.Errorf("--launch-site can't be combined with --lat/--lon")
			}
			site, ok := sim.LaunchSiteByName(r.launchSite)
			if !ok {
				return nil, fmt.Errorf("unknown launch site %q (have: %s)", r.launchSite, strings.Join(launchSiteKeys(), ", "))
			}
			s.LatDeg, s.LonDeg = site.LatitudeDeg, site.LongitudeEastDeg
		case latSet || lonSet:
			// Numeric site; a missing component defaults to 0 (equator /
			// prime meridian).
			s.LatDeg, s.LonDeg = r.lat, r.lon
		default:
			// --launchpad alone → the form's per-system default site (KSC; Lumen is equatorial), so a bare
			// --launchpad lands somewhere sensible rather than the equator.
			site := sim.DefaultLaunchSite(r.system)
			s.LatDeg = site.LatitudeDeg
			s.LonDeg = site.LongitudeEastDeg
		}
		return s, nil
	}

	// Orbital placement (also the path when only --system/--body/--loadout
	// are given — a plain orbital spawn at the default altitude).
	if r.altitude == sim.FrozenPresetKey {
		s.PresetKey = r.altitude
	} else if r.altitude != "" {
		m, err := parseDistanceM(r.altitude)
		if err != nil {
			return nil, fmt.Errorf("--altitude: %w", err)
		}
		s.AltitudeM = m
	}
	s.InclDeg = r.inclination
	s.Retrograde = r.retrograde
	return s, nil
}

// startAltitudeClampNote reports whether the CLI's orbital scenario s will
// have its altitude moved by the target body's Orbit Band (ADR 0044 §5) —
// the same clamp ApplyStartScenario's SpawnCraft call applies — so main can
// print the move before the TUI takes the screen. Returns "" for a surface
// scenario (the band never applies there); when the target can't be
// resolved (ApplyStartScenario will surface that error itself once the app
// actually starts); when the altitude is already in-band; and — on
// purpose — for a body with no legal orbit altitude at all (e.g.
// "--orbit phobos"). That last case is a refusal, not a move: SpawnCraft's
// own error ("Mars owns everything outside Phobos's surface...") is the
// single message it should produce, surfaced through the normal
// tui.New/main error path rather than echoed here under a misleading
// "altitude" prefix.
//
// This mirrors SpawnCraft's own default (unset/non-positive AltitudeM ->
// sim.DefaultOrbitAltitudeM) so the reported note matches what actually
// gets spawned, even when --altitude was never passed.
func startAltitudeClampNote(systems []bodies.System, s *sim.StartScenario) string {
	if s == nil || s.Surface || s.PresetKey != "" {
		return ""
	}
	sysIdx, body, err := sim.ResolveStartTarget(systems, *s)
	if err != nil {
		return ""
	}
	alt := s.AltitudeM
	if alt <= 0 {
		alt = sim.DefaultOrbitAltitudeM
	}
	_, note, ok := sim.ClampToOrbitBand(systems[sysIdx], body, alt)
	if !ok {
		return ""
	}
	return note
}

// checkStartPreset refuses "--altitude frozen" at a body with no such preset
// (ADR 0065), naming the body, before the TUI takes the screen.
func checkStartPreset(systems []bodies.System, s *sim.StartScenario) error {
	if s == nil || s.PresetKey == "" {
		return nil
	}
	sysIdx, body, err := sim.ResolveStartTarget(systems, *s)
	if err != nil {
		return nil // ApplyStartScenario surfaces the same error itself
	}
	if _, ok := sim.OrbitPresetByKey(systems[sysIdx], body, s.PresetKey); !ok {
		return fmt.Errorf("no preset %q at %s", s.PresetKey, body.EnglishName)
	}
	return nil
}

// parseDistanceM parses an altitude with an optional unit suffix: "400km",
// "400000m", or a bare number treated as kilometres. Returns metres.
func parseDistanceM(s string) (float64, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	mult := 1000.0 // bare number → kilometres
	switch {
	case strings.HasSuffix(s, "km"):
		s, mult = strings.TrimSuffix(s, "km"), 1000
	case strings.HasSuffix(s, "m"):
		s, mult = strings.TrimSuffix(s, "m"), 1
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid distance %q", s)
	}
	if val < 0 {
		return 0, fmt.Errorf("altitude must be non-negative")
	}
	return val * mult, nil
}

// validateResetFleet guards --reset-fleet. It is a serve-time fleet
// administration action — meaningless without a session to reset — so
// it is refused without --serve rather than silently ignored. It is
// also refused alongside scenario flags: the reset places the host's
// vessel on ring slot 0 (the default fresh-start seed), and a custom
// scenario start would put the host's craft somewhere else entirely,
// silently breaking the "everyone on the same ring" guarantee.
func validateResetFleet(resetFleet, serveMode, hasScenario bool) error {
	if !resetFleet {
		return nil
	}
	if !serveMode {
		return fmt.Errorf("--reset-fleet requires --serve (it resets the multiplayer session's fleet at server startup)")
	}
	if hasScenario {
		return fmt.Errorf("--reset-fleet can't be combined with scenario flags — the host's vessel is placed on the reset ring's default slot")
	}
	return nil
}

// launchSiteKeys lists the short CLI tokens for the named launch sites.
func launchSiteKeys() []string {
	keys := make([]string, len(sim.LaunchSites))
	for i, s := range sim.LaunchSites {
		keys[i] = s.Key
	}
	return keys
}

// validateLoad refuses --load beside the spawn flags or --reset-fleet: a save
// already fixes the start, so the other flags would be silently ignored.
func validateLoad(load string, hasScenario, resetFleet bool) error {
	if load == "" {
		return nil
	}
	if hasScenario {
		return fmt.Errorf("--load can't be combined with the spawn flags (--system/--orbit/--altitude/--inclination/--retrograde/--launchpad/--launch-site/--lat/--lon/--loadout)")
	}
	if resetFleet {
		return fmt.Errorf("--load can't be combined with --reset-fleet")
	}
	return nil
}

// resolveLoad maps a --load argument to a saves-directory id or a file path.
// A save's display name (as the Load menu shows it) wins over a path; an
// ambiguous or unknown argument errors, listing what matched or what exists.
func resolveLoad(arg string, infos []save.SaveInfo) (id, path string, err error) {
	names := screens.SaveDisplayNames(infos)
	var hits []string
	for i, n := range names {
		if n == arg {
			hits = append(hits, fmt.Sprintf("%s (%s, %s)", n, infos[i].ID, infos[i].Meta.SavedAt.Local().Format("Jan 2 15:04")))
			id = infos[i].ID
		}
	}
	if len(hits) > 1 {
		return "", "", fmt.Errorf("--load %q matches %d saves, pass a file path to pick one:\n  %s", arg, len(hits), strings.Join(hits, "\n  "))
	}
	if len(hits) == 1 {
		return id, "", nil
	}
	if st, e := os.Stat(arg); e == nil && !st.IsDir() {
		return "", arg, nil
	}
	if len(names) == 0 {
		return "", "", fmt.Errorf("--load %q: no such save or file, and there are no saves", arg)
	}
	return "", "", fmt.Errorf("--load %q: no such save or file; saves are:\n  %s", arg, strings.Join(names, "\n  "))
}
