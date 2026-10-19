# Security Policy

## Reporting a vulnerability

Please report security issues **privately** through GitHub's built-in
flow: go to the
[**Security** tab](https://github.com/jasonfen/terminal-space-program/security)
and click **"Report a vulnerability"**. This opens a private advisory
visible only to the maintainers. Please use it instead of a public
issue or pull request so the report isn't disclosed before a fix is
available.

Include the version (`terminal-space-program --version`), your OS,
whether you were playing solo or in a `--serve` session, and the steps
to reproduce. A minimal save file, overlay file, or the exact input you
sent over ssh is the most useful thing you can attach.

I aim to acknowledge a report within a few days. This is a hobby project
maintained in spare time, so please be patient on turnaround.

## Supported versions

Fixes land on the latest release only. There are no long-term support
branches: please upgrade to the newest tagged release before reporting.

| Version | Supported          |
| ------- | ------------------ |
| latest tagged release | :white_check_mark: |
| anything older | :x: |

## Scope / threat model

Terminal Space Program is a single static binary that runs in two ways.

### Solo play (the default)

Offline. The game makes no network connections and runs no server. The
attack surface is **untrusted input parsed from local files**:

- **Saves**: `$XDG_STATE_HOME/terminal-space-program/` (a versioned
  JSON envelope; `~/.local/state/...` when `XDG_STATE_HOME` is unset).
- **User overlays and settings** under
  `$XDG_CONFIG_HOME/terminal-space-program/` (`~/.config/...` when
  unset): body-catalog systems, textures, themes, ground stations,
  missions, vehicle loadouts and parts, VAB designs, and `settings.json`.
  These are meant to be shared between players, so treat them as
  untrusted.

### Multiplayer host (`--serve`)

`--serve` (or hosting from inside a running game) opens an **SSH
listener**, port 23234 by default, so friends can join from any
terminal. How access works:

- The SSH layer accepts any public key. A key's fingerprint is the
  player's identity.
- An unknown key gets a game only after entering a **one-time invite
  code** the host minted (`serve invite <handle>`). Enrolled keys resume
  their own persistent world on reconnect.
- The host can promote guests to admin (`serve promote`). Admins handle
  invites and removals, but can't promote anyone, remove another admin,
  or remove the host.
- Guest worlds, the roster, and the dock ledger live in the host's
  session directory, `$XDG_STATE_HOME/terminal-space-program/session/`.
  The server's SSH identity is
  `$XDG_STATE_HOME/terminal-space-program/ssh_host_ed25519_key`.

Hosting is designed for **a small group of friends on a private
network** (for example a Tailscale tailnet). Don't expose the port to
the open internet: invite codes are short enough to read out loud,
and the server is not hardened against strangers.

## What to report

In scope:

- A crafted save, overlay, or settings file that crashes the game, makes
  a panic escape recovery, grows memory without bound, writes or reads
  outside the game's own directories, or executes input as code.
- Over ssh, anything that lets a connection:
  - crash or hang the host, before or after enrollment;
  - get a game without a valid invite code, or take over another
    player's identity or world;
  - read or change another player's world, the host's own saves or
    settings, or files outside the session directory;
  - inject terminal control sequences into other players' screens
    (through handles, chat, or vessel names);
  - lose or duplicate another player's vessel through the docking or
    hand-over flow.

Out of scope:

- Anything that needs an already-compromised machine (an attacker who
  can already write to your home directory or run code as you).
- A denial of service from absurd but honest input you fed yourself.
- Running `--serve` on the open internet against the advice above.
- Things an enrolled friend can do within the game's rules (griefing is
  for the host to handle with kick and the roster).
- Issues in third-party dependencies that this project doesn't reach.
  Report those upstream.

## Dependencies

The game is built on the Charm TUI stack (Bubble Tea, Lip Gloss,
Bubbles). The multiplayer server uses `charmbracelet/wish` and
`golang.org/x/crypto/ssh`, so advisories in those are in scope whenever
the `--serve` path reaches the vulnerable code. If you spot a
known-vulnerable pin, a regular issue or PR to bump it is welcome.
