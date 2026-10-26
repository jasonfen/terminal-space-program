package screens

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/keylayout"
	"github.com/jasonfen/terminal-space-program/internal/render"
	"github.com/jasonfen/terminal-space-program/internal/sim"
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

// helpPhrase is one wording pin on an authored F1 row. section is a header
// prefix ("" = first row with the token anywhere, as helpRow does); token is
// the key cell. has is case-sensitive, hasFold is case-insensitive, anyFold
// needs at least one, not must be absent. before/after assert reading order
// within the row. absent flips the row: it must not exist. exists lists
// further key cells that must be present in the same section.
type helpPhrase struct {
	name, section, token       string
	sectionHasFold             string
	has, hasFold, anyFold, not []string
	before, after              string
	absent                     bool
	exists                     []string
}

func helpPhraseRows() []helpPhrase {
	var views []string
	for _, m := range sim.ProjectionViewModes {
		views = append(views, m.Label())
	}
	return []helpPhrase{
		// #423: q quits only inside the pause menu; on the flight screen it is radial+.
		{name: "no bare q in GENERAL", section: "GENERAL", token: "q", absent: true},
		{name: "q is menu-scoped quit", token: "q", sectionHasFold: "menu", has: []string{"quit"}},
		// #423 / ADR 0052: left leans back from the heading, right toward it, in key order.
		{name: "pitch trim both directions", token: "← / →", before: "back from", after: "toward your heading"},
		{name: "heading trim both directions", section: "MANUAL FLIGHT", token: "↑ / ↓", before: "north", after: "south"},
		{name: "nav mode skip rule", token: ";", hasFold: []string{"skip"}},
		{name: "U releases a batch in a fan, refused near the velocity", section: "VESSEL", token: "U (batch)", has: []string{"fan", "10°", "[q]"}},
		// ADR 0059 decision 9: a released batch is one stop for [t]; the map stays quick to about 30 vessels per player.
		{name: "batch is one stop for t, about 30 vessels", section: "VESSEL", token: "batch", has: []string{"[t] steps over it as one stop", "About 30 vessels per player"}},
		// ADR 0059 decision 12: L orders an orbit altitude; a raise neither caps warp nor is chased by G.
		{name: "L orders an orbit altitude, asks, again stops", section: "VESSEL", token: "L", has: []string{"altitude", "any warp", "asks first", "stop"}},
		// ADR 0059 decision 5: on a released batch L spreads it; again on the carrier stops the spread.
		{name: "L on a released batch spreads it", section: "VESSEL", token: "L", has: []string{"released batch", "spreads every satellite evenly", "stop the spread"}},
		{name: "a raise never caps warp", section: "TIME & WARP", token: "warp caps", has: []string{"a raise never caps"}},
		{name: "G chases burns, not a raise", token: "G", section: "TIME & WARP", has: []string{"not a raise"}},
		{name: "glossary names raise / lower", section: "READOUT GLOSSARY", token: "raise / lower", has: []string{"node row", "T-", "RAISE"}},
		{name: "glossary boosters / core names the PROPELLANT row", section: "READOUT GLOSSARY", token: "boosters / core", has: []string{"PROPELLANT's fuel row"}},
		{name: "E ends a crashed flight", token: "E", anyFold: []string{"end flight", "crashed"}},
		// ADR 0051: F2 hides all eight boxes and the navball.
		{name: "F2 declutter", token: "F2", has: []string{"instrument boxes", "navball", "Centerline scope", "ENGINE", "PROPELLANT"}, not: []string{"core column", "chips"}},
		{name: "K re-solves the planner", section: "RENDEZVOUS PLANNER", token: "K", has: []string{"re-solve"}},
		{name: "v names the screen's views", token: "v", has: []string{"cycle view (" + strings.Join(views, " / ") + ")"}},
		// ADR 0058 decision 14: c is a throttle row on MANUAL FLIGHT; C is documented under PLAN BURNS.
		{name: "c throttle target", section: "MANUAL FLIGHT", token: "c", has: []string{"throttle target", "all / core / boosters", "one throttle"}},
		{name: "no C on MANUAL FLIGHT", section: "MANUAL FLIGHT", token: "C", absent: true},
		{name: "PLAN BURNS C", section: "PLAN BURNS", token: "C", has: []string{"circularize"}},
		{name: "PLAN BURNS c", section: "PLAN BURNS", token: "c", has: []string{"throttle target"}},
		{name: "z/x selected throttle", section: "MANUAL FLIGHT", token: "z / x", has: []string{"selected throttle"}},
		{name: "Z/X selected throttle", section: "MANUAL FLIGHT", token: "Z / X", has: []string{"selected throttle"}},
		{name: "alt+z/x selected throttle", section: "MANUAL FLIGHT", token: "alt+z / alt+x", has: []string{"selected throttle"}},
		{name: "b cuts liquid engines", section: "MANUAL FLIGHT", token: "b", has: []string{"liquid"}},
		// GK Q6 (#589): F1 says what Enter does on every screen that uses it.
		{name: "enter GENERAL", section: "GENERAL", exists: []string{"enter (any screen)"}},
		{name: "enter SPAWN", section: "SPAWN FORM (n)", exists: []string{"enter", "enter (box open)", "esc (box open)"}},
		{name: "enter VAB", section: "VEHICLE ASSEMBLY (VAB)", exists: []string{"enter", "enter (name box)", "enter (Δv box)", "enter (design list)", "x (design list)"}},
		{name: "enter SAVES", section: "SAVES (menu → Save / Load Game)", exists: []string{"enter", "enter (name box)"}},
		{name: "enter MULTIPLAYER", section: "MULTIPLAYER (session screen, open with O)", exists: []string{"enter (invite box)"}},
		{name: "enter PAUSE MENU", section: "PAUSE MENU (esc from the map)", exists: []string{"in Settings", "in Keyboard layout"}},
		{name: "enter PLAN BURNS", section: "PLAN BURNS", exists: []string{"enter"}},
		{name: "enter NAVIGATION", section: "NAVIGATION", exists: []string{"enter"}},
		{name: "enter RENDEZVOUS PLANNER", section: "RENDEZVOUS PLANNER (map chip, opens from K)", exists: []string{"enter"}},
		// DOCKING quotes the sim's own constants, not copies: a retuned gate goes red here.
		{name: "docking gate", section: "DOCKING", token: "the gate", has: []string{
			fmt.Sprintf("%.1f m", sim.DockPortRangeM), fmt.Sprintf("%.1f m/s", sim.DockingVMS), fmt.Sprintf("%.0f°", sim.DockAlignDeg), "gap × orbit rate"}},
		{name: "docking magnet", section: "DOCKING", token: "magnet", has: []string{"five-second"}},
		{name: "docking U spring", section: "DOCKING", token: "U", has: []string{fmt.Sprintf("%.2f m/s", sim.UndockSpringMS)}},
		{name: "docking bump", section: "DOCKING", token: "bump", has: []string{fmt.Sprintf("re-approach under %.2f m/s", sim.DockingVMS)}},
		// ADR 0049 stage A3a glossary (readout codes), then later glossary rows.
		{name: "gloss fpa", token: "fpa", hasFold: []string{"flight path angle"}},
		{name: "gloss Q", token: "Q", hasFold: []string{"dynamic pressure"}},
		{name: "gloss TCA", token: "TCA", hasFold: []string{"closest approach"}},
		{name: "gloss TWR", token: "TWR", hasFold: []string{"thrust to weight"}},
		{name: "gloss Ap/Pe", token: "Ap / Pe", hasFold: []string{"height above the surface"}},
		{name: "gloss dv/dincl", token: "Δv / Δincl", hasFold: []string{"delta-v", "relative inclination"}},
		{name: "gloss T-/T+", token: "T- / T+", hasFold: []string{"T- counts down", "T+ counts up"}},
		{name: "gloss Ap arrow", token: "Ap ↑ / ↓", hasFold: []string{"climbing", "falling"}},
		{name: "gloss becomes", token: "→", hasFold: []string{"becomes"}},
		{name: "gloss node warn", token: "⚠ (node)", hasFold: []string{"Δv budget"}},
		{name: "gloss plan", token: "plan", hasFold: []string{"planned burn's numbers are measured from"}},
		{name: "gloss pro/retro", token: "pro / retro", hasFold: []string{"prograde or retrograde"}},
		{name: "gloss frames", token: "(ORBIT/SURF/TGT)", hasFold: []string{"one spelling everywhere", "target or PORT"}},
		{name: "gloss rcs", token: "rcs", hasFold: []string{"RCS thrusters"}},
		{name: "gloss speed", token: "speed", hasFold: []string{"navball frame"}},
		{name: "gloss max N", token: "(max N)", hasFold: []string{"full throttle"}},
		{name: "gloss ORBIT READY", token: "● ORBIT READY [C]", hasFold: []string{"orbit floor"}},
		// Wave C review: which heading figure is commanded and which measured.
		{name: "gloss hdg", token: "heading: / hdg", has: []string{"commanded", "measured", "on the pad", "out of the orbit plane"}},
		// #592 remainder: the navball's attitude tag and RCS toggle.
		{name: "gloss MAN/AUT", token: "[MAN] / [AUT]", has: []string{"rate-limited", "instant"}},
		{name: "gloss RCS", token: "RCS", has: []string{"thrusters"}},
		// Review #629 M1: hold:'s amber frame word.
		{name: "gloss amber", section: "READOUT GLOSSARY", token: "amber (SURF)", has: []string{"amber", "hold:", "nav:"}},
	}
}

// TestHelpRowPhrases is the one wording table for the F1 overlay: each row
// pins the phrases a review once found missing or wrong. Add a row here, not
// a new test, for the next wording finding. It reads the authored sections
// (not a rendered frame), so it is independent of scroll and width.
func TestHelpRowPhrases(t *testing.T) {
	for _, c := range helpPhraseRows() {
		t.Run(c.name, func(t *testing.T) {
			var sec *helpSection
			var desc string
			found := false
			for i := range helpSections {
				s := &helpSections[i]
				if c.section != "" && !strings.HasPrefix(s.header, c.section) {
					continue
				}
				if c.token == "" {
					sec = s
					found = true
					break
				}
				for _, r := range s.rows {
					if r[0] == c.token {
						sec, desc, found = s, r[1], true
						break
					}
				}
				if found {
					break
				}
			}
			if c.absent {
				if found {
					t.Errorf("row %q exists in %q: %q", c.token, sec.header, desc)
				}
				return
			}
			if !found {
				t.Fatalf("no help row for section %q token %q", c.section, c.token)
			}
			for _, k := range c.exists {
				has := false
				for _, r := range sec.rows {
					has = has || r[0] == k
				}
				if !has {
					t.Errorf("%s has no %q row", sec.header, k)
				}
			}
			if c.sectionHasFold != "" && !strings.Contains(strings.ToLower(sec.header), c.sectionHasFold) {
				t.Errorf("row %q sits under %q, want a header naming %q", c.token, sec.header, c.sectionHasFold)
			}
			for _, w := range c.has {
				if !strings.Contains(desc, w) {
					t.Errorf("row %q = %q, lacks %q", c.token, desc, w)
				}
			}
			for _, w := range c.hasFold {
				if !strings.Contains(strings.ToLower(desc), strings.ToLower(w)) {
					t.Errorf("row %q = %q, lacks %q", c.token, desc, w)
				}
			}
			if len(c.anyFold) > 0 {
				ok := false
				for _, w := range c.anyFold {
					ok = ok || strings.Contains(strings.ToLower(desc), w)
				}
				if !ok {
					t.Errorf("row %q = %q, names none of %v", c.token, desc, c.anyFold)
				}
			}
			for _, w := range c.not {
				if strings.Contains(desc, w) {
					t.Errorf("row %q = %q, still says %q", c.token, desc, w)
				}
			}
			if c.before != "" {
				b, a := strings.Index(desc, c.before), strings.Index(desc, c.after)
				if b < 0 || a < 0 || b > a {
					t.Errorf("row %q = %q, want %q before %q", c.token, desc, c.before, c.after)
				}
			}
		})
	}
	// The magnet row quotes five seconds in words; the constant must still be 5.
	if got := sim.DockMagnetDuration.Seconds(); got != 5 {
		t.Errorf("DockMagnetDuration = %v, the DOCKING magnet row says five-second", sim.DockMagnetDuration)
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
		"DOCKING (port to port)", // ADR 0053 slice F
		"SPAWN FORM (n)",         // GK Q3, #589: the spawn form had no page
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

// TestHelpGlossarySection (ADR 0049 stage A3a): the glossary rows live
// together under their own header, not scattered across other sections.
// ADR 0050 decision 5 made this the ninth entry (`depart`); ADR 0051
// slice 2b added eight more, one per new symbol/word the instrument
// boxes introduced (the Ap trend arrow and → share one line with the
// node ⚠, plan/dir/speed, (max N), and ORBIT READY), for 17. #478
// retired `dir` in favour of `pro / retro` (same row, new token) and
// added two: the frame-spelling entry and the `rcs` line ADR 0049's own
// decision-6 comment always meant to include, for 19. G8 Q3 (#507) added
// `heading: / hdg` (commanded vs measured), for 20, and #592 added the navball's `[MAN] / [AUT]` and `RCS` tags, for 22, and #600 the STAGES pips, for 23, and the amber hold frame word, for 24, and ADR 0057 S4 the `cargo / hold` row, for 25, and ADR 0058 S5 the `boosters / core` row, for 26, and ADR 0059 S5 the `raise / lower` row, for 27. Tightened from
// the original loose "at least 7" bound to an exact count so a stray
// addition or removal is caught rather than silently absorbed.
func TestHelpGlossarySection(t *testing.T) {
	for _, s := range helpSections {
		if s.header != "READOUT GLOSSARY" {
			continue
		}
		const want = 27
		if len(s.rows) != want {
			t.Errorf("READOUT GLOSSARY has %d rows, want exactly %d", len(s.rows), want)
		}
		return
	}
	t.Fatal("no READOUT GLOSSARY section in helpSections")
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

// TestHelpManAndRcsRowsSurviveDesignSize (#592 remainder, clarity 26): the
// navball's [MAN] / [AUT] and RCS glossary rows (their wording is in
// TestHelpRowPhrases) must not be cut at the design size.
func TestHelpManAndRcsRowsSurviveDesignSize(t *testing.T) {
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
