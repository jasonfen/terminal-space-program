package screens

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// TestFormScreenPlayerTextHasNoEmDashProse pins review #555 L7: player text
// in the spawn and VAB forms must not use an em dash as punctuation (a
// comma, colon or parens instead). A lone "—" (or "——") string is the
// established empty-value mark of the readout boxes and is allowed; so is
// any "engine —" style placeholder that ends in it. Scans the string
// literals of the real source, so a new offender fails the build.
func TestFormScreenPlayerTextHasNoEmDashProse(t *testing.T) {
	scanned := 0
	for _, file := range []string{"spawn.go", "vab.go", "vab_render.go"} {
		fs := token.NewFileSet()
		f, err := parser.ParseFile(fs, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			bl, ok := n.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				return true
			}
			scanned++
			s, err := strconv.Unquote(bl.Value)
			if err != nil {
				return true
			}
			if strings.Contains(s, " — ") {
				t.Errorf("%s: em dash in player text %q", fs.Position(bl.Pos()), s)
			}
			return true
		})
	}
	if scanned < 200 {
		t.Fatalf("only %d string literals scanned, the guard is not seeing the sources", scanned)
	}
}
