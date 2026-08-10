package input

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// ParseGesture parses the markup gesture syntax — "ctrl+s", "j", "tab",
// "enter", "up", "shift+tab", "esc", "space" — into the KeyEvent that
// must arrive for the gesture to fire. Modifier order does not matter.
//
// A shift modifier on a printable character is folded into the rune
// ("shift+j" → "J"), because that is what the terminal actually sends.
func ParseGesture(s string) (KeyEvent, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return KeyEvent{}, fmt.Errorf("input: empty gesture")
	}
	// The key is whatever follows the last '+' — unless '+' itself is
	// the key, which is the one case that needs spelling out ("ctrl++").
	key, mods := s, []string(nil)
	if i := strings.LastIndex(s, "+"); i > 0 {
		switch {
		case i < len(s)-1:
			key, mods = s[i+1:], strings.Split(s[:i], "+")
		case strings.HasSuffix(s[:i], "+"):
			key, mods = "+", strings.Split(strings.TrimSuffix(s[:i], "+"), "+")
		default:
			return KeyEvent{}, fmt.Errorf("input: gesture %q has no key", s)
		}
	}
	var ev KeyEvent
	for _, m := range mods {
		switch strings.ToLower(m) {
		case "ctrl", "control", "c":
			ev.Mods |= ModCtrl
		case "alt", "meta", "option":
			ev.Mods |= ModAlt
		case "shift":
			ev.Mods |= ModShift
		default:
			return KeyEvent{}, fmt.Errorf("input: unknown modifier %q in gesture %q", m, s)
		}
	}
	if key == "space" {
		key = " "
	}
	for _, n := range keyNames {
		if strings.EqualFold(key, n.name) {
			ev.Key = n.k
			return ev, nil
		}
	}
	r, size := utf8.DecodeRuneInString(key)
	if size != len(key) || r == utf8.RuneError {
		return KeyEvent{}, fmt.Errorf("input: unknown key %q in gesture %q", key, s)
	}
	ev.Key = KeyRune
	ev.Rune = r
	if ev.Mods&ModShift != 0 {
		ev.Rune = []rune(strings.ToUpper(string(r)))[0]
		ev.Mods &^= ModShift
	}
	// ctrl+<letter> arrives from the terminal as a control byte, which
	// the decoder normalizes to the lowercase letter.
	if ev.Mods&ModCtrl != 0 {
		ev.Rune = []rune(strings.ToLower(string(ev.Rune)))[0]
	}
	return ev, nil
}
