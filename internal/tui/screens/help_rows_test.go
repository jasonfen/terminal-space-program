package screens

import (
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// helpRow returns the description the overlay prints for a key token,
// searching the authored sections rather than the rendered frame so the
// assertion doesn't depend on scroll position.
func helpRow(t *testing.T, token string) (section, desc string) {
	t.Helper()
	for _, s := range helpSections {
		for _, r := range s.rows {
			if r[0] == token {
				return s.header, r[1]
			}
		}
	}
	t.Fatalf("no help row for token %q", token)
	return "", ""
}

// TestHelpQuitRowIsMenuScoped (#423): `q` is AttitudeRadialOut on the
// flight screen — it only quits inside the pause menu (menu.go). The
// overlay used to open with "q — quit (confirm + autosave)" in GENERAL,
// which is the first line a player reads and the one that misleads worst.
func TestHelpQuitRowIsMenuScoped(t *testing.T) {
	for _, s := range helpSections {
		if s.header != "GENERAL" {
			continue
		}
		for _, r := range s.rows {
			if r[0] == "q" {
				t.Errorf("GENERAL still lists a bare `q` row (%q) — on the flight screen q is radial+, not quit", r[1])
			}
		}
	}
	section, desc := helpRow(t, "q")
	if !strings.Contains(strings.ToLower(section), "menu") {
		t.Errorf("the quit `q` row sits under %q; it must be in a clearly menu-scoped section", section)
	}
	if !strings.Contains(desc, "quit") {
		t.Errorf("menu-scoped `q` row no longer describes quit: %q", desc)
	}
}

// TestHelpPitchTrimNamesBothDirections (#423, keys moved by ADR 0052): ←
// leans back from your heading, → toward it (2026-10-09; it said west /
// east, true only at the default due-east heading). The row once named only
// one direction while showing the keys in ←-then-→ order.
func TestHelpPitchTrimNamesBothDirections(t *testing.T) {
	_, desc := helpRow(t, "← / →")
	back := strings.Index(desc, "back from")
	toward := strings.Index(desc, "toward your heading")
	if back < 0 || toward < 0 {
		t.Fatalf("pitch trim row names only one direction: %q", desc)
	}
	if back > toward {
		t.Errorf("pitch trim row reads toward-before-back (%q) but the keys are ← back then → toward", desc)
	}
}

// TestHelpNavModeStatesSkipRule (#423): the `;` cycle skips Target when no
// vessel target is bound, so a player watching it toggle Orbit⇄Surface with
// a body targeted otherwise concludes the key is broken. docs/controls.md
// already says this.
func TestHelpNavModeStatesSkipRule(t *testing.T) {
	_, desc := helpRow(t, ";")
	if !strings.Contains(strings.ToLower(desc), "skip") {
		t.Errorf("NavMode row omits the skip-Target rule: %q", desc)
	}
}

// TestHelpDocumentsEndFlight (#423): `E` is the only way to clear a crashed
// vessel and was absent from the overlay entirely.
func TestHelpDocumentsEndFlight(t *testing.T) {
	_, desc := helpRow(t, "E")
	if !strings.Contains(strings.ToLower(desc), "end flight") && !strings.Contains(strings.ToLower(desc), "crashed") {
		t.Errorf("`E` row does not describe ending the flight of a crashed vessel: %q", desc)
	}
}

// TestHelpSectionOrder (#425): "how do I fly" (camera, warp, manual
// flight) used to sit six PgDn presses down, behind SAVES and the 13-row
// MULTIPLAYER section. Pins the grilled 2026-09-04 order so a future edit
// can't silently drift it back.
func TestHelpSectionOrder(t *testing.T) {
	want := []string{
		"GENERAL",
		"PAUSE MENU (esc from the map)",
		"CAMERA & VIEW",
		"TIME & WARP",
		"MANUAL FLIGHT",
		"NAVIGATION",
		"PLAN BURNS",
		"RENDEZVOUS PLANNER (map chip, opens from K)",
		"VESSEL",
		"SPAWN FORM (n)", // GK Q3, #589: the spawn form had no page
		"VEHICLE ASSEMBLY (VAB)",
		"MISSIONS (ladder screen, open with M)", // #426, screen-scoped, after VAB
		"SAVES (menu → Save / Load Game)",
		"MULTIPLAYER (session screen, open with O)",
		"MOUSE",
		"READOUT GLOSSARY",
		"MAP MARKERS",
	}
	var got []string
	for _, s := range helpSections {
		got = append(got, s.header)
	}
	if len(got) != len(want) {
		t.Fatalf("section count = %d, want %d\ngot:  %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("section %d: got %q, want %q\nfull order: %v", i, got[i], want[i], got)
		}
	}
}

// TestHelpGlossaryBlockContent (ADR 0049 stage A3a): the six codes that
// survive the readout contract each get a one-line explanation, plus the
// two facts the contract otherwise strips off every panel: apsides are
// already height above the surface, and countdowns read T- until an
// event, T+ since it.
func TestHelpGlossaryBlockContent(t *testing.T) {
	cases := []struct {
		token, want string
	}{
		{"fpa", "flight path angle"},
		{"Q", "dynamic pressure"},
		{"TCA", "closest approach"},
		{"TWR", "thrust to weight"},
		{"Ap / Pe", "height above the surface"},
		{"Δv / Δincl", "delta-v"},
		{"Δv / Δincl", "relative inclination"},
		{"T- / T+", "T- counts down"},
		{"T- / T+", "T+ counts up"},
		{"Ap ↑ / ↓", "climbing"},
		{"Ap ↑ / ↓", "falling"},
		{"→", "becomes"},
		{"⚠ (node)", "Δv budget"},
		{"plan", "planned burn's numbers are measured from"},
		{"pro / retro", "prograde or retrograde"},
		{"(ORBIT/SURF/TGT)", "one spelling everywhere"},
		{"rcs", "RCS thrusters"},
		{"speed", "navball frame"},
		{"(max N)", "full throttle"},
		{"● ORBIT READY [C]", "orbit floor"},
	}
	for _, c := range cases {
		_, desc := helpRow(t, c.token)
		if !strings.Contains(strings.ToLower(desc), strings.ToLower(c.want)) {
			t.Errorf("glossary row %q = %q, want it to mention %q", c.token, desc, c.want)
		}
	}
}

// TestHelpGlossarySection (ADR 0049 stage A3a): the glossary rows live
// together under their own header, not scattered across other sections.
// ADR 0050 decision 5 made this the ninth entry (`depart`); ADR 0051
// slice 2b added eight more, one per new symbol/word the instrument
// boxes introduced (the Ap trend arrow and → share one line with the
// node ⚠, plan/dir/speed, (max N), and ORBIT READY), for 17. #478
// retired `dir` in favour of `pro / retro` (same row, new token) and
// added two: the frame-spelling entry and the `rcs` line ADR 0049's own
// decision-6 comment always meant to include, for 19. G8 Q3 (#507) added
// `heading: / hdg` (commanded vs measured), for 20, and #592 added the navball's `[MAN] / [AUT]` and `RCS` tags, for 22, and #600 the STAGES pips, for 23, and the amber hold frame word, for 24. Tightened from
// the original loose "at least 7" bound to an exact count so a stray
// addition or removal is caught rather than silently absorbed.
func TestHelpGlossarySection(t *testing.T) {
	for _, s := range helpSections {
		if s.header != "READOUT GLOSSARY" {
			continue
		}
		const want = 24
		if len(s.rows) != want {
			t.Errorf("READOUT GLOSSARY has %d rows, want exactly %d", len(s.rows), want)
		}
		return
	}
	t.Fatal("no READOUT GLOSSARY section in helpSections")
}

// TestHelpDeclutterRowNamesInstrumentBoxes: since ADR 0051 (v0.44.0) F2
// hides all eight instrument boxes and the navball; there is no "core
// column" that stays. The only exception is ENGINE and PROPELLANT while
// an engine is lit (docs/controls.md says the same).
func TestHelpDeclutterRowNamesInstrumentBoxes(t *testing.T) {
	_, desc := helpRow(t, "F2")
	if strings.Contains(desc, "core column") || strings.Contains(desc, "chips") {
		t.Errorf("F2 row still describes the pre-ADR-0051 chips/core column: %q", desc)
	}
	for _, want := range []string{"instrument boxes", "navball", "ENGINE", "PROPELLANT"} {
		if !strings.Contains(desc, want) {
			t.Errorf("F2 row omits %q: %q", want, desc)
		}
	}
}

// TestHelpIndexSectionsHaveWhenClauses (#494): every section is on the
// index with a when-you-would-open-this clause, so a new section can't
// ship as a bare name.
func TestHelpIndexSectionsHaveWhenClauses(t *testing.T) {
	for _, s := range helpSections {
		if strings.TrimSpace(helpWhenFor(s.header)) == "" {
			t.Errorf("section %q has no when-clause on the index", s.header)
		}
	}
	if len(helpWhen) != len(helpSections) {
		t.Errorf("helpWhen has %d entries for %d sections (stale key?)", len(helpWhen), len(helpSections))
	}
}

// TestHelpFirstFlightLayoutTranslation: the page's key column follows the
// active layout like every other row (QWERTZ throttle reads y).
func TestHelpFirstFlightLayoutTranslation(t *testing.T) {
	h := NewHelp(chipTestTheme())
	h.OpenPage(0)
	if out := h.Render(120, 40, keylayout.QWERTZ); !strings.Contains(out, "y   ") || !strings.Contains(out, "on the pad, throttle to full") {
		t.Errorf("first-flight key column not layout-translated:\n%s", out)
	}
}

// TestHelpHeadingTrimNamesBothDirections: ↑ is north, ↓ is south (ADR 0052
// decision 2, the heading half of the trim pair).
func TestHelpHeadingTrimNamesBothDirections(t *testing.T) {
	var hdesc string
	for _, sec := range helpSections {
		if sec.header != "MANUAL FLIGHT" {
			continue
		}
		for _, r := range sec.rows {
			if r[0] == "↑ / ↓" {
				hdesc = r[1]
			}
		}
	}
	if hdesc == "" {
		t.Fatal("no heading-trim row in MANUAL FLIGHT")
	}
	north := strings.Index(hdesc, "north")
	south := strings.Index(hdesc, "south")
	if north < 0 || south < 0 || north > south {
		t.Errorf("heading trim row must name north then south for ↑ then ↓: %q", hdesc)
	}
}

// TestHelpRendezvousPlannerNamesKReSolve (v0.48.0 docs parity): `K` inside
// the open Rendezvous Planner re-solves every row from now (review fix
// #541); docs/controls.md says so, and F1 is the source of truth.
func TestHelpRendezvousPlannerNamesKReSolve(t *testing.T) {
	for _, s := range helpSections {
		if !strings.HasPrefix(s.header, "RENDEZVOUS PLANNER") {
			continue
		}
		for _, r := range s.rows {
			if r[0] == "K" && strings.Contains(r[1], "re-solve") {
				return
			}
		}
		t.Fatalf("RENDEZVOUS PLANNER section has no `K` re-solve row: %v", s.rows)
	}
	t.Fatal("no RENDEZVOUS PLANNER section")
}

// markerKeySection returns the F1 section that keys the map's marker glyphs.
func markerKeySection(t *testing.T) helpSection {
	t.Helper()
	for _, s := range helpSections {
		if strings.HasPrefix(s.header, "MAP MARKERS") {
			return s
		}
	}
	t.Fatal("F1 has no MAP MARKERS section")
	return helpSection{}
}

// TestHelpHasMarkerKey (#592): F1 explained readout codes but not the glyphs
// the map draws. Every marker the map stamps (render.MarkerGlyph, the vessel
// chevron) needs a row under its real glyph with a meaning, and the page
// must fit at the design size without truncation.
func TestHelpHasMarkerKey(t *testing.T) {
	sec := markerKeySection(t)
	got := map[string]string{}
	for _, r := range sec.rows {
		got[r[0]] = r[1]
	}
	glyphs := []string{spacecraft.VesselGlyph}
	for _, mt := range []render.MarkerType{
		render.MarkerApoapsis, render.MarkerPeriapsis, render.MarkerAscendingNode,
		render.MarkerDescendingNode, render.MarkerPerilune, render.MarkerClosestApproach,
		render.MarkerManeuver, render.MarkerSOIEntry, render.MarkerSOIExit, render.MarkerImpact,
	} {
		glyphs = append(glyphs, string(render.MarkerGlyph(mt)))
	}
	for _, g := range glyphs {
		if strings.TrimSpace(got[g]) == "" {
			t.Errorf("MAP MARKERS has no row for the drawn glyph %q", g)
		}
	}
	if _, ok := got["green"]; !ok {
		t.Error("MAP MARKERS does not say what the Target colour is")
	}
	for _, sz := range [][2]int{{140, 40}, {181, 49}} {
		h := NewHelp(chipTestTheme())
		for n := 0; n < helpPageCount(); n++ {
			if pageTitle(n) == helpIndexTitle(sec.header) {
				h.OpenPage(n)
			}
		}
		out := ansi.Strip(h.Render(sz[0], sz[1], keylayout.QWERTY))
		if strings.Contains(out, "…") {
			t.Errorf("%dx%d: the marker page truncates a row:\n%s", sz[0], sz[1], out)
		}
		if !strings.Contains(out, string(render.MarkerGlyph(render.MarkerManeuver))) {
			t.Errorf("%dx%d: the marker page does not render:\n%s", sz[0], sz[1], out)
		}
	}
}

// TestControlsDocMirrorsMarkerKey: docs/controls.md carries every marker row
// F1 does, glyph and meaning, row by row.
func TestControlsDocMirrorsMarkerKey(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(raw)
	i := strings.Index(doc, "## Map markers")
	j := strings.Index(doc, "## Screens")
	if i < 0 || j < i {
		t.Fatal("docs/controls.md has no Map markers section")
	}
	rows := markerKeySection(t).rows
	for _, r := range rows {
		if !strings.Contains(doc[i:j], "| `"+r[0]+"` |") {
			t.Errorf("docs/controls.md Map markers lacks a row for %q", r[0])
		}
	}
	// Two-way (review #614 L4): a docs-only row is drift too.
	inF1 := map[string]bool{}
	for _, r := range rows {
		inF1[r[0]] = true
	}
	for _, ln := range strings.Split(doc[i:j], "\n") {
		if !strings.HasPrefix(ln, "| `") {
			continue
		}
		cell := strings.TrimPrefix(ln, "| `")
		k := strings.Index(cell, "` |")
		if k < 0 {
			continue
		}
		if g := cell[:k]; !inF1[g] {
			t.Errorf("docs/controls.md Map markers has a row for %q that F1 lacks", g)
		}
	}
}

// TestMarkerKeyWording pins L1: the words that were wrong.
func TestMarkerKeyWording(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/controls.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range markerKeySection(t).rows {
		for _, bad := range []string{"planned pass", "planned path", "you gave it"} {
			if strings.Contains(r[1], bad) {
				t.Errorf("F1 marker row %q still says %q", r[0], bad)
			}
		}
	}
	for _, bad := range []string{"planned pass", "planned path", "you gave it"} {
		if strings.Contains(string(raw), bad) {
			t.Errorf("docs/controls.md still says %q", bad)
		}
	}
}

// TestHelpExpandsManAndRcsTags (#592 remainder, clarity 26): the navball's
// [MAN] / [AUT] attitude tag and its RCS toggle were never spelled out in F1.
func TestHelpExpandsManAndRcsTags(t *testing.T) {
	_, man := helpRow(t, "[MAN] / [AUT]")
	for _, want := range []string{"rate-limited", "instant"} {
		if !strings.Contains(man, want) {
			t.Errorf("[MAN] / [AUT] row omits %q: %q", want, man)
		}
	}
	_, rcs := helpRow(t, "RCS")
	if !strings.Contains(rcs, "thrusters") {
		t.Errorf("RCS row does not say what it arms: %q", rcs)
	}
	// Both rows must survive the design size untruncated.
	h := NewHelp(chipTestTheme())
	for n := 0; n < helpPageCount(); n++ {
		if pageTitle(n) == helpIndexTitle("READOUT GLOSSARY") {
			h.OpenPage(n)
		}
	}
	if !strings.Contains(ansi.Strip(h.Render(140, 40, keylayout.QWERTY)), "READOUT GLOSSARY") {
		t.Fatal("glossary page did not open by section lookup")
	}
	for _, ln := range strings.Split(ansi.Strip(h.Render(140, 40, keylayout.QWERTY)), "\n") {
		if (strings.Contains(ln, "[MAN] / [AUT]") || strings.Contains(ln, "  RCS  ")) && strings.Contains(ln, "…") {
			t.Errorf("glossary row truncated at 140x40: %q", ln)
		}
	}
}

// TestHelpExplainsGuidanceAmberFrameWord: GUIDANCE's hold: frame word turns
// amber when it differs from nav: (review #629 M1); F1 must say what that
// means, in the glossary.
func TestHelpExplainsGuidanceAmberFrameWord(t *testing.T) {
	for _, s := range helpSections {
		if s.header != "READOUT GLOSSARY" {
			continue
		}
		for _, r := range s.rows {
			if strings.Contains(r[1], "amber") && strings.Contains(r[1], "hold:") && strings.Contains(r[1], "nav:") {
				return
			}
		}
		t.Fatal("READOUT GLOSSARY has no row explaining hold:'s amber frame word")
	}
	t.Fatal("no READOUT GLOSSARY section in helpSections")
}

// TestHelpAmberRowSurvivesDesignSize: the `amber (SURF)` glossary row, as
// rendered on the F1 page at 140x40, ends in its point (the hold keeps its
// frame on purpose) rather than an ellipsis. The sections-level test above
// reads the source text and cannot see a render-time cut.
func TestHelpAmberRowSurvivesDesignSize(t *testing.T) {
	h := NewHelp(chipTestTheme())
	for n := 0; n < helpPageCount(); n++ {
		if pageTitle(n) == helpIndexTitle("READOUT GLOSSARY") {
			h.OpenPage(n)
		}
	}
	found := false
	for _, ln := range strings.Split(ansi.Strip(h.Render(140, 40, keylayout.QWERTY)), "\n") {
		if !strings.Contains(ln, "amber (SURF)") {
			continue
		}
		found = true
		if strings.Contains(ln, "…") {
			t.Errorf("amber (SURF) row truncated at 140x40: %q", ln)
		}
		if !strings.Contains(ln, "on purpose") {
			t.Errorf("amber (SURF) row lost its meaning (on purpose) at 140x40: %q", ln)
		}
	}
	if !found {
		t.Fatal("amber (SURF) row not on the rendered glossary page; instrument blind")
	}
}
