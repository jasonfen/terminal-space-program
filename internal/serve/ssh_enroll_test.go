package serve

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var simDateRE = regexp.MustCompile(`T\+(\d{4}-\d{2}-\d{2})`)

func lastSimDate(out string) string {
	m := simDateRE.FindAllStringSubmatch(stripANSI(out), -1)
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1][1]
}

// lastWarp parses the most recently rendered warp chip ("warp 100x"
// → "100"). The LAST match is the newest frame, so laddering warp up
// or down can wait on the live value — plain substring matching can't
// ("warp 100000x" contains "warp 10000x").
var warpRE = regexp.MustCompile(`warp (\d+)x`)

func lastWarp(out string) string {
	m := warpRE.FindAllStringSubmatch(stripANSI(out), -1)
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1][1]
}

// The S3 enroll-flow integration test (v0.27 plan): invite → card →
// code → handle → game; disconnect persists the player's world;
// reconnect by key skips the flow and resumes the same program.
func TestSSHEnrollFlow(t *testing.T) {
	srv := startTestServer(t)
	signer, fp := newClientKey(t)

	inv, err := srv.store.MintInvite("dave")
	if err != nil {
		t.Fatalf("MintInvite: %v", err)
	}

	// --- First connect: full enroll flow.
	sess := dialGameSession(t, srv.Addr(), signer)
	sess.waitFor(t, "[y/n]") // calibration card
	mustWrite(t, sess, "y")
	sess.waitFor(t, "invite code:")
	mustWrite(t, sess, inv.Code+"\r")
	sess.waitFor(t, "your handle:")
	sess.waitFor(t, "dave") // pre-bound handle offered for editing
	mustWrite(t, sess, "\r")
	sess.waitFor(t, "Sol") // in the game

	if p, err := srv.store.FindPlayer(fp); err != nil || p.Handle != "dave" {
		t.Fatalf("roster after enroll: %+v, %v", p, err)
	}

	// Make the world distinctive: warp hard until the sim date rolls,
	// then drop back to 1x so the date is stable for comparison.
	d0 := ""
	sess.waitUntil(t, "a sim date in the HUD", func(out string) bool {
		d0 = lastSimDate(out)
		return d0 != ""
	})
	// Ladder the warp one confirmed step at a time — a byte burst
	// coalesces into one multi-rune KeyMsg and matches nothing, and on
	// slow runners even spaced writes can outrun the render loop.
	for _, want := range []string{"10", "100", "1000", "10000", "100000"} {
		mustWrite(t, sess, ".")
		sess.waitUntil(t, "warp chip "+want+"x", func(out string) bool {
			return lastWarp(out) == want
		})
	}
	sess.waitUntil(t, "sim date to advance under warp", func(out string) bool {
		d := lastSimDate(out)
		return d != "" && d != d0
	})
	for _, want := range []string{"10000", "1000", "100", "10", "1"} { // back down to 1x
		mustWrite(t, sess, ",")
		sess.waitUntil(t, "warp chip "+want+"x", func(out string) bool {
			return lastWarp(out) == want
		})
	}
	time.Sleep(150 * time.Millisecond) // let a settled 1x frame land
	advanced := lastSimDate(sess.output())
	if advanced == "" || advanced == d0 {
		t.Fatalf("sim date did not advance (d0=%q advanced=%q)", d0, advanced)
	}

	// Quit with ctrl+c: the guest sink persists; the session ends.
	mustWrite(t, sess, "\x03")
	deadline := time.Now().Add(10 * time.Second)
	for !srv.store.HasPayload(fp) {
		if time.Now().After(deadline) {
			t.Fatal("player payload never appeared after quit")
		}
		time.Sleep(25 * time.Millisecond)
	}

	// --- Reconnect with the same key: no card, no code — resume.
	sess2 := dialGameSession(t, srv.Addr(), signer)
	sess2.waitFor(t, "Sol")
	out := stripANSI(sess2.output())
	if strings.Contains(out, "[y/n]") || strings.Contains(out, "invite code:") {
		t.Error("reconnect ran the enroll flow again")
	}
	sess2.waitUntil(t, "resumed sim date "+advanced, func(o string) bool {
		return lastSimDate(o) == advanced
	})
}

func mustWrite(t *testing.T, g *gameSession, s string) {
	t.Helper()
	if _, err := g.stdin.Write([]byte(s)); err != nil {
		t.Fatalf("write %q: %v", s, err)
	}
}
