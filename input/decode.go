package input

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Decode decodes the first event in b — a key or an SGR mouse report —
// and reports how many bytes it
// consumed. ok == false with n == 0 means b holds the start of a
// sequence that is not complete yet: read more bytes and call again.
// ok == false with n > 0 means those bytes were a complete sequence this
// package does not map — skip them and carry on.
//
// idle resolves the classic ambiguity — a lone ESC and the first byte of
// a CSI sequence are the same byte. Callers pass idle = true only when
// no further bytes arrived within the escape timeout, at which point a
// dangling ESC really was the Esc key.
//
// LIVENESS, and it is a contract rather than an observation: when idle
// is true, Decode always consumes at least one byte or produces an
// event. Never (0, false).
//
// That case is the drain loop's ONLY "wait for more bytes" signal (see
// term.DecodeEvents), and under idle no further byte is coming — so a
// sequence that still reports incomplete can never be resolved. The
// buffer strands, every later keystroke is appended behind it, and the
// app paints on forever without taking another key. No error, no exit,
// no tripwire: App.Run watches for a decoder that DIED, and this one is
// alive.
//
// It costs nothing to preserve and is silent to break, so the guarantee
// is asserted exhaustively rather than trusted — decodeidle_test.go
// checks every 1- and 2-byte input. If you are changing a `return` in
// decodeCSI, decodeEsc or decodeX10Mouse, that test is the one to run.
func Decode(b []byte, idle bool) (Event, int, bool) {
	if len(b) == 0 {
		return Event{}, 0, false
	}
	switch c := b[0]; {
	case c == 0x1b:
		return decodeEsc(b, idle)
	case c == '\r' || c == '\n':
		return KeyOf(Named(KeyEnter)), 1, true
	case c == '\t':
		return KeyOf(Named(KeyTab)), 1, true
	case c == 0x7f || c == 0x08:
		return KeyOf(Named(KeyBackspace)), 1, true
	case c < 0x20:
		// A control byte is its key with bit 6 cleared — `key & 0x1f` —
		// so the inverse is `c | 0x40`, NOT `'a' + c - 1`.
		//
		// The two agree across 0x01–0x1a, which is ctrl+a through ctrl+z
		// and covers everything anyone had bound. They diverge on the
		// five bytes above that range, and the old formula ran off the
		// end of the alphabet into punctuation: ctrl+] (0x1d) arrived as
		// ctrl+} , ctrl+\ as ctrl+| , ctrl+_ as ctrl+DEL, and ctrl+space
		// as ctrl+backtick. A binding on any of them silently never
		// fired — the key was decoded, dispatched, and matched nothing.
		//
		// Tab, enter, escape and backspace are handled above as the named
		// keys people mean, so they never reach here.
		r := rune(c | 0x40)
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A' // ctrl+a, not ctrl+A: the shift is not real
		}
		if c == 0 {
			r = ' ' // ctrl+space and ctrl+@ are the same byte; space is what people press
		}
		return KeyOf(KeyEvent{Key: KeyRune, Rune: r, Mods: ModCtrl}), 1, true
	case c < 0x80:
		return KeyOf(Rune(rune(c))), 1, true
	default:
		r, size := utf8.DecodeRune(b)
		if r == utf8.RuneError && size <= 1 {
			if !idle && len(b) < utf8.UTFMax {
				return Event{}, 0, false // truncated multi-byte rune
			}
			return Event{}, 1, false // undecodable: drop the byte
		}
		return KeyOf(Rune(r)), size, true
	}
}

func decodeEsc(b []byte, idle bool) (Event, int, bool) {
	if len(b) == 1 {
		if idle {
			return KeyOf(Named(KeyEsc)), 1, true
		}
		return Event{}, 0, false
	}
	switch b[1] {
	case '[':
		return decodeCSI(b, idle)
	case 'O':
		// SS3: application cursor keys (ESC O A … ESC O F).
		if len(b) < 3 {
			if idle {
				return KeyOf(Named(KeyEsc)), 1, true
			}
			return Event{}, 0, false
		}
		if k, ok := finalKey(b[2]); ok {
			return KeyOf(Named(k)), 3, true
		}
		return KeyOf(Named(KeyEsc)), 1, true
	default:
		// ESC + key in the same read is alt+key.
		ev, n, ok := Decode(b[1:], idle)
		if ok && ev.IsKey() {
			ev.Key.Mods |= ModAlt
			return ev, n + 1, true
		}
		// Anything else means the ESC was NOT a prefix, and the only
		// answer that keeps the decoder alive is to consume it alone.
		//
		// Returning (0, false) here — which is what this did — is the
		// drain loop's "incomplete, wait for more bytes" signal, and for
		// a sequence that is already complete no further byte can ever
		// resolve it. The buffer strands, every later keystroke is
		// appended behind it, and the app paints on forever without
		// taking another key: no error, no exit, no tripwire. Two
		// ordinary inputs reached it. ESC before a MOUSE report decodes
		// perfectly and fails `IsKey`; ESC before an undecodable byte or
		// a known-shape-but-unmapped CSI reports !ok having consumed
		// bytes.
		//
		// Consuming ONLY the ESC — rather than swallowing what follows —
		// is what makes the mouse report arrive as itself on the next
		// pass, so "press Escape, then click" delivers both events
		// instead of losing the click.
		if n == 0 && !ok && !idle {
			return Event{}, 0, false // genuinely truncated: more may come
		}
		return KeyOf(Named(KeyEsc)), 1, true
	}
}

// decodeCSI parses ESC [ params intermediates final, where params are
// 0x30–0x3f, intermediates 0x20–0x2f and the final byte 0x40–0x7e.
func decodeCSI(b []byte, idle bool) (Event, int, bool) {
	i := 2
	for i < len(b) && b[i] >= 0x20 && b[i] < 0x40 {
		i++
	}
	if i >= len(b) {
		if idle {
			return KeyOf(Named(KeyEsc)), 1, true // truncated sequence: it was Esc
		}
		return Event{}, 0, false
	}
	params, final, n := string(b[2:i]), b[i], i+1

	// Mouse reports share the CSI shape: SGR carries a '<' parameter
	// prefix, the legacy X10 form is a bare CSI M with three binary bytes
	// after the final byte. Neither may reach the key mapping below.
	if strings.HasPrefix(params, "<") && (final == 'M' || final == 'm') {
		if m, ok := decodeSGRMouse(params, final); ok {
			return MouseOf(m), n, true
		}
		return Event{}, n, false
	}
	// Bracketed paste. The opening bracket has to be caught HERE, before
	// the key mapping, because CSI 200 ~ is shaped exactly like a key
	// sequence and would otherwise be dropped as an unmapped one — and
	// its payload would then arrive as the burst of keystrokes that mode
	// 2004 exists to prevent.
	if final == '~' && (params == "200" || params == "201") {
		if params == "200" {
			return decodePaste(b[n:], n)
		}
		// A closing bracket with no opening one. Complete, meaningless,
		// and skipped — the n>0/!ok case this decoder already has a
		// contract for. It happens for real: the mode is enabled and
		// disabled around a suspend, and a paste that straddled the
		// window can leave its tail behind.
		return Event{}, n, false
	}
	if params == "" && final == 'M' {
		m, consumed, ok := decodeX10Mouse(b, idle)
		if consumed == 0 {
			if idle {
				return KeyOf(Named(KeyEsc)), 1, true
			}
			return Event{}, 0, false // wait for the coordinate bytes
		}
		if !ok {
			return Event{}, consumed, false
		}
		return MouseOf(m), consumed, true
	}

	ev := KeyEvent{}
	nums := csiParams(params)
	if len(nums) > 1 {
		// xterm modifier encoding: param-1 is a bitmask of shift/alt/ctrl.
		m := nums[1] - 1
		if m&1 != 0 {
			ev.Mods |= ModShift
		}
		if m&2 != 0 {
			ev.Mods |= ModAlt
		}
		if m&4 != 0 {
			ev.Mods |= ModCtrl
		}
	}

	if final == 'Z' { // shift-tab has its own final byte
		ev.Key, ev.Mods = KeyTab, ev.Mods|ModShift
		return KeyOf(ev), n, true
	}
	if k, ok := finalKey(final); ok {
		ev.Key = k
		return KeyOf(ev), n, true
	}
	if final == '~' && len(nums) > 0 {
		if k, ok := tildeKey(nums[0]); ok {
			ev.Key = k
			return KeyOf(ev), n, true
		}
	}
	return Event{}, n, false // known shape, unmapped key: swallow it
}

func csiParams(s string) []int {
	s = strings.TrimPrefix(s, "?")
	if s == "" {
		return nil
	}
	fields := strings.Split(s, ";")
	out := make([]int, len(fields))
	for i, f := range fields {
		out[i], _ = strconv.Atoi(f)
	}
	return out
}

func finalKey(final byte) (Key, bool) {
	switch final {
	case 'A':
		return KeyUp, true
	case 'B':
		return KeyDown, true
	case 'C':
		return KeyRight, true
	case 'D':
		return KeyLeft, true
	case 'H':
		return KeyHome, true
	case 'F':
		return KeyEnd, true
	}
	return 0, false
}

func tildeKey(n int) (Key, bool) {
	switch n {
	case 1, 7:
		return KeyHome, true
	case 3:
		return KeyDelete, true
	case 4, 8:
		return KeyEnd, true
	case 5:
		return KeyPageUp, true
	case 6:
		return KeyPageDown, true
	}
	return 0, false
}
