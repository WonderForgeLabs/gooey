package input

import (
	"fmt"
	"strings"
	"sync"
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
		// ctrl+@ AND ctrl+space ARE THE SAME BYTE, 0x00, and the decoder
		// answers space because space is what people press
		// (decode.go's c == 0 arm). The parser used to leave the '@',
		// which meant the two ends agreed about the byte and disagreed
		// about the event — so a ctrl+@ binding was dead. Normalised
		// rather than rejected: it is the one unproducible spelling
		// whose intent is unambiguous.
		if ev.Rune == '@' {
			ev.Rune = ' '
		}
		if err := errUnproducible(ev, s); err != nil {
			return KeyEvent{}, err
		}
	}
	return ev, nil
}

// producible is every KeyEvent a single byte can decode to, keyed for
// lookup and DERIVED FROM Decode rather than written down.
//
// A table here would be stale the first time an arm of decode.go moves,
// which is exactly the failure this exists to catch: a gesture the
// parser accepts and no decoder can emit loads cleanly, dispatches
// forever, and never fires — no error, no warning, and nothing at
// runtime that distinguishes it from a key you never pressed. Building
// the set by asking the decoder means the two cannot drift.
//
// SINGLE BYTES ONLY, which is the whole domain of the question. A
// printable carrying ModCtrl can only come from decode.go's c < 0x20
// arm; the multi-byte sequences produce named keys, and alt is an ESC
// prefix that this deliberately looks past (see errUnproducible).
var producible = sync.OnceValue(func() map[KeyEvent]struct{} {
	out := map[KeyEvent]struct{}{}
	for c := 0; c < 0x100; c++ {
		ev, _, ok := Decode([]byte{byte(c)}, true)
		if !ok || ev.Kind != EventKey {
			continue
		}
		// MEMBERSHIP is the whole contract — several bytes can decode to
		// one event and the set does not care which. Keeping the byte as
		// the value would have needed a first-wins guard to break that
		// tie, over a value nothing reads: errUnproducible recomputes the
		// byte from ctrlByte where it needs one for the message.
		out[ev.Key] = struct{}{}
	}
	return out
})

// ctrlByte inverts decode.go's `r := rune(c | 0x40)`, answering which
// control byte a terminal would send for ctrl+r — and false when there
// is none, which is most of the printable range.
//
// It is the one hand-written direction here, so
// TestCtrlByteInvertsTheDecoder checks it against Decode for every byte
// below 0x20 rather than trusting it.
func ctrlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r-'a') + 1, true
	case r >= '@' && r <= '_':
		return byte(r - '@'), true
	}
	return 0, false
}

// errUnproducible rejects a ctrl gesture no decoder can emit, naming the
// reason rather than the verdict: "ctrl+h is backspace" is actionable
// where "unproducible" sends the reader back to the source.
//
// ALT IS MASKED OFF because it is an ESC PREFIX, not part of the byte:
// alt+ctrl+p arrives as 0x1b 0x10, and the question this asks is only
// about the 0x10. The dock's real bindings are alt-prefixed ctrl
// gestures, so that mask is on the path of shipped keys.
//
// The KeyRune guard below is unreachable from ParseGesture today — the
// named-key loop returns before the ctrl block that calls this — so it
// is a guard for a future caller rather than a live branch. Named ctrl
// gestures do NOT flow through here and get waved past: ctrl+enter and
// friends are also mostly unreportable, and that reverse direction is
// still open (see #427).
func errUnproducible(ev KeyEvent, s string) error {
	if ev.Key != KeyRune {
		return nil
	}
	probe := ev
	probe.Mods &^= ModAlt
	if _, ok := producible()[probe]; ok {
		return nil
	}
	if b, ok := ctrlByte(probe.Rune); ok {
		// The byte exists; something else claimed it. Those are the
		// right calls — people pressing backspace mean backspace — but
		// the parser did not know about them.
		if got, _, ok := Decode([]byte{b}, true); ok && got.Kind == EventKey {
			return fmt.Errorf("input: gesture %q never fires: a terminal sends %#02x for it, "+
				"which decodes as %s", s, b, got.Key)
		}
	}
	// "gooey's decoder never produces it", NOT "no terminal can report
	// it", which was false for two of the entries this branch answers:
	// xterm really does send 0x7f for ctrl+? and 0x1f for ctrl+/. It is
	// this decoder that reads those as backspace and ctrl+_, so the
	// verdict holds and the earlier CAUSE did not — in a file whose
	// whole thesis is that naming the cause is the deliverable. Found in
	// review of #427's PR.
	return fmt.Errorf("input: gesture %q never fires: gooey's decoder never produces "+
		"it. ctrl reaches only @ through _ and a through z, because a control byte "+
		"is decoded as byte|0x40", s)
}
