package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jasonfen/terminal-space-program/internal/serve"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
)

const serveUsage = `usage:
  terminal-space-program serve invite <handle>
  terminal-space-program serve promote <handle>
  terminal-space-program serve demote <handle>
  terminal-space-program serve roster`

// runServeCLI handles the `terminal-space-program serve …` subcommands
// (v0.27 S3, ADR 0034; promote/demote/roster #242). They administer the
// session store on the host's own box, so they exercise host authority.
func runServeCLI(args []string) {
	dir, err := sessiondir.DefaultDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
		os.Exit(1)
	}
	os.Exit(serveCLI(args, dir, os.Stdout, os.Stderr))
}

// serveCLI runs one subcommand against the session dir and returns the
// process exit code.
func serveCLI(args []string, dir string, stdout, stderr io.Writer) int {
	fail := func(err error) int {
		fmt.Fprintf(stderr, "terminal-space-program: %v\n", err)
		return 1
	}
	usage := func() int {
		fmt.Fprintln(stderr, serveUsage)
		return 2
	}
	if len(args) == 0 {
		return usage()
	}
	switch {
	case args[0] == "invite" && len(args) == 2,
		(args[0] == "promote" || args[0] == "demote") && len(args) == 2,
		args[0] == "roster" && len(args) == 1:
	default:
		return usage()
	}
	store, err := sessiondir.Open(dir)
	if err != nil {
		return fail(err)
	}
	switch args[0] {
	case "invite":
		inv, err := store.MintInvite(args[1])
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "invite minted for %s: %s\n", inv.Handle, inv.Code)
		fmt.Fprintf(stdout, "one-time, they join with:  ssh -p %d <your-host>\n", serve.DefaultPort)
	case "roster":
		m, err := store.Meta()
		if err != nil {
			return fail(err)
		}
		w := 6
		for _, p := range m.Roster {
			if len(p.Handle) > w {
				w = len(p.Handle)
			}
		}
		for _, p := range m.Roster {
			fmt.Fprintf(stdout, "%s  %s\n", p.Handle+strings.Repeat(" ", w-len(p.Handle)), p.Role)
		}
	case "promote", "demote":
		m, err := store.Meta()
		if err != nil {
			return fail(err)
		}
		p, err := resolveHandle(m.Roster, args[1])
		if err != nil {
			return fail(err)
		}
		if p.Role == sessiondir.RoleHost {
			return fail(fmt.Errorf("%s is the host: the host role can't be changed", p.Handle))
		}
		want := sessiondir.RoleAdmin
		if args[0] == "promote" {
			err = store.PromoteAdmin(p.Fingerprint)
		} else {
			want = sessiondir.RoleGuest
			err = store.DemoteAdmin(p.Fingerprint)
		}
		if err != nil {
			return fail(err)
		}
		fmt.Fprintf(stdout, "%s is now %s\n", p.Handle, want)
	}
	return 0
}

// resolveHandle finds the one roster entry whose handle matches
// case-insensitively. Unknown or ambiguous handles error rather than
// guess (#242).
func resolveHandle(roster []sessiondir.Player, handle string) (sessiondir.Player, error) {
	handle = strings.TrimSpace(handle)
	var hits []sessiondir.Player
	for _, p := range roster {
		if strings.EqualFold(p.Handle, handle) {
			hits = append(hits, p)
		}
	}
	switch len(hits) {
	case 0:
		return sessiondir.Player{}, fmt.Errorf("no player with handle %q (see: serve roster)", handle)
	case 1:
		return hits[0], nil
	}
	return sessiondir.Player{}, fmt.Errorf("handle %q is ambiguous: %d players match, refusing to guess", handle, len(hits))
}
