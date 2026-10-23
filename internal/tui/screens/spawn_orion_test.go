package screens

import (
	"regexp"
	"strings"
	"testing"
)

// ADR 0060 decision 6 guard 8, through the real spawn form: `SLS + Orion`
// under Mission Stacks, `Orion` under Landers & Capsules, and the bare
// `SLS Block 1` still uncrewed under Launch Vehicles.
func TestSpawnFormOrionRows(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "", "", nil)
	s.showAll = true
	out := stripANSI(s.Render(140, 0))
	group := ""
	rows := map[string]string{}
	groupOf := map[string]string{}
	for _, ln := range strings.Split(out, "\n") {
		if r := []rune(ln); len(r) > 98 {
			ln = string(r[:98]) // the VESSEL TYPE box only
		}
		txt := strings.TrimSpace(strings.Trim(ln, "│ "))
		switch txt {
		case "Launch Vehicles", "Mission Stacks", "Landers & Capsules":
			group = txt
			continue
		}
		for _, name := range []string{"SLS Block 1", "SLS + Orion", "Orion"} {
			if strings.HasPrefix(strings.TrimLeft(txt, "▸ "), name+" ") && strings.Contains(txt, "dry ") {
				rows[name] = txt
				groupOf[name] = group
			}
		}
	}
	want := map[string]struct{ group, re string }{
		"SLS + Orion": {"Mission Stacks", `^▸?\s*SLS \+ Orion\s+crewed\s.*41290kN @ Isp 295s\s+5 stages`},
		"Orion":       {"Landers & Capsules", `^▸?\s*Orion\s+crewed\s.*27kN @ Isp 316s\s+2 stages`},
		"SLS Block 1": {"Launch Vehicles", `^▸?\s*SLS Block 1\s+uncrewed\s.*41290kN @ Isp 295s\s+3 stages`},
	}
	for name, w := range want {
		row, ok := rows[name]
		if !ok {
			t.Errorf("spawn form has no row %q:\n%s", name, out)
			continue
		}
		if groupOf[name] != w.group {
			t.Errorf("%q is under %q, want %q", name, groupOf[name], w.group)
		}
		if !regexp.MustCompile(w.re).MatchString(row) {
			t.Errorf("%q row = %q, want match %s", name, row, w.re)
		}
	}
}
