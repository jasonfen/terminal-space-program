// Package serve hosts the embedded SSH front door for multiplayer
// sessions (ADR 0034 addendum, v0.27 S1). `--serve` starts a wish
// listener next to the host's in-process game; every inbound SSH
// connection gets a fresh, ephemeral single-player World running in
// this process. No identity, no persistence, no shared store yet —
// those are later slices. Any public key is accepted for now;
// enrollment gates arrive with S3.
package serve

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	"github.com/charmbracelet/wish/activeterm"
	bm "github.com/charmbracelet/wish/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// DefaultPort is the SSH listener port when --serve-port isn't given.
const DefaultPort = 23234

// Config shapes a Server. Addr is a listen address ("[host]:port";
// use port 0 to let the OS pick — Addr() reports the bound address).
// HostKeyPath locates the server's ed25519 identity; a missing key is
// generated there on first start.
type Config struct {
	Addr        string
	HostKeyPath string
}

// Server is a running (or startable) SSH listener whose sessions each
// run their own game. The listener is bound in New so port conflicts
// surface before the host's own TUI takes the screen.
type Server struct {
	ssh *ssh.Server
	ln  net.Listener
}

// DefaultHostKeyPath returns the per-host SSH identity path, sibling
// to the save state: $XDG_STATE_HOME/terminal-space-program/
// ssh_host_ed25519_key, falling back to ~/.local/state (same
// resolution as save.DefaultPath).
func DefaultHostKeyPath() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "terminal-space-program", "ssh_host_ed25519_key"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".local", "state", "terminal-space-program", "ssh_host_ed25519_key"), nil
}

// New binds cfg.Addr and prepares the wish server. The host key is
// created at cfg.HostKeyPath if absent (ed25519). Serve must be called
// to start accepting sessions.
func New(cfg Config) (*Server, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.HostKeyPath), 0o755); err != nil {
		return nil, fmt.Errorf("serve: host key dir: %w", err)
	}
	s, err := wish.NewServer(
		wish.WithAddress(cfg.Addr),
		wish.WithHostKeyPath(cfg.HostKeyPath),
		// S1: every key is welcome — sessions are ephemeral and hold no
		// identity. S3 replaces this with roster-enrollment checks.
		wish.WithPublicKeyAuth(func(ssh.Context, ssh.PublicKey) bool { return true }),
		wish.WithMiddleware(
			bm.Middleware(sessionHandler),
			activeterm.Middleware(), // require a PTY before handing off to bubbletea
		),
	)
	if err != nil {
		return nil, fmt.Errorf("serve: %w", err)
	}
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("serve: listen %s: %w", cfg.Addr, err)
	}
	return &Server{ssh: s, ln: ln}, nil
}

// Addr reports the bound listen address (useful with ":0").
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Serve accepts sessions until Shutdown; it blocks. A graceful
// shutdown returns nil.
func (s *Server) Serve() error {
	err := s.ssh.Serve(s.ln)
	if err != nil && !errors.Is(err, ssh.ErrServerClosed) {
		return err
	}
	return nil
}

// Shutdown stops the listener and ends every live session.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.ssh.Shutdown(ctx)
}

// sessionHandler builds the per-connection game: a fresh default-start
// World, exactly like launching the binary bare. The bubbletea
// middleware wires the session's PTY as the program's input/output and
// translates SSH window changes into tea.WindowSizeMsg.
func sessionHandler(sess ssh.Session) (tea.Model, []tea.ProgramOption) {
	app, opts, err := newSessionApp()
	if err != nil {
		wish.Fatalln(sess, "terminal-space-program:", err)
		return nil, nil
	}
	// v0.27 S2: guests confirm braille + color rendering before the
	// game starts — undetectable server-side, so we ask.
	return withCalibrationCard(app), opts
}

// newSessionApp is the per-session game factory, split from the ssh
// plumbing so World independence is testable headlessly.
func newSessionApp() (tea.Model, []tea.ProgramOption, error) {
	app, err := tui.New(nil)
	if err != nil {
		return nil, nil, err
	}
	return app, []tea.ProgramOption{tea.WithAltScreen(), tea.WithMouseAllMotion()}, nil
}
