package screens

import (
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

// The RENDEZVOUS chip (v0.29 S2) is the persistent main-screen surface
// of the Rendezvous Warp state machine: the join prompt while a partner
// is armed toward the viewer, the armed-waiting readout after
// initiating, and the coasting readout (committed CA + live approach +
// degrade warning) once the shared coast runs. buildRendezvousChip
// reads the World slate directly; states are exercised through it.

func rendezvousChipWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatalf("NewWorld: %v", err)
	}
	return w
}

func TestRendezvousChipHiddenWhenIdle(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	if chip := v.buildRendezvousChip(w); chip != nil {
		t.Errorf("chip rendered with no rendezvous state:\n%s", strings.Join(chip, "\n"))
	}
}

func TestRendezvousChipInvitePrompt(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.RendezvousInvite = &sim.RendezvousInvite{
		Owner: "SHA256:guest", Handle: "gern",
		Tau: w.Clock.SimTime.Add(2 * time.Hour), CA: 900,
	}
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"RENDEZVOUS", "gern wants to rendezvous", "[y] join", "2h0m", "900 m"} {
		if !strings.Contains(joined, want) {
			t.Errorf("invite prompt missing %q:\n%s", want, joined)
		}
	}
}

func TestRendezvousChipArmedWaiting(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.EngageRendezvousWarp("SHA256:guest", "gern", w.Clock.SimTime.Add(2*time.Hour), 900)
	w.Session = &sim.SessionInfo{Players: []sim.SessionPlayer{
		{Fingerprint: "SHA256:guest", Handle: "gern"},
	}}
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"RENDEZVOUS", "gern", "waiting", "[/] cancel"} {
		if !strings.Contains(joined, want) {
			t.Errorf("armed-waiting chip missing %q:\n%s", want, joined)
		}
	}
}

// #250: when the armed pair has diverged past the subspace window, the
// chip must stop blaming the partner ("waiting for them to join") and
// name the actual condition — who is ahead, and the direction-correct
// fix. Sync is forward-only, so a viewer who is ahead cannot Sync back:
// only the laggard's side may say "Sync to rejoin".
func TestRendezvousChipArmedSubspaceGap(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.EngageRendezvousWarp("SHA256:guest", "gern", w.Clock.SimTime.Add(2*time.Hour), 900)
	w.RendezvousWait = sim.RendezvousWait{
		Reason: sim.RendezvousWaitSubspaceGap, AheadBy: 2 * time.Minute,
	}

	// Viewer ahead: Sync (forward-only) can't reach the partner behind —
	// the partner must come forward.
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"cannot couple", "you are 2m0s ahead of gern", "they must Sync to you"} {
		if !strings.Contains(joined, want) {
			t.Errorf("gap chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "Sync to rejoin") {
		t.Errorf("viewer-ahead gap tells the viewer to Sync into the past:\n%s", joined)
	}
	if strings.Contains(joined, "waiting for them to join") {
		t.Errorf("gap chip still blames the partner:\n%s", joined)
	}

	// The partner ahead instead — the wording flips direction and the
	// viewer is the one who can Sync forward.
	w.RendezvousWait.AheadBy = -2 * time.Minute
	joined = strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"gern is 2m0s ahead of you", "Sync to rejoin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("partner-ahead gap chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "they must Sync to you") {
		t.Errorf("partner-ahead gap points the fix at the wrong side:\n%s", joined)
	}
}

// #260: while the viewer's own Sync or node-chase defers the coast start
// (RendezvousWaitSelf), the armed chip must own the wait — name the
// viewer's running Auto-Warp — and neither blame the partner ("waiting
// for them to join") nor reach for the gap vocabulary (Sync-to-rejoin):
// the partner already joined, and no Sync is owed.
func TestRendezvousChipArmedWaitSelf(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.EngageRendezvousWarp("SHA256:guest", "gern", w.Clock.SimTime.Add(2*time.Hour), 900)
	w.RendezvousWait = sim.RendezvousWait{Reason: sim.RendezvousWaitSelf}

	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{
		"RENDEZVOUS",
		"your Auto-Warp is running — coast starts when it releases",
		"[/] cancel",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("wait-self chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "waiting for them to join") {
		t.Errorf("wait-self chip still blames the partner:\n%s", joined)
	}
	for _, gapism := range []string{"Sync to rejoin", "they must Sync to you", "cannot couple"} {
		if strings.Contains(joined, gapism) {
			t.Errorf("wait-self chip borrows the gap wording %q:\n%s", gapism, joined)
		}
	}
}

// #250 responder side: a blocked (subspace-gapped) invite renders as a
// dimmed attribution with the [y] join affordance suppressed, instead
// of vanishing. The Sync advice is direction-aware (forward-only): a
// viewer behind the initiator can Sync to join; a viewer ahead cannot,
// so the initiator must Sync forward instead.
func TestRendezvousChipInviteBlocked(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	w.RendezvousInvite = &sim.RendezvousInvite{
		Owner: "SHA256:guest", Handle: "gern",
		Tau: w.Clock.SimTime.Add(2 * time.Hour), CA: 900,
		Blocked: true, AheadBy: -3 * time.Minute,
	}

	// Viewer behind the initiator: Sync forward is the way in.
	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"gern wants to rendezvous", "subspace gap", "Sync to join"} {
		if !strings.Contains(joined, want) {
			t.Errorf("blocked invite chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "[y]") {
		t.Errorf("blocked invite still offers [y] join:\n%s", joined)
	}

	// Viewer ahead of the initiator: Sync into the past is impossible —
	// the advice flips to the initiator, [y] stays suppressed.
	w.RendezvousInvite.AheadBy = 3 * time.Minute
	joined = strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"gern wants to rendezvous", "subspace gap", "they must Sync to you"} {
		if !strings.Contains(joined, want) {
			t.Errorf("viewer-ahead blocked invite chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "Sync to join") {
		t.Errorf("viewer-ahead blocked invite tells the viewer to Sync into the past:\n%s", joined)
	}
	if strings.Contains(joined, "[y]") {
		t.Errorf("viewer-ahead blocked invite still offers [y] join:\n%s", joined)
	}
}

func TestRendezvousChipCoastingAndDegraded(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	tau := w.Clock.SimTime.Add(2 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 900)
	w.AutoWarp = &sim.AutoWarpTarget{
		T: tau, Rendezvous: true,
		RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern",
	}
	w.RendezvousApproachM = 1200

	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"RENDEZVOUS", "gern", "committed", "900 m", "1.20 km", "[/] cancel"} {
		if !strings.Contains(joined, want) {
			t.Errorf("coasting chip missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "degraded") {
		t.Errorf("degrade warning shown while the encounter holds:\n%s", joined)
	}

	w.RendezvousDegraded = true
	w.RendezvousApproachM = 15_000
	joined = strings.Join(v.buildRendezvousChip(w), "\n")
	if !strings.Contains(joined, "degraded") {
		t.Errorf("no degrade warning after the encounter slipped:\n%s", joined)
	}

	// Hold-the-leader (v0.29 review): the freeze is surfaced, not silent.
	w.RendezvousHold = true
	joined = strings.Join(v.buildRendezvousChip(w), "\n")
	if !strings.Contains(joined, "holding — waiting for gern") {
		t.Errorf("hold state not surfaced on the chip:\n%s", joined)
	}
}

// TestRendezvousChipTrendRowShrinking — ADR 0039 S3 / #281: once a
// second waypoint has been derived, the chip shows whether the encounter
// is converging. A shrinking CommittedCA (577 km → 430 km) reads as
// quiet good news, not a warning.
func TestRendezvousChipTrendRowShrinking(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	tau := w.Clock.SimTime.Add(2 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 430_000)
	w.AutoWarp = &sim.AutoWarpTarget{
		T: tau, Rendezvous: true,
		RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern",
	}
	w.RendezvousArm.PrevCommittedCA = 577_000
	w.RendezvousArm.PrevCommittedCASet = true

	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	if !strings.Contains(joined, "shrinking") {
		t.Errorf("no shrinking-trend row for a converging CA:\n%s", joined)
	}
	if strings.Contains(joined, "phasing direction is wrong") {
		t.Errorf("diverging warning shown for a converging CA:\n%s", joined)
	}
}

// TestRendezvousChipTrendRowGrowing — the mirror case, matching the
// ADR's own worked example: committed CA grew 577 km → 1,100 km across a
// waypoint. Must warn AND name the doctrine's own diagnosis ("phasing
// direction is wrong") — the degrade watchdog's own baseline can't catch
// this (it re-bases every waypoint, ADR 0039 S3's whole reason for
// being), so this trend row is the only signal a diverging standing
// intent gets.
func TestRendezvousChipTrendRowGrowing(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	tau := w.Clock.SimTime.Add(2 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 1_100_000)
	w.AutoWarp = &sim.AutoWarpTarget{
		T: tau, Rendezvous: true,
		RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern",
	}
	w.RendezvousArm.PrevCommittedCA = 577_000
	w.RendezvousArm.PrevCommittedCASet = true

	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, want := range []string{"⚠", "growing", "phasing direction is wrong"} {
		if !strings.Contains(joined, want) {
			t.Errorf("diverging-trend row missing %q:\n%s", want, joined)
		}
	}
}

// TestRendezvousChipTrendRowSilentBeforeFirstAdvance — with only the
// initial commit (no waypoint advance yet), there is exactly one CA data
// point — no trend to show, and the row must not appear (a false
// "shrinking"/"growing" claim from a single point would be worse than
// silence).
func TestRendezvousChipTrendRowSilentBeforeFirstAdvance(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	tau := w.Clock.SimTime.Add(2 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 900)
	w.AutoWarp = &sim.AutoWarpTarget{
		T: tau, Rendezvous: true,
		RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern",
	}

	joined := strings.Join(v.buildRendezvousChip(w), "\n")
	for _, unwanted := range []string{"shrinking", "growing", "phasing direction is wrong"} {
		if strings.Contains(joined, unwanted) {
			t.Errorf("trend row rendered before any waypoint advance (%q present):\n%s", unwanted, joined)
		}
	}
}

// #253: Away is persistent state (the partner's session flies on under the
// Commitment Reprieve with nobody at the controls), so the coasting chip
// carries a STANDING away line driven by the world slate — present while
// they are away, gone when they return — never by a SessionEvent that
// expires off the canvas after 6 s.
func TestRendezvousChipPartnerAway(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	tau := w.Clock.SimTime.Add(2 * time.Hour)
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 900)
	w.AutoWarp = &sim.AutoWarpTarget{
		T: tau, Rendezvous: true,
		RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern",
	}

	const line = "gern is away — their session is still flying"
	if joined := strings.Join(v.buildRendezvousChip(w), "\n"); strings.Contains(joined, "away") {
		t.Errorf("away line shown while the partner is at the controls:\n%s", joined)
	}
	w.RendezvousPartnerAway = true
	if joined := strings.Join(v.buildRendezvousChip(w), "\n"); !strings.Contains(joined, line) {
		t.Errorf("coasting chip missing the standing away line:\n%s", joined)
	}
	w.RendezvousPartnerAway = false
	if joined := strings.Join(v.buildRendezvousChip(w), "\n"); strings.Contains(joined, "away") {
		t.Errorf("away line still shown after the partner returned:\n%s", joined)
	}
}

// The standing DOCKED block (ADR 0038 S4) is unconditional while riding in
// another player's stack — a strictly wider surface than #253's original
// away-only line. The exit list forks on whether the stack owner has a
// live Session right now (ADR 0040 §4's empty-seat presence gate, reused
// via the roster rather than reinvented); #253's away line survives as an
// extra row on this SAME block, never a second competing one (ADR 0038
// consequences: "one surface, not two").
func TestDockGuestChipStandingBlock(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	if chip := v.buildDockGuestChip(w); chip != nil {
		t.Errorf("dock chip rendered while not docked as guest:\n%s", strings.Join(chip, "\n"))
	}

	w.DockGuest = &sim.DockGuestLink{OwnerFP: "SHA256:host", OwnerHandle: "vex"}
	w.Session = &sim.SessionInfo{Players: []sim.SessionPlayer{
		{Fingerprint: "SHA256:host", Handle: "vex", Online: true},
	}}
	joined := strings.Join(v.buildDockGuestChip(w), "\n")
	for _, want := range []string{"DOCKED", "riding in vex's stack", "[J] request control", "[U] ask to undock"} {
		if !strings.Contains(joined, want) {
			t.Errorf("live-owner standing block missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "take the stick") {
		t.Errorf("live-owner block offers the empty-seat exit:\n%s", joined)
	}

	// Owner away but still connected (#253): an extra row on the SAME
	// block, not a second one, and the live-owner exits stay put.
	w.DockGuest.OwnerAway = true
	joined = strings.Join(v.buildDockGuestChip(w), "\n")
	if !strings.Contains(joined, "vex is away — their session is still flying") {
		t.Errorf("no standing away line while the stack owner is away:\n%s", joined)
	}
	if !strings.Contains(joined, "[U] ask to undock") {
		t.Errorf("away row replaced the exits instead of joining them:\n%s", joined)
	}
	w.DockGuest.OwnerAway = false

	// Empty seat (ADR 0040 §4 / amendment 3): the owner has no live Session
	// at all. The exit list changes and [u] drops — there is nobody to ask.
	w.Session.Players[0].Online = false
	joined = strings.Join(v.buildDockGuestChip(w), "\n")
	if !strings.Contains(joined, "[J] take the stick (pilot's gone)") {
		t.Errorf("empty-seat block missing the reclaim exit:\n%s", joined)
	}
	if strings.Contains(joined, "ask to undock") {
		t.Errorf("empty-seat block still offers 'ask to undock' — nobody to ask:\n%s", joined)
	}
	if strings.Contains(joined, "request control") {
		t.Errorf("empty-seat block still offers the ask-first phrasing:\n%s", joined)
	}
}

// Regression guard for the 💤 defect (#253 review): the away lines were
// the first chip content whose glyph measured 2 terminal cells but
// spliced as 1 rune, overflowing the canvas row on overlay. Run every
// glyph-bearing chip state through the measure-vs-splice consistency
// check so the next width-2 glyph can't sneak onto the chip path.
func TestChipLinesMeasureOneCellPerSplicedRune(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	tau := w.Clock.SimTime.Add(2 * time.Hour)

	// Invite prompt (◇).
	w.RendezvousInvite = &sim.RendezvousInvite{
		Owner: "SHA256:guest", Handle: "gern", Tau: tau, CA: 900,
	}
	assertChipCellWidthConsistent(t, "rendezvous invite chip", v.buildRendezvousChip(w))
	w.RendezvousInvite = nil

	// Coasting with every standing line lit: hold (⏸), away (z),
	// degraded (⚠).
	w.EngageRendezvousWarp("SHA256:guest", "gern", tau, 900)
	w.AutoWarp = &sim.AutoWarpTarget{
		T: tau, Rendezvous: true,
		RendezvousOwner: "SHA256:guest", RendezvousHandle: "gern",
	}
	w.RendezvousApproachM = 1200
	w.RendezvousHold = true
	w.RendezvousPartnerAway = true
	w.RendezvousDegraded = true
	assertChipCellWidthConsistent(t, "rendezvous coasting chip", v.buildRendezvousChip(w))

	// Docked-as-guest away line (z).
	w.DockGuest = &sim.DockGuestLink{
		OwnerFP: "SHA256:host", OwnerHandle: "vex", OwnerAway: true,
	}
	assertChipCellWidthConsistent(t, "dock guest chip", v.buildDockGuestChip(w))
}

// The SESSION moments chip renders the four new rendezvous kinds
// (v0.29 S2) alongside the existing vocabulary.
func TestSessionEventsChipRendezvousKinds(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	w := rendezvousChipWorld(t)
	now := time.Now()
	w.SessionEvents = []sim.SessionEvent{
		{Kind: sim.SessionEventRendezvousArmed, Handle: "gern", At: now},
		{Kind: sim.SessionEventRendezvousArrived, Handle: "gern", At: now},
		{Kind: sim.SessionEventRendezvousCancelled, Handle: "gern", At: now},
		{Kind: sim.SessionEventRendezvousDegraded, Handle: "gern", At: now},
	}
	joined := strings.Join(v.buildSessionEventsChip(w), "\n")
	for _, want := range []string{
		"gern wants to rendezvous",
		"rendezvous: encounter reached",
		"rendezvous with gern cancelled",
		"rendezvous encounter degraded",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("session chip missing %q:\n%s", want, joined)
		}
	}
}
