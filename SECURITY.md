# Security Policy

## Reporting a vulnerability

Please report security issues **privately** through GitHub's built-in
flow: go to the
[**Security** tab](https://github.com/jasonfen/terminal-space-program/security)
and click **"Report a vulnerability"**. This opens a private advisory
visible only to the maintainers — please use it instead of a public
issue or pull request so the report isn't disclosed before a fix is
available.

Include the version (`terminal-space-program --version`), your OS, and
the steps to reproduce. A minimal save file or `systems/*.json` overlay
that triggers the issue is the most useful thing you can attach.

I aim to acknowledge a report within a few days. This is a hobby project
maintained in spare time, so please be patient on turnaround.

## Supported versions

Fixes land on the latest release only. There are no long-term support
branches — please upgrade to the newest tagged release before reporting.

| Version | Supported          |
| ------- | ------------------ |
| latest release (v0.17.x) | :white_check_mark: |
| anything older | :x: |

## Scope / threat model

Terminal Space Program is an **offline, single static binary**. It makes
no network connections, runs no server, and has no authentication,
accounts, or remote surface. That sharply limits what "vulnerability"
means here. The realistic attack surface is **untrusted input parsed
from local files**:

- **Save files** — `save.json` under
  `$XDG_STATE_HOME/terminal-space-program/` (a versioned JSON envelope).
- **Body-catalog overlays** — user `systems/*.json` files loaded from
  `$XDG_CONFIG_HOME` that override or extend the embedded body catalog.

In-scope reports are things like a crafted save or system-overlay file
that causes a crash, a panic that escapes recovery, unbounded memory
growth, a path-traversal or arbitrary-file-write during load/save, or
any code path that would execute attacker-controlled input. If you can
make the binary do something it shouldn't by feeding it a file, that's
worth reporting.

**Out of scope:** anything requiring an already-compromised machine
(e.g. an attacker who can already write to your home directory or run
arbitrary code), denial-of-service from absurd-but-honest inputs you fed
yourself, or issues in third-party dependencies (report those upstream;
flag them here only if this project's usage is what makes them
exploitable).

## Dependencies

The binary is built from a small, pinned dependency set (see `go.mod` —
primarily the Charm TUI stack). Dependency advisories are tracked via
GitHub's Dependabot alerts; if you spot a known-vulnerable pin, opening
a regular issue or PR to bump it is welcome.
