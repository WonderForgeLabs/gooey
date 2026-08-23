// Package term is the thin swappable floor: raw mode, screen setup, and
// capability detection (which graphics protocol, cell pixel size).
package term

import (
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/WonderForgeLabs/gooey/input"
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

// The cell size assumed when nothing can measure the real one — a probe
// that timed out, a host that pinned a protocol without capabilities.
// Only sixel scales by it (kitty and iTerm2 size in cells), and a wrong
// guess there costs aspect ratio; a ZERO there costs the whole image, so
// no path that yields a pixel protocol may leave the cell size at zero.
const (
	DefaultCellW = 10
	DefaultCellH = 20
)

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

	// Decoder ownership. A Screen that started a decoder must be able to
	// prove it died before teardown returns — see Events and Restore.
	evs       chan input.Event
	decDone   chan struct{}
	decLeaked bool

	// decErr is the read error that stopped the decoder. Nil only while
	// one is still running (or was never started); a decoder has exactly
	// one exit path and it always records a non-nil error, teardown
	// included. Written by the decoder goroutine and read by whoever is
	// watching DecoderDone; see DecoderErr for the ordering that makes
	// that safe without a lock.
	decErr error
}

func Open() (*Screen, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	return FromFile(tty), nil
}

// FromFile wraps an already-open terminal file. It is the seam for hosts
// that obtained their tty some other way and for tests, which drive a
// Screen over a pty slave instead of /dev/tty.
func FromFile(f *os.File) *Screen { return &Screen{tty: f} }

func (s *Screen) File() *os.File { return s.tty }

// control runs fn with the tty's file descriptor WITHOUT detaching the
// file from the runtime poller.
//
// This is the whole fix for the tty read lifecycle. os.File.Fd() puts the
// file back into blocking mode and unregisters it from the netpoller; a
// Read pending on such a file becomes an uninterruptible syscall that
// Close cannot cancel, so every decoder goroutine outlived its Screen and
// sat on the terminal forever (docs/specs/2026-08-10-tty-read-lifecycle.md).
// SyscallConn().Control hands the same fd to the ioctl without any of
// that, which is what makes Close a reliable cancellation and lets
// Restore JOIN its reader instead of abandoning it.
//
// It also restores read deadlines: SetReadDeadline fails with
// ErrNoDeadline on a detached file, so Detect's timeout silently
// degraded to a single blocking read the moment anything called Fd().
func (s *Screen) control(fn func(fd int) error) error {
	c, err := s.tty.SyscallConn()
	if err != nil {
		return err
	}
	var inner error
	if err := c.Control(func(fd uintptr) { inner = fn(int(fd)) }); err != nil {
		return err
	}
	return inner
}

// SizeOK reports the terminal's size and whether the terminal could
// answer at all. Size is the same query with the historical 80x24
// fallback already applied, which throws that distinction away — and a
// host carrying its own declared size needs it, because "the terminal
// says 80x24" and "there is no terminal to ask" call for different
// answers.
func (s *Screen) SizeOK() (cols, rows int, ok bool) {
	var c, r int
	err := s.control(func(fd int) error {
		var e error
		c, r, e = term.GetSize(fd)
		return e
	})
	if err != nil || c <= 0 || r <= 0 {
		return 0, 0, false
	}
	return c, r, true
}

func (s *Screen) Size() (cols, rows int) {
	if c, r, ok := s.SizeOK(); ok {
		return c, r
	}
	return 80, 24
}

// Raw enters raw mode + alternate screen with hidden cursor.
func (s *Screen) Raw() error {
	err := s.control(func(fd int) error {
		st, e := term.MakeRaw(fd)
		if e != nil {
			return e
		}
		s.oldState = st
		return nil
	})
	if err != nil {
		return err
	}
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

// Events starts the input decoder on this Screen's tty and returns the
// channel it delivers on. The Screen owns the goroutine from here: it is
// stopped and JOINED by Restore.
//
// Calling it twice returns the same channel — a Screen has one decoder,
// because two readers on one tty split the keystrokes between them.
func (s *Screen) Events(buf int) <-chan input.Event {
	if s.evs != nil {
		return s.evs
	}
	s.evs = make(chan input.Event, buf)
	s.decDone = make(chan struct{})
	// Passed in rather than captured: Restore nils these out, and a
	// goroutine reading them from the struct would be reading whichever
	// session happened to be current.
	go func(out chan<- input.Event, done chan struct{}) {
		defer close(done)
		DecodeEvents(s, out)
	}(s.evs, s.decDone)
	return s.evs
}

// DecoderTimeout bounds how long teardown waits for the decoder to die.
// Reaching it means the invariant below was violated and something is
// still on the terminal; the caller is expected to say so out loud.
const DecoderTimeout = 2 * time.Second

// joinDecoder closes nothing — the caller has already closed the tty,
// which is what unblocks the read — and waits for the decoder goroutine
// to actually exit.
//
// It drains events while waiting, for two reasons: the decoder flushes
// whatever it had buffered on the way out and would block forever
// sending into an unread channel, and those pre-teardown keystrokes are
// exactly the stale input that must never be replayed into a resumed UI.
func (s *Screen) joinDecoder() bool {
	if s.decDone == nil {
		return true
	}
	deadline := time.After(DecoderTimeout)
	for {
		select {
		case <-s.decDone:
			s.evs, s.decDone = nil, nil
			return true
		case <-s.evs:
		case <-deadline:
			s.evs, s.decDone = nil, nil
			return false
		}
	}
}

// Restore leaves the alternate screen, restores the tty state, closes the
// tty and waits for the decoder to die.
//
// It disables mouse reporting unconditionally — leaving a terminal in
// tracking mode after exit is the one unrecoverable mistake here.
//
// The ORDER is the contract. Escapes and termios state have to be put
// back while the fd is still open; Close comes next and is what cancels
// the decoder's pending Read; the join is last. That establishes the
// invariant this package now guarantees:
//
//	no Screen teardown leaves a goroutine reading the terminal.
//
// It is what makes handing the terminal to a child process (and getting
// it back) safe: nothing of ours is still on the tty when the child
// starts. DecoderLeaked reports the failure case for callers that want a
// tripwire.
func (s *Screen) Restore() {
	s.DisableMouse()
	fmt.Fprint(s.tty, "\x1b[?25h\x1b[?1049l\x1b[0m")
	if s.oldState != nil {
		s.control(func(fd int) error { return term.Restore(fd, s.oldState) })
		s.oldState = nil
	}
	s.tty.Close()
	s.decLeaked = !s.joinDecoder()
}

// DecoderLeaked reports whether the last Restore timed out waiting for
// the input decoder to exit. False after a clean teardown, and after a
// teardown of a Screen that never started one.
func (s *Screen) DecoderLeaked() bool { return s.decLeaked }

// DecoderDone is closed when the input decoder goroutine has exited, and
// is the tripwire for the failure DecoderLeaked cannot see.
//
// The two are duals. DecoderLeaked asks "did the decoder outlive the
// terminal?" and is checked at teardown. This asks "did the terminal
// outlive the decoder?" and has to be watched WHILE THE APP RUNS,
// because nothing else reveals it: DecodeEvents does not close the
// events channel on its way out, so a run loop selecting on that channel
// simply blocks on it forever. The app stays alive and keeps painting —
// every frame correct, every keystroke ignored.
//
// Nil for a Screen with no decoder, and nil again after Restore has
// joined one. A receive on a nil channel blocks forever, which is the
// right behaviour in a select for both cases.
func (s *Screen) DecoderDone() <-chan struct{} { return s.decDone }

// DecoderErr is the read error that stopped the decoder. Once
// DecoderDone has fired it is ALWAYS non-nil — the decoder's single exit
// path records the failing read before it closes the chunk channel that
// lets DecodeEvents return, so there is no way to reach a fired
// DecoderDone with a nil error here.
//
// That includes a clean teardown, which is not a special case but the
// ordinary one: Restore closes the tty precisely to cancel the pending
// read, and the read reports that. So the discriminator is the KIND of
// error, not its presence — errors.Is(err, os.ErrClosed) is the teardown
// this Screen asked for, and anything else is a terminal that failed
// under a decoder nobody told to stop.
//
// Nil, therefore, means only that no decoder has exited: none was ever
// started, or one is still running and this was read too early.
//
// Read it only after DecoderDone has fired. That is not a lock-free
// hopeful convention but an ordering the decoder establishes: it assigns
// the field before closing its chunk channel, which precedes
// DecodeEvents returning, which precedes the close of decDone. Observing
// that close therefore happens-after the write, so the plain read is
// ordered — and the -race tier proves it, since a caller who reads this
// without waiting is a genuine data race the detector will name.
func (s *Screen) DecoderErr() error { return s.decErr }

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

	var st *term.State
	err := s.control(func(fd int) error {
		var e error
		st, e = term.MakeRaw(fd)
		return e
	})
	if err != nil {
		return caps, err
	}
	defer s.control(func(fd int) error { return term.Restore(fd, st) })

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
		caps.CellW, caps.CellH = DefaultCellW, DefaultCellH
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
// miss a slow reply) but still incapable of stealing later input. That
// fallback used to be the NORMAL path rather than the exception: the
// ioctls went through os.File.Fd(), which unregisters the file from the
// poller, and SetReadDeadline on a detached file always fails. Routing
// them through control() keeps the deadline working.
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
