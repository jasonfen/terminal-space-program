package serve

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// actorHandle is the roster handle of whoever pressed the key.
func (m *reportingModel) actorHandle() string {
	if h, ok := m.handleOf(m.owner); ok {
		return h
	}
	return m.srv.handleOf(m.owner)
}

// disconnectPlayer ends fp's live session; their key stays valid and they
// may reconnect at once. Authorization (host and admins) was checked by the
// caller; the actor-by-target rules are MayRemove's, the same ones a kick
// answers to.
func (m *reportingModel) disconnectPlayer(fp string) {
	if !m.srv.store.MayRemove(m.owner, fp) {
		m.app.Toast("you can't disconnect that player")
		return
	}
	target := m.srv.handleOf(fp)
	if !m.srv.dropPlayer(fp, &dropRecord{kind: dropDisconnect, actor: m.actorHandle(), target: target}) {
		m.app.Toast(target + " is not connected")
	}
}

// kickPlayer drops fp's live session and removes their roster row, so only
// a freshly minted invite lets them back. The drop is flagged first (call 3:
// the purge flag must be on the session before the roster edit). Their
// vessels stay saved, unless purge is set, in which case the returned Cmd
// finishes the delete and reports it to the host.
func (m *reportingModel) kickPlayer(fp string, purge bool) tea.Cmd {
	// Target-aware guardrail (v0.30 S3): an admin may kick guests but not the
	// host, another admin, or themselves. MayAdminister passed in the caller;
	// MayRemove adds the actor-by-target rules.
	if !m.srv.store.MayRemove(m.owner, fp) {
		m.app.Toast("you can't kick that player")
		return nil
	}
	if purge {
		return m.purgeKickedPlayer(fp)
	}
	target := m.srv.handleOf(fp)
	m.srv.dropPlayer(fp, &dropRecord{kind: dropKick, actor: m.actorHandle(), target: target})
	_ = m.srv.store.RemovePlayer(fp)
	return nil
}

// purgeDoneMsg is the host's receipt for a kick and delete vessels.
type purgeDoneMsg struct {
	handle string
	backup string // backup path, empty when there was nothing to back up
	err    error
}

// text is the one toast line the host gets.
func (d purgeDoneMsg) text() string {
	switch {
	case d.err != nil:
		return "kicked " + d.handle + ", vessels not deleted: " + d.err.Error()
	case d.backup == "":
		return "kicked " + d.handle + ", no saved vessels to delete"
	}
	return "backed up " + d.handle + "'s vessels to backup/" + filepath.Base(d.backup)
}

// purgeKickedPlayer is kick and delete vessels (ADR 0054 slice E). The
// flag goes on the live session first (call 3), then the roster row goes,
// then the returned Cmd waits for the session to unwind and finishes the
// purge off the host's update loop: the wait can last a dropWait.
func (m *reportingModel) purgeKickedPlayer(fp string) tea.Cmd {
	s := m.srv
	target := s.handleOf(fp)
	rec := &dropRecord{kind: dropKick, actor: m.actorHandle(), target: target, purge: true}
	ls, live := s.live.get(fp)
	if live {
		if !s.dropSession(ls, rec) {
			// Already ending for another reason (a disconnect a moment ago):
			// it must still not write the payload the purge is about to
			// delete, and its line must say what happened to them now.
			if prev := ls.dropped.Load(); prev != nil && !prev.purge {
				ls.dropped.Store(rec)
			}
		}
	}
	_ = s.store.RemovePlayer(fp)
	return func() tea.Msg {
		if live && ls.done != nil {
			select {
			case <-ls.done:
			case <-s.after(2 * s.dropWait):
			}
		}
		return s.finishPurge(fp, target, ls)
	}
}

// finishPurge does the deleting, once the player's session has unwound (or
// the wait ran out): their saved program is backed up and removed, the
// friends' vessels riding in their stack are handed back, every dock record
// naming them is rewritten or closed, and their report is forgotten so no
// ghost of theirs is rebuilt. ls is nil when they were not connected.
func (s *Server) finishPurge(fp, handle string, ls *liveSession) purgeDoneMsg {
	defer s.admit.enter(fp)()
	done := purgeDoneMsg{handle: handle}

	// A stack of theirs with a friend's vessel inside is released on their own
	// last tick when they were connected. Anything that did not happen there
	// (they were offline, or the session never ticked) is done against their
	// saved program, read BEFORE the delete.
	var offline *sim.World
	if ls == nil || !ls.purgeHandled.Load() {
		if w, err := s.store.LoadPlayer(fp); err == nil {
			offline = w
		} else if errors.Is(err, fs.ErrNotExist) {
			// No program is repaired as an empty one (ADR 0040 amendment): a
			// fuse they never saved hands the friend's Vessel back.
			offline, _ = emptyWorld()
		} else {
			done.err = fmt.Errorf("their saved program could not be read: %w", err)
		}
	}

	// Friends' vessels first, the delete last (review 1, F5). Everything the
	// delete would orphan is handed back and flushed to session.json before
	// the payload goes, so a crash in between leaves their program intact and
	// the purge simply runs again; it can never leave a friend's vessel only
	// in backup/. The ledger and ghost are cleaned whatever happens to the
	// file: a kicked player must not stay named by a record or drawn on a map.
	if offline != nil {
		// A payload older than the ledger is made to agree first, so the
		// friend's Vessel is peeled out of the Composite it is really in.
		s.dock.Repair(offline, fp)
		s.dock.ReleaseGuestsOf(offline, fp, s.presence.isOnline)
	}
	s.dock.DropRecordsNaming(fp)
	s.relay.Forget(fp)
	flushErr := s.persistDocks()
	if flushErr != nil {
		s.markDockFlushFailed(flushErr)
	}
	if flushErr != nil && offline != nil {
		// The released vessels are not durable: deleting the payload now
		// would leave them only in backup/. Keep it; the purge reports why.
		if done.err == nil {
			done.err = fmt.Errorf("the dock ledger could not be saved, so their saved program was kept: %w", flushErr)
		}
		return done
	}

	if s.purgeBeforeDelete != nil {
		s.purgeBeforeDelete()
	}
	res, err := s.store.PurgePlayer(fp)
	if err != nil && done.err == nil {
		done.err = err
	}
	done.backup = res.BackupPath
	return done
}
