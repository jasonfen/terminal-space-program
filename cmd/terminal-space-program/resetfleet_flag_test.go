package main

import (
	"strings"
	"testing"
)

// (f) --reset-fleet without --serve is refused with a clear error, and
// so is combining it with a scenario start; --serve --reset-fleet on a
// default start passes.
func TestValidateResetFleet(t *testing.T) {
	cases := []struct {
		name                           string
		resetFleet, serve, hasScenario bool
		wantErr                        string
	}{
		{"flag off is always fine", false, false, false, ""},
		{"flag off with serve is fine", false, true, false, ""},
		{"reset without serve refused", true, false, false, "--serve"},
		{"reset with serve ok", true, true, false, ""},
		{"reset with scenario refused", true, true, true, "scenario"},
		{"reset without serve names the flag", true, false, false, "--reset-fleet"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateResetFleet(c.resetFleet, c.serve, c.hasScenario)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error mentioning %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %q should mention %q", err, c.wantErr)
			}
		})
	}
}
