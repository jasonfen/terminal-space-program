package serve

// Server version surface (v0.30 S5, #226): a courtesy readout of the
// running server version against the newest published release, wired to
// the S4 drain-restart so an admin can adopt an update from inside the
// game. The game NEVER fetches, verifies, or swaps a binary — "adopt"
// is drain-restart with the exit-42 marker; the supervising service
// manager on the deploy host pulls the release. The version check only
// queries the public release feed, and degrades silently on any failure.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/version"
)

const (
	// releaseFeedURL is the repo's public tag list on the GitHub API. Tag
	// names carry a leading "v"; four-part checkpoint tags are filtered
	// out by the three-part SemVer test, not by the endpoint.
	releaseFeedURL = "https://api.github.com/repos/jasonfen/terminal-space-program/tags"

	// adoptCapabilityEnv is the supervisor-set marker signalling the host
	// has adopt tooling (tsp-adopt on ExecStopPost). Only when it is set
	// does the Session screen offer the [u] restart-to-adopt affordance;
	// otherwise the version readout still appears but points at the manual
	// update path, so the UI never offers an update it can't perform.
	// The exact token is a game↔host contract detail (proposed with ansi).
	adoptCapabilityEnv = "TSP_ADOPT"

	// versionCheckInterval is deliberately long — this is a courtesy
	// readout, not a polling service.
	versionCheckInterval = 6 * time.Hour

	versionFetchTimeout = 10 * time.Second
)

// versionSurface holds the running-vs-available readout the Session
// screen shows. available is the newest published three-part SemVer
// release strictly greater than running, or "" when the check hasn't
// succeeded or nothing newer exists. adopt records whether the
// supervisor signalled adopt-capability. fetch is indirected so tests
// exercise the comparison without touching the network.
type versionSurface struct {
	mu        sync.Mutex
	running   string
	available string
	adopt     bool
	fetch     func() ([]string, error)
}

func newVersionSurface() *versionSurface {
	return &versionSurface{
		running: version.Version,
		adopt:   os.Getenv(adoptCapabilityEnv) != "",
		fetch:   fetchReleaseTags,
	}
}

// snapshot returns the current readout under the lock.
func (v *versionSurface) snapshot() (running, available string, adopt bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.running, v.available, v.adopt
}

// refresh runs one feed check and updates available. A failed or
// malformed fetch leaves available untouched — the readout degrades
// silently to the running version alone, never wedging the session.
func (v *versionSurface) refresh() {
	tags, err := v.fetch()
	if err != nil {
		return
	}
	latest := latestRelease(v.running, tags)
	v.mu.Lock()
	v.available = latest
	v.mu.Unlock()
}

// watch polls the feed on a conservative interval until stop closes. It
// runs off the tick loop, in its own goroutine. A build whose running
// version isn't a real three-part SemVer (a dev/test build) never polls
// — there is nothing to compare against, and it keeps CI off the wire.
func (v *versionSurface) watch(stop <-chan struct{}) {
	if _, _, _, ok := parseSemVer(v.running); !ok {
		return
	}
	v.refresh()
	t := time.NewTicker(versionCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			v.refresh()
		}
	}
}

// fetchReleaseTags reads the repo's tag names off the public GitHub API.
func fetchReleaseTags() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionFetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseFeedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("serve: release feed status %d", resp.StatusCode)
	}
	var payload []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	tags := make([]string, 0, len(payload))
	for _, t := range payload {
		tags = append(tags, t.Name)
	}
	return tags, nil
}

// latestRelease returns the newest tag that is a valid three-part SemVer
// release strictly greater than running, normalised to leading-v display
// form ("v0.30.0"), or "" when none qualifies. Four-part checkpoint tags
// and malformed entries are ignored; comparison is numeric per component
// (so v0.29.10 > v0.29.9).
func latestRelease(running string, tags []string) string {
	best := ""
	for _, tag := range tags {
		if _, _, _, ok := parseSemVer(tag); !ok {
			continue // not a three-part release (checkpoint tag / junk)
		}
		if !semVerLess(running, tag) {
			continue // not newer than what we run
		}
		if best == "" || semVerLess(best, tag) {
			best = tag
		}
	}
	if best == "" {
		return ""
	}
	return "v" + strings.TrimPrefix(best, "v")
}

// parseSemVer parses "vX.Y.Z" (leading v optional) into three
// non-negative ints. Four-part (checkpoint) tags and anything malformed
// return ok=false, so they are never treated as releases.
func parseSemVer(tag string) (maj, min, pat int, ok bool) {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(tag), "v"), ".")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var v [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return 0, 0, 0, false
		}
		v[i] = n
	}
	return v[0], v[1], v[2], true
}

// semVerLess reports whether a < b as three-part SemVer. An unparseable
// operand makes the comparison false, so a dev running version never
// counts as older than any release (no nag) and junk tags never win.
func semVerLess(a, b string) bool {
	am, an, ap, aok := parseSemVer(a)
	bm, bn, bp, bok := parseSemVer(b)
	if !aok || !bok {
		return false
	}
	if am != bm {
		return am < bm
	}
	if an != bn {
		return an < bn
	}
	return ap < bp
}
