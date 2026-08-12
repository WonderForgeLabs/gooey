package main

import (
	"github.com/WonderForgeLabs/gooey"
)

// preview is the editor's live island: a one-child container whose child
// is REPLACED whenever the document changes.
//
// It exists because the editor rebuilds the thing being edited on every
// keystroke, and that is the operation the whole layout is arranged
// around. Replacing a subtree resets component-local state — a caret
// most visibly — so nothing the user types into may live inside this
// container. The inspector's inputs are siblings of it, and
// TestEditorInputsAreSiblingsOfThePreview fails if a later edit moves
// one inside.
//
// gooey.Dynamic is the framework's existing mechanism for exactly this:
// change what ChildComponents returns, call the hook, and the next frame
// re-syncs paint nodes and the input tree while KEEPING the node of
// every component that is still there. Swapping the preview therefore
// repaints the preview, not the editor.
type preview struct {
	gooey.Base
	child gooey.Component
	hook  func()
	size  gooey.Size
}

// SetStructureHook is gooey.Dynamic.
func (p *preview) SetStructureHook(h func()) { p.hook = h }

func (p *preview) ChildComponents() []gooey.Component {
	if p.child == nil {
		return nil
	}
	return []gooey.Component{p.child}
}

// swap replaces the previewed tree. Called on the UI goroutine, from a
// property subscription — never from another goroutine.
func (p *preview) swap(c gooey.Component) {
	p.child = c
	if p.hook != nil {
		p.hook()
	}
}

func (p *preview) Measure(avail gooey.Size) gooey.Size {
	if p.child != nil {
		p.size = gooey.MeasureChild(p.child, avail)
	}
	return avail
}

func (p *preview) Arrange(b gooey.Rect) {
	p.Base.Arrange(b)
	if p.child == nil {
		return
	}
	gooey.ArrangeChild(p.child, gooey.Rect{
		X: b.X, Y: b.Y,
		W: min(p.size.W, b.W),
		H: min(p.size.H, b.H),
	})
}

// Render paints nothing: a container paints only its own chrome, and
// this one has none. Pre-clearing here would wipe the previewed tree.
func (p *preview) Render(f *gooey.Frame) {}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
