package components

import (
	"strings"
	"testing"

	"github.com/WonderForgeLabs/gooey"
	"github.com/WonderForgeLabs/gooey/input"
	"github.com/WonderForgeLabs/gooey/prop"
	"github.com/WonderForgeLabs/gooey/render"
)

// Everything a component measures is a COLUMN count — Measure's W is
// cells, a popup's width() is cells, a mnemonic's column is cells.
// Counting runes was the same number only for ASCII, and every fixture
// in this package's other suites is ASCII, which is why ~20 sites could
// count the wrong thing with the suite green.
//
// The discriminator used throughout is a PAIR of labels with the same
// column width and different rune counts: "世界" is 2 runes and 4
// columns, "abcd" is 4 of each. A correct component measures them the
// same; a rune-counting one does not. Pairing this way means no test
// here has to know any component's chrome constants, so none of them
// break when a border or a padding changes.

const (
	wideLabel   = "世界"   // 2 runes, 4 columns
	narrowLabel = "abcd" // 4 runes, 4 columns
)

func TestMeasureCountsColumnsNotRunes(t *testing.T) {
	avail := gooey.Size{W: 80, H: 24}
	for _, c := range []struct {
		name string
		make func(label string) gooey.Component
	}{
		{"Button", func(l string) gooey.Component {
			return &Button{Content: Str(l), Click: gooey.Command(func() {})}
		}},
		{"Button/pixel-chrome", func(l string) gooey.Component {
			return &Button{Content: Str(l), Chrome: ChromePixel, Click: gooey.Command(func() {})}
		}},
		{"Checkbox", func(l string) gooey.Component {
			return &Checkbox{Label: prop.NewSource(l)}
		}},
		{"Toggle", func(l string) gooey.Component {
			return &Toggle{Label: prop.NewSource(l)}
		}},
		{"Spinner", func(l string) gooey.Component {
			return &Spinner{Label: prop.NewSource(l)}
		}},
	} {
		wide := c.make(wideLabel).Measure(avail)
		narrow := c.make(narrowLabel).Measure(avail)
		if wide.W != narrow.W {
			t.Errorf("%s.Measure().W = %d for %q but %d for %q — both are 4 "+
				"columns wide, so the two must measure the same. A rune count "+
				"sees 2 and 4 and asks the layout for a box two cells too narrow",
				c.name, wide.W, wideLabel, narrow.W, narrowLabel)
		}
	}
}

// The same property for the popups, which size themselves rather than
// being measured — a Toast, a drag ghost, a tooltip.
func TestPopupWidthsCountColumns(t *testing.T) {
	for _, c := range []struct {
		name  string
		width func(label string) int
	}{
		{"Toast", func(l string) int { return (&Toast{Text: l}).width() }},
		{"DragGhost", func(l string) int {
			return (&DragGhost{Label: prop.NewSource(l)}).width()
		}},
		{"tipPopup", func(l string) int {
			return (&tipPopup{tip: &Tooltip{Text: prop.NewSource(l)}}).width()
		}},
	} {
		if wide, narrow := c.width(wideLabel), c.width(narrowLabel); wide != narrow {
			t.Errorf("%s.width() = %d for %q but %d for %q — both are 4 columns",
				c.name, wide, wideLabel, narrow, narrowLabel)
		}
	}
}

// A MenuBar lays its titles out end to end, so a mis-measured title
// displaces every title after it AND the dropdown that opens under it —
// the click target and the paint come apart.
func TestMenuBarTitleSpansCountColumns(t *testing.T) {
	spans := func(title string) (int, int) {
		m := &MenuBar{Menus: []Menu{
			{Title: title},
			{Title: "Edit"},
		}}
		m.Arrange(gooey.Rect{X: 0, Y: 0, W: 40, H: 1})
		_, w0 := m.titleSpan(0)
		x1, _ := m.titleSpan(1)
		return w0, x1
	}
	wideW, wideX := spans(wideLabel)
	narrowW, narrowX := spans(narrowLabel)
	if wideW != narrowW {
		t.Errorf("title span for %q is %d columns, for %q it is %d — both are 4",
			wideLabel, wideW, narrowLabel, narrowW)
	}
	if wideX != narrowX {
		t.Errorf("the SECOND title starts at column %d after %q and %d after %q "+
			"— a mis-measured title shifts every one after it", wideX, wideLabel,
			narrowX, narrowLabel)
	}
}

// The mnemonic underline is a different failure from the widths above:
// splitMnemonic reports a RUNE index, and four painters used it directly
// as a column offset. With a wide glyph before the accelerator the
// underline drifts left of the letter it names.
func TestMnemonicUnderlineLandsOnItsLetter(t *testing.T) {
	// "世_x" — the accelerator is x, at rune index 1 and COLUMN 2.
	if got, want := mnemonicCol("世x", 1), 2; got != want {
		t.Fatalf("mnemonicCol(%q, 1) = %d, want %d — 世 occupies two columns",
			"世x", got, want)
	}
	// pos < 0 means no mnemonic, and must not index into the runes.
	if got := mnemonicCol("世x", -1); got != 0 {
		t.Errorf("mnemonicCol(%q, -1) = %d, want 0", "世x", got)
	}
	// Nor may an out-of-range pos panic; the callers derive it from a
	// different string than they paint in at least one place.
	if got, want := mnemonicCol("世x", 99), 3; got != want {
		t.Errorf("mnemonicCol(%q, 99) = %d, want %d (the whole string)", "世x", got, want)
	}

	// And the painted cell agrees. THE ASSERTION IS NOT "find the x and
	// check it is underlined" — that was the first version of this test
	// and it could not fail. Advancing by a rune index writes the
	// accelerator INTO the continuation cell of the wide glyph, leaving
	// two x's in the row, and a left-to-right search matches the
	// corrupted one, which is underlined. The bug manufactured its own
	// decoy.
	//
	// So: two labels of the same COLUMN width and different rune counts
	// must underline the same columns, and the continuation cell must
	// still be a continuation. Two mechanisms, because the pair
	// comparison alone cannot see a row that is corrupt in both arms.
	for _, c := range []struct {
		name   string
		chrome ButtonChrome
	}{
		{"cell", ChromeCell},
		{"pixel", ChromePixel},
	} {
		paint := func(label string) *render.Buffer {
			buf := render.NewBuffer(24, 4)
			b := &Button{Content: Str(label), Chrome: c.chrome, Click: gooey.Command(func() {})}
			b.Arrange(gooey.Rect{X: 0, Y: 0, W: 24, H: 4})
			b.Render(&gooey.Frame{Cells: buf})
			return buf
		}
		// "世_x" and "ab_x" are both three columns of text with the
		// accelerator in the last one, so they must underline alike.
		wide, narrow := paint("世_x"), paint("ab_x")
		for y := 0; y < 4; y++ {
			gotW, gotN := underlined(wide, y), underlined(narrow, y)
			if !sameInts(gotW, gotN) {
				t.Errorf("%s row %d: %q underlines %v but %q underlines %v — both "+
					"put the accelerator in the third column, so a column offset "+
					"gives the same answer and a rune index does not",
					c.name, y, "世_x", gotW, "ab_x", gotN)
			}
		}
		// And the second column of 世 is still the marker that suppresses
		// it, not a letter written on top.
		if x, y, ok := runeAt(wide, '世'); ok {
			if got := wide.At(x+1, y).Rune; got != render.Continuation {
				t.Errorf("%s: column %d holds %q — that is 世's second column and "+
					"must stay a continuation marker; a rune-indexed painter "+
					"writes the accelerator into it and corrupts the row",
					c.name, x+1, string(got))
			}
		}
	}
}

func sameInts(a, b []int) bool {
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

func runeAt(b *render.Buffer, want rune) (int, int, bool) {
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			if b.At(x, y).Rune == want {
				return x, y, true
			}
		}
	}
	return 0, 0, false
}

func underlined(b *render.Buffer, y int) []int {
	var out []int
	for x := 0; x < b.W; x++ {
		if b.At(x, y).Style.Underline {
			out = append(out, x)
		}
	}
	return out
}

// A meter writes a CLIPPED label and then advances past it. Those used to
// be two different strings — clipCols(label, b.W) written, len([]rune(
// label)) advanced — so the bar started in the wrong column twice over.
func TestMeterBarStartsWhereTheLabelEnds(t *testing.T) {
	const w = 34
	for _, c := range []struct {
		name  string
		paint func(label string, buf *render.Buffer)
	}{
		{"Gauge", func(l string, buf *render.Buffer) {
			g := &Gauge{Value: prop.NewSource(0), Label: prop.NewSource(l)}
			g.Arrange(gooey.Rect{X: 0, Y: 0, W: w, H: 1})
			g.Render(&gooey.Frame{Cells: buf})
		}},
		{"ProgressBar", func(l string, buf *render.Buffer) {
			p := &ProgressBar{Value: prop.NewSource(0), Label: prop.NewSource(l)}
			p.Arrange(gooey.Rect{X: 0, Y: 0, W: w, H: 1})
			p.Render(&gooey.Frame{Cells: buf})
		}},
	} {
		wideBuf, narrowBuf := render.NewBuffer(w, 1), render.NewBuffer(w, 1)
		c.paint(wideLabel, wideBuf)
		c.paint(narrowLabel, narrowBuf)
		// The label occupies four columns either way, so the track must
		// begin at the same column and be the same length.
		if got, want := trackStart(wideBuf), trackStart(narrowBuf); got != want {
			t.Errorf("%s track starts at column %d after the 4-column label %q, "+
				"but at %d after the 4-column label %q", c.name, got, wideLabel,
				want, narrowLabel)
		}
	}
}

// trackStart is the first column of the meter's track — the first cell
// that is neither label text nor blank.
func trackStart(b *render.Buffer) int {
	for x := 0; x < b.W; x++ {
		switch r := b.At(x, 0).Rune; r {
		case '░', '█':
			return x
		}
	}
	return -1
}

// A Segmented sizes itself from its OPTIONS rather than a label, through
// segWidth — a separate arithmetic site from Toggle's, and one the label
// pair above cannot reach.
func TestSegmentedCountsOptionColumns(t *testing.T) {
	measure := func(opts []string) int {
		return (&Segmented{Options: prop.NewSource(opts)}).
			Measure(gooey.Size{W: 200, H: 4}).W
	}
	if wide, narrow := measure([]string{"世界", "世界"}), measure([]string{"abcd", "abcd"}); wide != narrow {
		t.Errorf("Segmented.Measure().W = %d for two 4-column options but %d for "+
			"two 4-column ASCII options", wide, narrow)
	}
}

// The validation popup sizes itself from the error message, which is the
// one string on screen most likely to be localised.
func TestValidationPopupCountsColumns(t *testing.T) {
	size := func(msg string) int {
		return (&markerPopup{m: &ValidationMarker{Error: prop.NewSource(msg)}}).size().W
	}
	if wide, narrow := size(wideLabel), size(narrowLabel); wide != narrow {
		t.Errorf("markerPopup.size().W = %d for %q but %d for %q — both are 4 columns",
			wide, wideLabel, narrow, narrowLabel)
	}
}

// THE MENU BAR'S PAINT AND ITS HIT TEST ARE TWO SEPARATE WALKS over the
// same titles, each advancing by its own arithmetic. titleSpan is what a
// click resolves against and what positions the dropdown; the Render
// loop is what the user sees. When they disagree, clicking a title opens
// the menu belonging to its neighbour — and neither walk is wrong on its
// own terms, which is why this needs an assertion that compares them.
func TestMenuBarPaintAgreesWithItsHitTest(t *testing.T) {
	for _, titles := range [][]string{
		{"世界", "Edit", "View"},
		{"abcd", "Edit", "View"},
	} {
		menus := make([]Menu, len(titles))
		for i, ti := range titles {
			menus[i] = Menu{Title: ti}
		}
		m := &MenuBar{Menus: menus}
		buf := render.NewBuffer(40, 1)
		m.Arrange(gooey.Rect{X: 0, Y: 0, W: 40, H: 1})
		m.Render(&gooey.Frame{Cells: buf})

		for i, ti := range titles {
			x, _ := m.titleSpan(i)
			// Each title is painted as " Title ", so the first column of
			// the text itself is one past the span's start.
			want := []rune(ti)[0]
			if got := buf.At(x+1, 0).Rune; got != want {
				t.Errorf("titles %v: titleSpan(%d) says title %q starts at column %d, "+
					"but the painted row has %q there — the hit test and the paint "+
					"have advanced by different arithmetic, so a click lands on the "+
					"wrong menu", titles, i, ti, x, string(got))
			}
		}
	}
}

// The menu's own mnemonic underlines, on the bar and in the dropdown —
// two more painters that took a rune index for a column. Driven through
// a Composer, because opening the dropdown is what puts the second
// painter on screen.
func TestMenuMnemonicUnderlinesAtAColumn(t *testing.T) {
	paint := func(title, item string) *render.Buffer {
		bar := &MenuBar{Menus: []Menu{{
			Title: title,
			Items: []MenuItem{{Text: item, Action: gooey.Command(func() {})}},
		}}}
		c := gooey.NewComposer(&Canvas{Children: []gooey.Component{bar}}, 40, 8)
		c.Frame()
		c.Focus().SetFocus(bar)
		c.Frame()
		c.HandleKey(input.Named(input.KeyEnter))
		c.Frame()
		return c.Cells()
	}
	// Three columns of text in every arm, accelerator in the third.
	wide, narrow := paint("世_x", "世_y"), paint("ab_x", "ab_y")
	for y := 0; y < 8; y++ {
		gotW, gotN := underlined(wide, y), underlined(narrow, y)
		if !sameInts(gotW, gotN) {
			t.Errorf("row %d: the wide-glyph menu underlines %v, the ASCII one %v — "+
				"the accelerator is the third column in both, so a column offset "+
				"gives the same answer and a rune index does not", y, gotW, gotN)
		}
	}
}

// centerCols pads to exactly w CELLS. Its own doc comment said "cells"
// while it counted runes, so the pill label of a button holding a wide
// glyph was centred by the wrong number.
func TestCenterColsCentresByColumn(t *testing.T) {
	for _, c := range []struct {
		in   string
		w    int
		want string
	}{
		{"世界", 8, "  世界  "},
		{"abcd", 8, "  abcd  "},
		{"世", 5, " 世  "},
		{"abc", 3, "abc"},
		{"世界", 3, "世 "},
		{"", 3, "   "},
		{"abc", 0, ""},
	} {
		got := centerCols(c.in, c.w)
		if got != c.want {
			t.Errorf("centerCols(%q, %d) = %q, want %q", c.in, c.w, got, c.want)
		}
		if n := render.StringWidth(got); c.w > 0 && n != c.w {
			t.Errorf("centerCols(%q, %d) is %d columns, not %d — it pads to an "+
				"EXACT width and a caller writes it into a fixed slot",
				c.in, c.w, n, c.w)
		}
	}
}

// paintedCols is the rightmost column any row of b actually puts ink in.
// For a dropdown that is the popup's right edge, which is what its
// measured width decides.
func paintedCols(b *render.Buffer) int {
	last := -1
	for y := 0; y < b.H; y++ {
		for x := 0; x < b.W; x++ {
			if t := b.At(x, y).Text(); t != "" && t != " " && x > last {
				last = x
			}
		}
	}
	return last
}

// rowHolding returns the first row of b whose text contains sub, or "".
func rowHolding(b *render.Buffer, sub string) string {
	for y := 0; y < b.H; y++ {
		if r := render.RowText(b, y); strings.Contains(r, sub) {
			return r
		}
	}
	return ""
}

// The dropdown's WIDTH — the half TestMenuMnemonicUnderlinesAtAColumn
// cannot see. That test pins where the accelerator lands; this one pins
// how many cells the popup claimed in the first place.
//
// `iw` in Menu.width (menu.go) is measured from the item labels, so
// counting runes sizes a CJK menu short by exactly the cells the wide
// glyphs added — and the label is then clipped by its own width. The
// mnemonic test stays green through that, because an underline at the
// right column of a too-narrow popup is still at the right column.
//
// This is also the assertion that pins the ide-shell merge: Menu.lead()
// added a check column to the same expression, and `lead` is a cell
// count while the label needed a column measure. Only one of the two
// halves had a test before this.
func TestDropdownSizesToItsLabelsInColumns(t *testing.T) {
	paint := func(item string) *render.Buffer {
		bar := &MenuBar{Menus: []Menu{{
			Title: "M",
			Items: []MenuItem{{Text: item, Action: gooey.Command(func() {})}},
		}}}
		c := gooey.NewComposer(&Canvas{Children: []gooey.Component{bar}}, 40, 8)
		c.Frame()
		c.Focus().SetFocus(bar)
		c.Frame()
		c.HandleKey(input.Named(input.KeyEnter))
		c.Frame()
		return c.Cells()
	}
	// Four columns in both arms; two runes in one, four in the other.
	wide, narrow := paint(wideLabel), paint(narrowLabel)

	if w, n := paintedCols(wide), paintedCols(narrow); w != n {
		t.Errorf("the dropdown holding a 4-column CJK label spans to column %d, "+
			"the 4-column ASCII one to %d — the popup is measured from its item "+
			"labels, so a rune count sizes it short by exactly the cells the wide "+
			"glyphs added", w, n)
	}

	// And the shortfall is not absorbed by clipping the label instead.
	if row := rowHolding(wide, "世"); !strings.Contains(row, "世界") {
		t.Errorf("the dropdown row reads %q, want it to contain %q — a popup "+
			"measured in runes is too narrow for its own label and clips it",
			row, "世界")
	}
}
