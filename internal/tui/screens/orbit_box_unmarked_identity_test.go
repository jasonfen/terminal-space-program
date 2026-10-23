package screens

import (
	"reflect"
	"testing"
	"time"

	"github.com/jasonfen/terminal-space-program/internal/sim"
	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// ADR 0058 guard [9] (unmarked vessels): the ENGINE, PROPELLANT and STAGES
// boxes of vessels with no booster group render byte-identical to main at
// 23e28e0 (before the S5 booster readouts). Goldens were dumped from that
// code, not from this slice's. Throttle 40% so the (max N) suffix and a
// non-default throttle value are covered; burning and idle both.
func TestUnmarkedVesselBoxesByteIdentical(t *testing.T) {
	type golden struct{ engine, propellant, stages []string }
	cases := []struct {
		id      string
		burning bool
		want    golden
	}{
		{"Saturn-V", false, golden{
			[]string{"ENGINE", "  throttle: 40% idle              mode:  main", "  TWR:      0.49 (max 1.23)", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     100%                mass:     2902 t", "  Δv:       3518 / 18872 m/s    Δv→circ:  —", "  monoprop: 11.85 t             rcs:      8.83 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◉●●  ▸ S-IC (1/3) [space]"}}},
		{"Saturn-V", true, golden{
			[]string{"ENGINE  ● BURN", "  throttle: 40% ● FIRING T+42s    mode:  main", "  TWR:      0.49 (max 1.23)", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     100%                mass:     2902 t", "  Δv:       3518 / 18872 m/s    Δv→circ:  —", "  monoprop: 11.85 t             rcs:      8.83 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◉●●  ▸ S-IC (1/3) [space]"}}},
		{"Falcon-9", false, golden{
			[]string{"ENGINE", "  throttle: 40% idle              mode:  main", "  TWR:      0.56 (max 1.41)", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     100%                mass:     549.9 t", "  Δv:       3805 / 15274 m/s    Δv→circ:  —", "  monoprop: 1.931 t             rcs:      7.59 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◉●  ▸ F9-S1 (1/2) [space]"}}},
		{"Falcon-9", true, golden{
			[]string{"ENGINE  ● BURN", "  throttle: 40% ● FIRING T+42s    mode:  main", "  TWR:      0.56 (max 1.41)", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     100%                mass:     549.9 t", "  Δv:       3805 / 15274 m/s    Δv→circ:  —", "  monoprop: 1.931 t             rcs:      7.59 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◉●  ▸ F9-S1 (1/2) [space]"}}},
		{"Relay-Tug", false, golden{
			[]string{"ENGINE", "  throttle: 40% idle              mode:  main", "  TWR:      0.19 (max 0.48)", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     100%                mass:     14.20 t", "  Δv:       13159 m/s           Δv→circ:  —", "  monoprop: 196 kg              rcs:      30.05 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◉  ▸ NTR Tug (1/1)"}}},
		{"Relay-Tug", true, golden{
			[]string{"ENGINE  ● BURN", "  throttle: 40% ● FIRING T+42s    mode:  main", "  TWR:      0.19 (max 0.48)", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     100%                mass:     14.20 t", "  Δv:       13159 m/s           Δv→circ:  —", "  monoprop: 196 kg              rcs:      30.05 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◉  ▸ NTR Tug (1/1)"}}},
		{"Capsule", false, golden{
			[]string{"ENGINE", "  throttle: 40% idle              mode:  main", "  TWR:      —", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     0 kg                mass:     6.180 t", "  Δv:       0.00 m/s            Δv→circ:  —", "  monoprop: 380 kg              rcs:      137 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◇  ▸ Capsule (1/1)"}}},
		{"Capsule", true, golden{
			[]string{"ENGINE  ● BURN", "  throttle: 40% ● FIRING T+42s    mode:  main", "  TWR:      —", "  node:     —"},
			[]string{"PROPELLANT", "  fuel:     0 kg                mass:     6.180 t", "  Δv:       0.00 m/s            Δv→circ:  —", "  monoprop: 380 kg              rcs:      137 m/s", "  cargo:    —                   hold:     —"},
			[]string{"STAGES  ◇  ▸ Capsule (1/1)"}}},
	}
	for _, tc := range cases {
		v := NewOrbitView(launchThemeForTest())
		w, err := sim.NewWorld()
		if err != nil {
			t.Fatal(err)
		}
		c := spacecraft.NewFromLoadout(tc.id)
		c.Throttle = 0.4
		c.Primary = w.ActiveCraft().Primary
		w.Crafts[w.ActiveCraftIdx] = c
		if tc.burning {
			c.ManualBurn = &spacecraft.ManualBurn{StartTime: w.Clock.SimTime.Add(-42 * time.Second)}
		}
		for name, p := range map[string][2][]string{
			"ENGINE":     {v.buildEngineBox(w), tc.want.engine},
			"PROPELLANT": {v.buildPropellantBox(w), tc.want.propellant},
			"STAGES":     {v.buildStagesBox(w), tc.want.stages},
		} {
			if !reflect.DeepEqual(p[0], p[1]) {
				t.Errorf("%s burning=%v %s drifted:\n got %q\nwant %q", tc.id, tc.burning, name, p[0], p[1])
			}
		}
	}
}
