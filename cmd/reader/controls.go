package main

import (
	"fmt"
	"io/fs"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
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
		body := &articleBody{story: story}
		built(body)
		return &markup.Context{
			Values: map[string]any{"Title": paneTitle(e.Attrs["Title"], body.IsFocused)},
			Components: map[string]markup.Builder{
				"ArticleBody": func(markup.Element, *markup.Context) (gooey.Component, error) { return body, nil },
			},
		}, nil
	})
}

// articleBody is a focus stop with no keys of its own: everything it
// does not handle bubbles to the page bindings.
type articleBody struct {
	gooey.Base
	gooey.FocusState
	story *prop.Property[*Story]
}

func (w *articleBody) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *articleBody) Render(f *gooey.Frame) {
	b := w.Bounds()
	s := w.story.Get()
	if s == nil {
		f.Cells.SetString(b.X, b.Y, "select a story and press enter", dim)
		return
	}
	y := b.Y
	for _, ln := range wrap(s.Title, b.W) {
		if y >= b.Y+b.H {
			return
		}
		f.Cells.SetString(b.X, y, ln, accent)
		y++
	}
	meta := s.Published
	if s.Author != "" {
		meta += "  " + s.Author
	}
	f.Cells.SetString(b.X, y, clipTo(meta, b.W), dim)
	y++
	f.Cells.SetString(b.X, y, clipTo(s.Link, b.W), dim)
	y += 2
	for _, ln := range wrap(s.Body, b.W) {
		if y >= b.Y+b.H {
			return
		}
		f.Cells.SetString(b.X, y, ln, render.Style{})
		y++
	}
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
