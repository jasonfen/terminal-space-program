// ADR 0057 decision 5.3 (slice S4): PROPELLANT's permanent fourth row,
// cargo: (what the vessel carries to deliver) and hold: (what its Cargo
// Hold has received). Every guard here is proven red by a sabotage that
// restores the OLD behaviour (no row, a stale value after u, a row that
// widens the box), recorded in the S4 log.

package screens

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/jasonfen/terminal-space-program/internal/missions"
	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/physics"
	"github.com/jasonfen/terminal-space-program/internal/settings"
	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// cargoRow is the PROPELLANT box's fourth line, ANSI stripped.
func cargoRow(t *testing.T, w *sim.World) string {
	t.Helper()
	v := NewOrbitView(launchThemeForTest())
	lines := v.buildPropellantBox(w)
	if len(lines) != 5 {
		t.Fatalf("PROPELLANT is %d lines, want 5 (title plus four rows)", len(lines))
	}
	return ansi.Strip(lines[4])
}

// cargoCells splits a cargo row into its two value cells.
func cargoCells(t *testing.T, row string) (cargo, hold string) {
	t.Helper()
	ci, hi := strings.Index(row, "cargo:"), strings.Index(row, "hold:")
	if ci < 0 || hi < 0 || hi < ci {
		t.Fatalf("row %q lacks cargo: then hold:", row)
	}
	return strings.TrimSpace(row[ci+len("cargo:") : hi]), strings.TrimSpace(row[hi+len("hold:"):])
}

func placeInOrbit(w *sim.World, c *spacecraft.Spacecraft) {
	earth := w.Systems[0].FindBody("Earth")
	c.Primary = *earth
	r := orbital.Vec3{X: earth.RadiusMeters() + 500e3}
	c.State = physics.StateVector{R: r, V: orbital.Vec3{Y: math.Sqrt(earth.GravitationalParameter() / r.Norm())}}
	c.State.M = c.TotalMass()
}

func holdStation(t *testing.T, withHold bool) *spacecraft.Spacecraft {
	t.Helper()
	ids := []string{"probe-core", "docking-port"}
	if withHold {
		ids = append(ids, "cargo-hold")
	}
	st, warn := spacecraft.ComposeStage(ids, spacecraft.Components)
	if warn != "" {
		t.Fatal(warn)
	}
	st.Name = "Depot"
	c := spacecraft.NewFromStages([]spacecraft.Stage{st})
	c.Name = "Depot-1"
	return c
}

func cargoDragonWorld(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := spacecraft.NewFromLoadout("Cargo-Dragon")
	placeInOrbit(w, c)
	w.Crafts[w.ActiveCraftIdx] = c
	return w
}

func stationWorld(t *testing.T, withHold bool) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	c := holdStation(t, withHold)
	placeInOrbit(w, c)
	w.Crafts[w.ActiveCraftIdx] = c
	return w
}

// dockedDragonAtStation: a Cargo Dragon (active) docked to a station with a
// hold; one composite on the slate.
func dockedDragonAtStation(t *testing.T) *sim.World {
	t.Helper()
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	dragon, station := spacecraft.NewFromLoadout("Cargo-Dragon"), holdStation(t, true)
	placeInOrbit(w, dragon)
	placeInOrbit(w, station)
	w.Crafts = []*spacecraft.Spacecraft{dragon, station}
	w.ActiveCraftIdx = 0
	w.DockCrafts(0, 1)
	if len(w.Crafts) != 1 {
		t.Fatalf("DockCrafts left %d vessels", len(w.Crafts))
	}
	return w
}

// Guard: Apollo TLI-1 (the default world) prints cargo: and hold: as dashes,
// in a fifth line, red on the three-row box.
func TestPropellantCargoRowApolloReadsDashes(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	row := cargoRow(t, w)
	cargo, hold := cargoCells(t, row)
	if cargo != "—" || hold != "—" {
		t.Errorf("Apollo row %q: cargo %q hold %q, want both —", row, cargo, hold)
	}
	if !strings.HasPrefix(row, "  cargo:    —") {
		t.Errorf("row %q does not start with the value column's pin", row)
	}
}

// Guard: a Cargo Dragon prints its cargo as mass and no hold.
func TestPropellantCargoRowCargoDragon(t *testing.T) {
	row := cargoRow(t, cargoDragonWorld(t))
	cargo, hold := cargoCells(t, row)
	if cargo != "3.307 t" || hold != "—" {
		t.Errorf("Cargo Dragon row %q: cargo %q hold %q, want 3.307 t / —", row, cargo, hold)
	}
}

// Guard: a hold with nothing in it reads 0 kg (this vessel can receive);
// a station without a hold reads a dash.
func TestPropellantCargoRowEmptyHoldReadsZero(t *testing.T) {
	cargo, hold := cargoCells(t, cargoRow(t, stationWorld(t, true)))
	if cargo != "—" || hold != "0 kg" {
		t.Errorf("hold station: cargo %q hold %q, want — / 0 kg", cargo, hold)
	}
	cargo, hold = cargoCells(t, cargoRow(t, stationWorld(t, false)))
	if cargo != "—" || hold != "—" {
		t.Errorf("no-hold station: cargo %q hold %q, want — / —", cargo, hold)
	}
}

// Guard: after u the same vessel reads cargo — and hold 3.307 t. The row is
// read live from the stages, not cached at dock time.
func TestPropellantCargoRowFollowsHandOver(t *testing.T) {
	w := dockedDragonAtStation(t)
	cargo, hold := cargoCells(t, cargoRow(t, w))
	if cargo != "3.307 t" || hold != "0 kg" {
		t.Fatalf("docked, before u: cargo %q hold %q, want 3.307 t / 0 kg", cargo, hold)
	}
	if _, refusal := w.HandOverCargo(w.ActiveCraftIdx); refusal != "" {
		t.Fatalf("u refused: %q", refusal)
	}
	cargo, hold = cargoCells(t, cargoRow(t, w))
	if cargo != "—" || hold != "3.307 t" {
		t.Errorf("after u: cargo %q hold %q, want — / 3.307 t", cargo, hold)
	}
}

// Guard: no vessel at all still prints five lines, the fifth being the row.
func TestPropellantBoxNoVesselIsFiveLines(t *testing.T) {
	w, err := sim.NewWorld()
	if err != nil {
		t.Fatal(err)
	}
	w.Crafts = nil
	v := NewOrbitView(launchThemeForTest())
	lines := v.buildPropellantBox(w)
	if len(lines) != 5 {
		t.Fatalf("no-vessel PROPELLANT is %d lines, want 5", len(lines))
	}
	if cargo, hold := cargoCells(t, ansi.Strip(lines[4])); cargo != "—" || hold != "—" {
		t.Errorf("no-vessel row cargo %q hold %q, want dashes", cargo, hold)
	}
}

// tallestMissionWorld sets w's mission slate to the catalog step whose
// MISSION box is tallest and returns the line count.
func tallestMissionWorld(t *testing.T, w *sim.World) int {
	t.Helper()
	cat, err := missions.DefaultCatalog()
	if err != nil {
		t.Fatal(err)
	}
	v := NewOrbitView(launchThemeForTest())
	best, bestLines := missions.Mission{}, 0
	for _, m := range cat.Missions {
		for cur := range m.Objectives {
			mm := m
			mm.Objectives = append([]missions.Objective(nil), m.Objectives...)
			for j := range mm.Objectives {
				mm.Objectives[j].Status = missions.InProgress
				if j < cur {
					mm.Objectives[j].Status = missions.Passed
				}
			}
			mm.Status = missions.InProgress
			mm.Requires = nil
			if n := len(v.missionChipLines("", false, &mm, 0)); n > bestLines {
				best, bestLines = mm, n
			}
		}
	}
	w.Missions = []missions.Mission{best}
	if w.ActiveMission() == nil {
		t.Fatal("the tallest step is not the active mission")
	}
	return bestLines
}

// Alignment at the Design Size, measured on rendered columns with the widest
// real values: a Cargo Dragon docked at a hold station with the tallest
// MISSION step. The box keeps its tier width, hold: lands on mass:'s column
// in every vessel state, and the left column's last box ends clear of the
// compass labels (rows 37-38 on a 40-row screen).
func TestPropellantCargoRowAlignsAtDesignSize(t *testing.T) {
	type state struct {
		name string
		w    *sim.World
	}
	docked := dockedDragonAtStation(t)
	delivered := dockedDragonAtStation(t)
	delivered.HandOverCargo(delivered.ActiveCraftIdx)
	states := []state{
		{"cargo dragon", cargoDragonWorld(t)},
		{"docked before u", docked},
		{"docked after u", delivered},
		{"apollo", func() *sim.World { w, _ := sim.NewWorld(); return w }()},
	}
	for _, s := range states {
		t.Run(s.name, func(t *testing.T) {
			steps := tallestMissionWorld(t, s.w)
			v := NewOrbitView(launchThemeForTest())
			v.Resize(140, 40)
			out := ansi.Strip(v.Render(s.w, 0, 140, 40))
			rows := strings.Split(out, "\n")
			var propRect, missionRect *chipRect
			for i := range v.chipRects {
				switch v.chipRects[i].id {
				case settings.ChipPropellant:
					propRect = &v.chipRects[i]
				case settings.ChipMissions:
					missionRect = &v.chipRects[i]
				}
			}
			if propRect == nil || missionRect == nil {
				t.Fatalf("PROPELLANT/MISSION rects missing: %+v", v.chipRects)
			}
			if got := propRect.colEnd - propRect.colStart + 1; got != tierTopLeftWidth {
				t.Errorf("PROPELLANT is %d wide, want the tier pin %d", got, tierTopLeftWidth)
			}
			if h := propRect.rowEnd - propRect.rowStart + 1; h != 7 {
				t.Errorf("PROPELLANT is %d rows tall with its border, want 7 (title plus four rows)", h)
			}
			// hold: lands on mass:'s display column.
			var massCol, holdCol = -1, -1
			for r := propRect.rowStart; r <= propRect.rowEnd && r < len(rows); r++ {
				if c, ok := labelCol(rows[r], "mass:"); ok {
					massCol = c
				}
				if c, ok := labelCol(rows[r], "hold:"); ok {
					holdCol = c
				}
			}
			if massCol < 0 || holdCol < 0 || massCol != holdCol {
				t.Errorf("mass: at col %d, hold: at col %d, want one column", massCol, holdCol)
			}
			// The left column ends above the compass labels.
			t.Logf("%d-line MISSION step: PROPELLANT rows %d-%d, MISSION rows %d-%d", steps, propRect.rowStart, propRect.rowEnd, missionRect.rowStart, missionRect.rowEnd)
			if missionRect.rowEnd >= 36 {
				t.Errorf("MISSION ends on row %d; rows 37-38 (0-indexed 36-37) hold the compass labels", missionRect.rowEnd)
			}
			// Nothing overlaps: every chip rect on the left is disjoint.
			for i, a := range v.chipRects {
				for _, b := range v.chipRects[i+1:] {
					if a.colStart <= b.colEnd && b.colStart <= a.colEnd && a.rowStart <= b.rowEnd && b.rowStart <= a.rowEnd {
						t.Errorf("chips %v and %v overlap: %+v / %+v", a.id, b.id, a, b)
					}
				}
			}
			_ = fmt.Sprint
		})
	}
}
