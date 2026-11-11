package screens

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func sitesMenu(n int) *TargetMenu {
	m := NewTargetMenu(Theme{})
	m.body = "MOON"
	for i := 0; i < n; i++ {
		m.rows = append(m.rows, sim.TargetRow{Group: sim.GroupSites, Name: "s", Own: true, DistM: float64(i)})
	}
	return m
}

// Five sites show, the rest fold into `… N more`; enter on it opens the list.
// The equal case: exactly five sites have no more row.
func TestTargetMenuSitesCapAndExpand(t *testing.T) {
	if got := len(sitesMenu(5).items()); got != 5 {
		t.Errorf("5 sites: %d items, want 5 (no more row)", got)
	}
	m := sitesMenu(7)
	if got := len(m.items()); got != 6 || m.items()[5].more != 2 {
		t.Fatalf("7 sites: items=%d more=%d, want 6 items and 2 hidden", got, m.items()[5].more)
	}
	if !strings.Contains(m.Render(), "… 2 more") {
		t.Errorf("render lacks the more row")
	}
	m.cursor = 5
	if m.HandleKey("enter") != TargetMenuNone || len(m.items()) != 7 {
		t.Errorf("enter on more did not expand in place (items=%d)", len(m.items()))
	}
}

// Every rendered line is exactly the card width in cells (glyphs one cell wide).
func TestTargetMenuRenderIsCardWide(t *testing.T) {
	m := sitesMenu(2)
	m.rows = append(m.rows,
		sim.TargetRow{Group: sim.GroupVessels, Name: "Starlink-3", Count: 24, DistM: 1.2e6},
		sim.TargetRow{Group: sim.GroupSites, Name: "Lander-6 wreck", Wreck: true, DistM: 88e3},
		sim.TargetRow{Group: sim.GroupPartners, Name: "ansi: Orion-2", DistM: 8.4e6},
		sim.TargetRow{Group: sim.GroupPartners, Target: sim.Target{Kind: sim.TargetSite, SiteID: 3, GhostOwner: "fp"}, Name: "ansi: Orion site", DistM: 310e3},
		sim.TargetRow{Group: sim.GroupBodies, Name: "Earth", DistM: 384.4e6})
	out := m.Render()
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != CardW {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, CardW, ln)
		}
	}
	// A partner's site keeps its flag glyph under PARTNERS and has no r/d.
	if !strings.Contains(out, "ansi: Orion site") || !strings.Contains(out, "⚑") {
		t.Errorf("partner site row or flag glyph missing:\n%s", out)
	}
}

// ADR 0068 decision 6: the arrows on a multi-LZ site row step auto, LZ-1..n
// (stopping at the ends, the equal case at n), do nothing on a one-LZ row, and
// the row with the longest name still fits the card with its selector.
func TestTargetMenuLZArrows(t *testing.T) {
	site := func(name string, lzs int) sim.TargetRow {
		return sim.TargetRow{Group: sim.GroupSites, Target: sim.Target{Kind: sim.TargetSite, SiteID: 7}, Name: name, LZs: lzs, DistM: 1}
	}
	m := NewTargetMenu(Theme{})
	m.body = "MOON"
	m.rows = []sim.TargetRow{site("Tranquility Base", 2), site("Flag-1", 1)}
	chosen := func() int { r, _ := m.Row(); return r.LZ }
	steps := []struct {
		key  string
		want int
	}{{"left", 0}, {"right", 1}, {"right", 2}, {"right", 2}, {"left", 1}, {"left", 0}, {"left", 0}, {"right", 1}}
	for i, s := range steps {
		m.HandleKey(s.key)
		if got := chosen(); got != s.want {
			t.Fatalf("step %d %s: LZ %d, want %d", i, s.key, got, s.want)
		}
	}
	out := m.Render()
	for i, ln := range strings.Split(out, "\n") {
		if w := lipgloss.Width(ln); w != CardW {
			t.Errorf("line %d is %d cells, want %d: %q", i, w, CardW, ln)
		}
	}
	for _, want := range []string{"◂ LZ-1 ▸", "[←/→] LZ"} {
		if !strings.Contains(out, want) {
			t.Errorf("render lacks %q:\n%s", want, out)
		}
	}
	m.HandleKey("left")
	if !strings.Contains(m.Render(), "◂ auto ▸") {
		t.Error("left of LZ-1 does not read auto")
	}
	m.HandleKey("down") // the one-LZ flag
	m.HandleKey("right")
	if r, _ := m.Row(); r.LZ != 0 || strings.Contains(m.Render(), "◂") {
		t.Errorf("a one-LZ row took an arrow or draws a selector (LZ %d)", r.LZ)
	}
}

func TestTargetDist(t *testing.T) {
	for _, c := range []struct {
		m    float64
		want string
	}{{999, "999 m"}, {1000, "1.0 km"}, {9999, "10.0 km"}, {10000, "10 km"}, {384.4e6, "384,400 km"}, {999999e3, "999,999 km"}, {1e9, "1.0 Mkm"}, {170.1e9, "170.1 Mkm"}} {
		if got := targetDist(c.m); got != c.want {
			t.Errorf("targetDist(%g) = %q, want %q", c.m, got, c.want)
		}
	}
}

// ADR 0068 decision 2: + and - are card keys on an own flag only (not a
// wreck, a partner's row or a shipped zone, which is not Own); the footer
// names every key on lines that fit the card.
func TestTargetMenuPlusMinusAndFooter(t *testing.T) {
	flag := func(lzs int) sim.TargetRow {
		return sim.TargetRow{Group: sim.GroupSites, Target: sim.Target{Kind: sim.TargetSite, SiteID: 7}, Name: "Flag-1", LZs: lzs, Own: true, DistM: 1}
	}
	cases := []struct {
		name     string
		row      sim.TargetRow
		add, del TargetMenuAction
		footer   []string
	}{
		{"own one-LZ flag", flag(1), TargetMenuAddLZ, TargetMenuDropLZ, []string{"[+] LZ [-] LZ", "[r] rename [d] delete [esc] close"}},
		{"own nine-LZ flag", flag(9), TargetMenuAddLZ, TargetMenuDropLZ, []string{"[←/→] LZ [+] LZ [-] LZ", "[r] rename [d] delete [esc] close"}},
		{"own wreck", sim.TargetRow{Group: sim.GroupSites, Name: "w", Own: true, Wreck: true}, 0, 0, []string{"[r] rename [d] delete [esc] close"}},
		{"shipped zone", sim.TargetRow{Group: sim.GroupSites, Target: sim.Target{Kind: sim.TargetSite, SiteID: 1 << 62}, Name: "LC-13", LZs: 2}, 0, 0, []string{"[←/→] LZ"}},
		{"partner site", sim.TargetRow{Group: sim.GroupPartners, Target: sim.Target{Kind: sim.TargetSite, GhostOwner: "fp"}, Name: "ansi: B", LZs: 3}, 0, 0, []string{"[←/→] LZ"}},
	}
	for _, c := range cases {
		m := NewTargetMenu(Theme{})
		m.body = "MOON"
		m.rows = []sim.TargetRow{c.row}
		if got := m.HandleKey("+"); got != c.add {
			t.Errorf("%s: + = %v, want %v", c.name, got, c.add)
		}
		if got := m.HandleKey("-"); got != c.del {
			t.Errorf("%s: - = %v, want %v", c.name, got, c.del)
		}
		out := m.Render()
		for _, f := range c.footer {
			if !strings.Contains(out, f) {
				t.Errorf("%s: footer lacks %q:\n%s", c.name, f, out)
			}
		}
		if c.add == 0 && (strings.Contains(out, "[+]") || strings.Contains(out, "[-]")) {
			t.Errorf("%s: footer offers + or -:\n%s", c.name, out)
		}
		for i, ln := range strings.Split(out, "\n") {
			if w := lipgloss.Width(ln); w != CardW {
				t.Errorf("%s: line %d is %d cells, want %d", c.name, i, w, CardW)
			}
		}
	}
}
