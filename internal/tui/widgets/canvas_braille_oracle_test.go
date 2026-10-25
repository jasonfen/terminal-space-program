package widgets

// New file because canvas_test.go is over 600 lines. Oracle for the
// drawille -> in-house braille grid swap (audit L6-3): the hashes below were
// produced by the drawille-backed canvas and must not change.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
)

type oracleScene struct {
	name string
	cols int
	rows int
	draw func(c *Canvas)
}

func oracleScenes() []oracleScene {
	rawEdges := func(c *Canvas) {
		// Cell edges, last pixels, and raw out-of-range writes (callers
		// bounds-check, but the old lib tolerated these: -1 spills into cell 0).
		for _, p := range [][2]int{{0, 0}, {1, 0}, {0, 3}, {1, 3}, {2, 4}, {3, 7},
			{c.pxW - 1, c.pxH - 1}, {c.pxW - 2, c.pxH - 4}, {c.pxW - 1, 0}, {0, c.pxH - 1},
			{-1, 0}, {0, -1}, {-1, -1}, {-2, 0}, {0, -4}, {-3, -5},
			{c.pxW, 0}, {0, c.pxH}, {c.pxW, c.pxH}, {c.pxW + 1, c.pxH + 3}} {
			c.dc.Set(p[0], p[1])
		}
	}
	return []oracleScene{
		{"empty-4x4", 4, 4, func(c *Canvas) {}},
		{"empty-40x12-after-clear", 40, 12, func(c *Canvas) { c.dc.Set(5, 5); c.Clear() }},
		{"edges-4x4", 4, 4, rawEdges},
		{"edges-5x7", 5, 7, rawEdges},
		{"edges-17x9", 17, 9, rawEdges},
		{"stripes-9x6", 9, 6, func(c *Canvas) {
			for y := 0; y < c.pxH; y++ {
				for x := 0; x < c.pxW; x++ {
					if (x*3+y*5)%7 < 2 {
						c.dc.Set(x, y)
					}
				}
			}
		}},
		{"full-6x5", 6, 5, func(c *Canvas) {
			for y := 0; y < c.pxH; y++ {
				for x := 0; x < c.pxW; x++ {
					c.dc.Set(x, y)
				}
			}
		}},
		{"scene-coloured-40x12", 40, 12, func(c *Canvas) {
			c.SetScale(1)
			c.FillColoredDiskTagged(orbital.Vec3{X: -10}, 9, CellTag{Color: "#6B8E4E", BodyID: "x"})
			c.PlotDenseLineColored(orbital.Vec3{X: -30, Y: 3}, orbital.Vec3{X: 30, Y: -5}, "#E8C547", 1)
			c.PlotDenseLineColored(orbital.Vec3{X: -30, Y: -12}, orbital.Vec3{X: 30, Y: 12}, "#3DDC84", 3)
			c.PlotColoredTagged(orbital.Vec3{X: 20, Y: 15}, CellTag{Color: "#FF0000", IsVessel: true})
			c.SetCellLabelColored(2, 1, "label", lipgloss.Color("#9DC8FF"))
			c.SetCellOverlay(orbital.Vec3{X: 14, Y: 0}, '@')
			c.dc.Set(3, 40)
		}},
		{"scene-uncoloured-140x40", 140, 40, func(c *Canvas) {
			c.SetScale(1)
			c.RingColoredOutline(orbital.Vec3{X: 3, Y: -2}, 60, "#fff")
			c.PlotDenseLineColored(orbital.Vec3{X: -100, Y: -70}, orbital.Vec3{X: 120, Y: 60}, "", 1)
			c.dc.Set(c.pxW-1, c.pxH-1)
		}},
		{"bg-tint-12x6", 12, 6, func(c *Canvas) {
			c.SetCellBackground(1, 1, "#112233")
			c.SetCellBackground(5, 2, "#112233")
			c.dc.Set(10, 6) // cell (5,1)
			c.dc.Set(11, 11)
		}},
	}
}

func renderOracle(s oracleScene) string {
	c := NewCanvas(s.cols, s.rows)
	s.draw(c)
	var b strings.Builder
	b.WriteString(c.String())
	b.WriteString("\n--get--\n")
	for y := 0; y < c.pxH; y++ {
		for x := 0; x < c.pxW; x++ {
			if c.SubPixelSet(x, y) {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// oracleHashes: sha256 of renderOracle, captured from the drawille canvas.
var oracleHashes = map[string]string{
	"empty-4x4":               "0d7bd89c58055054d15b2377681dfb9a7411aa31ad19d3db9d7205016f74a5d4",
	"empty-40x12-after-clear": "c3423257423cb82abe12b449d998fc8faf1171ecfb0adb3022258bd45133a759",
	"edges-4x4":               "d12d0f7c0030351ae600125fbb1728c14d9936a5a91a9835770a96a30230eb5c",
	"edges-5x7":               "b1aa456e42408f0dc024033f1cadc913cacabbde6942b1b5c81449eedb361aea",
	"edges-17x9":              "006e16772e7697e544c4c2b65ac7c3466d9b12c08f2cad20cf49bb5c695479e2",
	"stripes-9x6":             "f395d12af63d62a49a34563ffdf9c6b3b6c7fe34d7779dc5f7831e16bf41d662",
	"full-6x5":                "3b153c53c911da520fd13b93bad819f2b86ca7b9105b62fc68b22e940d4750d5",
	"scene-coloured-40x12":    "32c3fd512fd367991c2a8fe95cb562bf22f2569ccd89333c7d125fa90d490e23",
	"scene-uncoloured-140x40": "8384666ab1889d02df5f604909d65c8f3d5228c7bff6ddaaa743835e852bec53",
	"bg-tint-12x6":            "d2347a3cec4813948f4fd0781210eae4d489324ba82ed1c26da24a8c7d03ed22",
}

func TestBrailleOracleByteIdentical(t *testing.T) {
	trueColour(t)
	for _, s := range oracleScenes() {
		out := renderOracle(s)
		sum := sha256.Sum256([]byte(out))
		got := hex.EncodeToString(sum[:])
		if dir := os.Getenv("ORACLE_DUMP"); dir != "" {
			os.WriteFile(dir+"/"+s.name+".txt", []byte(out), 0o644)
			fmt.Printf("\t%q: %q,\n", s.name, got)
			continue
		}
		if want := oracleHashes[s.name]; got != want {
			t.Errorf("%s: output changed\n got  %s\n want %s", s.name, got, want)
		}
	}
}
