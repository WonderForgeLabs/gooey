package main

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

var (
	accent   = render.Style{Fg: render.RGB(255, 170, 60), Bold: true}
	dim      = render.Style{Fg: render.RGB(140, 140, 150)}
	errStyle = render.Style{Fg: render.RGB(240, 90, 90)}
)

// attr resolves an attribute binding from the parent context to a
// typed property handle — the receiving side of the UserControl
// hand-off.
func attr[T any](e markup.Element, parent *markup.Context, name string) (*prop.Property[T], error) {
	v, err := parent.BindingValue(e.Attrs[name])
	if err != nil {
		return nil, fmt.Errorf("attribute %s: %w", name, err)
	}
	p, ok := v.(*prop.Property[T])
	if !ok {
		var zero *prop.Property[T]
		return nil, fmt.Errorf("attribute %s: got %T, want %T", name, v, zero)
	}
	return p, nil
}

// paneTitle decorates a pane's name with ● while it holds focus. Focus
// is a property like any other, so this computed makes the two panes
// that swap focus the only ones that repaint.
//
// focused is a func rather than the component because a pane's focus
// stop is not always something the setup built: StoryList's is an
// <ItemsView> declared in markup, and the title has to be in the context
// before the markup that creates it is loaded. Asking at paint time
// instead of at setup time costs nothing and works either way.
func paneTitle(name string, focused func() bool) *prop.Property[string] {
	return prop.NewComputed(func() string {
		if focused() {
			return "● " + name
		}
		return name
	})
}

// namedFocus resolves a focus stop by markup name, late. Load fills the
// Named map the control hands it, so a computed that looks the component
// up when it runs — rather than when it was created — sees it.
func namedFocus(named map[string]gooey.Component, name string) func() bool {
	return func() bool {
		w, ok := named[name].(interface{ IsFocused() bool })
		return ok && w.IsFocused()
	}
}

// ---- FeedList ----

// feedListControl is the same shape as StoryList now: feedlist.gooey
// declares an <ItemsView> with an <ItemsView.ItemTemplate>, and this
// setup hands it the item source and the shared selection handle. The
// SelectionChanged attribute crosses the control boundary onto the
// view's own SelectionChanged, so moving the feed cursor still resets
// the story cursor — the event fires on actual change, from any gesture.
func feedListControl(fsys fs.FS) markup.Builder {
	return markup.UserControl(fsys, "feedlist.gooey", func(e markup.Element, parent *markup.Context) (*markup.Context, error) {
		feeds, err := attr[[]*Feed](e, parent, "Feeds")
		if err != nil {
			return nil, err
		}
		sel, err := attr[int](e, parent, "Selected")
		if err != nil {
			return nil, err
		}
		reset, err := parent.Command(e.Attrs["SelectionChanged"])
		if err != nil {
			return nil, err
		}
		named := map[string]gooey.Component{}
		return &markup.Context{
			Named: named,
			Values: map[string]any{
				"Title":    paneTitle(e.Attrs["Title"], namedFocus(named, "Feeds")),
				"Rows":     feedRows(feeds),
				"Selected": sel,
				"Changed":  reset,
			},
		}, nil
	})
}

// feedRows is the feed list's projection: the label decorations the old
// Render loop drew — the loading ellipsis, the error mark, the story
// count — as row values. The projection reads nothing beyond the item
// (a *Feed is replaced wholesale when its fetch lands), so the plain
// Items adapter carries it.
func feedRows(feeds *prop.Property[[]*Feed]) *prop.Property[components.ItemSource] {
	return components.Items(feeds, func(fd *Feed) map[string]any {
		label, style := fd.Title, render.Style{}
		switch {
		case fd.Loading:
			label += " …"
		case fd.Err != nil:
			label += " ✗"
			style = errStyle
		default:
			label = fmt.Sprintf("%s (%d)", label, len(fd.Stories))
		}
		return map[string]any{"Label": oneLine(label), "Style": style}
	})
}

// ---- StoryList ----

// storyListControl is the migrated pane: there is no rows component here
// any more. storylist.gooey declares an <ItemsView> with an
// <ItemsView.ItemTemplate>, and this setup's whole job is to hand it the
// three things markup cannot produce — an item source, the shared
// selection handle, and the open command.
//
// What used to be a Render loop is now a PROJECTION. Every visual
// decision the loop made (which mark, which style, the date) is a value
// in the item's map, and the template places them. The selected row's
// reverse bar, which the loop drew by hand, is the view's house
// highlight. What is left in Go is the part that was never about
// painting: which stories have been read.
func storyListControl(fsys fs.FS) markup.Builder {
	return markup.UserControl(fsys, "storylist.gooey", func(e markup.Element, parent *markup.Context) (*markup.Context, error) {
		stories, err := attr[[]Story](e, parent, "Stories")
		if err != nil {
			return nil, err
		}
		sel, err := attr[int](e, parent, "Selected")
		if err != nil {
			return nil, err
		}
		read, err := attr[map[string]bool](e, parent, "Read")
		if err != nil {
			return nil, err
		}
		// The Open command crosses the control boundary like any other
		// bound value, and lands on the view's Activate — so enter (and a
		// second click) opens a story only while this pane has focus.
		open, err := parent.Command(e.Attrs["Open"])
		if err != nil {
			return nil, err
		}
		named := map[string]gooey.Component{}
		return &markup.Context{
			Named: named,
			Values: map[string]any{
				"Title":    paneTitle(e.Attrs["Title"], namedFocus(named, "Stories")),
				"Rows":     storyRows(stories, read),
				"Selected": sel,
				"Open":     open,
			},
		}, nil
	})
}

// storyRows is the item source. It is built inside a computed BECAUSE
// the projection reads the read-marks map: a projection runs during
// layout, where reads record nothing, so anything beyond the item itself
// has to be read here to become a dependency. Marking a story read then
// repaints the list.
func storyRows(stories *prop.Property[[]Story], read *prop.Property[map[string]bool]) *prop.Property[components.ItemSource] {
	return prop.NewComputed(func() components.ItemSource {
		marks := read.Get()
		return components.ItemsOf(stories.Get(), func(s Story) map[string]any {
			mark, markStyle, titleStyle := "●", accent, render.Style{}
			if marks[s.Link] {
				mark, markStyle, titleStyle = " ", dim, dim
			}
			return map[string]any{
				"Mark":       mark,
				"MarkStyle":  markStyle,
				"Title":      oneLine(s.Title),
				"TitleStyle": titleStyle,
				"Published":  oneLine(s.Published),
			}
		})
	})
}

// oneLine flattens a feed's newlines. A Text measures one row per line,
// and a template's root is what decides a list's row height.
func oneLine(s string) string { return strings.ReplaceAll(s, "\n", " ") }

// ---- ReaderPane ----

// readerPaneControl reports the body component it built through built, so
// the page can move focus to it when a story opens — the control owns
// the instance, the page holds a handle to it.
func readerPaneControl(fsys fs.FS, built func(gooey.Component)) markup.Builder {
	return markup.UserControl(fsys, "readerpane.gooey", func(e markup.Element, parent *markup.Context) (*markup.Context, error) {
		story, err := attr[*Story](e, parent, "Story")
		if err != nil {
			return nil, err
		}
		// The scroll offset crosses the control boundary like Story does,
		// and it belongs to the PAGE rather than to this control for one
		// reason: opening a story has to put the reader back at the top,
		// and the command that opens it lives out there. A pane-private
		// offset would leave the second article opened at the first
		// article's scroll position.
		scroll, err := attr[int](e, parent, "Scroll")
		if err != nil {
			return nil, err
		}
		body := &articleBody{story: story}
		body.scroll.Offset = scroll
		built(body)
		return &markup.Context{
			Values: map[string]any{"Title": paneTitle(e.Attrs["Title"], body.IsFocused)},
			Components: map[string]markup.Builder{
				"ArticleBody": func(markup.Element, *markup.Context) (gooey.Component, error) { return body, nil },
			},
		}, nil
	})
}

// articleBody is the reader's scrolling article pane, and it is a
// PANE-LOCAL viewport: it owns its own line layout and shows a window
// onto it. Why it is not an ItemsView — the framework's other viewport —
// is the subject of docs/specs/2026-08-23-scrolling.md, and the short
// version is that an ItemsView window is built on a uniform row height
// discovered from row 0, while an article's line count is a function of
// this pane's own width, which nothing in the framework can tell a
// projection. What it does share with ItemsView is the scroll MODEL:
// components.Scroller owns the clamp, the compared Set and the wheel
// velocity, so the two panes behave identically under the same gesture.
//
// Everything it does not handle still bubbles to the page bindings, so
// q/esc/a keep working while the reader has focus.
type articleBody struct {
	gooey.Base
	gooey.FocusState
	story  *prop.Property[*Story]
	scroll components.Scroller

	// The last wrap, and the two things it depended on. See lines.
	wrapStory *Story
	wrapWidth int
	wrapped   []articleLine
}

// articleLine is one laid-out display line: text already wrapped to the
// pane width, and the style it is painted in.
type articleLine struct {
	text  string
	style render.Style
}

// lines is the pane's VIEWPORT MODEL — the flat list of display lines a
// scroll offset indexes into. It is rebuilt on demand rather than cached
// because it depends on exactly two things, the story and the pane's own
// width, and both are already in hand wherever it is called.
//
// Which call site calls it decides what the story Get MEANS, as
// everywhere in gooey: from Render it subscribes the pane to the story,
// from a key handler it is a plain read.
func (w *articleBody) lines(width int) []articleLine {
	// The Get stays ABOVE the cache check, and that placement is the
	// whole correctness argument for caching here at all. Called from
	// Render this Get IS the pane's subscription to the story, so a
	// cache that returned early before reaching it would drop the
	// dependency on exactly the frames that hit — and the pane would go
	// deaf to the story changing, with no error and no panic, just a
	// stale article. Reaching the Get is cheap; wrapping is what is not.
	s := w.story.Get()
	if s == nil {
		return nil
	}
	// Pointer identity is a sound key, not a convenient one. `current`
	// (main.go) is a computed that returns &s for a per-iteration copy,
	// so it hands back a FRESH pointer every time it re-evaluates, and
	// it re-evaluates exactly when the story or the selection changes.
	// Same pointer therefore implies same content; changed content
	// implies a new pointer. Nothing writes through a *Story after the
	// computed makes it.
	if s == w.wrapStory && width == w.wrapWidth {
		return w.wrapped
	}
	out := make([]articleLine, 0, 16)
	for _, ln := range wrap(s.Title, width) {
		out = append(out, articleLine{ln, accent})
	}
	meta := s.Published
	if s.Author != "" {
		meta += "  " + s.Author
	}
	out = append(out, articleLine{clipTo(meta, width), dim})
	out = append(out, articleLine{clipTo(s.Link, width), dim})
	out = append(out, articleLine{"", render.Style{}})
	for _, ln := range wrap(s.Body, width) {
		out = append(out, articleLine{ln, render.Style{}})
	}
	w.wrapStory, w.wrapWidth, w.wrapped = s, width, out
	return out
}

// extent is the two numbers every scroll decision needs: how many lines
// the article has at the current width, and how many of them fit.
func (w *articleBody) extent() (lines, viewport int) {
	b := w.Bounds()
	return len(w.lines(b.W)), b.H
}

func (w *articleBody) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *articleBody) Render(f *gooey.Frame) {
	b := w.Bounds()
	lines := w.lines(b.W)
	if len(lines) == 0 {
		// The offset is deliberately NOT read on this path, and that is a
		// dropped dependency on purpose.
		//
		// The usual hazard — a component going deaf to a property because
		// the Get behind an early return never ran — needs the property to
		// be VISIBLE while the component stays clean. Here it cannot be:
		// with no article open there is nothing an offset could move. The
		// empty state ends only when the story changes, and the story IS a
		// dependency of this node, so the frame that brings an article back
		// reads the offset again on the way past.
		//
		// Reading it here would not prevent a stale cell; it would only
		// charge the pane a repaint every time something scrolled an empty
		// reader. TestScrollingAnEmptyReaderIsDamageFree pins the cheap
		// behaviour and TestOffsetSetWhileEmptyStillApplies pins that
		// nothing goes stale for it.
		f.Cells.SetString(b.X, b.Y, "select a story and press enter", dim)
		return
	}
	off := w.scroll.At(len(lines), b.H)
	for i := 0; i < b.H && off+i < len(lines); i++ {
		ln := lines[off+i]
		f.Cells.SetString(b.X, b.Y+i, clipTo(ln.text, b.W), ln.style)
	}
}

// HandleKey is the document anchor: offset 0 is the FIRST line, so k/up
// decreases it and j/down increases it. That is the exact opposite of
// ItemsView's scroll mode, which anchors to the tail so that offset 0
// follows appends — the difference Scroller deliberately leaves to its
// host rather than hiding behind a flag.
func (w *articleBody) HandleKey(ev input.KeyEvent) bool {
	n, h := w.extent()
	if n == 0 {
		return false
	}
	page := max(1, h)
	switch ev {
	case input.Rune('j'), input.Named(input.KeyDown):
		return w.scroll.By(+1, n, h)
	case input.Rune('k'), input.Named(input.KeyUp):
		return w.scroll.By(-1, n, h)
	case input.Named(input.KeyPageDown):
		return w.scroll.By(+page, n, h)
	case input.Named(input.KeyPageUp):
		return w.scroll.By(-page, n, h)
	case input.Named(input.KeyHome):
		return w.scroll.By(-n, n, h)
	case input.Named(input.KeyEnd):
		return w.scroll.By(+n, n, h)
	}
	return false
}

// HandleMouse is the wheel half of the same gesture vocabulary. dir is
// the direction the OFFSET moves, which is all Scroller uses it for — it
// only needs to notice a reversal to reset the velocity run.
func (w *articleBody) HandleMouse(ev input.MouseEvent) bool {
	n, h := w.extent()
	if n == 0 {
		return false
	}
	switch ev.Kind {
	case input.WheelUp:
		return w.scroll.By(-w.scroll.WheelStep(n, -1), n, h)
	case input.WheelDown:
		return w.scroll.By(+w.scroll.WheelStep(n, +1), n, h)
	}
	return false
}

func clampIdx(i, n int) int { return max(0, min(i, n-1)) }

func clipTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) > w {
		r = r[:w]
	}
	return string(r)
}
