// Package gooey — POC of the retained visual tree / component model.
//
// The tree is retained: components are persistent objects with parents,
// children, and computed bounds. A frame is produced by the classic
// two-pass layout (Measure bottom-up, Arrange top-down) followed by a
// Render walk. Pixel content never enters the cell buffer — components
// record graphics.Placements on the Frame, and the flush composites the
// two planes (cells first, then pixel placements over them).
package gooey

import (
	"image"
	"io"

	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

type Size struct{ W, H int }

type Rect struct{ X, Y, W, H int }

// Component is the component model. Everything in the tree implements it.
type Component interface {
	// Measure returns the size the component wants within avail.
	Measure(avail Size) Size
	// Arrange assigns final bounds (and arranges children).
	Arrange(bounds Rect)
	// Render paints THIS component only into the frame using the bounds
	// from Arrange; children are walked by the framework via Container.
	Render(f *Frame)
}

// Container is implemented by components with children. The framework —
// not the component — walks them, so the Composer can give every component
// its own paint node.
type Container interface{ ChildComponents() []Component }

// ChildSetter is the write half of Container: a container that can have
// one of its children REPLACED in place, at the same index
// ChildComponents reported it at.
//
// It exists because control.PatchMarkup has to put a rebuilt subtree back
// where the old one was, and until this interface that was a closed type
// switch over six concrete types in the control package. The cost of the
// switch was not that it was ugly — it was that it made "may a component
// sit between a container and a named element?" a question only the
// control package could answer, and its answer was no. Any container the
// framework did not ship, or an app's own wrapper, or a design surface's
// decorator around the selected element, broke patching for EVERYTHING
// inside it. apps/wysiwyg/decorate_probe_test.go measured that.
//
// The contract has one rule, and it is the rule the type switch used to
// get by construction:
//
//	The index passed to SetChild is an index into the slice
//	ChildComponents returned. A container whose ChildComponents BUILDS a
//	list — interleaving chrome, filtering by visibility, wrapping rows —
//	must not implement this interface, because its walk index is not an
//	address it can write back to.
//
// That is why this is opt-in rather than a method on Container. Refusing
// is a legitimate implementation: a container that does not implement
// ChildSetter simply cannot be patched through, which is exactly the
// behaviour every non-listed container had before, and the caller gets a
// load-time refusal naming the type rather than a silently misplaced
// child.
//
// SetChild reports whether the write happened. False means the index was
// out of range — the composition changed under the caller — and callers
// must treat it as a refusal rather than ignoring it.
type ChildSetter interface {
	Container
	SetChild(i int, w Component) bool
}

// Overlay is implemented by a component whose paint node belongs ABOVE
// the page rather than at its place in document order. Its subtree comes
// with it.
//
// Z-order is otherwise document order, and for everything that sits IN
// the layout that is the right rule — later siblings are in front,
// children are in front of their parents. An overlay is the case it
// cannot express: a dropdown, a tooltip, a toast is not at a position in
// the document, it is on top of it, and where its owner happens to be
// declared has nothing to do with what it must cover.
//
// THE OLD ANSWER WAS "DECLARE IT LAST", and it was not an answer. A
// popup surface is the last of its OWNER'S children, which buys being
// above the owner's other children and nothing else: Frame's z-ordered
// pass forces a repaint only of nodes LATER in c.nodes than a painter
// beneath them, so the surface stayed on top exactly while its owner was
// the last thing in the whole document. Put a component after the owner
// that overlaps the dropdown — a Gauge beside a MenuBar on a design
// canvas — and every frame in which that component repaints paints over
// the open popup, with nothing able to put it back, because forcing only
// ever runs forward. That is #430, and "the popup vanished" was the
// report.
//
// A MARKER, not a predicate, and the empty method is the point. Making
// it `Overlays() bool` would put the answer in a property somebody
// writes at runtime, and a paint node's position is structural — the
// framework would then need an observer to notice a flip and a re-sync
// to act on it, which is the machinery Frozen needs and earns. Nothing
// wants to become an overlay halfway through its life. A popup that is
// closed is arranged to a zero rect and skipped by the bounds check, so
// membership costs nothing while it is not showing.
//
// Overlays keep document order WITHIN A RANK. Between ranks the rank
// decides — see OverlayRanker, and #439 for why leaving the whole layer
// to document order did not survive contact with a second kind of
// overlay.
//
// Within one rank it is still a real limit: two overlapping popups paint
// in the order they were declared rather than the order they were
// opened. Nothing in the tree needs the other answer yet, and the
// machinery it would take — an open-order stack the Composer maintains —
// is worth writing when something does. A rank does not help there,
// because the two popups are the same KIND; that is exactly the
// distinction the rank draws and the one it does not.
//
// IT MOVES PAINT, NOT INPUT. FocusManager.HitTest walks document order
// and knows nothing about this marker, so a later sibling still takes a
// press even where an overlay paints above it. Popup gets away with it
// by holding pointer capture for as long as it is open, which routes
// presses before the walk runs — but that is Popup's mechanism, not
// something this interface provides.
//
// So an overlay that does NOT take capture is responsible for its own
// routing. Implementing this alone will paint you on top and leave the
// clicks to whoever is underneath. Stated in review of #437; the same
// gap is named in docs/specs/2026-08-30-overlay-layer.md.
type Overlay interface{ OverlaysPage() }

// OverlayRanker is an Overlay that says where in the overlay layer it
// belongs. Higher is nearer the viewer; an Overlay that does not
// implement this is rank 0, which is the floor.
//
// WHY A RANK AND NOT DECLARATION ORDER. #437 lifted overlays into one
// layer and left order within it to the document, documenting that as a
// limit. It stopped being tenable the moment more than one KIND of
// overlay existed (#439): a dropdown, a toast and a tooltip are not
// peers whose stacking is a matter of taste, and the framework already
// tells an author to declare the MenuBar LAST so its dropdown covers the
// page. Getting a toast above that dropdown would then ALSO require
// declaring the ToastHost after it — two rules pulling opposite ways,
// where following the wrong one silently hides a notification on
// exactly the frames somebody most wanted to read it.
//
// The three ranks the framework itself uses are OverlayRankPopup,
// OverlayRankToast and OverlayRankAdornment. They are spaced so an
// application can sit between them.
//
// EQUAL RANKS STILL KEEP DOCUMENT ORDER. Two popups paint in the order
// they were declared rather than the order they were opened; the rank
// orders KINDS, and #437's limit survives untouched within each one.
type OverlayRanker interface {
	Overlay
	OverlayRank() int
}

// The framework's own overlay ranks, from the page upward. Spaced by ten
// so an app can put something between two of them without patching this
// file — a rank is an int, not an enum, and nothing here is exhaustive.
//
// The ORDER is the user-facing claim, and it is the one the docs have
// always made: a popup covers the page, a toast covers the popup, a
// tooltip covers the toast. Read it as "a notification is never hidden
// by a menu", which is the failure #439 reported.
const (
	// OverlayRankPopup is the floor, and deliberately 0 — every Overlay
	// written before ranks existed lands here, which is where popup
	// surfaces already were.
	OverlayRankPopup = 0
	// OverlayRankToast is above popups: a notification the user did not
	// ask for and cannot re-request must not be hidden by a menu they
	// opened, because nothing tells them it happened.
	OverlayRankToast = 10
	// OverlayRankAdornment is the top: a tooltip or a validation marker
	// describes something already on screen, so it is only ever useful
	// above whatever it is describing.
	OverlayRankAdornment = 20
)

// overlayRank is c.orderPaint's question: the component's declared rank,
// or the floor.
func overlayRank(w Component) int {
	if r, ok := w.(OverlayRanker); ok {
		return r.OverlayRank()
	}
	return OverlayRankPopup
}

// HasBackground is implemented by containers that declare a background
// fill. The fill itself is the framework's job — the Composer (and the
// one-shot Compose) paints the container's bounds with the color before
// the container's chrome and its children go down — so a container
// declares the surface and still paints only its own chrome.
//
// A nil handle means the container has no background and stays on the
// cheap chrome-only damage path. A non-nil handle whose color is unset
// still fills — with the nearest ancestor's background — so clearing a
// background at runtime erases the old fill instead of leaving it
// stranded. Either way, a container with a background handle overpaints
// its subtree when it repaints, and the Composer's z-ordered pass
// repaints the subtree above it in the same frame.
type HasBackground interface {
	BackgroundProperty() *prop.Property[render.Color]
}

// Frozen is implemented by a component whose subtree renders but does not
// act. The picture is live; the behaviour is not.
//
// Descendants lay out, paint, and keep their own paint nodes — damage
// granularity is untouched — and they are simply never the target of
// anything. Keys, scoped KeyBindings, mnemonics, clicks, drags, the
// wheel, hover watchers and focus all stop at the frozen component, and
// nothing Startable below it is started.
//
// THIS BOOL IS NOW A PROJECTION, not the framework's primary question.
// "Renders but does not act" is all-or-nothing, and a design surface
// needs "frozen except X" — so the framework asks FrozenAllows for an
// Allow SET, and an implementer of this interface alone answers the two
// endpoints of that lattice: AllowNone for true, AllowAll for false. Every
// existing implementation keeps working unchanged, and the sentence below
// about the observer applies verbatim to whichever method the component
// implements.
//
// The motivating case is a UI builder's design surface: click a button
// and it sits there like a picture. A read-only preview and a disabled
// subtree are the same shape.
//
// Two things are deliberately NOT frozen, because freezing them would
// freeze the picture rather than the behaviour. Validators are computeds
// that evaluate during paint, and their evaluation is what puts a
// validation marker on screen — so a validator with a side effect gets
// that side effect anyway, and nothing here can prevent it. And a style
// that reads HoverState still repaints, because hover is an ordinary
// property; what is lost is only motion over time, which in this
// framework needs a clock, and a clock is a Startable.
//
// The component itself is not frozen — its subtree is. A frozen host is
// still focusable, still receives the events its subtree would have, and
// its own attachments still run. That is what makes it the place a design
// surface puts its own gestures.
//
// FLIPPING IT IS A STRUCTURAL CHANGE, and the framework makes that happen
// rather than asking the host to. Composer.armFrozen gives every
// implementer an observer whose evaluation CALLS this method, so any
// property read inside it is subscribed by the ordinary call-site rule; a
// Set on that property schedules a frame, Frame's sweep compares the new
// answer against the last one, and a real flip raises the same structDirty
// a Dynamic container raises. The re-sync that follows runs in the SAME
// frame, before anything paints: walkNodes re-derives the Startable set
// through frozenAncestor, FocusManager.Resync re-derives the focus order,
// the scoped bindings, the mnemonics and the hover watchers, and
// FocusManager.evictFrozen retargets a hover and drops a capture that is
// now inside the picture. So pressing the key that turns design mode on
// leaves nothing in the subtree reachable by the very next event.
//
// Freezing costs no repaint of its own. It changes what the tree MEANS,
// not what it looks like, so the only components that repaint for a flip
// are the ones that read the same property while painting (a mode
// indicator) plus the two involved if focus had to leave the subtree.
//
// THE LIMIT, and it is the same limit every derived value in this
// framework has: the observer subscribes to what Frozen() READS. An
// implementation over plain Go state — a bare bool field written by a
// handler — records no dependency, so nothing notices when it changes and
// the old sampled behaviour is what you get. Read a property, or call
// Composer.InvalidateStructure by hand.
type Frozen interface{ Frozen() bool }

// FrozenAllows is Frozen's widened form: instead of "does the subtree
// act", it answers "WHAT still acts" — see Allow.
//
// It embeds Frozen rather than replacing it, so the two answers cannot
// disagree: an implementer states its base case with Frozen() and its
// exceptions with FrozenAllow(), and frozenAllow below is the one place
// that combines them.
//
// Both methods are called from inside Composer.armFrozen's computed, so
// the subscription rule that made a bool Frozen() observable applies to
// the allow set with nothing added: whatever FrozenAllow() reads to
// decide becomes a dependency of the observer, and a Set on it schedules
// a frame whose sweep sees the new SET and re-syncs on any change — not
// only on the true/false flip.
type FrozenAllows interface {
	Frozen
	// FrozenAllow is consulted only when Frozen() is true; a component
	// that is not frozen already allows everything.
	FrozenAllow() Allow
}

// frozenAllow is what w permits inside its subtree: AllowAll for anything
// that does not freeze, and otherwise the host's own set.
//
// BOTH READS ARE HOISTED, unconditionally, above the decision. That is
// not style: this function runs inside armFrozen's computed, so a Get
// behind an early return drops out of the dependency set on the frames
// where it does not execute (see prop's recordRead, and CLAUDE.md's
// "Dependencies are recorded by the Get that actually runs"). Reading
// FrozenAllow() only when Frozen() came back true would leave the
// observer deaf to a change in the allow set on exactly the frames where
// the answer is about to start mattering.
func frozenAllow(w Component) Allow {
	if fa, ok := w.(FrozenAllows); ok {
		allowed := fa.FrozenAllow()
		frozen := fa.Frozen()
		if !frozen {
			return AllowAll
		}
		return allowed
	}
	if f, ok := w.(Frozen); ok && f.Frozen() {
		return AllowNone
	}
	return AllowAll
}

// Decorator is implemented by components whose Render owns no cells of
// its own but re-styles cells that earlier siblings painted (ItemsView's
// row highlight). It must be re-applied after an underlying repaint.
type Decorator interface{ DecoratesCells() }

// CellPassthrough is implemented by transparent containers whose Render
// neither owns nor re-styles cells. Their paint nodes never need to be
// restored when an overlay leaves a region.
//
// The claim is about the TYPE's Render; the Composer still checks the
// INSTANCE, because a container that declares a Background is filled by
// the framework and owns its bounds however empty its Render is. A
// Canvas implements this and is a passthrough only while it has no
// colour — see cellPassthrough in composer.go.
type CellPassthrough interface{ PassesCellsThrough() }

// backgroundProp returns w's declared background handle, or nil when w
// declares none.
func backgroundProp(w Component) *prop.Property[render.Color] {
	if hb, ok := w.(HasBackground); ok {
		return hb.BackgroundProperty()
	}
	return nil
}

// Frame is one composed frame: the cell plane plus deferred pixel
// placements. Graphics is nil when the terminal has no pixel protocol —
// components with pixel content must then degrade into cells (halfblock).
//
// Caps is the terminal's detected capability set, carried on the frame
// so a component can adapt AT RENDER TIME: the color depth it will
// actually be shown in, which graphics protocol (if any) is available,
// and the pixel size of a cell. This is the mechanism behind
// "a different experience per rendering engine" — the component asks the
// frame what it is painting onto. It is a plain field, not a property:
// capabilities are fixed for the life of a session, so making them
// observable would buy nothing and cost every component a dependency edge.
type Frame struct {
	Cells        *render.Buffer
	Graphics     graphics.Encoder
	CellW, CellH int
	Caps         term.Caps

	placements []graphics.Placement
	// sink is installed by the Composer around each paint node, so a
	// placement recorded during Render is filed under the component that
	// recorded it. See Place.
	sink func(graphics.Placement)
	// fault is whatever layout breach this frame's own compose hit, set
	// by Compose. Nil on a Composer-built frame, which carries its fault
	// on the Composer instead. See Frame.LayoutFault.
	fault *LayoutFault
}

// Depth is the color depth this frame will be flushed at.
func (f *Frame) Depth() render.ColorDepth { return f.Caps.Color }

// Place records pixel content to be composited over the cells. It is the
// pixel-plane counterpart of writing to f.Cells, and a component with an
// image calls it from Render exactly where a text component would write
// runes.
//
// It is a method rather than an appendable field because a placement has
// an OWNER. Under the Composer only dirty components re-render, so a
// placement list rebuilt from scratch each frame would lose the images of
// every component that did not repaint. Routing through here files each
// placement under the paint node that was executing, which is what lets
// the flush say "this component's images changed" and leave the rest
// alone — the same per-component damage rule the cell plane follows.
func (f *Frame) Place(p graphics.Placement) {
	// CLIPPED AGAINST THE SAME RECT AS THE CELLS (#357). A clip that
	// bounded text and not pictures would be worse than none, because it
	// would look like it works right up until a component with an image
	// overflows — and a sixel or kitty image is composited by the
	// terminal, so no cell-plane check can catch it.
	p, ok := clipPlacement(p, f.Cells.ClipRect())
	if !ok {
		return
	}
	if f.sink != nil {
		f.sink(p)
		return
	}
	f.placements = append(f.placements, p)
}

// clipPlacement trims a placement to the visible cells and crops its
// image to match, reporting false when nothing of it survives.
//
// Cropping rather than dropping a partly-visible image is what makes a
// viewport possible at all: a row scrolling off the top of a pane should
// lose its top half, not vanish. The image was rasterized for exactly
// Cols x Rows cells, so cells map to pixels by a plain ratio.
//
// An image that cannot be cropped is DROPPED rather than placed whole.
// Losing a picture is visible and local; letting it composite over a
// neighbour is the silent corruption this is here to prevent.
func clipPlacement(p graphics.Placement, clip render.Rect) (graphics.Placement, bool) {
	x0, y0 := max(p.Col, clip.X), max(p.Row, clip.Y)
	x1, y1 := min(p.Col+p.Cols, clip.X+clip.W), min(p.Row+p.Rows, clip.Y+clip.H)
	if x0 >= x1 || y0 >= y1 {
		return p, false
	}
	if x0 == p.Col && y0 == p.Row && x1 == p.Col+p.Cols && y1 == p.Row+p.Rows {
		return p, true // wholly inside: the common case, untouched
	}
	sub, ok := p.Img.(interface {
		SubImage(image.Rectangle) image.Image
	})
	if !ok || p.Cols <= 0 || p.Rows <= 0 {
		return p, false
	}
	b := p.Img.Bounds()
	pxW, pxH := b.Dx(), b.Dy()
	crop := image.Rect(
		b.Min.X+(x0-p.Col)*pxW/p.Cols, b.Min.Y+(y0-p.Row)*pxH/p.Rows,
		b.Min.X+(x1-p.Col)*pxW/p.Cols, b.Min.Y+(y1-p.Row)*pxH/p.Rows,
	)
	p.Img = sub.SubImage(crop.Intersect(b))
	p.Col, p.Row, p.Cols, p.Rows = x0, y0, x1-x0, y1-y0
	return p, true
}

// Placements is this frame's pixel plane in paint order.
func (f *Frame) Placements() []graphics.Placement { return f.placements }

// LayoutFault reports a breach seen while composing THIS frame, or nil.
// It is the one-shot path's equivalent of Composer.LayoutFault: without
// it Compose had nowhere to put a fault, which is why it used to leave
// one in the package global for somebody else to find.
//
// Non-nil means some subtree was too deep to walk and was left unlaid.
// The frame is still valid, just missing that part.
func (f *Frame) LayoutFault() *LayoutFault { return f.fault }

// Compose lays out root into a fresh frame sized to caps — the one-shot
// path (full repaint). The damage-tracked path is Composer.
//
// It BRACKETS the pass with TakeLayoutFault, and both halves are load
// bearing because layoutFault is package-level state.
//
// Taking on the way IN discards anything an earlier pass left behind, so
// the fault this Frame reports is this compose's own rather than one
// inherited from a tree it never saw.
//
// Taking on the way OUT is what stops the leak in the other direction.
// Measure, Arrange and renderTree can each record a fault; before this,
// none of them was drained here, so a cyclic tree composed once left the
// fault sitting in the global — and the next Composer picked it up at
// CONSTRUCTION (composer.go, where the same Take runs so a cycle is
// readable before the first frame). A clean tree then reported a fault
// naming a component from an unrelated one.
func Compose(root Component, caps term.Caps, enc graphics.Encoder) *Frame {
	TakeLayoutFault()
	f := &Frame{
		Cells:    render.NewBuffer(caps.Cols, caps.Rows),
		Graphics: enc,
		CellW:    caps.CellW,
		CellH:    caps.CellH,
		Caps:     caps,
	}
	root.Measure(Size{caps.Cols, caps.Rows})
	root.Arrange(Rect{0, 0, caps.Cols, caps.Rows})
	renderTree(root, f, 0)
	f.fault = TakeLayoutFault()
	return f
}

func renderTree(w Component, f *Frame, depth int) {
	if depth > MaxLayoutDepth {
		noteLayoutFaultAt("Render", w, depth)
		return
	}
	if l := LayoutOf(w); l != nil && l.Visibility == Collapsed {
		return // collapsed subtrees paint nothing at all
	}
	if paintable(w) {
		if bp := backgroundProp(w); bp != nil {
			if col := bp.Get(); col.Set {
				if b, ok := w.(Bounded); ok {
					fillRect(f.Cells, b.Bounds(), render.Style{Bg: col})
				}
			}
		}
		w.Render(f)
	}
	if c, ok := w.(Container); ok {
		for _, ch := range c.ChildComponents() {
			renderTree(ch, f, depth+1)
		}
	}
}

// Flush writes the frame: cell plane first, then pixel placements. The
// whole sequence is one synchronized update — cells and the images that
// sit on top of them are a single frame, so the terminal must not
// present the gap between them.
func (f *Frame) Flush(w io.Writer) error {
	if _, err := io.WriteString(w, render.BeginSync); err != nil {
		return err
	}
	defer io.WriteString(w, render.EndSync)
	if err := render.FlushCells(w, f.Cells, f.Caps.Color, false); err != nil {
		return err
	}
	for _, p := range f.placements {
		// Position cursor at the placement cell (1-based), emit protocol bytes.
		var out []byte
		out = append(out, []byte(cursorTo(p.Col, p.Row))...)
		if err := f.Graphics.Encode(&out, p.Img, p.Cols, p.Rows, f.CellW, f.CellH); err != nil {
			return err
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
	return nil
}

func cursorTo(col, row int) string {
	return "\x1b[" + itoa(row+1) + ";" + itoa(col+1) + "H"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
