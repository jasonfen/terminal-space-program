package serve

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/tui"
	"github.com/jasonfen/terminal-space-program/internal/tui/screens"
)

// ADR 0054 slice E: kick and delete vessels. The tests read the far end of
// each promise: the file on disk, the backup's bytes, the report store, the
// ledger as persisted, the friend's world.

// namedInLedger lists every place the live ledger and the persisted docks
// list still name fp or handle.
func namedInLedger(t *testing.T, srv *Server, fp, handle string) []string {
	t.Helper()
	var out []string
	for _, r := range srv.dock.FullRecords() {
		if r.Owner == fp || r.GuestOwner == fp || r.OwnerHandle == handle || r.GuestHandle == handle {
			out = append(out, "live ledger")
		}
	}
	meta, err := srv.store.Meta()
	if err != nil {
		t.Fatalf("Meta: %v", err)
	}
	for _, d := range meta.Docks {
		if d.Owner == fp || d.GuestOwner == fp || d.OwnerHandle == handle || d.GuestHandle == handle {
			out = append(out, "session.json docks")
		}
	}
	return out
}

func (f *dropFixture) adminCmd(cmd screens.SessionCommand) tea.Cmd {
	var c tea.Cmd
	f.host, c = f.host.Update(screens.SessionAdminMsg{Cmd: cmd})
	return c
}

func reportedBy(srv *Server, fp string) bool {
	for _, r := range srv.relay.Snapshot("") {
		if r.Owner == fp {
			return true
		}
	}
	return false
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// A connected guest, purge-kicked from the roster: their line, the backup
// (the raw bytes of the payload that was on disk), the payload gone, the row
// gone, no report, no dock record, and the host's toast says where the backup
// is.
func TestPurgeKickBacksUpDeletesAndForgetsOverSSH(t *testing.T) {
	f := startDropFixture(t)
	waitFor(t, "the guest's first report", func() bool { return reportedBy(f.srv, f.gfp) })

	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.srv.store.SavePlayer(f.gfp, w); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(f.payloadFile())
	if err != nil {
		t.Fatal(err)
	}

	cmd := f.adminCmd(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: f.gfp, Handle: "pilot2", Purge: true})
	if cmd == nil {
		t.Fatal("a purge kick returned no follow-up: nothing would ever finish it")
	}
	f.waitGone(t, 2*time.Second, "purge kick")
	msg := cmd()
	done, ok := msg.(purgeDoneMsg)
	if !ok {
		t.Fatalf("purge finished with %T, want a purgeDoneMsg", msg)
	}
	f.host, _ = f.host.Update(msg)

	if done.err != nil {
		t.Fatalf("purge failed: %v", done.err)
	}
	if f.srv.store.HasPayload(f.gfp) {
		t.Error("players/<sha>.json is still there after the purge")
	}
	if done.backup == "" {
		t.Fatal("no backup reported")
	}
	after, err := os.ReadFile(done.backup)
	if err != nil {
		t.Fatalf("backup unreadable: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("backup is not byte-identical to the payload that was on disk (%d vs %d bytes)", len(after), len(before))
	}
	if filepath.Dir(done.backup) != filepath.Join(f.dir, "backup") {
		t.Errorf("backup at %s, want under %s/backup", done.backup, f.dir)
	}
	if want := "backed up pilot2's vessels to backup/" + filepath.Base(done.backup); done.text() != want {
		t.Errorf("host toast = %q, want %q", done.text(), want)
	}
	if _, err := f.srv.store.FindPlayer(f.gfp); err == nil {
		t.Error("the purged player is still on the roster")
	}
	if reportedBy(f.srv, f.gfp) {
		t.Error("the purged player still has a report in Snapshot(\"\"), so a ghost of them would be rebuilt")
	}
	if n := namedInLedger(t, f.srv, f.gfp, "pilot2"); len(n) != 0 {
		t.Errorf("something still names the purged player: %v", n)
	}
	want := "terminal-space-program: kicked from this session by " + f.hostHandle(t) + " — ask them for a new invite"
	if got := f.guest.stderr(); !strings.Contains(got, want) {
		t.Errorf("guest stderr = %q, want %q", got, want)
	}
	// Invites they minted stay: an invite has no owner field. Nothing to
	// assert but that the purge did not touch the invite list.
}

// The host's ghost list loses them on its next tick.
func TestPurgeKickEmptiesTheHostsGhostListNextTick(t *testing.T) {
	f := startDropFixture(t)
	waitFor(t, "the guest's first report", func() bool { return reportedBy(f.srv, f.gfp) })
	f.host = tick(f.host)
	hw := f.host.(reportingModel).app.World()
	if len(hw.Ghosts) == 0 {
		t.Fatal("control: the host sees no ghost of the connected guest, so an empty list proves nothing")
	}

	cmd := f.adminCmd(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: f.gfp, Handle: "pilot2", Purge: true})
	f.waitGone(t, 2*time.Second, "purge kick")
	f.host, _ = f.host.Update(cmd())
	f.host = tick(f.host)
	hw = f.host.(reportingModel).app.World()
	for _, g := range hw.Ghosts {
		if g.Owner == f.gfp {
			t.Errorf("the host still draws a ghost of the purged player: %+v", g)
		}
	}
}

// A plain kick is not a purge: payload kept, report kept (the ghost keeps its
// name), no backup. The toast path is untouched.
func TestPlainKickStillKeepsEverything(t *testing.T) {
	f := startDropFixture(t)
	waitFor(t, "the guest's first report", func() bool { return reportedBy(f.srv, f.gfp) })
	if cmd := f.adminCmd(screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: f.gfp, Handle: "pilot2"}); cmd != nil {
		t.Error("a plain kick returned a follow-up command")
	}
	f.waitGone(t, 2*time.Second, "kick")
	if !f.srv.store.HasPayload(f.gfp) {
		t.Error("a plain kick lost the payload")
	}
	if !reportedBy(f.srv, f.gfp) {
		t.Error("a plain kick forgot the report: the ghost would lose its name")
	}
	if _, err := os.Stat(filepath.Join(f.dir, "backup")); err == nil {
		t.Error("a plain kick made a backup folder")
	}
}

// A purge for a player who is not connected and has no payload still removes
// the row and says so.
func TestPurgeOfAnOfflinePlayerWithNoPayload(t *testing.T) {
	srv := newOfflineServer(t)
	const gfp = "SHA256:gern"
	enrollDirect(t, srv, gfp, "gern")
	app, _ := tui.New(nil)
	var host tea.Model = srv.HostModel(app)
	host, cmd := host.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{Kind: screens.SessionCmdKick, Fingerprint: gfp, Handle: "gern", Purge: true}})
	if cmd == nil {
		t.Fatal("no follow-up")
	}
	msg := cmd().(purgeDoneMsg)
	if msg.err != nil || msg.backup != "" {
		t.Errorf("purge of a never-saved player: %+v", msg)
	}
	if msg.text() != "kicked gern, no saved vessels to delete" {
		t.Errorf("toast = %q", msg.text())
	}
	if _, err := srv.store.FindPlayer(gfp); err == nil {
		t.Error("row still on the roster")
	}
	_ = host
}

// dockedHostAndGern docks host and gern for real through two reporting
// models. approacher picks who docks (and so who owns the stack).
type midDock struct {
	srv        *Server
	hostApp    *tui.App
	gernApp    *tui.App
	host, gern tea.Model
	gernLS     *liveSession
	gfp        string
	dir        string
	// hostMass / gernMass are each player's one vessel before they dock, so
	// "handed back their own vessel" is checkable by mass afterwards.
	hostMass, gernMass float64
}

func (d *midDock) payloadFile() string {
	sum := sha256.Sum256([]byte(d.gfp))
	return filepath.Join(d.dir, "players", hex.EncodeToString(sum[:])+".json")
}

const gernFP = "SHA256:gern"

func newMidDock(t *testing.T, gernDocks bool, gernLive bool) *midDock {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	srv, err := New(Config{Addr: "127.0.0.1:0", HostKeyPath: filepath.Join(t.TempDir(), "hostkey"), SessionDir: dir})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.ln.Close() })
	enrollDirect(t, srv, gernFP, "gern")
	hostApp, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	gernApp, err := tui.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	d := &midDock{srv: srv, dir: dir, hostApp: hostApp, gernApp: gernApp, gfp: gernFP}
	d.host = srv.HostModel(hostApp)
	if gernLive {
		d.gernLS = &liveSession{done: make(chan struct{})}
		srv.live.add(gernFP, d.gernLS)
		srv.presence.markOnline(gernFP)
		d.gern = srv.withReporting(gernApp, gernFP, d.gernLS)
	} else {
		d.gern = srv.withReporting(gernApp, gernFP)
	}
	hw, gw := hostApp.World(), gernApp.World()
	gw.Clock.SimTime = hw.Clock.SimTime
	arrangePortContactAt(t, hw, gw, 777, 0.55)
	d.hostMass, d.gernMass = hw.Crafts[0].TotalMass(), gw.Crafts[0].TotalMass()
	if gernDocks {
		// The passive vessel holds its pointing, as in the approacher tests.
		hw.Crafts[0].SlewRateDegPerSec = 1e-9
	}
	d.gern = tick(d.gern)
	d.host = tick(d.host)
	if gernDocks {
		d.host = tick(d.host)
		d.gern = tick(d.gern)
	} else {
		d.gern = tick(d.gern)
	}
	dockerW := hw
	if gernDocks {
		dockerW = gw
	}
	aimAtGhost(t, dockerW)
	for i := 0; i < 600 && !(len(dockerW.Crafts) > 0 && sim.StackHasGuest(dockerW.Crafts[0])); i++ {
		gw.Clock.SimTime = hw.Clock.SimTime
		d.host, _ = d.host.Update(sim.TickMsg(time.Now()))
		d.gern, _ = d.gern.Update(sim.TickMsg(time.Now()))
	}
	if !(len(dockerW.Crafts) > 0 && sim.StackHasGuest(dockerW.Crafts[0])) {
		t.Fatalf("no cross-player stack formed (gernDocks=%v) hostCrafts=%d gernCrafts=%d records=%d hostGhosts=%d gernGhosts=%d gernTarget=%v pulling=%v dist=%.2f", gernDocks, len(hw.Crafts), len(gw.Crafts), srv.dock.RecordCount(), len(hw.Ghosts), len(gw.Ghosts), gw.Target.Kind, ghostPulling(gw), gw.Crafts[0].State.R.Sub(hw.Crafts[0].State.R).Norm())
	}
	return d
}

func (d *midDock) purgeFromHost() tea.Cmd {
	var c tea.Cmd
	d.host, c = d.host.Update(screens.SessionAdminMsg{Cmd: screens.SessionCommand{
		Kind: screens.SessionCmdKick, Fingerprint: d.gfp, Handle: "gern", Purge: true}})
	return c
}

// Seat one: THEIR vessel rides in the host's stack. It becomes the host's for
// good; the host's [U] later releases it as the host's own vessel.
func TestPurgeMidDockTheirVesselInMyStackBecomesMine(t *testing.T) {
	d := newMidDock(t, false, false)
	hw, gw := d.hostApp.World(), d.gernApp.World()
	partsBefore := len(hw.Crafts[0].DockedComponents)
	// Their payload: whatever they have saved, to be backed up and deleted.
	if err := d.srv.store.SavePlayer(d.gfp, gw); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(d.payloadFile())

	cmd := d.purgeFromHost()
	done := cmd().(purgeDoneMsg)
	if done.err != nil {
		t.Fatal(done.err)
	}
	if after, err := os.ReadFile(done.backup); err != nil || !bytes.Equal(before, after) {
		t.Errorf("backup not byte-identical (err %v)", err)
	}
	if n := namedInLedger(t, d.srv, d.gfp, "gern"); len(n) != 0 {
		t.Fatalf("a record still names the purged player right after the purge: %v", n)
	}

	d.host = tick(d.host)
	d.host = tick(d.host)
	if len(hw.Crafts) != 1 || len(hw.Crafts[0].DockedComponents) != partsBefore {
		t.Fatalf("the host's stack lost their vessel: crafts=%d parts=%d want %d", len(hw.Crafts), len(hw.Crafts[0].DockedComponents), partsBefore)
	}
	if sim.StackHasGuest(hw.Crafts[0]) {
		t.Error("the stack still carries a guest tag")
	}
	if why := hw.UndockRefusal(0); why != "" {
		t.Errorf("the host's [U] is refused: %q", why)
	}
	if !hw.Undock(0) || len(hw.Crafts) != 2 {
		t.Errorf("host [U] did not split the stack into two vessels of the host's: %d", len(hw.Crafts))
	}
	if n := namedInLedger(t, d.srv, d.gfp, "gern"); len(n) != 0 {
		t.Errorf("a record names the purged player after the owner reconciled: %v", n)
	}
	if n := len(d.srv.dock.Records()); n != 0 {
		t.Errorf("%d records left: the transfer did not close", n)
	}
}

// Seat two: the HOST's vessel rides in THEIR stack. It is handed back before
// their program is deleted. Live: on their own last tick.
func TestPurgeMidDockMyVesselInTheirStackIsHandedBackLive(t *testing.T) {
	d := newMidDock(t, true, true)
	hw, gw := d.hostApp.World(), d.gernApp.World()
	_ = gw
	if hw.DockGuest == nil {
		t.Fatal("setup: the host is not riding in gern's stack")
	}
	massBefore := d.hostMass

	cmd := d.purgeFromHost()
	// Their session sees the flag on its next tick, hands the vessel back,
	// quits, and finishes.
	d.gern, _ = d.gern.Update(sim.TickMsg(time.Now()))
	if !d.gernLS.purgeHandled.Load() {
		t.Fatal("their last tick did not release the friend's vessel")
	}
	close(d.gernLS.done)
	done := cmd().(purgeDoneMsg)
	if done.err != nil {
		t.Fatal(done.err)
	}
	if n := namedInLedger(t, d.srv, d.gfp, "gern"); len(n) != 0 {
		t.Fatalf("a record still names the purged player: %v", n)
	}
	d.host = tick(d.host)
	d.host = tick(d.host)
	if len(hw.Crafts) != 1 {
		t.Fatalf("the host holds %d vessels, want their own back", len(hw.Crafts))
	}
	if got := hw.Crafts[0].TotalMass(); got != massBefore {
		t.Errorf("host vessel mass %v, want %v: they were not handed their own vessel", got, massBefore)
	}
	if hw.DockGuest != nil {
		t.Error("the host still reads as riding in a stack that is gone")
	}
	if len(d.srv.dock.Records()) != 0 {
		t.Errorf("%d records left", len(d.srv.dock.Records()))
	}
}

// The same when they were not connected: the stack is read from their saved
// program, before it is deleted.
func TestPurgeMidDockMyVesselInTheirStackIsHandedBackOffline(t *testing.T) {
	d := newMidDock(t, true, false)
	hw, gw := d.hostApp.World(), d.gernApp.World()
	massBefore := d.hostMass
	if err := d.srv.store.SavePlayer(d.gfp, gw); err != nil {
		t.Fatal(err)
	}

	done := d.purgeFromHost()().(purgeDoneMsg)
	if done.err != nil {
		t.Fatal(done.err)
	}
	if done.backup == "" || d.srv.store.HasPayload(d.gfp) {
		t.Errorf("backup %q, payload still there %v", done.backup, d.srv.store.HasPayload(d.gfp))
	}
	if n := namedInLedger(t, d.srv, d.gfp, "gern"); len(n) != 0 {
		t.Fatalf("a record still names the purged player: %v", n)
	}
	d.host = tick(d.host)
	d.host = tick(d.host)
	if len(hw.Crafts) != 1 || hw.Crafts[0].TotalMass() != massBefore {
		t.Fatalf("the host did not get their own vessel back: crafts=%d", len(hw.Crafts))
	}
}
