package components

import (
	"slices"
	"time"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// ItemsView is the data-driven list: an item source, a template, and one
// realized row per item the view can actually see.
//
// It is the second of the two XAML pillars (the first being UserControls)
// and the one that makes list UI declarative. Everything it does follows
// from three decisions, all of them forced by "no reflection anywhere":
//
//   - The COLLECTION arrives through ItemSource, not as a typed slice.
//     A slice property stays typed in the viewmodel; Items adapts it.
//   - An ITEM arrives as a value map, produced by the app's projection
//     func. That map is the honest, no-reflection stand-in for XAML's
//     x:DataType — the ceiling of what v1 can typecheck, and the reason
//     a template's bindings can resolve against an item at all.
//   - A TEMPLATE is a factory, not a tree. Markup captures the element
//     subtree and hands back an ItemFactory; the view knows only how to
//     call it, which is what keeps this package free of any dependency
//     on markup.
//
// Realization is windowed from day one: only the rows that fit are built,
// and they are keyed by item index. A change to one item re-projects the
// window, finds one row whose values differ, and Sets exactly those
// values — so the components that read them repaint and nothing else does.
// That is the damage guarantee applied to lists, and it is what the
// damage-count tests pin down.
type ItemsView struct {
	gooey.Base
	gooey.FocusState

	// Items is the collection. Build one with Items[T] over a typed
	// slice property.
	Items *prop.Property[ItemSource]
	// Selected is the selected index, shared with the viewmodel: the
	// view Sets it on navigation and reads it to scroll and highlight.
	// Nil means the list is not selectable.
	Selected *prop.Property[int]
	// SelectionChanged runs after the VIEW moves the selection — a key, a
	// click, the wheel. It does not run when the viewmodel Sets Selected
	// itself (the viewmodel already knows), and not when a gesture clamps
	// to the index already selected: it reports change, not intent.
	SelectionChanged gooey.Action
	// Scroll turns a list with no Selected binding into a scroll view —
	// the log-pane shape. It is the offset back from the TAIL, in rows:
	// 0 pins the window to the end so it follows appends, and scrolling
	// up moves into history that stays put while new items arrive. The
	// view Sets it on the scroll keys and the wheel and reads it to place
	// the window; a Selected binding takes precedence, because selection
	// already decides where the window is.
	Scroll *prop.Property[int]
	// Activate runs on enter, on a double click, and on a second click of
	// the already-selected row. It is an Action, so a command with a When
	// condition simply does not fire while the condition is false.
	Activate gooey.Action
	// Template builds one row's subtree from that row's value handles.
	Template ItemFactory
	// Highlight adds the house selection visual: the selected row's cells
	// re-styled Reverse, applied over whatever the template painted.
	// Markup turns it ON unless the template mentions the reserved
	// SelectedKey, which is how a template says it is drawing selection
	// itself. It is opt-IN from Go — a component built in code is drawing
	// its own row, so it should say whether it wants this on top.
	Highlight bool
	// NoFocus takes the view out of the tab order (markup spells it
	// Focusable="false"). It is for lists that are display surfaces for a
	// selection some OTHER component drives — finder's results, whose
	// query line owns the keyboard fzf-style: a click still selects by
	// hit-test, but focus-follows-click finds nothing to move to, so
	// typing never goes dead.
	NoFocus bool
	// Now is the clock wheel velocity is measured against. Nil means
	// time.Now; tests inject a fake so event rate is simulated, not
	// slept — the FocusManager's Now field is the precedent.
	Now func() time.Time

	top     int
	rowH    int
	visible int
	rows    []*itemRow
	kids    []gooey.Component

	// Wheel-velocity state: when the last notch arrived, which way it
	// went, and how many have arrived back-to-back. Plain fields, not
	// properties — a notch's bookkeeping must not be damage.
	wheelAt  time.Time
	wheelDir int
	wheelRun int

	structure       func()
	pressedSelected bool
	err             error
}

// Reserved row values. A projection must not produce these keys — the
// view owns them, because they describe the row's state in the VIEW, not
// anything about the item.
const (
	// SelectedKey is the row's selection state, a *prop.Property[bool].
	SelectedKey = "_selected"
	// HoveredKey is the row's hover state, a *prop.Property[bool].
	HoveredKey = "_hovered"
)

func reserved(k string) bool { return k == SelectedKey || k == HoveredKey }

// ItemSource is what an ItemsView consumes: how many items there are and
// the value map for item i. The interface exists so the collection can
// stay typed on the viewmodel side while the view — and the markup layer
// above it — sees one non-generic type it can name in a binding.
type ItemSource interface {
	Len() int
	At(i int) map[string]any
}

// ItemFactory builds one row's component subtree. The map it receives is
// a binding context: keys are the projection's field names, values are
// the property HANDLES the row will Set as the item changes, so a
// template binds them exactly like it binds a viewmodel property.
type ItemFactory func(values map[string]any) (gooey.Component, error)

// Items adapts a typed slice property to an ItemSource property.
//
// project is where the type system hands over. gooey cannot walk an
// arbitrary struct's fields without reflection, so the app says what a
// row is made of:
//
//	components.Items(stories, func(s Story) map[string]any {
//	    return map[string]any{"Title": s.Title, "Published": s.Published}
//	})
//
// The result is a computed, so reading it inside a paint records a
// dependency on the underlying slice — the list repaints when the
// viewmodel changes, through the ordinary property graph.
func Items[T any](p *prop.Property[[]T], project func(T) map[string]any) *prop.Property[ItemSource] {
	return prop.NewComputed(func() ItemSource {
		return ItemsOf(p.Get(), project)
	})
}

// ItemsOf is the inner half of Items: a slice and a projection, with no
// property around them. It is for a viewmodel that must build the source
// inside its OWN computed, which is what you need the moment a
// projection reads more than the item — a lookup table of what has been
// read, a filter, a formatting mode.
//
// That is not a convenience, it is the dependency rule. A projection runs
// during layout, outside any evaluation, so anything it reads there is
// invisible to the graph. Read it in the computed instead and the list
// repaints when it changes:
//
//	rows := prop.NewComputed(func() components.ItemSource {
//	    marks := read.Get() // recorded: this source depends on it
//	    return components.ItemsOf(stories.Get(), func(s Story) map[string]any {
//	        return map[string]any{"Title": s.Title, "Seen": marks[s.Link]}
//	    })
//	})
func ItemsOf[T any](items []T, project func(T) map[string]any) ItemSource {
	return &sliceSource[T]{items: items, project: project}
}

type sliceSource[T any] struct {
	items   []T
	project func(T) map[string]any
}

func (s *sliceSource[T]) Len() int { return len(s.items) }

func (s *sliceSource[T]) At(i int) map[string]any {
	if i < 0 || i >= len(s.items) {
		return nil
	}
	return s.project(s.items[i])
}

// AcceptsFocus shadows the FocusState mixin so NoFocus can take the view
// out of the tab order without losing the mixin's focused flag (a view
// that opted out simply never has it set).
func (v *ItemsView) AcceptsFocus() bool { return !v.NoFocus }

// SetStructureHook receives the composition's structural-change hook.
// The view calls it when the realized window changes — see dynamic.go.
func (v *ItemsView) SetStructureHook(fn func()) { v.structure = fn }

func (v *ItemsView) ChildComponents() []gooey.Component { return v.kids }

// Err reports a template error from the most recent realization. Markup
// catches most of these at load time (Validate), but a source that was
// empty then has nothing to check against, so the rest surface here.
func (v *ItemsView) Err() error { return v.err }

// Validate builds one throwaway row against the first item, so a typo in
// a template binding fails the LOAD like every other binding in the
// document. An empty source has nothing to check against; those errors
// surface at first realization instead, and are painted into the view.
func (v *ItemsView) Validate() error {
	src := v.source()
	if src == nil || src.Len() == 0 || v.Template == nil {
		return nil
	}
	_, err := v.newRow(0, src.At(0))
	return err
}

func (v *ItemsView) Measure(avail gooey.Size) gooey.Size { return avail }

// Arrange is where realization happens, because arranging is when the
// view finally knows how many rows fit. Every value the rows read is Set
// from here — BEFORE the paint loop of the same frame, so what changed
// during layout paints in this frame rather than the next.
func (v *ItemsView) Arrange(b gooey.Rect) {
	v.Base.Arrange(b)
	src := v.source()
	n := 0
	if src != nil {
		n = src.Len()
	}
	sel := v.selection(n)

	top, count := v.window(b, n, sel)
	v.sync(src, top, count, sel)
	v.place(b)

	// Row height is discovered, not declared: the template decides it, by
	// measuring against the view's full height like any other child. If
	// the first realized row disagrees with the guess the window was
	// computed from, redo the window once with the truth. Once is enough —
	// the second pass uses a measured height, not a guess.
	//
	// A template rooted in something that STRETCHES (a Grid with default
	// star rows) will therefore ask for the whole view and get a
	// one-row list. That is the same answer XAML gives, and the same fix:
	// say what height the row wants — <Grid Rows="1">, or Height="1".
	if h := v.measuredRowH(b); h != v.rowH {
		v.rowH = h
		top, count = v.window(b, n, sel)
		v.sync(src, top, count, sel)
		v.place(b)
	}
	v.top = top
}

// window is the virtualization: which slice of the collection is worth
// building. It also carries the scroll rule — keep the selection visible,
// and never scroll past the end. A scroll-mode list (Scroll set, no
// Selected) anchors to the tail instead: the window ends Scroll rows
// back from the end, which is what makes offset 0 follow appends.
func (v *ItemsView) window(b gooey.Rect, n, sel int) (top, count int) {
	if v.rowH < 1 {
		v.rowH = 1
	}
	v.visible = max(1, b.H/v.rowH)
	if n == 0 || b.H <= 0 {
		return 0, 0
	}
	if v.scrolls() {
		top = max(0, n-v.visible-v.offset(n))
		return top, min(v.visible, n-top)
	}
	top = v.top
	if sel >= 0 {
		if sel < top {
			top = sel
		}
		if sel >= top+v.visible {
			top = sel - v.visible + 1
		}
	}
	top = clamp(top, 0, max(0, n-v.visible))
	return top, min(v.visible, n-top)
}

// sync brings the realized rows in line with the window: reuse the row
// for an index that is still visible (re-projecting its values into the
// handles it already holds), build the ones that are new, drop the rest.
//
// Reuse is the whole economy of this component. A row that keeps its
// index and its values keeps its paint nodes CLEAN, so scrolling by one
// line repaints the rows whose content moved and leaves the composition
// otherwise untouched.
func (v *ItemsView) sync(src ItemSource, top, count, sel int) {
	byIndex := make(map[int]*itemRow, len(v.rows))
	for _, r := range v.rows {
		byIndex[r.index] = r
	}
	next := make([]*itemRow, 0, count)
	for i := top; i < top+count; i++ {
		vals := src.At(i)
		r := byIndex[i]
		if r != nil && !r.accepts(vals) {
			r = nil // the item's SHAPE changed; the template must be rebuilt
		}
		if r == nil {
			nr, err := v.newRow(i, vals)
			if err != nil {
				v.err = err
				continue
			}
			r = nr
		} else {
			r.update(vals)
		}
		r.setSelected(i == sel)
		next = append(next, r)
	}
	if sameRows(v.rows, next) {
		return
	}
	v.rows = next
	v.kids = v.kids[:0]
	for _, r := range next {
		v.kids = append(v.kids, r)
	}
	if v.structure != nil {
		v.structure()
	}
}

func sameRows(a, b []*itemRow) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (v *ItemsView) place(b gooey.Rect) {
	y := b.Y
	for _, r := range v.rows {
		gooey.MeasureChild(r, gooey.Size{W: b.W, H: b.H})
		gooey.ArrangeChild(r, gooey.Rect{X: b.X, Y: y, W: b.W, H: max(0, min(v.rowH, b.Y+b.H-y))})
		y += v.rowH
	}
}

func (v *ItemsView) measuredRowH(b gooey.Rect) int {
	if len(v.rows) == 0 || b.H <= 0 {
		return v.rowH
	}
	return clamp(v.rows[0].desiredH, 1, b.H)
}

func (v *ItemsView) newRow(i int, vals map[string]any) (*itemRow, error) {
	r := &itemRow{view: v, index: i, selected: prop.NewSource(false)}
	handles := make(map[string]any, len(vals)+2)
	r.setters = make(map[string]func(any), len(vals))
	r.keys = make(map[string]bool, len(vals))
	for k, raw := range vals {
		if reserved(k) {
			continue
		}
		r.keys[k] = true
		h, set, touch := rowValue(raw)
		handles[k] = h
		if set != nil {
			r.setters[k] = set
		}
		if touch != nil {
			r.touch = append(r.touch, touch)
		}
	}
	handles[SelectedKey] = r.selected
	handles[HoveredKey] = prop.NewComputed(func() bool { return r.IsHovered() })

	child, err := v.Template(handles)
	if err != nil {
		return nil, err
	}
	r.child, r.kids = child, []gooey.Component{child}
	if v.Highlight {
		r.hi = &rowHighlight{row: r}
		r.kids = append(r.kids, r.hi)
	}
	return r, nil
}

// Render paints nothing — the rows do that. What it does is READ, and
// reading is the whole job: this paint node's dependencies on Items and
// Selected are what turn a viewmodel change into a scheduled frame.
// Without them a new list would sit in the property graph with nothing
// dirty and never reach the screen, because the rows learn about it in
// Arrange and Arrange only runs inside a frame somebody asked for.
//
// It costs one paint per change to the list or the selection, which is
// the honest price of the view being the thing that observes them.
func (v *ItemsView) Render(f *gooey.Frame) {
	src := v.source()
	n := 0
	if src != nil {
		n = src.Len()
	}
	v.selection(n)
	if v.scrolls() {
		v.Scroll.Get() // the scroll offset moves the window: subscribe
	}
	if v.err != nil {
		b := v.Bounds()
		f.Cells.SetString(b.X, b.Y, clipRunes("template: "+v.err.Error(), b.W),
			render.Style{Fg: render.RGB(240, 90, 90)})
	}
}

func (v *ItemsView) source() ItemSource {
	if v.Items == nil {
		return nil
	}
	return v.Items.Get()
}

func (v *ItemsView) count() int {
	if src := v.source(); src != nil {
		return src.Len()
	}
	return 0
}

// selection is the clamped selected index, or -1 when the list is not
// selectable. Called from Render it records the dependency; called from
// Arrange or an input handler it records nothing — read versus
// subscription is decided by the call site, as everywhere in gooey.
func (v *ItemsView) selection(n int) int {
	if v.Selected == nil || n == 0 {
		return -1
	}
	return clamp(v.Selected.Get(), 0, n-1)
}

// scrolls reports scroll mode: a tail-anchored window driven by the
// Scroll offset rather than by a selection.
func (v *ItemsView) scrolls() bool { return v.Selected == nil && v.Scroll != nil }

// offset is the clamped scroll offset. Like selection, what a Get here
// means is decided by the call site.
func (v *ItemsView) offset(n int) int {
	return clamp(v.Scroll.Get(), 0, max(0, n-v.visible))
}

// scrollBy moves the window and reports the gesture consumed. The Set is
// compared so holding a key at either end stays damage-free.
func (v *ItemsView) scrollBy(d, n int) bool {
	if off := clamp(v.offset(n)+d, 0, max(0, n-v.visible)); off != v.Scroll.Get() {
		v.Scroll.Set(off)
	}
	return true
}

// ---- input ----

// Wheel velocity. One notch is one row — the precision that lets a slow
// wheel touch every item — until notches arrive fast enough to read as a
// continuous gesture, at which point the step scales with the LIST, so a
// flick crosses a big collection in a bounded number of notches instead
// of the fixed lines-per-notch that made every flick overshoot to the
// end. The tiers are entered by run length, not by one fast interval, so
// the first notches of any gesture are always precise.
const (
	// wheelFastGap is the longest interval that still sustains a run;
	// terminals deliver a flick's notches a few ms apart, a deliberate
	// slow roll hundreds of ms apart.
	wheelFastGap = 120 * time.Millisecond
	// wheelFastRun and wheelFlickRun are the run lengths entering the
	// fast (~5% of the list) and flick (~15%) tiers.
	wheelFastRun  = 3
	wheelFlickRun = 9
)

func (v *ItemsView) now() time.Time {
	if v.Now != nil {
		return v.Now()
	}
	return time.Now()
}

// wheelStep is how many rows this notch moves, given the recent event
// rate. A pause longer than wheelFastGap or a change of direction resets
// the run to the precise tier. The percentage tiers are floored (2 and 5
// rows) so they outrun the base tier even on a short list.
func (v *ItemsView) wheelStep(n, dir int) int {
	t := v.now()
	if dir == v.wheelDir && !v.wheelAt.IsZero() && t.Sub(v.wheelAt) <= wheelFastGap {
		v.wheelRun++
	} else {
		v.wheelRun = 0
	}
	v.wheelAt, v.wheelDir = t, dir
	switch {
	case v.wheelRun >= wheelFlickRun:
		return max(5, n*15/100)
	case v.wheelRun >= wheelFastRun:
		return max(2, n*5/100)
	}
	return 1
}

func (v *ItemsView) HandleKey(ev input.KeyEvent) bool {
	n := v.count()
	if n == 0 {
		return false
	}
	if v.scrolls() {
		// The scroll keys read like a pager: up is back into history
		// (offset grows), down is toward the tail, end re-follows.
		switch ev {
		case input.Rune('k'), input.Named(input.KeyUp):
			return v.scrollBy(+1, n)
		case input.Rune('j'), input.Named(input.KeyDown):
			return v.scrollBy(-1, n)
		case input.Named(input.KeyPageUp):
			return v.scrollBy(+v.visible, n)
		case input.Named(input.KeyPageDown):
			return v.scrollBy(-v.visible, n)
		case input.Named(input.KeyHome):
			return v.scrollBy(+n, n)
		case input.Named(input.KeyEnd):
			return v.scrollBy(-n, n)
		}
		return false
	}
	sel := v.selection(n)
	// A page is the realized window height, the same stride the scroll
	// mode's PageUp/PageDown use — the two modes must read as one pager.
	page := max(1, v.visible)
	switch ev {
	case input.Rune('j'), input.Named(input.KeyDown):
		return v.selectIndex(sel+1, n)
	case input.Rune('k'), input.Named(input.KeyUp):
		return v.selectIndex(sel-1, n)
	case input.Named(input.KeyPageDown):
		return v.selectIndex(sel+page, n)
	case input.Named(input.KeyPageUp):
		return v.selectIndex(sel-page, n)
	case input.Named(input.KeyHome):
		return v.selectIndex(0, n)
	case input.Named(input.KeyEnd):
		return v.selectIndex(n-1, n)
	case input.Named(input.KeyEnter):
		if !gooey.CanExecute(v.Activate) {
			return false
		}
		v.Activate.Run()
		return true
	}
	return false
}

func (v *ItemsView) HandleMouse(ev input.MouseEvent) bool {
	n := v.count()
	if n == 0 {
		return false
	}
	if v.scrolls() {
		switch ev.Kind {
		case input.WheelUp:
			return v.scrollBy(+v.wheelStep(n, +1), n)
		case input.WheelDown:
			return v.scrollBy(-v.wheelStep(n, -1), n)
		}
		return false
	}
	switch ev.Kind {
	case input.MousePress:
		i, ok := v.indexAt(ev.Y)
		if !ok {
			return false
		}
		// Compared before the selection moves, so the first click on a row
		// selects it and only a second click activates it.
		v.pressedSelected = i == v.selection(n)
		return v.selectIndex(i, n)
	case input.MouseClick:
		// Two ways to activate with the pointer, and they are not the same
		// gesture: a real double click, and a deliberate second click on a
		// row that was already selected (which may be seconds apart).
		if (ev.Count >= 2 || v.pressedSelected) && gooey.CanExecute(v.Activate) {
			v.Activate.Run()
		}
		return true
	case input.WheelUp:
		return v.selectIndex(v.selection(n)-v.wheelStep(n, +1), n)
	case input.WheelDown:
		return v.selectIndex(v.selection(n)+v.wheelStep(n, -1), n)
	}
	return false
}

// indexAt maps a screen row back to an item index through the realized
// rows themselves, so a click lands on the row the user actually sees
// however the window happens to be scrolled.
func (v *ItemsView) indexAt(y int) (int, bool) {
	for _, r := range v.rows {
		b := r.Bounds()
		if y >= b.Y && y < b.Y+b.H {
			return r.index, true
		}
	}
	return 0, false
}

func (v *ItemsView) selectIndex(i, n int) bool {
	if v.Selected == nil {
		return false
	}
	i = clamp(i, 0, n-1)
	if i != v.Selected.Get() {
		v.Selected.Set(i)
		if gooey.CanExecute(v.SelectionChanged) {
			v.SelectionChanged.Run()
		}
	}
	return true
}

// ---- rows ----

// itemRow is one realized row: the template instance, the handles its
// bindings resolved to, and the row's own view state. It is a container,
// so it paints nothing and — critically — does not clear its rectangle;
// the cells belong to the template's components.
type itemRow struct {
	gooey.Base
	gooey.HoverState

	view  *ItemsView
	index int

	child gooey.Component
	hi    *rowHighlight
	kids  []gooey.Component

	keys     map[string]bool
	setters  map[string]func(any)
	touch    []func()
	selected *prop.Property[bool]
	desiredH int
}

func (r *itemRow) ChildComponents() []gooey.Component { return r.kids }

func (r *itemRow) Measure(avail gooey.Size) gooey.Size {
	s := gooey.MeasureChild(r.child, avail)
	if r.hi != nil {
		gooey.MeasureChild(r.hi, avail)
	}
	r.desiredH = s.H
	return s
}

func (r *itemRow) Arrange(b gooey.Rect) {
	r.Base.Arrange(b)
	gooey.ArrangeChild(r.child, b)
	if r.hi != nil {
		gooey.ArrangeChild(r.hi, b)
	}
}

func (r *itemRow) Render(*gooey.Frame) {}

// accepts reports whether a freshly projected map fits the handles this
// row already built its template against. Same keys means the same
// bindings, so the row can be updated in place; anything else is a
// different shape of item and needs a new template instance.
func (r *itemRow) accepts(vals map[string]any) bool {
	n := 0
	for k := range vals {
		if reserved(k) {
			continue
		}
		if !r.keys[k] {
			return false
		}
		n++
	}
	return n == len(r.keys)
}

// update pushes a re-projection into the row's handles. Each setter
// compares before it Sets — an uncompared Set would invalidate every
// dependent every frame and repaint the whole list forever, which is the
// single easiest way to lose the damage guarantee.
func (r *itemRow) update(vals map[string]any) {
	for k, set := range r.setters {
		if raw, ok := vals[k]; ok {
			set(raw)
		}
	}
}

func (r *itemRow) setSelected(on bool) {
	if r.selected.Get() != on {
		r.selected.Set(on)
	}
}

// rowValue wraps one projected value as the handle a template binds to,
// plus the two operations the view needs afterwards: a Set that compares
// first, and a read the highlight overlay uses to stay ordered behind the
// cells it re-styles.
//
// The type switch IS the type system here, as everywhere else in gooey. A
// value of a type this switch does not name crosses as a literal and is
// FIXED for the life of the row — which is exactly what you want for a
// gooey.Command delegate projected onto a row, and what you must not rely
// on for anything that changes.
func rowValue(v any) (handle any, set func(any), touch func()) {
	switch x := v.(type) {
	case string:
		p := prop.NewSource(x)
		return p, func(nv any) {
			if s, ok := nv.(string); ok && s != p.Get() {
				p.Set(s)
			}
		}, func() { p.Get() }
	case bool:
		p := prop.NewSource(x)
		return p, func(nv any) {
			if b, ok := nv.(bool); ok && b != p.Get() {
				p.Set(b)
			}
		}, func() { p.Get() }
	case int:
		p := prop.NewSource(x)
		return p, func(nv any) {
			if n, ok := nv.(int); ok && n != p.Get() {
				p.Set(n)
			}
		}, func() { p.Get() }
	case float64:
		p := prop.NewSource(x)
		return p, func(nv any) {
			if f, ok := nv.(float64); ok && f != p.Get() {
				p.Set(f)
			}
		}, func() { p.Get() }
	case render.Style:
		p := prop.NewSource(x)
		return p, func(nv any) {
			if s, ok := nv.(render.Style); ok && s != p.Get() {
				p.Set(s)
			}
		}, func() { p.Get() }
	case render.Color:
		p := prop.NewSource(x)
		return p, func(nv any) {
			if c, ok := nv.(render.Color); ok && c != p.Get() {
				p.Set(c)
			}
		}, func() { p.Get() }
	case []int:
		// Positions into the item — finder's matched-rune indexes. The
		// compare is by contents, so a re-projection that produces the
		// same positions stays damage-free; the slice is stored as given,
		// which is safe because a projection builds its maps fresh.
		p := prop.NewSource(x)
		return p, func(nv any) {
			if s, ok := nv.([]int); ok && !slices.Equal(s, p.Get()) {
				p.Set(s)
			}
		}, func() { p.Get() }
	}
	return v, nil, nil
}

// rowHighlight is the house selection visual, and it paints no cells of
// its own — it re-styles the ones the row's components painted. That is
// why it reports itself as a Container with no children: a LEAF's paint
// node pre-clears its rectangle (see composer.go), which here would wipe
// the row it is supposed to decorate. It owns no cells; it owns their
// Reverse flag.
//
// Paint order is what makes it correct. It is the row's last child, so
// its node comes after the template's nodes, and while it is on it reads
// every one of the row's values — so a re-projection dirties the content
// AND this overlay, and the reverse is re-applied over the cells that
// were just repainted, in the same frame. While it is OFF it reads only
// `selected`, so an unselected row's content changes cost nothing extra.
// That short-circuit is deliberate: in this graph a read is a
// subscription, so not reading is how you opt out.
type rowHighlight struct {
	gooey.Base
	row *itemRow
	on  bool
}

func (h *rowHighlight) ChildComponents() []gooey.Component { return nil }

// DecoratesCells marks the highlight for the Composer's z-ordered
// repaint: it owns no cells, so a repaint below it has nothing of its to
// restore, and forcing it would charge every content change in an
// unselected row an extra paint. A live (selected) overlay re-applies
// through the dependencies Render records — that is the contract.
func (h *rowHighlight) DecoratesCells() {}

func (h *rowHighlight) Measure(avail gooey.Size) gooey.Size { return avail }

func (h *rowHighlight) Render(f *gooey.Frame) {
	if !h.row.selected.Get() {
		if !h.on {
			return
		}
		h.on = false
		h.apply(f, false)
		return
	}
	for _, touch := range h.row.touch {
		touch()
	}
	h.on = true
	h.apply(f, true)
}

func (h *rowHighlight) apply(f *gooey.Frame, on bool) {
	b := h.Bounds()
	for y := b.Y; y < b.Y+b.H; y++ {
		for x := b.X; x < b.X+b.W; x++ {
			c := f.Cells.At(x, y)
			c.Style.Reverse = on
			f.Cells.Set(x, y, c.Rune, c.Style)
		}
	}
}
