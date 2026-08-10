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

// moveKey maps the shared list gestures to a direction. j/k and the
// arrows are view-local: they are handled by whichever pane has focus,
// not by a page-wide binding, so each pane owns its own cursor.
func moveKey(ev input.KeyEvent) (int, bool) {
	switch ev {
	case input.Rune('j'), input.Named(input.KeyDown):
		return +1, true
	case input.Rune('k'), input.Named(input.KeyUp):
		return -1, true
	}
	return 0, false
}

// wheelStep is the conventional three lines per notch. One line per
// notch is technically responsive and reads as broken: in a tall list
// the selection creeps and the view does not move at all until the
// cursor reaches the edge.
const wheelStep = 3

func wheelDelta(ev input.MouseEvent) (int, bool) {
	switch ev.Kind {
	case input.WheelUp:
		return -wheelStep, true
	case input.WheelDown:
		return +wheelStep, true
	}
	return 0, false
}

// ---- FeedList ----

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
		rows := &feedRows{feeds: feeds, sel: sel, changed: reset}
		return &markup.Context{
			Values: map[string]any{"Title": paneTitle(e.Attrs["Title"], rows.IsFocused)},
			Components: map[string]markup.Builder{
				"FeedRows": func(markup.Element, *markup.Context) (gooey.Component, error) { return rows, nil },
			},
		}, nil
	})
}

type feedRows struct {
	gooey.Base
	gooey.FocusState
	feeds   *prop.Property[[]*Feed]
	sel     *prop.Property[int]
	changed gooey.Action
}

func (w *feedRows) Measure(avail gooey.Size) gooey.Size { return avail }

func (w *feedRows) HandleKey(ev input.KeyEvent) bool {
	d, ok := moveKey(ev)
	if !ok {
		return false
	}
	w.selectRow(w.sel.Get() + d)
	return true
}

func (w *feedRows) HandleMouse(ev input.MouseEvent) bool {
	switch ev.Kind {
	case input.MousePress:
		row := ev.Y - w.Bounds().Y
		if row < 0 || row >= len(w.feeds.Get()) {
			return false
		}
		w.selectRow(row)
		return true
	}
	if d, ok := wheelDelta(ev); ok {
		w.selectRow(w.sel.Get() + d)
		return true
	}
	return false
}

func (w *feedRows) selectRow(i int) {
	w.sel.Set(clampIdx(i, len(w.feeds.Get())))
	if gooey.CanExecute(w.changed) {
		w.changed.Run()
	}
}

func (w *feedRows) Render(f *gooey.Frame) {
	b := w.Bounds()
	fs := w.feeds.Get()
	sel := clampIdx(w.sel.Get(), len(fs))
	for i, fd := range fs {
		if i >= b.H {
			break
		}
		st := render.Style{}
		if i == sel {
			st.Reverse = true
			for x := 0; x < b.W; x++ {
				f.Cells.Set(b.X+x, b.Y+i, ' ', st)
			}
		}
		label := fd.Title
		switch {
		case fd.Loading:
			label += " …"
		case fd.Err != nil:
			st = errStyle
			st.Reverse = i == sel
			label += " ✗"
		default:
			label = fmt.Sprintf("%s (%d)", label, len(fd.Stories))
		}
		f.Cells.SetString(b.X, b.Y+i, clipTo(label, b.W), st)
	}
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
