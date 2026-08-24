package components

import (
	"testing"

	"github.com/WonderForgeLabs/gooey"
)

// RowBounds is the seam an EDITOR OVER A CELL needs: the view is one
// focus stop by design, so an in-cell caret is an overlay positioned over
// the selected row's arranged rect rather than a focusable child of the
// row. Its whole contract is "where did row i land, and did it land at
// all".

// TestRowBoundsReportsTheArrangedRowsInOrder.
func TestRowBoundsReportsTheArrangedRowsInOrder(t *testing.T) {
	_, _, v, c := newList(t, numbered(4), 20, 4)
	defer c.Close()
	c.Frame()

	var last gooey.Rect
	for i := 0; i < 4; i++ {
		b, ok := v.RowBounds(i)
		if !ok {
			t.Fatalf("row %d is not realized in a view sized to hold all four", i)
		}
		if b.W <= 0 || b.H <= 0 {
			t.Errorf("row %d reported %+v", i, b)
		}
		if i > 0 && b.Y <= last.Y {
			t.Errorf("row %d is at y=%d, not below row %d at y=%d", i, b.Y, i-1, last.Y)
		}
		last = b
	}
	// It must be the ARRANGED rect, not a measurement: the view is 20
	// wide, so the row is too.
	if b, _ := v.RowBounds(0); b.W != 20 {
		t.Errorf("row 0 is %d wide in a 20-wide view; RowBounds is not reporting the arranged "+
			"rect", b.W)
	}
}

// TestRowBoundsRefusesRowsThatAreNotOnScreen is the important half.
//
// Realization is windowed, so a scrolled-away row has no rect — and the
// alternative to saying so is a caller positioning an editor at the zero
// rect, i.e. a live control in the view's top-left corner over a row it
// does not belong to. The second return is what makes that impossible to
// get wrong by accident.
func TestRowBoundsRefusesRowsThatAreNotOnScreen(t *testing.T) {
	_, sel, v, c := newList(t, numbered(40), 20, 4)
	defer c.Close()
	c.Frame()

	// Off the end of the collection entirely.
	if b, ok := v.RowBounds(400); ok {
		t.Errorf("a row past the end of the list reported %+v", b)
	}
	if b, ok := v.RowBounds(-1); ok {
		t.Errorf("a negative index reported %+v", b)
	}

	// Scrolled out of the window. The selection keeps its own row
	// visible, so moving it to the end is what pushes row 0 out.
	sel.Set(39)
	c.Frame()
	if b, ok := v.RowBounds(0); ok {
		t.Errorf("row 0 reported %+v after the window scrolled to row 39; it is not realized "+
			"and has no rect", b)
	}
	if _, ok := v.RowBounds(39); !ok {
		t.Fatal("the selected row is not realized: the window rule is broken, and this test " +
			"would pass for a RowBounds that always says no")
	}
}

// TestRowBoundsRefusesARowArrangedToNothing is the OTHER zero case, and
// it is a different one: the row IS realized — it is in v.rows, it has a
// paint node — and the view was arranged with no height, so the row was
// placed at H:0.
//
// It is worth its own test because it is the shape of the bug this seam
// was built next to: a pane where a greedy ItemsView left a sibling
// arranged at W:0 H:0, every unit working perfectly and nothing on
// screen. A caller that positioned an in-cell editor from a realized row
// with no size would put a live control in the corner of a list that is
// not being displayed.
//
// Written after a mutation survived: replacing the size check with a bare
// `true` left TestRowBoundsRefusesRowsThatAreNotOnScreen green, because
// an unrealized row never reaches that return at all.
//
// The reachable half of the check is the WIDTH. window() floors the
// visible count by rowH, so a realized row always has its full height —
// the H clause of the same expression is belt-and-braces, and the first
// attempt at this test (a view with no HEIGHT) proved nothing, because
// window() returns a count of zero and no row is realized to ask about.
// A view with no WIDTH does realize its rows, and places them at W:0.
func TestRowBoundsRefusesARowArrangedToNothing(t *testing.T) {
	_, _, v, c := newList(t, numbered(4), 0, 4)
	defer c.Close()
	c.Frame()

	if b, ok := v.RowBounds(0); ok {
		t.Errorf("row 0 reported %+v in a view with no width; a realized row arranged to "+
			"nothing has no place to put an editor either", b)
	}

	// The discrimination half: give the view width and the same row
	// answers. Without it this passes for a RowBounds that never says
	// yes.
	c.Resize(20, 4)
	c.Frame()
	if _, ok := v.RowBounds(0); !ok {
		t.Fatal("row 0 is still refused after the view was given width")
	}
}

// TestRowBoundsIsALayoutReadAndCostsNoDamage.
//
// A caller asks this from its own Arrange, which runs outside any
// evaluation context, so the reads inside must record no dependency and
// the call itself must dirty nothing. A damage count is the only thing
// that can say so.
func TestRowBoundsIsALayoutReadAndCostsNoDamage(t *testing.T) {
	_, _, v, c := newList(t, numbered(4), 20, 4)
	defer c.Close()
	c.Frame()
	if _, n := c.Frame(); n != 0 {
		t.Fatalf("the list had not settled: %d components repainted", n)
	}
	for i := 0; i < 4; i++ {
		v.RowBounds(i)
	}
	if _, n := c.Frame(); n != 0 {
		t.Errorf("asking for row bounds repainted %d components; it is plain bookkeeping "+
			"about a frame that already happened", n)
	}
}
