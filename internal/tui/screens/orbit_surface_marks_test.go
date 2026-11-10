package screens

// One-line reason for a new test file: the marks file it covers is new.

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
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

// lzMapCells renders the map focused on the vessel at the given multiple of
// the LZ-circle threshold scale and returns, for each LZ of site s, its centre
// cell's rune and which of eight points on the circle sit on an inked cell.
func lzMapCells(t *testing.T, v *OrbitView, w *sim.World, s sim.Site, xThreshold float64) (centre map[int]rune, ink map[int][8]bool) {
	t.Helper()
	v.Resize(DesignWidth, DesignHeight)
	v.Render(w, 0, DesignWidth, DesignHeight)
	thr := sim.LZMapMinCells / sim.SiteLZRadiusM
	v.setUserZoom(xThreshold * thr / v.baseScale)
	v.Render(w, 0, DesignWidth, DesignHeight)
	rows := strings.Split(ansi.Strip(v.canvas.String()), "\n")
	at := func(col, row int) rune {
		if row < 0 || row >= len(rows) || col < 0 || col >= len([]rune(rows[row])) {
			return 0
		}
		return []rune(rows[row])[col]
	}
	centre, ink = map[int]rune{}, map[int][8]bool{}
	for _, p := range w.LZPlaces(s) {
		if px, py, on := v.canvas.Project(p.Pos); on {
			centre[p.N] = at(px/2, py/4)
		}
		e1 := p.Up.Cross(orbital.Vec3{Z: 1}).Unit()
		e2 := p.Up.Cross(e1)
		var pts [8]bool
		for i := range pts {
			th := float64(i) * math.Pi / 4
			q := p.Pos.Add(e1.Scale(sim.SiteLZRadiusM * math.Cos(th))).Add(e2.Scale(sim.SiteLZRadiusM * math.Sin(th)))
			if px, py, on := v.canvas.Project(q); on {
				c := at(px/2, py/4)
				pts[i] = c != '\u2800' && c != ' ' && c != 0
			}
		}
		ink[p.N] = pts
	}
	return
}

// newInk counts circle points inked with the circles on that the baseline
// (the same frame, circles off) left blank: the circle's own ink, apart from
// the orbit, the vessel and the labels that share the frame.
func newInk(on, off [8]bool) (n int) {
	for i := range on {
		if on[i] && !off[i] {
			n++
		}
	}
	return
}

// ADR 0068 decision 8, T8b: from four cells across the map draws each LZ as a
// circle with its digit at the centre and the flag at the one you fly to;
// further out it is the one glyph. The `y` flags row governs the circles (an
// untargeted site hidden by it draws none) and a shipped zone, which has no
// glyph, shows only as circles.
func TestSurfaceMarksLZCircles(t *testing.T) {
	w := lzRingWorld(t, 7)
	w.Focus = sim.Focus{Kind: sim.FocusCraft}
	s := w.Sites[0]
	v := NewOrbitView(Theme{})
	// The baseline: the same frame with the site hidden (untargeted, flags row off).
	hidden := NewOrbitView(Theme{})
	st := hidden.settings
	st.SetChip(settings.ChipMarksFlags, false)
	hidden.SetSettings(st)
	w.ClearTarget()
	_, baseFar := lzMapCells(t, hidden, w, s, 0.5)
	_, baseNear := lzMapCells(t, hidden, w, s, 2)
	w.SetTargetSite(s.ID)

	far, farInk := lzMapCells(t, v, w, s, 0.5)
	if far[2] == '2' || newInk(farInk[2], baseFar[2]) != 0 || newInk(farInk[7], baseFar[7]) != 0 {
		t.Errorf("half the threshold zoom drew circles: LZ-2 centre %q, ink %v", far[2], farInk[2])
	}
	near, nearInk := lzMapCells(t, v, w, s, 2)
	if near[1] != flagGlyph || near[2] != '2' || near[7] != '7' {
		t.Errorf("twice the threshold: centres LZ-1 %q (want the flag) LZ-2 %q LZ-7 %q", near[1], near[2], near[7])
	}
	if a, b := newInk(nearInk[2], baseNear[2]), newInk(nearInk[7], baseNear[7]); a < 4 || b < 4 {
		t.Errorf("circles not inked about their own centres: LZ-2 %d LZ-7 %d of 8 points", a, b)
	}
	// A claim on LZ-2 moves the flag there and leaves LZ-1 its digit.
	w.ActiveCraft().LZClaim = spacecraft.LZClaim{SiteID: s.ID, N: 2}
	if c, _ := lzMapCells(t, v, w, s, 2); c[2] != flagGlyph || c[1] != '1' {
		t.Errorf("claimed LZ-2: centres LZ-1 %q LZ-2 %q, want the digit 1 and the flag", c[1], c[2])
	}
	w.ActiveCraft().LZClaim = spacecraft.LZClaim{}
	// Untargeted: the digit 1 and no flag; the flags row off hides the lot.
	w.ClearTarget()
	if c, _ := lzMapCells(t, v, w, s, 2); c[1] != '1' || c[2] != '2' {
		t.Errorf("untargeted site: LZ-1 %q LZ-2 %q, want digits 1 and 2", c[1], c[2])
	}
	if c, ink := lzMapCells(t, hidden, w, s, 2); c[2] == '2' || newInk(ink[2], baseNear[2]) != 0 {
		t.Errorf("flags row off still drew circles: %q", c[2])
	}
	// A shipped zone: Tranquility Base, no glyph far, circles near.
	w2 := moonSiteDescentWorld(t, false)
	var zone sim.Site
	for _, z := range w2.ShippedZones() {
		if z.Name == "Tranquility Base" {
			zone = z
		}
	}
	placeCraftNear(w2, w2.LZPlaces(zone)[0])
	w2.ClearTarget()
	w2.Focus = sim.Focus{Kind: sim.FocusCraft}
	if c, _ := lzMapCells(t, NewOrbitView(Theme{}), w2, zone, 0.5); c[1] == flagGlyph || c[1] == '1' {
		t.Errorf("a shipped zone drew a glyph far out: %q", c[1])
	}
	c, ink := lzMapCells(t, NewOrbitView(Theme{}), w2, zone, 2)
	_, base := lzMapCells(t, hidden, w2, zone, 2)
	if c[1] != '1' || c[2] != '2' || newInk(ink[2], base[2]) < 4 {
		t.Errorf("shipped zone near: LZ-1 %q LZ-2 %q ink %v", c[1], c[2], ink[2])
	}
}
