// typeahead puts <TypeAhead> — Windows Explorer's type-ahead find —
// on a list whose rows are PICTURES, to find out what Explorer semantics
// feel like when a row's identity is art rather than a line of text.
//
//	typeahead                 detect the graphics protocol, run
//	typeahead --mode=sixel    force one (kitty|sixel|iterm2|halfblock)
//	typeahead --dump          one frame to stdout, no tty control
//	typeahead --hold=3s       exit after a while instead of on a key
//
// Type a letter and the selection jumps to the first record whose title
// begins with it, in the list's CURRENT sort order, wrapping at the end.
// Nothing is filtered — no row is ever hidden — which is what makes
// "any movement resets the search" coherent. Repeat a letter to cycle
// through the records that start with it; pause a second and the buffer
// is dropped.
//
//	type       jump to a title            ctrl+s  cycle the sort column
//	↑ ↓        move (and reset the buffer) ctrl+r  reverse the sort
//	esc        drop the buffer             ctrl+q  quit
//
// # What this demo is FOR
//
// Three things are hard to see on a list of text rows and obvious here:
//
//  1. Image rows are TALL. A cover is four cells high, so a normal
//     terminal shows six or seven of them. Type-ahead is a jump into an
//     unfiltered list, and a jump you cannot see the neighbours of is
//     a teleport — the status line under the list is not decoration, it
//     is the only thing telling you where you landed.
//
//  2. A jump that leaves the visible window re-realizes every row, so a
//     single keystroke re-transmits a screenful of pictures. The footer
//     reports both currencies of that: components repainted and bytes
//     written.
//
//  3. Type-ahead matches ONE projected value (Key="Title"), fixed at
//     load time. Sort by artist with ctrl+s and typing still searches
//     titles, because Key is not bindable. The status line says which
//     column is being searched for exactly that reason.
//
// # Why the demo draws its own selection
//
// The item template mentions the reserved _selected value, which turns
// off ItemsView's house highlight. That is not a style preference. The
// house highlight re-styles the row's CELLS as Reverse, and a cover's
// cells are either empty (a graphics protocol paints over them, so the
// highlight is invisible) or the picture itself (halfblock, so the
// highlight photo-negatives the art). Neither is a selection visual.
// A marker column is legible in both tiers.
//
// # Why the accelerators are all modified keys
//
// Key dispatch offers KeyBindings before behaviour attachments, so a
// KeyBinding on a bare letter would take that letter out of the
// searchable alphabet permanently and silently. On a page with a
// type-ahead list every unmodified single-key accelerator in the focused
// chain is a letter the user can no longer type.
package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"os"
	"sort"
	"strings"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

func main() {
	mode := flag.String("mode", "", "force graphics mode: kitty|sixel|iterm2|halfblock")
	dump := flag.Bool("dump", false, "render one frame to stdout (no tty control)")
	hold := flag.Duration("hold", 0, "exit after this duration instead of waiting for a quit key")
	flag.Parse()

	enc, forced, err := encoderFor(*mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	vm := newModel()
	dir := os.DirFS(pageDir())
	if *dump {
		root, err := markup.Load(dir, pageFile, vm.ctx())
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		caps := term.Caps{Cols: 96, Rows: 30, CellW: 10, CellH: 20, Color: render.TrueColor}
		gooey.Compose(root, caps, enc).Flush(os.Stdout)
		fmt.Println()
		return
	}

	opts := []gooey.Option{}
	if forced {
		// A forced protocol still needs a cell size: sixel scales by it,
		// and a zero CellW emits an empty image while Image skips the
		// halfblock path — a black screen with no error.
		opts = append(opts,
			gooey.WithGraphics(enc),
			gooey.WithCaps(term.Caps{CellW: 10, CellH: 20, Color: term.DetectColorDepth()}))
	} else {
		opts = append(opts, gooey.WithCapabilityProbe())
	}

	app := gooey.NewApp(markup.Page(dir, pageFile, vm.ctx()), opts...)
	vm.quit = gooey.Command(app.Quit)
	app.BeforeFrame(func() { vm.refresh(app) })
	if *hold > 0 {
		app.Every(*hold, app.Quit)
	}
	gooey.Exit(app.Run(context.Background()))
}

const pageFile = "typeahead.gooey"

// pageDir is the demo's own directory, so `go run ./cmd/typeahead`
// from the repository root finds the markup — the same convention the
// other markup demos use.
func pageDir() string {
	if _, err := os.Stat(pageFile); err == nil {
		return "."
	}
	return "cmd/typeahead"
}

func encoderFor(mode string) (enc graphics.Encoder, forced bool, err error) {
	switch mode {
	case "":
		return nil, false, nil // capabilities decide
	case "kitty":
		return graphics.Kitty{}, true, nil
	case "sixel":
		return graphics.Sixel{}, true, nil
	case "iterm2":
		return graphics.ITerm2{}, true, nil
	case "halfblock":
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("unknown mode: %s", mode)
	}
}

// record is one item. Its picture is a plain image.Image, and getting it
// onto the screen is the single hardest thing in this demo.
//
// ItemsView projects an item into a map[string]any and wraps each value
// in a handle the template binds — but only for the types rowValue's
// switch names (components/itemsview.go:717). image.Image is not one of
// them, and neither is *prop.Property[image.Image]: both fall through to
// the default, which returns the value AS the handle with no setter. So
// whatever a row's <Image Src> resolves to on the frame the row is built
// is what that row shows FOREVER.
//
// Rows are keyed by collection INDEX and reused, so re-sorting re-projects
// row 22 with a different record: the title updates (strings have a
// setter), the picture does not. You get one record's name over another
// record's cover, with no error anywhere.
//
// The way out is `shown` below: the model owns one image property per
// POSITION, hands the row that stable handle, and writes the new picture
// into it whenever the order changes. The row's binding never has to be
// replaced because it was never per-record in the first place.
type record struct {
	Title  string
	Artist string
	Year   int
	img    image.Image
}

// shown is a record in a position: the record's data, plus the image
// property that BELONGS TO THAT POSITION and is what the row at that
// index binds.
type shown struct {
	record
	art *prop.Property[image.Image]
}

type model struct {
	all    []record
	slots  []*prop.Property[image.Image] // one per position, never replaced
	sortBy *prop.Property[int]           // 0 title, 1 artist, 2 year
	desc   *prop.Property[bool]
	sorted *prop.Property[[]shown]
	sel    *prop.Property[int]
	typed  *prop.Property[string]
	missed *prop.Property[bool]
	stats  *prop.Property[string]
	quit   gooey.Action
}

var sortNames = []string{"title", "artist", "year"}

func newModel() *model {
	m := &model{
		all:    catalogue(),
		sortBy: prop.NewSource(0),
		desc:   prop.NewSource(false),
		sel:    prop.NewSource(0),
		typed:  prop.NewSource(""),
		missed: prop.NewSource(false),
		stats:  prop.NewSource(""),
	}
	m.slots = make([]*prop.Property[image.Image], len(m.all))
	for i := range m.slots {
		m.slots[i] = components.Img(nil)
	}
	// The order is a computed over the two sort properties, so changing
	// either one invalidates the collection through the ordinary graph and
	// the list re-projects. No refresh call anywhere.
	//
	// The computed only PAIRS each position with its slot property. It
	// does not write the pictures — a Set inside an evaluation would
	// invalidate dependents mid-evaluation. syncSlots does that, from the
	// command handler, outside any evaluation.
	m.sorted = prop.NewComputed(func() []shown {
		out := append([]record(nil), m.all...)
		by, down := m.sortBy.Get(), m.desc.Get()
		sort.SliceStable(out, func(i, j int) bool {
			a, b := out[i], out[j]
			var less bool
			switch by {
			case 1:
				less = strings.ToLower(a.Artist) < strings.ToLower(b.Artist)
			case 2:
				less = a.Year < b.Year
			default:
				less = strings.ToLower(a.Title) < strings.ToLower(b.Title)
			}
			if down {
				return !less
			}
			return less
		})
		res := make([]shown, len(out))
		for i, r := range out {
			res[i] = shown{record: r, art: m.slots[i]}
		}
		return res
	})
	m.syncSlots()
	return m
}

// syncSlots writes the picture of the record now in each position into
// that position's property. It is the price of a row binding that can
// never be re-pointed: the app has to move the pixels to the row instead
// of moving the row to the pixels.
//
// Runs from a command handler, outside any evaluation, so these Gets
// subscribe to nothing; each Set is compared, so an order that did not
// actually move a picture costs no repaint.
func (m *model) syncSlots() {
	for i, s := range m.sorted.Get() {
		if m.slots[i].Get() != s.img {
			m.slots[i].Set(s.img)
		}
	}
}

// items is the ItemSource the page binds.
func (m *model) items() *prop.Property[components.ItemSource] {
	return components.Items(m.sorted, func(r shown) map[string]any {
		return map[string]any{
			"Title":  r.Title,
			"Artist": r.Artist,
			"Year":   fourDigits(r.Year),
			"Cover":  r.art,
			// Every row projects the same four-cell bar; it is the marker
			// column's content, shown only while the row is selected. A
			// template's context is the ITEM and nothing else, so a
			// constant a row needs has to arrive as a projected value.
			"Bar": "▌\n▌\n▌\n▌",
		}
	})
}

// resort changes the ordering and keeps the SELECTED RECORD selected.
//
// Selected is an index, and a re-sort moves every record to a different
// one. Nothing in the framework preserves the selection across that: the
// index that meant `Halcyon` before the sort means whatever landed there
// after it. An app that re-sorts a selectable list has to do this itself,
// and a type-ahead list makes the omission loud — you type to land on a
// record, change the sort to see it in context, and the selection is on
// someone else's record.
func (m *model) resort(mutate func()) {
	var was string
	if cur := m.sorted.Get(); m.sel.Get() >= 0 && m.sel.Get() < len(cur) {
		was = cur[m.sel.Get()].Title
	}
	mutate()
	m.syncSlots()
	for i, r := range m.sorted.Get() {
		if r.Title == was {
			if m.sel.Get() != i {
				m.sel.Set(i)
			}
			return
		}
	}
}

func (m *model) cycleSort() {
	m.resort(func() { m.sortBy.Set((m.sortBy.Get() + 1) % len(sortNames)) })
}

func (m *model) reverse() {
	m.resort(func() { m.desc.Set(!m.desc.Get()) })
}

// refresh writes the footer. It runs in BeforeFrame — outside any
// evaluation — so the Gets here subscribe to nothing; the Set is what
// schedules the repaint, and it is compared so an unchanged footer costs
// no frame.
func (m *model) refresh(app *gooey.App) {
	s := fmt.Sprintf("painted %d components · %d bytes · %s",
		app.PaintedLastFrame(), app.FlushBytes(), modeName(app.Composer().Graphics()))
	if m.stats.Get() != s {
		m.stats.Set(s)
	}
}

// modeName says which tier the pictures are going out on. There is no
// App accessor for it because capabilities are fixed for a session; the
// composition holds the encoder the probe chose.
func modeName(enc graphics.Encoder) string {
	switch enc.(type) {
	case nil:
		return "halfblock (cells)"
	case graphics.Kitty:
		return "kitty"
	case graphics.Sixel:
		return "sixel"
	case graphics.ITerm2:
		return "iterm2"
	}
	return "pixels"
}

func (m *model) ctx() *markup.Context {
	dim := render.Style{Fg: render.RGB(140, 140, 155)}
	return &markup.Context{
		Values: map[string]any{
			"Records": m.items(),
			"Sel":     m.sel,
			"Typed":   m.typed,
			"Missed":  m.missed,
			"Stats":   m.stats,
			"Sorted": prop.NewComputed(func() string {
				dir := "asc"
				if m.desc.Get() {
					dir = "desc"
				}
				return fmt.Sprintf("sorted by %s (%s) · searching titles", sortNames[m.sortBy.Get()], dir)
			}),
			// The search buffer is the mode indicator. An implicitly armed
			// mode that shows nothing is a UI misrepresenting what the next
			// keystroke will do — and on picture rows there is no text
			// cursor anywhere to suggest that typing does something.
			"Search": prop.NewComputed(func() string {
				if s := m.typed.Get(); s != "" {
					return "search: " + s
				}
				return "type a title…"
			}),
			"Where": prop.NewComputed(func() string {
				rows := m.sorted.Get()
				i := m.sel.Get()
				if i < 0 || i >= len(rows) {
					return ""
				}
				return fmt.Sprintf("%d/%d  %s — %s", i+1, len(rows), rows[i].Title, rows[i].Artist)
			}),
			"CycleSort": gooey.Command(m.cycleSort),
			"Reverse":   gooey.Command(m.reverse),
			"Quit":      gooey.Command(func() { m.runQuit() }),
		},
		Styles: map[string]render.Style{
			"dim":    dim,
			"accent": {Fg: render.RGB(255, 170, 60), Bold: true},
			"title":  {Fg: render.RGB(235, 235, 245), Bold: true},
			"miss":   {Fg: render.RGB(240, 90, 90), Bold: true},
		},
	}
}

func (m *model) runQuit() {
	if gooey.CanExecute(m.quit) {
		m.quit.Run()
	}
}
