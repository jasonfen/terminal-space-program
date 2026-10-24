package screens

import (
	"strconv"
	"strings"
	"testing"

	"github.com/jasonfen/terminal-space-program/internal/spacecraft"
)

// S3 / ADR 0032 §8 — the session Σ Δv target and the tank-row count hint.

// TestVABTargetInputSetClear — [t] opens the numeric input; digits + enter set
// the target; an empty entry clears it.
func TestVABTargetInputSetClear(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.HandleKey("t")
	if v.mode != vabModeTarget {
		t.Fatalf("[t] did not open target mode: %v", v.mode)
	}
	for _, k := range []string{"9", "2", "0", "0"} {
		v.HandleKey(k)
	}
	v.HandleKey("enter")
	if v.target != 9200 || v.mode != vabModeBuild {
		t.Fatalf("after typing 9200+enter: target=%g mode=%v, want 9200/build", v.target, v.mode)
	}
	// Re-open, clear the field, enter → target cleared.
	v.HandleKey("t")
	for i := 0; i < 4; i++ {
		v.HandleKey("backspace")
	}
	v.HandleKey("enter")
	if v.target != 0 {
		t.Errorf("empty entry did not clear target: %g", v.target)
	}
	// The input filter rejects non-numeric keystrokes.
	v.HandleKey("t")
	v.HandleKey("a")
	v.HandleKey("z")
	if v.targetInput != "" {
		t.Errorf("input buffer accepted letters: %q", v.targetInput)
	}
}

// TestVABSetTargetValidation — setTarget clears on empty, rejects non-numeric
// and negative, accepts a number.
func TestVABSetTargetValidation(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	if !v.setTarget("") || v.target != 0 {
		t.Error("empty should clear and be accepted")
	}
	if !v.setTarget("9200") || v.target != 9200 {
		t.Errorf("numeric rejected: target=%g", v.target)
	}
	if v.setTarget("-5") || v.target != 9200 {
		t.Error("negative should be rejected and leave target unchanged")
	}
	if v.setTarget("x.y") || v.target != 9200 {
		t.Error("non-numeric should be rejected and leave target unchanged")
	}
	// A typed 0 clears (accepted), rather than reading as a silent "no target".
	if !v.setTarget("0") || v.target != 0 {
		t.Errorf("setTarget(\"0\") should clear: accepted=%v target=%g", v.setTarget("0"), v.target)
	}
}

// TestVABTargetReadout — with a target set the stats strip shows the
// current / target (delta) form; without one it stays the plain line.
func TestVABTargetReadout(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{components: []string{"eng", "tank"}}}
	stats := v.Stats()

	plain := v.targetReadout(stats)
	if strings.Contains(plain, " / ") {
		t.Errorf("no-target readout should be plain: %q", plain)
	}
	v.target = stats.TotalDV + 800
	readout := v.targetReadout(stats)
	if !strings.Contains(readout, "/ "+strconv.FormatFloat(v.target, 'f', 0, 64)) {
		t.Errorf("readout missing the target: %q", readout)
	}
	if !strings.Contains(readout, "(-800)") && !strings.Contains(readout, "(−800)") {
		t.Errorf("readout missing the (-800) delta: %q", readout)
	}
}

// TestVABTankHintAdds — with a target and a tank row selected, the hint names a
// count that actually closes the Σ gap, and it is minimal (N-1 falls short).
func TestVABTankHintAdds(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{components: []string{"eng", "tank"}}}
	v.target = v.Stats().TotalDV + 800
	v.focus = focusStack
	v.stackCursor = v.headerRowIndex(0) + 2 // the tank group (engine then tank)

	hint := v.tankHint()
	if !strings.Contains(hint, "Big Tank") || !strings.Contains(hint, "✓") {
		t.Fatalf("hint = %q, want a Big Tank +N ✓ suggestion", hint)
	}
	n := parsePlusN(t, hint)
	if got := v.sigmaWithTank(0, "tank", n); got < v.target {
		t.Errorf("hint says +%d reaches target but Σ=%.0f < %.0f", n, got, v.target)
	}
	if n > 1 {
		if got := v.sigmaWithTank(0, "tank", n-1); got >= v.target {
			t.Errorf("hint +%d not minimal: +%d already reaches Σ=%.0f", n, n-1, got)
		}
	}
}

// TestVABTankHintUnreachable — an absurd target the tank's dry-mass asymptote
// can't reach yields the unreachable hint (ADR 0032 §8).
func TestVABTankHintUnreachable(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{components: []string{"eng", "tank"}}}
	v.target = 1e12
	v.focus = focusStack
	v.stackCursor = v.headerRowIndex(0) + 2 // tank row
	if hint := v.tankHint(); !strings.Contains(hint, "unreachable") {
		t.Errorf("hint = %q, want unreachable", hint)
	}
}

// TestVABTankHintGating — no hint when there is no target, when the selected
// row is not a tank, or when the target is already met.
func TestVABTankHintGating(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.stages = []vabStage{{components: []string{"eng", "tank"}}}
	v.focus = focusStack

	v.stackCursor = v.headerRowIndex(0) + 2 // tank row
	if h := v.tankHint(); h != "" {
		t.Errorf("no target should give no hint, got %q", h)
	}
	v.target = v.Stats().TotalDV + 800

	v.stackCursor = v.headerRowIndex(0) + 1 // engine row
	if h := v.tankHint(); h != "" {
		t.Errorf("engine row should give no tank hint, got %q", h)
	}
	v.stackCursor = v.headerRowIndex(0) // header
	if h := v.tankHint(); h != "" {
		t.Errorf("header should give no hint, got %q", h)
	}
	v.stackCursor = v.headerRowIndex(0) + 2 // tank row, but target already met
	v.target = 1
	if h := v.tankHint(); h != "" {
		t.Errorf("met target should give no hint, got %q", h)
	}
}

// TestVABTargetClearedOnReset — the target is session-only; Reset drops it.
func TestVABTargetClearedOnReset(t *testing.T) {
	v := NewVAB(Theme{})
	v.Reset(testVABComps())
	v.target = 9200
	v.Reset(testVABComps())
	if v.target != 0 {
		t.Errorf("Reset left target = %g, want 0", v.target)
	}
}

// parsePlusN extracts the integer after the first '+' in a hint string.
func parsePlusN(t *testing.T, hint string) int {
	t.Helper()
	i := strings.IndexByte(hint, '+')
	if i < 0 {
		t.Fatalf("no +N in hint %q", hint)
	}
	j := i + 1
	for j < len(hint) && hint[j] >= '0' && hint[j] <= '9' {
		j++
	}
	n, err := strconv.Atoi(hint[i+1 : j])
	if err != nil {
		t.Fatalf("bad +N in hint %q: %v", hint, err)
	}
	return n
}

// sigmaWithTank returns the whole-stack Σ Δv if `extra` more of compID were in
// stage i (ADR 0032 §8 — computed against Σ, since adding a tank to one stage
// lowers every stage below it, so a per-stage number would lie).
func (v *VAB) sigmaWithTank(i int, compID string, extra int) float64 {
	stages := make([]spacecraft.Stage, len(v.stages))
	for k, vs := range v.stages {
		if k == i && !vs.isCatalog() {
			comps := append([]string(nil), vs.components...)
			for e := 0; e < extra; e++ {
				comps = append(comps, compID)
			}
			st, _ := spacecraft.ComposeStage(comps, v.comps)
			stages[k] = st
		} else {
			stages[k] = v.resolveStage(vs)
		}
	}
	return v.statsOf(stages).TotalDV
}
