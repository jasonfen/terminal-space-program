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

	// Cursor on the host — [p] must not act, and must say why (v0.30.1).
	if cmd := s.HandleKey(w, key("p")); cmd.Kind != SessionCmdToast {
		t.Errorf("[p] on host's own row = %+v, want a refusal toast", cmd)
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
	if cmd := s.HandleKey(w, key("p")); cmd.Kind != SessionCmdToast {
		t.Errorf("admin [p] = %+v, want a refusal toast (delegation is host-only)", cmd)
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

// Restart (v0.30 S4): [u] arms a confirm naming the drop count; y emits
// the restart command. Reachable by host and admin; a plain guest never
// sees the key.
func TestSessionScreenRestartConfirm(t *testing.T) {
	// Host viewer: one online guest (gern).
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)

	if cmd := s.HandleKey(w, key("u")); cmd.Kind != SessionCmdNone {
		t.Fatalf("[u] emitted %v before confirm", cmd.Kind)
	}
	if out := s.Render(w, 120); !strings.Contains(out, "restart server? drops 1 player(s)") {
		t.Errorf("restart confirm prompt missing:\n%s", out)
	}
	if !strings.Contains(s.Render(w, 120), "[u] restart server") {
		t.Error("admin footer missing the restart hint")
	}
	if cmd := s.HandleKey(w, key("y")); cmd.Kind != SessionCmdRestart {
		t.Errorf("confirm y = %+v, want SessionCmdRestart", cmd)
	}
	// n cancels.
	s.HandleKey(w, key("u"))
	if cmd := s.HandleKey(w, key("n")); cmd.Kind != SessionCmdNone {
		t.Errorf("confirm n = %+v, want no command", cmd)
	}

	// An admin (CanAdminister, not IsHost) also gets it.
	adminScreen := NewSessionScreen(sessionTheme())
	wa := sessionWorld(t, false)
	wa.Session.CanAdminister = true
	if cmd := adminScreen.HandleKey(wa, key("u")); cmd.Kind != SessionCmdNone || !adminScreen.confirmRestart {
		t.Errorf("admin [u] didn't arm restart confirm (cmd %+v)", cmd)
	}

	// A guest doesn't: [u] refuses (with a reason, v0.30.1) and no hint.
	guestScreen := NewSessionScreen(sessionTheme())
	wg := sessionWorld(t, false)
	if cmd := guestScreen.HandleKey(wg, key("u")); cmd.Kind != SessionCmdToast || guestScreen.confirmRestart {
		t.Errorf("guest [u] armed a restart (cmd %+v)", cmd)
	}
	if strings.Contains(guestScreen.Render(wg, 120), "restart server") {
		t.Error("guest screen offers the restart key")
	}
}

// Version surface (v0.30 S5): the running version always shows; an
// available release reframes [u] as "restart to adopt" only when the box
// is adopt-capable, otherwise it points at the manual update path and [u]
// stays a plain restart.
func TestSessionScreenVersionSurface(t *testing.T) {
	// Running only, no update.
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Session.RunningVersion = "0.30.0"
	out := s.Render(w, 120)
	if !strings.Contains(out, "running v0.30.0") {
		t.Errorf("running version missing:\n%s", out)
	}
	if strings.Contains(out, "update available") || strings.Contains(out, "restart to adopt") {
		t.Errorf("spurious update UI with no available version:\n%s", out)
	}
	if !strings.Contains(out, "[u] restart server") {
		t.Error("plain restart key missing")
	}

	// Update available + adopt-capable → adopt framing.
	s = NewSessionScreen(sessionTheme())
	w.Session.AvailableVersion = "v0.31.0"
	w.Session.AdoptCapable = true
	out = s.Render(w, 120)
	if !strings.Contains(out, "update available: v0.31.0") {
		t.Errorf("available version missing:\n%s", out)
	}
	if !strings.Contains(out, "[u] restart to adopt v0.31.0") {
		t.Errorf("adopt affordance missing:\n%s", out)
	}
	if strings.Contains(out, "update manually") {
		t.Error("adopt-capable box shows the manual path")
	}
	// The confirm is adopt-aware.
	s.HandleKey(w, key("u"))
	if !strings.Contains(s.Render(w, 120), "restart to adopt v0.31.0? drops") {
		t.Errorf("adopt confirm prompt missing:\n%s", s.Render(w, 120))
	}

	// Update available + NOT adopt-capable → manual path, plain restart.
	s = NewSessionScreen(sessionTheme())
	w.Session.AdoptCapable = false
	out = s.Render(w, 120)
	if !strings.Contains(out, "update available: v0.31.0") {
		t.Error("readout should still show the available version without adopt tooling")
	}
	if !strings.Contains(out, "update manually — "+releasesPageURL) {
		t.Errorf("manual update path missing:\n%s", out)
	}
	if strings.Contains(out, "restart to adopt") {
		t.Error("adopt affordance shown without adopt capability (the UI would lie)")
	}
	if !strings.Contains(out, "[u] restart server") {
		t.Error("plain restart key missing on a non-adopt box")
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

// Ghost picker (v0.30 S6, #220): a player flying 2+ targetable craft
// gets a craft sub-list on [t]; the cursor picks one, and t/v/w all act
// on the selection. esc pops the sub-list one level before closing.
func TestSessionScreenGhostPicker(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	// gern (players[1]) flies three craft, all in the viewer's system.
	// In tolerance so [w] resolves rather than refusing across subspaces.
	w.Session.Players[1].DeltaT = 30 * time.Second
	w.Ghosts = []sim.Ghost{
		{Owner: "SHA256:guest", CraftID: 10, Handle: "gern", Name: "Scout", Glyph: "^"},
		{Owner: "SHA256:guest", CraftID: 11, Handle: "gern", Name: "Hauler", Glyph: "#"},
		{Owner: "SHA256:guest", CraftID: 12, Handle: "gern", Name: "Probe", Glyph: "."},
	}
	s.HandleKey(w, key("j")) // move to gern

	// First [t] on a multi-craft player opens the picker, no target yet.
	if cmd := s.HandleKey(w, key("t")); cmd.Kind != SessionCmdNone {
		t.Fatalf("[t] on multi-craft player = %+v, want none (opens picker)", cmd)
	}
	// The sub-list enumerates the ghosts by name.
	out := s.Render(w, 120)
	for _, want := range []string{"Scout", "Hauler", "Probe"} {
		if !strings.Contains(out, want) {
			t.Errorf("picker missing craft %q:\n%s", want, out)
		}
	}

	// Move to the second craft and target it.
	s.HandleKey(w, key("j"))
	if cmd := s.HandleKey(w, key("t")); cmd.Kind != SessionCmdTargetGhost || cmd.CraftID != 11 {
		t.Errorf("[t] on selection = %+v, want target craft 11", cmd)
	}
	// v and w honour the same selection (all three verbs fixed at once).
	if cmd := s.HandleKey(w, key("v")); cmd.Kind != SessionCmdSpectate || cmd.CraftID != 11 {
		t.Errorf("[v] on selection = %+v, want spectate craft 11", cmd)
	}
	if cmd := s.HandleKey(w, key("w")); cmd.Kind != SessionCmdRendezvous || cmd.CraftID != 11 {
		t.Errorf("[w] on selection = %+v, want rendezvous craft 11", cmd)
	}

	// esc pops the sub-list one level (no close); a second esc closes.
	if cmd := s.HandleKey(w, key("esc")); cmd.Kind != SessionCmdNone {
		t.Errorf("esc in picker = %+v, want none (pop)", cmd)
	}
	if cmd := s.HandleKey(w, key("esc")); cmd.Kind != SessionCmdClose {
		t.Errorf("esc after pop = %+v, want close", cmd)
	}
}

// A single-craft player keeps the one-keystroke behaviour — [t] targets
// immediately, no picker step (v0.30 S6).
func TestSessionScreenGhostPickerSingleCraft(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:guest", CraftID: 7, Handle: "gern", Name: "Solo"}}
	s.HandleKey(w, key("j")) // gern
	if cmd := s.HandleKey(w, key("t")); cmd.Kind != SessionCmdTargetGhost || cmd.CraftID != 7 {
		t.Errorf("[t] single craft = %+v, want immediate target craft 7", cmd)
	}
}

// The roster row distinguishes craft targetable here from the reported
// total (v0.30 S6 trap): a "%d craft" count from the report can include
// landed / other-system craft with zero targetable ghosts in-system.
func TestSessionScreenTargetableCount(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	// gern reports 3 craft (in the viewer's Sol system) but only one is
	// a targetable ghost here — the other two are landed.
	w.Session.Players[1].CraftCount = 3
	w.Ghosts = []sim.Ghost{{Owner: "SHA256:guest", CraftID: 1, Handle: "gern", Name: "Scout"}}
	out := s.Render(w, 120)
	if !strings.Contains(out, "3 craft (1 here)") {
		t.Errorf("row should distinguish targetable-here count:\n%s", out)
	}
}

// Opening the craft picker from the invites pane must pull focus back to
// the player rows (v0.30 S7 review). t/v/w always act on the selected
// player row, but the sub-list only renders when the invites pane isn't
// focused — so without this the screen enters an invisible picker mode
// where j/k silently drive a craft cursor the player can't see.
func TestSessionScreenGhostPickerFromInvitesPane(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Ghosts = []sim.Ghost{
		{Owner: "SHA256:guest", CraftID: 10, Handle: "gern", Name: "Scout"},
		{Owner: "SHA256:guest", CraftID: 11, Handle: "gern", Name: "Hauler"},
	}
	s.HandleKey(w, key("j"))   // gern
	s.HandleKey(w, key("tab")) // focus the invites pane
	s.HandleKey(w, key("t"))   // opens the picker

	out := s.Render(w, 120)
	for _, want := range []string{"Scout", "Hauler"} {
		if !strings.Contains(out, want) {
			t.Fatalf("picker opened from the invites pane but never rendered %q:\n%s", want, out)
		}
	}
	// The cursor now drives the craft list, not the invites section.
	s.HandleKey(w, key("j"))
	if cmd := s.HandleKey(w, key("t")); cmd.Kind != SessionCmdTargetGhost || cmd.CraftID != 11 {
		t.Errorf("[t] after moving in the picker = %+v, want target craft 11", cmd)
	}
}

// A shrinking ghost slate must not strand the craft cursor past the end
// (v0.30 S7 review): pickGhost would return no selection and no empty
// flag, so t/v/w silently did nothing — with no toast — until the player
// happened to press an arrow key.
func TestSessionScreenGhostPickerSlateShrinks(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Ghosts = []sim.Ghost{
		{Owner: "SHA256:guest", CraftID: 10, Handle: "gern", Name: "Scout"},
		{Owner: "SHA256:guest", CraftID: 11, Handle: "gern", Name: "Hauler"},
		{Owner: "SHA256:guest", CraftID: 12, Handle: "gern", Name: "Probe"},
	}
	s.HandleKey(w, key("j")) // gern
	s.HandleKey(w, key("t")) // open the picker
	s.HandleKey(w, key("j"))
	s.HandleKey(w, key("j")) // craft cursor on Probe (index 2)

	// gern lands Probe between ticks: the slate drops to two.
	w.Ghosts = w.Ghosts[:2]
	if cmd := s.HandleKey(w, key("t")); cmd.Kind != SessionCmdTargetGhost || cmd.CraftID != 11 {
		t.Errorf("[t] after the slate shrank = %+v, want the clamped last craft 11", cmd)
	}
}

// Every refusal path for the admin keys must SAY why (v0.30.1). A silent
// no-op is indistinguishable from a dead key — the [p] report that
// prompted this was the host pressing it on their own row, with the
// screen giving no reason at all.
func TestSessionScreenPromoteRefusalsToast(t *testing.T) {
	toastFor := func(setup func(w *sim.World, s *SessionScreen)) string {
		w := sessionWorld(t, true)
		s := NewSessionScreen(sessionTheme())
		setup(w, s)
		cmd := s.HandleKey(w, key("p"))
		if cmd.Kind != SessionCmdToast {
			t.Fatalf("[p] refusal = %+v, want a toast explaining why", cmd)
		}
		return cmd.Message
	}

	// The reported case: cursor sits on the host's own row by default.
	if msg := toastFor(func(*sim.World, *SessionScreen) {}); !strings.Contains(msg, "yourself") {
		t.Errorf("[p] on own row toast = %q, want it to say you can't promote yourself", msg)
	}
	// Hosting alone — nobody to promote yet.
	if msg := toastFor(func(w *sim.World, s *SessionScreen) {
		w.Session.Players = w.Session.Players[:1]
	}); !strings.Contains(msg, "invite") {
		t.Errorf("[p] solo toast = %q, want it to point at inviting a player", msg)
	}
	// A promoted admin can't delegate — single-rooted escalation.
	if msg := toastFor(func(w *sim.World, s *SessionScreen) {
		w.Session.IsHost = false
		w.Session.CanAdminister = true
		s.HandleKey(w, key("j"))
	}); !strings.Contains(msg, "only the host") {
		t.Errorf("admin [p] toast = %q, want it to say delegation is host-only", msg)
	}
}

func TestSessionScreenRemoveRefusalsToast(t *testing.T) {
	// Viewer is gern, a promoted admin: host, gern (self), dave, pat.
	toastAt := func(steps int, setup func(w *sim.World)) string {
		w := sessionWorld(t, false)
		w.Session.CanAdminister = true
		if setup != nil {
			setup(w)
		}
		s := NewSessionScreen(sessionTheme())
		for i := 0; i < steps; i++ {
			s.HandleKey(w, key("j"))
		}
		cmd := s.HandleKey(w, key("x"))
		if cmd.Kind != SessionCmdToast {
			t.Fatalf("[x] refusal at row %d = %+v, want a toast", steps, cmd)
		}
		return cmd.Message
	}

	if msg := toastAt(0, nil); !strings.Contains(msg, "host") {
		t.Errorf("[x] on host row toast = %q, want it to name the host", msg)
	}
	if msg := toastAt(1, nil); !strings.Contains(msg, "yourself") {
		t.Errorf("[x] on own row toast = %q, want it to say you can't remove yourself", msg)
	}
	if msg := toastAt(2, func(w *sim.World) { w.Session.Players[2].Role = "admin" }); !strings.Contains(msg, "only the host") {
		t.Errorf("admin [x] on another admin toast = %q, want host-only", msg)
	}
	// A plain guest has no removal capability at all.
	w := sessionWorld(t, false)
	s := NewSessionScreen(sessionTheme())
	s.HandleKey(w, key("j"))
	if cmd := s.HandleKey(w, key("x")); cmd.Kind != SessionCmdToast || !strings.Contains(cmd.Message, "admin") {
		t.Errorf("guest [x] = %+v, want a toast naming the required capability", cmd)
	}
}

func TestSessionScreenRestartRefusalToast(t *testing.T) {
	w := sessionWorld(t, false) // plain guest: no admin capability
	s := NewSessionScreen(sessionTheme())
	cmd := s.HandleKey(w, key("u"))
	if cmd.Kind != SessionCmdToast || !strings.Contains(cmd.Message, "restart") {
		t.Errorf("guest [u] = %+v, want a toast explaining the refusal", cmd)
	}
	if s.confirmRestart {
		t.Error("guest [u] armed the restart confirm")
	}
}

// dimCSI wraps text in a real dim/reset pair, hand-built so the test
// doesn't depend on lipgloss's environment-dependent color profile
// (same technique as the navball panel tests).
func dimCSI(s string) string { return "\x1b[2m" + s + "\x1b[0m" }

// displayCol reports where needle starts in TERMINAL CELLS, not bytes —
// the roster's markers (▸) and presence dots (●/○) are multi-byte runes,
// so a byte offset says nothing about alignment.
func displayCol(t *testing.T, line, needle string) int {
	t.Helper()
	i := strings.Index(line, needle)
	if i < 0 {
		t.Fatalf("%q not found in %q", needle, line)
	}
	return lipgloss.Width(line[:i])
}

// padCell must pad to a display width, so a styled cell occupies the
// same number of columns as a plain one. This is the actual defect
// behind the playtest report: fmt's %-32s counts the invisible ANSI
// bytes of a styled tag against the column budget, so promoting a
// player (which adds a styled "(admin)") shifted every column after it.
func TestPadStyledIgnoresANSI(t *testing.T) {
	plain := padStyled("gern", 20)
	styled := padStyled("gern"+dimCSI(" (admin)"), 20)
	if lipgloss.Width(plain) != 20 || lipgloss.Width(styled) != 20 {
		t.Errorf("padCell widths = plain %d, styled %d, want 20 and 20",
			lipgloss.Width(plain), lipgloss.Width(styled))
	}
}

// Over-long content is truncated into its column instead of pushing the
// rest of the row right.
func TestTruncWidthFitsColumn(t *testing.T) {
	got := truncWidth(strings.Repeat("verylonghandle", 4), 20)
	if lipgloss.Width(got) > 20 {
		t.Errorf("truncPlain width = %d, want <= 20 (%q)", lipgloss.Width(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated text should be ellipsised, got %q", got)
	}
	if short := truncWidth("gern", 20); short != "gern" {
		t.Errorf("truncPlain shortened a fitting string: %q", short)
	}
}

// The roster is a fixed-width table: every row puts its columns in the
// same terminal cell, whatever the name cell holds (v0.30.1, playtest).
func TestSessionScreenColumnsStayAligned(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	rowFor := func(w *sim.World, handle string) string {
		for _, line := range strings.Split(stripANSI(s.Render(w, 140)), "\n") {
			if strings.Contains(line, handle) {
				return line
			}
		}
		t.Fatalf("no roster row for %q", handle)
		return ""
	}

	// A tagged row (jason — "host, you") and an untagged one (dave) must
	// agree on where the location column starts.
	w := sessionWorld(t, true)
	tagged := displayCol(t, rowFor(w, "jason"), "Sol/earth")
	plain := displayCol(t, rowFor(w, "dave"), "Lumen/lumen")
	if tagged != plain {
		t.Errorf("location column: tagged row at cell %d, untagged at %d — table is ragged", tagged, plain)
	}

	// The reported case: promoting must not move gern's columns.
	before := displayCol(t, rowFor(w, "gern"), "Sol/moon")
	beforeCraft := displayCol(t, rowFor(w, "gern"), "1 craft")
	w.Session.Players[1].Role = "admin"
	if after := displayCol(t, rowFor(w, "gern"), "Sol/moon"); before != after {
		t.Errorf("promoting shifted the location column: cell %d → %d", before, after)
	}
	if after := displayCol(t, rowFor(w, "gern"), "1 craft"); beforeCraft != after {
		t.Errorf("promoting shifted the craft column: cell %d → %d", beforeCraft, after)
	}

	// An over-long handle is truncated into its column, not allowed to
	// push the row's remaining columns right.
	w2 := sessionWorld(t, true)
	w2.Session.Players[1].Handle = strings.Repeat("verylonghandle", 4)
	if got, want := displayCol(t, rowFor(w2, "Sol/moon"), "Sol/moon"), plain; got != want {
		t.Errorf("a long handle broke the table: location at cell %d, want %d", got, want)
	}
}

// The invites table holds its columns too, whatever the code length.
func TestSessionScreenInviteColumnsStayAligned(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Session.Invites = []sim.SessionInvite{
		{Code: "AB2C-DE3F", Handle: "newbie", Age: 3 * time.Minute},
		{Code: "SHORT", Handle: "second", Age: time.Minute},
	}
	lines := strings.Split(stripANSI(s.Render(w, 140)), "\n")
	var a, b string
	for _, l := range lines {
		if strings.Contains(l, "newbie") {
			a = l
		}
		if strings.Contains(l, "second") {
			b = l
		}
	}
	if x, y := displayCol(t, a, "newbie"), displayCol(t, b, "second"); x != y {
		t.Errorf("invite handle column is ragged: cell %d vs %d\n  %q\n  %q", x, y, a, b)
	}
}

// Your own row must never carry the "(N here)" annotation (v0.30.1).
// The ghost slate deliberately excludes the viewer — you don't ghost
// yourself — so playerGhosts is always empty for your own fingerprint,
// and the S6 annotation read "2 craft (0 here)" on every self row with
// craft. The count is about what YOU can target; it is meaningless
// pointed at yourself.
func TestSessionScreenSelfRowHasNoTargetableCount(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true) // jason is self, 2 craft, in Sol
	out := stripANSI(s.Render(w, 140))
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "jason") && strings.Contains(line, "here)") {
			t.Errorf("self row claims a targetable count:\n  %q", line)
		}
	}
	if !strings.Contains(out, "2 craft") {
		t.Errorf("self row lost its craft count:\n%s", out)
	}
}
