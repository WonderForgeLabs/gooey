package gooey

// The FrameworkElement layer: every component that embeds Base carries
// Layout — margin, explicit size, alignment, visibility, and grid
// attachments. Parents never call child.Measure/Arrange directly; they
// go through MeasureChild/ArrangeChild, which implement the XAML
// measure/arrange sandwich: subtract margin, honor explicit size,
// cache DesiredSize, then align the child inside its slot.

import (
	"fmt"

	"github.com/WonderForgeLabs/gooey/prop"
)

// Thickness is margin in cells: left, top, right, bottom.
type Thickness struct{ L, T, R, B int }

// M is a uniform thickness; MH/MV set horizontal/vertical pairs.
func M(all int) Thickness   { return Thickness{all, all, all, all} }
func MH(h, v int) Thickness { return Thickness{h, v, h, v} }

type Align uint8

const (
	AlignStretch Align = iota // fill the slot (default)
	AlignStart
	AlignCenter
	AlignEnd
)

type Visibility uint8

const (
	Visible   Visibility = iota
	Hidden               // occupies space, does not paint
	Collapsed            // occupies nothing
)

// Layout is the per-element layout state — the XAML FrameworkElement
// properties plus grid attached properties (Grid.Row etc. live here
// because Go has no attached-property store; the element itself is it).
type Layout struct {
	Width, Height  int // explicit size in cells; 0 = auto
	Margin         Thickness
	HAlign, VAlign Align
	Visibility     Visibility

	// Grid attached properties.
	Row, Col         int
	RowSpan, ColSpan int // 0 means 1

	// Canvas attached properties: the child's offset from the Canvas's
	// top-left corner, in cells.
	Left, Top int

	desired Size // cached by MeasureChild, margin included

	// visSrc, when non-nil, is the bound source of Visibility: the plain
	// field above becomes a per-frame cache of visSrc(). Layout and the
	// Composer's sweeps keep reading the field (plain reads, outside any
	// evaluation — exactly as before); the framework syncs it from the
	// source at defined points (MeasureChild, and the Composer's
	// visibility observers before layout). See BindVisibility.
	visSrc func() Visibility
}

// BindVisibility makes p the source of this element's Visibility —
// markup's Visibility="{{.ShowDetails}}" lands here, and code-behind may
// call it directly with a source or a computed. A Set on p (or on any
// dependency of a computed p) schedules a frame through the Composer's
// visibility observer, and the existing per-frame sweep then erases,
// restores, and relayouts exactly as a literal flip does; while bound,
// direct writes to the Visibility field are overwritten each frame.
//
// Bind before the tree is composed (markup does). Rebinding a composed
// element is not supported: the observer subscribed to the first source.
func (l *Layout) BindVisibility(p *prop.Property[Visibility]) {
	l.BindVisibilityFunc(p.Get)
}

// BindVisibilityBool binds Visibility to a bool property: true is
// Visible, false is Collapsed — the XAML BooleanToVisibilityConverter
// default, chosen because show/hide state in a viewmodel is almost
// always a bool. An element that should reserve its space when hidden
// binds a *prop.Property[Visibility] instead.
func (l *Layout) BindVisibilityBool(p *prop.Property[bool]) {
	l.BindVisibilityFunc(func() Visibility {
		if p.Get() {
			return Visible
		}
		return Collapsed
	})
}

// BindVisibilityFunc is the general form behind both Bind variants: get
// becomes the source of Visibility. Whether a call to get subscribes is
// decided by the CALL SITE, like every property read — the Composer's
// observer evaluates it (subscription), layout calls it plain (read).
// The field is synced immediately so a tree inspected before its first
// frame is already right.
func (l *Layout) BindVisibilityFunc(get func() Visibility) {
	l.visSrc = get
	if get != nil {
		l.Visibility = get()
	}
}

// VisibilitySource reports the bound source of Visibility, nil when the
// plain field is the whole story. The markup patch path uses it to carry
// a binding onto a rebuilt element that did not restate the attribute.
func (l *Layout) VisibilitySource() func() Visibility { return l.visSrc }

// HasLayout is implemented by anything embedding Base.
type HasLayout interface{ LayoutProps() *Layout }

// LayoutOf returns w's Layout, or nil if it does not carry one (only
// components embedding Base do). Container authors outside this package
// need it to read a child's Visibility and attached properties, which is
// why it is exported: a panel in gooey/components has no other way to ask.
func LayoutOf(w Component) *Layout {
	if hl, ok := w.(HasLayout); ok {
		return hl.LayoutProps()
	}
	return nil
}

// L applies layout to a component in-place and returns it — the literal-
// friendly way to set layout in Go composition:
//
//	gooey.L(&Text{...}, gooey.Layout{Margin: gooey.M(1), HAlign: gooey.AlignCenter})
func L(w Component, l Layout) Component {
	if hl, ok := w.(HasLayout); ok {
		*hl.LayoutProps() = l
	}
	return w
}

// MaxLayoutDepth bounds how deep one Measure or Arrange pass will walk
// before it stops and records a LayoutFault instead of recursing.
//
// # Where the number comes from
//
// It is not a round number someone liked. Two measurements bracket it,
// both taken on this tree (issue #216):
//
// Below: the deepest component tree anything in this repository actually
// produces is 7. That is instrumented MeasureChild depth, not a reading
// of the markup — the whole test corpus of every module tops out at 7
// (apps/wysiwyg and the root module), and the deepest screen of a real
// app driven under a pty reaches 6 (apps/store). The deepest .gooey
// document in the tree nests 10 XML elements, several of which
// (<Gooey>, <Grid.Rows>, <Gooey.Resources>) are not layout nodes at all,
// which is why the static figure is the larger one. 512 is 73x the
// deepest tree that has ever been laid out here.
//
// Above: what the stack survives. An uncapped cycle through three real
// containers (Border/VStack/Grid) dies with "goroutine stack exceeds
// 1000000000-byte limit", and its own crash trace gives the cost: 688
// bytes of stack per Border+VStack+Grid turn, i.e. 229 bytes per
// MeasureChild level. So the process dies at roughly 4.4 million levels,
// and 512 levels costs about 117KB — four orders of magnitude of margin.
//
// The cap therefore does not exist to keep the stack alive; anything
// under a million would do that. It exists to make a cycle cost one
// frame instead of a gigabyte, while sitting far enough above every real
// tree that it cannot reject a document that works today.
const MaxLayoutDepth = 512

// LayoutFault is what the framework records instead of dying when a tree
// cannot be walked. It is a report, not a panic: the walk stops, the
// subtree below is left alone, and everything above it lays out and
// paints normally — a localized hole rather than a dead process. Read it
// with Composer.LayoutFault or App.LayoutFault.
//
// A crash is the one failure this framework cannot report through its own
// error path: a stack overflow is fatal, unrecoverable, and skips
// Screen.Restore, so it takes the user's terminal modes and their unsaved
// work with it. That is why this is prevented rather than caught.
//
// A cycle is not one walk but SEVERAL, and the issue that asked for this
// (#216) named only one of them. Each of these recurses over
// ChildComponents independently, each one died on a cyclic tree, and
// every one runs on an ordinary frame or an ordinary mouse event:
//
//	Compose   — Composer.build, which runs before any layout at all.
//	Focus     — FocusManager.walk, in the same frame as Compose.
//	Measure   — MeasureChild.
//	Arrange   — ArrangeChild.
//	HitTest   — hitTest, per mouse motion event.
//	Focusable — firstFocusable, per click.
//	Render    — renderTree, the one-shot Compose path.
//
// They divide into two detection strategies, and Depth says which. A
// phase that already keeps a map keyed by component (Compose, Focus)
// detects the repeat by IDENTITY, which is exact and free, and reports
// Depth 0. The rest count depth against MaxLayoutDepth.
type LayoutFault struct {
	Phase string    // which walk stopped; see above
	At    Component // the component the walk refused to descend into
	Depth int       // the depth at which it stopped, or 0 when detected by identity
}

func (f *LayoutFault) Error() string {
	if f.Depth == 0 {
		return fmt.Sprintf("gooey: the %s walk stopped at %T: this component appears more than once in the tree — a cycle, or one instance placed under two parents. Every component gets exactly one paint node, so the second placement cannot be composed and its subtree was skipped.", f.Phase, f.At)
	}
	return fmt.Sprintf("gooey: %s stopped at depth %d in %T: the component tree is deeper than MaxLayoutDepth (%d), which in practice means a cycle — a container that is its own descendant. That subtree was not laid out; the rest of the tree is unaffected.",
		f.Phase, f.Depth, f.At, MaxLayoutDepth)
}

// layoutDepth counts the Measure/Arrange walk's current depth, and
// layoutFault holds the first fault since it was last taken.
//
// Package-level and unlocked, like every property in the framework, and
// for the same reason: layout runs on the UI goroutine and nowhere else
// (see App.Run). Both are cheap on the normal path — one increment and
// one compare per child per pass, no allocation and no map.
var (
	layoutDepth int
	layoutFault *LayoutFault
)

// TakeLayoutFault returns the fault recorded since the last call and
// clears it, so a runaway reports once rather than once per level.
// Composer.Frame calls it; app code reads Composer.LayoutFault instead.
func TakeLayoutFault() *LayoutFault {
	f := layoutFault
	layoutFault = nil
	return f
}

// noteLayoutFault keeps the FIRST fault of a pass. The deepest is no
// more informative — every level of a cycle names the same components —
// and keeping the first means a runaway allocates one record, not one
// per level.
func noteLayoutFault(phase string, w Component) {
	if layoutFault == nil {
		layoutFault = &LayoutFault{Phase: phase, At: w, Depth: layoutDepth}
	}
}

// noteIdentityFault is the identity-detected half: a walk that already
// keys a map by component found the same one twice. Depth is 0 because
// these walks do not count one — see LayoutFault.
func noteIdentityFault(phase string, w Component) {
	if layoutFault == nil {
		layoutFault = &LayoutFault{Phase: phase, At: w}
	}
}

// noteLayoutFaultAt is for the walks that carry their depth in a
// parameter rather than in layoutDepth — the ones that return from inside
// a loop, where a deferred decrement would cost a defer per component per
// mouse event.
func noteLayoutFaultAt(phase string, w Component, depth int) {
	if layoutFault == nil {
		layoutFault = &LayoutFault{Phase: phase, At: w, Depth: depth}
	}
}

// MeasureChild measures w within avail, applying margin, explicit
// size, and visibility, and caches the resulting desired size
// (margin included) — the XAML Measure pass.
//
// It refuses to descend past MaxLayoutDepth, recording a LayoutFault and
// returning a zero size instead.
//
// The decrement is DEFERRED rather than written at each return, and that
// is a priced decision, not a reflex. A deferred call runs during panic
// unwinding, so a container that panics mid-measure cannot leave the
// counter climbing and make every later frame report a phantom fault —
// and that path is reachable rather than theoretical: Bridge.round
// (control/bridge.go) recovers a panic from a control-plane closure and
// lets the process keep running, and ItemsView.Validate measures a
// throwaway row from exactly there. App.Run's recover re-panics, so it
// is not the one that matters.
//
// The price, measured: 160.1ns/op with the guard against 137.8ns without,
// for a 7-level pass — about 3ns per call, 0 allocations either way. On a
// 200-component tree that is well under a microsecond per frame, which is
// worth paying for a counter that cannot desynchronize.
func MeasureChild(w Component, avail Size) Size {
	layoutDepth++
	defer func() { layoutDepth-- }()
	if layoutDepth > MaxLayoutDepth {
		noteLayoutFault("Measure", w)
		return Size{}
	}
	l := LayoutOf(w)
	if l == nil {
		return w.Measure(avail)
	}
	if l.visSrc != nil {
		// Sync the field from the bound source. Layout runs outside any
		// evaluation context, so this records no dependency — change
		// notification is the Composer's visibility observer's job; this
		// keeps the one-shot Compose path and every direct field reader
		// (panels, focus, hit-testing) correct without touching them.
		l.Visibility = l.visSrc()
	}
	if l.Visibility == Collapsed {
		l.desired = Size{}
		return Size{}
	}
	inner := Size{
		W: max(0, avail.W-l.Margin.L-l.Margin.R),
		H: max(0, avail.H-l.Margin.T-l.Margin.B),
	}
	if l.Width > 0 {
		inner.W = min(l.Width, inner.W)
	}
	if l.Height > 0 {
		inner.H = min(l.Height, inner.H)
	}
	m := w.Measure(inner)
	if l.Width > 0 {
		m.W = l.Width
	}
	if l.Height > 0 {
		m.H = l.Height
	}
	l.desired = Size{
		W: min(m.W+l.Margin.L+l.Margin.R, avail.W),
		H: min(m.H+l.Margin.T+l.Margin.B, avail.H),
	}
	return l.desired
}

// ArrangeChild places w inside slot, applying margin and alignment —
// the XAML Arrange pass. Stretch fills the slot; other alignments use
// the measured desired size.
//
// It carries the same MaxLayoutDepth bound as MeasureChild. The issue
// filed for this asked whether Arrange needed it too, having only
// observed the crash in Measure; it does — a cycle arranges exactly as
// far as it measures, and an Arrange-only cycle (a container that
// arranges a child it never measured, which components.go documents as
// reachable) dies the same way.
func ArrangeChild(w Component, slot Rect) {
	layoutDepth++
	defer func() { layoutDepth-- }()
	if layoutDepth > MaxLayoutDepth {
		noteLayoutFault("Arrange", w)
		return
	}
	l := LayoutOf(w)
	if l == nil {
		w.Arrange(slot)
		return
	}
	if l.Visibility == Collapsed {
		w.Arrange(Rect{slot.X, slot.Y, 0, 0})
		return
	}
	content := Rect{
		X: slot.X + l.Margin.L,
		Y: slot.Y + l.Margin.T,
		W: max(0, slot.W-l.Margin.L-l.Margin.R),
		H: max(0, slot.H-l.Margin.T-l.Margin.B),
	}
	dw := max(0, l.desired.W-l.Margin.L-l.Margin.R)
	dh := max(0, l.desired.H-l.Margin.T-l.Margin.B)
	final := content
	if l.HAlign != AlignStretch || l.Width > 0 {
		final.W = min(dw, content.W)
		switch l.HAlign {
		case AlignCenter:
			final.X += (content.W - final.W) / 2
		case AlignEnd:
			final.X += content.W - final.W
		}
	}
	if l.VAlign != AlignStretch || l.Height > 0 {
		final.H = min(dh, content.H)
		switch l.VAlign {
		case AlignCenter:
			final.Y += (content.H - final.H) / 2
		case AlignEnd:
			final.Y += content.H - final.H
		}
	}
	w.Arrange(final)
}

// paintable reports whether w should render (Visible) — Hidden and
// Collapsed elements keep their state but produce no cells.
func paintable(w Component) bool {
	l := LayoutOf(w)
	return l == nil || l.Visibility == Visible
}
