package gooey

import (
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/term"
)

// blankWinsize sets the pty's window size WITHOUT touching testTTY's
// model screen, which setSize does. A zero winsize is how a terminal
// reports "I cannot tell you my size": term.Screen.Size treats a
// non-positive answer exactly like a failed ioctl (term/term.go).
func blankWinsize(t *testing.T, f *os.File) {
	t.Helper()
	ws := struct{ rows, cols, xpix, ypix uint16 }{}
	if err := ttyIoctl(f, syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws))); err != nil {
		t.Fatalf("TIOCSWINSZ: %v", err)
	}
}

// size reads App.Size on the UI goroutine, which is the only legal place
// to read it.
func size(t *testing.T, app *App) [2]int {
	t.Helper()
	ch := make(chan [2]int, 1)
	app.Post(func() { c, r := app.Size(); ch <- [2]int{c, r} })
	select {
	case got := <-ch:
		return got
	case <-time.After(3 * time.Second):
		t.Fatal("the UI goroutine did not answer")
		return [2]int{}
	}
}

// A host whose terminal cannot report a size gets the size it declared,
// not the 80x24 constant buried in term.Screen.Size. This is the whole
// reason WithSize exists: a headless or non-tty backend has no ioctl to
// ask, and before this option there was NO way to tell the app how big
// it was.
func TestWithSizeSuppliesTheSizeWhenTheTerminalCannotReportOne(t *testing.T) {
	tty := newTestTTY(t)
	blankWinsize(t, tty.master)

	app := NewApp(Tree(&label{text: prop.NewSource("hi")}),
		WithTerminal(tty.open), WithSize(120, 40))
	start(t, app)
	tty.waitForFrame(t)

	if got := size(t, app); got != [2]int{120, 40} {
		t.Fatalf("size is %v, want 120x40 — the declared size was discarded", got)
	}
}

// WithSize is a FALLBACK, not an override: a terminal that can answer is
// authoritative, because the alternative is an app that paints 120
// columns into a 40-column window and blames the user.
func TestATerminalThatKnowsItsSizeBeatsWithSize(t *testing.T) {
	tty := newTestTTY(t) // newTestTTY sizes the pty 40x10

	app := NewApp(Tree(&label{text: prop.NewSource("hi")}),
		WithTerminal(tty.open), WithSize(120, 40))
	start(t, app)
	tty.waitForFrame(t)

	if got := size(t, app); got != [2]int{40, 10} {
		t.Fatalf("size is %v, want 40x10 — WithSize overrode a working ioctl", got)
	}
}

// term.Caps carries Cols and Rows, so WithCaps LOOKS like it sets the
// size. It does not, and it never did — App.caps overwrites both fields
// from the app's own size on every call (app.go). Pinning the deliberate
// behavior so the trap is documented rather than rediscovered: capability
// detection describes what the terminal can DO, geometry comes from
// WithSize or the ioctl.
func TestWithCapsDoesNotSetTheSize(t *testing.T) {
	tty := newTestTTY(t)
	blankWinsize(t, tty.master)

	app := NewApp(Tree(&label{text: prop.NewSource("hi")}),
		WithTerminal(tty.open), WithCaps(term.Caps{Cols: 200, Rows: 60}))
	start(t, app)
	tty.waitForFrame(t)

	// Assert on the caps the COMPOSER carries, not on App.Size: Size
	// reports a.cols/a.rows, which App.caps never writes to, so it cannot
	// witness geometry leaking out of WithCaps. Frame.Caps is what every
	// component's Render actually sees.
	ch := make(chan term.Caps, 1)
	app.Post(func() { ch <- app.Composer().Caps() })
	got := <-ch

	// 80x24 is term.Screen.Size's constant: no ioctl, no WithSize.
	if got.Cols != 80 || got.Rows != 24 {
		t.Fatalf("frame caps are %dx%d, want 80x24 — WithCaps must not carry geometry",
			got.Cols, got.Rows)
	}
}
