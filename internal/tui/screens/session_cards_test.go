package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 slice C (#616 UI, #633): the roster's four asks are cards. These
// guards drive the real HandleKey and read the card the screen exposes, so
// they measure what the player is shown and what the screen answers.

// press feeds keys in order and returns the last command.
func press(s *SessionScreen, w *sim.World, keys ...string) SessionCommand {
	var cmd SessionCommand
	for _, k := range keys {
		cmd = s.HandleKey(w, key(k))
	}
	return cmd
}

// cardText is the active card rendered with ANSI stripped, "" when none.
func cardText(s *SessionScreen) string {
	c := s.ActiveCard()
	if c == nil {
		return ""
	}
	return ansi.Strip(c.Render())
}

// cardFlat is the card's text with the box edges dropped and the wrapped
// lines joined, so a note that wraps still reads as one sentence.
func cardFlat(s *SessionScreen) string {
	return strings.Join(strings.Fields(strings.NewReplacer("│", " ", "╭", " ", "╮", " ", "╰", " ", "╯", " ", "─", " ").Replace(cardText(s))), " ")
}

// onGuest moves the cursor to gern (row 1), the online guest.
func onGuest(s *SessionScreen, w *sim.World) { press(s, w, "j") }

func TestRosterKickCardHasThreeRowsAndOpensOnKick(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	onGuest(s, w)
	if cmd := press(s, w, "x"); cmd.Kind != SessionCmdNone {
		t.Fatalf("[x] acted before the card: %+v", cmd)
	}
	out := cardText(s)
	for _, want := range []string{"Kick gern?", "kick", "kick and delete vessels", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("kick card missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "remove") {
		t.Errorf("the word is kick, never remove:\n%s", out)
	}
	if strings.Contains(s.Render(w, 140, 40), "[y/n]") {
		t.Error("a [y/n] line is still on the roster")
	}
	// The ▸ opens on the first row, never on the delete row.
	for _, ln := range strings.Split(out, "\n") {
		if strings.Contains(ln, "▸") && !strings.Contains(ln, "kick") {
			t.Errorf("▸ is not on kick: %q", ln)
		}
		if strings.Contains(ln, "▸") && strings.Contains(ln, "delete vessels") {
			t.Errorf("card opens on the delete row: %q", ln)
		}
	}
}

func TestRosterKickAsksTwiceThenKicksWithoutPurge(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	onGuest(s, w)
	press(s, w, "x")
	if cmd := press(s, w, "y"); cmd.Kind != SessionCmdNone {
		t.Fatalf("first y acted at once (call 1: every action row asks again): %+v", cmd)
	}
	if s.ActiveCard() == nil || !strings.Contains(cardText(s), "yes, kick") {
		t.Fatalf("no confirm step after y:\n%s", cardText(s))
	}
	cmd := press(s, w, "y")
	if cmd.Kind != SessionCmdKick || cmd.Purge || cmd.Fingerprint != "SHA256:guest" || cmd.Handle != "gern" {
		t.Errorf("second y = %+v, want a plain kick of gern", cmd)
	}
	if s.ActiveCard() != nil {
		t.Error("card still up after the answer")
	}
	// enter alone kicks without purging: enter, then up (to the yes row), enter.
	onGuestCard := func() {
		s = NewSessionScreen(sessionTheme())
		onGuest(s, w)
		press(s, w, "x")
	}
	onGuestCard()
	press(s, w, "enter") // picks kick -> confirm step, ▸ on cancel
	if cmd := press(s, w, "enter"); cmd.Kind != SessionCmdNone {
		t.Errorf("enter on the confirm step's way out acted: %+v", cmd)
	}
	onGuestCard()
	press(s, w, "enter", "up")
	if cmd := press(s, w, "enter"); cmd.Kind != SessionCmdKick || cmd.Purge {
		t.Errorf("enter,up,enter = %+v, want a plain kick", cmd)
	}
}

func TestRosterPurgeRowTakesDownAndEnterOnly(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	onGuest(s, w)
	press(s, w, "x", "down", "enter")
	if !strings.Contains(cardText(s), "yes, kick and delete vessels") {
		t.Fatalf("no confirm step on the purge row:\n%s", cardText(s))
	}
	cmd := press(s, w, "up", "enter")
	if cmd.Kind != SessionCmdKick || !cmd.Purge || cmd.Fingerprint != "SHA256:guest" {
		t.Errorf("down,enter,up,enter = %+v, want kick with Purge", cmd)
	}
}

func TestRosterPurgeRowHasNoLetter(t *testing.T) {
	// Every letter a player could reach for on the first step must leave the
	// purge row alone: y kicks, n and esc cancel, anything else is swallowed.
	for _, k := range []string{"y", "Y", "n", "N", "esc", "d", "p", "x", "delete", "K"} {
		s := NewSessionScreen(sessionTheme())
		w := sessionWorld(t, true)
		onGuest(s, w)
		press(s, w, "x")
		press(s, w, k)
		cmd := press(s, w, k) // a second press confirms whatever the first picked
		if cmd.Kind == SessionCmdKick && cmd.Purge {
			t.Errorf("key %q purged vessels", k)
		}
	}
}

func TestRosterPurgeIsNeverRefusedOrGreyedWhileDocked(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Session.Players[1].DockedGuest = true
	onGuest(s, w)
	press(s, w, "x")
	out := cardText(s)
	if !strings.Contains(out, "kick and delete vessels") {
		t.Fatalf("purge row missing for a docked friend:\n%s", out)
	}
	cmd := press(s, w, "down", "enter", "up", "enter")
	if cmd.Kind != SessionCmdKick || !cmd.Purge {
		t.Errorf("purge for a docked friend = %+v, want it to go through", cmd)
	}
}

func TestRosterKickCardCancelAndSwallow(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	onGuest(s, w)
	press(s, w, "x")
	// Roster keys do nothing under the card, and the screen stays open.
	for _, k := range []string{"t", "v", "s", "w", "i", "p", "h", "f4", "q", "O", "tab", "x", "d"} {
		if cmd := press(s, w, k); cmd.Kind != SessionCmdNone {
			t.Errorf("[%s] under the card acted: %+v", k, cmd)
		}
		if s.ActiveCard() == nil {
			t.Fatalf("[%s] closed the card", k)
		}
	}
	for _, k := range []string{"n", "esc"} {
		s = NewSessionScreen(sessionTheme())
		onGuest(s, w)
		press(s, w, "x")
		if cmd := press(s, w, k); cmd.Kind != SessionCmdNone || s.ActiveCard() != nil {
			t.Errorf("[%s] = %+v card=%v, want the card gone and nothing sent", k, cmd, s.ActiveCard() != nil)
		}
	}
	// A cancel from the confirm step acts at once too.
	s = NewSessionScreen(sessionTheme())
	onGuest(s, w)
	press(s, w, "x", "y")
	if cmd := press(s, w, "n"); cmd.Kind != SessionCmdNone || s.ActiveCard() != nil {
		t.Errorf("n on the confirm step = %+v", cmd)
	}
}

func TestRosterDisconnectCard(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	onGuest(s, w)
	if cmd := press(s, w, "d"); cmd.Kind != SessionCmdNone {
		t.Fatalf("[d] acted before the card: %+v", cmd)
	}
	out := cardText(s)
	for _, want := range []string{"Disconnect gern?", "disconnect", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("disconnect card missing %q:\n%s", want, out)
		}
	}
	press(s, w, "y")
	cmd := press(s, w, "y")
	if cmd.Kind != SessionCmdDisconnect || cmd.Fingerprint != "SHA256:guest" || cmd.Handle != "gern" {
		t.Errorf("d,y,y = %+v, want a disconnect of gern", cmd)
	}
	s = NewSessionScreen(sessionTheme())
	onGuest(s, w)
	if cmd := press(s, w, "d", "n"); cmd.Kind != SessionCmdNone || s.ActiveCard() != nil {
		t.Errorf("d,n = %+v", cmd)
	}
}

func TestRosterDisconnectAndKickRefusalsToastBeforeAnyCard(t *testing.T) {
	type tc struct {
		name  string
		world func() *sim.World
		down  int
		want  string
	}
	adminWorld := func() *sim.World {
		w := sessionWorld(t, false) // viewer is gern
		w.Session.CanAdminister = true
		w.Session.Players[2].Role = "admin" // dave
		w.Session.Players[2].Online = true
		return w
	}
	cases := []tc{
		{"host row", adminWorld, 0, "host"},
		{"self", func() *sim.World { return adminWorld() }, 1, "yourself"},
		{"admin by admin", adminWorld, 2, "only the host"},
		{"guest seat", func() *sim.World { return sessionWorld(t, false) }, 2, "admin"},
	}
	for _, c := range cases {
		for _, k := range []string{"d", "x"} {
			s := NewSessionScreen(sessionTheme())
			w := c.world()
			for i := 0; i < c.down; i++ {
				press(s, w, "j")
			}
			cmd := press(s, w, k)
			if cmd.Kind != SessionCmdToast || !strings.Contains(cmd.Message, c.want) {
				t.Errorf("%s [%s] = %+v, want a toast naming %q", c.name, k, cmd, c.want)
			}
			if s.ActiveCard() != nil {
				t.Errorf("%s [%s] opened a card", c.name, k)
			}
			if strings.Contains(cmd.Message, "remove") {
				t.Errorf("%s [%s] toast says remove: %q", c.name, k, cmd.Message)
			}
		}
	}
}

func TestRosterDisconnectOfAnOfflinePlayerToasts(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	press(s, w, "j", "j") // dave, offline
	cmd := press(s, w, "d")
	if cmd.Kind != SessionCmdToast || !strings.Contains(cmd.Message, "dave") || s.ActiveCard() != nil {
		t.Errorf("[d] on an offline player = %+v card=%v", cmd, s.ActiveCard() != nil)
	}
	// A kick works on an offline player: it ends their key.
	if press(s, w, "x"); s.ActiveCard() == nil {
		t.Error("[x] refused an offline player")
	}
}

func TestRosterCardNotes(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	onGuest(s, w)
	press(s, w, "d")
	if strings.Contains(cardText(s), "docked") || strings.Contains(cardText(s), "rendezvous") {
		t.Errorf("a note with nothing to say:\n%s", cardText(s))
	}
	for _, k := range []string{"d", "x"} {
		s = NewSessionScreen(sessionTheme())
		w = sessionWorld(t, true)
		w.Session.Players[1].DockedGuest = true
		onGuest(s, w)
		press(s, w, k)
		if !strings.Contains(cardFlat(s), "they are docked with you: the dock stands until you release it") {
			t.Errorf("[%s] dock note missing:\n%s", k, cardText(s))
		}
		s = NewSessionScreen(sessionTheme())
		w = sessionWorld(t, true)
		w.Session.Players[1].RendezvousOut = true
		onGuest(s, w)
		press(s, w, k)
		if !strings.Contains(cardFlat(s), "in a rendezvous with jason: it is cancelled") {
			t.Errorf("[%s] rendezvous note missing:\n%s", k, cardText(s))
		}
	}
}

func TestRosterStopHostingCard(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true) // one online guest
	if cmd := press(s, w, "h"); cmd.Kind != SessionCmdNone {
		t.Fatalf("[h] acted before the card: %+v", cmd)
	}
	out := cardFlat(s)
	for _, want := range []string{"Stop hosting?", "drops 1 guest, their progress is saved", "stop hosting", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("stop-hosting card missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(s.Render(w, 140, 40), "stop hosting? drops") {
		t.Error("the NOTES-box ask is still drawn")
	}
	press(s, w, "y")
	if cmd := press(s, w, "y"); cmd.Kind != SessionCmdStopHost {
		t.Errorf("h,y,y = %+v, want StopHost", cmd)
	}
	w.Session.Players[1].Online = false
	s = NewSessionScreen(sessionTheme())
	press(s, w, "h")
	if !strings.Contains(cardFlat(s), "drops 0 guests") {
		t.Errorf("plural of zero:\n%s", cardText(s))
	}
	if cmd := press(s, w, "esc"); cmd.Kind != SessionCmdNone || s.ActiveCard() != nil {
		t.Errorf("esc = %+v", cmd)
	}
}

func TestRosterRestartCard(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	if cmd := press(s, w, "f4"); cmd.Kind != SessionCmdNone {
		t.Fatalf("[F4] acted before the card: %+v", cmd)
	}
	out := cardFlat(s)
	for _, want := range []string{"Restart the server?", "drops 1 player, they reconnect; progress is saved", "restart", "cancel"} {
		if !strings.Contains(out, want) {
			t.Errorf("restart card missing %q:\n%s", want, out)
		}
	}
	press(s, w, "y")
	if cmd := press(s, w, "y"); cmd.Kind != SessionCmdRestart {
		t.Errorf("F4,y,y = %+v, want Restart", cmd)
	}
	// Adopt variant.
	w.Session.AvailableVersion, w.Session.AdoptCapable = "1.2.3", true
	s = NewSessionScreen(sessionTheme())
	press(s, w, "f4")
	if !strings.Contains(cardText(s), "Restart to adopt v1.2.3?") {
		t.Errorf("adopt card title:\n%s", cardText(s))
	}
	press(s, w, "y")
	if cmd := press(s, w, "y"); cmd.Kind != SessionCmdRestart {
		t.Errorf("adopt F4,y,y = %+v", cmd)
	}
}

func TestRosterLongHandleIsCutInTheCardTitle(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	w.Session.Players[1].Handle = strings.Repeat("w", 60)
	onGuest(s, w)
	press(s, w, "x")
	for _, ln := range strings.Split(cardText(s), "\n") {
		if len([]rune(ln)) > CardW {
			t.Errorf("card line wider than the card: %q", ln)
		}
	}
	if !strings.Contains(cardText(s), "?") {
		t.Error("title lost its ?")
	}
}

func TestRosterEnterWithNoCardDoesNothing(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	if cmd := press(s, w, "enter"); cmd.Kind != SessionCmdNone || s.ActiveCard() != nil {
		t.Errorf("enter with no card = %+v", cmd)
	}
}

// A card never outlives the session it asks about: when the roster stops being
// a session (stop hosting finished, a drop) the card goes with it.
func TestRosterCardClearsWhenTheSessionEnds(t *testing.T) {
	s := NewSessionScreen(sessionTheme())
	w := sessionWorld(t, true)
	onGuest(s, w)
	press(s, w, "x")
	if s.ActiveCard() == nil {
		t.Fatal("setup: no card")
	}
	w.Session = nil
	s.Render(w, 140, 40)
	if s.ActiveCard() != nil {
		t.Error("the kick card is still up after the session ended")
	}
}
