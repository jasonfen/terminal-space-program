package tui

import (
	"reflect"
	"strconv"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Modified-key decoding (Jason's playtest 2026-10-04: `|`, `Z`, `X` did
// nothing). A terminal that another program switched into xterm
// modifyOtherKeys mode sends shift+\ as ESC[27;2;124~; a kitty-keyboard
// terminal sends ESC[124;2u or ESC[92;2u. Bubble Tea v1 does not parse
// either form and hands the program an unexported unknownCSISequenceMsg,
// which every key handler ignores, so each shifted key was silently
// swallowed. decodeModifiedKey turns those sequences back into the
// KeyMsg the player pressed, before any handler sees the message.

// decodeModifiedKey reports the KeyMsg an unknown CSI key sequence stands
// for. ok is false for any other message, or a sequence it does not know.
func decodeModifiedKey(msg tea.Msg) (tea.KeyMsg, bool) {
	v := reflect.ValueOf(msg)
	if !v.IsValid() || v.Kind() != reflect.Slice || v.Type().Elem().Kind() != reflect.Uint8 ||
		v.Type().Name() != "unknownCSISequenceMsg" {
		return tea.KeyMsg{}, false
	}
	seq := string(v.Bytes())
	if !strings.HasPrefix(seq, "\x1b[") || len(seq) < 4 {
		return tea.KeyMsg{}, false
	}
	final := seq[len(seq)-1]
	params := strings.Split(seq[2:len(seq)-1], ";")
	var code, mod int
	switch {
	case final == '~' && len(params) == 3 && params[0] == "27":
		// xterm modifyOtherKeys: CSI 27 ; modifier ; code ~
		mod, code = atoi(params[1]), atoi(params[2])
	case final == 'u' && len(params) >= 1 && len(params) <= 2:
		// kitty / fixterms: CSI code[:shifted[:base]] [; modifier[:event]] u
		keys := strings.Split(params[0], ":")
		code = atoi(keys[0])
		if len(keys) > 1 && keys[1] != "" {
			code = atoi(keys[1]) // the terminal already applied shift
		}
		mod = 1
		if len(params) == 2 {
			m := strings.Split(params[1], ":")
			mod = atoi(m[0])
			if len(m) > 1 && m[1] == "3" {
				return tea.KeyMsg{}, false // key release: no action
			}
		}
	default:
		return tea.KeyMsg{}, false
	}
	if code <= 0 || mod < 1 {
		return tea.KeyMsg{}, false
	}
	bits := mod - 1
	shift, alt, ctrl := bits&1 != 0, bits&2 != 0, bits&4 != 0

	switch code {
	case 9:
		if shift {
			return tea.KeyMsg{Type: tea.KeyShiftTab, Alt: alt}, true
		}
		return tea.KeyMsg{Type: tea.KeyTab, Alt: alt}, true
	case 13:
		return tea.KeyMsg{Type: tea.KeyEnter, Alt: alt}, true
	case 27:
		return tea.KeyMsg{Type: tea.KeyEsc, Alt: alt}, true
	case 127:
		return tea.KeyMsg{Type: tea.KeyBackspace, Alt: alt}, true
	case 32:
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}, true
	}
	r := rune(code)
	if !unicode.IsPrint(r) {
		return tea.KeyMsg{}, false
	}
	if ctrl && r >= 'a' && r <= 'z' {
		return tea.KeyMsg{Type: tea.KeyCtrlA + tea.KeyType(r-'a'), Alt: alt}, true
	}
	if shift {
		r = shifted(r)
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}, Alt: alt}, true
}

// shifted is the character shift gives a base key on a US layout, for the
// kitty form that reports the unshifted key (ESC[92;2u for shift+\). A key
// that is already shifted (modifyOtherKeys sends 124 for |) maps to itself.
func shifted(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return unicode.ToUpper(r)
	}
	const base, up = "`1234567890-=[]\\;',./", "~!@#$%^&*()_+{}|:\"<>?"
	if i := strings.IndexRune(base, r); i >= 0 {
		return []rune(up)[i]
	}
	return r
}

func atoi(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}
