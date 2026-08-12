// Package preview is the editor's live island — and the one pane that is
// not a pure function of editor state.
//
// The other three panes display; this one HOSTS. It owns a swappable
// child component tree, which is why it needs a Go type at all: a
// markup-only control has no way to receive a *gooey.Component, and
// gooey.Dynamic is the framework mechanism for changing what
// ChildComponents returns without rebuilding everything around it.
//
// That asymmetry is worth stating rather than smoothing over, because it
// is also what the layout is arranged around: this subtree is discarded
// and rebuilt on every edit, so nothing the user types into may live
// inside it.
package preview

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/render"
)

// Pane is the one-child container whose child is REPLACED whenever the
// document changes.
//
// gooey.Dynamic is the framework's existing mechanism for exactly this:
// change what ChildComponents returns, call the hook, and the next frame
// re-syncs paint nodes and the input tree while KEEPING the node of every
// component that is still there. Swapping the preview therefore repaints
// the preview, not the editor.
type Pane struct {
	gooey.Base
	child gooey.Component
	hook  func()
	size  gooey.Size
}

// Builder registers the pane as <Preview Title="designer"/>, with p as
// its host.
//
// There is no .gooey here, and that is the rule rather than an exception:
// this control is two components deep — a titled Border around the host —
// so it is built with the OBJECT MODEL. A markup file would express the
// same two nodes as a string that has to be parsed at every load, and a
// string is the one form the compiler cannot check.
//
// Markup earns its keep where a pane is a LAYOUT — the palette's list and
// item template, the inspector's seven-way grid. It does not earn it for
// a wrapper.
//
// One Pane per Builder, deliberately: the editor has exactly one preview,
// and a second instance of this element would mount the same tree twice.
func Builder(p *Pane) markup.Builder {
	return func(e markup.Element, ctx *markup.Context) (gooey.Component, error) {
		title := e.Attrs["Title"]
		if title == "" {
			title = "designer"
		}
		return &components.Border{Title: components.Str(title), Child: p}, nil
	}
}

// SetStructureHook is gooey.Dynamic.
func (p *Pane) SetStructureHook(h func()) { p.hook = h }

// Child is the tree currently mounted, or nil before the first Swap.
// Exported for the tests that assert the island was populated at all;
// the field stays unexported because writing it without the structure
// hook would leave the composer's paint nodes describing the old tree.
func (p *Pane) Child() gooey.Component { return p.child }

func (p *Pane) ChildComponents() []gooey.Component {
	if p.child == nil {
		return nil
	}
	return []gooey.Component{p.child}
}

// Swap replaces the previewed tree. Called on the UI goroutine, from a
// property subscription — never from another goroutine.
func (p *Pane) Swap(c gooey.Component) {
	p.child = c
	if p.hook != nil {
		p.hook()
	}
}

func (p *Pane) Measure(avail gooey.Size) gooey.Size {
	if p.child != nil {
		p.size = gooey.MeasureChild(p.child, avail)
	}
	return avail
}

func (p *Pane) Arrange(b gooey.Rect) {
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

// Render paints nothing: a container paints only its own chrome, and this
// one has none. Pre-clearing here would wipe the previewed tree.
func (p *Pane) Render(f *gooey.Frame) {}

// MirrorBuilder is what <Preview> becomes in the DOCUMENT's vocabulary —
// see mirror.go for why the document gets a different builder for the
// same element name rather than a recursion guard.
func MirrorBuilder(style render.Style) markup.Builder {
	return func(markup.Element, *markup.Context) (gooey.Component, error) {
		return &Mirror{style: style}, nil
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
