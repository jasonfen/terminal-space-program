package screens

import (
	"regexp"
	"strings"
	"testing"
)

// ADR 0057 decision 1 / decision 3 guard 5, through the real spawn form:
// the group is `Mission Stacks`, the four Dragon rows sit under their groups
// with the lit engine figure, and the Capsule still reads RCS-only.
func TestSpawnFormDragonRows(t *testing.T) {
	s := NewSpawnCraft(Theme{})
	s.Reset(nil, "", nil, "", "", nil)
	s.showAll = true
	out := stripANSI(s.Render(140, 0))
	if strings.Contains(out, "Crewed Mission Stacks") {
		t.Error("the old group label `Crewed Mission Stacks` is still drawn")
	}
	lines := strings.Split(out, "\n")
	group := ""
	rows := map[string]string{}    // name -> row text
	groupOf := map[string]string{} // name -> group
	for _, ln := range lines {
		if r := []rune(ln); len(r) > 98 {
			ln = string(r[:98]) // the VESSEL TYPE box only, not the field column beside it
		}
		txt := strings.TrimSpace(strings.Trim(ln, "│ "))
		if txt == "Mission Stacks" || txt == "Landers & Capsules" {
			group = txt
			continue
		}
		for _, name := range []string{"Falcon 9 + Dragon", "F9 + Cargo Dragon", "Dragon", "Cargo Dragon", "Capsule"} {
			if strings.HasPrefix(strings.TrimLeft(txt, "▸ "), name+" ") && strings.Contains(txt, "dry ") {
				rows[name] = txt
				groupOf[name] = group
			}
		}
	}
	want := map[string]struct{ group, crew string }{
		"Falcon 9 + Dragon": {"Mission Stacks", " crewed"},
		"F9 + Cargo Dragon": {"Mission Stacks", "uncrewed"},
		"Dragon":            {"Landers & Capsules", " crewed"},
		"Cargo Dragon":      {"Landers & Capsules", "uncrewed"},
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
		if !strings.Contains(row, "Isp 300s") && strings.HasSuffix(name, "Dragon") && !strings.Contains(name, "Falcon") && !strings.HasPrefix(name, "F9") {
			t.Errorf("%q row = %q, want the lit engine (Isp 300s)", name, row)
		}
	}
	for _, name := range []string{"Dragon", "Cargo Dragon"} {
		row := rows[name]
		if !regexp.MustCompile(`dry 9\.500 t\s+fuel 2\.562 t\s+2kN @ Isp 300s\s+2 stages`).MatchString(row) {
			t.Errorf("%q row = %q, want dry 9.500 t, fuel 2.562 t, 2kN @ Isp 300s, 2 stages", name, row)
		}
	}
	for _, name := range []string{"Falcon 9 + Dragon", "F9 + Cargo Dragon"} {
		if !strings.Contains(rows[name], "4 stages") {
			t.Errorf("%q row = %q, want 4 stages", name, rows[name])
		}
	}
	if !strings.Contains(rows["Dragon"], "crewed") || strings.Contains(rows["Dragon"], "uncrewed") {
		t.Errorf("Dragon row = %q, want crewed", rows["Dragon"])
	}
	if !strings.Contains(rows["Cargo Dragon"], "uncrewed") {
		t.Errorf("Cargo Dragon row = %q, want uncrewed", rows["Cargo Dragon"])
	}
	if c := rows["Capsule"]; !strings.Contains(c, "RCS-only") {
		t.Errorf("Capsule row = %q, want RCS-only", c)
	}
}
