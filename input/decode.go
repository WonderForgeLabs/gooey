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
// is true, Decode consumes at least one byte or produces an event —
// never (0, false) — EXCEPT for a bracketed paste. That exception has
// TWO members, and naming only the first is how it got understated
// before:
//
//   - a SPLIT MARKER — ESC [ 2 0 0 ~ or ESC [ 2 0 1 ~ and their prefixes
//     from the third byte on, nothing else (splitPasteMarker). This one
//     is time-limited: DecodeFinal withdraws it, because those same
//     bytes are also keys a person can type (#440).
//   - an OPEN PASTE — the marker complete, the payload's end not yet
//     arrived (decodePaste). This one is NOT time-limited and survives
//     DecodeFinal, because resolving it truncates the paste silently.
//
// See splitPasteMarker and decodePaste for the reasoning behind each.
//
// Stating the exception rather than the absolute matters more than it
// looks. This comment used to say "Never (0, false)" flat, while
// decodePaste had ALREADY departed from it and said so in its own
// doc — so the contract read as universal, the counter-example lived
// one file away, and the exhaustive test below could not see the
// difference because it only checks 1- and 2-byte inputs. A reader
// arriving at the paste code with this sentence in mind would have
// "restored" the guarantee and reintroduced the bug.
//
// The reason the rule holds everywhere else: (0, false) is the drain
// loop's ONLY "wait for more bytes" signal (see term.DecodeEvents), and
// under idle no further byte is coming — so a sequence that still
// reports incomplete can never be resolved. The buffer strands, every
// later keystroke is appended behind it, and the app paints on forever
// without taking another key. No error, no exit, no tripwire: App.Run
// watches for a decoder that DIED, and this one is alive.
//
// What was supposed to make the paste exception safe is that the
// terminal is mid-write: the rest of the marker and its payload already
// on the wire, so any later byte resolves the buffer either way. THAT
// ARGUMENT IS FALSE FOR HALF THE EXCEPTION, and the sentence it replaces
// asserted it for all of it. ESC [ 2 is a marker prefix and also Esc,
// `[`, `2` typed by a person, and in the typed case there is no later
// byte — the hold was permanent, and the app went deaf while waking
// every EscTimeout forever (#440). The split-marker half is now bounded
// in time (DecodeFinal, term.PasteMarkerGrace); the OPEN-paste half
// keeps the wedge on purpose, and its justification is a different one
// — truncating a paste is worse — written out on decodePaste.
//
// It costs nothing to preserve and is silent to break, so the rule is
// asserted exhaustively rather than trusted — decodeidle_test.go checks
// every 1- and 2-byte input, which is also exactly the range the paste
// exception stays out of. If you are changing a `return` in decodeCSI,
// decodeEsc or decodeX10Mouse, that test is the one to run.
//
// THE EXCEPTION IS TIME-LIMITED, and DecodeFinal is how a caller says
// the time is up. See its doc for the case that made it necessary.
func Decode(b []byte, idle bool) (Event, int, bool) {
	if idle {
		return decode(b, deadlineIdled)
	}
	return decode(b, deadlineLive)
}

// DecodeFinal is Decode with the split-paste-marker exception WITHDRAWN.
// The caller is asserting that NOTHING MORE CAN ARRIVE, so "the terminal
// is mid-write" — the entire justification for that exception — is no
// longer available.
//
// That precondition is about arrival, not about elapsed time, and the
// two callers reach it differently: term.DecodeEvents' stall path gets
// there on the PasteMarkerGrace'th consecutive fruitless timeout, and
// its tty-close path gets there with ZERO timeouts elapsed, a closed tty
// being a stronger guarantee than any deadline. Stating this as "after
// two timeouts" was wrong for the second caller. Raised in review of
// #445.
//
// The case it exists for is not a paste. It is a user pressing Esc and
// then typing `[`, `2`. Those three bytes are a strict prefix of
// ESC [ 200 ~, so splitPasteMarker claims them and Decode answers
// (0, false) under idle forever: the Esc is never delivered, the next
// unrelated keystroke is absorbed into the CSI parse (ESC [ 2 a is one
// unmapped CSI, four bytes, no event at all), and DecodeEvents re-arms
// its 40ms timer on every iteration for the rest of the process's life
// because pend never shrinks. Reported as #440, found in the seventh
// review of #425.
//
// What it does NOT withdraw is decodePaste's wedge — an OPEN paste,
// ESC [ 200 ~ complete with a payload whose end marker has not arrived,
// still waits. That is a different exception with a different reason
// (delivering the prefix silently truncates the paste, and a user who
// pastes 40KB and gets 8KB has no way to tell), and it is deliberate.
// So DecodeFinal's liveness is "progress on everything except an open
// paste", not an absolute — stating the exception rather than the
// absolute is the lesson Decode's own comment above records.
//
// The residual risk, stated rather than waved at: a genuine paste marker
// split across reads MORE than two escape timeouts apart resolves to Esc
// and its payload arrives as keystrokes, which is #419's symptom. The
// grace period is term.PasteMarkerGrace, named there so the trade is one
// number in one place; going deaf forever is the worse half of it.
//
// AND HALF OF #440'S SYMPTOM SURVIVES. The permanent strand is gone
// unconditionally, but the absorbed keystroke is only fixed for a key
// arriving AFTER the window — one arriving inside it is still swallowed,
// because the window is exactly the interval in which the decoder is
// still waiting for more bytes and the key is more bytes. ESC [ 2 then z
// within 80ms yields no event at all. Closing that needs the decoder to
// remember that this CSI began as a held prefix, which is state this
// package does not have; it is
// https://github.com/WonderForgeLabs/gooey/issues/447.
func DecodeFinal(b []byte) (Event, int, bool) { return decode(b, deadlineFinal) }

// deadline is how much of the terminal's benefit of the doubt is left.
// Internal on purpose: Decode's public signature stays a bool, so no
// caller and no test moves for this.
type deadline uint8

const (
	// deadlineFinal, not `final`: decodeCSI's local for the CSI final byte
	// is also called `final`, so the bare name means one thing above that
	// declaration and is a type error below it — inside the function every
	// arm of this change routes through. Prefixed all three rather than one,
	// so the set reads as a set. Raised in review of #445.
	deadlineLive  deadline = iota // bytes may still be arriving
	deadlineIdled                 // none arrived within the escape timeout
	deadlineFinal                 // nor within a second one: nothing more is coming
)

// idle reports whether the escape timeout has fired at least once, which
// is the only question every arm but the paste one asks.
func (d deadline) idle() bool { return d != deadlineLive }

func decode(b []byte, d deadline) (Event, int, bool) {
	idle := d.idle()
	if len(b) == 0 {
		return Event{}, 0, false
	}
	switch c := b[0]; {
	case c == 0x1b:
		return decodeEsc(b, d)
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

func decodeEsc(b []byte, d deadline) (Event, int, bool) {
	idle := d.idle()
	if len(b) == 1 {
		if idle {
			return KeyOf(Named(KeyEsc)), 1, true
		}
		return Event{}, 0, false
	}
	switch b[1] {
	case '[':
		return decodeCSI(b, d)
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
		ev, n, ok := decode(b[1:], d)
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
func decodeCSI(b []byte, d deadline) (Event, int, bool) {
	idle := d.idle()
	i := 2
	for i < len(b) && b[i] >= 0x20 && b[i] < 0x40 {
		i++
	}
	if i >= len(b) {
		// A SPLIT PASTE MARKER IS NOT THE ESC KEY, and the general rule
		// below cannot tell the difference on its own.
		//
		// decodePaste already refuses to let idle resolve an open
		// bracket, for the reason written out there. But the bracket
		// itself is six bytes and straddles a read just as readily, and
		// this arm ran first: every prefix of it became Esc, consuming
		// one byte and leaving "[200~" to arrive as five ordinary keys
		// with the entire payload behind them as keystrokes. A 40ms
		// stall inside six bytes is all it takes, and what the user gets
		// is the burst that mode 2004 exists to prevent. Found in the
		// review of #391 (issue #419).
		//
		// `d == idled`, NOT `idle`, and the difference is the whole of
		// #440. Waiting is only defensible while "the terminal is
		// mid-write" is still a live possibility; after a SECOND timeout
		// with the buffer unchanged it is not, and the reading that is
		// left — a user who pressed Esc and then typed `[`, `2` — has no
		// rest of the marker on the wire to wait for. Holding on made the
		// app permanently deaf and re-armed the escape timer every 40ms
		// forever. See DecodeFinal.
		if d == deadlineIdled && splitPasteMarker(b) {
			return Event{}, 0, false
		}
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
