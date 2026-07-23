package serve

import (
	"errors"
	"testing"
)

// latestRelease picks the newest three-part SemVer strictly greater than
// the running version, ignoring four-part checkpoint tags and junk, with
// numeric (not lexical) component comparison (v0.30 S5, #226).
func TestLatestRelease(t *testing.T) {
	cases := []struct {
		name    string
		running string
		tags    []string
		want    string
	}{
		{"newer release", "0.29.1", []string{"v0.29.1", "v0.30.0", "v0.28.4"}, "v0.30.0"},
		{"numeric compare beats lexical", "0.29.9", []string{"v0.29.10"}, "v0.29.10"},
		{"four-part checkpoint ignored", "0.29.0", []string{"v0.29.1.3"}, ""},
		{"checkpoint alongside release", "0.29.1", []string{"v0.29.1.3", "v0.30.0"}, "v0.30.0"},
		{"nothing newer", "0.30.0", []string{"v0.29.1", "v0.30.0"}, ""},
		{"malformed tags skipped", "0.29.1", []string{"garbage", "v", "v0.30.0"}, "v0.30.0"},
		{"dev running never nags", "dev", []string{"v0.30.0"}, ""},
		{"picks the max of several", "0.28.0", []string{"v0.29.0", "v0.30.2", "v0.30.1"}, "v0.30.2"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := latestRelease(c.running, c.tags); got != c.want {
				t.Errorf("latestRelease(%q, %v) = %q, want %q", c.running, c.tags, got, c.want)
			}
		})
	}
}

// refresh stores the newest release; a failed fetch degrades silently
// (available left untouched). adopt-capability comes from the env.
func TestVersionSurfaceRefresh(t *testing.T) {
	vs := &versionSurface{running: "0.29.1"}

	// Successful fetch → available set.
	vs.fetch = func() ([]string, error) { return []string{"v0.30.0", "v0.29.1.9"}, nil }
	vs.refresh()
	if _, avail, _ := vs.snapshot(); avail != "v0.30.0" {
		t.Errorf("available = %q after fetch, want v0.30.0", avail)
	}

	// A later failed fetch must not clobber the last good reading.
	vs.fetch = func() ([]string, error) { return nil, errors.New("network down") }
	vs.refresh()
	if _, avail, _ := vs.snapshot(); avail != "v0.30.0" {
		t.Errorf("failed fetch clobbered available to %q", avail)
	}

	// adopt-capability rides the env marker.
	t.Setenv(adoptCapabilityEnv, "1")
	if vs := newVersionSurface(); !vs.adopt {
		t.Error("adopt not set from env marker")
	}
	t.Setenv(adoptCapabilityEnv, "")
	if vs := newVersionSurface(); vs.adopt {
		t.Error("adopt set with the env marker cleared")
	}
}
