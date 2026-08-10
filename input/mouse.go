package input

// MouseKind is what the pointer did. Click is not a terminal report:
// the dispatcher synthesizes it when a press and its release land on the
// same component.
type MouseKind uint8

const (
	MousePress MouseKind = iota
	MouseRelease
	MouseMove
	MouseClick
	WheelUp
	WheelDown
)

type MouseButton uint8

const (
	ButtonLeft MouseButton = iota
	ButtonMiddle
	ButtonRight
	ButtonNone // motion with nothing held, and wheel events
)

// MouseEvent is one pointer report in 0-based cell coordinates.
//
// Count is meaningful only on MouseClick, where it is 1 for a single
// click and 2 for a double click — the dispatcher counts clicks that
// land on the same component inside its double-click interval. There is
// no triple click: a third click inside the interval starts a new
// sequence at 1, so nothing ever reports a count above 2.
type MouseEvent struct {
	Kind   MouseKind
	Button MouseButton
	X, Y   int
	Mods   Mods
	Count  int
}

// EventKind discriminates the union below.
type EventKind uint8

const (
	EventKey EventKind = iota
	EventMouse
)

// Event is one input event. Keys and mouse reports arrive interleaved on
// the same wire, so they stay on one ordered stream rather than two
// channels that could reorder relative to each other.
type Event struct {
	Kind  EventKind
	Key   KeyEvent
	Mouse MouseEvent
}

func KeyOf(k KeyEvent) Event     { return Event{Kind: EventKey, Key: k} }
func MouseOf(m MouseEvent) Event { return Event{Kind: EventMouse, Mouse: m} }

func (e Event) IsKey() bool   { return e.Kind == EventKey }
func (e Event) IsMouse() bool { return e.Kind == EventMouse }

// IsMove reports a motion event — the high-frequency kind callers
// coalesce between frames.
func (e Event) IsMove() bool {
	return e.Kind == EventMouse && e.Mouse.Kind == MouseMove
}

// mouseFromBits maps the Cb bitfield — shared by both mouse encodings —
// to an event. The low two bits pick the button, +4/+8/+16 are
// shift/alt/ctrl, +32 marks motion, +64 marks the wheel. release comes
// from outside because the two encodings report it differently.
func mouseFromBits(cb, x, y int, release bool) (MouseEvent, bool) {
	ev := MouseEvent{X: x, Y: y}
	if cb&4 != 0 {
		ev.Mods |= ModShift
	}
	if cb&8 != 0 {
		ev.Mods |= ModAlt
	}
	if cb&16 != 0 {
		ev.Mods |= ModCtrl
	}
	switch {
	case cb&64 != 0:
		ev.Button = ButtonNone
		switch cb & 3 {
		case 0:
			ev.Kind = WheelUp
		case 1:
			ev.Kind = WheelDown
		default:
			return MouseEvent{}, false // horizontal wheel: unmapped
		}
	case release:
		// X10 cannot say which button came up; SGR can.
		ev.Kind, ev.Button = MouseRelease, ButtonNone
		if cb&3 != 3 {
			ev.Button = MouseButton(cb & 3)
		}
	case cb&32 != 0:
		// Motion. Button bits are 3 ("no button") for a plain hover and
		// the held button during a drag.
		ev.Kind, ev.Button = MouseMove, ButtonNone
		if cb&3 != 3 {
			ev.Button = MouseButton(cb & 3)
		}
	default:
		ev.Kind, ev.Button = MousePress, MouseButton(cb&3)
	}
	return ev, true
}

// decodeSGRMouse parses CSI < Cb ; Cx ; Cy M|m. The M/m final byte
// distinguishes press from release, which is the whole point of SGR
// mode — and the reason to prefer it.
func decodeSGRMouse(params string, final byte) (MouseEvent, bool) {
	nums := csiParams(params[1:]) // drop '<'
	if len(nums) < 3 {
		return MouseEvent{}, false
	}
	return mouseFromBits(nums[0], nums[1]-1, nums[2]-1, final == 'm')
}

// decodeX10Mouse parses the original encoding, CSI M Cb Cx Cy, where
// each of the three bytes is its value plus 32. Terminals fall back to
// it when they do not honour the SGR request — and it must be decoded,
// not skipped: the trailing bytes are printable ASCII, so an unhandled
// report degrades into phantom keystrokes (a wheel notch arrives as
// 'a', a click as a space) that reach the app as real commands.
//
// It cannot report which button was released, and it cannot express a
// column or row past 223.
func decodeX10Mouse(b []byte, idle bool) (MouseEvent, int, bool) {
	const n = 6 // ESC [ M Cb Cx Cy
	if len(b) < n {
		if idle {
			return MouseEvent{}, 0, false // truncated: caller falls back to Esc
		}
		return MouseEvent{}, 0, false
	}
	// Coordinates are 1-based and biased by 32, so the smallest legal
	// byte is 33. Anything lower is a malformed report — consume it
	// rather than letting the bytes fall through as keystrokes.
	cb, x, y := int(b[3])-32, int(b[4])-33, int(b[5])-33
	if cb < 0 || x < 0 || y < 0 {
		return MouseEvent{}, n, false
	}
	release := cb&3 == 3 && cb&32 == 0 && cb&64 == 0
	ev, ok := mouseFromBits(cb, x, y, release)
	return ev, n, ok
}
