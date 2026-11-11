package sim

import (
	"math"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// moonLanderWorld has the active vessel soft-landed on the Moon at (12.5N, 33W)
// plus a flag planted there, and a second vessel to aim from.
func siteWorld(t *testing.T) (w *World, lander *spacecraft.Spacecraft, siteID uint64) {
	t.Helper()
	w, err := NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.navErrorOff = true
	lander = w.ActiveCraft()
	for _, b := range w.System().Bodies {
		if b.ID == "moon" {
			lander.Primary = b
		}
	}
	if lander.Primary.ID != "moon" {
		t.Fatal("setup: no moon")
	}
	lander.Landed, lander.LandedLatDeg, lander.LandedLonDeg = true, 12.5, -33
	integrateLanded(w, lander, time.Second)
	w.plantSite(lander, SiteFlag, "site", 12.5, -33)
	return w, lander, w.Sites[0].ID
}

// ADR 0066 guard 3: a site target resolves to exactly the state a landed
// vessel at the same lat/lon has, a day apart.
func TestTargetStateOfSiteEqualsLandedVesselAtSameLatLon(t *testing.T) {
	w, lander, id := siteWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatal(err)
	}
	for day := 0; day < 2; day++ {
		w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(day) * 24 * time.Hour)
		integrateLanded(w, lander, time.Second)
		w.SetTargetCraft(0)
		vessel, ok := w.TargetState()
		if !ok {
			t.Fatal("vessel target did not resolve")
		}
		w.SetTargetSite(id)
		site, ok := w.TargetState()
		if !ok {
			t.Fatal("site target did not resolve")
		}
		if d := site.R.Sub(vessel.R).Norm(); d > 1e-6 {
			t.Errorf("day %d: R differs by %g m, want <= 1e-6", day, d)
		}
		if d := site.V.Sub(vessel.V).Norm(); d > 1e-9 {
			t.Errorf("day %d: V differs by %g m/s, want <= 1e-9", day, d)
		}
	}
}

func TestSiteTargetNamesRefusesAndSharesPrimary(t *testing.T) {
	w, lander, id := siteWorld(t)
	if _, err := w.SpawnCraft(SpawnSpec{Alongside: true}); err != nil {
		t.Fatal(err)
	}
	w.SetTargetSite(id)
	if w.Target.Kind != TargetSite || w.Target.SiteID != id || w.TargetName() != lander.Name+" site" {
		t.Errorf("target = %+v name %q", w.Target, w.TargetName())
	}
	if w.HasRelativeTarget() {
		t.Error("a site is not a relative (vessel) target")
	}
	if st, ok := w.ProximityState(); !ok || !st.Surface || st.HasPort {
		t.Errorf("Proximity View on a site on this primary: ok=%v Surface=%v, want the landing scene with no port", ok, st.Surface)
	}
	if _, ok := w.launchWindowTargetNormal(); ok {
		t.Error("a launch window must refuse a site as it refuses a landed vessel")
	}
	if _, ok := w.rendezvousTargetPrimary(); ok {
		t.Error("the Rendezvous Planner must refuse a site")
	}
	for _, b := range w.System().Bodies {
		if b.ID == "earth" {
			w.ActiveCraft().Primary = b
		}
	}
	if w.TargetSharesActivePrimary() {
		t.Error("an Earth vessel does not share the Moon site's primary")
	}
	w.SetTargetSite(id + 99)
	if w.Target.Kind != TargetNone {
		t.Errorf("unknown site id left target %+v, want none", w.Target)
	}
}

func TestSiteSharesPrimaryWithLanderAndSurvivesRename(t *testing.T) {
	w, lander, id := siteWorld(t)
	w.SetTargetSite(id)
	lander.Landed = false // aim from the same body without the landed pin
	if !w.TargetSharesActivePrimary() {
		t.Error("a Moon vessel shares the Moon site's primary")
	}
	if rT, vT, ok := w.TargetStateRelativeToActivePrimary(); !ok || math.Abs(rT.Norm()-lander.Primary.RadiusMeters()) > 1e-3 || vT.Norm() == 0 {
		t.Errorf("relative state ok=%v |r|=%g |v|=%g", ok, rT.Norm(), vT.Norm())
	}
	if w.RenameSite(id, "") || w.Sites[0].Name != lander.Name+" site" {
		t.Error("empty rename must be refused")
	}
	if !w.RenameSite(id, "Tranquility") || w.TargetName() != "Tranquility" {
		t.Errorf("rename: name %q", w.TargetName())
	}
	if w.RenameSite(id+1, "x") {
		t.Error("renaming an unknown id must fail")
	}
}

// ADR 0066 guard 6 (model half): removing a site clears a target on it and
// leaves other sites; an unknown id removes nothing.
func TestRemoveSiteClearsTargetOnIt(t *testing.T) {
	w, lander, id := siteWorld(t)
	w.plantSite(lander, SiteWreck, "wreck", 1, 2)
	other := w.Sites[1].ID
	w.SetTargetSite(id)
	if w.RemoveSite(id + 99) {
		t.Error("removing an unknown id reported success")
	}
	w.SetTargetSite(other)
	w.RemoveSite(id)
	if w.Target.Kind != TargetSite || w.Target.SiteID != other {
		t.Errorf("removing another site disturbed the target: %+v", w.Target)
	}
	w.SetTargetSite(id + 0) // already removed: clears
	w.SetTargetSite(other)
	if !w.RemoveSite(other) || w.Target.Kind != TargetNone || len(w.Sites) != 0 {
		t.Errorf("remove target site: target %+v sites %+v", w.Target, w.Sites)
	}
}

// Equal id: deleting your id-1 flag must not clear an aim on a partner's id-1 flag.
func TestRemoveSiteKeepsPartnerTargetWithSameID(t *testing.T) {
	w, _, _, _, _, _ := moonSiteWorld(t)
	moon := siteBody(w, "moon")
	sys := w.System().Name
	w.Sites = append(w.Sites, Site{ID: 1, Body: moon.EnglishName, System: sys})
	w.PartnerSites = []PartnerSite{{Site: Site{ID: 1, Owner: "p", Body: moon.EnglishName, System: sys}}}
	w.SetTargetPartnerSite("p", 1)
	if !w.RemoveSite(1) {
		t.Fatal("own site 1 not removed")
	}
	if w.Target.Kind != TargetSite || w.Target.GhostOwner != "p" {
		t.Errorf("partner aim cleared: %+v", w.Target)
	}
}

// F4: a partner's site that vanished (or a solo save with the aim persisted)
// keeps a pending name, not a blank TARGET title.
func TestTargetNameVanishedPartnerSiteIsPending(t *testing.T) {
	w, _, _, _, _, _ := moonSiteWorld(t)
	w.Target = Target{Kind: TargetSite, SiteID: 7, GhostOwner: "p"}
	if got := w.TargetName(); got != "pending target lock" {
		t.Errorf("name = %q, want pending target lock", got)
	}
}

// ADR 0068 guard 3 (S6): the eighth + makes nine LZs, the next adds nothing
// (the equal case is the cap itself); a stamp moves so the ledger re-publishes.
func TestAddLZStopsAtNineAndStampsTheSite(t *testing.T) {
	w, _, _ := moonLZWorld(t)
	id := w.Sites[0].ID
	w.Sites[0].LZCount = 1
	w.Sites[0].UpdatedAtNano = 1
	for n := 2; n <= SiteMaxLZs; n++ {
		if !w.AddLZ(id) || w.Sites[0].LZs() != n {
			t.Fatalf("+ number %d: LZs = %d, want %d", n-1, w.Sites[0].LZs(), n)
		}
	}
	if w.Sites[0].UpdatedAtNano <= 1 {
		t.Error("+ did not stamp the site, so the ledger would never see it")
	}
	if w.AddLZ(id) || w.Sites[0].LZs() != SiteMaxLZs {
		t.Errorf("the tenth LZ was added: %d", w.Sites[0].LZs())
	}
	if w.AddLZ(id+99) || w.AddLZ(shippedZones[0].ID) {
		t.Error("+ reached a site you do not own")
	}
	w.Sites[0].Kind, w.Sites[0].LZCount = SiteWreck, 1
	if w.AddLZ(id) {
		t.Error("+ added an LZ to a wreck")
	}
}

// ADR 0068 guards 3 and 12: - takes the last LZ, but not the only one, not one
// a vessel holds a claim on, and not one a vessel stands on.
func TestRemoveLZRefusesTheOnlyTheClaimedAndTheOccupied(t *testing.T) {
	w, c, o := moonLZWorld(t) // two LZs
	id := w.Sites[0].ID
	w.Sites[0].LZNames = []string{"", "Bravo"}
	c.LZClaim = spacecraft.LZClaim{SiteID: id, N: 2}
	if w.RemoveLZ(id) || w.Sites[0].LZs() != 2 {
		t.Fatal("- removed an LZ a vessel is flying to")
	}
	c.LZClaim = spacecraft.LZClaim{SiteID: id, N: 1} // the equal case: a claim on another LZ does not block
	stamp := w.Sites[0].UpdatedAtNano
	w.Target.LZ = 2
	if !w.RemoveLZ(id) || w.Sites[0].LZs() != 1 || len(w.Sites[0].LZNames) != 1 || w.Target.LZ != 0 || w.Sites[0].UpdatedAtNano <= stamp {
		t.Fatalf("- on a free last LZ: LZs %d names %v target LZ %d", w.Sites[0].LZs(), w.Sites[0].LZNames, w.Target.LZ)
	}
	if w.RemoveLZ(id) || w.Sites[0].LZs() != 1 {
		t.Error("- took the only LZ away")
	}
	w.Sites[0].LZCount = 2
	c.LZClaim = spacecraft.LZClaim{}
	lat, lon := w.Sites[0].lzLatLon(2, w.siteRadiusM(w.Sites[0]))
	standOn(o, lat, lon)
	if w.RemoveLZ(id) || w.Sites[0].LZs() != 2 {
		t.Error("- removed an LZ with a vessel standing on it")
	}
	w.Sites[0].Kind = SiteWreck
	standOn(o, 0, 0)
	if w.RemoveLZ(id) {
		t.Error("- touched a wreck")
	}
	if w.RemoveLZ(shippedZones[0].ID) || shippedZones[0].LZs() != 2 {
		t.Error("- touched a shipped zone")
	}
}
