package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// StatusBar is the bottom row every demo in this repo had hand-rolled as
// a dim Text with spaces counted by hand: three sections, one against
// each edge and one in the middle.
//
// The sections are COMPONENTS, not strings. That is the whole promotion:
// a status bar whose right section is a Spinner while something loads,
// or whose centre is a ProgressBar, is the same component as one showing
// three pieces of text — and each section keeps its own paint node, so a
// clock ticking on the right repaints the right section and leaves the
// key hints on the left alone.
//
// It paints NOTHING of its own. A container's bounds enclose its
// children's cells, so filling the row would wipe sections whose nodes
// are clean and will not repaint; that is the same rule Border follows
// and the reason there is no Background here (see
// docs/specs/2026-08-10-container-backgrounds.md). A bar that should
// look like a bar gets there by styling its sections.
type StatusBar struct {
	gooey.Base
	Left, Center, Right gooey.Component

	kids  []gooey.Component
	sizes [3]gooey.Size // as measured, in Left/Center/Right order
}

// StatusText is the shorthand the promoted pattern deserves: a dim Text
// for a section that is only ever text. <StatusBar Left="{{.Status}}"/>
// builds one of these.
func StatusText(content *prop.Property[string]) *Text {
	return &Text{Content: content, Style: Sty(render.Style{Dim: true})}
}

func (s *StatusBar) ChildComponents() []gooey.Component {
	s.kids = s.kids[:0]
	for _, c := range []gooey.Component{s.Left, s.Center, s.Right} {
		if c != nil {
			s.kids = append(s.kids, c)
		}
	}
	return s.kids
}

func (s *StatusBar) Measure(avail gooey.Size) gooey.Size {
	h := 0
	for i, c := range [3]gooey.Component{s.Left, s.Center, s.Right} {
		s.sizes[i] = gooey.Size{}
		if c == nil {
			continue
		}
		s.sizes[i] = gooey.MeasureChild(c, avail)
		if s.sizes[i].H > h {
			h = s.sizes[i].H
		}
	}
	if h == 0 {
		h = 1
	}
	// A status bar spans its host: it is defined by its edges, so it must
	// be given the whole width to have edges to sit against.
	return gooey.Size{W: avail.W, H: min(h, avail.H)}
}

// Arrange gives the edges priority. Left takes what it asked for, Right
// takes what is left of what it asked for, and Centre gets the gap
// between them — so a long status message shortens the middle rather
// than pushing a key hint off the screen.
func (s *StatusBar) Arrange(b gooey.Rect) {
	s.Base.Arrange(b)
	lw := s.sizes[0].W
	rw := s.sizes[2].W
	lw = min(lw, b.W)
	rw = min(rw, b.W-lw)

	if s.Left != nil {
		gooey.ArrangeChild(s.Left, gooey.Rect{X: b.X, Y: b.Y, W: lw, H: b.H})
	}
	if s.Right != nil {
		gooey.ArrangeChild(s.Right, gooey.Rect{X: b.X + b.W - rw, Y: b.Y, W: rw, H: b.H})
	}
	if s.Center == nil {
		return
	}
	gap := gooey.Rect{X: b.X + lw, Y: b.Y, W: max(0, b.W-lw-rw), H: b.H}
	cw := min(s.sizes[1].W, gap.W)
	gooey.ArrangeChild(s.Center, gooey.Rect{
		X: gap.X + (gap.W-cw)/2, Y: gap.Y, W: cw, H: gap.H,
	})
}

func (s *StatusBar) Render(*gooey.Frame) {}
