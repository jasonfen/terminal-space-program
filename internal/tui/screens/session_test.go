package screens

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func sessionTheme() Theme {
	return Theme{
		Primary: lipgloss.NewStyle(),
		Warning: lipgloss.NewStyle(),
		Alert:   lipgloss.NewStyle(),
		Dim:     lipgloss.NewStyle(),
		HUDBox:  lipgloss.NewStyle(),
		Footer:  lipgloss.NewStyle(),
		Title:   lipgloss.NewStyle(),
	}
}

func sessionWorld(t *testing.T, isHost bool) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	self := "local"
	if !isHost {
		self = "SHA256:guest"
	}
	w.Session = &sim.SessionInfo{
		IsHost:        isHost,
		CanAdminister: isHost, // the host administers; admins are covered separately
		Self:          self,
		Players: []sim.SessionPlayer{
			{Fingerprint: "local", Handle: "jason", Role: "host", Online: true,
				HasReport: true, System: "Sol", Primary: "earth", CraftCount: 2, DeltaT: 0},
			{Fingerprint: "SHA256:guest", Handle: "gern", Role: "guest", Online: true,
				HasReport: true, System: "Sol", Primary: "moon", CraftCount: 1,
				DeltaT: 2*24*time.Hour + 4*time.Hour},
			{Fingerprint: "SHA256:offline", Handle: "dave", Role: "guest", Online: false,
				HasReport: true, System: "Lumen", Primary: "lumen", CraftCount: 3,
				DeltaT: -3 * time.Hour},
			{Fingerprint: "SHA256:never", Handle: "pat", Role: "guest", Online: false},
		},
		Invites: []sim.SessionInvite{
			{Code: "AB2C-DE3F", Handle: "newbie", Age: 3 * time.Minute},
		},
	}
	return w
}

// Roster rows across state combinations: online/offline dots, host/
// you tags, ahead/behind/in-sync Δt, no-report placeholder.
func TestSessionScreenRows(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	out := s.Render(w, 120)

	for _, want := range []string{
		"jason", "(host, you)",
		"gern", "Sol/moon", "1 craft", "+2d4h ahead",
		"dave", "Lumen/lumen", "3 craft", "-3h0m behind",
		"pat", "—", // never reported
	} {
		if !strings.Contains(out, want) {
			t.Errorf("roster missing %q:\n%s", want, out)
		}
	}

	// Δt formatting directly: in-sync band, and the self row is blank.
	inSync := sim.SessionPlayer{HasReport: true, DeltaT: time.Second}
	if got := formatDeltaT(inSync, false); got != "in sync" {
		t.Errorf("formatDeltaT(1s) = %q, want in sync", got)
	}
	if got := formatDeltaT(inSync, true); got != "" {
		t.Errorf("formatDeltaT(self) = %q, want empty", got)
	}
}

// The Invites section renders for the host and not for guests.
func TestSessionScreenHostVsGuestSections(t *testing.T) {
	s := NewSessionScreen(sessionTheme())

	host := s.Render(sessionWorld(t, true), 120)
	if !strings.Contains(host, "INVITES") || !strings.Contains(host, "AB2C-DE3F") {
		t.Errorf("host screen missing invites section:\n%s", host)
	}
	if !strings.Contains(host, "[i] invite") || !strings.Contains(host, "[x] remove player") {
		t.Errorf("host screen missing admin key hints:\n%s", host)
	}

	s2 := NewSessionScreen(sessionTheme())
	guest := s2.Render(sessionWorld(t, false), 120)
	if strings.Contains(guest, "INVITES") || strings.Contains(guest, "AB2C-DE3F") {
		t.Errorf("guest screen leaked the invites section:\n%s", guest)
	}
	if strings.Contains(guest, "[i] invite") {
		t.Error("guest screen offers the invite key")
	}
}

// Outside a session the screen explains rather than renders an empty
// roster.
func TestSessionScreenSinglePlayer(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	out := s.Render(w, 120)
	if !strings.Contains(out, "Not in a multiplayer session") {
		t.Errorf("single-player explainer missing:\n%s", out)
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEscape}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// Mint flow: [i] arms the input, typed handle + enter emits the mint
// command.
func TestSessionScreenMintFlow(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)

	if cmd := s.HandleKey(w, key("i")); cmd.Kind != SessionCmdNone {
		t.Fatalf("[i] emitted %v immediately", cmd.Kind)
	}
	if !s.CapturingText() {
		t.Fatal("mint input not armed after [i]")
	}
	for _, r := range "newpal" {
		s.HandleKey(w, key(string(r)))
	}
	cmd := s.HandleKey(w, key("enter"))
	if cmd.Kind != SessionCmdMint || cmd.Handle != "newpal" {
		t.Errorf("mint command = %+v", cmd)
	}
	if s.CapturingText() {
		t.Error("mint input still armed after submit")
	}
}

// Remove flow: [x] on a guest arms a confirm; y emits the removal;
// the host row can't be removed.
func TestSessionScreenRemoveConfirm(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)

	// Cursor starts on the host — [x] must not arm.
	s.HandleKey(w, key("x"))
	if cmd := s.HandleKey(w, key("y")); cmd.Kind == SessionCmdRemove {
		t.Fatal("removed the host")
	}

	s.HandleKey(w, key("j")) // down to gern
	s.HandleKey(w, key("x"))
	cmd := s.HandleKey(w, key("y"))
	if cmd.Kind != SessionCmdRemove || cmd.Fingerprint != "SHA256:guest" {
		t.Errorf("remove command = %+v", cmd)
	}
}

// Sync flow: [s] on a player ahead emits the sync command with their
// subspace time; behind → refusal toast (forward only); no report →
// toast.
func TestSessionScreenSync(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)

	s.HandleKey(w, key("j")) // gern: +2d4h ahead
	cmd := s.HandleKey(w, key("s"))
	if cmd.Kind != SessionCmdSync || cmd.Handle != "gern" {
		t.Fatalf("sync command = %+v", cmd)
	}
	want := w.Clock.SimTime.Add(2*24*time.Hour + 4*time.Hour)
	if !cmd.Time.Equal(want) {
		t.Errorf("sync target = %v, want %v", cmd.Time, want)
	}

	s.HandleKey(w, key("j")) // dave: -3h behind
	if cmd := s.HandleKey(w, key("s")); cmd.Kind != SessionCmdToast || !strings.Contains(cmd.Message, "behind you") {
		t.Errorf("[s] on a laggard = %+v, want forward-only refusal", cmd)
	}

	s.HandleKey(w, key("j")) // pat: no report
	if cmd := s.HandleKey(w, key("s")); cmd.Kind != SessionCmdToast {
		t.Errorf("[s] with no report = %+v, want toast", cmd)
	}
}

// Solo: the screen is the hosting entry point. [h] emits StartHost and
// the explainer advertises it (v0.28 S3).
func TestSessionScreenSoloStartHost(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	if out := s.Render(w, 120); !strings.Contains(out, "[h] start hosting") {
		t.Errorf("solo explainer missing the host hint:\n%s", out)
	}
	if cmd := s.HandleKey(w, key("h")); cmd.Kind != SessionCmdStartHost {
		t.Errorf("[h] solo = %+v, want SessionCmdStartHost", cmd)
	}
}

// Hosting: [h] arms a confirm naming the guest count; y stops, n
// cancels. The prompt is host-only.
func TestSessionScreenStopHostConfirm(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true) // one online guest (gern)

	if cmd := s.HandleKey(w, key("h")); cmd.Kind != SessionCmdNone {
		t.Fatalf("[h] emitted %v before confirm", cmd.Kind)
	}
	if out := s.Render(w, 120); !strings.Contains(out, "stop hosting? drops 1 guest(s)") {
		t.Errorf("stop-host confirm prompt missing:\n%s", out)
	}
	if !strings.Contains(s.Render(w, 120), "[h] stop hosting") {
		t.Error("host key hints missing the stop-hosting toggle")
	}
	if cmd := s.HandleKey(w, key("y")); cmd.Kind != SessionCmdStopHost {
		t.Errorf("confirm y = %+v, want SessionCmdStopHost", cmd)
	}

	// n cancels — no command, confirm cleared.
	s.HandleKey(w, key("h"))
	if cmd := s.HandleKey(w, key("n")); cmd.Kind != SessionCmdNone {
		t.Errorf("confirm n = %+v, want no command", cmd)
	}
}

// Promote/demote (v0.30 S2) is host-only: [p] on a guest row emits
// Promote, on an admin row emits Demote, and is inert on the host's own
// row.
func TestSessionScreenPromoteKey(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)

	// Cursor on the host — [p] must not act.
	if cmd := s.HandleKey(w, key("p")); cmd.Kind != SessionCmdNone {
		t.Errorf("[p] on host's own row = %+v, want no command", cmd)
	}

	s.HandleKey(w, key("j")) // gern (guest)
	if cmd := s.HandleKey(w, key("p")); cmd.Kind != SessionCmdPromote || cmd.Fingerprint != "SHA256:guest" {
		t.Errorf("[p] on guest = %+v, want Promote", cmd)
	}

	// Flip gern to admin — [p] now demotes.
	w.Session.Players[1].Role = "admin"
	if cmd := s.HandleKey(w, key("p")); cmd.Kind != SessionCmdDemote || cmd.Fingerprint != "SHA256:guest" {
		t.Errorf("[p] on admin = %+v, want Demote", cmd)
	}

	if !strings.Contains(s.Render(w, 120), "[p] promote/demote") {
		t.Error("host footer missing the promote/demote hint")
	}
	if !strings.Contains(s.Render(w, 120), "admin") {
		t.Error("admin role tag missing on the roster row")
	}
}

// An admin (CanAdminister but not IsHost) sees and uses the invites
// pane, but never the host-only delegation / removal / lifecycle keys.
func TestSessionScreenAdminView(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, false)
	w.Session.CanAdminister = true // promoted admin, still not the host

	out := s.Render(w, 120)
	if !strings.Contains(out, "INVITES") || !strings.Contains(out, "[i] invite") {
		t.Errorf("admin screen missing the invites pane:\n%s", out)
	}
	// Admins can remove guests (S3), so [x] is theirs; but delegation and
	// server lifecycle stay host-only.
	if strings.Contains(out, "[p] promote/demote") || strings.Contains(out, "stop hosting") {
		t.Errorf("admin screen offers host-only keys:\n%s", out)
	}
	// [i] arms minting for the admin.
	if s.HandleKey(w, key("i")); !s.CapturingText() {
		t.Error("admin [i] didn't arm the mint input")
	}
	s.HandleKey(w, key("esc"))
	// [p] is inert for a non-host.
	s.HandleKey(w, key("j")) // move off self
	if cmd := s.HandleKey(w, key("p")); cmd.Kind != SessionCmdNone {
		t.Errorf("admin [p] = %+v, want no command (delegation is host-only)", cmd)
	}
}

// Removal row-gating for an admin viewer (v0.30 S3): [x] arms on a
// guest row, but not on another admin's row, the host's row, or the
// viewer's own row.
func TestSessionScreenAdminRemoveGating(t *testing.T) {
	// Viewer is gern (a promoted admin). Fixture roster: host, gern
	// (self), dave, pat — mark dave an admin, pat stays a guest.
	w := sessionWorld(t, false)
	w.Session.CanAdminister = true
	w.Session.Players[2].Role = "admin" // dave → admin

	// Row 0 host: not offered.
	s := NewSessionScreen(sessionTheme())
	s.HandleKey(w, key("x"))
	if s.confirmRemove {
		t.Error("[x] armed on the host row")
	}

	// Row 1 self (gern): not offered.
	s = NewSessionScreen(sessionTheme())
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("x"))
	if s.confirmRemove {
		t.Error("[x] armed on the viewer's own row")
	}

	// Row 2 dave (admin): an admin can't remove another admin.
	s = NewSessionScreen(sessionTheme())
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("x"))
	if s.confirmRemove {
		t.Error("[x] armed on another admin's row")
	}

	// Row 3 pat (guest): offered → confirm → removal command.
	s = NewSessionScreen(sessionTheme())
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("x"))
	if !s.confirmRemove {
		t.Fatal("[x] didn't arm on a guest row")
	}
	if cmd := s.HandleKey(w, key("y")); cmd.Kind != SessionCmdRemove || cmd.Fingerprint != "SHA256:never" {
		t.Errorf("guest removal command = %+v", cmd)
	}
}

// A guest never reaches the host toggle: their slate is never IsHost,
// so [h] is inert and the stop-hosting hint is absent.
func TestSessionScreenGuestNoHost(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, false)
	if cmd := s.HandleKey(w, key("h")); cmd.Kind != SessionCmdNone {
		t.Errorf("[h] as guest = %+v, want no command", cmd)
	}
	if strings.Contains(s.Render(w, 120), "stop hosting") {
		t.Error("guest screen offers the stop-hosting toggle")
	}
}

// Target flow: [t] on a player with a ghost emits the target command;
// on yourself it toasts.
func TestSessionScreenTarget(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:guest", CraftID: 42, Handle: "gern"}}

	if cmd := s.HandleKey(w, key("t")); cmd.Kind != SessionCmdToast {
		t.Errorf("[t] on self = %+v, want toast", cmd)
	}
	s.HandleKey(w, key("j")) // gern
	cmd := s.HandleKey(w, key("t"))
	if cmd.Kind != SessionCmdTargetGhost || cmd.Owner != "SHA256:guest" || cmd.CraftID != 42 {
		t.Errorf("target command = %+v", cmd)
	}
	s.HandleKey(w, key("j")) // dave — no ghost in slate
	if cmd := s.HandleKey(w, key("t")); cmd.Kind != SessionCmdToast {
		t.Errorf("[t] with no ghost = %+v, want toast", cmd)
	}
}

// Rendezvous Warp flow (v0.29 S2): [w] on a same-subspace player with a
// ghost emits the rendezvous command; own row is absent; a player in
// another subspace gets the "Sync first" refusal; no ghost / no report
// toast. The footer advertises it.
func TestSessionScreenRendezvous(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	// gern in Δt tolerance for this test (the fixture has them +2d4h out).
	w.Session.Players[1].DeltaT = 30 * time.Second
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:guest", CraftID: 42, Handle: "gern"}}

	if !strings.Contains(s.Render(w, 120), "[w] rendezvous") {
		t.Error("footer missing the [w] rendezvous hint")
	}

	// Own row: absent — no command.
	if cmd := s.HandleKey(w, key("w")); cmd.Kind != SessionCmdNone {
		t.Errorf("[w] on own row = %+v, want none", cmd)
	}

	s.HandleKey(w, key("j")) // gern — in tolerance, has a ghost
	cmd := s.HandleKey(w, key("w"))
	if cmd.Kind != SessionCmdRendezvous || cmd.Owner != "SHA256:guest" || cmd.CraftID != 42 || cmd.Handle != "gern" {
		t.Errorf("rendezvous command = %+v", cmd)
	}

	// Out of Δt tolerance → "Sync first" refusal.
	w.Session.Players[1].DeltaT = 2 * 24 * time.Hour
	if cmd := s.HandleKey(w, key("w")); cmd.Kind != SessionCmdToast || !strings.Contains(cmd.Message, "Sync") {
		t.Errorf("[w] across subspaces = %+v, want Sync-first toast", cmd)
	}
	w.Session.Players[1].DeltaT = 30 * time.Second

	// No ghost in this system → toast.
	w.Ghosts = nil
	if cmd := s.HandleKey(w, key("w")); cmd.Kind != SessionCmdToast {
		t.Errorf("[w] with no ghost = %+v, want toast", cmd)
	}

	// No report at all → toast.
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("j")) // pat: never reported
	if cmd := s.HandleKey(w, key("w")); cmd.Kind != SessionCmdToast {
		t.Errorf("[w] with no report = %+v, want toast", cmd)
	}
}

// Roster rows carry the Rendezvous Warp markers (v0.29 S2): incoming
// arm ("wants rendezvous") and outgoing arm ("rendezvous armed").
func TestSessionScreenRendezvousRowMarkers(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Session.Players[1].WantsRendezvous = true
	w.Session.Players[2].RendezvousOut = true

	out := s.Render(w, 120)
	if !strings.Contains(out, "wants rendezvous") {
		t.Errorf("incoming-arm marker missing:\n%s", out)
	}
	if !strings.Contains(out, "rendezvous armed") {
		t.Errorf("outgoing-arm marker missing:\n%s", out)
	}
}

// Spectate flow (v0.28 S6): [v] on a player with a ghost emits the
// spectate command; on your own row it is absent (no command); on a
// player with no ghost in the slate it toasts. The footer advertises it.
func TestSessionScreenSpectate(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:guest", CraftID: 42, Handle: "gern"}}

	if !strings.Contains(s.Render(w, 120), "[v] spectate") {
		t.Error("footer missing the [v] spectate hint")
	}

	// Own row (cursor starts on self): [v] is absent — no spectate command.
	if cmd := s.HandleKey(w, key("v")); cmd.Kind == SessionCmdSpectate {
		t.Errorf("[v] on own row emitted spectate: %+v", cmd)
	}

	s.HandleKey(w, key("j")) // move to gern
	cmd := s.HandleKey(w, key("v"))
	if cmd.Kind != SessionCmdSpectate || cmd.Owner != "SHA256:guest" || cmd.CraftID != 42 {
		t.Errorf("spectate command = %+v, want SessionCmdSpectate owner=SHA256:guest craft=42", cmd)
	}

	s.HandleKey(w, key("j")) // dave — no ghost in slate
	if cmd := s.HandleKey(w, key("v")); cmd.Kind != SessionCmdToast {
		t.Errorf("[v] with no ghost = %+v, want toast", cmd)
	}
}
