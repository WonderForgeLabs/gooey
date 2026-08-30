package input

import (
	"sort"
	"strconv"
	"strings"
	"testing"
)

// deadCtrlGestures is every ctrl+<printable> spelling ParseGesture used
// to accept and no decoder can emit, RECORDED rather than derived — the
// derived half is the implementation, and a test that derives the same
// set the same way agrees with any bug in it.
//
// It is a snapshot with a job: if this list stops matching, an arm of
// decode.go moved and the parser followed it silently. That is the
// whole failure class here — a gesture that loads, dispatches forever,
// and never fires — so the drift has to be loud. Read a diff here as
// "which keys changed meaning", not as "update the list".
//
// The three causes, in the order they appear: outside the c|0x40 range
// (everything below @ and above _), claimed by a named key first (h, i,
// j, m and [ are backspace, tab, enter, enter and esc), and — until
// this change — the parser and decoder normalising @ differently.
var deadCtrlGestures = []string{
	"ctrl+!", `ctrl+"`, "ctrl+#", "ctrl+$", "ctrl+%", "ctrl+&", "ctrl+'",
	"ctrl+(", "ctrl+)", "ctrl+*", "ctrl++", "ctrl+,", "ctrl+-", "ctrl+.",
	"ctrl+/", "ctrl+0", "ctrl+1", "ctrl+2", "ctrl+3", "ctrl+4", "ctrl+5",
	"ctrl+6", "ctrl+7", "ctrl+8", "ctrl+9", "ctrl+:", "ctrl+;", "ctrl+<",
	"ctrl+=", "ctrl+>", "ctrl+?",
	"ctrl+H", "ctrl+I", "ctrl+J", "ctrl+M", "ctrl+[", "ctrl+`",
	"ctrl+h", "ctrl+i", "ctrl+j", "ctrl+m",
	"ctrl+{", "ctrl+|", "ctrl+}", "ctrl+~",
}

// namesTheCause is what the error has to say for the gestures whose byte
// was taken by a NAMED key, and it is asserted rather than assumed
// because it is the entire reason errUnproducible has two branches and
// calls Decode a second time.
//
// Without these, swapping the two fmt.Errorf bodies — or dropping the
// Decode call for one generic message — passes every other assertion in
// this file. "ctrl+h is backspace" is the deliverable; "unproducible"
// sends the reader back to the source.
var namesTheCause = map[string]string{
	"ctrl+h": "backspace",
	"ctrl+i": "tab",
	"ctrl+j": "enter",
	"ctrl+m": "enter",
	"ctrl+[": "esc",
}

// TestUnproducibleCtrlGesturesAreRejected is the issue's list, one
// assertion per entry.
//
// ctrl+@ is deliberately NOT here: it is the one unproducible spelling
// whose intent is unambiguous, so it normalises to ctrl+space rather
// than failing. See TestCtrlAtNormalisesToCtrlSpace.
func TestUnproducibleCtrlGesturesAreRejected(t *testing.T) {
	named := map[string]bool{}
	for _, g := range deadCtrlGestures {
		ev, err := ParseGesture(g)
		if err == nil {
			t.Errorf("ParseGesture(%q) = %v, want an error — a binding on it "+
				"loads cleanly and never fires", g, ev)
			continue
		}
		if !strings.Contains(err.Error(), strconv.Quote(g)) {
			t.Errorf("ParseGesture(%q) errored without naming the gesture: %v", g, err)
		}
		if want, ok := namesTheCause[strings.ToLower(g)]; ok {
			named[strings.ToLower(g)] = true
			if !strings.Contains(err.Error(), want) {
				t.Errorf("ParseGesture(%q) = %q, which does not say the byte "+
					"decodes as %s — the cause is the deliverable, not the verdict",
					g, err, want)
			}
		}
	}
	// Every entry reached, rather than a count: ctrl+h and ctrl+H fold to
	// one gesture and ctrl+[ has no upper-case spelling, so counting
	// visits would encode that arithmetic instead of the claim.
	for g := range namesTheCause {
		if !named[g] {
			t.Errorf("%q is not in the dead set, so nothing asserted that its "+
				"error names the key that took its byte", g)
		}
	}
}

// TestAnAltPrefixDoesNotRescueADeadCtrlGesture pins the ModAlt mask in
// errUnproducible from the REJECT side. TestALiveCtrlGestureStillParses
// covers the accept side (ctrl+alt+p), and the mask is on the path of
// shipped keys — the wysiwyg dock binds ctrl+alt+left, ctrl+alt+right
// and ctrl+alt+p — so a mask that leaked either way would matter.
//
// alt is an ESC PREFIX rather than part of the byte, so it cannot make
// an unreachable byte reachable: alt+ctrl+j is 0x1b 0x0a, and the 0x0a
// is still enter.
func TestAnAltPrefixDoesNotRescueADeadCtrlGesture(t *testing.T) {
	for _, g := range []string{"alt+ctrl+j", "ctrl+alt+h", "alt+ctrl+1"} {
		if ev, err := ParseGesture(g); err == nil {
			t.Errorf("ParseGesture(%q) = %v; alt is an ESC prefix and does not "+
				"change which byte the ctrl half needs", g, ev)
		}
	}
}

// TestTheDeadSetIsExactlyWhatTheDecoderSays walks the printable range
// and compares what ParseGesture rejects against the recorded list.
//
// This is the tripwire the issue asked for: the producible set is built
// from Decode at run time, so a decoder arm that moves changes what the
// parser accepts with nothing else to notice. An entry appearing here
// means a key stopped being reportable; an entry disappearing means one
// started.
func TestTheDeadSetIsExactlyWhatTheDecoderSays(t *testing.T) {
	want := map[string]bool{}
	for _, g := range deadCtrlGestures {
		want[g] = true
	}
	var extra, missing []string
	for r := rune(0x21); r < 0x7f; r++ {
		g := "ctrl+" + string(r)
		_, err := ParseGesture(g)
		switch {
		case err != nil && !want[g]:
			extra = append(extra, g)
		case err == nil && want[g]:
			missing = append(missing, g)
		}
	}
	sort.Strings(extra)
	sort.Strings(missing)
	if len(extra) > 0 {
		t.Errorf("these ctrl gestures are newly unproducible: %v — an arm of "+
			"decode.go claimed their bytes, and any binding on them has gone "+
			"silently dead", extra)
	}
	if len(missing) > 0 {
		t.Errorf("these ctrl gestures are newly producible: %v — decode.go "+
			"released their bytes", missing)
	}
}

// TestCtrlByteInvertsTheDecoder checks the one hand-written direction in
// gesture.go against the decoder it claims to invert.
//
// ctrlByte answers "which byte would a terminal send for ctrl+r", and
// it is a switch rather than a derivation — so for every control byte,
// decode it, and if the result is a ctrl rune, ctrlByte must map that
// rune back to the byte it came from.
func TestCtrlByteInvertsTheDecoder(t *testing.T) {
	checked := 0
	for c := 0; c < 0x20; c++ {
		ev, _, ok := Decode([]byte{byte(c)}, true)
		if !ok || ev.Kind != EventKey {
			continue
		}
		k := ev.Key
		if k.Key != KeyRune || k.Mods&ModCtrl == 0 {
			continue // a named key took this byte; ctrlByte says nothing about it
		}
		checked++
		if k.Rune == ' ' {
			continue // 0x00 is ctrl+space, spelled ctrl+@ on the way in
		}
		got, ok := ctrlByte(k.Rune)
		if !ok {
			t.Errorf("Decode(%#02x) produces %s, and ctrlByte has no byte for %q",
				c, k, k.Rune)
			continue
		}
		if got != byte(c) {
			t.Errorf("Decode(%#02x) produces %s, but ctrlByte(%q) = %#02x",
				c, k, k.Rune, got)
		}
	}
	if checked == 0 {
		t.Fatal("no control byte decoded to a ctrl rune — this test proves nothing")
	}
}

// TestCtrlAtNormalisesToCtrlSpace pins the one gesture that is fixed
// rather than refused.
//
// 0x00 is the byte for both spellings and the decoder answers space, so
// leaving the '@' meant the two ends agreed about the byte and
// disagreed about the event.
func TestCtrlAtNormalisesToCtrlSpace(t *testing.T) {
	at, err := ParseGesture("ctrl+@")
	if err != nil {
		t.Fatalf("ParseGesture(\"ctrl+@\") = %v; it is the one unambiguous case "+
			"and should normalise, not fail", err)
	}
	space, err := ParseGesture("ctrl+space")
	if err != nil {
		t.Fatal(err)
	}
	if at != space {
		t.Errorf("ctrl+@ parses to %s and ctrl+space to %s; 0x00 is both", at, space)
	}
	ev, _, ok := Decode([]byte{0x00}, true)
	if !ok || ev.Key != at {
		t.Errorf("the decoder answers %v for 0x00, which is not what ctrl+@ parses to (%s)",
			ev.Key, at)
	}
}

// TestALiveCtrlGestureStillParses is the non-vacuity floor. A change
// that rejected every ctrl gesture would pass every assertion above.
func TestALiveCtrlGestureStillParses(t *testing.T) {
	for _, g := range []string{"ctrl+a", "ctrl+s", "ctrl+z", "ctrl+\\", "ctrl+k",
		"ctrl+l", "ctrl+space", "ctrl+alt+p", "alt+j", "alt+h"} {
		if _, err := ParseGesture(g); err != nil {
			t.Errorf("ParseGesture(%q) = %v, want it to parse", g, err)
		}
	}
}
