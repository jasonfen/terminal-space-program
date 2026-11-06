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

// F2: a partner's flag and wreck draw (ghost Dim) from the ledger, the
// "partners' marks" row governs them, and a targeted one draws alone.
// F3 (drawing half): the target is owner plus id, so your own id-1 flag does
// not draw in TARGET colour while the partner's id 1 is aimed.
func TestSurfaceMarksPartnerSites(t *testing.T) {
	v := NewOrbitView(Theme{})
	v.Resize(200, 60)
	w := marksWorld(t, 0)
	body, sys := w.ActiveCraft().Primary.EnglishName, w.System().Name
	// Two spots on opposite sides of the disk so one faces the camera.
	for i, lon := range []float64{0, 180} {
		w.PartnerSites = append(w.PartnerSites, sim.PartnerSite{Handle: "pal", Site: sim.Site{
			ID: uint64(i + 1), Owner: "SHA256:pal", Body: body, System: sys, LonDeg: lon,
			Kind: []sim.SiteKind{sim.SiteFlag, sim.SiteWreck}[i], Name: "theirs"}})
	}
	f, k := marksCount(marksFrame(v, w))
	if f+k != 1 {
		t.Fatalf("partner sites: %d flags %d wrecks drawn, want exactly the near-side one", f, k)
	}
	s := v.settings
	s.SetChip(settings.ChipMarksPartners, false)
	v.SetSettings(s)
	if f, k = marksCount(marksFrame(v, w)); f+k != 0 {
		t.Errorf("partners' marks off: %d drawn, want 0", f+k)
	}
	// Equal case: own id 1 and the aimed partner id 1; only the partner is targeted.
	w.Sites = append(w.Sites, sim.Site{ID: 1, Body: body, System: sys, LonDeg: 30, Name: "mine"})
	w.SetTargetPartnerSite("SHA256:pal", 1)
	if w.Target.Kind != sim.TargetSite {
		t.Fatalf("partner site not aimed: %+v", w.Target)
	}
	s.SetChip(settings.ChipMarksPartners, true)
	v.SetSettings(s)
	own := sim.Site{ID: 1, Owner: ""}
	if v.siteTargeted(w, own) || !v.siteTargeted(w, w.PartnerSites[0].Site) {
		t.Errorf("owner not part of the target match")
	}
}
