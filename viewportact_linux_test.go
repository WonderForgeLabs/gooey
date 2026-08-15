package gooey

import (
	"testing"

	"github.com/WonderForgeLabs/gooey/internal/viewport"
	"github.com/WonderForgeLabs/gooey/prop"
)

// The viewport hook is how a control-plane act reaches App.resized, which
// stays unexported: internal/ makes the hook unreachable outside gooey's
// own modules, and the method itself is unreachable outside this package.
// A host with no tty has no SIGWINCH, so this is the ONLY way its size
// ever changes after startup.
func TestTheViewportHookResizesARunningApp(t *testing.T) {
	tty := newTestTTY(t)
	blankWinsize(t, tty.master)

	app := NewApp(Tree(&label{text: prop.NewSource("hi")}),
		WithTerminal(tty.open), WithSize(120, 40))
	start(t, app)
	tty.waitForFrame(t)

	if got := size(t, app); got != [2]int{120, 40} {
		t.Fatalf("size before resize is %v, want 120x40", got)
	}

	errc := make(chan error, 1)
	app.Post(func() { errc <- viewport.Resize(app, 60, 20) })
	if err := <-errc; err != nil {
		t.Fatalf("Resize: %v", err)
	}

	if got := size(t, app); got != [2]int{60, 20} {
		t.Fatalf("size after resize is %v, want 60x20", got)
	}
}

// A resize invalidates every node, because each one painted into a buffer
// that no longer exists — so the next frame repaints the whole tree, not
// a subset. Pinning the count is the only assertion that distinguishes
// "the composition was re-targeted" from "the size field changed and
// nothing else did"; a size assertion alone passes for both.
func TestAResizeRepaintsTheWholeTree(t *testing.T) {
	tty := newTestTTY(t)
	blankWinsize(t, tty.master)

	// Two leaves under a stack: three components, all of which must
	// repaint after the buffer is replaced.
	root := &dynBox{kids: []Component{
		&label{text: prop.NewSource("one")},
		&label{text: prop.NewSource("two")},
	}}
	app := NewApp(Tree(root), WithTerminal(tty.open), WithSize(120, 40))
	start(t, app)
	tty.waitForFrame(t)

	errc := make(chan error, 1)
	app.Post(func() { errc <- viewport.Resize(app, 60, 20) })
	if err := <-errc; err != nil {
		t.Fatalf("Resize: %v", err)
	}
	tty.waitForFrame(t)

	painted := make(chan int, 1)
	app.Post(func() { painted <- app.PaintedLastFrame() })
	if got := <-painted; got != 3 {
		t.Fatalf("a resize repainted %d components, want 3 (the whole tree)", got)
	}
}

// The hook takes the host as any, because internal/viewport cannot name
// *App without an import cycle. A host that is not an App is a LEGITIMATE
// configuration — control.NewService accepts any Host, and the tree ships
// composer-only ones — so this is an ordinary unsupported-capability
// answer, not a panic and not a programming error.
func TestTheViewportHookRefusesAHostThatIsNotAnApp(t *testing.T) {
	err := viewport.Resize(struct{ notAnApp bool }{}, 60, 20)
	if err == nil {
		t.Fatal("resizing a non-App host succeeded; want an error")
	}
}
