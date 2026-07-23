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
	inCraftList     bool // cursor focus: selected player's craft sub-list (v0.30 S6)
	craftCursor     int  // index into the selected player's ghost sub-list
	minting         bool // typing a new invite's handle
	mintInput       []rune
	confirmRemove   bool // pending [x] confirmation on the selected player
	confirmStopHost bool // pending [h] stop-hosting confirmation (v0.28 S3)
	confirmRestart  bool // pending [u] server-restart confirmation (v0.30 S4)
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
	SessionCmdPromote     // host promotes Fingerprint (guest → admin) (v0.30 S2)
	SessionCmdDemote      // host demotes Fingerprint (admin → guest) (v0.30 S2)
	SessionCmdRestart     // admin drain-and-restart the server (v0.30 S4)
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

// SessionAdminMsg carries a session-admin intent (mint/revoke/remove,
// or host-only promote/demote) out of the App to the serve-layer
// wrapper, which owns the session store and enforces authorization. In
// single-player nothing handles it — the message is inert.
type SessionAdminMsg struct{ Cmd SessionCommand }

// SessionHostMsg carries a start/stop-hosting intent out of the App to
// the always-present reporting wrapper (v0.28 S3, ADR 0034). Start
// lazily binds the SSH listener; Stop shuts it down. In a build with
// no wrapper (there is always one now) it is inert.
type SessionHostMsg struct{ Start bool }

// SessionRestartMsg carries an admin's drain-and-restart intent out of
// the App to the serve-layer wrapper (v0.30 S4). The wrapper enforces
// authorization, drains connected players, and exits with a marker the
// supervising service manager restarts on. Inert in single-player.
type SessionRestartMsg struct{}

// Reset re-arms the screen for a fresh open.
func (s *SessionScreen) Reset() {
	s.cursor, s.inviteCursor, s.craftCursor = 0, 0, 0
	s.inInvites, s.inCraftList = false, false
	s.minting, s.confirmRemove = false, false
	s.confirmStopHost, s.confirmRestart = false, false
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

	// Normalise craft-picker focus (v0.30 S6): if the selected player's
	// targetable ghosts vanished (they landed the craft or left the
	// system between ticks), pop the sub-list so keys can't strand on an
	// empty picker. A slate that merely SHRANK clamps instead (S7 review)
	// — leaving the cursor past the end deadened t/v/w silently.
	if s.inCraftList {
		p, ok := s.selectedPlayer(info)
		n := 0
		if ok {
			n = len(playerGhosts(w, p.Fingerprint))
		}
		if n == 0 {
			s.inCraftList = false
		} else {
			s.craftCursor = clamp(s.craftCursor, 0, n-1)
		}
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

	if s.confirmRestart {
		s.confirmRestart = false
		if msg.String() == "y" || msg.String() == "Y" {
			return SessionCommand{Kind: SessionCmdRestart}
		}
		return SessionCommand{}
	}

	switch msg.String() {
	case "esc":
		// esc pops the craft sub-list one level before closing the screen
		// (v0.30 S6).
		if s.inCraftList {
			s.inCraftList = false
			return SessionCommand{}
		}
		return SessionCommand{Kind: SessionCmdClose}
	case "q", "O":
		return SessionCommand{Kind: SessionCmdClose}
	case "up", "k":
		s.moveCursor(w, info, -1)
	case "down", "j":
		s.moveCursor(w, info, +1)
	case "tab":
		if info.CanAdminister && !s.inCraftList && len(info.Invites) > 0 {
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
		g, ok, empty := s.pickGhost(w, p)
		if empty {
			return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no craft in this system to target"}
		}
		if !ok {
			return SessionCommand{} // 2+ craft: the sub-list is now open — pick one
		}
		return SessionCommand{Kind: SessionCmdTargetGhost, Owner: g.Owner, CraftID: g.CraftID, Handle: p.Handle}
	case "v":
		// Spectate (v0.28 S6, ADR 0034): camera-follow the player's ghost.
		// Absent on your own row — you can't spectate yourself (no ghost of
		// your own exists in the slate anyway). Read-only; reachable by
		// host and guest alike.
		p, ok := s.selectedPlayer(info)
		if !ok || p.Fingerprint == info.Self {
			return SessionCommand{}
		}
		g, ok, empty := s.pickGhost(w, p)
		if empty {
			return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no craft in this system to spectate"}
		}
		if !ok {
			return SessionCommand{}
		}
		return SessionCommand{Kind: SessionCmdSpectate, Owner: g.Owner, CraftID: g.CraftID, Handle: p.Handle}
	case "w":
		// Rendezvous Warp (v0.29 S2, ADR 0034 v0.29 addendum): arm the
		// cooperative coast to the encounter with this player. Gated like
		// Sync/Spectate — absent on your own row — plus the same-subspace
		// Δt gate: across a real subspace divergence the arm could never
		// couple, so refuse with the actionable fix (Sync first). The
		// per-player gates fire before the craft picker opens.
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
		g, ok, empty := s.pickGhost(w, p)
		if empty {
			return SessionCommand{Kind: SessionCmdToast, Message: p.Handle + " has no craft in this system to rendezvous with"}
		}
		if !ok {
			return SessionCommand{}
		}
		return SessionCommand{Kind: SessionCmdRendezvous, Owner: g.Owner, CraftID: g.CraftID, Handle: p.Handle}
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
		if info.CanAdminister && !s.inCraftList {
			s.minting = true
			s.mintInput = nil
		}
	case "r":
		if info.CanAdminister && s.inInvites {
			if inv, ok := s.selectedInvite(info); ok {
				return SessionCommand{Kind: SessionCmdRevoke, Code: inv.Code, Handle: inv.Handle}
			}
		}
	case "x":
		// Removal is reachable by the host and admins (v0.30 S3), gated
		// per-row by the same guardrail the handler enforces: never self,
		// the host, or (for an admin actor) another admin.
		if info.CanAdminister && !s.inInvites && !s.inCraftList {
			if p, ok := s.selectedPlayer(info); ok && mayRemoveRow(info, p) {
				s.confirmRemove = true
			}
		}
	case "p":
		// Host-only delegation (v0.30 S2): promote a guest to admin or
		// demote an admin back to guest. Single-rooted — an admin never
		// sees this. Absent on the host's own row and on non-guest/admin
		// rows.
		if info.IsHost && !s.inInvites && !s.inCraftList {
			if p, ok := s.selectedPlayer(info); ok && p.Fingerprint != info.Self {
				switch p.Role {
				case "guest":
					return SessionCommand{Kind: SessionCmdPromote, Fingerprint: p.Fingerprint, Handle: p.Handle}
				case "admin":
					return SessionCommand{Kind: SessionCmdDemote, Fingerprint: p.Fingerprint, Handle: p.Handle}
				}
			}
		}
	case "h":
		// Host-only stop toggle (v0.28 S3): confirm before dropping
		// guests. Guests never reach this — their slate is never IsHost.
		if info.IsHost && !s.inCraftList {
			s.confirmStopHost = true
		}
	case "u":
		// Admin server restart (v0.30 S4): drain everyone and exit with a
		// marker the supervisor restarts on. Confirm first (states the
		// drop count). Host and admins; guests never reach it.
		if info.CanAdminister && !s.inCraftList {
			s.confirmRestart = true
		}
	}
	return SessionCommand{}
}

func (s *SessionScreen) moveCursor(w *sim.World, info *sim.SessionInfo, d int) {
	if s.inCraftList {
		if p, ok := s.selectedPlayer(info); ok {
			n := len(playerGhosts(w, p.Fingerprint))
			s.craftCursor = clamp(s.craftCursor+d, 0, n-1)
		}
		return
	}
	if s.inInvites {
		s.inviteCursor = clamp(s.inviteCursor+d, 0, len(info.Invites)-1)
		return
	}
	s.cursor = clamp(s.cursor+d, 0, len(info.Players)-1)
}

// playerGhosts returns the viewer's ghost-slate entries owned by the
// given fingerprint, in slate order (v0.30 S6). The slate is already
// filtered to non-landed craft in the viewer's current system by the
// relay layer, so this is the set of the player's targetable craft.
func playerGhosts(w *sim.World, fingerprint string) []sim.Ghost {
	var out []sim.Ghost
	for _, g := range w.Ghosts {
		if g.Owner == fingerprint {
			out = append(out, g)
		}
	}
	return out
}

// pickGhost resolves which of a player's ghosted craft a t/v/w verb acts
// on (v0.30 S6, #220). With exactly one targetable ghost it returns it
// for a single-keystroke action. With two or more, and the sub-list not
// yet open, it opens the craft picker and returns ok=false (no action
// this press); a subsequent verb, with the sub-list open, returns the
// highlighted craft. With none it returns empty=true so the caller can
// toast the right "no craft here" message.
func (s *SessionScreen) pickGhost(w *sim.World, p sim.SessionPlayer) (g sim.Ghost, ok, empty bool) {
	ghosts := playerGhosts(w, p.Fingerprint)
	if len(ghosts) == 0 {
		return sim.Ghost{}, false, true
	}
	if s.inCraftList {
		if s.craftCursor < 0 || s.craftCursor >= len(ghosts) {
			return sim.Ghost{}, false, false
		}
		return ghosts[s.craftCursor], true, false
	}
	if len(ghosts) == 1 {
		return ghosts[0], true, false
	}
	// Opening the picker takes focus off the invites pane (S7 review).
	// t/v/w act on the player row from either section, but the sub-list
	// only renders when the invites pane isn't focused — so opening it
	// without this left the screen in an invisible picker mode where j/k
	// drove a craft cursor the player could not see.
	s.inInvites = false
	s.inCraftList, s.craftCursor = true, 0
	return sim.Ghost{}, false, false
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

// mayRemoveRow mirrors sessiondir.MayRemove for UI gating (v0.30 S3):
// the screen only offers [x] on rows the viewer is actually allowed to
// remove. The handler re-checks authoritatively — this just avoids
// dangling a key that would no-op.
func mayRemoveRow(info *sim.SessionInfo, p sim.SessionPlayer) bool {
	if !info.CanAdminister {
		return false
	}
	if p.Fingerprint == info.Self || p.Role == "host" {
		return false
	}
	// An admin (can administer but isn't the host) can't remove another admin.
	if !info.IsHost && p.Role == "admin" {
		return false
	}
	return true
}

// releasesPageURL is where an install without adopt tooling is pointed
// to update manually (v0.30 S5).
const releasesPageURL = "github.com/jasonfen/terminal-space-program/releases"

// adopting reports whether the [u] restart should be framed as adopting
// an available release: a newer version exists AND the supervisor
// signalled adopt-capability. Absent either, [u] is a plain restart and
// the readout points at the manual update path — the UI never offers an
// update it can't perform.
func adopting(info *sim.SessionInfo) bool {
	return info.AvailableVersion != "" && info.AdoptCapable
}

// restartKeyLabel is the footer label for [u] — "restart to adopt vX"
// when adopting, else a plain "restart server".
func restartKeyLabel(info *sim.SessionInfo) string {
	if adopting(info) {
		return "[u] restart to adopt " + displayVer(info.AvailableVersion)
	}
	return "[u] restart server"
}

// restartPrompt is the confirmation line for [u], adopt-aware and naming
// the drop count.
func restartPrompt(info *sim.SessionInfo) string {
	if adopting(info) {
		return fmt.Sprintf("restart to adopt %s? drops %d player(s) — progress persists, they reconnect",
			displayVer(info.AvailableVersion), onlineGuests(info))
	}
	return fmt.Sprintf("restart server? drops %d player(s) — progress persists, they reconnect", onlineGuests(info))
}

// displayVer normalises a version string for display: a numeric SemVer
// gets a leading "v" (release ldflags strip it); a non-numeric build
// string like "dev" is shown as-is.
func displayVer(v string) string {
	if v == "" {
		return v
	}
	if v[0] >= '0' && v[0] <= '9' {
		return "v" + v
	}
	return v
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
		switch p.Role {
		case "host":
			tags = append(tags, "host")
		case "admin":
			tags = append(tags, "admin")
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
		// Craft count: the reported total can include landed / other-
		// system craft the viewer can't target. When the player is in the
		// viewer's system and fewer craft are targetable here than reported,
		// spell out the targetable-here count so "%d craft" doesn't mislead
		// (v0.30 S6 trap). Other-system players read from the system column.
		here := len(playerGhosts(w, p.Fingerprint))
		craft := fmt.Sprintf("%d craft", p.CraftCount)
		if p.System != "" && p.System == w.System().Name && here < p.CraftCount {
			craft = fmt.Sprintf("%d craft (%d here)", p.CraftCount, here)
		}
		b.WriteString(fmt.Sprintf("  %s%s %-32s %-16s %-16s %s\n",
			marker, dot, name, where, craft, formatDeltaT(p, info.Self == p.Fingerprint)))

		// Craft picker (v0.30 S6): when this player's sub-list is open,
		// enumerate their targetable ghosts (glyph + name) with a second
		// cursor, mirroring the two-level invites idiom.
		if s.inCraftList && !s.inInvites && i == s.cursor {
			for gi, g := range playerGhosts(w, p.Fingerprint) {
				gmark := "      "
				if gi == s.craftCursor {
					gmark = "    " + s.theme.Primary.Render("▸ ")
				}
				glyph := g.Glyph
				if glyph == "" {
					glyph = "·"
				}
				b.WriteString(gmark + glyph + " " + s.theme.Dim.Render(g.Name) + "\n")
			}
		}
	}

	if info.CanAdminister {
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
	if s.confirmRestart {
		b.WriteString(s.theme.Alert.Render("  "+restartPrompt(info)+" [y/n]") + "\n")
	}

	// Version surface (v0.30 S5): always show the running version; when a
	// newer release exists, show it, and — on a box with no adopt tooling
	// — point at the manual update path so the UI never offers an update
	// it can't perform.
	if info.RunningVersion != "" {
		line := "  running " + displayVer(info.RunningVersion)
		if info.AvailableVersion != "" {
			line += " — update available: " + displayVer(info.AvailableVersion)
		}
		b.WriteString(s.theme.Dim.Render(line) + "\n")
		if info.AvailableVersion != "" && !info.AdoptCapable {
			b.WriteString(s.theme.Dim.Render("  update manually — "+releasesPageURL) + "\n")
		}
	}

	keys := "  [t] target craft  [v] spectate  [s] sync-to  [w] rendezvous warp"
	if info.CanAdminister {
		keys += "  [i] invite  [r] revoke code  [x] remove player  " + restartKeyLabel(info)
	}
	if info.IsHost {
		keys += "  [p] promote/demote  [h] stop hosting"
	}
	if info.CanAdminister {
		keys += "  [tab] section"
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
