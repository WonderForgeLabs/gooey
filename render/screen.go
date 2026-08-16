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
// # It used to be a model, and is now also a display
//
// The original sentence here was "a model of a terminal, not an emulator:
// no scrollback, no scrolling regions, no wrap", and for replaying a
// gooey flush that was exactly right — a frame never writes past the last
// column, never scrolls, and repositions absolutely for every run.
//
// Then this type started being fed a real pty and its cells blitted into
// a real frame, and every one of those omissions became a visible defect
// at once. The one that gave it away: an editor inserting a line emits
// IL (`ESC[L`) and shifts characters with ICH/DCH (`ESC[@`, `ESC[P`),
// none of which were implemented — so the inserted text landed ON TOP of
// the line already there and the two spliced into one unreadable row. It
// looked like something occluding something else. It was arithmetic that
// never happened.
//
// So the CSI set below is now the one a full-screen program actually
// uses: absolute column and row addressing, insert/delete of characters
// and lines, scroll up/down, a scrolling region, and deferred autowrap.
// Not a complete VT — no scrollback, no origin mode, no character sets —
// but enough that vim, a shell and top are modelled rather than mangled.
//
// Deferred wrap is the subtle one and it is why gooey's own flush is
// unaffected: writing to the LAST column sets a pending flag rather than
// moving the cursor, so a frame that fills a row edge to edge and then
// repositions never scrolls the screen. Wrapping eagerly would have
// broken every full-width row this package emits.
type Screen struct {
	Buf *Buffer

	x, y    int
	cur     Style
	pending []byte

	// The scrolling region, inclusive, in rows. Defaults to the whole
	// screen; DECSTBM (`ESC[r`) narrows it, and every vertical motion —
	// LF at the bottom, IL, DL, SU, SD, RI — is relative to it.
	top, bot int

	// wrapNext is the deferred-wrap flag: set after printing into the
	// last column, consumed by the next printable rune.
	wrapNext bool
	// nowrap tracks DECAWM (`ESC[?7l`). Inverted so the zero value is
	// autowrap-on, which is what a terminal boots with.
	nowrap bool

	// saved cursor, for ESC 7 / ESC 8 and CSI s / CSI u
	sx, sy  int
	sStyle  Style
	hasSave bool

	// What the program running on this screen asked for, recorded so a
	// host can answer the question "may I forward a mouse report to it".
	mouseTrack, mouseSGR bool

	// DECTCEM (`ESC[?25l`). Inverted so the zero value is "visible",
	// which is what a terminal boots with.
	cursorHidden bool
}

// Cursor is where the program on this screen would put the caret, in
// cells relative to the screen's own origin.
//
// It exists because a host draws this model into ITS cell plane, and a
// cell plane has no cursor: gooey hides the real one for the whole
// frame (`term.Screen` emits `ESC[?25l` once at startup), so a guest's
// caret is invisible unless the host paints it. A shell with no caret
// does not read as "a shell waiting for input" — it reads as a frozen
// screenshot, which is exactly the wrong impression for a pane whose
// whole point is that the thing inside it is alive.
func (s *Screen) Cursor() (x, y int) { return s.x, s.y }

// CursorVisible reports DECTCEM. A full-screen program hides the caret
// while it repaints and shows it again when it is ready for a key, so
// honouring this is what makes a painted caret mean "your turn" rather
// than "here is where the last byte landed".
func (s *Screen) CursorVisible() bool { return !s.cursorHidden }

// MouseTracking reports whether the program has enabled mouse reporting
// (DECSET 1000/1002/1003).
//
// It exists for hosts. Forwarding a mouse report to a guest that never
// asked for one does not do nothing — the bytes arrive on its stdin and
// it types them into itself. Anything relaying a pointer has to gate on
// this, and this is the only place that knows.
func (s *Screen) MouseTracking() bool { return s.mouseTrack }

// MouseSGR reports whether the program asked for the SGR encoding
// (DECSET 1006) rather than the legacy X10 one. A host should send what
// was asked for; X10 cannot express coordinates past 223.
func (s *Screen) MouseSGR() bool { return s.mouseSGR }

func NewScreen(w, h int) *Screen {
	return &Screen{Buf: NewBuffer(w, h), top: 0, bot: h - 1}
}

// Resize re-targets the model, clearing it — the same thing a terminal
// does to a full-frame flush that follows a size change. The scrolling
// region goes with it: a region that outlived a resize would pin the
// bottom of the screen to a row that no longer exists.
func (s *Screen) Resize(w, h int) {
	s.Buf = NewBuffer(w, h)
	s.x, s.y = 0, 0
	s.top, s.bot = 0, h-1
	s.wrapNext = false
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
		s.x, s.wrapNext = 0, false
	case '\n', '\v', '\f':
		s.index()
		s.wrapNext = false
	case '\b':
		if s.x > 0 {
			s.x--
		}
		s.wrapNext = false
	case '\t':
		// Eight-column tab stops. Previously this fell through to the
		// default and wrote a literal tab INTO a cell, which is a glyph
		// the terminal then renders as who-knows-what — and a shell emits
		// tabs constantly.
		if n := (s.x/8 + 1) * 8; n < s.Buf.W {
			s.x = n
		} else {
			s.x = s.Buf.W - 1
		}
		s.wrapNext = false
	case 0x07: // BEL — audible, not visible
	default:
		if s.wrapNext && !s.nowrap {
			s.x = 0
			s.index()
		}
		s.wrapNext = false
		s.Buf.Set(s.x, s.y, r, s.cur)
		if s.x >= s.Buf.W-1 {
			// Deferred wrap: the cursor STAYS on the last column and a
			// flag is set. Moving now would scroll the screen every time
			// a frame filled a row to its edge.
			s.wrapNext = true
			return
		}
		s.x++
	}
}

// index moves down one row, scrolling the region when it is already at
// the bottom of it. This is what a line feed does, and what makes a
// hosted shell scroll instead of overwriting its last line forever.
func (s *Screen) index() {
	if s.y == s.bot {
		s.scrollUp(1)
		return
	}
	if s.y < s.Buf.H-1 {
		s.y++
	}
}

// reverseIndex is index's mirror: up one, scrolling the region down when
// already at its top. Editors use it to open a line above the viewport.
func (s *Screen) reverseIndex() {
	if s.y == s.top {
		s.scrollDown(1)
		return
	}
	if s.y > 0 {
		s.y--
	}
}

// scrollUp moves the region's rows up by n and blanks the n rows it
// vacates at the bottom.
func (s *Screen) scrollUp(n int) { s.scrollRegion(s.top, n) }

// scrollDown is the same in the other direction.
func (s *Screen) scrollDown(n int) { s.scrollRegion(s.top, -n) }

// scrollRegion shifts rows [from, bot] by n (positive = up). It is one
// function rather than two because the only difference is the direction
// of the copy, and two copies of this loop is two places to get the
// off-by-one wrong.
func (s *Screen) scrollRegion(from, n int) {
	if n == 0 || from > s.bot {
		return
	}
	w, blank := s.Buf.W, s.blank()
	rows := s.bot - from + 1
	if n >= rows || -n >= rows {
		for y := from; y <= s.bot; y++ {
			for x := 0; x < w; x++ {
				s.Buf.Cells[y*w+x] = blank
			}
		}
		return
	}
	if n > 0 {
		for y := from; y <= s.bot-n; y++ {
			copy(s.Buf.Cells[y*w:(y+1)*w], s.Buf.Cells[(y+n)*w:(y+n+1)*w])
		}
		for y := s.bot - n + 1; y <= s.bot; y++ {
			for x := 0; x < w; x++ {
				s.Buf.Cells[y*w+x] = blank
			}
		}
		return
	}
	n = -n
	for y := s.bot; y >= from+n; y-- {
		copy(s.Buf.Cells[y*w:(y+1)*w], s.Buf.Cells[(y-n)*w:(y-n+1)*w])
	}
	for y := from; y < from+n && y <= s.bot; y++ {
		for x := 0; x < w; x++ {
			s.Buf.Cells[y*w+x] = blank
		}
	}
}

// insertChars shifts the rest of the row right, blanking n cells at the
// cursor. This and deleteChars are how an editor edits a line without
// redrawing it, and their absence is what spliced two lines into one.
func (s *Screen) insertChars(n int) {
	w := s.Buf.W
	if s.y < 0 || s.y >= s.Buf.H || n <= 0 {
		return
	}
	row := s.Buf.Cells[s.y*w : (s.y+1)*w]
	if s.x >= w {
		return
	}
	if n > w-s.x {
		n = w - s.x
	}
	copy(row[s.x+n:], row[s.x:w-n])
	blank := s.blank()
	for i := s.x; i < s.x+n; i++ {
		row[i] = blank
	}
}

func (s *Screen) deleteChars(n int) {
	w := s.Buf.W
	if s.y < 0 || s.y >= s.Buf.H || n <= 0 {
		return
	}
	row := s.Buf.Cells[s.y*w : (s.y+1)*w]
	if s.x >= w {
		return
	}
	if n > w-s.x {
		n = w - s.x
	}
	copy(row[s.x:], row[s.x+n:])
	blank := s.blank()
	for i := w - n; i < w; i++ {
		row[i] = blank
	}
}

func (s *Screen) eraseChars(n int) {
	blank := s.blank()
	for i := 0; i < n; i++ {
		s.Buf.Set(s.x+i, s.y, blank.Rune, blank.Style)
	}
}

// blank is the cell every erase, scroll and shift leaves behind, and it
// is a method rather than a `Cell{Rune: ' '}` literal because there were
// six sites making one and they did not agree: ICH and DCH used the
// current style, ED, EL, ECH and scrollRegion used the zero one. That
// disagreement IS the bug — back-color-erase is what makes `ESC[44m`
// followed by `ESC[K` paint a blue status bar to the end of the line,
// and with the zero style the bar came out unpainted while the same
// program's line edits came out blue.
//
// It is not simply `s.cur`. A blank cell shows its background, and under
// SGR 7 that background is the foreground — so Fg, Bg and Reverse all
// have to survive. Bold and Dim are invisible on a space, and Underline
// is worse than invisible: carrying it would draw a rule across every
// region an underlining program erased. xterm's BCE drops them and so
// does this.
func (s *Screen) blank() Cell {
	return Cell{Rune: ' ', Style: Style{
		Fg:      s.cur.Fg,
		Bg:      s.cur.Bg,
		Reverse: s.cur.Reverse,
	}}
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
	case '(', ')', '*', '+', '#', ' ':
		// THREE-byte escapes: charset designation (ESC ( B selects
		// ASCII as G0), DECALN (ESC # 8), and the C1-control select
		// (ESC SP F). None of them change the cell plane, but they must
		// still be CONSUMED — top emits ESC ( B constantly, and a
		// two-byte consume leaves the B to be printed as text. That is
		// what a screen full of stray Bs is.
		if len(b) < 3 {
			return 0, false
		}
		return 3, true
	case '\\': // a stray string terminator
		return 2, true
	case 'D': // IND — index
		s.index()
		return 2, true
	case 'M': // RI — reverse index. An editor scrolling back a line uses
		// this and nothing else; swallowed as a no-op it simply redraws
		// the same row twice.
		s.reverseIndex()
		return 2, true
	case 'E': // NEL — next line
		s.x = 0
		s.index()
		return 2, true
	case '7': // DECSC
		s.saveCursor()
		return 2, true
	case '8': // DECRC
		s.restoreCursor()
		return 2, true
	case 'c': // RIS — full reset
		s.Buf.Clear()
		s.x, s.y = 0, 0
		s.top, s.bot = 0, s.Buf.H-1
		s.cur, s.wrapNext, s.nowrap = Style{}, false, false
		s.cursorHidden = false
		return 2, true
	default:
		return 2, true // two-byte escape we do not model
	}
}

func (s *Screen) csi(body string, final byte) {
	if strings.HasPrefix(body, "?") {
		// Private modes. Two of them reach the cell plane, and the modes
		// are PARSED rather than substring-matched: `strings.Contains(body,
		// "7")` is true of ?47, ?1007 and ?27 as well as ?7, and a mode
		// switch that fires on the wrong number turns wrapping on and off
		// at random.
		if final != 'h' && final != 'l' {
			return
		}
		for _, m := range csiArgs(strings.TrimPrefix(body, "?")) {
			switch m {
			case 47, 1047, 1049:
				// The alternate screen arrives blank, which is why a host
				// invalidates its flush after re-acquiring the terminal.
				s.Buf.Clear()
				s.x, s.y = 0, 0
				s.wrapNext = false
			case 7:
				// DECAWM. A shell turns wrap off around its prompt redraw
				// and back on after it; honouring it is the difference
				// between a long command line wrapping and overwriting its
				// own last column.
				s.nowrap = final == 'l'
			case 1000, 1002, 1003:
				// Mouse TRACKING. Recorded rather than acted on: nothing
				// here draws a pointer. It is here because a host that
				// forwards mouse reports to a guest has to know whether
				// the guest asked for any — a program that did not will
				// read `ESC[<0;5;5M` as keystrokes and type it into
				// itself. MouseTracking is the only honest gate for that.
				s.mouseTrack = final == 'h'
			case 1006:
				s.mouseSGR = final == 'h'
			case 25:
				// DECTCEM. Recorded for the same reason as mouse
				// tracking: this model has no caret of its own, and the
				// host that draws it needs to know whether the guest
				// wanted one right now.
				s.cursorHidden = final == 'l'
			}
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
	// Any explicit cursor motion cancels a pending wrap. The list is not
	// "the cursor commands" — it is every final below that assigns s.x or
	// s.y, and the three easy ones to forget are at the end: IL and DL
	// home the column, and DECSTBM homes both. Leaving a stale wrapNext
	// after one of those is the same defect deferred wrap exists to
	// prevent, just reached by a different sequence: fill a row edge to
	// edge, set a scrolling region, and the next printed character
	// scrolls the screen out from under the program.
	switch final {
	case 'H', 'f', 'A', 'B', 'C', 'D', 'E', 'F', 'G', '`', 'd', 'u', 'L', 'M', 'r':
		s.wrapNext = false
	}

	switch final {
	case 'H', 'f': // CUP
		s.y, s.x = arg(0, 1)-1, arg(1, 1)-1
	case 'A': // CUU
		s.y -= arg(0, 1)
	case 'B': // CUD
		s.y += arg(0, 1)
	case 'C': // CUF
		s.x += arg(0, 1)
	case 'D': // CUB
		s.x -= arg(0, 1)
	case 'E': // CNL
		s.y += arg(0, 1)
		s.x = 0
	case 'F': // CPL
		s.y -= arg(0, 1)
		s.x = 0
	case 'G', '`': // CHA / HPA — absolute column
		s.x = arg(0, 1) - 1
	case 'd': // VPA — absolute row
		s.y = arg(0, 1) - 1
	case 'J':
		s.erase(arg(0, 0))
	case 'K':
		s.eraseLine(arg(0, 0))
	case '@': // ICH
		s.insertChars(arg(0, 1))
	case 'P': // DCH
		s.deleteChars(arg(0, 1))
	case 'X': // ECH
		s.eraseChars(arg(0, 1))
	case 'L': // IL — open n lines AT the cursor, pushing the rest down
		if s.y >= s.top && s.y <= s.bot {
			s.scrollRegion(s.y, -arg(0, 1))
			s.x = 0
		}
	case 'M': // DL — remove n lines at the cursor, pulling the rest up
		if s.y >= s.top && s.y <= s.bot {
			s.scrollRegion(s.y, arg(0, 1))
			s.x = 0
		}
	case 'S': // SU
		s.scrollUp(arg(0, 1))
	case 'T': // SD
		s.scrollDown(arg(0, 1))
	case 'r': // DECSTBM
		t, b := arg(0, 1)-1, arg(1, s.Buf.H)-1
		if t < 0 {
			t = 0
		}
		if b >= s.Buf.H {
			b = s.Buf.H - 1
		}
		if t < b {
			s.top, s.bot = t, b
		} else {
			s.top, s.bot = 0, s.Buf.H-1
		}
		// DECSTBM homes the cursor. Programs rely on that — omit it and
		// the first line an editor draws after setting its region lands
		// wherever the cursor happened to be.
		s.x, s.y = 0, s.top
	case 's': // save cursor (ANSI.SYS form)
		s.saveCursor()
	case 'u': // restore cursor
		s.restoreCursor()
	case 'm':
		s.sgr(args, body)
	}
	s.x, s.y = max(0, s.x), max(0, s.y)
	if s.x >= s.Buf.W {
		s.x = s.Buf.W - 1
	}
	if s.y >= s.Buf.H {
		s.y = s.Buf.H - 1
	}
}

func (s *Screen) saveCursor() {
	s.sx, s.sy, s.sStyle, s.hasSave = s.x, s.y, s.cur, true
}

func (s *Screen) restoreCursor() {
	if !s.hasSave {
		s.x, s.y = 0, 0
		return
	}
	s.x, s.y, s.cur = s.sx, s.sy, s.sStyle
}

func (s *Screen) erase(mode int) {
	from, to := 0, s.Buf.W*s.Buf.H
	switch mode {
	case 0:
		from = s.y*s.Buf.W + s.x
	case 1:
		to = s.y*s.Buf.W + s.x + 1
	}
	blank := s.blank()
	for i := max(0, from); i < min(to, len(s.Buf.Cells)); i++ {
		s.Buf.Cells[i] = blank
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
	blank := s.blank()
	for x := from; x < to; x++ {
		s.Buf.Set(x, s.y, blank.Rune, blank.Style)
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
		case 2:
			s.cur.Dim = true
		case 4:
			s.cur.Underline = true
		case 7:
			s.cur.Reverse = true
		case 22:
			s.cur.Bold, s.cur.Dim = false, false
		case 24:
			s.cur.Underline = false
		case 27:
			s.cur.Reverse = false
		case 38, 48:
			c, used := parseColor(args[i+1:])
			if args[i] == 38 {
				s.cur.Fg = c
			} else {
				s.cur.Bg = c
			}
			i += used
		case 39:
			s.cur.Fg = Color{}
		case 49:
			s.cur.Bg = Color{}
		default:
			// The indexed forms. These were unhandled while Screen was
			// only ever a test model — "nothing asks a model screen what
			// shade a cell is" — but the model is a display now: a hosted
			// guest's cells get blitted into a frame, and an unhandled
			// SGR 32 is a green word painted in the previous color.
			switch n := args[i]; {
			case n >= 30 && n <= 37:
				s.cur.Fg = palette256(n - 30)
			case n >= 40 && n <= 47:
				s.cur.Bg = palette256(n - 40)
			case n >= 90 && n <= 97:
				s.cur.Fg = palette256(n - 90 + 8)
			case n >= 100 && n <= 107:
				s.cur.Bg = palette256(n - 100 + 8)
			}
		}
	}
}

// parseColor reads an extended color parameter and reports how many
// arguments it consumed.
//
// The 256-color form is expanded through palette256 rather than stashed
// as an index. It used to be stashed — Color{R: uint8(idx)} — on the
// grounds that nothing asks a model screen what shade a cell is. Once
// those cells are blitted into a real frame, something does: index 4 is
// blue, and R=4,G=0,B=0 is black.
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
		return palette256(clamp255(rest[1])), 2
	}
	return Color{Set: true}, 1
}

func clamp255(n int) int {
	if n < 0 {
		return 0
	}
	if n > 255 {
		return 255
	}
	return n
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
