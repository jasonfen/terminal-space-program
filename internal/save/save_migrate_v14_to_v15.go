// Schema v14 -> v15 (ADR 0056 decision 7, #611): the first non-identity
// migration since v13.
//
// Ports. A Stage used to carry no port data; Stage.Ports() looked it up from
// the catalog by (LoadoutID, Name). From v15 the Stage stores its ports and
// the save persists them, so an old save is backfilled with exactly what that
// lookup returned (spacecraft.LookupCatalogPorts), for every stage of every
// craft and every docked component. After this a stage with no "ports" key
// has no port, whatever it is called.
//
// Launch site. The launch view measured downrange from (LaunchLatDeg,
// LaunchLonDeg) even for a vessel that never had a pad, where both are the
// zero value (#611). v15 records HasLaunchSite. An old save is backfilled
// with: HasLaunchSite = OnPad || LaunchLatDeg != 0 || LaunchLonDeg != 0.
// A vessel on its pad has a site; a vessel that launched kept the pad's
// coordinates, which are non-zero for any real pad; an orbit spawn has
// 0/0 and no site. The one case this cannot recover is a pad at exactly
// latitude 0 and longitude 0 (the Equator preset, which is Lumen's Kern
// Space Center) whose vessel has since lifted off: in an old save it is
// indistinguishable from an orbit spawn, so it reads as no site and its
// downrange is not shown. A vessel still on that pad keeps its site (OnPad),
// and new launches set the flag when they spawn.

package save

import "github.com/jasonfen/terminal-space-program/internal/spacecraft"

func migrateV14PayloadToV15(p *Payload) {
	crafts := make([]*Craft, 0, len(p.Crafts)+1)
	for i := range p.Crafts {
		crafts = append(crafts, &p.Crafts[i])
	}
	if p.Craft != nil { // the pre-v5 singular form, if still present
		crafts = append(crafts, p.Craft)
	}
	for _, c := range crafts {
		MigrateWireCraftV14ToV15(c)
	}
}

// MigrateWireCraftV14ToV15 applies the v14 -> v15 backfill (ports for every
// stage and docked-component stage, HasLaunchSite) to one wire craft. The
// save envelope runs it for every craft it loads; session.json's parked dock
// payloads are bare wire crafts outside any envelope, so sessiondir's
// MetaVersion 3 -> 4 step runs it once for each of them. Call it only on a
// craft written before v15: a v15 stage with no ports is a stage with no port.
func MigrateWireCraftV14ToV15(c *Craft) {
	if c == nil {
		return
	}
	backfillWirePorts(c.Stages)
	for j := range c.DockedComponents {
		backfillWirePorts(c.DockedComponents[j].Stages)
	}
	if c.OnPad || c.LaunchLatDeg != 0 || c.LaunchLonDeg != 0 {
		c.HasLaunchSite = true
	}
}

func backfillWirePorts(stages []Stage) {
	for i := range stages {
		stages[i].Ports = portsToWire(spacecraft.LookupCatalogPorts(stages[i].LoadoutID, stages[i].Name))
	}
}
