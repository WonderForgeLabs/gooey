package render

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// Screen is a terminal model: write the bytes a Flush produced into it and
// it holds what a terminal would be showing.
//
// It exists because the flush stopped being self-describing. While every
// frame was a full repaint, "what is on screen" and "what went down the
// wire" were the same string, and a test could grep the wire for the text
// it expected. An incremental flush sends the DIFFERENCE — change "n=2"
// to "n=3" and the only byte on the wire is a 3 — so the wire no longer
// contains the screen, and anything asserting on the screen has to
// reconstruct it. This is that reconstruction, and it doubles as the
// audit of the diff: if replaying the emitted bytes does not reproduce
// the buffer, the diff is wrong.
//
// It is an io.Writer, so it can be handed straight to Flush, to
// Composer.Flush, or fed the bytes arriving from a pty. It understands
// the grammar this package emits — cursor positioning, SGR, erases,
// newlines — skips the sequences it does not model (mode changes, OSC,
// and the DCS/APC blocks the graphics protocols use) rather than
// printing them, and tolerates a sequence split across writes.
//
// It is a model of a terminal, not an emulator: no scrollback, no
// scrolling regions, no wrap. Content written past the last column or row
// is dropped, which is what a gooey frame never does anyway.
type Screen struct {
	Buf *Buffer

	x, y    int
	cur     Style
	pending []byte
}

func NewScreen(w, h int) *Screen { return &Screen{Buf: NewBuffer(w, h)} }

// Resize re-targets the model, clearing it — the same thing a terminal
// does to a full-frame flush that follows a size change.
func (s *Screen) Resize(w, h int) {
	s.Buf = NewBuffer(w, h)
	s.x, s.y = 0, 0
}

// Text is the visible screen as lines, trailing blanks trimmed.
func (s *Screen) Text() string {
	rows := make([]string, s.Buf.H)
	for y := range rows {
		rows[y] = s.Row(y)
	}
	return strings.Join(rows, "\n")
}

// Row is one line of the model, trailing blanks trimmed.
func (s *Screen) Row(y int) string {
	var sb strings.Builder
	for x := 0; x < s.Buf.W; x++ {
		r := s.Buf.At(x, y).Rune
		if r == 0 {
			r = ' '
		}
		sb.WriteRune(r)
	}
	return strings.TrimRight(sb.String(), " ")
}

// Contains reports whether any single row contains want. Text() joins
// rows with newlines, so a caller looking for a phrase that a frame put
// on one line should ask here rather than searching the join.
func (s *Screen) Contains(want string) bool {
	for y := 0; y < s.Buf.H; y++ {
		if strings.Contains(s.Row(y), want) {
			return true
		}
	}
	return false
}

func (s *Screen) Write(p []byte) (int, error) {
	n := len(p)
	buf := p
	if len(s.pending) > 0 {
		buf = append(s.pending, p...)
		s.pending = nil
	}
	for len(buf) > 0 {
		if buf[0] == 0x1b {
			used, complete := s.escape(buf)
			if !complete {
				// A sequence split across writes: hold it for the rest.
				s.pending = append(s.pending[:0], buf...)
				return n, nil
			}
			buf = buf[used:]
			continue
		}
		r, size := utf8.DecodeRune(buf)
		if r == utf8.RuneError && size == 1 && len(buf) < 4 {
			s.pending = append(s.pending[:0], buf...)
			return n, nil
		}
		s.put(r)
		buf = buf[size:]
	}
	return n, nil
}

func (s *Screen) put(r rune) {
	switch r {
	case '\r':
		s.x = 0
	case '\n':
		s.y++
	case '\b':
		if s.x > 0 {
			s.x--
		}
	default:
		s.Buf.Set(s.x, s.y, r, s.cur)
		s.x++
	}
}

// escape consumes one escape sequence, reporting how many bytes it used
// and whether it was complete. An incomplete one is held for the next
// write rather than misparsed.
func (s *Screen) escape(b []byte) (used int, complete bool) {
	if len(b) < 2 {
		return 0, false
	}
	switch b[1] {
	case '[':
		i := 2
		for i < len(b) && b[i] >= 0x20 && b[i] <= 0x3f {
			i++
		}
		if i >= len(b) {
			return 0, false
		}
		s.csi(string(b[2:i]), b[i])
		return i + 1, true
	case ']': // OSC: string terminator is BEL or ESC \
		for i := 2; i < len(b); i++ {
			if b[i] == 0x07 {
				return i + 1, true
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2, true
			}
		}
		return 0, false
	case 'P', '_', '^', 'X': // DCS (sixel), APC (kitty), PM, SOS — all ESC \ terminated
		for i := 2; i+1 < len(b); i++ {
			if b[i] == 0x1b && b[i+1] == '\\' {
				return i + 2, true
			}
		}
		return 0, false
	case '\\': // a stray string terminator
		return 2, true
	default:
		return 2, true // two-byte escape we do not model
	}
}

func (s *Screen) csi(body string, final byte) {
	if strings.HasPrefix(body, "?") {
		// Private modes. The alternate screen arrives blank, which is the
		// one that matters: it is why a host invalidates its flush after
		// re-acquiring the terminal.
		if (final == 'h' || final == 'l') && strings.Contains(body, "1049") {
			s.Buf.Clear()
			s.x, s.y = 0, 0
		}
		return
	}
	args := csiArgs(body)
	arg := func(i, def int) int {
		if i < len(args) && args[i] > 0 {
			return args[i]
		}
		return def
	}
	switch final {
	case 'H', 'f':
		s.y, s.x = arg(0, 1)-1, arg(1, 1)-1
	case 'A':
		s.y -= arg(0, 1)
	case 'B':
		s.y += arg(0, 1)
	case 'C':
		s.x += arg(0, 1)
	case 'D':
		s.x -= arg(0, 1)
	case 'J':
		s.erase(arg(0, 0))
	case 'K':
		s.eraseLine(arg(0, 0))
	case 'm':
		s.sgr(args, body)
	}
	s.x, s.y = max(0, s.x), max(0, s.y)
}

func (s *Screen) erase(mode int) {
	from, to := 0, s.Buf.W*s.Buf.H
	switch mode {
	case 0:
		from = s.y*s.Buf.W + s.x
	case 1:
		to = s.y*s.Buf.W + s.x + 1
	}
	for i := max(0, from); i < min(to, len(s.Buf.Cells)); i++ {
		s.Buf.Cells[i] = Cell{Rune: ' '}
	}
}

func (s *Screen) eraseLine(mode int) {
	from, to := 0, s.Buf.W
	switch mode {
	case 0:
		from = s.x
	case 1:
		to = s.x + 1
	}
	for x := from; x < to; x++ {
		s.Buf.Set(x, s.y, ' ', Style{})
	}
}

// sgr walks the parameters as a sequence, because the color forms consume
// the ones after them: 38;2;r;g;b and 38;5;idx are single parameters
// spelled with semicolons, not five and three separate attributes.
func (s *Screen) sgr(args []int, body string) {
	if body == "" {
		s.cur = Style{}
		return
	}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case 0:
			s.cur = Style{}
		case 1:
			s.cur.Bold = true
		case 4:
			s.cur.Underline = true
		case 7:
			s.cur.Reverse = true
		case 38, 48:
			c, used := parseColor(args[i+1:])
			if args[i] == 38 {
				s.cur.Fg = c
			} else {
				s.cur.Bg = c
			}
			i += used
		}
	}
}

// parseColor reads an extended color parameter and reports how many
// arguments it consumed. The 256-color form is not inverted back to RGB —
// nothing asks a model screen what shade a cell is, only whether the flush
// changed style where it should have.
func parseColor(rest []int) (Color, int) {
	if len(rest) == 0 {
		return Color{}, 0
	}
	switch rest[0] {
	case 2:
		if len(rest) < 4 {
			return Color{Set: true}, len(rest)
		}
		return Color{R: uint8(rest[1]), G: uint8(rest[2]), B: uint8(rest[3]), Set: true}, 4
	case 5:
		if len(rest) < 2 {
			return Color{Set: true}, len(rest)
		}
		return Color{R: uint8(rest[1]), Set: true}, 2
	}
	return Color{Set: true}, 1
}

func csiArgs(body string) []int {
	if body == "" {
		return nil
	}
	parts := strings.Split(body, ";")
	out := make([]int, len(parts))
	for i, p := range parts {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
