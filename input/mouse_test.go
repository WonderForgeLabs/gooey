package input

import "testing"

func TestDecodeSGRMouse(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want MouseEvent
		n    int
	}{
		{"left press", "\x1b[<0;10;5M", MouseEvent{Kind: MousePress, Button: ButtonLeft, X: 9, Y: 4}, 10},
		{"left release", "\x1b[<0;10;5m", MouseEvent{Kind: MouseRelease, Button: ButtonLeft, X: 9, Y: 4}, 10},
		{"middle press", "\x1b[<1;1;1M", MouseEvent{Kind: MousePress, Button: ButtonMiddle}, 9},
		{"right press", "\x1b[<2;3;4M", MouseEvent{Kind: MousePress, Button: ButtonRight, X: 2, Y: 3}, 9},
		{"wheel up", "\x1b[<64;7;8M", MouseEvent{Kind: WheelUp, Button: ButtonNone, X: 6, Y: 7}, 10},
		{"wheel down", "\x1b[<65;7;8M", MouseEvent{Kind: WheelDown, Button: ButtonNone, X: 6, Y: 7}, 10},
		{"ctrl+click", "\x1b[<16;2;2M", MouseEvent{Kind: MousePress, Button: ButtonLeft, X: 1, Y: 1, Mods: ModCtrl}, 10},
		{"shift+click", "\x1b[<4;2;2M", MouseEvent{Kind: MousePress, Button: ButtonLeft, X: 1, Y: 1, Mods: ModShift}, 9},
		{"hover motion", "\x1b[<35;40;12M", MouseEvent{Kind: MouseMove, Button: ButtonNone, X: 39, Y: 11}, 12},
		{"left drag", "\x1b[<32;40;12M", MouseEvent{Kind: MouseMove, Button: ButtonLeft, X: 39, Y: 11}, 12},
		{"ctrl wheel", "\x1b[<80;5;5M", MouseEvent{Kind: WheelUp, Button: ButtonNone, X: 4, Y: 4, Mods: ModCtrl}, 10},
		{"wide coords", "\x1b[<0;250;120M", MouseEvent{Kind: MousePress, Button: ButtonLeft, X: 249, Y: 119}, 13},
	}
	for _, c := range cases {
		ev, n, ok := Decode([]byte(c.in), false)
		if !ok || !ev.IsMouse() {
			t.Errorf("%s: Decode(%q) did not yield a mouse event", c.name, c.in)
			continue
		}
		if ev.Mouse != c.want || n != c.n {
			t.Errorf("%s: Decode(%q) = %+v,%d want %+v,%d", c.name, c.in, ev.Mouse, n, c.want, c.n)
		}
	}
}

// The mouse prefix must not disturb the escape handling: ESC [ < is a
// prefix of a longer sequence and must stay pending, not resolve to Esc.
func TestMouseSequenceStaysPending(t *testing.T) {
	for _, partial := range []string{"\x1b[<", "\x1b[<0;10", "\x1b[<0;10;5"} {
		if ev, n, ok := Decode([]byte(partial), false); ok || n != 0 {
			t.Errorf("Decode(%q) = %v,%d,%v — want incomplete", partial, ev, n, ok)
		}
	}
	// Idle with a partial mouse report: it degrades to Esc rather than
	// hanging, and the remaining bytes decode as runes.
	if ev, n, ok := Decode([]byte("\x1b[<0;10"), true); !ok || n != 1 || ev.Key != Named(KeyEsc) {
		t.Errorf("idle partial mouse = %v,%d,%v — want Esc consuming 1", ev, n, ok)
	}
}

// Terminals that ignore the SGR request fall back to CSI M + three
// bytes. Those bytes are printable ASCII, so failing to decode the
// report does not merely lose the event — it injects phantom keystrokes
// (a wheel notch arrives as 'a', which in an app with an 'a' binding
// fires a command). Every case here must decode as a mouse event and
// consume all six bytes.
func TestDecodeX10Mouse(t *testing.T) {
	x10 := func(cb, x, y int) string {
		return string([]byte{0x1b, '[', 'M', byte(cb + 32), byte(x + 32), byte(y + 32)})
	}
	cases := []struct {
		name string
		in   string
		want MouseEvent
	}{
		{"left press", x10(0, 10, 5), MouseEvent{Kind: MousePress, Button: ButtonLeft, X: 9, Y: 4}},
		{"middle press", x10(1, 3, 3), MouseEvent{Kind: MousePress, Button: ButtonMiddle, X: 2, Y: 2}},
		{"release", x10(3, 10, 5), MouseEvent{Kind: MouseRelease, Button: ButtonNone, X: 9, Y: 4}},
		{"wheel up", x10(64, 7, 8), MouseEvent{Kind: WheelUp, Button: ButtonNone, X: 6, Y: 7}},
		{"wheel down", x10(65, 7, 8), MouseEvent{Kind: WheelDown, Button: ButtonNone, X: 6, Y: 7}},
		{"hover motion", x10(35, 40, 12), MouseEvent{Kind: MouseMove, Button: ButtonNone, X: 39, Y: 11}},
		{"ctrl+click", x10(16, 2, 2), MouseEvent{Kind: MousePress, Button: ButtonLeft, X: 1, Y: 1, Mods: ModCtrl}},
	}
	for _, c := range cases {
		ev, n, ok := Decode([]byte(c.in), false)
		if !ok || !ev.IsMouse() {
			t.Errorf("%s: Decode(%q) did not yield a mouse event", c.name, c.in)
			continue
		}
		if ev.Mouse != c.want || n != 6 {
			t.Errorf("%s: Decode = %+v,%d want %+v,6", c.name, ev.Mouse, n, c.want)
		}
	}
}

// The three coordinate bytes must never be decoded as keys, whether the
// report is complete or still arriving.
func TestX10MouseNeverLeaksKeystrokes(t *testing.T) {
	full := []byte{0x1b, '[', 'M', 32 + 65, 32 + 35, 32 + 6} // wheel down
	var got []Event
	b := full
	for len(b) > 0 {
		ev, n, ok := Decode(b, false)
		if n == 0 && !ok {
			t.Fatalf("stalled with %q remaining", b)
		}
		b = b[n:]
		if ok {
			got = append(got, ev)
		}
	}
	if len(got) != 1 || !got[0].IsMouse() || got[0].Mouse.Kind != WheelDown {
		t.Fatalf("X10 report decoded as %+v, want exactly one WheelDown", got)
	}
	for _, partial := range [][]byte{full[:3], full[:4], full[:5]} {
		if ev, n, ok := Decode(partial, false); ok || n != 0 {
			t.Errorf("partial X10 %q decoded as %v (n=%d) — must wait for the rest", partial, ev, n)
		}
	}
}

func TestDecodeMixedStream(t *testing.T) {
	in := []byte("a\x1b[<0;3;4M\x1b[<0;3;4m\x1b[Bz")
	var got []Event
	for len(in) > 0 {
		ev, n, ok := Decode(in, false)
		if n == 0 && !ok {
			t.Fatalf("stalled with %q remaining", in)
		}
		in = in[n:]
		if ok {
			got = append(got, ev)
		}
	}
	if len(got) != 5 {
		t.Fatalf("got %d events, want 5: %+v", len(got), got)
	}
	if got[0].Key != Rune('a') || !got[1].IsMouse() || got[1].Mouse.Kind != MousePress ||
		got[2].Mouse.Kind != MouseRelease || got[3].Key != Named(KeyDown) || got[4].Key != Rune('z') {
		t.Fatalf("stream decoded wrong: %+v", got)
	}
}
