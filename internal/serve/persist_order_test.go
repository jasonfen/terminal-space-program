package serve

import (
	"context"
	"testing"

	"github.com/charmbracelet/ssh"

	"github.com/jasonfen/terminal-space-program/internal/sessiondir"
	"github.com/jasonfen/terminal-space-program/internal/tui"
)

// hookCtx is a session context whose Value lookup runs a hook, so a test can
// act at an exact point of persistMiddleware's teardown.
type hookCtx struct {
	ssh.Context
	vals map[any]any
	on   func(key any)
}

func (c *hookCtx) Value(k any) any {
	if c.on != nil {
		c.on(k)
	}
	return c.vals[k]
}
func (c *hookCtx) SetValue(k, v any) { c.vals[k] = v }

type hookSess struct {
	ssh.Session
	ctx ssh.Context
}

func (s hookSess) Context() ssh.Context { return s.ctx }

var _ context.Context = (*hookCtx)(nil)

// TestTeardownKeepsOwnerOnlineUntilFinalPayloadIsWritten (audit #10b): the
// player went offline before their last world was saved, so a guest's empty-
// seat [J] landing in between lifted the stack out of the STALE payload and
// then the final write put it back: one stack, two pilots. The hook fires at
// the moment the teardown reads the app (the step right before SavePlayer);
// the reclaim must still be refused there.
func TestTeardownKeepsOwnerOnlineUntilFinalPayloadIsWritten(t *testing.T) {
	const ownerFP = "SHA256:gern"
	srv := newOfflineServer(t)
	enrollDirect(t, srv, ownerFP, "gern")
	rec := parkedStackFor(t, srv, ownerFP, sessiondir.HostFingerprint)
	app, err := tui.New(nil)
	if err != nil {
		t.Fatalf("tui.New: %v", err)
	}
	srv.presence.markOnline(ownerFP)

	var reclaimErr error
	fired := false
	ctx := &hookCtx{vals: map[any]any{
		ctxKeyFingerprint: ownerFP, ctxKeyJoined: true, ctxKeyApp: app,
	}}
	ctx.on = func(k any) {
		if k == ctxKeyApp && !fired {
			fired = true
			reclaimErr = srv.reclaimFromEmptySeat(rec, sessiondir.HostFingerprint)
		}
	}
	srv.persistMiddleware(func(ssh.Session) {})(hookSess{ctx: ctx})

	if !fired {
		t.Fatalf("hook never fired: the teardown no longer reads the app")
	}
	if reclaimErr == nil {
		t.Errorf("a reclaim was granted before the owner's final payload was written")
	}
}
