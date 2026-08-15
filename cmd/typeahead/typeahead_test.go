package main

import (
	"image"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/components"
	"github.com/WonderForgeLabs/gooey/graphics"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/markup"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
	"github.com/WonderForgeLabs/gooey/term"
)

// What is under test is the SHIPPED page, not a fixture that resembles
// it. The demo's claim is about how type-ahead behaves over image rows,
// and that behaviour is a property of this exact markup — its row
// height, its marker column, its lack of a Background.
//
// Every count below is a contract. Composer.Frame returns how many
// components repainted, and that number is the only thing that can pin a
// damage claim: a "the cell says X" assertion passes just as well when
// the whole tree repainted.

const (
	// cols/rows are the geometry the numbers below were measured at. The
	// window fits six four-cell rows out of forty records, which is the
	// interesting regime: most jumps leave it.
	cols, rows = 96, 30
	// visibleRows is how many records fit.
	visibleRows = 6
)

type rig struct {
	c *gooey.Composer
	m *model
}

// newRig loads the shipped page. enc nil means the halfblock tier; a
// non-nil encoder is installed BEFORE the first frame, because the tier
// decides what the first paint records and switching afterwards would
// leave the covers on the plane they were first drawn on.
func newRig(t *testing.T, enc graphics.Encoder) *rig {
	t.Helper()
	b, err := os.ReadFile(pageFile)
	if err != nil {
		t.Fatal(err)
	}
	m := newModel()
	root, err := markup.Load(fstest.MapFS{pageFile: &fstest.MapFile{Data: b}}, pageFile, m.ctx())
	if err != nil {
		t.Fatal(err)
	}
	c := gooey.NewComposer(root, cols, rows)
	c.SetCaps(term.Caps{Cols: cols, Rows: rows, CellW: 10, CellH: 20, Color: render.TrueColor})
	if enc != nil {
		c.SetGraphics(enc)
	}
	c.Focus().Resync()
	// Two frames to reach steady state: the first realizes the rows, the
	// second settles the row height the template turned out to want.
	c.Frame()
	c.Frame()
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("the rig is not at rest: %d components still repainting", n)
	}
	return &rig{c: c, m: m}
}

// typeStr sends runes the way a user produces them, one keystroke each,
// and composes a frame after the last one.
func (r *rig) typeStr(t *testing.T, s string) int {
	t.Helper()
	for _, ch := range s {
		r.c.HandleKey(input.Rune(ch))
	}
	_, n := r.c.Frame()
	return n
}

func (r *rig) screen(t *testing.T) *render.Screen {
	t.Helper()
	s := render.NewScreen(cols, rows)
	if err := r.c.Snapshot(s); err != nil {
		t.Fatal(err)
	}
	return s
}

// title reports the title of the record at the selected index, read
// through the same sorted collection the list is showing.
func (r *rig) title() string {
	recs := r.m.sorted.Get()
	i := r.m.sel.Get()
	if i < 0 || i >= len(recs) {
		return "<none>"
	}
	return recs[i].Title
}

// ---- what a jump costs ----

// A hop that keeps the window still is cheap, and this is the number
// that says so. Eight components: the two marker Texts changing
// visibility, the nodes the Composer restores under the vacated marker
// rectangles, the ItemsView (which reads Selected), and the three status
// lines that read the buffer and the position.
//
// It is eight and not five because the template draws its own selection.
// A list of text rows would use ItemsView's house highlight and pay four
// (two row overlays, the view, the status line); see
// TestHouseHighlightIsWrongOverPixelRows for why this demo cannot.
func TestAJumpInsideTheWindowRepaintsEightComponents(t *testing.T) {
	r := newRig(t, nil)
	// `a` from Aftertone (index 0) searches from AFTER the selection and
	// lands on Alabaster (index 1) — one row down, no scroll.
	if n := r.typeStr(t, "a"); n != 8 {
		t.Fatalf("a jump inside the window repainted %d components, want 8", n)
	}
	if got := r.title(); got != "Alabaster" {
		t.Fatalf("selected %q, want Alabaster", got)
	}
}

// The headline cost, and the reason this demo exists. Type-ahead is a
// mechanism for LONG jumps — that is the whole point of typing a letter
// instead of holding Down — and a long jump scrolls the window, which
// re-realizes every visible row from the template. On picture rows that
// means every cover on screen is rebuilt and re-transmitted for one
// keystroke.
//
// Fifty-two of the tree's components repaint. The exact number is
// pinned rather than a bound, because a change to it is a change to what
// this demo is reporting.
func TestAJumpThatScrollsRepaintsTheWholeList(t *testing.T) {
	r := newRig(t, nil)
	n := r.typeStr(t, "x") // Xebec, the last record in title order
	if got := r.title(); got != "Xebec" {
		t.Fatalf("selected %q, want Xebec", got)
	}
	if n != 52 {
		t.Fatalf("a jump that scrolls repainted %d components, want 52", n)
	}
	// Stated as a ratio too, because the ratio is the finding: the cheap
	// case and the common case are an order of magnitude apart.
	local := newRig(t, nil)
	if hop := local.typeStr(t, "a"); n < hop*6 {
		t.Fatalf("scrolling jump %d is not dramatically dearer than the local hop %d", n, hop)
	}
}

// ---- the two tiers ----

// With a protocol, every visible cover is a placement on the pixel
// plane, and the cells under it are never touched.
func TestCoversGoOutAsPlacementsWhenTheTerminalHasAProtocol(t *testing.T) {
	r := newRig(t, graphics.Kitty{})
	f, _ := r.c.Frame()
	if got := len(f.Placements()); got != visibleRows {
		t.Fatalf("%d placements, want %d — one per visible cover", got, visibleRows)
	}
	// The cover column is cells 2..9 of each row; on this tier they hold
	// nothing, because the picture is composited above them.
	if got := coverRune(r.screen(t), 3); got != ' ' {
		t.Fatalf("cover cell holds %q on the pixel tier, want a blank", got)
	}
}

// Without one, the same tree draws the covers INTO the cell buffer as
// halfblock runes and records no placements. Both paths matter: a demo
// verified only on the tier its author's terminal happens to speak is a
// demo verified on one tier.
func TestCoversFallBackToHalfblockCells(t *testing.T) {
	r := newRig(t, nil)
	f, _ := r.c.Frame()
	if got := len(f.Placements()); got != 0 {
		t.Fatalf("%d placements on the halfblock tier, want 0", got)
	}
	if got := coverRune(r.screen(t), 3); got != '▀' {
		t.Fatalf("cover cell holds %q, want the halfblock rune", got)
	}
}

// coverRune reads a cell from inside the first cover's rectangle. The
// cover occupies columns 2..9 of a row; column 4 is comfortably inside
// it whichever tier is painting.
func coverRune(s *render.Screen, y int) rune {
	return []rune(s.Row(y))[4]
}

// ---- the selection visual ----

// ItemsView's house highlight re-styles the selected row's CELLS as
// Reverse. Over pixel content that is not a selection visual, and this
// pins both halves of why:
//
//   - halfblock: the cover IS the cells, so the highlight photo-negatives
//     the artwork;
//   - a protocol: the cover's cells are blank and the picture is
//     composited above them, so the highlight is invisible where the row
//     is most of what you see.
//
// The demo therefore mentions _selected in its template — which turns
// the house highlight off — and draws a marker column instead, at a cost
// of three extra repainted components per hop.
func TestHouseHighlightIsWrongOverPixelRows(t *testing.T) {
	t.Run("halfblock inverts the cover", func(t *testing.T) {
		c := houseRig(t, nil)
		c.HandleKey(input.Named(input.KeyDown)) // select row 1
		f, _ := c.Frame()
		if !f.Cells.At(4, 4).Style.Reverse {
			t.Fatal("the house highlight did not reach the cover's cells")
		}
		if f.Cells.At(4, 4).Rune != '▀' {
			t.Fatal("the cell under test is not part of the halfblock cover")
		}
	})
	t.Run("a protocol hides it", func(t *testing.T) {
		c := houseRig(t, graphics.Kitty{})
		c.HandleKey(input.Named(input.KeyDown))
		f, _ := c.Frame()
		if f.Cells.At(4, 4).Rune != 0 && f.Cells.At(4, 4).Rune != ' ' {
			t.Fatal("the cover's cells are not blank on the pixel tier")
		}
		var covered bool
		for _, p := range f.Placements() {
			if p.Row <= 4 && 4 < p.Row+p.Rows && p.Col <= 4 && 4 < p.Col+p.Cols {
				covered = true
			}
		}
		if !covered {
			t.Fatal("no placement covers the highlighted cell, so the highlight would be visible")
		}
	})
}

// houseRig is the same list with the HOUSE highlight: a template that
// does not mention _selected, built in Go so the two selection visuals
// can be compared without shipping two pages.
func houseRig(t *testing.T, enc graphics.Encoder) *gooey.Composer {
	t.Helper()
	m := newModel()
	view := &components.ItemsView{
		Items:     m.items(),
		Selected:  m.sel,
		Highlight: true,
		Template: func(vals map[string]any) (gooey.Component, error) {
			return &components.Grid{
				Rows: []components.GridLen{components.Fixed(4)},
				Cols: []components.GridLen{components.Fixed(10), components.Star(1)},
				Children: []gooey.Component{
					cell(&components.Image{
						Src:  vals["Cover"].(*prop.Property[image.Image]),
						Cols: components.Cells(8),
						Rows: components.Cells(4),
					}, 0, 0),
					cell(&components.Text{Content: vals["Title"].(*prop.Property[string])}, 0, 1),
				},
			}, nil
		},
	}
	c := gooey.NewComposer(view, cols, rows)
	c.SetCaps(term.Caps{Cols: cols, Rows: rows, CellW: 10, CellH: 20, Color: render.TrueColor})
	if enc != nil {
		c.SetGraphics(enc)
	}
	c.Focus().Resync()
	c.Frame()
	c.Frame()
	return c
}

func cell(w gooey.Component, row, col int) gooey.Component {
	l := gooey.LayoutOf(w)
	l.Row, l.Col = row, col
	l.HAlign, l.VAlign = gooey.AlignStart, gooey.AlignStart
	return w
}

// ---- the search buffer is the only thing that says the mode is armed ----

// A miss must not move the selection, and must be visible. On a list of
// text rows a user can usually tell a failed search from the fact that
// nothing moved; on six tall picture rows, "nothing moved" and "it moved
// somewhere off-window" look identical, so the state has to be painted.
//
// The damage count is the point: a miss repaints two components — the
// search line and the "no match" text — and touches no row at all.
func TestAMissRepaintsOnlyTheStatusLine(t *testing.T) {
	r := newRig(t, nil)
	r.typeStr(t, "c") // Cinder Lane
	before := r.title()
	n := r.typeStr(t, "q")
	if got := r.title(); got != before {
		t.Fatalf("a miss moved the selection from %q to %q", before, got)
	}
	if n != 2 {
		t.Fatalf("a miss repainted %d components, want 2 — the search line and the miss flag", n)
	}
	if s := r.screen(t); !s.Contains("no match") || !s.Contains("search: cq") {
		t.Fatalf("the miss is not on screen:\n%s", s.Text())
	}
}

// ---- what type-ahead does NOT follow ----

// Key is a load-time literal, so the searched column cannot follow the
// sorted one. Sort by artist and typing still matches titles: `h` lands
// on a TITLE beginning with h, not on Halva's records. This is a
// limitation of the component, recorded as a test because the demo's
// status line makes a claim about it ("searching titles") that has to
// stay true.
func TestSearchDoesNotFollowTheSortColumn(t *testing.T) {
	r := newRig(t, nil)
	r.m.cycleSort() // by artist
	r.c.Frame()
	if got := r.m.sortBy.Get(); got != 1 {
		t.Fatalf("sortBy = %d, want 1 (artist)", got)
	}
	r.typeStr(t, "h")
	if got := r.title(); !strings.HasPrefix(strings.ToLower(got), "h") {
		t.Fatalf("selected %q — typing h must still match a TITLE, whatever the sort", got)
	}
	recs := r.m.sorted.Get()
	if got := recs[r.m.sel.Get()].Artist; strings.HasPrefix(strings.ToLower(got), "h") &&
		!strings.HasPrefix(strings.ToLower(recs[r.m.sel.Get()].Title), "h") {
		t.Fatalf("landed on artist %q — the test is not discriminating", got)
	}
}

// ---- the projection trap ----

// An image projected as a bare value used to cross as a literal — fixed
// for the life of the row, with no setter — because rowValue's switch
// named neither image.Image nor *prop.Property[image.Image]. Re-projecting
// a row, which is exactly what a re-sort does since rows are keyed by
// INDEX, then updated the title and left the picture belonging to whatever
// used to sit at that index (gooey #217).
//
// rowValue names both shapes now, so the demo projects `r.img` directly.
// This pins the behaviour that fix buys: after a re-sort, every realized
// row's Image must hold the art of the record now at its index. The
// framework-level pins live in components/itemsview_test.go; this one is
// end-to-end through the real page, the real template and a real sort.
func TestCoversTravelWithTheRecordAcrossAReSort(t *testing.T) {
	r := newRig(t, nil)
	// Land somewhere with company, then re-sort under the selection.
	r.typeStr(t, "h") // Halcyon
	r.m.cycleSort()   // by artist; every index now means a different record
	r.c.Frame()

	recs := r.m.sorted.Get()
	view := findItemsView(t, r.c.Root())
	seen := 0
	forEachRow(view, func(index int, img image.Image, title string) {
		seen++
		if index >= len(recs) {
			t.Fatalf("row for index %d, but only %d records", index, len(recs))
		}
		want := recs[index]
		if title != want.Title {
			t.Fatalf("row %d shows title %q, want %q", index, title, want.Title)
		}
		if img != want.img {
			t.Fatalf("row %d (%q) is showing another record's cover — the picture did not travel with the record", index, want.Title)
		}
	})
	if seen == 0 {
		t.Fatal("no realized rows were inspected")
	}
}

func findItemsView(t *testing.T, w gooey.Component) *components.ItemsView {
	t.Helper()
	if v, ok := w.(*components.ItemsView); ok {
		return v
	}
	if c, ok := w.(gooey.Container); ok {
		for _, k := range c.ChildComponents() {
			if v := findItemsViewIn(k); v != nil {
				return v
			}
		}
	}
	t.Fatal("no ItemsView in the tree")
	return nil
}

func findItemsViewIn(w gooey.Component) *components.ItemsView {
	if v, ok := w.(*components.ItemsView); ok {
		return v
	}
	if c, ok := w.(gooey.Container); ok {
		for _, k := range c.ChildComponents() {
			if v := findItemsViewIn(k); v != nil {
				return v
			}
		}
	}
	return nil
}

// forEachRow walks the realized rows and reports each one's index, the
// image its Image component is actually holding, and its title. A row
// holds several Texts — the marker bar, the title, the artist, the year
// — so the title is identified by being a title the COLLECTION knows,
// which also makes a row showing a stale title fail loudly rather than
// being mistaken for the marker.
func forEachRow(v *components.ItemsView, fn func(index int, img image.Image, title string)) {
	titles := titleSet(v)
	top := topIndexOf(v, titles)
	for i, row := range v.ChildComponents() {
		img, title := rowContent(row, titles)
		fn(top+i, img, title)
	}
}

func rowContent(row gooey.Component, titles map[string]bool) (image.Image, string) {
	var img image.Image
	var title string
	walk(row, func(w gooey.Component) {
		switch x := w.(type) {
		case *components.Image:
			if img == nil && x.Src != nil {
				img = x.Src.Get()
			}
		case *components.Text:
			if title == "" && x.Content != nil && titles[x.Content.Get()] {
				title = x.Content.Get()
			}
		}
	})
	return img, title
}

func titleSet(v *components.ItemsView) map[string]bool {
	src := v.Items.Get()
	out := make(map[string]bool, src.Len())
	for i := range src.Len() {
		if s, ok := src.At(i)["Title"].(string); ok {
			out[s] = true
		}
	}
	return out
}

// topIndexOf recovers the first realized index by matching the first
// row's title against the collection — the view does not publish its
// window, and a test that guessed it would be pinning the guess.
func topIndexOf(v *components.ItemsView, titles map[string]bool) int {
	kids := v.ChildComponents()
	if len(kids) == 0 {
		return 0
	}
	_, first := rowContent(kids[0], titles)
	src := v.Items.Get()
	for i := range src.Len() {
		if s, ok := src.At(i)["Title"].(string); ok && s == first {
			return i
		}
	}
	return 0
}

func walk(w gooey.Component, fn func(gooey.Component)) {
	fn(w)
	if c, ok := w.(gooey.Container); ok {
		for _, k := range c.ChildComponents() {
			walk(k, fn)
		}
	}
}
