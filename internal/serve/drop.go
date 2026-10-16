package serve

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// ADR 0054 decisions 4 to 6: the drop. One mechanism ends a live session
// from outside it, for a disconnect, a kick, stop hosting, a restart and
// the host's own quit.
//
// A drop writes a dropRecord on the target's liveSession and nothing else.
// The session's own wrapper reads it on its next tick and returns tea.Quit,
// so the program unwinds on its own goroutine and restores the terminal;
// persistMiddleware then prints the line, saves the world and returns. A
// peer that is not draining can hang that unwind (the renderer's stop() is
// a blocked write), so each drop also arms a watchdog: after dropWait the
// connection is closed under it.

// defaultDropWait is how long a dropped session gets to unwind cleanly
// before its connection is closed (Jason's confirmation, call 4). Forty
// ticks, ample for a draining peer (ADR 0036 measured a clean exit at
// 20 ms), short enough not to hold a restart for a peer that is not there.
const defaultDropWait = 2 * time.Second

type dropKind int

const (
	dropDisconnect dropKind = iota + 1
	dropKick
	dropStopHosting
	dropRestart
)

// dropRecord says why a session is ending. actor is the roster handle of
// whoever pressed the key; target is the dropped player's handle, carried
// here because a kick removes the roster row the chip would look it up in.
type dropRecord struct {
	kind   dropKind
	actor  string
	target string
	// purge is the kick-and-delete-vessels flag. The teardown writes no final
	// payload when it is set, so a late write cannot resurrect the file the
	// purge deletes (call 3). The purge itself is slice E.
	purge bool
}

const linePrefix = "terminal-space-program: "

// line is the one line the dropped friend is left with.
func (r *dropRecord) line() string {
	switch r.kind {
	case dropDisconnect:
		return linePrefix + "disconnected by " + r.actor + " — reconnect any time, your progress is saved"
	case dropKick:
		if r.purge {
			return linePrefix + "kicked from this session by " + r.actor + " — ask them for a new invite"
		}
		return linePrefix + "kicked from this session by " + r.actor + " — ask them for a new invite; your vessels are saved"
	case dropStopHosting:
		return linePrefix + r.actor + " stopped hosting — reconnect when they host again; your progress is saved"
	case dropRestart:
		return linePrefix + "server restarting — reconnect in a moment; your progress is saved"
	}
	return ""
}

// chipKind is the departure the others see. Stop hosting and restart end
// the room, they are not a verdict on a player, so they keep the ordinary
// kinds (ok false).
func (r *dropRecord) chipKind() (sim.SessionEventKind, bool) {
	switch r.kind {
	case dropDisconnect:
		return sim.SessionEventDisconnectedBy, true
	case dropKick:
		return sim.SessionEventKickedBy, true
	}
	return 0, false
}

// dropSession flags ls as dropped and arms the dead-peer watchdog. The
// first drop wins: a session already ending keeps its first reason.
func (s *Server) dropSession(ls *liveSession, rec *dropRecord) bool {
	if ls == nil || !ls.dropped.CompareAndSwap(nil, rec) {
		return false
	}
	go func() {
		select {
		case <-ls.done: // nil done blocks forever: only the watchdog applies
		case <-s.after(s.dropWait):
			if ls.conn != nil {
				_ = ls.conn.Close()
			}
		}
	}()
	return true
}

// dropPlayer drops fp's live session, if it has one.
func (s *Server) dropPlayer(fp string, rec *dropRecord) bool {
	ls, ok := s.live.get(fp)
	if !ok {
		return false
	}
	return s.dropSession(ls, rec)
}

// dropAll drops every live session with rec and returns once each has
// unwound, bounded so one stuck session cannot hold a restart.
func (s *Server) dropAll(rec *dropRecord) {
	snap := s.live.snapshot()
	for _, ls := range snap {
		r := *rec
		s.dropSession(ls, &r)
	}
	limit := s.after(2 * s.dropWait)
	for _, ls := range snap {
		if ls.done == nil {
			continue
		}
		select {
		case <-ls.done:
		case <-limit:
			return
		}
	}
}
