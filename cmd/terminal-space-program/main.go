package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/bodies"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/save"
	"github.com/jasonfen/terminal-space-program/internal/serve"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/version"
)

func main() {
	var (
		showVersion bool
		raw         rawFlags
		listSystems bool
		listBodies  bool
		listLoadout bool
		listSites   bool
		serveMode   bool
		servePort   int
	)
	flag.BoolVar(&showVersion, "version", false, "print version + commit and exit")
	flag.BoolVar(&showVersion, "v", false, "print version + commit and exit (shorthand)")
	flag.StringVar(&raw.system, "system", "", "star system to start in (e.g. Sol, Lumen)")
	flag.StringVar(&raw.body, "orbit", "", "body to orbit / launch from (ID or name)")
	flag.StringVar(&raw.body, "parent", "", "alias for --orbit")
	flag.StringVar(&raw.body, "body", "", "alias for --orbit")
	flag.StringVar(&raw.altitude, "altitude", "", "circular-orbit altitude, e.g. 400km or 400000m (default 500km)")
	flag.Float64Var(&raw.inclination, "inclination", 0, "orbit inclination in degrees")
	flag.BoolVar(&raw.retrograde, "retrograde", false, "spawn into a retrograde orbit")
	flag.BoolVar(&raw.launchpad, "launchpad", false, "spawn on the surface (launchpad) instead of in orbit")
	flag.StringVar(&raw.launchSite, "launch-site", "", "named launch site (KSC, Baikonur, Plesetsk, Equator, North-Pole)")
	flag.Float64Var(&raw.lat, "lat", 0, "launchpad latitude in degrees north (implies --launchpad)")
	flag.Float64Var(&raw.lon, "lon", 0, "launchpad longitude in degrees east (implies --launchpad)")
	flag.StringVar(&raw.loadout, "loadout", "", "craft loadout, e.g. Saturn-V or Kern-Stack (default S-IVB-1)")
	flag.BoolVar(&listSystems, "list-systems", false, "list available star systems and exit")
	flag.BoolVar(&listBodies, "list-bodies", false, "list bodies (honours --system) and exit")
	flag.BoolVar(&listLoadout, "list-loadouts", false, "list craft loadouts and exit")
	flag.BoolVar(&listSites, "list-launch-sites", false, "list named launch sites and exit")
	flag.BoolVar(&serveMode, "serve", false, "host a multiplayer session: play here and accept SSH guests (ADR 0034)")
	flag.IntVar(&servePort, "serve-port", serve.DefaultPort, "SSH listener port for --serve")
	flag.Parse()

	if showVersion {
		fmt.Printf("terminal-space-program %s (%s)\n", version.Version, version.Commit)
		return
	}

	// Record which flags the user actually passed, so buildScenario can tell
	// an explicit 0 (e.g. --lat 0) from an unset float.
	raw.set = map[string]bool{}
	flag.Visit(func(f *flag.Flag) { raw.set[f.Name] = true })

	// Load systems once (also surfaces user-overlay warnings before bubbletea
	// takes the screen) — reused for the --list-* helpers below.
	systems, sysWarnings, sysErr := bodies.LoadAllWithWarnings()
	if sysErr == nil {
		for _, w := range sysWarnings {
			fmt.Fprintf(os.Stderr, "terminal-space-program: skipping %s: %v\n", w.Path, w.Err)
		}
	}
	if _, warnings, err := render.LoadTheme(); err == nil {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "terminal-space-program: skipping theme %s: %v\n", w.Path, w.Err)
		}
	}
	// UI preferences (per-Chip visibility, ADR 0010). A missing file is the
	// common case and yields all-on defaults silently; a malformed file
	// degrades to defaults plus a warning here.
	if _, warnings := settings.Load(); len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "terminal-space-program: skipping settings %s: %v\n", w.Path, w.Err)
		}
	}
	// Loadout/parts catalog user overlay (ADR 0026). Layers any user
	// loadouts/parts (the XDG loadouts/ dir) onto the embedded catalog so
	// --list-loadouts and the spawn form reflect mods; a malformed file is
	// skipped with a warning. Must run before printLists + buildScenario so
	// modded loadouts are spawnable.
	for _, w := range spacecraft.LoadCatalogOverlay() {
		fmt.Fprintf(os.Stderr, "terminal-space-program: skipping loadout %s: %v\n", w.Path, w.Err)
	}
	// CommNet ground-station overlay (ADR 0027). Surfaces malformed user
	// ground_stations/*.json before bubbletea takes the screen; NewWorld
	// loads the merged ring into World.GroundStations.
	if _, warnings := sim.LoadGroundStationsWithWarnings(); len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "terminal-space-program: skipping ground station %s: %v\n", w.Path, w.Err)
		}
	}

	if listSystems || listBodies || listLoadout || listSites {
		printLists(systems, raw.system, listSystems, listBodies, listLoadout, listSites)
		return
	}

	scenario, err := buildScenario(raw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
		os.Exit(2)
	}

	// v0.26 (ADR 0033 §G): one-time migration of the legacy single-slot
	// save.json into the saves/ directory as a named save. Gated on a
	// settled-marker (not the mere existence of saves/, which an autosave
	// or a once-failed import can create), so a failed import retries next
	// run instead of hiding the legacy save forever. The legacy file stays
	// behind untouched as a downgrade safety net. Never fatal — on failure
	// the player just starts without the import.
	if info, imported, err := save.ImportLegacyIfNeeded(); err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: legacy save import skipped: %v\n", err)
	} else if imported {
		fmt.Fprintf(os.Stderr, "terminal-space-program: imported legacy save.json as %q\n", info.Meta.Name)
	}

	app, err := tui.New(scenario)
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
		os.Exit(1)
	}

	// v0.27 S1 (ADR 0034): --serve starts the embedded SSH listener next
	// to the host's own in-process game. Guests get fresh ephemeral
	// Worlds; the session ends for everyone when the host quits.
	var srv *serve.Server
	if serveMode {
		keyPath, err := serve.DefaultHostKeyPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
			os.Exit(1)
		}
		srv, err = serve.New(serve.Config{Addr: fmt.Sprintf(":%d", servePort), HostKeyPath: keyPath})
		if err != nil {
			fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
			os.Exit(1)
		}
		go func() {
			if err := srv.Serve(); err != nil {
				fmt.Fprintf(os.Stderr, "terminal-space-program: ssh listener: %v\n", err)
			}
		}()
		fmt.Fprintf(os.Stderr, "terminal-space-program: serving SSH guests on %s (host key %s)\n", srv.Addr(), keyPath)
	}

	p := tea.NewProgram(app, tea.WithAltScreen(), tea.WithMouseAllMotion())
	_, runErr := p.Run()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", runErr)
		os.Exit(1)
	}
}

// printLists handles the --list-* discovery flags. --list-bodies scopes to
// --system when given, else lists every system's bodies.
func printLists(systems []bodies.System, systemFilter string, sys, bodiesL, loadouts, sites bool) {
	if sys {
		fmt.Println("Systems:")
		for _, s := range systems {
			fmt.Printf("  %-8s (%d bodies)\n", s.Name, len(s.Bodies))
		}
	}
	if bodiesL {
		fmt.Println("Bodies:")
		for _, s := range systems {
			if systemFilter != "" && !strings.EqualFold(s.Name, systemFilter) {
				continue
			}
			fmt.Printf("  [%s]\n", s.Name)
			for _, b := range s.Bodies {
				parent := b.ParentID
				if parent == "" {
					parent = "—"
				}
				fmt.Printf("    %-12s %-14s parent=%s\n", b.ID, b.EnglishName, parent)
			}
		}
	}
	if loadouts {
		// Reflect the merged catalog (embedded + user overlay, ADR 0026) so a
		// modder can confirm their loadouts/*.json loaded: each loadout with
		// its resolved stage names, then the full parts catalog.
		fmt.Println("Loadouts:")
		for _, id := range spacecraft.LoadoutOrder {
			l := spacecraft.Loadouts[id]
			names := make([]string, len(l.Stages))
			for i, s := range l.Stages {
				names[i] = s.Name
			}
			fmt.Printf("  %-16s %-16s [%s]\n", id, l.Name, strings.Join(names, ", "))
		}
		fmt.Println("Parts:")
		pids := make([]string, 0, len(spacecraft.StageCatalog))
		for pid := range spacecraft.StageCatalog {
			pids = append(pids, pid)
		}
		sort.Strings(pids)
		for _, pid := range pids {
			fmt.Printf("  %-16s %s\n", pid, spacecraft.StageCatalog[pid].Name)
		}
	}
	if sites {
		fmt.Println("Launch sites:")
		for _, s := range sim.LaunchSites {
			fmt.Printf("  %-12s %-28s %7.3f°N  %8.3f°E\n", s.Key, s.Name, s.LatitudeDeg, s.LongitudeEastDeg)
		}
	}
}
