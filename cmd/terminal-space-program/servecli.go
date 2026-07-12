package main

import (
	"fmt"
	"os"

	"github.com/jasonfen/terminal-space-program/internal/serve"
	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
)

// runServeCLI handles the `terminal-space-program serve …` subcommands
// (v0.27 S3, ADR 0034). Today that's invite-minting; the Session
// screen (S6) grows the in-game frontend over the same store.
//
//	terminal-space-program serve invite <handle>
func runServeCLI(args []string) {
	if len(args) == 2 && args[0] == "invite" {
		dir, err := sessiondir.DefaultDir()
		if err != nil {
			fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
			os.Exit(1)
		}
		store, err := sessiondir.Open(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
			os.Exit(1)
		}
		inv, err := store.MintInvite(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "terminal-space-program: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("invite minted for %s: %s\n", inv.Handle, inv.Code)
		fmt.Printf("one-time — they join with:  ssh -p %d <your-host>\n", serve.DefaultPort)
		return
	}
	fmt.Fprintln(os.Stderr, "usage: terminal-space-program serve invite <handle>")
	os.Exit(2)
}
