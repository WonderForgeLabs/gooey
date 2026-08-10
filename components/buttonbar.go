package components

import (
	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
)

// ButtonBar groups buttons into a toolbar: laid out left to right,
// optionally all the same width, optionally separated by a rule, and
// clipped with an indicator when the bar is wider than its slot.
//
// Two things make it more than an HStack.
//
// Uniform sizing is a measure-pass decision, not a styling one: the bar
// measures every member, takes the widest, and hands that width to all
// of them, so a row of buttons reads as a row of buttons rather than as
// text of assorted lengths. Nothing about the members changes — they are
// ordinary components arranged into slots.
//
// Arrow traversal makes the bar a focus SCOPE. Left and right move
// between its members and wrap at the ends instead of walking out into
// the rest of the page, which is what a toolbar means by "the arrows
// move along the toolbar". It reaches focus through gooey.FocusHost:
// arrows arrive here by bubbling (a Button does not consume them), and
// only a component that asks gets handed the manager. Up and down are
// left alone, so they still leave the bar by the ordinary spatial route.
type ButtonBar struct {
	gooey.Base
	Children []gooey.Component
	// Gap is the space between members. A bar with a Separator forces at
	// least three, since the rule needs a column with air either side.
	Gap int
	// Uniform gives every member the width of the widest one.
	Uniform bool
	// Separator is the rune drawn between members; empty draws none.
	Separator string

	mgr   *gooey.FocusManager
	sizes []gooey.Size
	slots []int // x of each member, for the separator painter
	cut   []cutMember
	over  bool // the bar did not fit
}

// cutMember is a member this bar collapsed for overflow, remembering the
// visibility it had first. Restoring to Visible unconditionally would
// quietly un-hide a member the author had set Hidden.
type cutMember struct {
	c   gooey.Component
	was gooey.Visibility
}

// OverflowMark is drawn in the bar's last column when members did not fit.
const OverflowMark = '›'

func (b *ButtonBar) ChildComponents() []gooey.Component { return b.Children }

// SetFocusManager receives the input tree. See gooey.FocusHost.
func (b *ButtonBar) SetFocusManager(m *gooey.FocusManager) { b.mgr = m }

func (b *ButtonBar) gap() int {
	g := b.Gap
	if b.Separator != "" && g < 3 {
		g = 3
	}
	return g
}

func (b *ButtonBar) Measure(avail gooey.Size) gooey.Size {
	// Members this bar collapsed for overflow are put back before
	// measuring: a bar that got wider must be able to show them again,
	// and a collapsed child measures as nothing.
	for _, m := range b.cut {
		if l := gooey.LayoutOf(m.c); l != nil && l.Visibility == gooey.Collapsed {
			l.Visibility = m.was
		}
	}
	b.cut = b.cut[:0]

	b.sizes = b.sizes[:0]
	gap, wmax, h := b.gap(), 0, 0
	for _, c := range b.Children {
		s := gooey.MeasureChild(c, avail)
		b.sizes = append(b.sizes, s)
		if s.W > wmax {
			wmax = s.W
		}
		if s.H > h {
			h = s.H
		}
	}
	if b.Uniform {
		for i := range b.sizes {
			if !collapsed(b.Children[i]) {
				b.sizes[i].W = wmax
			}
		}
	}
	w, placed := 0, false
	for i, c := range b.Children {
		if gapBefore(c, placed) {
			w += gap
		}
		w += b.sizes[i].W
		if !collapsed(c) {
			placed = true
		}
	}
	if h == 0 {
		h = 1
	}
	return gooey.Size{W: min(w, avail.W), H: min(h, avail.H)}
}

// Arrange places what fits and collapses the rest. Collapsing rather
// than clipping is what keeps the bar honest with the keyboard: a
// collapsed member is skipped by focus traversal, so tab never lands on
// a button nobody can see.
func (b *ButtonBar) Arrange(r gooey.Rect) {
	b.Base.Arrange(r)
	b.slots = b.slots[:0]
	b.over = false
	gap := b.gap()
	// One column is held back for the overflow mark, but only once
	// something has actually overflowed — otherwise a bar that exactly
	// fits would clip itself to make room for a mark it does not need.
	x, placed := r.X, false
	for i, c := range b.Children {
		if collapsed(c) {
			b.slots = append(b.slots, x)
			gooey.ArrangeChild(c, gooey.Rect{X: x, Y: r.Y, W: 0, H: r.H})
			continue
		}
		at := x
		if placed {
			at += gap
		}
		w := b.sizes[i].W
		if at+w > r.X+r.W {
			b.over = true
			if l := gooey.LayoutOf(c); l != nil {
				b.cut = append(b.cut, cutMember{c: c, was: l.Visibility})
				l.Visibility = gooey.Collapsed
			}
			b.slots = append(b.slots, at)
			gooey.ArrangeChild(c, gooey.Rect{X: at, Y: r.Y, W: 0, H: r.H})
			continue
		}
		b.slots = append(b.slots, at)
		gooey.ArrangeChild(c, gooey.Rect{X: at, Y: r.Y, W: w, H: r.H})
		x, placed = at+w, true
	}
}

// Render paints the bar's own chrome only — the separators between
// members and the overflow mark. It must not touch the rest of its
// bounds: those cells belong to the members, whose paint nodes are
// clean.
func (b *ButtonBar) Render(f *gooey.Frame) {
	r := b.Bounds()
	if r.W <= 0 || r.H <= 0 {
		return
	}
	if b.Separator != "" {
		sep := []rune(b.Separator)[0]
		prev := -1
		for i, c := range b.Children {
			if collapsed(c) || i >= len(b.slots) {
				continue
			}
			if prev >= 0 {
				// Centred in the gap between the two members.
				if x := (prev + b.slots[i]) / 2; x >= r.X && x < r.X+r.W {
					f.Cells.Set(x, r.Y, sep, styleDim)
				}
			}
			prev = b.slots[i] + b.sizes[i].W
		}
	}
	if b.over {
		f.Cells.Set(r.X+r.W-1, r.Y, OverflowMark, styleAccent)
	}
}

// HandleKey moves focus along the bar. It is reached by bubbling from
// the focused member, so a member that wants an arrow for itself simply
// consumes it first.
func (b *ButtonBar) HandleKey(ev input.KeyEvent) bool {
	var d int
	switch ev {
	case input.Named(input.KeyLeft):
		d = -1
	case input.Named(input.KeyRight):
		d = 1
	default:
		return false
	}
	if b.mgr == nil {
		return false
	}
	cur := b.indexOf(b.mgr.Focused())
	if cur < 0 {
		return false
	}
	n := len(b.Children)
	for step := 1; step <= n; step++ {
		next := ((cur+d*step)%n + n) % n
		if next == cur {
			break
		}
		if c := b.Children[next]; !collapsed(c) && b.mgr.SetFocus(c) {
			return true
		}
	}
	return false
}

// indexOf finds which member holds w — the focused component itself, or
// a member that contains it, so a bar of composite members still knows
// where focus is.
func (b *ButtonBar) indexOf(w gooey.Component) int {
	if w == nil {
		return -1
	}
	for i, c := range b.Children {
		if c == w || contains(c, w) {
			return i
		}
	}
	return -1
}

func contains(parent, w gooey.Component) bool {
	c, ok := parent.(gooey.Container)
	if !ok {
		return false
	}
	for _, ch := range c.ChildComponents() {
		if ch == w || contains(ch, w) {
			return true
		}
	}
	return false
}
