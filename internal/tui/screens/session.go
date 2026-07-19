package screens

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// SessionScreen is the multiplayer roster (v0.27 S6, ADR 0034): one
// row per player — online state, handle, last-known system/SOI, craft
// count, subspace Δt vs the viewer — plus, for the host, the Invites
// section (mint / outstanding codes / revoke / remove player, the
// same operations as the `serve invite` CLI). Persistent state gets a
// screen; moments get chips (the orbit canvas shows join/leave).
//
// It renders from w.Session (the serve-layer slate) and returns
// SessionCommands for the App/serve layers to execute — the screen
// itself mutates nothing.
type SessionScreen struct {
	theme  Theme
	cursor int // index into the players list

	inviteCursor    int  // index into the invites list (host)
	inInvites       bool // cursor focus: players vs invites section
	minting         bool // typing a new invite's handle
	mintInput       []rune
	confirmRemove   bool // pending [x] confirmation on the selected player
	confirmStopHost bool // pending [h] stop-hosting confirmation (v0.28 S3)
}

func NewSessionScreen(th Theme) *SessionScreen { return &SessionScreen{theme: th} }

// SessionCommandKind enumerates what the screen asks the app to do.
type SessionCommandKind int

const (
	SessionCmdNone SessionCommandKind = iota
	SessionCmdClose
	SessionCmdTargetGhost // aim at Owner/CraftID
	SessionCmdSpectate    // Spectate: camera-follow Owner/CraftID's ghost (v0.28 S6)
	SessionCmdSync        // Sync-to: Auto-Warp to Time (v0.27 S7)
	SessionCmdMint        // mint invite for Handle
	SessionCmdRevoke      // revoke invite Code
	SessionCmdRemove      // remove player Fingerprint
	SessionCmdToast       // surface Message
	SessionCmdStartHost   // solo → start hosting (v0.28 S3)
	SessionCmdStopHost    // hosting → stop the listener (v0.28 S3)
	SessionCmdRendezvous  // arm a Rendezvous Warp toward Owner's ghost Owner/CraftID (v0.29 S2)
)

// SessionCommand is the screen's finalized action.
type SessionCommand struct {
	Kind        SessionCommandKind
	Owner       string
	CraftID     uint64
	Handle      string
	Code        string
	Fingerprint string
	Message     string
	Time        time.Time // SessionCmdSync: the subspace time to chase
}

// SessionAdminMsg carries a mint/revoke/remove intent out of the App
// to the serve-layer wrapper (which owns the session store). In
// single-player nothing handles it — the message is inert.
type SessionAdminMsg struct{ Cmd SessionCommand }

// SessionHostMsg carries a start/stop-hosting intent out of the App to
// the always-present reporting wrapper (v0.28 S3, ADR 0034). Start
// lazily binds the SSH listener; Stop shuts it down. In a build with
// no wrapper (there is always one now) it is inert.
type SessionHostMsg struct{ Start bool }

// Reset re-arms the screen for a fresh open.
func (s *SessionScreen) Reset() {
	s.cursor, s.inviteCursor = 0, 0
	s.inInvites, s.minting, s.confirmRemove = false, false, false
	s.confirmStopHost = false
	s.mintInput = nil
}

// CapturingText reports whether the mint input is armed (the App
// skips keyboard-layout normalization while typing literal text).
func (s *SessionScreen) CapturingText() bool { return s.minting }

// HandleKey routes a keypress. w supplies the slate (rows, ghosts for
// targeting).
func (s *SessionScreen) HandleKey(w *sim.World, msg tea.KeyMsg) SessionCommand {
	info := w.Session
	if info == nil {
		// Solo: the screen is the entry point for hosting (v0.28 S3).
		// [h] asks the always-present wrapper to start the listener; the
		// App refuses it for a guest by construction (guestSave gate).
		switch msg.String() {
		case "esc", "O", "q":
			return SessionCommand{Kind: SessionCmdClose}
		case "h":
			return SessionCommand{Kind: SessionCmdStartHost}
		}
		return SessionCommand{}
	}

	// Normalise section focus (review follow-up): revoking the last
	// invite while focused there must not strand the cursor in an
	// empty section with tab gated off.
	if s.inInvites && len(info.Invites) == 0 {
		s.inInvites = false
	}

	if s.minting {
		switch msg.Type {
		case tea.KeyEnter:
			handle := strings.TrimSpace(string(s.mintInput))
			s.minting, s.mintInput = false, nil
			if handle == "" {
				return SessionCommand{}
			}
			return SessionCommand{Kind: SessionCmdMint, Handle: handle}
		case tea.KeyEscape:
			s.minting, s.mintInput = false, nil
			return SessionCommand{}
		case tea.KeyBackspace:
			if len(s.mintInput) > 0 {
				s.mintInput = s.mintInput[:len(s.mintInput)-1]
			}
			return SessionCommand{}
		case tea.KeyRunes:
			for _, r := range msg.Runes {
				if len(s.mintInput) < 24 {
					s.mintInput = append(s.mintInput, r)
				}
			}
			return SessionCommand{}
		}
		return SessionCommand{}
	}

	if s.confirmRemove {
		s.confirmRemove = false
		if msg.String() == "y" || msg.String() == "Y" {
			if p, ok := s.selectedPlayer(info); ok {
				return SessionCommand{Kind: SessionCmdRemove, Fingerprint: p.Fingerprint, Handle: p.Handle}
			}
		}
		return SessionCommand{}
	}

	if s.confirmStopHost {
		s.confirmStopHost = false
		if msg.String() == "y" || msg.String() == "Y" {
			return SessionCommand{Kind: SessionCmdStopHost}
		}
		return SessionCommand{}
	}

	switch msg.String() {
	case "esc", "q", "O":
		return SessionCommand{Kind: SessionCmdClose}
	case "up", "k":
		s.moveCursor(info, -1)
	case "down", "j":
		s.moveCursor(info, +1)
	case "tab":
		if info.IsHost && len(info.Invites) > 0 {
			s.inInvites = !s.inInvites
		}
	case "t":
		p, ok := s.selectedPlayer(info)
		if !ok {
			return SessionCommand{}
		}
		if p.Fingerprint == info.Self {
			return SessionCommand{Kind: SessionCmdToast, Message: "that's you"}
		}
		// Aim at the player's first ghosted craft — the slate is
		// already gated to this system and evaluated at our time.
		for _, g := range w.Ghosts {
			if g.Owner == p.Fingerprint {
				return SessionCommand{Kind: SessionCmdTargetGhost, Owner: g.Owner, CraftID: g.CraftID, Handle: p.Handle}
			}
		}
		return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no craft in this system to target"}
	case "v":
		// Spectate (v0.28 S6, ADR 0034): camera-follow the player's ghost.
		// Absent on your own row — you can't spectate yourself (no ghost of
		// your own exists in the slate anyway). Read-only; reachable by
		// host and guest alike.
		p, ok := s.selectedPlayer(info)
		if !ok || p.Fingerprint == info.Self {
			return SessionCommand{}
		}
		for _, g := range w.Ghosts {
			if g.Owner == p.Fingerprint {
				return SessionCommand{Kind: SessionCmdSpectate, Owner: g.Owner, CraftID: g.CraftID, Handle: p.Handle}
			}
		}
		return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no craft in this system to spectate"}
	case "w":
		// Rendezvous Warp (v0.29 S2, ADR 0034 v0.29 addendum): arm the
		// cooperative coast to the encounter with this player. Gated like
		// Sync/Spectate — absent on your own row — plus the same-subspace
		// Δt gate: across a real subspace divergence the arm could never
		// couple, so refuse with the actionable fix (Sync first).
		p, ok := s.selectedPlayer(info)
		if !ok || p.Fingerprint == info.Self {
			return SessionCommand{}
		}
		if !p.HasReport {
			return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no reported position to rendezvous with"}
		}
		if p.DeltaT > sim.CoWarpSubspaceTolerance || p.DeltaT < -sim.CoWarpSubspaceTolerance {
			return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " is in another subspace — Sync first, then rendezvous"}
		}
		for _, g := range w.Ghosts {
			if g.Owner == p.Fingerprint {
				return SessionCommand{Kind: SessionCmdRendezvous, Owner: g.Owner, CraftID: g.CraftID, Handle: p.Handle}
			}
		}
		return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no craft in this system to rendezvous with"}
	case "s":
		// Sync-to (v0.27 S7): forward only — the laggard always comes
		// forward (ADR 0034); someone behind you syncs to you instead.
		p, ok := s.selectedPlayer(info)
		if !ok || p.Fingerprint == info.Self {
			return SessionCommand{}
		}
		if !p.HasReport {
			return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no reported time to sync to"}
		}
		if p.DeltaT <= 0 {
			return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " is behind you — they sync to you"}
		}
		return SessionCommand{Kind: SessionCmdSync, Owner: p.Fingerprint, Handle: p.Handle, Time: w.Clock.SimTime.Add(p.DeltaT)}
	case "i":
		if info.IsHost {
			s.minting = true
			s.mintInput = nil
		}
	case "r":
		if info.IsHost && s.inInvites {
			if inv, ok := s.selectedInvite(info); ok {
				return SessionCommand{Kind: SessionCmdRevoke, Code: inv.Code, Handle: inv.Handle}
			}
		}
	case "x":
		if info.IsHost && !s.inInvites {
			if p, ok := s.selectedPlayer(info); ok && p.Role != "host" {
				s.confirmRemove = true
			}
		}
	case "h":
		// Host-only stop toggle (v0.28 S3): confirm before dropping
		// guests. Guests never reach this — their slate is never IsHost.
		if info.IsHost {
			s.confirmStopHost = true
		}
	}
	return SessionCommand{}
}

func (s *SessionScreen) moveCursor(info *sim.SessionInfo, d int) {
	if s.inInvites {
		s.inviteCursor = clamp(s.inviteCursor+d, 0, len(info.Invites)-1)
		return
	}
	s.cursor = clamp(s.cursor+d, 0, len(info.Players)-1)
}

func (s *SessionScreen) selectedPlayer(info *sim.SessionInfo) (sim.SessionPlayer, bool) {
	if s.cursor < 0 || s.cursor >= len(info.Players) {
		return sim.SessionPlayer{}, false
	}
	return info.Players[s.cursor], true
}

func (s *SessionScreen) selectedInvite(info *sim.SessionInfo) (sim.SessionInvite, bool) {
	if s.inviteCursor < 0 || s.inviteCursor >= len(info.Invites) {
		return sim.SessionInvite{}, false
	}
	return info.Invites[s.inviteCursor], true
}

// onlineGuests counts online roster members other than the viewer
// (the host) — the population a stop-hosting drops (v0.28 S3).
func onlineGuests(info *sim.SessionInfo) int {
	n := 0
	for _, p := range info.Players {
		if p.Online && p.Fingerprint != info.Self {
			n++
		}
	}
	return n
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Render draws the roster.
func (s *SessionScreen) Render(w *sim.World, width int) string {
	var b strings.Builder
	title := s.theme.Title.Render(" SESSION ")
	info := w.Session
	if info == nil {
		b.WriteString(title + "\n\n")
		b.WriteString("  Not in a multiplayer session.\n\n")
		b.WriteString(s.theme.Dim.Render("  [h] start hosting — accept ssh guests on this machine; invite them with serve invite.") + "\n\n")
		b.WriteString(s.theme.Footer.Render("  [h] start hosting   [esc] close"))
		return b.String()
	}

	b.WriteString(title + s.theme.Dim.Render(fmt.Sprintf("  %d players", len(info.Players))) + "\n\n")
	for i, p := range info.Players {
		marker := "  "
		if !s.inInvites && i == s.cursor {
			marker = s.theme.Primary.Render("▸ ")
		}
		dot := s.theme.Dim.Render("○")
		if p.Online {
			dot = s.theme.Primary.Render("●")
		}
		name := p.Handle
		var tags []string
		if p.Role == "host" {
			tags = append(tags, "host")
		}
		if p.Fingerprint == info.Self {
			tags = append(tags, "you")
		}
		if p.DockedGuest {
			tags = append(tags, "docked") // v0.28 S5: live — riding another player's stack
		}
		// Rendezvous Warp arm markers (v0.29 S2): who's waiting on whom.
		if p.WantsRendezvous {
			tags = append(tags, "wants rendezvous")
		}
		if p.RendezvousOut {
			tags = append(tags, "rendezvous armed")
		}
		if len(tags) > 0 {
			name += s.theme.Dim.Render(" (" + strings.Join(tags, ", ") + ")")
		}
		where := "—"
		if p.System != "" {
			where = p.System
			if p.Primary != "" {
				where += "/" + p.Primary
			}
		}
		craft := fmt.Sprintf("%d craft", p.CraftCount)
		b.WriteString(fmt.Sprintf("  %s%s %-32s %-16s %-9s %s\n",
			marker, dot, name, where, craft, formatDeltaT(p, info.Self == p.Fingerprint)))
	}

	if info.IsHost {
		b.WriteString("\n" + s.theme.Title.Render(" INVITES ") + "\n\n")
		// Normalise section focus (review follow-up): revoking the last
		// invite while focused there must not strand the cursor in an
		// empty section with tab gated off.
		if s.inInvites && len(info.Invites) == 0 {
			s.inInvites = false
		}

		if s.minting {
			b.WriteString("  new invite handle: " + string(s.mintInput) + "▌\n")
		} else if len(info.Invites) == 0 {
			b.WriteString(s.theme.Dim.Render("  no outstanding codes — [i] to mint one") + "\n")
		}
		for i, inv := range info.Invites {
			marker := "  "
			if s.inInvites && i == s.inviteCursor {
				marker = s.theme.Primary.Render("▸ ")
			}
			b.WriteString(fmt.Sprintf("  %s%s  %-16s %s\n",
				marker, inv.Code, inv.Handle, s.theme.Dim.Render(compactDuration(inv.Age)+" old")))
		}
	}

	b.WriteString("\n")
	if s.confirmRemove {
		if p, ok := s.selectedPlayer(info); ok {
			b.WriteString(s.theme.Alert.Render(fmt.Sprintf("  remove %s from the session? [y/n]", p.Handle)) + "\n")
		}
	}
	if s.confirmStopHost {
		b.WriteString(s.theme.Alert.Render(fmt.Sprintf("  stop hosting? drops %d guest(s) — progress persists [y/n]", onlineGuests(info))) + "\n")
	}
	keys := "  [t] target craft  [v] spectate  [s] sync-to  [w] rendezvous warp"
	if info.IsHost {
		keys += "  [i] invite  [r] revoke code  [x] remove player  [h] stop hosting  [tab] section"
	}
	keys += "  [esc] close"
	b.WriteString(s.theme.Footer.Render(keys))
	return b.String()
}

// formatDeltaT renders the subspace gap: "+2d4h ahead", "-3h12m
// behind", "in sync" inside a couple of seconds.
func formatDeltaT(p sim.SessionPlayer, isSelf bool) string {
	if isSelf {
		return ""
	}
	if !p.HasReport {
		return "—"
	}
	d := p.DeltaT
	if d > -2*time.Second && d < 2*time.Second {
		return "in sync"
	}
	if d > 0 {
		return "+" + compactDuration(d) + " ahead"
	}
	return "-" + compactDuration(-d) + " behind"
}
