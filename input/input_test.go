package input

import "testing"

func TestParseGesture(t *testing.T) {
	cases := []struct {
		in   string
		want KeyEvent
	}{
		{"j", Rune('j')},
		{"J", Rune('J')},
		{"shift+j", Rune('J')},
		{"space", Rune(' ')},
		{"ctrl+s", KeyEvent{Key: KeyRune, Rune: 's', Mods: ModCtrl}},
		{"ctrl+S", KeyEvent{Key: KeyRune, Rune: 's', Mods: ModCtrl}},
		{"alt+ctrl+x", KeyEvent{Key: KeyRune, Rune: 'x', Mods: ModCtrl | ModAlt}},
		{"tab", Named(KeyTab)},
		{"shift+tab", KeyEvent{Key: KeyTab, Mods: ModShift}},
		{"Enter", Named(KeyEnter)},
		{"esc", Named(KeyEsc)},
		{"up", Named(KeyUp)},
		{" pagedown ", Named(KeyPageDown)},
		{"ctrl++", KeyEvent{Key: KeyRune, Rune: '+', Mods: ModCtrl}},
	}
	for _, c := range cases {
		got, err := ParseGesture(c.in)
		if err != nil {
			t.Errorf("ParseGesture(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseGesture(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseGestureErrors(t *testing.T) {
	for _, in := range []string{"", "hyper+s", "ctrl+nope", "ctrl+"} {
		if ev, err := ParseGesture(in); err == nil {
			t.Errorf("ParseGesture(%q) = %v, want error", in, ev)
		}
	}
}

func TestGestureStringRoundTrips(t *testing.T) {
	for _, in := range []string{"j", "space", "ctrl+s", "ctrl+alt+x", "tab", "shift+tab", "esc", "pageup"} {
		ev, err := ParseGesture(in)
		if err != nil {
			t.Fatalf("ParseGesture(%q): %v", in, err)
		}
		back, err := ParseGesture(ev.String())
		if err != nil {
			t.Fatalf("ParseGesture(%q): %v", ev.String(), err)
		}
		if back != ev {
			t.Errorf("%q → %v → %q → %v", in, ev, ev.String(), back)
		}
	}
}

func TestDecode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		idle bool
		want KeyEvent
		n    int
	}{
		{"rune", "a", false, Rune('a'), 1},
		{"utf8", "é", false, Rune('é'), 2},
		{"enter cr", "\r", false, Named(KeyEnter), 1},
		{"enter lf", "\n", false, Named(KeyEnter), 1},
		{"tab", "\t", false, Named(KeyTab), 1},
		{"backspace", "\x7f", false, Named(KeyBackspace), 1},
		{"ctrl-s", "\x13", false, KeyEvent{Key: KeyRune, Rune: 's', Mods: ModCtrl}, 1},
		{"ctrl-c", "\x03", false, KeyEvent{Key: KeyRune, Rune: 'c', Mods: ModCtrl}, 1},
		{"up", "\x1b[A", false, Named(KeyUp), 3},
		{"down", "\x1b[B", false, Named(KeyDown), 3},
		{"right", "\x1b[C", false, Named(KeyRight), 3},
		{"left", "\x1b[D", false, Named(KeyLeft), 3},
		{"ss3 up", "\x1bOA", false, Named(KeyUp), 3},
		{"shift-tab", "\x1b[Z", false, KeyEvent{Key: KeyTab, Mods: ModShift}, 3},
		{"home", "\x1b[H", false, Named(KeyHome), 3},
		{"end tilde", "\x1b[4~", false, Named(KeyEnd), 4},
		{"delete", "\x1b[3~", false, Named(KeyDelete), 4},
		{"pageup", "\x1b[5~", false, Named(KeyPageUp), 4},
		{"ctrl-up", "\x1b[1;5A", false, KeyEvent{Key: KeyUp, Mods: ModCtrl}, 6},
		{"alt-j", "\x1bj", false, KeyEvent{Key: KeyRune, Rune: 'j', Mods: ModAlt}, 2},
		{"lone esc when idle", "\x1b", true, Named(KeyEsc), 1},
	}
	for _, c := range cases {
		ev, n, ok := Decode([]byte(c.in), c.idle)
		if !ok || !ev.IsKey() {
			t.Errorf("%s: Decode(%q) did not yield a key event", c.name, c.in)
			continue
		}
		if ev.Key != c.want || n != c.n {
			t.Errorf("%s: Decode(%q) = %v,%d want %v,%d", c.name, c.in, ev.Key, n, c.want, c.n)
		}
	}
}

// A bare ESC is also the first byte of every escape sequence: it must
// stay pending until the caller reports the input went idle.
func TestDecodeEscAmbiguity(t *testing.T) {
	if _, n, ok := Decode([]byte("\x1b"), false); ok || n != 0 {
		t.Fatalf("pending ESC decoded as %d bytes (ok=%v), want incomplete", n, ok)
	}
	if _, n, ok := Decode([]byte("\x1b["), false); ok || n != 0 {
		t.Fatalf("partial CSI decoded (n=%d ok=%v), want incomplete", n, ok)
	}
	ev, n, ok := Decode([]byte("\x1b[A"), false)
	if !ok || ev.Key != Named(KeyUp) || n != 3 {
		t.Fatalf("completed CSI = %v,%d,%v", ev, n, ok)
	}
}

func TestDecodeStream(t *testing.T) {
	in := []byte("ab\x1b[Bx\r\x1b[Z")
	want := []KeyEvent{Rune('a'), Rune('b'), Named(KeyDown), Rune('x'), Named(KeyEnter),
		{Key: KeyTab, Mods: ModShift}}
	var got []KeyEvent
	for len(in) > 0 {
		ev, n, ok := Decode(in, false)
		if n == 0 && !ok {
			t.Fatalf("stalled with %q remaining", in)
		}
		in = in[n:]
		if ok {
			got = append(got, ev.Key)
		}
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event %d = %v, want %v", i, got[i], want[i])
		}
	}
}
