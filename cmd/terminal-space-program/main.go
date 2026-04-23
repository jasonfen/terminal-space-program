package main

import (
	"fmt"
	"os"

	"github.com/jasonfen/terminal-space-program/internal/version"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("terminal-space-program %s (%s)\n", version.Version, version.Commit)
		return
	}
	fmt.Fprintln(os.Stderr, "terminal-space-program: v0 scaffold, TUI not yet wired up.")
	fmt.Fprintln(os.Stderr, "See https://github.com/jasonfen/terminal-space-program for roadmap.")
	os.Exit(1)
}
