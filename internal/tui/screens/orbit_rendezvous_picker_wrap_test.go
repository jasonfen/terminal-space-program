package screens

import (
	"time"

	"github.com/jasonfen/terminal-space-program/internal/planner"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// Wave B review fix (REVIEW LOW 148): the bay wrapped a styled (dimmed
// refusal / highlighted) row at the cell, not the word, because
// cellIsBlank only stripped a trailing reset and so never saw a styled
// space as blank. Colour forced on: under go test lipgloss emits no SGR and
// the bug is invisible.
func TestWrapBayLine_StyledLineBreaksOnSpaces(t *testing.T) {
	ambient := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(ambient) })

	plain := "  3 laps  burn T-4m57s  wait 1h52m  (burn drops periapsis unsafely)"
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#5F5F5F")).Render(plain)
	if !strings.Contains(styled, "\x1b[") {
		t.Fatal("test setup broken: expected SGR in the styled line")
	}
	got := wrapBayLine(styled, 54)
	if len(got) < 2 {
		t.Fatalf("line did not wrap at 54: %q", got)
	}
	var words []string
	for _, l := range got {
		words = append(words, strings.Fields(ansi.Strip(l))...)
	}
	if want := strings.Fields(plain); !reflect.DeepEqual(words, want) {
		t.Errorf("wrap split a word:\n got  %q\n want %q", words, want)
	}
}

func refusalLadder() planner.RendezvousLadder {
	return planner.RendezvousLadder{
		Place: planner.RendezvousYourOrbit, MoverIsA: false,
		Rows: []planner.RendezvousBurnOption{
			{Laps: 2, Reason: "no rendezvous solution", TBurn: 297},
			{Laps: 3, Reason: "burn drops periapsis unsafely", TBurn: 297, TArrival: 6720},
			{Laps: 10, Reason: "burn drops periapsis unsafely", TBurn: 297, TArrival: 53580},
			{Laps: 20, Reason: "unaffordable", TBurn: 297, TArrival: 110000},
		},
	}
}

// LOW 145: a refusal row with no solved arrival reads a dash, not "wait 0s".
func TestRendezvousPickerChip_RefusalRowWithoutArrival_ReadsDash(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousYourOrbit, refusalLadder(), nil)
	lines := v.buildRendezvousPickerChip()
	row := ansi.Strip(lines[2])
	if strings.Contains(row, "wait 0s") {
		t.Errorf("refusal row reads a zero wait: %q", row)
	}
	if !strings.Contains(row, "wait —") {
		t.Errorf("refusal row should read a dash wait: %q", row)
	}
}

// LOW 148: every refusal row fits the bay's 56-cell chip without wrapping at
// all (the reason text is short), so nothing breaks mid-word.
func TestRendezvousPickerChip_RefusalRowsFitTheBay(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousYourOrbit, refusalLadder(), nil)
	for _, l := range v.buildRendezvousPickerChip() {
		if w := lipgloss.Width(l); w > 54 {
			t.Errorf("picker line %d cells wide, want <= 54: %q", w, l)
		}
	}
}

// A structural refusal (no rows) is word-wrapped to the chip, never one long
// line the bay has to cut.
func TestRendezvousPickerChip_StructuralRefusalWrapsOnWords(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	v.OpenRendezvousPicker(planner.RendezvousCrossing, planner.RendezvousLadder{},
		errString(`these orbits have no single crossing point, try "their orbit" or "your orbit"`))
	var words []string
	for _, l := range v.buildRendezvousPickerChip()[2:] {
		if w := lipgloss.Width(l); w > 54 {
			t.Errorf("refusal line %d cells wide: %q", w, l)
		}
		words = append(words, strings.Fields(ansi.Strip(l))...)
	}
	if got := strings.Join(words, " "); got != `these orbits have no single crossing point, try "their orbit" or "your orbit"` {
		t.Errorf("wrapped text lost or split words: %q", got)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// LOW 146: the burn countdown ticks by the second while the wait column is
// deliberately minute-resolution (a day-long wait in seconds is noise). Pins
// the observed behaviour: 20 s later the burn reading changed, the wait did not.
func TestRendezvousPickerChip_BurnTicksWaitStaysCoarse(t *testing.T) {
	v := NewOrbitView(chipTestTheme())
	l := planner.RendezvousLadder{
		Place: planner.RendezvousTheirOrbit, MoverIsA: true,
		SolvedAt: time.Date(2000, 1, 5, 0, 0, 0, 0, time.UTC),
		Rows:     []planner.RendezvousBurnOption{{Laps: 3, Ok: true, DV: 500, TBurn: 297, TArrival: 12230}},
	}
	v.OpenRendezvousPicker(planner.RendezvousTheirOrbit, l, nil)
	before := ansi.Strip(v.buildRendezvousPickerChip()[2])
	v.SetRendezvousPickerNow(l.SolvedAt.Add(20 * time.Second))
	after := ansi.Strip(v.buildRendezvousPickerChip()[2])
	if before == after {
		t.Fatal("nothing ticked in 20 s")
	}
	cut := func(s, key string) string { i := strings.Index(s, key); return strings.Fields(s[i:])[1] }
	if cut(before, "burn") == cut(after, "burn") {
		t.Errorf("burn did not tick: %q vs %q", before, after)
	}
	if cut(before, "wait") != cut(after, "wait") {
		t.Errorf("wait is minute-resolution and should not change in 20 s here: %q vs %q", before, after)
	}
}
