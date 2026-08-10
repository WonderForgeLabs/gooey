package gooey

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// testTTY is a pty the framework's own tests run a real App on: frames
// arrive on the master side as the escape sequences a terminal would
// receive, and keystrokes go back the same way. No third-party pty
// dependency — the three ioctls every such library wraps.
type testTTY struct {
	master *os.File
	name   string

	mu    sync.Mutex
	buf   strings.Builder
	scr   *render.Screen
	opens int
}

func newTestTTY(t *testing.T) *testTTY {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	var unlock int32
	if err := ttyIoctl(m, syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); err != nil {
		m.Close()
		t.Skipf("TIOCSPTLCK: %v", err)
	}
	var n int32
	if err := ttyIoctl(m, syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); err != nil {
		m.Close()
		t.Skipf("TIOCGPTN: %v", err)
	}
	tt := &testTTY{master: m, name: "/dev/pts/" + itoa(int(n)), scr: render.NewScreen(40, 10)}
	tt.setSize(t, 40, 10)
	// A slave handle held open for the whole test, opened and closed by
	// nobody. Linux gives the master EIO once the LAST slave fd closes,
	// and teardown closes the app's — so without this the pty would go
	// dead the instant the app suspended, and the resumed UI would have
	// nowhere to paint. A real terminal emulator is this keeper.
	keeper, err := os.OpenFile(tt.name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		m.Close()
		t.Skipf("open pts keeper: %v", err)
	}
	t.Cleanup(func() { keeper.Close() })
	// Drain continuously: a pty master whose buffer fills would block the
	// app's flush, and the test needs the bytes anyway.
	go func() {
		b := make([]byte, 4096)
		for {
			k, err := m.Read(b)
			if k > 0 {
				tt.mu.Lock()
				tt.buf.Write(b[:k])
				tt.scr.Write(b[:k])
				tt.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { m.Close() })
	return tt
}

// open is the WithTerminal hook: a FRESH slave handle every time, since
// teardown closes the one it was given. Counting the calls is how the
// suspend test proves the terminal was really re-acquired.
func (tt *testTTY) open() (*term.Screen, error) {
	f, err := os.OpenFile(tt.name, os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, err
	}
	tt.mu.Lock()
	tt.opens++
	tt.mu.Unlock()
	return term.FromFile(f), nil
}

func (tt *testTTY) send(s string) { tt.master.WriteString(s) }

func (tt *testTTY) text() string {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.buf.String()
}

// screen is what a terminal fed these bytes would be showing.
func (tt *testTTY) screen() string {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.scr.Text()
}

func (tt *testTTY) reset() {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	tt.buf.Reset()
}

func (tt *testTTY) openCount() int {
	tt.mu.Lock()
	defer tt.mu.Unlock()
	return tt.opens
}

// waitFor polls for text on the modelled SCREEN. Frames are asynchronous
// by construction — the loop decides when to paint — so tests wait for
// the screen to say something rather than for a tick.
//
// It models the terminal instead of grepping the byte stream because the
// flush is incremental: changing "n=2" to "n=3" puts a single 3 on the
// wire, so the phrase a test is looking for exists only once the bytes
// have been applied to what was there before.
func (tt *testTTY) waitFor(t *testing.T, want string) bool {
	t.Helper()
	return tt.poll(func() bool { return strings.Contains(tt.screen(), want) })
}

// waitForBytes polls the raw stream. For assertions that ARE about the
// escape sequences — leaving the alternate screen, disabling mouse
// reporting — which by definition leave no mark on the screen.
func (tt *testTTY) waitForBytes(t *testing.T, want string) bool {
	t.Helper()
	return tt.poll(func() bool { return strings.Contains(tt.text(), want) })
}

func (tt *testTTY) poll(ok func() bool) bool {
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// waitForFrame waits for the app to paint anything at all: the cursor
// home that starts every full flush, and the first flush is always full.
func (tt *testTTY) waitForFrame(t *testing.T) {
	t.Helper()
	if !tt.waitForBytes(t, "\x1b[H") {
		t.Fatal("the app never painted a frame")
	}
}

func (tt *testTTY) setSize(t *testing.T, cols, rows int) {
	t.Helper()
	ws := struct{ rows, cols, xpix, ypix uint16 }{rows: uint16(rows), cols: uint16(cols)}
	if err := ttyIoctl(tt.master, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil {
		t.Fatalf("TIOCSWINSZ: %v", err)
	}
}

// ttyIoctl goes through SyscallConn rather than Fd for the reason the
// whole tty lifecycle now does: Fd detaches the file from the poller.
func ttyIoctl(f *os.File, req, arg uintptr) error {
	c, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var errno syscall.Errno
	if cerr := c.Control(func(fd uintptr) {
		_, _, errno = syscall.Syscall(syscall.SYS_IOCTL, fd, req, arg)
	}); cerr != nil {
		return cerr
	}
	if errno != 0 {
		return errno
	}
	return nil
}
