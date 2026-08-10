// Package term is the thin swappable floor: raw mode, screen setup, and
// capability detection (which graphics protocol, cell pixel size).
package term

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/WonderForgeLabs/gooey/render"
)

type Caps struct {
	Kitty  bool
	Sixel  bool
	ITerm2 bool
	// Color is how many colors the cell plane can show. Its zero value
	// is render.TrueColor, so a zero Caps keeps the pre-detection
	// behavior rather than silently degrading a terminal that was never
	// probed.
	Color render.ColorDepth
	// Cell size in pixels (for sixel scaling). Zero if unknown.
	CellW, CellH int
	Cols, Rows   int
}

// Best returns the preferred graphics protocol name, most capable first.
func (c Caps) Best() string {
	switch {
	case c.Kitty:
		return "kitty"
	case c.Sixel:
		return "sixel"
	case c.ITerm2:
		return "iterm2"
	default:
		return "halfblock"
	}
}

type Screen struct {
	tty      *os.File
	oldState *term.State
}

func Open() (*Screen, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return &Screen{tty: tty}, nil
}

func (s *Screen) File() *os.File { return s.tty }

func (s *Screen) Size() (cols, rows int) {
	cols, rows, err := term.GetSize(int(s.tty.Fd()))
	if err != nil || cols <= 0 || rows <= 0 {
		return 80, 24
	}
	return cols, rows
}

// Raw enters raw mode + alternate screen with hidden cursor.
func (s *Screen) Raw() error {
	st, err := term.MakeRaw(int(s.tty.Fd()))
	if err != nil {
		return err
	}
	s.oldState = st
	fmt.Fprint(s.tty, "\x1b[?1049h\x1b[?25l") // alt screen, hide cursor
	return nil
}

// EnableMouse turns on SGR mouse reporting: button events (1000),
// any-motion tracking so hover works without a button held (1003), and
// the SGR extended encoding (1006), which is the only one that survives
// past column 223 and distinguishes press from release.
//
// It is opt-in rather than part of Raw because motion reports are just
// bytes on the tty: an app that treats any byte as a keypress would exit
// when the pointer moves. Apps that decode with input.Decode call this;
// the rest are unaffected.
func (s *Screen) EnableMouse() {
	fmt.Fprint(s.tty, "\x1b[?1000h\x1b[?1003h\x1b[?1006h")
}

func (s *Screen) DisableMouse() {
	fmt.Fprint(s.tty, "\x1b[?1006l\x1b[?1003l\x1b[?1000l")
}

// Restore leaves the alternate screen and restores the tty state. It
// disables mouse reporting unconditionally — leaving a terminal in
// tracking mode after exit is the one unrecoverable mistake here.
func (s *Screen) Restore() {
	s.DisableMouse()
	fmt.Fprint(s.tty, "\x1b[?25h\x1b[?1049l\x1b[0m")
	if s.oldState != nil {
		term.Restore(int(s.tty.Fd()), s.oldState)
	}
	s.tty.Close()
}

// Detect probes the terminal for graphics capabilities. It must run on a
// real tty. Strategy: send a Kitty graphics query, a cell-size query
// (XTWINOPS 16), and DA1 — terminals answer DA1 unconditionally, so it
// acts as the terminator; anything that arrived before it is parsed.
func (s *Screen) Detect() (Caps, error) {
	caps := Caps{}
	caps.Cols, caps.Rows = s.Size()

	// Env-only signals (no query protocol exists for iTerm2 images).
	tp := os.Getenv("TERM_PROGRAM")
	if tp == "iTerm.app" || tp == "WezTerm" || os.Getenv("LC_TERMINAL") == "iTerm2" {
		caps.ITerm2 = true
	}

	st, err := term.MakeRaw(int(s.tty.Fd()))
	if err != nil {
		return caps, err
	}
	defer term.Restore(int(s.tty.Fd()), st)

	// Kitty query (tiny 1×1 RGB transmit, q=1 → responds if supported),
	// then cell size, then DA1 terminator.
	fmt.Fprint(s.tty, "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\")
	fmt.Fprint(s.tty, "\x1b[16t")
	fmt.Fprint(s.tty, xtgettcapQuery)
	fmt.Fprint(s.tty, "\x1b[c")

	resp := s.readUntilDA1(500 * time.Millisecond)

	// The color ladder, now with the terminal's own answer available.
	// Terminals that ignore XTGETTCAP simply leave it unknown and the
	// environment rungs decide.
	caps.Color = colorDepthFrom(osEnv, parseXTGETTCAP(resp))

	if strings.Contains(resp, "\x1b_G") && strings.Contains(resp, "i=31") {
		caps.Kitty = true
	}
	// XTWINOPS 16 reply: CSI 6 ; height ; width t
	var ch, cw int
	if i := strings.Index(resp, "\x1b[6;"); i >= 0 {
		if n, _ := fmt.Sscanf(resp[i:], "\x1b[6;%d;%dt", &ch, &cw); n == 2 {
			caps.CellH, caps.CellW = ch, cw
		}
	}
	// DA1 reply: CSI ? attr ; attr ... c — attribute 4 means sixel.
	if i := strings.Index(resp, "\x1b[?"); i >= 0 {
		body := resp[i+3:]
		if j := strings.IndexByte(body, 'c'); j >= 0 {
			for _, a := range strings.Split(body[:j], ";") {
				if a == "4" {
					caps.Sixel = true
				}
			}
		}
	}
	if caps.CellW == 0 {
		caps.CellW, caps.CellH = 10, 20 // common default; only affects sixel scaling
	}
	return caps, nil
}

// readUntilDA1 collects responses until the DA1 reply or timeout.
//
// It reads SYNCHRONOUSLY under a read deadline. The obvious alternative —
// a reader goroutine plus a select on a timer — leaks: when the timeout
// fires (or DA1 arrives and the probe returns) that goroutine is still
// blocked in Read, and it stays there for the life of the process,
// swallowing the bytes the app's own input decoder was waiting for. The
// symptom is a keyboard-driven app quietly losing its first keystrokes
// after startup, which is exactly what it looked like: a probe that
// worked, followed by input that did not.
//
// Deadlines are not guaranteed on every character device, so an
// ErrNoDeadline falls back to a single bounded read — degraded (it may
// miss a slow reply) but still incapable of stealing later input.
func (s *Screen) readUntilDA1(timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	if err := s.tty.SetReadDeadline(deadline); err != nil {
		return s.readOnceBounded()
	}
	// Always clear the deadline: the same fd carries the app's input for
	// the rest of the session.
	defer s.tty.SetReadDeadline(time.Time{})

	var sb strings.Builder
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, err := s.tty.Read(buf)
		if n > 0 {
			sb.Write(buf[:n])
			// DA1 response ends with 'c' following CSI ?.
			if i := strings.LastIndex(sb.String(), "\x1b[?"); i >= 0 &&
				strings.IndexByte(sb.String()[i:], 'c') >= 0 {
				return sb.String()
			}
		}
		if err != nil {
			break // timeout or read error: take what arrived
		}
	}
	return sb.String()
}

// readOnceBounded is the fallback for ttys without deadline support: one
// read, which blocks until the terminal says something. Terminals answer
// DA1 unconditionally, so in practice this returns.
func (s *Screen) readOnceBounded() string {
	buf := make([]byte, 256)
	n, err := s.tty.Read(buf)
	if n <= 0 || err != nil {
		return ""
	}
	return string(buf[:n])
}
