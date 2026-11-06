// Package sim - the rows behind the `t` target menu (ADR 0066 decision 2).
// One-line reason for a new file: a new read-only view over targetCycle,
// Sites and Ghosts that no existing slice owns.

package sim

import (
	"sort"

	"github.com/jasonfen/terminal-space-program/internal/missions"
)

// TargetGroup is a menu section, in display order.
type TargetGroup int

const (
	GroupVessels TargetGroup = iota
	GroupSites
	GroupPartners
	GroupBodies
)

// TargetRow is one selectable row. Target is what a commit aims at; Own marks
// a site you may rename or delete; Count is the released batch size (>1).
type TargetRow struct {
	Group TargetGroup
	Target
	Name  string
	DistM float64
	Own   bool
	Wreck bool
	Count int
}

// TargetMenu lists the targets by group, nearest first inside each, plus the
// row `t t` takes: the one a single `t` press aimed at before the menu (the
// step after the current target in targetCycle). open is -1 when it has no row.
func (w *World) TargetMenu() (rows []TargetRow, open int) {
	sys := w.System()
	var from = w.CraftInertial()
	ac := w.ActiveCraft()
	cycle := w.targetCycle()
	for _, t := range cycle {
		switch t.Kind {
		case TargetCraft:
			c, _, ok := w.craftByID(t.CraftID)
			if !ok {
				continue
			}
			n := 0
			if c.BatchID != 0 {
				for _, o := range w.Crafts {
					if o != nil && o.BatchID == c.BatchID && o != ac {
						n++
					}
				}
			}
			rows = append(rows, TargetRow{Group: GroupVessels, Target: t, Name: c.Name, Count: n,
				DistM: w.BodyPosition(c.Primary).Add(c.State.R).Sub(from).Norm()})
		case TargetBody:
			b := sys.Bodies[t.BodyIdx]
			rows = append(rows, TargetRow{Group: GroupBodies, Target: t, Name: b.EnglishName,
				DistM: w.BodyPosition(b).Sub(from).Norm()})
		}
	}
	if ac != nil {
		for _, s := range w.Sites {
			if s.Body != ac.Primary.EnglishName || s.System != sys.Name {
				continue
			}
			r, _ := surfacePointState(ac.Primary, s.LatDeg, s.LonDeg, w.Clock.SimTime)
			rows = append(rows, TargetRow{Group: GroupSites, Target: Target{Kind: TargetSite, SiteID: s.ID},
				Name: s.Name, Own: s.Owner == "", Wreck: s.Kind == SiteWreck,
				DistM: w.BodyPosition(ac.Primary).Add(r).Sub(from).Norm()})
		}
	}
	for _, g := range w.Ghosts {
		rows = append(rows, TargetRow{Group: GroupPartners, Target: Target{Kind: TargetGhost, CraftID: g.CraftID, GhostOwner: g.Owner},
			Name: g.Handle + ": " + g.Name, DistM: g.Pos.Sub(from).Norm()})
	}
	sort.SliceStable(rows, func(a, b int) bool {
		if rows[a].Group != rows[b].Group {
			return rows[a].Group < rows[b].Group
		}
		return rows[a].DistM < rows[b].DistM
	})
	open = -1
	if len(cycle) > 1 {
		idx := 0
		for i, t := range cycle {
			if t == w.Target {
				idx = i
				break
			}
		}
		next := cycle[(idx+1)%len(cycle)]
		for i, r := range rows {
			if r.Target == next {
				open = i
			}
		}
	}
	return rows, open
}

// RecentActions is the event-objective sink as of now (ADR 0025 section 6),
// read by the tui tests that pin a menu commit records cycle_target.
func (w *World) RecentActions() []missions.Action { return w.recentActions }
