package screens

import (
	"strings"
)

// BossShell is the "boss key" fake terminal. A single global keypress
// (the backtick — see app.go screenBoss) swaps the whole screen to this
// convincing developer shell, and swaps back on exit/logout/ctrl+d. It is
// deliberately NOT space-game themed: it should read as a plain login
// shell so a passer-by sees "real work."
//
// The screen owns a one-line input buffer plus a scrollback of already-
// rendered output rows. Typing + Enter runs a canned fake command and
// appends its output. exit / logout / ctrl+d ask the App (via the returned
// BossAction) to restore the previous screen.
type BossShell struct {
	theme  Theme
	prompt string   // e.g. "user@workstation:~/projects/acme-api$ "
	input  string   // current (unsubmitted) command line
	scroll []string // rendered scrollback, one terminal row per entry
}

// BossAction is the outcome of a keypress, consumed by the App.
type BossAction int

const (
	BossActionNone BossAction = iota // stay in the shell
	BossActionExit                   // exit/logout/ctrl+d — return to the game
)

const bossPrompt = "user@workstation:~/projects/acme-api$ "

func NewBossShell(th Theme) *BossShell {
	b := &BossShell{theme: th, prompt: bossPrompt}
	b.Reset()
	return b
}

// Reset restores the "lived-in" initial scrollback and clears the input
// line. The App calls it each time the boss key opens the screen so it
// always looks freshly attached rather than showing the previous session's
// typed history (or a stale `clear`).
func (b *BossShell) Reset() {
	b.input = ""
	b.scroll = initialBossScrollback()
}

// initialBossScrollback seeds the screen so it looks like an existing
// session, not a cold boot: a last-login line plus a couple of already-run
// commands with their output.
func initialBossScrollback() []string {
	return []string{
		"Last login: Mon Jun  1 09:14:07 2026 from 10.0.0.42",
		bossPrompt + "git pull",
		"Already up to date.",
		bossPrompt + "npm run build",
		"",
		"> acme-api@2.4.1 build",
		"> tsc -p tsconfig.json",
		"",
		"Compiled successfully in 4.21s.",
	}
}

// HandleKey accumulates the typed line and runs canned commands on Enter.
// Returns BossActionExit when the player types exit/logout or presses
// ctrl+d; every other key returns BossActionNone. The key strings match
// bubbletea's m.String() ("enter", "backspace", "ctrl+d", " " for space,
// single runes for typed characters).
func (b *BossShell) HandleKey(key string) BossAction {
	switch key {
	case "ctrl+d":
		// Empty-line ctrl+d is the classic shell logout. We treat it as
		// logout regardless of buffer contents (simpler, still believable).
		return BossActionExit
	case "enter":
		return b.submit()
	case "backspace":
		if r := []rune(b.input); len(r) > 0 {
			// Trim one UTF-8 rune, not one byte, so multibyte input
			// doesn't corrupt the line.
			b.input = string(r[:len(r)-1])
		}
		return BossActionNone
	case " ":
		b.input += " "
		return BossActionNone
	default:
		// Only append genuine single-character text input. Named keys
		// ("up", "tab", "ctrl+x", "f1"...) are ignored so they can't
		// pollute the command line.
		if isSingleRune(key) {
			b.input += key
		}
		return BossActionNone
	}
}

// isSingleRune reports whether s is exactly one rune. Used to distinguish
// a typed character from a named key like "tab" or "ctrl+x".
func isSingleRune(s string) bool {
	n := 0
	for range s {
		n++
		if n > 1 {
			return false
		}
	}
	return n == 1
}

// submit echoes the prompt+line into scrollback, runs the canned command,
// appends its output, and clears the input line. Returns BossActionExit
// for exit/logout.
func (b *BossShell) submit() BossAction {
	line := b.input
	b.input = ""
	// Echo the entered command as a terminal would.
	b.scroll = append(b.scroll, b.prompt+line)

	fields := strings.Fields(line)
	if len(fields) == 0 {
		return BossActionNone // bare Enter just adds a fresh prompt row
	}
	cmd := fields[0]

	switch cmd {
	case "exit", "logout":
		b.scroll = append(b.scroll, "logout")
		return BossActionExit
	case "clear":
		b.scroll = nil // wipe scrollback like a real `clear`
		return BossActionNone
	}

	out, ok := bossCommandOutput(cmd, fields)
	if !ok {
		b.scroll = append(b.scroll, cmd+": command not found")
		return BossActionNone
	}
	b.scroll = append(b.scroll, out...)
	return BossActionNone
}

// bossCommandOutput returns the canned output rows for a recognized command
// (keyed on argv[0]) and ok=false for an unknown command so the caller can
// print the shell's "command not found". Output is fully static — no real
// filesystem or process access — so it is safe, deterministic, and
// testable. `clear`, `exit`, and `logout` are handled by the caller (they
// mutate scrollback / signal exit) and are not here.
func bossCommandOutput(cmd string, argv []string) ([]string, bool) {
	switch cmd {
	case "ls":
		return []string{
			"Dockerfile        README.md       node_modules  package.json",
			"docs              jest.config.js  src           tsconfig.json",
		}, true
	case "pwd":
		return []string{"/home/user/projects/acme-api"}, true
	case "whoami":
		return []string{"user"}, true
	case "echo":
		// Echo back the args (drop argv[0]).
		return []string{strings.Join(argv[1:], " ")}, true
	case "cat":
		// Canned content regardless of the requested file — believable for
		// a quick peek and avoids any real file access.
		return []string{
			"{",
			`  "name": "acme-api",`,
			`  "version": "2.4.1",`,
			`  "private": true,`,
			`  "scripts": {`,
			`    "build": "tsc -p tsconfig.json",`,
			`    "test": "jest",`,
			`    "start": "node dist/index.js"`,
			"  }",
			"}",
		}, true
	case "git":
		return bossGit(argv), true
	case "npm":
		return bossNpm(argv), true
	case "make":
		return []string{
			"go build -o bin/acme-api ./cmd/server",
			"go vet ./...",
			"build complete: bin/acme-api",
		}, true
	case "ps":
		return []string{
			"  PID TTY          TIME CMD",
			" 2114 pts/0    00:00:00 bash",
			" 4582 pts/0    00:00:03 node",
			" 4733 pts/0    00:00:00 ps",
		}, true
	case "top", "htop":
		return []string{
			"top - 09:21:44 up 3 days,  2:11,  1 user,  load average: 0.42, 0.51, 0.55",
			"Tasks: 213 total,   1 running, 212 sleeping",
			"%Cpu(s):  4.7 us,  1.2 sy,  0.0 ni, 93.6 id",
			"MiB Mem :  16003.1 total,   2841.0 free,   6120.4 used",
			"",
			"  PID USER      PR  NI    VIRT    RES   %CPU  %MEM  COMMAND",
			" 4582 user      20   0  982140 142880    3.7   0.9  node",
			" 1190 user      20   0  712044  88112    0.7   0.5  tmux",
		}, true
	case "help":
		return []string{
			"GNU bash, version 5.2.21",
			"These shell commands are defined internally.  Type `help name'.",
			" cd        exit      jobs      pwd       umask",
			" echo      export    kill      read      unset",
		}, true
	default:
		return nil, false
	}
}

// bossGit returns canned output for the `git` subcommands the fake shell
// recognizes; anything unrecognized falls through to a clean-tree status.
func bossGit(argv []string) []string {
	sub := ""
	if len(argv) > 1 {
		sub = argv[1]
	}
	switch sub {
	case "status", "":
		return []string{
			"On branch feature/rate-limiter",
			"Your branch is up to date with 'origin/feature/rate-limiter'.",
			"",
			"Changes not staged for commit:",
			`  (use "git add <file>..." to update what will be committed)`,
			"\tmodified:   src/middleware/rateLimit.ts",
			"\tmodified:   src/server.ts",
			"",
			"Untracked files:",
			"\tsrc/middleware/rateLimit.test.ts",
			"",
			`no changes added to commit (use "git add" and/or "git commit -a")`,
		}
	case "log":
		return []string{
			"commit 9f3a1c2  (HEAD -> feature/rate-limiter)",
			"Author: user <user@workstation>",
			"Date:   Mon Jun 1 09:02:11 2026 +0000",
			"",
			"    Add token-bucket rate limiter middleware",
			"",
			"commit 4b71e08",
			"Author: user <user@workstation>",
			"Date:   Fri May 29 17:48:30 2026 +0000",
			"",
			"    Wire request-id into structured logs",
		}
	default:
		return []string{
			"On branch feature/rate-limiter",
			"nothing to commit, working tree clean",
		}
	}
}

// bossNpm returns canned output for the `npm` subcommands the fake shell
// recognizes.
func bossNpm(argv []string) []string {
	sub := ""
	if len(argv) > 1 {
		sub = argv[1]
	}
	switch sub {
	case "test":
		return []string{
			"> acme-api@2.4.1 test",
			"> jest",
			"",
			"PASS  src/middleware/rateLimit.test.ts",
			"PASS  src/server.test.ts",
			"",
			"Test Suites: 2 passed, 2 total",
			"Tests:       18 passed, 18 total",
			"Snapshots:   0 total",
			"Time:        3.114 s",
			"Ran all test suites.",
		}
	case "run", "build":
		return []string{
			"> acme-api@2.4.1 build",
			"> tsc -p tsconfig.json",
			"",
			"Compiled successfully.",
		}
	case "install", "i", "ci":
		return []string{
			"added 1 package, and audited 642 packages in 1s",
			"found 0 vulnerabilities",
		}
	default:
		return []string{"npm <command> -- see `npm help`"}
	}
}

// Render draws the fake terminal sized to width x height. The current input
// line gets a block cursor; the whole buffer is bottom-anchored and clipped
// to the last `height` rows so a long session scrolls like a real terminal.
// Short buffers are top-padded so the frame fully covers the game screen
// underneath. Rows are per-rune truncated to width so output never wraps and
// breaks the illusion of a fixed-width terminal.
func (b *BossShell) Render(width, height int) string {
	if height < 1 {
		height = 1
	}
	lines := make([]string, 0, len(b.scroll)+1)
	lines = append(lines, b.scroll...)
	lines = append(lines, b.prompt+b.input+"█")

	// Clip to the last `height` rows (terminal-style scroll).
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	// Truncate each row to width so long output never wraps.
	if width > 0 {
		for i, ln := range lines {
			if r := []rune(ln); len(r) > width {
				lines[i] = string(r[:width])
			}
		}
	}
	// Top-pad so the rendered block always occupies the full height and no
	// stale game frame shows through.
	for len(lines) < height {
		lines = append([]string{""}, lines...)
	}
	return strings.Join(lines, "\n")
}
