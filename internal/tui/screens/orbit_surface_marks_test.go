package screens

// One-line reason for a new test file: the marks file it covers is new.

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
)

func marksWorld(t *testing.T, n int) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	body := w.ActiveCraft().Primary.EnglishName
	for i := 0; i < n; i++ {
		// A Fibonacci spread over the whole sphere: about half face the camera.
		lat := math.Asin(1-2*(float64(i)+0.5)/float64(n)) * 180 / math.Pi
		lon := math.Mod(float64(i)*137.508, 360) - 180
		kind := sim.SiteFlag
		if i%5 == 0 {
			kind = sim.SiteWreck
		}
		w.Sites = append(w.Sites, sim.Site{ID: uint64(i + 1), Body: body, System: w.System().Name,
			LatDeg: lat, LonDeg: lon, Kind: kind, Name: fmt.Sprintf("s%d", i+1)})
	}
	return w
}

func marksFrame(v *OrbitView, w *sim.World) string { return v.Render(w, 0, 200, 60) }

func marksCount(out string) (flags, wrecks int) {
	return strings.Count(out, "⚑"), strings.Count(out, "☠")
}

// 60 flags and wrecks over the globe: the limb hides the far side, a row off
// hides its kind, and a targeted hidden mark draws alone (the equal case: one
// site is one glyph).
func TestSurfaceMarksFilterAndLimb(t *testing.T) {
	v := NewOrbitView(Theme{})
	v.Resize(200, 60)
	w := marksWorld(t, 60)
	f, k := marksCount(marksFrame(v, w))
	if f+k == 0 || f+k > 45 || k == 0 || f == 0 {
		t.Fatalf("60 marks: %d flags %d wrecks drawn, want some of each and at most 45 cells (limb cull: 32 with it, 55 without)", f, k)
	}
	s := v.settings
	s.SetChip(settings.ChipMarksFlags, false)
	v.SetSettings(s)
	if f2, k2 := marksCount(marksFrame(v, w)); f2 != 0 || k2 != k {
		t.Errorf("flags off: %d flags %d wrecks, want 0 and %d", f2, k2, k)
	}
	s.SetChip(settings.ChipMarksWrecks, false)
	v.SetSettings(s)
	if f2, k2 := marksCount(marksFrame(v, w)); f2+k2 != 0 {
		t.Errorf("all off: %d marks drawn, want 0", f2+k2)
	}
	// Target a flag that faces the camera: it draws alone.
	for _, st := range w.Sites {
		w.SetTargetSite(st.ID)
		f2, k2 := marksCount(marksFrame(v, w))
		if f2+k2 == 1 {
			return
		}
	}
	t.Error("no targeted hidden mark drew alone")
}
