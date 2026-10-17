package sim

import (
	"fmt"
	"math"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/orbital"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// The [H] Moon transfer sweep behind the 2026-10-17 diagnosis (v0.57.0):
// every lunar-capable loadout from a 500 km 51.6 deg spawn at seven
// epochs, flown with G through the capture. It measures rates, it does
// not assert; the guards are in moon_plane_change_frame_test.go. Run with
//
//	DIAG_SWEEP=1 go test ./internal/sim -run TestDiagFastSweep -v
//
// (DIAG_LOADOUTS=a,b narrows it; "default" is the seed vessel, "fresh"
// the fresh game's lunar-window start.)

// TestDiagFastSweep: per loadout x epoch, the PC frame, the flown PC's
// speed vs the impulsive model, HUD vs flown perilune, capture timing and
// the captured orbit. Gated on DIAG_SWEEP=1.
func TestDiagFastSweep(t *testing.T) {
	if os.Getenv("DIAG_SWEEP") == "" {
		t.Skip("set DIAG_SWEEP=1")
	}
	ids := []string{"default", "fresh", "Apollo-TLI", "S-IVB-1", "Comsat-Carrier-3", "RCS-tug", "ICPS", "Relay-Tug", "Science-Probe", "Apollo-Stack", "Kern-Stack", "Capsule", "Lander"}
	if v := os.Getenv("DIAG_LOADOUTS"); v != "" && v != "none" {
		ids = strings.Split(v, ",")
	}
	for _, id := range ids {
		for _, d := range []int{0, 3, 7, 11, 15, 19, 23} {
			if id == "fresh" && d > 0 {
				break
			}
			w := mustWorld(t)
			if id == "fresh" {
				w.AdjustStartForLunarTransferWindow(DefaultLunarTransferLead)
			} else {
				w.Clock.SimTime = w.Clock.SimTime.Add(time.Duration(d) * 24 * time.Hour)
			}
			if id != "default" && id != "fresh" {
				if _, err := w.SpawnCraft(SpawnSpec{LoadoutID: id, ParentBodyID: "earth", AltitudeM: 500e3, Inclination: spacecraft.SeedInclinationDeg}); err != nil {
					t.Logf("%s: spawn %v", id, err)
					break
				}
				w.ActiveCraftIdx = len(w.Crafts) - 1
			}
			c := w.ActiveCraft()
			mi := moonIndex(w)
			w.SetTargetBody(mi)
			dvHave := c.RemainingDeltaV()
			if _, err := w.PlanTransfer(mi); err != nil {
				t.Logf("%-17s +%2dd: PlanTransfer refused: %v", id, d, err)
				continue
			}
			need := 0.0
			var capT time.Time
			for _, n := range c.Nodes {
				if n.PrimaryID == "moon" {
					capT = n.TriggerTime
					continue
				}
				need += n.DV
			}
			if need > dvHave {
				t.Logf("%-17s +%2dd: plan needs %.0f m/s, vessel has %.0f: skipped (cannot reach)", id, d, need, dvHave)
				break
			}
			strategy := w.LastTransfer.Strategy
			pcFrame, pcV := "-", ""
			hud, flownPe, tpCap, capPe := math.NaN(), math.NaN(), math.NaN(), math.NaN()
			crashedBeforeCapture := false
			burn := func() {
				for k := 0; k < 3; k++ {
					tickUntil(w, 5_000_000, func() bool { return c.ActiveBurn == nil || c.BurnStalled() || c.Crashed })
					if !c.BurnStalled() {
						return
					}
					w.StageActive(w.ActiveCraftIdx)
				}
			}
			for leg := 0; leg < 6 && len(c.Nodes) > 0 && !c.Crashed; leg++ {
				if !w.EngageAutoWarp() {
					break
				}
				tickUntil(w, 5_000_000, func() bool { return w.AutoWarp == nil || c.Crashed })
				next := c.Nodes[0]
				n := len(c.Nodes)
				tickUntil(w, 5_000_000, func() bool { return len(c.Nodes) < n || c.Crashed })
				if c.Crashed {
					crashedBeforeCapture = true
					break
				}
				switch {
				case next.Mode == spacecraft.BurnPlaneChange:
					pcFrame = c.Primary.ID
					pre := c.State
					burn()
					model := pre.V.Add(spacecraft.NodeBurnDirection(next, pre.R, pre.V).Scale(next.DV)).Norm()
					pcV = fmt.Sprintf("|v| %.0f->%.0f (model %.0f, theta %.0f)", pre.V.Norm(), c.State.V.Norm(), model, next.PlaneChangeRad*180/math.Pi)
					tickUntil(w, 5_000_000, func() bool { return c.Primary.ID == "moon" || c.Crashed })
					if c.Primary.ID == "moon" {
						el := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
						flownPe = (el.Periapsis() - c.Primary.RadiusMeters()) / 1e3
					}
				case next.PrimaryID == "moon":
					if c.Primary.ID == "moon" {
						tp, _ := orbital.TimeToPeriapsisHyperbolic(orbital.Vec3State{R: c.State.R, V: c.State.V}, c.Primary.GravitationalParameter())
						tpCap = tp / 3600
					}
					burn()
					if c.Primary.ID == "moon" {
						el := orbital.ElementsFromState(c.State.R, c.State.V, c.Primary.GravitationalParameter())
						capPe = (el.Periapsis() - c.Primary.RadiusMeters()) / 1e3
					}
				default:
					burn()
					hud = hudMoonPerilune(w)
				}
			}
			_ = capT
			outcome := "captured clear"
			switch {
			case crashedBeforeCapture || (c.Crashed && math.IsNaN(capPe)):
				outcome = "IMPACT before capture"
			case math.IsNaN(capPe):
				outcome = "no capture"
			case capPe < 0:
				outcome = "captured, Pe BELOW surface"
			}
			t.Logf("%-17s +%2dd %-8s dv %5.0f/%5.0f PC@%-5s %-40s HUD Pe %6.0f flown Pe %6.0f capture %+5.2fh from perilune -> Pe %6.0f km: %s",
				id, d, strategy, need, dvHave, pcFrame, pcV, hud, flownPe, -tpCap, capPe, outcome)
		}
	}
}
