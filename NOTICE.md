# NOTICE

`terminal-space-program` incorporates or is inspired by the following third-party work:

## Code lifted (with attribution, MIT-licensed)

- **[github.com/furan917/go-solar-system](https://github.com/furan917/go-solar-system)** —
  architectural foundation. Specifically: the `Calculator` interface and its
  `SolarSystemCalculator` / `GenericCalculator` / `ExactCalculator` implementations,
  the `CelestialBody` struct with full Keplerian orbital elements, and the
  bundled system data (Alpha Centauri, TRAPPIST-1, Kepler-452; `sol.json` is
  original to this repo). Lifted at upstream commit
  [`e632e6e`](https://github.com/furan917/go-solar-system/commit/e632e6e11729529fb13e0120ef1186c89b8f0824).
  Licensed MIT © 2025 Francis. MIT header preserved on each lifted file.

## Design / visual inspiration (no code lifted)

- **[github.com/Cladamos/solcl](https://github.com/Cladamos/solcl)** — Bubble Tea
  MVC structure and drawille braille rendering aesthetic.
- **[github.com/charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea)** —
  TUI framework (runtime dependency).
- **[github.com/charmbracelet/lipgloss](https://github.com/charmbracelet/lipgloss)** —
  terminal styling (runtime dependency).
- **`jellyshell`** — contextual help footer pattern.

## Original work

Everything under `internal/physics/`, `internal/spacecraft/`, `internal/planner/`,
`internal/tui/`, `internal/sim/`, and `cmd/` is original to this repository,
licensed MIT © jasonfen.
